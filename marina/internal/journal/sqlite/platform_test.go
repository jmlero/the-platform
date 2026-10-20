package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func platformStore(t *testing.T) (*sqlite.Platform, string, *time.Time) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, time.October, 17, 18, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "marina.db")
	store := openStore(t, ctx, path, &now)
	return store.Platform(), path, &now
}

func submitRun(t *testing.T, p *sqlite.Platform, name string, dependsOn ...string) sqlite.PlatformWork {
	t.Helper()
	w, created, err := p.SubmitWork(context.Background(), sqlite.NewPlatformWork{Namespace: "factory", Name: name, UID: "uid-" + name, ProjectID: "factory.acceptance",
		Item: workcontrol.WorkItem{Title: "Reject an empty range", AcceptanceCriteria: "A test covers min > max", BudgetMinutes: 30,
			Budget: workcontrol.Budget{TimeMinutes: 30, MaxAttempts: 7, MaxTokens: 1000}}, DependsOn: dependsOn})
	if err != nil || !created {
		t.Fatalf("submit %s: created=%v err=%v", name, created, err)
	}
	return w
}

func claim(t *testing.T, p *sqlite.Platform, workID, key string) workcontrol.Run {
	t.Helper()
	run, err := p.ClaimWork(context.Background(), workID, workcontrol.Claim{IdempotencyKey: key, AgentID: "marina", ExpectedTaskVersion: 1, Lease: time.Minute})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	return run
}

func mutationFor(run workcontrol.Run, key string) workcontrol.Mutation {
	return workcontrol.Mutation{IdempotencyKey: key, AgentID: run.AgentID, ExpectedVersion: run.Version, FenceToken: run.FenceToken}
}

func current(t *testing.T, p *sqlite.Platform, workID string) workcontrol.Run {
	t.Helper()
	detail, err := p.GetWork(context.Background(), workID)
	if err != nil || len(detail.Runs) == 0 {
		t.Fatalf("work %s: %v", workID, err)
	}
	return detail.Runs[len(detail.Runs)-1]
}

func isConflict(err error) bool {
	var conflict interface{ MutationConflict() bool }
	return errors.As(err, &conflict) && conflict.MutationConflict() && errors.Is(err, execution.ErrConflict)
}

func TestPlatformRunIsSubmittedOnceAndAChangedSpecConflicts(t *testing.T) {
	p, _, _ := platformStore(t)
	ctx := context.Background()
	first := submitRun(t, p, "task-1")
	again, created, err := p.SubmitWork(ctx, sqlite.NewPlatformWork{Namespace: "factory", Name: "task-1", UID: "uid-task-1", ProjectID: "factory.acceptance",
		Item: workcontrol.WorkItem{Title: "Reject an empty range", AcceptanceCriteria: "A test covers min > max", BudgetMinutes: 30,
			Budget: workcontrol.Budget{TimeMinutes: 30, MaxAttempts: 7, MaxTokens: 1000}}})
	if err != nil || created || again.ID != first.ID || again.SpecDigest != first.SpecDigest {
		t.Fatalf("replay: created=%v err=%v", created, err)
	}
	_, _, err = p.SubmitWork(ctx, sqlite.NewPlatformWork{Namespace: "factory", Name: "task-1", UID: "uid-task-1", ProjectID: "factory.acceptance",
		Item: workcontrol.WorkItem{Title: "Something else", AcceptanceCriteria: "x"}})
	if !isConflict(err) {
		t.Fatalf("changed spec: %v", err)
	}
	if first.ID != "factory.task-1" || first.Item.ID != first.ID || first.Item.Status != "ready" || first.Item.Version != 1 {
		t.Fatalf("work item = %+v", first.Item)
	}
}

