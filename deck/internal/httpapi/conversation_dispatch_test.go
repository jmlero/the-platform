package httpapi

import (
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

func TestConversationInvocationFeedRequiresCapabilityAndProjectGrant(t *testing.T) {
	s, _ := newTestStoreAndHandler(t)
	ctx := context.Background()
	channel, err := s.CreateChannel(ctx, "prj_default", "dispatch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterAgent(ctx, domain.Agent{ID: "agt_dispatch", InstallationID: "dispatch", Name: "Dispatch", Capabilities: []string{"conversation.invoke.prj_default"}}); err != nil {
		t.Fatal(err)
	}
	invoked, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_dispatch", Body: "Explain this", RequestID: "request-1", AuthorID: "operator:one"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "auth.json")
	for _, tc := range []struct {
		capability, project string
		status              int
	}{
		{capabilityProviderInfo, "prj_default", http.StatusForbidden},
		{capabilityConversationList, "prj_other", http.StatusForbidden},
		{capabilityConversationList, "prj_default", http.StatusOK},
	} {
		writeServiceAuthPolicy(t, path, serviceAuthTokenOne, []string{tc.capability}, []string{tc.project})
		auth, err := NewFileServiceAuthorizer(path)
		if err != nil {
			t.Fatal(err)
		}
		handler := New(s, slog.New(slog.NewTextHandler(io.Discard, nil)), WithServiceAuthorizer(auth)).Handler()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/conversation/invocations?project_id=prj_default&after_id=0", nil)
		request.Header.Set("Authorization", "Bearer "+serviceAuthTokenOne)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("%s/%s: %d %s", tc.capability, tc.project, response.Code, response.Body.String())
		}
		if response.Code == http.StatusOK {
			var page struct {
				Items []domain.ConversationDispatch `json:"items"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || page.Items[0].Message.ID != invoked.Message.ID {
				t.Fatalf("invocation page: %+v %v", page, err)
			}
		}
	}
}
