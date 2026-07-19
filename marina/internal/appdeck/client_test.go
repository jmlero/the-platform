package appdeck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func TestClientInitialLifecycleContract(t *testing.T) {
	createdAt := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	leaseExpiresAt := createdAt.Add(2 * time.Minute)
	calls := make([]string, 0, 5)

	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls = append(calls, request.Method+" "+request.URL.Path)

		switch request.Method + " " + request.URL.Path {
		case "GET /api/v1/provider":
			return jsonResponse(http.StatusOK, compatibleProviderInfo(createdAt)), nil
		case "POST /api/v1/agents":
			var input registerAgentRequest
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				return nil, err
			}
			if input.ID != "agent-marina" || input.Name != "Marina" || input.Kind != "factory" {
				t.Errorf("unexpected registration: %#v", input)
			}
			return jsonResponse(http.StatusCreated, agentDTO{
				ID:           input.ID,
				Name:         input.Name,
				Kind:         input.Kind,
				Status:       "idle",
				Capabilities: input.Capabilities,
				LastSeenAt:   createdAt.UnixMilli(),
				CreatedAt:    createdAt.UnixMilli(),
			}), nil
		case "GET /api/v1/tasks":
			if request.URL.Query().Get("status") != "ready" {
				t.Errorf("task status filter = %q", request.URL.Query().Get("status"))
			}
			return jsonResponse(http.StatusOK, []taskDTO{
				{ID: "task-backlog", Status: "backlog", Title: "Not ready"},
				{
					ID:                 "task-ready",
					ProjectID:          "project-1",
					Title:              "Ready work",
					Description:        "Implement a bounded change",
					AcceptanceCriteria: "The checks pass",
					Status:             "ready",
					Priority:           "high",
					BudgetMinutes:      30,
					Version:            3,
					CreatedAt:          createdAt.UnixMilli(),
					UpdatedAt:          createdAt.UnixMilli(),
				},
			}), nil
		case "POST /api/v1/tasks/task-ready/claim":
			var input claimRequest
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				return nil, err
			}
			if input.AgentID != "agent-marina" || input.LeaseSeconds != 2 {
				t.Errorf("unexpected claim: %#v", input)
			}
			leaseMillis := leaseExpiresAt.UnixMilli()
			return jsonResponse(http.StatusCreated, runDTO{
				ID:             "run-1",
				TaskID:         "task-ready",
				AgentID:        input.AgentID,
				Status:         "planning",
				LeaseExpiresAt: &leaseMillis,
				StartedAt:      createdAt.UnixMilli(),
				UpdatedAt:      createdAt.UnixMilli(),
			}), nil
		default:
			return jsonResponse(http.StatusNotFound, map[string]string{"error": "unexpected route"}), nil
		}
	})}

	client, err := New("http://appdeck.test", httpClient)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	ctx := context.Background()

	if err := client.Health(ctx); err != nil {
		t.Fatalf("health: %v", err)
	}
	agent, err := client.RegisterAgent(ctx, workcontrol.AgentRegistration{
		ID:           "agent-marina",
		Name:         "Marina",
		Kind:         "factory",
		Capabilities: []string{"work.claim"},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if agent.ID != "agent-marina" || agent.CreatedAt != createdAt {
		t.Fatalf("unexpected agent: %#v", agent)
	}

	work, err := client.ListReadyWork(ctx)
	if err != nil {
		t.Fatalf("list ready work: %v", err)
	}
	if len(work) != 1 || work[0].ID != "task-ready" || work[0].Status != workcontrol.WorkReady {
		t.Fatalf("unexpected ready work: %#v", work)
	}
	if work[0].AcceptanceCriteria != "The checks pass" || work[0].Version != 3 {
		t.Fatalf("ready work lost contract fields: %#v", work[0])
	}

	run, err := client.ClaimWork(ctx, work[0].ID, workcontrol.Claim{
		AgentID: agent.ID,
		Lease:   1500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("claim work: %v", err)
	}
	if run.ID != "run-1" || run.WorkItemID != work[0].ID || run.LeaseExpiresAt == nil {
		t.Fatalf("unexpected run: %#v", run)
	}
	if *run.LeaseExpiresAt != leaseExpiresAt {
		t.Fatalf("lease expiry = %s, want %s", *run.LeaseExpiresAt, leaseExpiresAt)
	}

	wantCalls := []string{
		"GET /api/v1/provider",
		"POST /api/v1/agents",
		"GET /api/v1/tasks",
		"POST /api/v1/tasks/task-ready/claim",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestClientSurfacesAPIError(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := jsonResponse(http.StatusConflict, map[string]any{
			"error":      "task already claimed",
			"code":       "conflict",
			"message":    "task already claimed",
			"retryable":  false,
			"request_id": "request-42",
		})
		response.Header.Set("X-Request-ID", "request-42")
		return response, nil
	})}
	client, err := New("http://appdeck.test", httpClient)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	_, err = client.ListReadyWork(context.Background())
	var apiError *APIError
	if !errors.As(err, &apiError) {
		t.Fatalf("error = %v, want APIError", err)
	}
	if apiError.StatusCode != http.StatusConflict || apiError.Code != "conflict" ||
		apiError.Message != "task already claimed" || apiError.Retryable || apiError.RequestID != "request-42" {
		t.Fatalf("unexpected API error: %#v", apiError)
	}
}

func TestCompatibilityReportsMissingCapabilityWithoutHidingProviderInfo(t *testing.T) {
	now := time.Date(2026, time.July, 18, 10, 0, 0, 0, time.UTC)
	info := compatibleProviderInfo(now)
	info.Capabilities = []string{workcontrol.CapabilityProviderInfo, workcontrol.CapabilityWorkInspect}
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("X-Request-ID") == "" {
			t.Error("Marina request correlation ID is empty")
		}
		return jsonResponse(http.StatusOK, info), nil
	})}
	client, err := New("http://appdeck.test", httpClient)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	observed, err := client.GetProviderInfo(context.Background())
	if err != nil {
		t.Fatalf("read-only provider inspection: %v", err)
	}
	if observed.Provider != "appdeck" || observed.ProtocolVersion != workcontrol.ProtocolVersion {
		t.Fatalf("provider info = %#v", observed)
	}
	_, err = client.CheckCompatibility(context.Background(), workcontrol.ReconciliationCapabilities())
	if !errors.Is(err, ErrIncompatibleProvider) {
		t.Fatalf("compatibility error = %v, want ErrIncompatibleProvider", err)
	}
	if !strings.Contains(err.Error(), workcontrol.CapabilityLeaseRenew) ||
		!strings.Contains(err.Error(), workcontrol.CapabilityRunBlock) {
		t.Fatalf("compatibility error does not name missing capabilities: %v", err)
	}
}

