package store

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestStructuredTaskBudgetRoundTrips(t *testing.T) {
	ctx := context.Background()
	appStore, err := Open(ctx, filepath.Join(t.TempDir(), "structured-budget.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer appStore.Close()

	deadline := time.Now().UTC().Add(2 * time.Hour).UnixMilli()
	want := domain.Budget{
		TimeMinutes: 45, DeadlineAt: &deadline, MaxAttempts: 3, MaxTokens: 125000,
		MaxCostMinor: 2400, Currency: "EUR", MaxConcurrency: 2,
		Authority: []string{"source.read", "source.write", "pull_request.create"},
	}
	task, err := appStore.CreateTask(ctx, domain.CreateTaskInput{
		Title: "Bounded delivery", AcceptanceCriteria: "Verified delivery", Status: domain.TaskReady, Priority: "high", Budget: &want,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if !reflect.DeepEqual(task.Budget, want) {
		t.Fatalf("created budget = %#v, want %#v", task.Budget, want)
	}

	loaded, err := appStore.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if !reflect.DeepEqual(loaded.Budget, want) {
		t.Fatalf("loaded budget = %#v, want %#v", loaded.Budget, want)
	}
	if loaded.BudgetMinutes != want.TimeMinutes {
		t.Fatalf("compatibility budget_minutes = %d, want %d", loaded.BudgetMinutes, want.TimeMinutes)
	}
}

func TestPlanRevisionsAreImmutableAndChangeRequestsReturnToPlanning(t *testing.T) {
	ctx := context.Background()
	appStore, task, run := createPlanningRun(t, ctx, "plan-revisions.db")
	defer appStore.Close()

	expiresAt := time.Now().UTC().Add(time.Hour).UnixMilli()
	firstInput := domain.PlanRevisionInput{
		Plan: "Implement the endpoint, then verify it.", ContextDigest: "sha256:context-one",
		RiskClass: "medium", VerificationStrategy: "Run focused and conformance tests.",
		ExpectedSideEffects: []string{"write source", "open pull request"},
		RollbackApproach:    "Revert the pull request.",
	}
	first, decision, err := appStore.SubmitPlanRevision(
		ctx, testMutation("plan-revision-one"), run.ID, firstInput,
		domain.DecisionRequest{
			Required: true, ExpiresAt: &expiresAt, Risk: "medium",
			EvidenceSummary: "Focused tests must pass.", EstimatedCostMinor: 2400, Currency: "EUR",
			WaitingConsequence: "Delivery remains paused.", RejectingConsequence: "The plan returns for revision.",
		},
	)
	if err != nil {
		t.Fatalf("submit first plan: %v", err)
	}
	if first.Revision != 1 || decision == nil || decision.PlanRevisionID != first.ID {
		t.Fatalf("first plan/decision = %#v / %#v", first, decision)
	}

	answered, err := appStore.AnswerDecision(ctx, decision.ID, "Request changes")
	if err != nil {
		t.Fatalf("request changes: %v", err)
	}
	if answered.PlanRevisionID != first.ID {
		t.Fatalf("answered plan revision = %q, want %q", answered.PlanRevisionID, first.ID)
	}
	afterRequest, err := appStore.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run after change request: %v", err)
	}
	if afterRequest.Status != domain.RunPlanning {
		t.Fatalf("run status = %q, want planning", afterRequest.Status)
	}

	secondInput := firstInput
	secondInput.Plan = "Implement the endpoint behind a compatibility layer, then verify it."
	secondInput.ContextDigest = "sha256:context-two"
	second, _, err := appStore.SubmitPlanRevision(
		ctx, testMutation("plan-revision-two"), run.ID, secondInput, domain.DecisionRequest{},
	)
	if err != nil {
		t.Fatalf("submit second plan: %v", err)
	}
	if second.Revision != 2 {
		t.Fatalf("second revision = %d, want 2", second.Revision)
	}
	revisions, err := appStore.ListPlanRevisions(ctx, run.ID)
	if err != nil {
		t.Fatalf("list plan revisions: %v", err)
	}
	if len(revisions) != 2 || revisions[0].Plan != firstInput.Plan || revisions[1].Plan != secondInput.Plan {
		t.Fatalf("plan history = %#v", revisions)
	}
	if got, err := appStore.GetTask(ctx, task.ID); err != nil || got.Status != domain.TaskRunning {
		t.Fatalf("task after approval-free revision = %#v, err %v", got, err)
	}
}

func TestExpiredDecisionAnswerIsRejected(t *testing.T) {
	ctx := context.Background()
	appStore, _, run := createPlanningRun(t, ctx, "decision-expiry.db")
	defer appStore.Close()

	expiredAt := time.Now().UTC().Add(-time.Minute).UnixMilli()
	_, decision, err := appStore.SubmitPlanRevision(
		ctx, testMutation("expired-decision-plan"), run.ID,
		domain.PlanRevisionInput{Plan: "A bounded plan", ContextDigest: "sha256:ctx", RiskClass: "low", VerificationStrategy: "go test ./...", RollbackApproach: "revert"},
		domain.DecisionRequest{Required: true, ExpiresAt: &expiredAt},
	)
	if err != nil {
		t.Fatalf("submit plan: %v", err)
	}
	if _, err := appStore.AnswerDecision(ctx, decision.ID, "Approve plan"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("answer expired decision error = %v, want conflict", err)
	}
	loaded, err := appStore.GetDecision(ctx, decision.ID)
	if err != nil {
		t.Fatalf("get decision: %v", err)
	}
	if loaded.Status != "expired" || loaded.Answer != "" {
		t.Fatalf("expired decision = %#v", loaded)
	}
}

func TestCompletionRequiresCurrentStructuredEvidenceAndRendersReport(t *testing.T) {
	ctx := context.Background()
	appStore, task, run := createPlanningRun(t, ctx, "structured-evidence.db")
	defer appStore.Close()

	revision, _, err := appStore.SubmitPlanRevision(
		ctx, testMutation("evidence-plan"), run.ID,
		domain.PlanRevisionInput{
			Plan: "Implement and independently verify.", ContextDigest: "sha256:ctx",
			RiskClass: "high", VerificationStrategy: "Run unit and conformance tests.",
			ExpectedSideEffects: []string{"write source"}, RollbackApproach: "revert",
			SourceRevision: "git:abc123", PolicyVersion: "policy:7",
		}, domain.DecisionRequest{},
	)
	if err != nil {
		t.Fatalf("submit plan: %v", err)
	}
	if err := appStore.BeginVerification(ctx, testMutation("begin-evidence-verification"), run.ID); err != nil {
		t.Fatalf("begin verification: %v", err)
	}
	expiresAt := time.Now().UTC().Add(time.Hour).UnixMilli()
	evidence, err := appStore.RecordEvidence(ctx, domain.EvidenceInput{
		RunID: run.ID, PlanRevisionID: revision.ID, Type: "test",
		Producer: "agent:builder", Verifier: "agent:reviewer",
		SubjectDigest: "sha256:source-tree", SourceRevision: "git:abc123",
		Command: "go test ./...", Outcome: "passed",
		ProvenanceURI: "deck://commands/cmd-42", ProvenanceDigest: "sha256:command-output",
		ObservedAt: time.Now().UTC().UnixMilli(), ExpiresAt: &expiresAt,
		ArtifactURI: "artifact://reports/test.json", ArtifactDigest: "sha256:artifact",
		PolicyVersion: "policy:7",
	})
	if err != nil {
		t.Fatalf("record evidence: %v", err)
	}
	if evidence.AdmissionStatus != "current" {
		t.Fatalf("evidence status = %q, want current", evidence.AdmissionStatus)
	}
	if err := appStore.CompleteRunWithEvidence(
		ctx, testMutation("complete-structured-evidence"), run.ID, "Delivered safely", []string{evidence.ID},
	); err != nil {
		t.Fatalf("complete with evidence: %v", err)
	}
	detail, err := appStore.GetTaskDetail(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task detail: %v", err)
	}
	if len(detail.Evidence) != 1 || detail.Evidence[0].ID != evidence.ID {
		t.Fatalf("evidence detail = %#v", detail.Evidence)
	}
	if len(detail.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want generated report", len(detail.Artifacts))
	}
	report := detail.Artifacts[0].Content
	for _, required := range []string{"# Verification report", "agent:reviewer", "go test ./...", "sha256:artifact", "git:abc123"} {
		if !strings.Contains(report, required) {
			t.Fatalf("verification report missing %q:\n%s", required, report)
		}
	}
}

func TestExpiredOrSelfVerifiedEvidenceCannotCompleteRun(t *testing.T) {
	ctx := context.Background()
	appStore, _, run := createPlanningRun(t, ctx, "rejected-evidence.db")
	defer appStore.Close()
	revision, _, err := appStore.SubmitPlanRevision(
		ctx, testMutation("rejected-evidence-plan"), run.ID,
		domain.PlanRevisionInput{
			Plan: "Implement and verify.", ContextDigest: "sha256:ctx", RiskClass: "medium",
			VerificationStrategy: "test", RollbackApproach: "revert", SourceRevision: "git:def456",
		}, domain.DecisionRequest{},
	)
	if err != nil {
		t.Fatalf("submit plan: %v", err)
	}
	if err := appStore.BeginVerification(ctx, testMutation("begin-rejected-evidence"), run.ID); err != nil {
		t.Fatalf("begin verification: %v", err)
	}
	expiredAt := time.Now().UTC().Add(-time.Minute).UnixMilli()
	evidence, err := appStore.RecordEvidence(ctx, domain.EvidenceInput{
		RunID: run.ID, PlanRevisionID: revision.ID, Type: "test",
		Producer: "agent:same", Verifier: "agent:same", SubjectDigest: "sha256:tree",
		SourceRevision: "git:def456", Command: "go test ./...", Outcome: "passed",
		ProvenanceURI: "artifact://output", ProvenanceDigest: "sha256:output",
		ObservedAt: time.Now().UTC().Add(-time.Hour).UnixMilli(), ExpiresAt: &expiredAt,
		ArtifactURI: "artifact://report", ArtifactDigest: "sha256:report",
	})
	if err != nil {
		t.Fatalf("record stale evidence: %v", err)
	}
	if evidence.AdmissionStatus != "stale" {
		t.Fatalf("evidence status = %q, want stale", evidence.AdmissionStatus)
	}
	if err := appStore.CompleteRunWithEvidence(
		ctx, testMutation("reject-stale-evidence"), run.ID, "not done", []string{evidence.ID},
	); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("completion error = %v, want conflict", err)
	}
}

func TestChannelMessageReferencesCanonicalResourcesAndPromotesBoundedTask(t *testing.T) {
	ctx := context.Background()
	appStore, err := Open(ctx, filepath.Join(t.TempDir(), "channels.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer appStore.Close()
	existing, err := appStore.CreateTask(ctx, domain.CreateTaskInput{
		Title: "Investigate signal", Status: domain.TaskBacklog, Priority: "high", BudgetMinutes: 15,
	})
	if err != nil {
		t.Fatalf("create referenced task: %v", err)
	}
	channel, err := appStore.CreateChannel(ctx, existing.ProjectID, "delivery")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	message, err := appStore.PostChannelMessage(ctx, domain.ChannelMessageInput{
		ChannelID: channel.ID, AuthorType: "human", AuthorID: "operator:alice",
		Body:       "Turn this signal into one bounded diagnostic task.",
		References: domain.CanonicalReferences{TaskIDs: []string{existing.ID}},
	})
	if err != nil {
		t.Fatalf("post message: %v", err)
	}
	budget := domain.Budget{
		TimeMinutes: 30, MaxAttempts: 2, MaxTokens: 50000, MaxCostMinor: 1000,
		Currency: "EUR", MaxConcurrency: 1, Authority: []string{"source.read"},
	}
	promoted, err := appStore.PromoteChannelMessage(ctx, message.ID, domain.CreateTaskInput{
		Title: "Diagnose the reported signal", AcceptanceCriteria: "A source-bound diagnosis is attached.",
		Status: domain.TaskBacklog, Priority: "high", Budget: &budget,
	})
	if err != nil {
		t.Fatalf("promote message: %v", err)
	}
	if promoted.Version != 1 || promoted.Description != message.Body || !reflect.DeepEqual(promoted.Budget, budget) {
		t.Fatalf("promoted task = %#v", promoted)
	}
	page, err := appStore.PageChannelMessages(ctx, channel.ID, 20, "")
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	messages := page.Items
	if len(messages) != 1 || messages[0].PromotedTaskID == nil || *messages[0].PromotedTaskID != promoted.ID {
		t.Fatalf("messages after promotion = %#v", messages)
	}
	if got, err := appStore.GetTask(ctx, existing.ID); err != nil || got.Version != existing.Version {
		t.Fatalf("chat mutated referenced task: %#v, err %v", got, err)
	}
}

func TestProviderStatusReportsConnectivitySyncLeaseAndRejectedProjections(t *testing.T) {
	ctx := context.Background()
	appStore, task, run := createPlanningRun(t, ctx, "provider-status.db")
	defer appStore.Close()

	laggedAt := time.Now().UTC().Add(-3 * time.Minute).UnixMilli()
	if err := appStore.RecordProjectionAccepted(ctx, task.ProjectID, "marina", laggedAt); err != nil {
		t.Fatalf("record accepted projection: %v", err)
	}
	if err := appStore.RecordProjectionRejected(
		ctx, task.ProjectID, "marina", "stale fence token", []string{"artifact-store"},
	); err != nil {
		t.Fatalf("record rejected projection: %v", err)
	}
	status, err := appStore.GetProviderStatus(ctx, task.ProjectID)
	if err != nil {
		t.Fatalf("get provider status: %v", err)
	}
	if status.Connectivity != "connected" || status.SyncState != "rejected" || status.SyncLagMillis < int64(2*time.Minute/time.Millisecond) {
		t.Fatalf("provider status = %#v", status)
	}
	if status.LeaseState != "active" || status.LeaseExpiresAt == nil || *status.LeaseExpiresAt != *run.LeaseExpiresAt {
		t.Fatalf("lease status = %#v", status)
	}
	if status.RejectedProjectionCount != 1 || status.StaleProjectionCount != 1 || !reflect.DeepEqual(status.DegradedDependencies, []string{"artifact-store"}) {
		t.Fatalf("projection status = %#v", status)
	}
}

func TestTaskDetailShowsTruthfulDisplayStateAndContinuousTrail(t *testing.T) {
	ctx := context.Background()
	appStore, task, run := createPlanningRun(t, ctx, "continuous-trail.db")
	defer appStore.Close()
	revision, _, err := appStore.SubmitPlanRevision(
		ctx, testMutation("trail-plan"), run.ID,
		domain.PlanRevisionInput{
			Plan: "Make and observe the change.", ContextDigest: "sha256:ctx", RiskClass: "high",
			VerificationStrategy: "test", ExpectedSideEffects: []string{"deploy canary"},
			RollbackApproach: "roll back canary", SourceRevision: "git:trail", PolicyVersion: "policy:trail",
		}, domain.DecisionRequest{},
	)
	if err != nil {
		t.Fatalf("submit plan: %v", err)
	}
	if err := appStore.BeginVerification(ctx, testMutation("trail-verification"), run.ID); err != nil {
		t.Fatalf("begin verification: %v", err)
	}
	expiredAt := time.Now().UTC().Add(-time.Second).UnixMilli()
	if _, err := appStore.RecordEvidence(ctx, domain.EvidenceInput{
		RunID: run.ID, PlanRevisionID: revision.ID, Type: "observation",
		Producer: "agent:producer", Verifier: "agent:verifier", SubjectDigest: "sha256:subject",
		SourceRevision: "git:trail", Command: "observe canary", Outcome: "passed",
		ProvenanceURI: "signal://canary", ProvenanceDigest: "sha256:signal",
		ObservedAt: time.Now().UTC().Add(-time.Minute).UnixMilli(), ExpiresAt: &expiredAt,
		ArtifactURI: "artifact://observation", ArtifactDigest: "sha256:observation", PolicyVersion: "policy:trail",
	}); err != nil {
		t.Fatalf("record stale evidence: %v", err)
	}
	detail, err := appStore.GetTaskDetail(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task detail: %v", err)
	}
	if detail.DisplayState != "stale-evidence" {
		t.Fatalf("display state = %q, want stale-evidence", detail.DisplayState)
	}
	wantStages := map[string]bool{"change": false, "verification": false}
	for _, item := range detail.Trail {
		if _, ok := wantStages[item.Stage]; ok {
			wantStages[item.Stage] = true
		}
	}
	for stage, found := range wantStages {
		if !found {
			t.Fatalf("continuous trail missing %q: %#v", stage, detail.Trail)
		}
	}
}

func TestProjectExportIsDeterministicAndImportsWithoutArtifactContent(t *testing.T) {
	ctx := context.Background()
	appStore, task, run := createPlanningRun(t, ctx, "export-source.db")
	defer appStore.Close()
	_, _, err := appStore.SubmitPlanRevision(
		ctx, testMutation("export-plan"), run.ID,
		domain.PlanRevisionInput{
			Plan: "Create a portable record.", ContextDigest: "sha256:ctx", RiskClass: "low",
			VerificationStrategy: "test export", RollbackApproach: "discard import",
		}, domain.DecisionRequest{},
	)
	if err != nil {
		t.Fatalf("submit export plan: %v", err)
	}
	if _, err := appStore.CreateCommand(ctx, run.ID, domain.CommandPause, "capture state", "operator:test"); err != nil {
		t.Fatalf("create command: %v", err)
	}
	first, err := appStore.ExportProject(ctx, task.ProjectID)
	if err != nil {
		t.Fatalf("first export: %v", err)
	}
	second, err := appStore.ExportProject(ctx, task.ProjectID)
	if err != nil {
		t.Fatalf("second export: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("exports differ:\n%s\n---\n%s", first, second)
	}
	if !strings.Contains(string(first), `"schema_version": "deck-export/v1"`) {
		t.Fatalf("export missing schema version: %s", first)
	}
	if strings.Contains(string(first), `"content"`) {
		t.Fatalf("export contains artifact content: %s", first)
	}

	target, err := Open(ctx, filepath.Join(t.TempDir(), "export-target.db"))
	if err != nil {
		t.Fatalf("open import target: %v", err)
	}
	defer target.Close()
	if err := target.ImportProject(ctx, first); err != nil {
		t.Fatalf("import project: %v", err)
	}
	roundTrip, err := target.ExportProject(ctx, task.ProjectID)
	if err != nil {
		t.Fatalf("round-trip export: %v", err)
	}
	if !bytes.Equal(first, roundTrip) {
		t.Fatalf("round trip differs:\n%s\n---\n%s", first, roundTrip)
	}
}

func int64Ptr(value int64) *int64 { return &value }

func createPlanningRun(t *testing.T, ctx context.Context, databaseName string) (*Store, domain.Task, domain.Run) {
	t.Helper()
	appStore, err := Open(ctx, filepath.Join(t.TempDir(), databaseName))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	task, err := appStore.CreateTask(ctx, domain.CreateTaskInput{
		Title: "Plan the work", Status: domain.TaskReady, Priority: "high", BudgetMinutes: 20,
	})
	if err != nil {
		appStore.Close()
		t.Fatalf("create task: %v", err)
	}
	if _, err := appStore.RegisterAgent(ctx, domain.Agent{ID: "agt_ledger", Name: "Ledger agent"}); err != nil {
		appStore.Close()
		t.Fatalf("register agent: %v", err)
	}
	run, err := appStore.ClaimTask(ctx, testMutation("claim-"+databaseName), task.ID, "agt_ledger", time.Hour)
	if err != nil {
		appStore.Close()
		t.Fatalf("claim task: %v", err)
	}
	return appStore, task, run
}
