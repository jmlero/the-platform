package execution

import (
	"testing"
	"time"
)

func TestPlanRecoveryPrioritizesSafetyAndResumesByState(t *testing.T) {
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	base := Execution{
		ID:             "exec-1",
		State:          StatePlanning,
		Attempt:        1,
		LeaseExpiresAt: now.Add(time.Hour),
		DeadlineAt:     now.Add(2 * time.Hour),
		MaxAttempts:    3,
		MaxTokens:      1000,
	}

	tests := []struct {
		name        string
		mutate      func(*Execution)
		renewBefore time.Duration
		want        RecoveryAction
	}{
		{
			name: "terminal execution has no action",
			mutate: func(record *Execution) {
				record.State = StateCompleted
			},
			want: RecoveryNone,
		},
		{
			name: "budget takes priority",
			mutate: func(record *Execution) {
				record.DeadlineAt = now
				record.LeaseExpiresAt = now
			},
			want: RecoveryStopBudget,
		},
		{
			name: "expired lease is reconciled",
			mutate: func(record *Execution) {
				record.LeaseExpiresAt = now
			},
			want: RecoveryReconcileLease,
		},
		{
			name: "near lease is renewed before phase work",
			mutate: func(record *Execution) {
				record.LeaseExpiresAt = now.Add(5 * time.Minute)
			},
			renewBefore: 10 * time.Minute,
			want:        RecoveryRenewLease,
		},
		{
			name: "durable retry backoff defers phase work",
			mutate: func(record *Execution) {
				next := now.Add(time.Minute)
				record.NextEligibleAt = &next
				record.RetryAttempt = 1
			},
			want: RecoveryNone,
		},
		{
			name: "lease safety overrides retry backoff",
			mutate: func(record *Execution) {
				next := now.Add(time.Hour)
				record.NextEligibleAt = &next
				record.RetryAttempt = 1
				record.LeaseExpiresAt = now.Add(time.Minute)
			},
			renewBefore: 2 * time.Minute,
			want:        RecoveryRenewLease,
		},
		{name: "planning resumes", want: RecoveryResumePlanning},
		{
			name: "decision is reconciled",
			mutate: func(record *Execution) {
				record.State = StateWaitingDecision
			},
			want: RecoveryReconcileDecision,
		},
		{
			name: "runtime is inspected",
			mutate: func(record *Execution) {
				record.State = StateRunning
			},
			want: RecoveryInspectRuntime,
		},
		{
			name: "verification resumes",
			mutate: func(record *Execution) {
				record.State = StateVerifying
			},
			want: RecoveryResumeVerification,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := base
			if test.mutate != nil {
				test.mutate(&record)
			}
			intent := PlanRecovery(record, now, test.renewBefore)
			if intent.Action != test.want {
				t.Fatalf("action = %q, want %q (reason: %s)", intent.Action, test.want, intent.Reason)
			}
			if intent.ExecutionID != record.ID {
				t.Fatalf("execution ID = %q, want %q", intent.ExecutionID, record.ID)
			}
			if intent.Reason == "" {
				t.Fatal("recovery reason is empty")
			}
		})
	}
}
