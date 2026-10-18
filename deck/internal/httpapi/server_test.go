package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/marina"
	"github.com/jmlero/the-platform/deck/internal/store"
)

func TestRequestBodyValidation(t *testing.T) {
	const prefix = `{"value":"`
	const suffix = `"}`
	atLimit := prefix + strings.Repeat("x", (1<<20)-len(prefix)-len(suffix)) + suffix
	for _, test := range []struct {
		name    string
		body    string
		invalid bool
	}{
		{"valid", `{"value":"ok"}`, false},
		{"at size limit", atLimit, false},
		{"over size limit", prefix + strings.Repeat("x", 1<<20) + suffix, true},
		{"unknown field", `{"unexpected":true}`, true},
		{"malformed JSON", `{"value":`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var input struct {
				Value string `json:"value"`
			}
			err := readJSON(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body)), &input)
			if test.invalid && !errors.Is(err, domain.ErrInvalidInput) || !test.invalid && err != nil {
				t.Fatalf("readJSON error = %v, invalid = %t", err, test.invalid)
			}
		})
	}
}

func TestReadinessReportsStoreHealthAndWhetherMarinaIsConfigured(t *testing.T) {
	appStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "deck-readiness.db"))
	if err != nil {
		t.Fatalf("open Deck store: %v", err)
	}
	readiness := func(handler http.Handler) (int, map[string]string) {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		var payload struct {
			Readiness map[string]string `json:"readiness"`
		}
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			t.Fatalf("decode readiness: %v", err)
		}
		return response.Code, payload.Readiness
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := marina.New(marina.Config{URL: "http://127.0.0.1:1", TokenFile: "/dev/null"})
	if err != nil {
		t.Fatal(err)
	}
	if status, checks := readiness(New(appStore, logger).Handler()); status != http.StatusOK || checks["store"] != "available" || checks["marina"] != "disabled" {
		t.Fatalf("without Marina = %d %#v", status, checks)
	}
	configured := New(appStore, logger, WithMarina(client)).Handler()
	if status, checks := readiness(configured); status != http.StatusOK || checks["marina"] != "configured" {
		t.Fatalf("with Marina = %d %#v", status, checks)
	}
	if err := appStore.Close(); err != nil {
		t.Fatal(err)
	}
	if status, checks := readiness(configured); status != http.StatusServiceUnavailable || checks["store"] != "unavailable" {
		t.Fatalf("closed store = %d %#v", status, checks)
	}
}

func TestStableErrorEnvelopeEchoesTheRequestID(t *testing.T) {
	appStore, handler := newTestStoreAndHandler(t)
	task, err := appStore.CreateTask(context.Background(), domain.CreateTaskInput{Title: "Backlog work", Priority: "low", BudgetMinutes: 10})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/move", strings.NewReader(`{"status":"unknown"}`))
	request.Header.Set("X-Request-ID", "contract-error-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var apiError apiErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&apiError); err != nil || response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, %v", response.Code, err)
	}
	if apiError.Code != "invalid_input" || apiError.Retryable || apiError.RequestID != "contract-error-1" || apiError.Error != apiError.Message {
		t.Fatalf("API error = %#v", apiError)
	}
}

// Marina owns execution; Deck serves no route an executor could call.
func TestDeckServesNoExecutorRoutes(t *testing.T) {
	handler := newTestHandler(t)
	for _, route := range []string{
		"GET /api/v1/provider", "GET /api/v1/tasks", "POST /api/v1/tasks/tsk_1/claim", "POST /api/v1/agents",
		"POST /api/v1/runs/run_1/heartbeat", "POST /api/v1/runs/run_1/plan", "POST /api/v1/runs/run_1/complete",
		"GET /api/v1/runs/run_1/commands", "GET /api/v1/decisions", "POST /api/v1/decisions/d/answer", "GET /api/v1/events",
		"GET /api/v1/conversation/invocations", "POST /api/v1/conversation/outcomes", "POST /api/v1/conversation/questions",
		"PUT /api/v1/projects/prj_default/workloads", "PUT /api/v1/projects/prj_default/repository/result",
		"GET /api/v1/runs/run_1/context", "GET /api/v1/channels", "GET /api/v1/operator/provider",
		"PUT /api/v1/operator/projects/prj_default/repository",
	} {
		method, path, _ := strings.Cut(route, " ")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader("{}")))
		if response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d", route, response.Code)
		}
	}
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d", response.Code, status)
	}
	var apiError apiErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&apiError); err != nil {
		t.Fatalf("decode API error: %v", err)
	}
	if apiError.Code != code || apiError.RequestID == "" {
		t.Fatalf("API error = %#v, want code %q", apiError, code)
	}
}

func newTestHandler(t *testing.T) http.Handler {
	_, handler := newTestStoreAndHandler(t)
	return handler
}

func newTestStoreAndHandler(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	appStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "deck-http-test.db"))
	if err != nil {
		t.Fatalf("open Deck store: %v", err)
	}
	t.Cleanup(func() {
		if err := appStore.Close(); err != nil {
			t.Errorf("close Deck store: %v", err)
		}
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return appStore, New(appStore, logger).Handler()
}
