package reconcile

import (
	"context"
	"errors"
	"fmt"
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

type ExecutionRetryJournal interface {
	GetExecution(context.Context, string) (execution.Execution, error)
	RecordExecutionRetry(context.Context, string, int64, execution.FailureClass, string, int) (execution.Execution, error)
	ResetExecutionRetry(context.Context, string, int64) (execution.Execution, error)
	Transition(context.Context, string, execution.Transition) (execution.Execution, error)
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
	plans            RecoveryPlanSource
	reconciler       RunLeaseReconciler
	budgetStopper    BudgetStopper
	interval         time.Duration
	renewBefore      time.Duration
	executionLimit   int
	clock            func() time.Time
	operationTimeout time.Duration
	maxRetries       int
	retryJournal     ExecutionRetryJournal
}

func (manager *LeaseManager) WithBudgetStopper(stopper BudgetStopper) *LeaseManager {
	manager.budgetStopper = stopper
	return manager
}

func (manager *LeaseManager) WithRetryJournal(journal ExecutionRetryJournal) *LeaseManager {
	manager.retryJournal = journal
	return manager
}

func (manager *LeaseManager) WithOperationTimeout(timeout time.Duration) *LeaseManager {
	if timeout > 0 {
		manager.operationTimeout = timeout
	}
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
		plans:            plans,
		reconciler:       reconciler,
		interval:         interval,
		renewBefore:      renewBefore,
		executionLimit:   executionLimit,
		clock:            time.Now,
		operationTimeout: 5 * time.Second,
		maxRetries:       5,
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
		operationCtx, cancel := context.WithTimeout(ctx, manager.operationTimeout)
		if intent.Action == execution.RecoveryStopBudget && manager.budgetStopper != nil {
			attempt.Attempted = true
			attempt.Execution, attempt.Err = manager.budgetStopper.StopBudget(operationCtx, intent.ExecutionID)
		} else if intent.Action != execution.RecoveryNone && intent.Action != execution.RecoveryStopBudget {
			attempt.Attempted = true
			attempt.Result, attempt.Err = manager.reconciler.Reconcile(operationCtx, intent.ExecutionID, manager.renewBefore)
		}
		cancel()
		manager.persistRetryOutcome(ctx, &attempt)
		cycle.Attempts = append(cycle.Attempts, attempt)
	}
	return cycle
}

func (manager *LeaseManager) persistRetryOutcome(ctx context.Context, attempt *LeaseAttempt) {
	if manager.retryJournal == nil || !attempt.Attempted {
		return
	}
	record, err := manager.retryJournal.GetExecution(ctx, attempt.Intent.ExecutionID)
	if err != nil {
		attempt.Err = errors.Join(attempt.Err, fmt.Errorf("load durable retry state: %w", err))
		return
	}
	if attempt.Err == nil {
		_, err = manager.retryJournal.ResetExecutionRetry(ctx, record.ID, record.Version)
		if err != nil {
			attempt.Err = fmt.Errorf("reset durable retry state: %w", err)
		}
		return
	}
	message := attempt.Err.Error()
	updated, err := manager.retryJournal.RecordExecutionRetry(ctx, record.ID, record.Version,
		classifyExternal(attempt.Err), message, manager.maxRetries)
	if err != nil {
		attempt.Err = errors.Join(attempt.Err, fmt.Errorf("persist classified retry: %w", err))
		return
	}
	if updated.ExhaustionReason == "" || updated.State.Terminal() {
		return
	}
	_, err = manager.retryJournal.Transition(ctx, updated.ID, execution.Transition{
		ExpectedVersion: updated.Version, Target: execution.StateFailed,
		Message:      "external reconciliation retry budget exhausted",
		FailureClass: updated.LastErrorClass, FailureMessage: updated.ExhaustionReason,
	})
	if err != nil {
		attempt.Err = errors.Join(attempt.Err, fmt.Errorf("terminally classify exhausted reconciliation: %w", err))
	}
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
