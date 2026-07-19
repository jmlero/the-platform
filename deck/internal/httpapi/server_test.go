package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/store"
)

func TestProviderInfoAdvertisesDeterministicContract(t *testing.T) {
	handler := newTestHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/provider", nil)
	request.Header.Set("X-Request-ID", "contract-test-1")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if value := response.Header().Get("X-Request-ID"); value != "contract-test-1" {
		t.Fatalf("X-Request-ID = %q", value)
	}
	if value := response.Header().Get("Cache-Control"); value != "no-store" {
		t.Fatalf("Cache-Control = %q", value)
	}
	var info providerInfoResponse
	if err := json.NewDecoder(response.Body).Decode(&info); err != nil {
		t.Fatalf("decode provider info: %v", err)
	}
	if info.Provider != providerName || info.ProtocolVersion != protocolVersion || !info.Ready {
		t.Fatalf("provider info = %#v", info)
	}
	if info.ServerTime <= 0 || info.Readiness["store"] != "available" {
		t.Fatalf("provider readiness = %#v", info)
	}
	if !reflect.DeepEqual(info.Capabilities, protocolCapabilities) {
		t.Fatalf("capabilities = %#v, want %#v", info.Capabilities, protocolCapabilities)
	}
}

func TestAdvertisedCapabilitiesMatchOpenAPIOperations(t *testing.T) {
	contract, err := os.Open(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("open OpenAPI contract: %v", err)
	}
	defer contract.Close()

	capabilities := make([]string, 0, len(protocolCapabilities))
	seen := make(map[string]struct{}, len(protocolCapabilities))
	scanner := bufio.NewScanner(contract)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "x-capability:") {
			continue
		}
		capability := strings.TrimSpace(strings.TrimPrefix(line, "x-capability:"))
		if _, exists := seen[capability]; exists {
			t.Fatalf("OpenAPI capability %q is assigned more than once", capability)
		}
		seen[capability] = struct{}{}
		capabilities = append(capabilities, capability)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan OpenAPI contract: %v", err)
	}
	sort.Strings(capabilities)
	if !reflect.DeepEqual(capabilities, protocolCapabilities) {
		t.Fatalf("OpenAPI capabilities = %#v, provider advertises %#v", capabilities, protocolCapabilities)
	}
}

func TestProviderInfoRemainsReadableWhenStoreIsUnavailable(t *testing.T) {
	appStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "closed-appdeck.db"))
	if err != nil {
		t.Fatalf("open AppDeck store: %v", err)
	}
	handler := New(appStore, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	if err := appStore.Close(); err != nil {
		t.Fatalf("close AppDeck store: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/provider", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want provider information despite degraded readiness", response.Code)
	}
	var info providerInfoResponse
	if err := json.NewDecoder(response.Body).Decode(&info); err != nil {
		t.Fatalf("decode provider info: %v", err)
	}
	if info.Ready || info.Readiness["store"] != "unavailable" {
		t.Fatalf("provider readiness = %#v", info)
	}
}

func TestTaskStatusFilterAndStableErrorEnvelope(t *testing.T) {
	appStore, handler := newTestStoreAndHandler(t)
	ctx := context.Background()
	if _, err := appStore.CreateTask(ctx, domain.CreateTaskInput{
		Title: "Ready work", Status: domain.TaskReady, Priority: "high", BudgetMinutes: 10,
	}); err != nil {
		t.Fatalf("create ready task: %v", err)
	}
	if _, err := appStore.CreateTask(ctx, domain.CreateTaskInput{
		Title: "Backlog work", Status: domain.TaskBacklog, Priority: "low", BudgetMinutes: 10,
	}); err != nil {
		t.Fatalf("create backlog task: %v", err)
	}

	filteredRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?status=ready", nil)
	filteredResponse := httptest.NewRecorder()
	handler.ServeHTTP(filteredResponse, filteredRequest)
	if filteredResponse.Code != http.StatusOK {
		t.Fatalf("filtered status = %d", filteredResponse.Code)
	}
	var tasks []domain.Task
	if err := json.NewDecoder(filteredResponse.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode filtered tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Status != domain.TaskReady {
		t.Fatalf("filtered tasks = %#v", tasks)
	}
	if filteredResponse.Header().Get("X-Request-ID") == "" {
		t.Fatal("generated X-Request-ID is empty")
	}

	invalidRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?status=unknown", nil)
	invalidRequest.Header.Set("X-Request-ID", "contract-error-1")
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid status = %d, want %d", invalidResponse.Code, http.StatusUnprocessableEntity)
	}
	var apiError apiErrorResponse
	if err := json.NewDecoder(invalidResponse.Body).Decode(&apiError); err != nil {
		t.Fatalf("decode API error: %v", err)
	}
	if apiError.Code != "invalid_input" || apiError.Retryable || apiError.RequestID != "contract-error-1" {
		t.Fatalf("API error = %#v", apiError)
	}
	if apiError.Error == "" || apiError.Error != apiError.Message {
		t.Fatalf("compatibility error alias = %#v", apiError)
	}
}

func newTestHandler(t *testing.T) http.Handler {
	_, handler := newTestStoreAndHandler(t)
	return handler
}

func newTestStoreAndHandler(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	appStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "appdeck-http-test.db"))
	if err != nil {
		t.Fatalf("open AppDeck store: %v", err)
	}
	t.Cleanup(func() {
		if err := appStore.Close(); err != nil {
			t.Errorf("close AppDeck store: %v", err)
		}
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return appStore, New(appStore, logger).Handler()
}
