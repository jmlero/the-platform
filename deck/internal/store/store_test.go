package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestHumanAgentLifecycle(t *testing.T) {
	ctx := context.Background()
	appStore, err := Open(ctx, filepath.Join(t.TempDir(), "deck-test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer appStore.Close()

	task, err := appStore.CreateTask(ctx, domain.CreateTaskInput{
		Title: "Test the lifecycle", Status: domain.TaskReady, Priority: "high", BudgetMinutes: 10,
		AcceptanceCriteria: "The task finishes with evidence.",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	agent, err := appStore.RegisterAgent(ctx, domain.Agent{
		ID: "agt_test", Name: "Test agent", Kind: "test", Capabilities: []string{"testing"},
	})
	if err != nil {
		t.Fatalf("register agent: %v", err)
	}
	run, err := appStore.ClaimTask(ctx, testMutation("claim-lifecycle"), task.ID, agent.ID, time.Minute)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if _, err := appStore.ClaimTask(ctx, testMutation("claim-duplicate"), task.ID, agent.ID, time.Minute); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate claim error = %v, want conflict", err)
	}

	decision, err := appStore.SubmitPlan(
		ctx, testMutation("plan-lifecycle"), run.ID, "Implement, test, and report.", true,
	)
	if err != nil {
		t.Fatalf("submit plan: %v", err)
	}
	if decision == nil || decision.Status != "pending" {
		t.Fatalf("decision = %#v, want pending decision", decision)
	}
	if err := appStore.StartRun(ctx, testMutation("start-lifecycle"), run.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("start before approval error = %v, want conflict", err)
	}
	if _, err := appStore.AnswerDecision(ctx, decision.ID, "Approve plan"); err != nil {
		t.Fatalf("answer decision: %v", err)
	}
	if err := appStore.StartRun(ctx, testMutation("start-lifecycle"), run.ID); err != nil {
		t.Fatalf("start run: %v", err)
	}
	if err := appStore.BeginVerification(ctx, testMutation("verify-lifecycle"), run.ID); err != nil {
		t.Fatalf("begin verification: %v", err)
	}
	evidenceID := recordPassingEvidence(t, ctx, appStore, run.ID, "lifecycle")
	if err := appStore.CompleteRunWithEvidence(
		ctx, testMutation("complete-lifecycle"), run.ID, "All checks passed", []string{evidenceID},
	); err != nil {
		t.Fatalf("complete run: %v", err)
	}

	detail, err := appStore.GetTaskDetail(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task detail: %v", err)
	}
	if detail.Task.Status != domain.TaskDone {
		t.Fatalf("task status = %q, want done", detail.Task.Status)
	}
	if len(detail.Runs) != 1 || detail.Runs[0].Status != domain.RunCompleted {
		t.Fatalf("runs = %#v, want one completed run", detail.Runs)
	}
	if len(detail.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(detail.Artifacts))
	}
	if len(detail.Events) < 6 {
		t.Fatalf("events = %d, want at least 6", len(detail.Events))
	}
}

func TestManualMoveRejectsActiveTask(t *testing.T) {
	ctx := context.Background()
	appStore, err := Open(ctx, filepath.Join(t.TempDir(), "deck-test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer appStore.Close()
	task, err := appStore.CreateTask(ctx, domain.CreateTaskInput{
		Title: "Move me", Status: domain.TaskReady, Priority: "medium", BudgetMinutes: 15,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := appStore.RegisterAgent(ctx, domain.Agent{ID: "agt_test", Name: "Test"}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	if _, err := appStore.ClaimTask(ctx, testMutation("claim-manual-move"), task.ID, "agt_test", time.Minute); err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if _, err := appStore.MoveTask(ctx, task.ID, domain.TaskBacklog); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("move active task error = %v, want invalid transition", err)
	}
}

func TestClaimIdempotencySurvivesRestartAndRejectsDifferentPayload(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "deck-idempotency-restart.db")
	appStore, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	task, err := appStore.CreateTask(ctx, domain.CreateTaskInput{
		Title: "Replay a lost claim response", Status: domain.TaskReady, Priority: "high", BudgetMinutes: 10,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := appStore.RegisterAgent(ctx, domain.Agent{ID: "agt_replay", Name: "Replay agent"}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	mutation := testMutation("claim-restart-proof")
	original, err := appStore.ClaimTask(ctx, mutation, task.ID, "agt_replay", time.Minute)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if err := appStore.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	appStore, err = Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer appStore.Close()
	replayed, err := appStore.ClaimTask(ctx, mutation, task.ID, "agt_replay", time.Minute)
	if err != nil {
		t.Fatalf("replay claim after restart: %v", err)
	}
	if !reflect.DeepEqual(replayed, original) {
		t.Fatalf("replayed run = %#v, want original %#v", replayed, original)
	}
	if _, err := appStore.ClaimTask(ctx, mutation, task.ID, "agt_replay", 2*time.Minute); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed claim payload error = %v, want conflict", err)
	}
	detail, err := appStore.GetTaskDetail(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task detail: %v", err)
	}
	if len(detail.Runs) != 1 {
		t.Fatalf("runs after claim replay = %d, want 1", len(detail.Runs))
	}
	claimedEvents, err := appStore.CountRunEvents(ctx, original.ID, "run.claimed")
	if err != nil {
		t.Fatalf("count claim events: %v", err)
	}
	if claimedEvents != 1 {
		t.Fatalf("claim events after replay = %d, want 1", claimedEvents)
	}
}

func TestConcurrentClaimRetriesConvergeOnOneResult(t *testing.T) {
	ctx := context.Background()
	appStore, err := Open(ctx, filepath.Join(t.TempDir(), "deck-idempotency-concurrent.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer appStore.Close()
	task, err := appStore.CreateTask(ctx, domain.CreateTaskInput{
		Title: "Converge concurrent retries", Status: domain.TaskReady, Priority: "high", BudgetMinutes: 10,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := appStore.RegisterAgent(ctx, domain.Agent{ID: "agt_concurrent", Name: "Concurrent agent"}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	type outcome struct {
		run domain.Run
		err error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			run, err := appStore.ClaimTask(
				ctx, testMutation("claim-concurrent"), task.ID, "agt_concurrent", time.Minute,
			)
			results <- outcome{run: run, err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	var runs []domain.Run
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent claim: %v", result.err)
		}
		runs = append(runs, result.run)
	}
	if len(runs) != 2 || !reflect.DeepEqual(runs[0], runs[1]) {
		t.Fatalf("concurrent results = %#v, want two identical runs", runs)
	}
	detail, err := appStore.GetTaskDetail(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task detail: %v", err)
	}
	if len(detail.Runs) != 1 {
		t.Fatalf("concurrent claims created %d runs, want 1", len(detail.Runs))
	}
}

func TestAllRunMutationsReplayWithoutDuplicateEffects(t *testing.T) {
	ctx := context.Background()
	appStore, err := Open(ctx, filepath.Join(t.TempDir(), "deck-idempotency-lifecycle.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer appStore.Close()
	if _, err := appStore.RegisterAgent(ctx, domain.Agent{ID: "agt_idempotent", Name: "Idempotent agent"}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	task, err := appStore.CreateTask(ctx, domain.CreateTaskInput{
		Title: "Complete idempotently", Status: domain.TaskReady, Priority: "high", BudgetMinutes: 10,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	run, err := appStore.ClaimTask(ctx, testMutation("claim-complete"), task.ID, "agt_idempotent", time.Minute)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}

	planMutation := testMutation("plan-complete")
	decision, err := appStore.SubmitPlan(ctx, planMutation, run.ID, "Implement and verify.", true)
	if err != nil {
		t.Fatalf("submit plan: %v", err)
	}
	replayedDecision, err := appStore.SubmitPlan(ctx, planMutation, run.ID, "Implement and verify.", true)
	if err != nil {
		t.Fatalf("replay plan: %v", err)
	}
	if !reflect.DeepEqual(replayedDecision, decision) {
		t.Fatalf("replayed decision = %#v, want %#v", replayedDecision, decision)
	}
	if _, err := appStore.SubmitPlan(ctx, planMutation, run.ID, "A different plan.", true); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed plan payload error = %v, want conflict", err)
	}
	if _, err := appStore.AnswerDecision(ctx, decision.ID, "Approve plan"); err != nil {
		t.Fatalf("answer decision: %v", err)
	}

	startMutation := testMutation("start-complete")
	if err := appStore.StartRun(ctx, startMutation, run.ID); err != nil {
		t.Fatalf("start run: %v", err)
	}
	if err := appStore.StartRun(ctx, startMutation, run.ID); err != nil {
		t.Fatalf("replay start: %v", err)
	}
	verifyMutation := testMutation("verify-complete")
	if err := appStore.BeginVerification(ctx, verifyMutation, run.ID); err != nil {
		t.Fatalf("begin verification: %v", err)
	}
	if err := appStore.BeginVerification(ctx, verifyMutation, run.ID); err != nil {
		t.Fatalf("replay verification: %v", err)
	}
	evidenceID := recordPassingEvidence(t, ctx, appStore, run.ID, "complete-idempotency")
	completeMutation := testMutation("complete-complete")
	if err := appStore.CompleteRunWithEvidence(ctx, completeMutation, run.ID, "Passed", []string{evidenceID}); err != nil {
		t.Fatalf("complete run: %v", err)
	}
	if err := appStore.CompleteRunWithEvidence(ctx, completeMutation, run.ID, "Passed", []string{evidenceID}); err != nil {
		t.Fatalf("replay completion: %v", err)
	}
	if err := appStore.CompleteRunWithEvidence(ctx, completeMutation, run.ID, "Different summary", []string{evidenceID}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed completion payload error = %v, want conflict", err)
	}
	detail, err := appStore.GetTaskDetail(ctx, task.ID)
	if err != nil {
		t.Fatalf("get completed task detail: %v", err)
	}
	if len(detail.Artifacts) != 1 {
		t.Fatalf("completion artifacts after replay = %d, want 1", len(detail.Artifacts))
	}

	blockedTask, err := appStore.CreateTask(ctx, domain.CreateTaskInput{
		Title: "Block idempotently", Status: domain.TaskReady, Priority: "medium", BudgetMinutes: 10,
	})
	if err != nil {
		t.Fatalf("create block task: %v", err)
	}
	blockedRun, err := appStore.ClaimTask(
		ctx, testMutation("claim-block"), blockedTask.ID, "agt_idempotent", time.Minute,
	)
	if err != nil {
		t.Fatalf("claim block task: %v", err)
	}
	if _, err := appStore.SubmitPlan(
		ctx, testMutation("plan-block"), blockedRun.ID, "Inspect failure.", false,
	); err != nil {
		t.Fatalf("submit block plan: %v", err)
	}
	blockMutation := testMutation("block-run")
	if err := appStore.BlockRun(ctx, blockMutation, blockedRun.ID, "External prerequisite missing"); err != nil {
		t.Fatalf("block run: %v", err)
	}
	if err := appStore.BlockRun(ctx, blockMutation, blockedRun.ID, "External prerequisite missing"); err != nil {
		t.Fatalf("replay block: %v", err)
	}
	if err := appStore.BlockRun(ctx, blockMutation, blockedRun.ID, "Different reason"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed block payload error = %v, want conflict", err)
	}
	blockedEvents, err := appStore.CountRunEvents(ctx, blockedRun.ID, "run.blocked")
	if err != nil {
		t.Fatalf("count blocked events: %v", err)
	}
	if blockedEvents != 1 {
		t.Fatalf("blocked events after replay = %d, want 1", blockedEvents)
	}

	wantOperations := map[string]int{
		operationClaimTask: 2, operationSubmitPlan: 2,
		operationStartRun: 1, operationBeginVerification: 1,
		operationCompleteRun: 1, operationBlockRun: 1,
	}
	rows, err := appStore.db.QueryContext(ctx, `
		SELECT operation, COUNT(*) FROM idempotency_records GROUP BY operation
	`)
	if err != nil {
		t.Fatalf("list idempotency records: %v", err)
	}
	defer rows.Close()
	gotOperations := make(map[string]int)
	for rows.Next() {
		var operation string
		var count int
		if err := rows.Scan(&operation, &count); err != nil {
			t.Fatalf("scan idempotency operation: %v", err)
		}
		gotOperations[operation] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate idempotency operations: %v", err)
	}
	if !reflect.DeepEqual(gotOperations, wantOperations) {
		t.Fatalf("idempotency operations = %#v, want %#v", gotOperations, wantOperations)
	}
}

func testMutation(key string) Mutation {
	return Mutation{ActorID: "service:test-marina", IdempotencyKey: key}
}

func recordPassingEvidence(t *testing.T, ctx context.Context, appStore *Store, runID, suffix string) string {
	t.Helper()
	run, err := appStore.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("get run for evidence: %v", err)
	}
	if run.ActivePlanRevisionID == nil {
		t.Fatal("run has no active plan revision")
	}
	evidence, err := appStore.RecordEvidence(ctx, domain.EvidenceInput{
		RunID:            runID,
		PlanRevisionID:   *run.ActivePlanRevisionID,
		Type:             "acceptance",
		Producer:         "agent:test",
		Verifier:         "check:" + suffix,
		SubjectDigest:    run.AcceptanceCriteriaDigest,
		SourceRevision:   "test-source",
		Command:          "go test ./...",
		Outcome:          "passed",
		ProvenanceURI:    "test://" + suffix + "/provenance",
		ProvenanceDigest: "sha256:" + suffix + "-provenance",
		ObservedAt:       time.Now().UTC().UnixMilli(),
		ArtifactURI:      "test://" + suffix + "/artifact",
		ArtifactDigest:   "sha256:" + suffix + "-artifact",
		PolicyVersion:    "test-v1",
	})
	if err != nil {
		t.Fatalf("record evidence: %v", err)
	}
	return evidence.ID
}
