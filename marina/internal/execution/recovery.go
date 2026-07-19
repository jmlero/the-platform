package execution

import "time"

type RecoveryAction string

const (
	RecoveryNone               RecoveryAction = "none"
	RecoveryStopBudget         RecoveryAction = "stop_budget"
	RecoveryReconcileLease     RecoveryAction = "reconcile_lease"
	RecoveryRenewLease         RecoveryAction = "renew_lease"
	RecoveryResumePlanning     RecoveryAction = "resume_planning"
	RecoveryReconcileDecision  RecoveryAction = "reconcile_decision"
	RecoveryInspectRuntime     RecoveryAction = "inspect_runtime"
	RecoveryResumeVerification RecoveryAction = "resume_verification"
)

type RecoveryIntent struct {
	ExecutionID string
	Action      RecoveryAction
	Reason      string
}

// PlanRecovery is deliberately side-effect free. A future controller can
// persist and execute the returned intent without guessing after a restart.
func PlanRecovery(record Execution, now time.Time, renewBefore time.Duration) RecoveryIntent {
	intent := RecoveryIntent{ExecutionID: record.ID, Action: RecoveryNone}
	if record.State.Terminal() {
		intent.Reason = "execution is terminal"
		return intent
	}
	if record.BudgetExpired(now) {
		intent.Action = RecoveryStopBudget
		intent.Reason = "execution budget is exhausted"
		return intent
	}
	if record.LeaseExpired(now) {
		intent.Action = RecoveryReconcileLease
		intent.Reason = "external lease expired before recovery"
		return intent
	}
	if renewBefore > 0 && !record.LeaseExpiresAt.After(now.Add(renewBefore)) {
		intent.Action = RecoveryRenewLease
		intent.Reason = "external lease is approaching expiry"
		return intent
	}

	switch record.State {
	case StatePlanning:
		intent.Action = RecoveryResumePlanning
		intent.Reason = "planning work is incomplete"
	case StateWaitingDecision:
		intent.Action = RecoveryReconcileDecision
		intent.Reason = "human decision state must be reconciled"
	case StateRunning:
		intent.Action = RecoveryInspectRuntime
		intent.Reason = "runtime state must be inspected before resuming"
	case StateVerifying:
		intent.Action = RecoveryResumeVerification
		intent.Reason = "verification is incomplete"
	default:
		intent.Reason = "state has no recovery action"
	}
	return intent
}
