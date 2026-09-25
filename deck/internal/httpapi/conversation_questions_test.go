package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestConversationQuestionServiceAndOperatorScopes(t *testing.T) {
	s, _ := newTestStoreAndHandler(t)
	ctx := context.Background()
	channel, err := s.CreateChannel(ctx, "prj_default", "questions-api")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RegisterAgent(ctx, domain.Agent{ID: "agt_questions", InstallationID: "questions", Name: "Questions", Capabilities: []string{"conversation.invoke.prj_default"}})
	if err != nil {
		t.Fatal(err)
	}
	invoked, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_questions", Body: "Help", RequestID: "request-api", AuthorID: "operator:one"})
	if err != nil {
		t.Fatal(err)
	}
	policy := filepath.Join(t.TempDir(), "service.json")
	questionBody, _ := json.Marshal(domain.ConversationQuestionInput{InvocationMessageID: invoked.Message.ID, AgentID: "agt_questions", QuestionID: "scope", Text: "Which provider?"})
	for _, tc := range []struct {
		capability, project string
		status              int
	}{
		{capabilityProviderInfo, "prj_default", http.StatusForbidden},
		{capabilityConversationQuestion, "prj_other", http.StatusForbidden},
		{capabilityConversationQuestion, "prj_default", http.StatusOK},
	} {
		writeServiceAuthPolicy(t, policy, serviceAuthTokenOne, []string{tc.capability}, []string{tc.project})
		auth, err := NewFileServiceAuthorizer(policy)
		if err != nil {
			t.Fatal(err)
		}
		handler := New(s, slog.New(slog.NewTextHandler(io.Discard, nil)), WithServiceAuthorizer(auth)).Handler()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/conversation/questions", bytes.NewReader(questionBody))
		request.Header.Set("Authorization", "Bearer "+serviceAuthTokenOne)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("%s/%s: %d %s", tc.capability, tc.project, response.Code, response.Body.String())
		}
	}
	questions, err := s.ListConversationQuestions(ctx, invoked.Thread.ID)
	if err != nil || len(questions) != 1 {
		t.Fatalf("question persisted: %+v %v", questions, err)
	}
	operatorHandler := New(s, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	answer := []byte(`{"answer":"GitHub","request_id":"answer-api"}`)
	for i := 0; i < 2; i++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/operator/conversation/questions/"+questions[0].ID+"/answer", bytes.NewReader(answer))
		response := httptest.NewRecorder()
		operatorHandler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("answer %d: %d %s", i, response.Code, response.Body.String())
		}
	}
	page, err := s.PageThreadMessages(ctx, invoked.Thread.ID, 100, "")
	if err != nil || len(page.Items) != 3 {
		t.Fatalf("one invoked message, question and answer: %+v %v", page, err)
	}
}
