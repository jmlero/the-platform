package planning_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/planning"
	"github.com/jmlero/the-platform/marina/internal/policy"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func TestCoordinatorPersistsPlanIntentAndReconcilesDecision(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "planning.db"), sqlite.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	record, _, err := store.EnsureExecution(ctx, execution.NewExecution{
		WorkProvider: "deck", ProjectID: "project-1", ExternalWorkID: "task-1", ExternalRunID: "run-1",
		AgentID: "marina-1", ExternalRunVersion: 7, FenceToken: 3, TaskVersion: 2,
		TaskDigest: "sha256:task", BindingVersion: 1, LeaseExpiresAt: now.Add(time.Hour),
		Budget: execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 3, MaxTokens: 1000, MaxCostMicros: 100000},
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := &providerStub{}
	coordinator := planning.New(store, provider, func() time.Time { return now })
	policySnapshot, policyRequest := allowedPolicy(now)

	result, err := coordinator.Submit(ctx, planning.SubmitInput{
		ExecutionID: record.ID, RunID: "run-1", Policy: policySnapshot, PolicyRequest: policyRequest,
		Plan: workcontrol.PlanRevisionSubmission{
			IdempotencyKey: "plan-key-1", AgentID: "marina-1", ExpectedVersion: 7, FenceToken: 3,
			Plan: "bounded plan", ContextDigest: "sha256:" + strings.Repeat("a", 64), RiskClass: "high",
			VerificationStrategy: "go test ./...", ExpectedSideEffects: []string{"source files"},
			RollbackApproach: "revert", SourceRevision: strings.Repeat("b", 40), PolicyVersion: "policy-1",
			RequireApproval: true,
		},
	})
	if err != nil {
		t.Fatalf("submit plan: %v", err)
	}
	if result.Decision == nil || provider.submissions != 1 {
		t.Fatalf("result = %#v, submissions = %d", result, provider.submissions)
	}
	checkpoint, err := store.LatestCheckpoint(ctx, record.ID)
	if err != nil || checkpoint.Phase != "plan_submitted" || checkpoint.Payload["decision_id"] != "decision-1" {
		t.Fatalf("latest checkpoint = %#v, err %v", checkpoint, err)
	}

	provider.decision.Status = workcontrol.DecisionAnswered
	provider.decision.Answer = "Approve plan"
	action, err := coordinator.ReconcileDecision(ctx, record.ID, "decision-1", "plan-1")
	if err != nil {
		t.Fatalf("reconcile decision: %v", err)
	}
	if action != planning.ActionStartRuntime {
		t.Fatalf("action = %q", action)
	}
	checkpoint, err = store.LatestCheckpoint(ctx, record.ID)
	if err != nil || checkpoint.Phase != "decision_answered" || checkpoint.Payload["answer"] != "Approve plan" {
		t.Fatalf("decision checkpoint = %#v, err %v", checkpoint, err)
	}
}

func TestCoordinatorDeniesMutationBeforeProviderCall(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(ctx, ":memory:", sqlite.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	record, _, err := store.EnsureExecution(ctx, execution.NewExecution{
		WorkProvider: "deck", ProjectID: "project-1", ExternalWorkID: "task-1", ExternalRunID: "run-1",
		AgentID: "marina-1", ExternalRunVersion: 1, FenceToken: 1, TaskVersion: 1, TaskDigest: "digest",
		BindingVersion: 1, LeaseExpiresAt: now.Add(time.Hour),
		Budget: execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 1, MaxTokens: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := &providerStub{}
	coordinator := planning.New(store, provider, func() time.Time { return now })
	policySnapshot, request := allowedPolicy(now)
	request.PolicyVersion = "different-version"
	_, err = coordinator.Submit(ctx, planning.SubmitInput{
		ExecutionID: record.ID, RunID: "run-1", Policy: policySnapshot, PolicyRequest: request,
		Plan: workcontrol.PlanRevisionSubmission{IdempotencyKey: "denied"},
	})
	if err == nil || provider.submissions != 0 {
		t.Fatalf("err = %v, submissions = %d", err, provider.submissions)
	}
}

func TestDecisionActionsAreExplicit(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Second)
	tests := []struct {
		name     string
		decision workcontrol.Decision
		want     planning.Action
	}{
		{name: "pending", decision: workcontrol.Decision{Status: workcontrol.DecisionPending}, want: planning.ActionWait},
		{name: "locally expired", decision: workcontrol.Decision{Status: workcontrol.DecisionPending, ExpiresAt: &expired}, want: planning.ActionReplan},
		{name: "changes", decision: workcontrol.Decision{Status: workcontrol.DecisionAnswered, Answer: "Request changes"}, want: planning.ActionReplan},
		{name: "cancelled", decision: workcontrol.Decision{Status: workcontrol.DecisionCancelled}, want: planning.ActionStop},
		{name: "superseded", decision: workcontrol.Decision{Status: workcontrol.DecisionSuperseded}, want: planning.ActionReplan},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := planning.Decide(now, test.decision); got != test.want {
				t.Fatalf("Decide() = %q, want %q", got, test.want)
			}
		})
	}
}

type providerStub struct {
	submissions int
	decision    workcontrol.Decision
}

func (provider *providerStub) SubmitPlanRevision(_ context.Context, runID string, submission workcontrol.PlanRevisionSubmission) (workcontrol.PlanRevisionResult, error) {
	provider.submissions++
	provider.decision = workcontrol.Decision{
		ID: "decision-1", TaskID: "task-1", RunID: runID, PlanRevisionID: "plan-1",
		Status: workcontrol.DecisionPending, ExpiresAt: submission.DecisionExpiresAt,
	}
	return workcontrol.PlanRevisionResult{
		PlanRevision: workcontrol.PlanRevision{
			ID: "plan-1", RunID: runID, Revision: 1, Plan: strings.TrimSpace(submission.Plan),
			ContextDigest: submission.ContextDigest, RiskClass: submission.RiskClass,
			SourceRevision: submission.SourceRevision, PolicyVersion: submission.PolicyVersion,
		},
		Decision: &provider.decision,
	}, nil
}

func (provider *providerStub) GetDecision(context.Context, string) (workcontrol.Decision, error) {
	return provider.decision, nil
}

func allowedPolicy(now time.Time) (policy.Policy, policy.Request) {
	snapshot := policy.Policy{
		ProjectID: "project-1", Version: "policy-1", ChangeClass: "code", Environment: "development", RiskClass: policy.RiskHigh,
		AllowedActions: []policy.Action{policy.ActionExternalMutation}, AllowedAuthority: []string{"source.read"},
		Limits: policy.Limits{MaxAttempts: 3, MaxTokens: 1000, MaxCostMicros: 100000, MaxConcurrency: 1,
			MaxCredentialLifetime: 30 * time.Minute, MaxBlastRadius: policy.BlastRepository},
	}
	request := policy.Request{
		ProjectID: "project-1", PolicyVersion: "policy-1", ChangeClass: "code", Environment: "development",
		RiskClass: policy.RiskHigh, Action: policy.ActionExternalMutation, TaskAuthority: []string{"source.read"},
		RequestedAuthority: []string{"source.read"}, Budget: policy.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 3,
			MaxTokens: 1000, MaxCostMicros: 100000, MaxConcurrency: 1, CredentialExpiresAt: now.Add(20 * time.Minute), BlastRadius: policy.BlastRepository},
	}
	return snapshot, request
}
