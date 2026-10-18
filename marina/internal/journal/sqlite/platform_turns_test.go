package sqlite_test

import (
	"context"
	"testing"

	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func submitTurn(t *testing.T, p *sqlite.Platform, name string) sqlite.PlatformTurn {
	t.Helper()
	turn, created, err := p.SubmitTurn(context.Background(), sqlite.NewPlatformTurn{Namespace: "factory", Name: name, UID: "uid-" + name, ProjectID: "factory.acceptance",
		AgentID: "marina", ThreadID: "thr-1", Messages: []sqlite.TurnMessage{{ID: "m1", Role: "human", Body: "Hello"}, {ID: "m2", Role: "agent", Body: "Hi"}, {ID: "m3", Role: "human", Body: "Which file?"}}})
	if err != nil || !created {
		t.Fatalf("submit turn: %v %v", created, err)
	}
	return turn
}

func TestTurnsFeedTheConversationWorkflowAndPublishOnce(t *testing.T) {
	p, _, now := platformStore(t)
	ctx := context.Background()
	turn := submitTurn(t, p, "turn-1")
	feed, err := p.ListConversationInvocations(ctx, "factory.acceptance", 0)
	if err != nil || len(feed) != 1 {
		t.Fatalf("feed = %+v %v", feed, err)
	}
	invoked := feed[0].Message
	if invoked.ID != turn.ID || invoked.InvokedAgentID != "marina" || invoked.InvocationRequestID != "uid-turn-1" || invoked.Body != "Which file?" || invoked.ThreadID != "thr-1" {
		t.Fatalf("invoked message = %+v", invoked)
	}
	if other, _ := p.ListConversationInvocations(ctx, "other.project", 0); len(other) != 0 {
		t.Fatalf("feed escaped project: %+v", other)
	}
	page, err := p.PageConversationMessages(ctx, invoked.ChannelID, "")
	if err != nil || len(page.Items) != 3 || page.Items[0].ID != "m1" || page.Items[2].ID != turn.ID {
		t.Fatalf("thread = %+v %v", page, err)
	}
	q, err := p.EnsureConversationQuestion(ctx, workcontrol.ConversationQuestionInput{InvocationMessageID: turn.ID, AgentID: "marina", QuestionID: "q1", Text: "Which module?", Choices: []string{"clamp", "range"}, ExpiresAt: now.Add(10 * 60e9).UnixMilli()})
	if err != nil || q.Status != "pending" || q.ChannelID != invoked.ChannelID {
		t.Fatalf("question = %+v %v", q, err)
	}
	decisions, _ := p.ListDecisions(ctx, turn.ID)
	if len(decisions) != 1 || decisions[0].OwnerKind != "Turn" {
		t.Fatalf("decisions = %+v", decisions)
	}
	if _, err := p.AnswerDecision(ctx, "factory", decisions[0].Name, "range", "deck"); err != nil {
		t.Fatal(err)
	}
	q, err = p.EnsureConversationQuestion(ctx, workcontrol.ConversationQuestionInput{InvocationMessageID: turn.ID, AgentID: "marina", QuestionID: "q1", Text: "Which module?", Choices: []string{"clamp", "range"}})
	if err != nil || q.Status != "answered" || q.Answer != "range" || q.AnswerMessageID == "" {
		t.Fatalf("answered question = %+v %v", q, err)
	}
	outcome := workcontrol.ConversationOutcomeInput{InvocationMessageID: turn.ID, AgentID: "marina", Status: "completed", Body: "src/range.cjs", ResultDigest: "r1",
		Task: &workcontrol.ConversationTaskInput{Title: "Add a test", AcceptanceCriteria: "A test passes", Status: "ready"}}
	out, err := p.PublishConversationOutcome(ctx, outcome)
	if err != nil || out.MessageID == "" || out.TaskID == "" {
		t.Fatalf("outcome = %+v %v", out, err)
	}
	if again, err := p.PublishConversationOutcome(ctx, outcome); err != nil || again != out {
		t.Fatalf("replay = %+v %v", again, err)
	}
	outcome.Body = "something else"
	if _, err := p.PublishConversationOutcome(ctx, outcome); !isConflict(err) {
		t.Fatalf("changed outcome: %v", err)
	}
	stored, _ := p.Turn(ctx, turn.ID)
	if stored.Status != "completed" || stored.Outcome == nil || stored.Outcome.Task.Title != "Add a test" {
		t.Fatalf("turn = %+v", stored)
	}
}

func TestTurnNeedsAHumanLastMessageAndAChangedSpecConflicts(t *testing.T) {
	p, _, _ := platformStore(t)
	ctx := context.Background()
	if _, _, err := p.SubmitTurn(ctx, sqlite.NewPlatformTurn{Namespace: "factory", Name: "turn-1", UID: "u", ProjectID: "p", AgentID: "marina", ThreadID: "t",
		Messages: []sqlite.TurnMessage{{ID: "m1", Role: "agent", Body: "I asked"}}}); err == nil {
		t.Fatal("agent-last turn accepted")
	}
	submitTurn(t, p, "turn-2")
	if _, _, err := p.SubmitTurn(ctx, sqlite.NewPlatformTurn{Namespace: "factory", Name: "turn-2", UID: "uid-turn-2", ProjectID: "factory.acceptance", AgentID: "marina", ThreadID: "thr-1",
		Messages: []sqlite.TurnMessage{{ID: "m1", Role: "human", Body: "Different"}}}); !isConflict(err) {
		t.Fatalf("changed turn: %v", err)
	}
}
