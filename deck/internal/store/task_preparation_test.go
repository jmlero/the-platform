package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestResultNeedsHumanAcceptanceAndAcceptanceSurvivesExport(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, _ := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Review result", Status: domain.TaskReady})
	if _, err := s.AcceptTask(ctx, task.ID, task.Version, "operator", false); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("accepted unfinished task: %v", err)
	}
	s.RegisterAgent(ctx, domain.Agent{ID: "review-agent", Name: "Review agent"})
	run, err := s.ClaimTask(ctx, testMutation("review-claim"), task.ID, "review-agent", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitPlan(ctx, testMutation("review-plan"), run.ID, "Verify result", false); err != nil {
		t.Fatal(err)
	}
	if err = s.BeginVerification(ctx, testMutation("review-verify"), run.ID); err != nil {
		t.Fatal(err)
	}
	evidence := recordPassingEvidence(t, ctx, s, run.ID, "review")
	if err = s.CompleteRunWithEvidence(ctx, testMutation("review-complete"), run.ID, "Verified", []string{evidence}); err != nil {
		t.Fatal(err)
	}
	task, _ = s.GetTask(ctx, task.ID)
	if task.Review != nil {
		t.Fatal("execution accepted its own result")
	}
	if _, err = s.AcceptTask(ctx, task.ID, task.Version-1, "operator", false); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("accepted stale version: %v", err)
	}
	accepted, err := s.AcceptTask(ctx, task.ID, task.Version, "operator", false)
	if err != nil || accepted.Review == nil || accepted.Review.RunID != run.ID {
		t.Fatalf("accept: %+v %v", accepted, err)
	}
	replay, err := s.AcceptTask(ctx, task.ID, task.Version, "operator", false)
	if err != nil || replay.Version != accepted.Version {
		t.Fatalf("accept replay: %+v %v", replay, err)
	}
	encoded, err := s.ExportProject(ctx, task.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := Open(ctx, filepath.Join(t.TempDir(), "import.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer imported.Close()
	if err = imported.ImportProject(ctx, encoded); err != nil {
		t.Fatal(err)
	}
	restored, err := imported.GetTask(ctx, task.ID)
	if err != nil || restored.Review == nil || *restored.Review != *accepted.Review {
		t.Fatalf("lost acceptance: %+v %v", restored, err)
	}
	requeued, err := s.MoveTask(ctx, task.ID, domain.TaskBacklog)
	if err != nil || requeued.Review != nil {
		t.Fatalf("old acceptance survived requeue: %+v %v", requeued, err)
	}
}

func TestDependenciesRejectCyclesAndBlockDirectClaims(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Prerequisite"})
	b, _ := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Dependent"})
	b, err = s.SetTaskDependencies(ctx, b.ID, b.Version, []string{a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetTaskDependencies(ctx, a.ID, a.Version, []string{b.ID}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("cycle allowed: %v", err)
	}
	b, err = s.MoveTask(ctx, b.ID, domain.TaskReady)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.NextReadyTask(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("discovered dependent: %v", err)
	}
	s.RegisterAgent(ctx, domain.Agent{ID: "dep-agent", Name: "Dependency agent"})
	if _, err = s.ClaimTask(ctx, testMutation("dep-claim"), b.ID, "dep-agent", time.Minute); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("claimed dependent: %v", err)
	}
	if err = s.DeleteDraft(ctx, a.ID, a.Version); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("deleted prerequisite: %v", err)
	}
}

func TestCodeDependencyWaitsForHumanAcceptanceAndSourceAvailability(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	budget := domain.Budget{TimeMinutes: 30, MaxAttempts: 2, Authority: []string{"source.read", "source.write", "pull_request.create"}}
	first, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "First", AcceptanceCriteria: "Verified", Status: domain.TaskReady, Budget: &budget})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Uses first"})
	second, err = s.SetTaskDependencies(ctx, second.ID, second.Version, []string{first.ID})
	if err != nil {
		t.Fatal(err)
	}
	s.MoveTask(ctx, second.ID, domain.TaskReady)
	s.RegisterAgent(ctx, domain.Agent{ID: "code-dep", Name: "Code dependency"})
	run, err := s.ClaimTask(ctx, testMutation("code-dep-claim"), first.ID, "code-dep", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitPlan(ctx, testMutation("code-dep-plan"), run.ID, "Implement", false); err != nil {
		t.Fatal(err)
	}
	if err = s.BeginVerification(ctx, testMutation("code-dep-verify"), run.ID); err != nil {
		t.Fatal(err)
	}
	evidence := recordPassingEvidence(t, ctx, s, run.ID, "code-dep")
	if err = s.CompleteRunWithEvidence(ctx, testMutation("code-dep-done"), run.ID, "Verified PR", []string{evidence}); err != nil {
		t.Fatal(err)
	}
	first, _ = s.GetTask(ctx, first.ID)
	if _, err = s.NextReadyTask(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("PR completion released dependency: %v", err)
	}
	first, err = s.AcceptTask(ctx, first.ID, first.Version, "human", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.NextReadyTask(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unmerged PR released dependency: %v", err)
	}
	first, err = s.AcceptTask(ctx, first.ID, first.Version, "human", true)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.NextReadyTask(ctx)
	if err != nil || next.ID != second.ID {
		t.Fatalf("accepted code not released: %+v %v", next, err)
	}
	if _, err = s.ClaimTask(ctx, testMutation("code-dep-second"), second.ID, "code-dep", time.Minute); err != nil {
		t.Fatal(err)
	}
}
