package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestConversationOutcomeRequiresScopedPublicationAndSeparateReadyGrant(t *testing.T) {
	s, _ := newTestStoreAndHandler(t)
	ctx := context.Background()
	profileDigest := strings.Repeat("b", 64)
	if err := s.PublishWorkloads(ctx, "prj_default", "test", domain.WorkloadCatalog{ExpiresAt: time.Now().Add(time.Minute).UnixMilli(), Profiles: []domain.WorkloadProfile{{ID: "repository/v1", Digest: profileDigest, Harness: "codex/v1", Authority: []string{"source.read", "source.write", "pull_request.create"}, MaxTokens: 30000, BudgetMinutes: 20, MinAttempts: 2}}}); err != nil {
		t.Fatal(err)
	}
	channel, _ := s.CreateChannel(ctx, "prj_default", "outcomes-api")
	_, _ = s.RegisterAgent(ctx, domain.Agent{ID: "agt_outcome", InstallationID: "outcome", Name: "Outcome", Capabilities: []string{"conversation.invoke.prj_default"}})
	invoked, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_outcome", Body: "Fix it", RequestID: "request-outcome", AuthorID: "operator:one"})
	if err != nil {
		t.Fatal(err)
	}
	input := domain.ConversationOutcomeInput{InvocationMessageID: invoked.Message.ID, AgentID: "agt_outcome", Status: "completed", Body: "Task created", ResultDigest: "digest", ProfileID: "repository/v1", ProfileDigest: profileDigest, Task: &domain.CreateTaskInput{Title: "Fix it", Status: domain.TaskReady, AcceptanceCriteria: "Regression passes", Budget: &domain.Budget{TimeMinutes: 10, MaxAttempts: 2, MaxTokens: 20000, Authority: []string{"source.read", "source.write", "pull_request.create"}}}}
	body, _ := json.Marshal(input)
	path := filepath.Join(t.TempDir(), "service.json")
	for _, tc := range []struct {
		capabilities []string
		project      string
		path         string
		status       int
	}{
		{[]string{capabilityConversationOutcome}, "prj_other", "/api/v1/conversation/outcomes", http.StatusForbidden},
		{[]string{capabilityConversationOutcome}, "prj_default", "/api/v1/conversation/outcomes", http.StatusForbidden},
		{[]string{capabilityConversationDelegate}, "prj_default", "/api/v1/conversation/delegations", http.StatusOK},
	} {
		fingerprint := sha256.Sum256([]byte(serviceAuthTokenOne))
		policy, _ := json.Marshal(map[string]any{"version": 1, "identities": []any{map[string]any{"id": "marina-installation", "token_sha256": []string{hex.EncodeToString(fingerprint[:])}, "projects": []string{tc.project}, "capabilities": tc.capabilities}}})
		if err := os.WriteFile(path, policy, 0600); err != nil {
			t.Fatal(err)
		}
		auth, err := NewFileServiceAuthorizer(path)
		if err != nil {
			t.Fatal(err)
		}
		h := New(s, slog.New(slog.NewTextHandler(io.Discard, nil)), WithServiceAuthorizer(auth)).Handler()
		r := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+serviceAuthTokenOne)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("grants=%v project=%s: %d %s", tc.capabilities, tc.project, w.Code, w.Body.String())
		}
	}
}
