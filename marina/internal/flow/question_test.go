package flow

import (
	"context"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

type questionProvider struct {
	Provider
	run       workcontrol.Run
	requested *workcontrol.EngineQuestion
}

func (p *questionProvider) GetWork(context.Context, string) (workcontrol.WorkDetail, error) {
	return workcontrol.WorkDetail{Runs: []workcontrol.Run{p.run}}, nil
}
func (p *questionProvider) RequestEngineQuestion(_ context.Context, _ string, q workcontrol.EngineQuestion) (workcontrol.Decision, error) {
	p.requested = &q
	return workcontrol.Decision{ID: "decision", RunID: p.run.ID, TaskID: p.run.WorkItemID, InteractionID: q.OperationID}, nil
}

func TestQuestionProjectionAdvancesWhenProgressChangedRunVersion(t *testing.T) {
	ctx := context.Background()
	j, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	deadline := time.Now().Add(time.Hour)
	e, _, err := j.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", LeaseExpiresAt: deadline, Budget: execution.Budget{DeadlineAt: deadline, MaxAttempts: 1, MaxTokens: 20000}})
	if err != nil {
		t.Fatal(err)
	}
	p := &questionProvider{run: workcontrol.Run{ID: "run", WorkItemID: "task", AgentID: e.AgentID, TaskVersion: e.TaskVersion, FenceToken: e.FenceToken, Version: 8}}
	c := Controller{Journal: j, Provider: p}
	s := state{Step: "analyze", QuestionID: "scope", QuestionRunVersion: 7}
	if err := c.handleEngineQuestion(ctx, e, rt.Attempt{ID: "attempt"}, s, rt.Question{ID: "scope", Text: "Which records?"}); err != nil {
		t.Fatal(err)
	}
	if p.requested == nil || p.requested.ExpectedVersion != 8 || p.requested.OperationID != "attempt/scope" {
		t.Fatalf("question never projected with refreshed version: %#v", p.requested)
	}
	got, err := c.load(ctx, e)
	if err != nil || got.QuestionDecisionID != "decision" {
		t.Fatalf("decision correlation not persisted: %#v, %v", got, err)
	}
}