func TestReadyWorkWaitsForDependenciesAndClaimsReplay(t *testing.T) {
	p, _, _ := platformStore(t)
	ctx := context.Background()
	first := submitRun(t, p, "task-1")
	submitRun(t, p, "task-2", "task-1")
	ready, err := p.ListReadyWork(ctx)
	if err != nil || len(ready) != 1 || ready[0].ID != first.ID {
		t.Fatalf("ready = %+v err=%v", ready, err)
	}
	run := claim(t, p, first.ID, "claim-1")
	replayed := claim(t, p, first.ID, "claim-1")
	if replayed.ID != run.ID || run.Status != workcontrol.RunPlanning || run.FenceToken != 1 {
		t.Fatalf("claim replay: %+v vs %+v", replayed, run)
	}
	if _, err := p.ClaimWork(ctx, first.ID, workcontrol.Claim{IdempotencyKey: "claim-2", AgentID: "other", ExpectedTaskVersion: 1}); !isConflict(err) {
		t.Fatalf("second claim: %v", err)
	}
	if _, err := p.ClaimWork(ctx, first.ID, workcontrol.Claim{IdempotencyKey: "claim-1", AgentID: "other", ExpectedTaskVersion: 1}); !isConflict(err) {
		t.Fatalf("reused key with a different request: %v", err)
	}
	if err := p.RenewRunLease(ctx, run.ID, workcontrol.LeaseRenewal{IdempotencyKey: "lease-1", AgentID: "marina", ExpectedVersion: run.Version, FenceToken: run.FenceToken, Lease: time.Minute}); err != nil {
		t.Fatal(err)
	}
	looked, err := p.LookupClaim(ctx, "factory.acceptance", "claim-1")
	if err != nil || looked.Version != run.Version+1 {
		t.Fatalf("lookup returns the current run: %+v %v", looked, err)
	}
	if err := p.RenewRunLease(ctx, run.ID, workcontrol.LeaseRenewal{IdempotencyKey: "lease-2", AgentID: "marina", ExpectedVersion: run.Version, FenceToken: run.FenceToken}); !isConflict(err) {
		t.Fatalf("stale version renewal: %v", err)
	}
	page, err := p.ListAssignedRuns(ctx, "factory.acceptance", "marina", "", 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].Work.ID != first.ID {
		t.Fatalf("assigned = %+v %v", page, err)
	}
}

func approvedRun(t *testing.T, p *sqlite.Platform, name string) (workcontrol.Run, workcontrol.PlanRevision) {
	t.Helper()
	ctx := context.Background()
	w := submitRun(t, p, name)
	run := claim(t, p, w.ID, "claim-"+name)
	result, err := p.SubmitPlanRevision(ctx, run.ID, workcontrol.PlanRevisionSubmission{IdempotencyKey: "plan-" + name, AgentID: "marina", ExpectedVersion: run.Version, FenceToken: run.FenceToken,
		Plan: "Add a guard", ContextDigest: "sha256:c", SourceRevision: strings.Repeat("a", 40), RequireApproval: true})
	if err != nil || result.Decision == nil {
		t.Fatalf("plan: %+v %v", result, err)
	}
	run = current(t, p, w.ID)
	if run.Status != workcontrol.RunWaitingDecision {
		t.Fatalf("run status = %s", run.Status)
	}
	if err := p.StartRun(ctx, run.ID, mutationFor(run, "start-early-"+name)); !isConflict(err) {
		t.Fatalf("start before approval: %v", err)
	}
	decision, err := p.AnswerDecision(ctx, "factory", name+"-plan-1", "approve plan", "system:serviceaccount:marina-system:deck")
	if err != nil || decision.Decision.Answer != "Approve plan" || decision.Decision.AnsweredBy == "" {
		t.Fatalf("answer: %+v %v", decision, err)
	}
	if again, err := p.AnswerDecision(ctx, "factory", name+"-plan-1", "Approve plan", "someone"); err != nil || again.Decision.AnsweredBy != decision.Decision.AnsweredBy {
		t.Fatalf("exact replay: %+v %v", again, err)
	}
	if _, err := p.AnswerDecision(ctx, "factory", name+"-plan-1", "Request changes", "someone"); !isConflict(err) {
		t.Fatalf("different answer: %v", err)
	}
	if err := p.StartRun(ctx, run.ID, mutationFor(run, "start-"+name)); err != nil {
		t.Fatal(err)
	}
	return current(t, p, w.ID), result.PlanRevision
}

