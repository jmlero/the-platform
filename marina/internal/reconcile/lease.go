package reconcile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

var (
	ErrExternalRunMissing        = errors.New("external run is missing")
	ErrExternalOwnershipMismatch = errors.New("external run ownership does not match the journal")
	ErrExternalRunState          = errors.New("external run has an unsupported state")
	ErrExternalLeaseMissing      = errors.New("external active run has no lease")
)

type LeaseReconciliationStatus string

const (
	LeaseConfirmed        LeaseReconciliationStatus = "confirmed"
	LeaseRenewed          LeaseReconciliationStatus = "renewed"
	LeaseProviderTerminal LeaseReconciliationStatus = "provider_terminal"
	LeaseLocalTerminal    LeaseReconciliationStatus = "local_terminal"
)

type LeaseReconciliation struct {
	ExecutionID           string
	Status                LeaseReconciliationStatus
	ProviderRunStatus     workcontrol.RunStatus
	ProviderResultSummary string
	LeaseExpiresAt        *time.Time
	LocalVersion          int64
}

type LeaseProvider interface {
	workcontrol.HealthChecker
	workcontrol.WorkInspection
	workcontrol.RunControl
}

type LeaseJournal interface {
	GetExecution(context.Context, string) (execution.Execution, error)
	ReconcileLease(context.Context, string, int64, time.Time, string) (execution.Execution, error)
	Transition(context.Context, string, execution.Transition) (execution.Execution, error)
}

const budgetBlockReason = "[marina:budget_exhausted] execution budget exhausted"

// LeaseReconciler confirms that Marina still owns the exact external run
// before any runtime recovery is allowed. It never claims replacement work.
type LeaseReconciler struct {
	provider      LeaseProvider
	journal       LeaseJournal
	clock         func() time.Time
	leaseDuration time.Duration
}

func NewLeaseReconciler(
	provider LeaseProvider,
	journal LeaseJournal,
	leaseDuration time.Duration,
) (*LeaseReconciler, error) {
	if provider == nil {
		return nil, errors.New("lease provider is required")
	}
	if journal == nil {
		return nil, errors.New("lease journal is required")
	}
	if leaseDuration < time.Second {
		return nil, errors.New("lease duration must be at least one second")
	}
	return &LeaseReconciler{
		provider:      provider,
		journal:       journal,
		clock:         time.Now,
		leaseDuration: leaseDuration,
	}, nil
}

func (reconciler *LeaseReconciler) WithClock(clock func() time.Time) *LeaseReconciler {
	if clock != nil {
		reconciler.clock = clock
	}
	return reconciler
}

func (reconciler *LeaseReconciler) Reconcile(
	ctx context.Context,
	executionID string,
	renewBefore time.Duration,
) (LeaseReconciliation, error) {
	if strings.TrimSpace(executionID) == "" {
		return LeaseReconciliation{}, fmt.Errorf("execution ID is required: %w", execution.ErrInvalidInput)
	}
	if renewBefore < 0 {
		return LeaseReconciliation{}, fmt.Errorf("lease renewal window cannot be negative: %w", execution.ErrInvalidInput)
	}
	record, err := reconciler.journal.GetExecution(ctx, executionID)
	if err != nil {
		return LeaseReconciliation{}, fmt.Errorf("load execution for provider reconciliation: %w", err)
	}
	if record.State.Terminal() {
		return LeaseReconciliation{
			ExecutionID:  record.ID,
			Status:       LeaseLocalTerminal,
			LocalVersion: record.Version,
		}, nil
	}
	if err := reconciler.provider.Health(ctx); err != nil {
		return LeaseReconciliation{}, fmt.Errorf("verify provider compatibility before lease reconciliation: %w", err)
	}

	run, err := reconciler.getOwnedRun(ctx, record)
	if err != nil {
		return LeaseReconciliation{}, err
	}
	result := LeaseReconciliation{
		ExecutionID:           record.ID,
		ProviderRunStatus:     run.Status,
		ProviderResultSummary: run.ResultSummary,
		LeaseExpiresAt:        cloneTime(run.LeaseExpiresAt),
		LocalVersion:          record.Version,
	}
	if run.Status.Terminal() {
		result.Status = LeaseProviderTerminal
		updated, transitionErr := reconciler.reconcileProviderTerminal(ctx, record, run)
		if transitionErr == nil {
			result.LocalVersion = updated.Version
		}
		return result, transitionErr
	}
	if !run.Status.Active() {
		return LeaseReconciliation{}, fmt.Errorf("provider run %s has state %q: %w", run.ID, run.Status, ErrExternalRunState)
	}
	if run.LeaseExpiresAt == nil {
		return LeaseReconciliation{}, fmt.Errorf("provider run %s: %w", run.ID, ErrExternalLeaseMissing)
	}

	now := reconciler.clock().UTC()
	renewed := false
	if !run.LeaseExpiresAt.After(now.Add(renewBefore)) {
		if err := reconciler.provider.RenewRunLease(ctx, run.ID, workcontrol.LeaseRenewal{
			AgentID: record.AgentID,
			Lease:   reconciler.leaseDuration,
		}); err != nil {
			return LeaseReconciliation{}, fmt.Errorf("renew provider run lease: %w", err)
		}
		run, err = reconciler.getOwnedRun(ctx, record)
		if err != nil {
			return LeaseReconciliation{}, fmt.Errorf("confirm renewed provider run: %w", err)
		}
		result.ProviderRunStatus = run.Status
		result.ProviderResultSummary = run.ResultSummary
		result.LeaseExpiresAt = cloneTime(run.LeaseExpiresAt)
		if run.Status.Terminal() {
			result.Status = LeaseProviderTerminal
			updated, transitionErr := reconciler.reconcileProviderTerminal(ctx, record, run)
			if transitionErr == nil {
				result.LocalVersion = updated.Version
			}
			return result, transitionErr
		}
		if !run.Status.Active() {
			return LeaseReconciliation{}, fmt.Errorf("provider run %s changed to state %q after heartbeat: %w", run.ID, run.Status, ErrExternalRunState)
		}
		if run.LeaseExpiresAt == nil || !run.LeaseExpiresAt.After(now) {
			return LeaseReconciliation{}, fmt.Errorf("provider heartbeat did not return a future lease for run %s: %w", run.ID, ErrExternalLeaseMissing)
		}
		renewed = true
	}

	updated, err := reconciler.persistProviderLease(ctx, record, *run.LeaseExpiresAt)
	if err != nil {
		return LeaseReconciliation{}, err
	}
	result.LeaseExpiresAt = cloneTime(run.LeaseExpiresAt)
	result.LocalVersion = updated.Version
	if renewed {
		result.Status = LeaseRenewed
	} else {
		result.Status = LeaseConfirmed
	}
	return result, nil
}

