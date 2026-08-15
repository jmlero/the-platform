package reconcile

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
)

type planSourceStub struct {
	intents []execution.RecoveryIntent
	err     error
	limit   int
}

func (source *planSourceStub) Plan(_ context.Context, limit int) ([]execution.RecoveryIntent, error) {
	source.limit = limit
	return source.intents, source.err
}

type runLeaseReconcilerStub struct {
	results map[string]LeaseReconciliation
	errors  map[string]error
	calls   []string
}

type budgetStopperStub struct {
	calls []string
	err   error
}

func (stopper *budgetStopperStub) StopBudget(_ context.Context, executionID string) (execution.Execution, error) {
	stopper.calls = append(stopper.calls, executionID)
	return execution.Execution{ID: executionID, State: execution.StateFailed}, stopper.err
}

func (reconciler *runLeaseReconcilerStub) Reconcile(
	_ context.Context,
	executionID string,
	_ time.Duration,
) (LeaseReconciliation, error) {
	reconciler.calls = append(reconciler.calls, executionID)
	return reconciler.results[executionID], reconciler.errors[executionID]
}

func TestLeaseManagerCyclesOnlyOwnedRecoverableWork(t *testing.T) {
	wantFailure := errors.New("provider unavailable")
	plans := &planSourceStub{intents: []execution.RecoveryIntent{
		{ExecutionID: "budget", Action: execution.RecoveryStopBudget, Reason: "budget exhausted"},
		{ExecutionID: "running", Action: execution.RecoveryInspectRuntime, Reason: "inspect runtime"},
		{ExecutionID: "planning", Action: execution.RecoveryResumePlanning, Reason: "resume planning"},
	}}
	reconciler := &runLeaseReconcilerStub{
		results: map[string]LeaseReconciliation{
			"running": {ExecutionID: "running", Status: LeaseConfirmed},
		},
		errors: map[string]error{"planning": wantFailure},
	}
	manager, err := NewLeaseManager(plans, reconciler, time.Second, 10*time.Second, 25)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	stopper := &budgetStopperStub{}
	manager.WithBudgetStopper(stopper)
	cycle := manager.Cycle(context.Background())
	if cycle.Err != nil {
		t.Fatalf("cycle: %v", cycle.Err)
	}
	if plans.limit != 25 || len(cycle.Attempts) != 3 {
		t.Fatalf("limit = %d, attempts = %#v", plans.limit, cycle.Attempts)
	}
	if !cycle.Attempts[0].Attempted || cycle.Attempts[0].Execution.State != execution.StateFailed {
		t.Fatalf("budget-exhausted execution was not stopped: %#v", cycle.Attempts[0])
	}
	if !cycle.Attempts[1].Attempted || cycle.Attempts[1].Result.Status != LeaseConfirmed {
		t.Fatalf("running attempt = %#v", cycle.Attempts[1])
	}
	if !cycle.Attempts[2].Attempted || !errors.Is(cycle.Attempts[2].Err, wantFailure) {
		t.Fatalf("planning attempt = %#v", cycle.Attempts[2])
	}
	if len(reconciler.calls) != 2 || reconciler.calls[0] != "running" || reconciler.calls[1] != "planning" {
		t.Fatalf("reconcile calls = %#v", reconciler.calls)
	}
	if len(stopper.calls) != 1 || stopper.calls[0] != "budget" {
		t.Fatalf("budget stop calls = %#v", stopper.calls)
	}
}

func TestLeaseManagerSurfacesPlanFailure(t *testing.T) {
	want := errors.New("journal unavailable")
	plans := &planSourceStub{err: want}
	manager, err := NewLeaseManager(plans, &runLeaseReconcilerStub{}, time.Second, 0, 1)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	cycle := manager.Cycle(context.Background())
	if !errors.Is(cycle.Err, want) || len(cycle.Attempts) != 0 {
		t.Fatalf("cycle = %#v", cycle)
	}
}

func TestLeaseManagerRunStopsWithContext(t *testing.T) {
	plans := &planSourceStub{}
	manager, err := NewLeaseManager(plans, &runLeaseReconcilerStub{}, time.Millisecond, 0, 1)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	observed := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		manager.Run(ctx, func(LeaseCycle) {
			select {
			case observed <- struct{}{}:
			default:
			}
		})
		close(done)
	}()
	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Fatal("manager did not run a scheduled cycle")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("manager did not stop after cancellation")
	}
}