func TestCompatibilityDistinguishesDegradedReadiness(t *testing.T) {
	now := time.Date(2026, time.July, 18, 10, 0, 0, 0, time.UTC)
	info := compatibleProviderInfo(now)
	info.Ready = false
	info.Readiness = map[string]string{"store": "unavailable"}
	client, err := New("http://appdeck.test", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, info), nil
	})})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	_, err = client.CheckCompatibility(context.Background(), workcontrol.ReconciliationCapabilities())
	if !errors.Is(err, ErrProviderNotReady) || errors.Is(err, ErrIncompatibleProvider) {
		t.Fatalf("readiness error = %v", err)
	}
}

func TestSupportedProtocolRange(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{version: "1.0", want: true},
		{version: "1.9", want: true},
		{version: "0.9", want: false},
		{version: "2.0", want: false},
		{version: "1", want: false},
		{version: "1.x", want: false},
		{version: "", want: false},
	}
	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			if got := compatibleProtocolVersion(test.version); got != test.want {
				t.Fatalf("compatibleProtocolVersion(%q) = %t, want %t", test.version, got, test.want)
			}
		})
	}
}

func TestClientReadsWorkDetailAndRenewsExistingRun(t *testing.T) {
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	leaseMillis := now.Add(time.Minute).UnixMilli()
	calls := make([]string, 0, 2)
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls = append(calls, request.Method+" "+request.URL.Path)
		switch request.Method + " " + request.URL.Path {
		case "GET /api/v1/tasks/task-1":
			return jsonResponse(http.StatusOK, taskDetailDTO{
				Task: taskDTO{ID: "task-1", Status: "running", Title: "Existing work"},
				Runs: []runDTO{{
					ID:             "run-1",
					TaskID:         "task-1",
					AgentID:        "marina-1",
					Status:         "running",
					LeaseExpiresAt: &leaseMillis,
				}},
			}), nil
		case "POST /api/v1/runs/run-1/heartbeat":
			var input claimRequest
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				return nil, err
			}
			if input.AgentID != "marina-1" || input.LeaseSeconds != 2 {
				t.Errorf("unexpected lease renewal: %#v", input)
			}
			return jsonResponse(http.StatusNoContent, nil), nil
		default:
			return jsonResponse(http.StatusNotFound, map[string]string{"error": "unexpected route"}), nil
		}
	})}
	client, err := New("http://appdeck.test", httpClient)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	detail, err := client.GetWork(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("get work: %v", err)
	}
	if detail.Work.ID != "task-1" || len(detail.Runs) != 1 || detail.Runs[0].Status != workcontrol.RunRunning {
		t.Fatalf("unexpected work detail: %#v", detail)
	}
	if err := client.RenewRunLease(context.Background(), "run-1", workcontrol.LeaseRenewal{
		AgentID: "marina-1",
		Lease:   1500 * time.Millisecond,
	}); err != nil {
		t.Fatalf("renew run lease: %v", err)
	}
	wantCalls := []string{
		"GET /api/v1/tasks/task-1",
		"POST /api/v1/runs/run-1/heartbeat",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestClientPublishesProgressAndBlocksRun(t *testing.T) {
	calls := make([]string, 0, 2)
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls = append(calls, request.Method+" "+request.URL.Path)
		switch request.URL.Path {
		case "/api/v1/runs/run-1/progress":
			var input progressRequest
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				return nil, err
			}
			if input.Message != "checkpoint saved" || input.Payload["phase"] != "implementation" {
				t.Errorf("unexpected progress: %#v", input)
			}
		case "/api/v1/runs/run-1/block":
			var input blockRequest
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				return nil, err
			}
			if input.Reason != "execution budget exhausted" {
				t.Errorf("unexpected block: %#v", input)
			}
		default:
			return jsonResponse(http.StatusNotFound, map[string]string{"error": "unexpected route"}), nil
		}
		return jsonResponse(http.StatusNoContent, nil), nil
	})}
	client, err := New("http://appdeck.test", httpClient)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := client.ReportRunProgress(context.Background(), "run-1", workcontrol.Progress{
		Message: " checkpoint saved ",
		Payload: map[string]any{"phase": "implementation"},
	}); err != nil {
		t.Fatalf("report progress: %v", err)
	}
	if err := client.BlockRun(context.Background(), "run-1", " execution budget exhausted "); err != nil {
		t.Fatalf("block run: %v", err)
	}
	wantCalls := []string{
		"POST /api/v1/runs/run-1/progress",
		"POST /api/v1/runs/run-1/block",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func jsonResponse(status int, payload any) *http.Response {
	var body bytes.Buffer
	_ = json.NewEncoder(&body).Encode(payload)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body.Bytes())),
	}
}

