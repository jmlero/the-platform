package conversation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kagent"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
)

type templateSource struct {
	message workcontrol.ConversationMessage
}

func (s templateSource) ListConversationInvocations(_ context.Context, _ string, afterID int64) ([]workcontrol.ConversationInvocation, error) {
	if afterID > 0 {
		return nil, nil
	}
	return []workcontrol.ConversationInvocation{{EventID: 1, InvokedAt: s.message.CreatedAt, Message: s.message}}, nil
}

func (s templateSource) PageConversationMessages(context.Context, string, string) (workcontrol.ConversationMessagePage, error) {
	return workcontrol.ConversationMessagePage{Items: []workcontrol.ConversationMessage{s.message}}, nil
}
func (s templateSource) EnsureConversationQuestion(context.Context, workcontrol.ConversationQuestionInput) (workcontrol.ConversationQuestion, error) {
	return workcontrol.ConversationQuestion{}, nil
}
func (s templateSource) PublishConversationOutcome(_ context.Context, in workcontrol.ConversationOutcomeInput) (workcontrol.ConversationOutcome, error) {
	return workcontrol.ConversationOutcome{InvocationMessageID: in.InvocationMessageID, MessageID: "msg_published", Status: in.Status, ResultDigest: in.ResultDigest}, nil
}

func TestPreparedConversationTemplateCannotServeAnotherTurn(t *testing.T) {
	ctx := context.Background()
	journal, err := sqlite.Open(ctx, t.TempDir()+"/journal.db")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	blobs, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first := workcontrol.ConversationMessage{ID: "msg_first", ProjectID: "prj_1", ChannelID: "chn_1", ThreadID: "thr_1", AuthorType: "human", Body: "First", InvokedAgentID: "agt_1", InvocationRequestID: "req_first"}
	second := first
	second.ID, second.Body, second.InvocationRequestID = "msg_second", "Second", "req_second"
	deadline := time.Now().Add(5 * time.Minute).UTC().Truncate(time.Millisecond)
	if _, err := journal.EnsureConversationTurn(ctx, first, deadline, 5000); err != nil {
		t.Fatal(err)
	}
	selection := rt.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: "kint-engine-controller.kint-engine.svc:8083", Namespace: "kint-engine", Harness: "kint-conversation-byo", Template: "turn-exclusive", Revision: strings.Repeat("a", 64)}
	if err := journal.SaveConversationSpecification(ctx, first.ID, rt.Specification{Engine: &selection, DeadlineAt: deadline}); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.EnsureConversationTurn(ctx, second, deadline, 5000); err != nil {
		t.Fatal(err)
	}
	model := workerapi.Model{Name: "fixture", MaxOutputTokens: 100, Currency: "USD"}
	controller := Controller{ProjectID: "prj_1", AgentID: "agt_1", PolicyVersion: "test", Source: templateSource{second}, Journal: journal, Blobs: blobs, Model: model,
		Profile: Profile{Engine: selection, MaxTokens: 5000, BudgetMinutes: 5}}
	turn, err := journal.GetConversationTurn(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.advance(ctx, turn); err != nil {
		t.Fatal(err)
	}
	turn, err = journal.GetConversationTurn(ctx, second.ID)
	if err != nil || turn.Status != "blocked" || turn.SpecificationDigest != "" {
		t.Fatalf("template reused: %+v %v", turn, err)
	}
}
