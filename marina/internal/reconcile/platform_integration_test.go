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

// The control loop claims, binds and cancels a client's Run on Marina's own
// ledger, with no remote provider.
func TestControlLoopRunsClientWorkOnTheMarinaLedger(t *testing.T) {
	ctx := context.Background()
	journal, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "platform.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	identity, err := journal.EnsureInstallationIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "factory.acceptance", Provider: "marina", Repository: "disabled://m1", Runtime: "disabled:m1",
		PolicyVersion: "m1", ContextConfig: "disabled:m1", ArtifactConfig: "disabled:m1", DeliveryConfig: "disabled:m1", BudgetMinutes: 20, MaxConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	platform := journal.Platform()
	if _, _, err := platform.SubmitWork(ctx, sqlite.NewPlatformWork{Namespace: "factory", Name: "task-1", UID: "uid-1", ProjectID: "factory.acceptance",
		Item: workcontrol.WorkItem{Title: "Reject an empty range", AcceptanceCriteria: "A test passes", BudgetMinutes: 20}}); err != nil {
		t.Fatal(err)
	}
	controller, err := reconcile.NewControlController(platform, journal, binding, identity, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if cycle := controller.Cycle(ctx); len(cycle.Errors) != 0 || cycle.Acquisitions != 1 {
		t.Fatalf("intake cycle = %+v", cycle)
	}
	executions, err := journal.ListActiveExecutions(ctx, 10)
	if err != nil || len(executions) != 1 || executions[0].ExternalWorkID != "factory.task-1" {
		t.Fatalf("executions = %+v %v", executions, err)
	}
	detail, err := platform.GetWork(ctx, "factory.task-1")
	if err != nil || len(detail.Runs) != 1 || detail.Runs[0].ID != executions[0].ExternalRunID || detail.Runs[0].Status != workcontrol.RunPlanning {
		t.Fatalf("ledger run = %+v %v", detail, err)
	}
	// A restart replays the cycle without a second claim.
	again, err := reconcile.NewControlController(platform, journal, binding, identity, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if cycle := again.Cycle(ctx); cycle.Acquisitions != 0 {
		t.Fatalf("restart claimed again: %+v", cycle)
	}
	if err := platform.RequestCancel(ctx, "factory.task-1", "deck", "not needed"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if cycle := again.Cycle(ctx); len(cycle.Errors) != 0 {
			t.Fatalf("cancel cycle %d = %+v", i, cycle)
		}
	}
	record, err := journal.GetExecution(ctx, executions[0].ID)
	if err != nil || record.State != execution.StateCancelled {
		t.Fatalf("execution = %+v %v", record, err)
	}
	if detail, _ := platform.GetWork(ctx, "factory.task-1"); detail.Runs[0].Status != workcontrol.RunCancelled || detail.Work.Status != "cancelled" {
		t.Fatalf("ledger after cancel = %+v", detail.Runs[0])
	}
}