func compatibleProviderInfo(now time.Time) providerInfoDTO {
	return providerInfoDTO{
		Provider:        "appdeck",
		ProtocolVersion: workcontrol.ProtocolVersion,
		Capabilities: []string{
			workcontrol.CapabilityProviderInfo,
			workcontrol.CapabilityAgentRegister,
			workcontrol.CapabilityWorkReadyList,
			workcontrol.CapabilityWorkInspect,
			workcontrol.CapabilityWorkClaim,
			workcontrol.CapabilityLeaseRenew,
			workcontrol.CapabilityPlanSubmit,
			workcontrol.CapabilityRunStart,
			workcontrol.CapabilityProgress,
			workcontrol.CapabilityRunVerify,
			workcontrol.CapabilityRunComplete,
			workcontrol.CapabilityRunBlock,
		},
		ServerTime: now.UnixMilli(),
		Ready:      true,
		Readiness:  map[string]string{"store": "available"},
	}
}

func TestNewRejectsUnsafeServerURLs(t *testing.T) {
	tests := []string{
		"",
		"localhost:8080",
		"ftp://localhost:8080",
		"http://user:pass@localhost:8080",
		"http://localhost:8080?token=secret",
		"http://localhost:8080#fragment",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := New(input, nil); err == nil {
				t.Fatal("expected invalid URL error")
			}
		})
	}
}

func TestClaimValidatesBeforeCallingAppDeck(t *testing.T) {
	client, err := New("http://localhost:8080", nil)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	tests := []struct {
		name   string
		workID string
		claim  workcontrol.Claim
	}{
		{name: "missing work ID", claim: workcontrol.Claim{AgentID: "agent", Lease: time.Second}},
		{name: "unsafe work ID", workID: "task/one", claim: workcontrol.Claim{AgentID: "agent", Lease: time.Second}},
		{name: "missing agent", workID: "task-one", claim: workcontrol.Claim{Lease: time.Second}},
		{name: "short lease", workID: "task-one", claim: workcontrol.Claim{AgentID: "agent", Lease: time.Millisecond}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := client.ClaimWork(context.Background(), test.workID, test.claim); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestExistingRunOperationsValidateBeforeCallingAppDeck(t *testing.T) {
	client, err := New("http://localhost:8080", nil)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if _, err := client.GetWork(context.Background(), "task/one"); err == nil {
		t.Fatal("unsafe work ID was accepted")
	}
	tests := []struct {
		name    string
		runID   string
		renewal workcontrol.LeaseRenewal
	}{
		{name: "missing run", renewal: workcontrol.LeaseRenewal{AgentID: "agent", Lease: time.Second}},
		{name: "unsafe run", runID: "run/one", renewal: workcontrol.LeaseRenewal{AgentID: "agent", Lease: time.Second}},
		{name: "missing agent", runID: "run-one", renewal: workcontrol.LeaseRenewal{Lease: time.Second}},
		{name: "short lease", runID: "run-one", renewal: workcontrol.LeaseRenewal{AgentID: "agent", Lease: time.Millisecond}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := client.RenewRunLease(context.Background(), test.runID, test.renewal); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if err := client.ReportRunProgress(context.Background(), "run-one", workcontrol.Progress{}); err == nil {
		t.Fatal("empty progress was accepted")
	}
	if err := client.BlockRun(context.Background(), "run-one", " "); err == nil {
		t.Fatal("empty block reason was accepted")
	}
}