func (reconciler *LeaseReconciler) StopBudget(
	ctx context.Context,
	executionID string,
) (execution.Execution, error) {
	record, err := reconciler.journal.GetExecution(ctx, executionID)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("load budget-exhausted execution: %w", err)
	}
	if record.State.Terminal() {
		return record, nil
	}
	if !record.BudgetExpired(reconciler.clock().UTC()) {
		return execution.Execution{}, fmt.Errorf("execution budget is not exhausted: %w", execution.ErrInvalidInput)
	}
	if err := reconciler.provider.Health(ctx); err != nil {
		return execution.Execution{}, fmt.Errorf("verify provider compatibility before budget stop: %w", err)
	}
	run, err := reconciler.getOwnedRun(ctx, record)
	if err != nil {
		return execution.Execution{}, err
	}
	if run.Status.Terminal() {
		return reconciler.reconcileProviderTerminal(ctx, record, run)
	}
	if !run.Status.Active() {
		return execution.Execution{}, fmt.Errorf("provider run %s has state %q: %w", run.ID, run.Status, ErrExternalRunState)
	}

	blockErr := reconciler.provider.BlockRun(ctx, run.ID, budgetBlockReason)
	confirmed, confirmErr := reconciler.getOwnedRun(ctx, record)
	if confirmErr != nil {
		if blockErr != nil {
			return execution.Execution{}, fmt.Errorf("block budget-exhausted provider run: %v; confirm state: %w", blockErr, confirmErr)
		}
		return execution.Execution{}, fmt.Errorf("confirm budget-exhausted provider run: %w", confirmErr)
	}
	if !confirmed.Status.Terminal() {
		if blockErr != nil {
			return execution.Execution{}, fmt.Errorf("block budget-exhausted provider run: %w", blockErr)
		}
		return execution.Execution{}, fmt.Errorf("provider run %s remained %q after budget stop: %w", confirmed.ID, confirmed.Status, ErrExternalRunState)
	}
	return reconciler.reconcileProviderTerminal(ctx, record, confirmed)
}

