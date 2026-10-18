package reconcile_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/reconcile"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func TestRestartCycleRenewsExactPersistedRunWithoutClaiming(t *testing.T) {
	ctx := context.Background()
	journalTime := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	databasePath := filepath.Join(t.TempDir(), "marina.db")
	store, err := sqlite.Open(ctx, databasePath, sqlite.WithClock(func() time.Time { return journalTime }))
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	platform := store.Platform()
	if _, _, err := platform.SubmitWork(ctx, sqlite.NewPlatformWork{Namespace: "factory", Name: "task-1", UID: "uid-1", ProjectID: "factory.acceptance",
		Item: workcontrol.WorkItem{Title: "Persisted work", AcceptanceCriteria: "A test passes", BudgetMinutes: 60}}); err != nil {
		t.Fatal(err)
	}
	run, err := platform.ClaimWork(ctx, "factory.task-1", workcontrol.Claim{IdempotencyKey: "claim", AgentID: "marina-1", ExpectedTaskVersion: 1, Lease: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := store.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "marina", ExternalWorkID: "factory.task-1", ExternalRunID: run.ID, AgentID: "marina-1",
		ExternalRunVersion: run.Version, FenceToken: run.FenceToken, LeaseExpiresAt: *run.LeaseExpiresAt,
		Budget: execution.Budget{DeadlineAt: journalTime.Add(time.Hour), MaxAttempts: 3, MaxTokens: 10_000}})
	if err != nil {
		t.Fatalf("ensure execution: %v", err)
	}
	record, err = store.Transition(ctx, record.ID, execution.Transition{ExpectedVersion: record.Version, Target: execution.StateRunning, Message: "runtime started before restart"})
	if err != nil {
		t.Fatalf("transition execution: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close journal before restart: %v", err)
	}

	journalTime = journalTime.Add(6 * time.Minute)
	store, err = sqlite.Open(ctx, databasePath, sqlite.WithClock(func() time.Time { return journalTime }))
	if err != nil {
		t.Fatalf("reopen journal: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	platform = store.Platform()
	planner, err := reconcile.NewPlanner(store, 10*time.Second)
	if err != nil {
		t.Fatalf("new planner: %v", err)
	}
	planner.WithClock(func() time.Time { return journalTime })
	providerReconciler, err := reconcile.NewLeaseReconciler(platform, store, 30*time.Second)
	if err != nil {
		t.Fatalf("new lease reconciler: %v", err)
	}
	providerReconciler.WithClock(func() time.Time { return journalTime })
	manager, err := reconcile.NewLeaseManager(planner, providerReconciler, time.Minute, 10*time.Second, 100)
	if err != nil {
		t.Fatalf("new lease manager: %v", err)
	}
	manager.WithBudgetStopper(providerReconciler)

	cycle := manager.Cycle(ctx)
	if cycle.Err != nil || len(cycle.Attempts) != 1 || cycle.Attempts[0].Err != nil {
		t.Fatalf("restart cycle = %#v", cycle)
	}
	if cycle.Attempts[0].Intent.Action != execution.RecoveryReconcileLease || cycle.Attempts[0].Result.Status != reconcile.LeaseRenewed {
		t.Fatalf("restart attempt = %#v", cycle.Attempts[0])
	}
	detail, err := platform.GetWork(ctx, "factory.task-1")
	if err != nil || len(detail.Runs) != 1 || detail.Runs[0].ID != run.ID || detail.Runs[0].Version != run.Version+1 {
		t.Fatalf("the persisted run was not renewed in place: %+v %v", detail.Runs, err)
	}
	persisted, err := store.GetExecution(ctx, record.ID)
	if err != nil {
		t.Fatalf("load reconciled execution: %v", err)
	}
	if persisted.State != execution.StateRunning || !persisted.LeaseExpiresAt.Equal(*detail.Runs[0].LeaseExpiresAt) {
		t.Fatalf("persisted execution = %#v, ledger lease %v", persisted, detail.Runs[0].LeaseExpiresAt)
	}
}
