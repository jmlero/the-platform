package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
)

func TestEngineObservationsDoNotChargeModelReservationsAsActualUsage(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "journal.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	now := time.Now().UTC().Truncate(time.Millisecond)
	deadline := now.Add(time.Hour)
	record, _, err := store.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", LeaseExpiresAt: deadline,
		Budget: execution.Budget{DeadlineAt: deadline, MaxAttempts: 3, MaxTokens: 100, MaxCostMicros: 100}})
	if err != nil {
		t.Fatal(err)
	}
	_, attempt, _, err := store.EnsureRuntimeAttempt(ctx, record.ID, record.Version, rt.Specification{
		IdempotencyKey: "engine-accounting", RepositoryURI: "https://git.example/repo", SourceRevision: strings.Repeat("a", 40),
		ContextURI: "artifact://sha256:" + strings.Repeat("b", 64), ContextDigest: "sha256:" + strings.Repeat("c", 64), ProfileDigest: strings.Repeat("d", 64),
		HarnessVersion: "native-codex", Engine: &rt.EngineSelection{Backend: "kagent", Version: "pinned", Endpoint: "engine:8083", Namespace: "engine", Harness: "codex", Template: "marina-accounting", Revision: strings.Repeat("e", 64)},
		CredentialReferences: []rt.CredentialReference{{Kind: "engine-gateway", Reference: "secret://engine/marina-accounting/gateway", ExpiresAt: deadline}},
		Limits:               rt.Limits{CPUMillis: 1000, MemoryBytes: 1024, EphemeralStorageBytes: 1024, WallTimeSeconds: 60, MaxProcesses: 32},
		NetworkPolicy:        "gateway-only", FilesystemPolicy: "ephemeral", AuthorityFence: 1, DeadlineAt: deadline, PolicyVersion: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := store.EnsureEngineAttempt(ctx, attempt.Specification)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"ready", "sending", "active"} {
		engine.Phase = phase
		engine, err = store.UpdateEngineAttempt(ctx, engine)
		if err != nil {
			t.Fatal(err)
		}
	}
	observe := func() {
		t.Helper()
		tokens, cost, err := store.ModelUsage(ctx, attempt.ID)
		if err != nil {
			t.Fatal(err)
		}
		attempt, err = store.RecordRuntimeObservation(ctx, attempt.ID, attempt.Version, rt.Observation{Found: true, ProviderHandle: "engine/instance", State: rt.StateRunning, Usage: rt.Usage{Tokens: tokens, CostMicros: cost}})
		if err != nil {
			t.Fatal(err)
		}
	}
	reopen := func() {
		t.Helper()
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		store, err = Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.ReserveModelCall(ctx, record.ID, attempt.ID, 1, 60, 60)
	if err != nil {
		t.Fatal(err)
	}
	observe()
	reopen()
	pending, err := store.GetExecution(ctx, record.ID)
	if err != nil || pending.UsedTokens != 0 || pending.UsedCostMicros != 0 {
		t.Fatalf("runtime observation charged an unfinished model call: %+v %v", pending, err)
	}
	totals, err := store.ExecutionModelTotals(ctx, record.ID)
	if err != nil || totals.Tokens != 0 || totals.CostMicros != 0 || totals.PendingCalls != 1 || totals.ReservedTokens != 60 || totals.ReservedCostMicros != 60 {
		t.Fatalf("pending engine observation must retain a reservation without spending it: %+v %v", totals, err)
	}
	if err := store.FinishModelCall(ctx, first, 10, 10); err != nil {
		t.Fatal(err)
	}
	second, err := store.ReserveModelCall(ctx, record.ID, attempt.ID, 1, 50, 50)
	if err != nil {
		t.Fatal(err)
	}
	observe()
	reopen()
	if err := store.FinishModelCall(ctx, second, 20, 20); err != nil {
		t.Fatal(err)
	}
	observe()
	updated, err := store.GetExecution(ctx, record.ID)
	if err != nil || updated.UsedTokens != 30 || updated.UsedCostMicros != 30 || !updated.DeadlineAt.Equal(deadline) || updated.MaxTokens != 100 || updated.MaxCostMicros != 100 {
		t.Fatalf("known gateway usage must be charged once within the original budget: %+v %v", updated, err)
	}
	if _, err := store.ReserveModelCall(ctx, record.ID, attempt.ID, 1, 71, 71); !errors.Is(err, execution.ErrBudgetExhausted) {
		t.Fatalf("engine observation expanded the original budget: %v", err)
	}
}
