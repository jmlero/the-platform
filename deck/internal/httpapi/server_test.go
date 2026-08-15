package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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

func TestOpenAPIDeclaresIdempotencyForEveryClaimAndRunMutation(t *testing.T) {
	contract, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read OpenAPI contract: %v", err)
	}
	const reference = `$ref: "#/components/parameters/IdempotencyKey"`
	if count := strings.Count(string(contract), reference); count != 10 {
		t.Fatalf("Idempotency-Key operation references = %d, want 10", count)
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

func TestReadinessReflectsStoreAndServiceAuthorizationHealth(t *testing.T) {
	appStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "appdeck-readiness.db"))
	if err != nil {
		t.Fatalf("open AppDeck store: %v", err)
	}
	policyPath := filepath.Join(t.TempDir(), "service-auth.json")
	writeServiceAuthPolicy(
		t,
		policyPath,
		serviceAuthTokenOne,
		[]string{capabilityProviderInfo},
		[]string{"prj_default"},
	)
	authorizer, err := NewFileServiceAuthorizer(policyPath)
	if err != nil {
		t.Fatalf("create service authorizer: %v", err)
	}
	handler := New(
		appStore,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithServiceAuthorizer(authorizer),
	).Handler()

	readiness := func() (int, map[string]string) {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		var payload struct {
			Status    string            `json:"status"`
			Readiness map[string]string `json:"readiness"`
		}
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			t.Fatalf("decode readiness: %v", err)
		}
		if response.Code == http.StatusOK && payload.Status != "ready" {
			t.Fatalf("healthy readiness status = %q", payload.Status)
		}
		if response.Code == http.StatusServiceUnavailable && payload.Status != "degraded" {
			t.Fatalf("degraded readiness status = %q", payload.Status)
		}
		return response.Code, payload.Readiness
	}

	status, checks := readiness()
	if status != http.StatusOK || checks["store"] != "available" || checks["service_auth"] != "available" {
		t.Fatalf("healthy readiness = status %d, checks %#v", status, checks)
	}
	if err := os.WriteFile(policyPath, []byte(`{"version":1,"identities":[]}`), 0o600); err != nil {
		t.Fatalf("replace service authorization policy: %v", err)
	}
	status, checks = readiness()
	if status != http.StatusServiceUnavailable || checks["service_auth"] != "unavailable" {
		t.Fatalf("authorization readiness = status %d, checks %#v", status, checks)
	}
	writeServiceAuthPolicy(
		t,
		policyPath,
		serviceAuthTokenOne,
		[]string{capabilityProviderInfo},
		[]string{"prj_default"},
	)
	if err := appStore.Close(); err != nil {
		t.Fatalf("close AppDeck store: %v", err)
	}
	status, checks = readiness()
	if status != http.StatusServiceUnavailable || checks["store"] != "unavailable" {
		t.Fatalf("store readiness = status %d, checks %#v", status, checks)
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

func TestClaimAPIReplaysOriginalResultAndRejectsChangedPayload(t *testing.T) {
	appStore, handler := newTestStoreAndHandler(t)
	ctx := context.Background()
	if _, err := appStore.RegisterAgent(ctx, domain.Agent{ID: "agt_http_idempotent", Name: "HTTP agent"}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	task, err := appStore.CreateTask(ctx, domain.CreateTaskInput{
		Title: "Replay HTTP claim", Status: domain.TaskReady, Priority: "high", BudgetMinutes: 10,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	claim := func(key string, leaseSeconds int) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/tasks/"+task.ID+"/claim",
			strings.NewReader(fmt.Sprintf(`{"agent_id":"agt_http_idempotent","expected_task_version":%d,"lease_seconds":%d}`, task.Version, leaseSeconds)),
		)
		request.Header.Set("Idempotency-Key", key)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	first := claim("claim-http-replay", 60)
	if first.Code != http.StatusCreated {
		t.Fatalf("initial claim status = %d, body = %s", first.Code, first.Body.String())
	}
	var original domain.Run
	if err := json.NewDecoder(first.Body).Decode(&original); err != nil {
		t.Fatalf("decode initial claim: %v", err)
	}
	replay := claim("claim-http-replay", 60)
	if replay.Code != http.StatusCreated {
		t.Fatalf("replayed claim status = %d, body = %s", replay.Code, replay.Body.String())
	}
	var replayed domain.Run
	if err := json.NewDecoder(replay.Body).Decode(&replayed); err != nil {
		t.Fatalf("decode replayed claim: %v", err)
	}
	if !reflect.DeepEqual(replayed, original) {
		t.Fatalf("replayed claim = %#v, want %#v", replayed, original)
	}

	changed := claim("claim-http-replay", 90)
	assertAPIError(t, changed, http.StatusConflict, errorCodeConflict)
	invalid := claim("invalid key", 60)
	assertAPIError(t, invalid, http.StatusUnprocessableEntity, errorCodeInvalidInput)
	detail, err := appStore.GetTaskDetail(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task detail: %v", err)
	}
	if len(detail.Runs) != 1 {
		t.Fatalf("claim API created %d runs, want 1", len(detail.Runs))
	}
	claimedEvents, err := appStore.CountRunEvents(ctx, original.ID, "run.claimed")
	if err != nil {
		t.Fatalf("count claim events: %v", err)
	}
	if claimedEvents != 1 {
		t.Fatalf("claim API created %d claim events, want 1", claimedEvents)
	}
}

func TestServiceAPIAuthenticatesCapabilityAndProjectScopes(t *testing.T) {
	appStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "appdeck-auth-http-test.db"))
	if err != nil {
		t.Fatalf("open AppDeck store: %v", err)
	}
	t.Cleanup(func() {
		if err := appStore.Close(); err != nil {
			t.Errorf("close AppDeck store: %v", err)
		}
	})
	task, err := appStore.CreateTask(context.Background(), domain.CreateTaskInput{
		Title: "Scoped work", Status: domain.TaskReady, Priority: "high", BudgetMinutes: 10,
	})
	if err != nil {
		t.Fatalf("create scoped task: %v", err)
	}
	policyPath := filepath.Join(t.TempDir(), "service-auth.json")
	writeServiceAuthPolicy(t, policyPath, serviceAuthTokenOne, []string{capabilityProviderInfo}, []string{"prj_default"})
	authorizer, err := NewFileServiceAuthorizer(policyPath)
	if err != nil {
		t.Fatalf("create service authorizer: %v", err)
	}
	handler := New(
		appStore,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithServiceAuthorizer(authorizer),
	).Handler()

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/provider", nil))
	assertAPIError(t, unauthenticated, http.StatusUnauthorized, errorCodeUnauthenticated)
	if unauthenticated.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("unauthenticated response omitted WWW-Authenticate")
	}

	authorizedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/provider", nil)
	authorizedRequest.Header.Set("Authorization", "Bearer "+serviceAuthTokenOne)
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized provider status = %d", authorized.Code)
	}

	forbiddenRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?status=ready", nil)
	forbiddenRequest.Header.Set("Authorization", "Bearer "+serviceAuthTokenOne)
	forbidden := httptest.NewRecorder()
	handler.ServeHTTP(forbidden, forbiddenRequest)
	assertAPIError(t, forbidden, http.StatusForbidden, errorCodeForbidden)

	writeServiceAuthPolicy(t, policyPath, serviceAuthTokenOne, []string{capabilityWorkReadyList}, []string{"prj_other"})
	projectRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?status=ready", nil)
	projectRequest.Header.Set("Authorization", "Bearer "+serviceAuthTokenOne)
	projectResponse := httptest.NewRecorder()
	handler.ServeHTTP(projectResponse, projectRequest)
	if projectResponse.Code != http.StatusOK {
		t.Fatalf("project-filtered status = %d", projectResponse.Code)
	}
	var tasks []domain.Task
	if err := json.NewDecoder(projectResponse.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode project-filtered tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("project scope leaked tasks: %#v", tasks)
	}

	writeServiceAuthPolicy(t, policyPath, serviceAuthTokenOne, []string{capabilityWorkClaim}, []string{"prj_other"})
	claimRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/claim",
		strings.NewReader(`{"agent_id":"agt_other","lease_seconds":30}`),
	)
	claimRequest.Header.Set("Authorization", "Bearer "+serviceAuthTokenOne)
	claimResponse := httptest.NewRecorder()
	handler.ServeHTTP(claimResponse, claimRequest)
	assertAPIError(t, claimResponse, http.StatusForbidden, errorCodeForbidden)

	operatorResponse := httptest.NewRecorder()
	handler.ServeHTTP(
		operatorResponse,
		httptest.NewRequest(http.MethodGet, "/api/v1/operator/tasks/"+task.ID, nil),
	)
	if operatorResponse.Code != http.StatusOK {
		t.Fatalf("local operator task status = %d", operatorResponse.Code)
	}

	if err := os.WriteFile(policyPath, []byte(`{"version":1,"identities":[]}`), 0o600); err != nil {
		t.Fatalf("replace service authorization policy: %v", err)
	}
	unavailableRequest := httptest.NewRequest(http.MethodGet, "/api/v1/provider", nil)
	unavailableRequest.Header.Set("Authorization", "Bearer "+serviceAuthTokenOne)
	unavailableResponse := httptest.NewRecorder()
	handler.ServeHTTP(unavailableResponse, unavailableRequest)
	assertAPIError(t, unavailableResponse, http.StatusServiceUnavailable, errorCodeServiceUnavailable)
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
