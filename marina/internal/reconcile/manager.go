package reconcile

import (
	"context"
	"errors"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
)

type RecoveryPlanSource interface {
	Plan(context.Context, int) ([]execution.RecoveryIntent, error)
}

type RunLeaseReconciler interface {
	Reconcile(context.Context, string, time.Duration) (LeaseReconciliation, error)
}

type BudgetStopper interface {
	StopBudget(context.Context, string) (execution.Execution, error)
}

type LeaseAttempt struct {
	Intent    execution.RecoveryIntent
	Attempted bool
	Result    LeaseReconciliation
	Execution execution.Execution
	Err       error
}

type LeaseCycle struct {
	StartedAt time.Time
	Attempts  []LeaseAttempt
	Err       error
}

// LeaseManager continuously protects ownership of executions already present
// in the journal. It has no work-discovery or claim capability by design.
type LeaseManager struct {
	plans          RecoveryPlanSource
	reconciler     RunLeaseReconciler
	budgetStopper  BudgetStopper
	interval       time.Duration
	renewBefore    time.Duration
	executionLimit int
	clock          func() time.Time
}

func (manager *LeaseManager) WithBudgetStopper(stopper BudgetStopper) *LeaseManager {
	manager.budgetStopper = stopper
	return manager
}

func NewLeaseManager(
	plans RecoveryPlanSource,
	reconciler RunLeaseReconciler,
	interval time.Duration,
	renewBefore time.Duration,
	executionLimit int,
) (*LeaseManager, error) {
	if plans == nil {
		return nil, errors.New("recovery plan source is required")
	}
	if reconciler == nil {
		return nil, errors.New("run lease reconciler is required")
	}
	if interval <= 0 {
		return nil, errors.New("reconciliation interval must be positive")
	}
	if renewBefore < 0 {
		return nil, errors.New("lease renewal window cannot be negative")
	}
	if executionLimit < 1 {
		return nil, errors.New("execution limit must be positive")
	}
	return &LeaseManager{
		plans:          plans,
		reconciler:     reconciler,
		interval:       interval,
		renewBefore:    renewBefore,
		executionLimit: executionLimit,
		clock:          time.Now,
	}, nil
}

func (manager *LeaseManager) Cycle(ctx context.Context) LeaseCycle {
	cycle := LeaseCycle{StartedAt: manager.clock().UTC()}
	intents, err := manager.plans.Plan(ctx, manager.executionLimit)
	if err != nil {
		cycle.Err = err
		return cycle
	}
	cycle.Attempts = make([]LeaseAttempt, 0, len(intents))
	for _, intent := range intents {
		attempt := LeaseAttempt{Intent: intent}
		if intent.Action == execution.RecoveryStopBudget && manager.budgetStopper != nil {
			attempt.Attempted = true
			attempt.Execution, attempt.Err = manager.budgetStopper.StopBudget(ctx, intent.ExecutionID)
		} else if intent.Action != execution.RecoveryNone && intent.Action != execution.RecoveryStopBudget {
			attempt.Attempted = true
			attempt.Result, attempt.Err = manager.reconciler.Reconcile(ctx, intent.ExecutionID, manager.renewBefore)
		}
		cycle.Attempts = append(cycle.Attempts, attempt)
	}
	return cycle
}

// Run waits one interval before the first cycle. Call Cycle once during
// startup when immediate reconciliation is required.
func (manager *LeaseManager) Run(ctx context.Context, observe func(LeaseCycle)) {
	ticker := time.NewTicker(manager.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cycle := manager.Cycle(ctx)
			if observe != nil {
				observe(cycle)
			}
		}
	}
}
