package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/store"
)

func TestChatQuestionAnswersReachMarinaOnceAndAppearInTheThread(t *testing.T) {
	s, handler, double := newMarinaHandler(t)
	ctx := context.Background()
	if err := s.EnsureMarinaAgent(ctx, []string{"prj_default"}); err != nil {
		t.Fatal(err)
	}
	channel, err := s.CreateChannel(ctx, "prj_default", "questions-api")
	if err != nil {
		t.Fatal(err)
	}
	invoked, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: store.MarinaAgentID, Body: "Help", RequestID: "request-api", AuthorID: "operator:one"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BeginMarinaTurn(ctx, invoked.Message.ID, "deck-turn", "factory", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	q, err := s.EnsureConversationQuestion(ctx, domain.ConversationQuestionInput{InvocationMessageID: invoked.Message.ID, AgentID: store.MarinaAgentID, QuestionID: "deck-turn-q1", Text: "Which provider?"})
	if err != nil {
		t.Fatal(err)
	}
	listed := requestJSON(t, handler, http.MethodGet, "/api/v1/operator/threads/"+invoked.Thread.ID+"/questions", "", nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "Which provider?") {
		t.Fatalf("questions = %d %s", listed.Code, listed.Body)
	}
	answer := map[string]string{"answer": "GitHub", "request_id": "answer-api"}
	for i := 0; i < 2; i++ {
		response := requestJSON(t, handler, http.MethodPost, "/api/v1/operator/conversation/questions/"+q.ID+"/answer", "", answer)
		if response.Code != http.StatusOK {
			t.Fatalf("answer %d: %d %s", i, response.Code, response.Body)
		}
	}
	if len(double.requests) != 1 || !strings.HasPrefix(double.requests[0], "POST /apis/marina.dev/v1/namespaces/factory/decisions/deck-turn-q1/answer deck-"+q.ID) {
		t.Fatalf("forwarded = %v", double.requests)
	}
	page, err := s.PageThreadMessages(ctx, invoked.Thread.ID, 100, "")
	if err != nil || len(page.Items) != 3 {
		t.Fatalf("one invoked message, question and answer: %+v %v", page, err)
	}
}
