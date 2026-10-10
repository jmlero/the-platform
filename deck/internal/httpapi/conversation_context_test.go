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

func TestConversationContextRequiresReadCapabilityAndInvocationProject(t *testing.T) {
	s, _ := newTestStoreAndHandler(t)
	ctx := context.Background()
	channel, err := s.CreateChannel(ctx, "prj_default", "context")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterAgent(ctx, domain.Agent{ID: "agt_chat", InstallationID: "chat", Name: "Chat", Capabilities: []string{"conversation.invoke.prj_default"}}); err != nil {
		t.Fatal(err)
	}
	source, err := s.PutContextSource(ctx, "prj_default", domain.ContextSourceInput{ID: "goal", Kind: "goal", Title: "Goal", Content: "Keep it small", Trust: "guidance", State: "active"})
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_chat", Body: "Explain this", RequestID: "context-1", AuthorID: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: channel.ID, AuthorType: "human", AuthorID: "operator", Body: "Not invoked"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "auth.json")
	for _, tc := range []struct {
		capability, project, messageID string
		status                         int
	}{
		{capabilityProviderInfo, "prj_default", invocation.Message.ID, http.StatusForbidden},
		{capabilityContextRead, "prj_default", invocation.Message.ID, http.StatusForbidden},
		{capabilityConversationContext, "other", invocation.Message.ID, http.StatusForbidden},
		{capabilityConversationContext, "prj_default", invocation.Message.ID, http.StatusOK},
		{capabilityConversationContext, "prj_default", ordinary.ID, http.StatusNotFound},
	} {
		writeServiceAuthPolicy(t, path, serviceAuthTokenOne, []string{tc.capability}, []string{tc.project})
		auth, err := NewFileServiceAuthorizer(path)
		if err != nil {
			t.Fatal(err)
		}
		handler := New(s, slog.New(slog.NewTextHandler(io.Discard, nil)), WithServiceAuthorizer(auth)).Handler()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/conversation/invocations/"+tc.messageID+"/context", nil)
		r.Header.Set("Authorization", "Bearer "+serviceAuthTokenOne)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s/%s: %d %s", tc.capability, tc.project, w.Code, w.Body.String())
		}
		if w.Code == http.StatusOK {
			var result domain.ResolvedContext
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Sources) != 1 || result.Sources[0].Digest != source.Digest {
				t.Fatalf("result: %#v %v", result, err)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("context response can be cached")
			}
		}
	}
}
