package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestOperatorChannelPromotionAndExportAPI(t *testing.T) {
	_, handler := newTestStoreAndHandler(t)
	channelResponse := requestJSON(t, handler, http.MethodPost, "/api/v1/operator/channels", "", map[string]any{
		"project_id": "prj_default", "name": "delivery",
	})
	if channelResponse.Code != http.StatusCreated {
		t.Fatalf("channel status = %d, body = %s", channelResponse.Code, channelResponse.Body.String())
	}
	var channel domain.Channel
	if err := json.NewDecoder(channelResponse.Body).Decode(&channel); err != nil {
		t.Fatalf("decode channel: %v", err)
	}
	messageResponse := requestJSON(t, handler, http.MethodPost, "/api/v1/operator/channels/"+channel.ID+"/messages", "", map[string]any{
		"body": "Turn the report into bounded work.", "references": map[string]any{},
	})
	if messageResponse.Code != http.StatusCreated {
		t.Fatalf("message status = %d, body = %s", messageResponse.Code, messageResponse.Body.String())
	}
	var message domain.ChannelMessage
	if err := json.NewDecoder(messageResponse.Body).Decode(&message); err != nil {
		t.Fatalf("decode message: %v", err)
	}
	promoteResponse := requestJSON(t, handler, http.MethodPost, "/api/v1/operator/messages/"+message.ID+"/promote", "", map[string]any{
		"title": "Investigate the report", "acceptance_criteria": "A source-bound diagnosis is recorded.",
		"status": "backlog", "priority": "high",
		"budget": map[string]any{"time_minutes": 30, "max_attempts": 2, "max_tokens": 50000, "max_cost_minor": 1000, "currency": "EUR", "max_concurrency": 1, "authority": []string{"source.read"}},
	})
	if promoteResponse.Code != http.StatusCreated {
		t.Fatalf("promote status = %d, body = %s", promoteResponse.Code, promoteResponse.Body.String())
	}

	exportRequest := httptest.NewRequest(http.MethodGet, "/api/v1/operator/export?project_id=prj_default", nil)
	exportResponse := httptest.NewRecorder()
	handler.ServeHTTP(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusOK || !strings.Contains(exportResponse.Body.String(), `"schema_version": "deck-export/v1"`) {
		t.Fatalf("export status = %d, body = %s", exportResponse.Code, exportResponse.Body.String())
	}
	if strings.Contains(exportResponse.Body.String(), `"content"`) {
		t.Fatalf("export leaked artifact content: %s", exportResponse.Body.String())
	}
}

func requestJSON(t *testing.T, handler http.Handler, method, target, idempotencyKey string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal %s %s: %v", method, target, err)
	}
	request := httptest.NewRequest(method, target, strings.NewReader(string(encoded)))
	request.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
