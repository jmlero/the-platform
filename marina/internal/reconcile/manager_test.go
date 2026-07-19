package reconcile

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
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
