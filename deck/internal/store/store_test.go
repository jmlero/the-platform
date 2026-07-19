package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestHumanAgentLifecycle(t *testing.T) {
	ctx := context.Background()
	appStore, err := Open(ctx, filepath.Join(t.TempDir(), "appdeck-test.db"))
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
	run, err := appStore.ClaimTask(ctx, task.ID, agent.ID, time.Minute)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if _, err := appStore.ClaimTask(ctx, task.ID, agent.ID, time.Minute); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate claim error = %v, want conflict", err)
	}

	decision, err := appStore.SubmitPlan(ctx, run.ID, "Implement, test, and report.", true)
	if err != nil {
		t.Fatalf("submit plan: %v", err)
	}
	if decision == nil || decision.Status != "pending" {
		t.Fatalf("decision = %#v, want pending decision", decision)
	}
	if err := appStore.StartRun(ctx, run.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("start before approval error = %v, want conflict", err)
	}
	if _, err := appStore.AnswerDecision(ctx, decision.ID, "Approve plan"); err != nil {
		t.Fatalf("answer decision: %v", err)
	}
	if err := appStore.StartRun(ctx, run.ID); err != nil {
		t.Fatalf("start run: %v", err)
	}
	if err := appStore.ReportProgress(ctx, run.ID, "Work completed", map[string]any{"percent": 90}); err != nil {
		t.Fatalf("report progress: %v", err)
	}
	if err := appStore.BeginVerification(ctx, run.ID); err != nil {
		t.Fatalf("begin verification: %v", err)
	}
	if err := appStore.CompleteRun(ctx, run.ID, "All checks passed", "- Acceptance criterion passed"); err != nil {
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
	if len(detail.Events) < 7 {
		t.Fatalf("events = %d, want at least 7", len(detail.Events))
	}
}

func TestManualMoveRejectsActiveTask(t *testing.T) {
	ctx := context.Background()
	appStore, err := Open(ctx, filepath.Join(t.TempDir(), "appdeck-test.db"))
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
	if _, err := appStore.ClaimTask(ctx, task.ID, "agt_test", time.Minute); err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if _, err := appStore.MoveTask(ctx, task.ID, domain.TaskBacklog); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("move active task error = %v, want invalid transition", err)
	}
}
