package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestChannelHistoryAPIContinuesPastPageLimit(t *testing.T) {
	s, handler := newTestStoreAndHandler(t)
	ctx := context.Background()
	channel, err := s.CreateChannel(ctx, "prj_default", "paged")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 205; i++ {
		if _, err = s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: channel.ID, AuthorType: "human", AuthorID: "test", Body: fmt.Sprintf("message %03d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, prefix := range []string{"/api/v1/channels/", "/api/v1/operator/channels/"} {
		count := 0
		cursor := ""
		for {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("GET", prefix+channel.ID+"/messages?limit=100&cursor="+url.QueryEscape(cursor), nil))
			if response.Code != 200 {
				t.Fatalf("page status=%d: %s", response.Code, response.Body.String())
			}
			var page struct {
				Items []domain.ChannelMessage `json:"items"`
				Next  string                  `json:"next_cursor"`
			}
			if err = json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			for _, message := range page.Items {
				if message.Body != fmt.Sprintf("message %03d", count) {
					t.Fatalf("history at %d = %s", count, message.Body)
				}
				count++
			}
			if page.Next == "" {
				break
			}
			cursor = page.Next
		}
		if count != 205 {
			t.Fatalf("truncated history: %d", count)
		}
	}
}

func TestRepositoryPromotionExplainsReadinessWithoutCreatingWork(t *testing.T) {
	s, handler := newTestStoreAndHandler(t)
	ctx := context.Background()
	channel, err := s.CreateChannel(ctx, "prj_default", "promotion")
	if err != nil {
		t.Fatal(err)
	}
	message, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: channel.ID, AuthorType: "human", AuthorID: "test", Body: "Fix the bug"})
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"title": "Fix bug", "acceptance_criteria": "Regression passes", "status": "ready", "budget": map[string]any{"time_minutes": 30, "max_attempts": 1, "authority": []string{"source.read"}}}
	response := requestJSON(t, handler, http.MethodPost, "/api/v1/operator/messages/"+message.ID+"/promote", "", input)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "two attempts") || !strings.Contains(response.Body.String(), "pull_request.create") {
		t.Fatalf("missing readiness explanation: %d %s", response.Code, response.Body.String())
	}
	count, err := s.TaskCount(ctx)
	if err != nil || count != 0 {
		t.Fatalf("dispatched invalid work: %d %v", count, err)
	}
	input["status"] = "backlog"
	response = requestJSON(t, handler, http.MethodPost, "/api/v1/operator/messages/"+message.ID+"/promote", "", input)
	if response.Code != http.StatusCreated {
		t.Fatalf("draft not allowed: %s", response.Body.String())
	}
}

func TestUnauthenticatedChannelMessageUsesSharedFallbackActor(t *testing.T) {
	s, handler := newTestStoreAndHandler(t)
	channel, err := s.CreateChannel(context.Background(), "prj_default", "fallback")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/channels/"+channel.ID+"/messages", strings.NewReader(`{"body":"hello"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("post message = %d %s", rec.Code, rec.Body)
	}
	var message domain.ChannelMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &message); err != nil {
		t.Fatal(err)
	}
	if message.AuthorType != "agent" || message.AuthorID != fallbackServiceActorID {
		t.Fatalf("author = %s/%s, want agent/%s", message.AuthorType, message.AuthorID, fallbackServiceActorID)
	}
}