func TestPlanApprovalGatesStartAndChangesReturnToPlanning(t *testing.T) {
	p, _, _ := platformStore(t)
	ctx := context.Background()
	run, _ := approvedRun(t, p, "task-1")
	if run.Status != workcontrol.RunRunning {
		t.Fatalf("status = %s", run.Status)
	}
	w := submitRun(t, p, "task-2")
	second := claim(t, p, w.ID, "claim-2")
	if _, err := p.SubmitPlanRevision(ctx, second.ID, workcontrol.PlanRevisionSubmission{IdempotencyKey: "plan-2", AgentID: "marina", ExpectedVersion: second.Version, FenceToken: second.FenceToken,
		Plan: "Plan", ContextDigest: "sha256:c", RequireApproval: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AnswerDecision(ctx, "factory", "task-2-plan-1", "request changes", "deck"); err != nil {
		t.Fatal(err)
	}
	if status := current(t, p, w.ID).Status; status != workcontrol.RunPlanning {
		t.Fatalf("after request changes: %s", status)
	}
	if _, err := p.AnswerDecision(ctx, "factory", "task-2-plan-1", "maybe", "deck"); err == nil {
		t.Fatal("answered decision accepted another answer")
	}
}

func evidence(revision workcontrol.PlanRevision, producer, verifier, outcome string, at time.Time) workcontrol.EvidenceSubmission {
	return workcontrol.EvidenceSubmission{PlanRevisionID: revision.ID, Type: "verification", Producer: producer, Verifier: verifier, SubjectDigest: "sha256:s",
		SourceRevision: revision.SourceRevision, Command: "node --test", Outcome: outcome, ObservedAt: at, ArtifactDigest: "sha256:a", PolicyVersion: "v1"}
}

func TestCompletionNeedsPassedIndependentEvidenceOnTheActivePlan(t *testing.T) {
	p, _, now := platformStore(t)
	ctx := context.Background()
	run, revision := approvedRun(t, p, "task-1")
	if err := p.BeginVerification(ctx, run.ID, mutationFor(run, "verify")); err != nil {
		t.Fatal(err)
	}
	run = current(t, p, run.WorkItemID)
	self, err := p.RecordEvidence(ctx, run.ID, mutationFor(run, "ev-self"), evidence(revision, "marina-worker", "marina-worker", "passed", *now))
	if err != nil {
		t.Fatal(err)
	}
	failed, err := p.RecordEvidence(ctx, run.ID, mutationFor(run, "ev-failed"), evidence(revision, "codex", "verifier", "failed", *now))
	if err != nil {
		t.Fatal(err)
	}
	good, err := p.RecordEvidence(ctx, run.ID, mutationFor(run, "ev-good"), evidence(revision, "codex", "verifier", "passed", *now))
	if err != nil {
		t.Fatal(err)
	}
	for name, ids := range map[string][]string{"self-verified": {self}, "failed": {failed}, "unknown": {"pevd_missing"}} {
		if err := p.CompleteRun(ctx, run.ID, mutationFor(run, "complete-"+name), "done", ids); !isConflict(err) {
			t.Fatalf("%s evidence completed the run: %v", name, err)
		}
	}
	landing := workcontrol.LandingObservation{Repository: "owner/repo", BaseBranch: "main", Commit: strings.Repeat("b", 40), ObservedAt: now.UnixMilli()}
	if err := p.CompleteLandedRun(ctx, run.ID, mutationFor(run, "complete"), "Landed", []string{good}, landing); err != nil {
		t.Fatal(err)
	}
	if err := p.CompleteLandedRun(ctx, run.ID, mutationFor(run, "complete"), "Landed", []string{good}, landing); err != nil {
		t.Fatalf("exact completion replay: %v", err)
	}
	w, _ := p.Work(ctx, run.WorkItemID)
	if w.Status != "completed" || current(t, p, run.WorkItemID).Status != workcontrol.RunCompleted {
		t.Fatalf("work status = %s", w.Status)
	}
}

func TestCancelEndsReadyWorkAtOnceAndActiveWorkThroughTheWorkflow(t *testing.T) {
	p, _, _ := platformStore(t)
	ctx := context.Background()
	ready := submitRun(t, p, "task-1")
	if err := p.RequestCancel(ctx, ready.ID, "deck", "not needed"); err != nil {
		t.Fatal(err)
	}
	if w, _ := p.Work(ctx, ready.ID); w.Status != "cancelled" {
		t.Fatalf("ready work = %s", w.Status)
	}
	if items, _ := p.ListReadyWork(ctx); len(items) != 0 {
		t.Fatalf("cancelled work is still ready: %+v", items)
	}
	// A client following the events sees the Run end.
	events, err := p.ListEvents(ctx, ready.ID, 0, 10)
	if err != nil || len(events) == 0 || events[len(events)-1].Type != "run.cancelled" {
		t.Fatalf("events of the ready work = %+v %v", events, err)
	}
	run, _ := approvedRun(t, p, "task-2")
	if err := p.RequestCancel(ctx, run.WorkItemID, "deck", "stop"); err != nil {
		t.Fatal(err)
	}
	commands, err := p.ListRunCommands(ctx, run.ID, 0, 10)
	if err != nil || len(commands) != 1 || commands[0].Type != "cancel" {
		t.Fatalf("commands = %+v %v", commands, err)
	}
	if status := current(t, p, run.WorkItemID).Status; status != workcontrol.RunRunning {
		t.Fatalf("cancel must wait for the workflow: %s", status)
	}
	if err := p.AcknowledgeRunCommand(ctx, run.ID, commands[0].ID, workcontrol.CommandAcknowledgement{IdempotencyKey: "ack", AgentID: "marina", FenceToken: run.FenceToken, Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	if status := current(t, p, run.WorkItemID).Status; status != workcontrol.RunCancelled {
		t.Fatalf("after acknowledgement: %s", status)
	}
}

func TestRunEventsAreOrderedDeduplicatedAndSurviveRestart(t *testing.T) {
	p, path, now := platformStore(t)
	ctx := context.Background()
	run, _ := approvedRun(t, p, "task-1")
	event := workcontrol.ProjectedEvent{IdempotencyKey: "out-1", AgentID: "marina", FenceToken: run.FenceToken, Producer: "marina", ProducerEventID: "e1",
		Type: "execution.progress", Message: "Implementing", Payload: map[string]any{"step": "implement", "tokens": 120, "prompt": map[string]any{"text": "secret"}}}
	for i := 0; i < 2; i++ {
		if err := p.AppendRunEvent(ctx, run.ID, event); err != nil {
			t.Fatal(err)
		}
	}
	event.IdempotencyKey = "out-1-replayed-later"
	if err := p.AppendRunEvent(ctx, run.ID, event); err != nil {
		t.Fatal(err)
	}
	reopened := openStore(t, ctx, path, now).Platform()
	events, err := reopened.ListEvents(ctx, run.WorkItemID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	progress := 0
	for i, e := range events {
		if e.Sequence != int64(i+1) {
			t.Fatalf("sequence %d at %d", e.Sequence, i)
		}
		if e.Type == "execution.progress" {
			progress++
			if _, nested := e.Fields["prompt"]; nested || e.Fields["step"] != "implement" {
				t.Fatalf("event fields = %v", e.Fields)
			}
		}
	}
	if progress != 1 || events[0].Type != "run.accepted" {
		t.Fatalf("events = %+v", events)
	}
	if later, _ := reopened.ListEvents(ctx, run.WorkItemID, events[len(events)-2].Sequence, 100); len(later) != 1 {
		t.Fatalf("after cursor = %+v", later)
	}
}