func (reconciler *LeaseReconciler) reconcileProviderTerminal(
	ctx context.Context,
	record execution.Execution,
	run workcontrol.Run,
) (execution.Execution, error) {
	target, failureClass, failureMessage, err := terminalTransition(run)
	if err != nil {
		return execution.Execution{}, err
	}
	if record.State.Terminal() {
		if record.State == target {
			return record, nil
		}
		return execution.Execution{}, fmt.Errorf("local state %q differs from provider terminal state %q: %w", record.State, run.Status, execution.ErrConflict)
	}
	transition := execution.Transition{
		ExpectedVersion: record.Version,
		Target:          target,
		Message:         fmt.Sprintf("external provider reported run %s", run.Status),
		FailureClass:    failureClass,
		FailureMessage:  failureMessage,
	}
	updated, err := reconciler.journal.Transition(ctx, record.ID, transition)
	if err == nil {
		return updated, nil
	}
	if !errors.Is(err, execution.ErrConflict) {
		return execution.Execution{}, fmt.Errorf("reconcile provider terminal state: %w", err)
	}
	current, getErr := reconciler.journal.GetExecution(ctx, record.ID)
	if getErr != nil {
		return execution.Execution{}, fmt.Errorf("reload terminal execution after conflict: %w", getErr)
	}
	if current.State.Terminal() {
		if current.State == target {
			return current, nil
		}
		return execution.Execution{}, fmt.Errorf("concurrent terminal state %q differs from provider target %q: %w", current.State, target, execution.ErrConflict)
	}
	transition.ExpectedVersion = current.Version
	updated, err = reconciler.journal.Transition(ctx, current.ID, transition)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("reconcile provider terminal state after conflict: %w", err)
	}
	return updated, nil
}

func terminalTransition(run workcontrol.Run) (execution.State, execution.FailureClass, string, error) {
	summary := strings.TrimSpace(run.ResultSummary)
	if summary == "" {
		summary = "external provider reported " + string(run.Status)
	}
	switch run.Status {
	case workcontrol.RunCompleted:
		return execution.StateCompleted, "", "", nil
	case workcontrol.RunBlocked:
		if strings.HasPrefix(summary, "[marina:budget_exhausted]") {
			return execution.StateFailed, execution.FailureBudget, summary, nil
		}
		return execution.StateBlocked, execution.FailureExternal, summary, nil
	case workcontrol.RunFailed:
		return execution.StateFailed, execution.FailureExternal, summary, nil
	case workcontrol.RunCancelled:
		return execution.StateCancelled, execution.FailureExternal, summary, nil
	default:
		return "", "", "", fmt.Errorf("provider run %s has non-terminal state %q: %w", run.ID, run.Status, ErrExternalRunState)
	}
}

func (reconciler *LeaseReconciler) getOwnedRun(
	ctx context.Context,
	record execution.Execution,
) (workcontrol.Run, error) {
	detail, err := reconciler.provider.GetWork(ctx, record.ExternalWorkID)
	if err != nil {
		return workcontrol.Run{}, fmt.Errorf("get external work %s: %w", record.ExternalWorkID, err)
	}
	if detail.Work.ID != record.ExternalWorkID {
		return workcontrol.Run{}, fmt.Errorf("provider returned work %q for %q: %w", detail.Work.ID, record.ExternalWorkID, ErrExternalOwnershipMismatch)
	}
	for _, run := range detail.Runs {
		if run.ID != record.ExternalRunID {
			continue
		}
		if run.WorkItemID != record.ExternalWorkID || run.AgentID != record.AgentID {
			return workcontrol.Run{}, fmt.Errorf("provider run %s owner or work differs from the journal: %w", run.ID, ErrExternalOwnershipMismatch)
		}
		return run, nil
	}
	return workcontrol.Run{}, fmt.Errorf("provider run %s was not found under work %s: %w", record.ExternalRunID, record.ExternalWorkID, ErrExternalRunMissing)
}

func (reconciler *LeaseReconciler) persistProviderLease(
	ctx context.Context,
	record execution.Execution,
	leaseExpiresAt time.Time,
) (execution.Execution, error) {
	if record.LeaseExpiresAt.UnixMilli() == leaseExpiresAt.UnixMilli() {
		return record, nil
	}
	updated, err := reconciler.journal.ReconcileLease(
		ctx,
		record.ID,
		record.Version,
		leaseExpiresAt,
		"external provider lease reconciled before runtime recovery",
	)
	if err == nil {
		return updated, nil
	}
	if !errors.Is(err, execution.ErrConflict) {
		return execution.Execution{}, fmt.Errorf("persist provider lease: %w", err)
	}

	current, getErr := reconciler.journal.GetExecution(ctx, record.ID)
	if getErr != nil {
		return execution.Execution{}, fmt.Errorf("reload execution after lease conflict: %w", getErr)
	}
	if current.State.Terminal() || current.LeaseExpiresAt.UnixMilli() == leaseExpiresAt.UnixMilli() {
		return current, nil
	}
	updated, err = reconciler.journal.ReconcileLease(
		ctx,
		current.ID,
		current.Version,
		leaseExpiresAt,
		"external provider lease reconciled after concurrent journal update",
	)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("persist provider lease after conflict: %w", err)
	}
	return updated, nil
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
