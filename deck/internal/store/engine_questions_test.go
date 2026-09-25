package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestEngineQuestionIsIdempotentAndAnswerRequiresActiveExactRun(t *testing.T) {
	ctx := context.Background()
	s, _, run := createPlanningRun(t, ctx, "engine-question.db")
	defer s.Close()
	_, _, err := s.SubmitPlanRevision(ctx, testMutation("plan"), run.ID, domain.PlanRevisionInput{
		Plan: "Run bounded task", ContextDigest: "sha256:context", RiskClass: "low", VerificationStrategy: "Independent validation", RollbackApproach: "Cancel task",
	}, domain.DecisionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	run, err = s.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour).UnixMilli()
	input := domain.EngineQuestion{OperationID: "ask-1", Question: "Which database?", Options: []string{"PostgreSQL", "SQLite"}, ExpiresAt: expires}
	mutation := testMutation("question-1")
	mutation.Guard = &RunGuard{AgentID: run.AgentID, ExpectedVersion: run.Version, FenceToken: run.FenceToken}
	first, err := s.RequestEngineQuestion(ctx, mutation, run.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := s.RequestEngineQuestion(ctx, mutation, run.ID, input)
	if err != nil || replayed.ID != first.ID || first.InteractionID != input.OperationID {
		t.Fatalf("question replay = %#v, %v", replayed, err)
	}
	if _, err := s.AnswerDecision(ctx, first.ID, "SQLite"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AnswerDecision(ctx, first.ID, "PostgreSQL"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate answer = %v", err)
	}
	secondInput := domain.EngineQuestion{OperationID: "ask-2", Question: "Explain the exception", ExpiresAt: expires}
	secondMutation := testMutation("question-2")
	secondMutation.Guard = mutation.Guard
	second, err := s.RequestEngineQuestion(ctx, secondMutation, run.ID, secondInput)
	if err != nil || second.AnswerMode != "text" {
		t.Fatalf("text question = %#v, %v", second, err)
	}
	answered, err := s.AnswerDecision(ctx, second.ID, "Request changes")
	if err != nil || answered.Answer != "Request changes" {
		t.Fatalf("text answer resembling a plan command = %#v, %v", answered, err)
	}
	run, err = s.GetRun(ctx, run.ID)
	if err != nil || run.Status != domain.RunRunning {
		t.Fatalf("engine answer changed the plan state: %#v, %v", run, err)
	}
	thirdInput := domain.EngineQuestion{OperationID: "ask-3", Question: "Explain the next exception", ExpiresAt: expires}
	thirdMutation := testMutation("question-3")
	thirdMutation.Guard = mutation.Guard
	third, err := s.RequestEngineQuestion(ctx, thirdMutation, run.ID, thirdInput)
	if err != nil {
		t.Fatal(err)
	}
	block := testMutation("block-engine-question")
	block.Guard = mutation.Guard
	if err := s.BlockRun(ctx, block, run.ID, "operator cancelled execution"); err != nil {
		t.Fatal(err)
	}
	closed, err := s.GetDecision(ctx, third.ID)
	if err != nil || closed.Status != "cancelled" {
		t.Fatalf("closed question = %#v, %v", closed, err)
	}
	if _, err := s.AnswerDecision(ctx, third.ID, "late answer"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("cancelled answer = %v", err)
	}
}