func TestLeaseManagerRejectsInvalidConfiguration(t *testing.T) {
	plans := &planSourceStub{}
	reconciler := &runLeaseReconcilerStub{}
	tests := []struct {
		name       string
		plans      RecoveryPlanSource
		reconciler RunLeaseReconciler
		interval   time.Duration
		renew      time.Duration
		limit      int
	}{
		{name: "missing plans", reconciler: reconciler, interval: time.Second, limit: 1},
		{name: "missing reconciler", plans: plans, interval: time.Second, limit: 1},
		{name: "zero interval", plans: plans, reconciler: reconciler, limit: 1},
		{name: "negative renewal", plans: plans, reconciler: reconciler, interval: time.Second, renew: -time.Second, limit: 1},
		{name: "zero limit", plans: plans, reconciler: reconciler, interval: time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewLeaseManager(test.plans, test.reconciler, test.interval, test.renew, test.limit); err == nil {
				t.Fatal("invalid manager configuration was accepted")
			}
		})
	}
}

func TestLeaseManagerPersistsAndProjectsPermanentExternalFailure(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	journal, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "manager-failure.db"), sqlite.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	record, _, err := journal.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "appdeck", ProjectID: "project-1",
		ExternalWorkID: "task-manager", ExternalRunID: "run-manager", AgentID: "agent-manager",
		ExternalRunVersion: 1, FenceToken: 1, TaskVersion: 1, TaskDigest: "digest", BindingVersion: 1,
		LeaseExpiresAt: now.Add(time.Minute), Budget: execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 5, MaxTokens: 1}})
	if err != nil {
		t.Fatal(err)
	}
	plans := &planSourceStub{intents: []execution.RecoveryIntent{{ExecutionID: record.ID, Action: execution.RecoveryResumePlanning}}}
	reconciler := &runLeaseReconcilerStub{errors: map[string]error{record.ID: ErrExternalOwnershipMismatch}}
	manager, err := NewLeaseManager(plans, reconciler, time.Second, 10*time.Second, 1)
	if err != nil {
		t.Fatal(err)
	}
	manager.WithRetryJournal(journal)
	cycle := manager.Cycle(ctx)
	if len(cycle.Attempts) != 1 || !errors.Is(cycle.Attempts[0].Err, ErrExternalOwnershipMismatch) {
		t.Fatalf("cycle = %#v", cycle)
	}
	failed, err := journal.GetExecution(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != execution.StateFailed || failed.FailureClass != execution.FailurePermanent || failed.ExhaustionReason == "" {
		t.Fatalf("classified execution = %#v", failed)
	}
	ready, err := journal.ListReadyOutbox(ctx, 10)
	if err != nil || len(ready) != 1 || ready[0].Payload["failure_class"] != string(execution.FailurePermanent) {
		t.Fatalf("failure projection = %#v, %v", ready, err)
	}
}

type blockingLeaseReconciler struct{}

func (blockingLeaseReconciler) Reconcile(ctx context.Context, _ string, _ time.Duration) (LeaseReconciliation, error) {
	<-ctx.Done()
	return LeaseReconciliation{}, ctx.Err()
}

func TestLeaseManagerBoundsExternalOperationAndPersistsBackoff(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	journal, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "manager-timeout.db"), sqlite.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	record, _, err := journal.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "appdeck", ProjectID: "project-1",
		ExternalWorkID: "task-timeout", ExternalRunID: "run-timeout", AgentID: "agent-timeout",
		ExternalRunVersion: 1, FenceToken: 1, TaskVersion: 1, TaskDigest: "digest", BindingVersion: 1,
		LeaseExpiresAt: now.Add(time.Minute), Budget: execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 5, MaxTokens: 1}})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewLeaseManager(&planSourceStub{intents: []execution.RecoveryIntent{{ExecutionID: record.ID, Action: execution.RecoveryResumePlanning}}},
		blockingLeaseReconciler{}, time.Second, 10*time.Second, 1)
	if err != nil {
		t.Fatal(err)
	}
	manager.WithRetryJournal(journal).WithOperationTimeout(5 * time.Millisecond)
	cycle := manager.Cycle(ctx)
	if len(cycle.Attempts) != 1 || !errors.Is(cycle.Attempts[0].Err, context.DeadlineExceeded) {
		t.Fatalf("timeout cycle = %#v", cycle)
	}
	deferred, err := journal.GetExecution(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deferred.State != execution.StatePlanning || deferred.RetryAttempt != 1 || deferred.NextEligibleAt == nil || deferred.LastErrorClass != execution.FailureTransient {
		t.Fatalf("durable backoff = %#v", deferred)
	}
}
