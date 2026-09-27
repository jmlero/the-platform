package workerapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func engineFixture(t *testing.T, limits ...int64) (*Server, rt.Attempt, *provider, func()) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "journal.db")
	j, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	lease := time.Now().Add(time.Minute)
	maxTokens := int64(10000)
	if len(limits) > 0 {
		maxTokens = limits[0]
	}
	maxCost := int64(0)
	if len(limits) > 1 {
		maxCost = limits[1]
	}
	e, _, err := j.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", LeaseExpiresAt: lease, Budget: execution.Budget{DeadlineAt: lease.Add(time.Hour), MaxAttempts: 2, MaxTokens: maxTokens, MaxCostMicros: maxCost}})
	if err != nil {
		t.Fatal(err)
	}
	spec := rt.Specification{IdempotencyKey: "engine", RepositoryURI: "https://git.example/repo.git", SourceRevision: strings.Repeat("a", 40), ContextURI: "artifact://sha256:" + strings.Repeat("b", 64), ContextDigest: "sha256:" + strings.Repeat("b", 64), ProfileDigest: strings.Repeat("c", 64), HarnessVersion: "test", Limits: rt.Limits{CPUMillis: 1, MemoryBytes: 1, EphemeralStorageBytes: 1, WallTimeSeconds: 60, MaxProcesses: 1}, NetworkPolicy: "restricted", FilesystemPolicy: "ephemeral", AuthorityFence: 1, DeadlineAt: lease, PolicyVersion: "test", Engine: &rt.EngineSelection{Backend: "kagent", Version: "pinned", Endpoint: "engine", Namespace: "trial", Harness: "codex", Template: "attempt", Revision: strings.Repeat("d", 64)}}
	spec.CredentialReferences = []rt.CredentialReference{{Kind: "engine-gateway", Reference: "secret://trial/attempt/gateway", ExpiresAt: lease}}
	e, a, _, err := j.EnsureRuntimeAttempt(ctx, e.ID, e.Version, spec)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(gatewayWorkloadInput("repository", a.Specification))
	if _, _, _, err = j.SaveCheckpoint(ctx, e.ID, e.Version, execution.CheckpointInput{Key: "worker-input:" + a.ID, Phase: "worker_input", Payload: map[string]any{"input": string(input)}}); err != nil {
		t.Fatal(err)
	}
	bindEngineForCalls(t, j, a.Specification)
	key := []byte(strings.Repeat("k", 32))
	p := &provider{run: workcontrol.Run{ID: "run", WorkItemID: "task", AgentID: "agent", TaskVersion: 1, Version: 1, FenceToken: 1, Status: workcontrol.RunRunning, LeaseExpiresAt: &lease}}
	s := New(Config{SigningKey: key, ContextCheck: func(context.Context, execution.Execution) error { return nil }}, j, nil, p, nil)

	restart := func() {
		j.Close()
		j, err = sqlite.Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		s.journal = j
	}
	return s, a, p, restart
}

func TestEngineCancellationRevokesCachedCredentialBeforeTermination(t *testing.T) {
	ctx := context.Background()
	s, a, _, restart := engineFixture(t)
	credential, err := s.EngineCredential(ctx, a.Specification)
	if err != nil {
		t.Fatal(err)
	}
	request := func() int { return engineRequest(s, "GET", "/engine/input", credential, "").Code }
	if got := request(); got != 200 {
		t.Fatalf("active: %d", got)
	}
	engine, err := s.journal.GetEngineAttempt(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	engine.CancelRequested = true
	if _, err = s.journal.UpdateEngineAttempt(ctx, engine); err != nil {
		t.Fatal(err)
	}
	restart()
	if got := request(); got != 403 {
		t.Fatalf("cached credential after cancellation and restart: %d", got)
	}
	if _, err = s.journal.ReserveModelCall(ctx, a.ExecutionID, a.ID, 1, 10, 0); !errors.Is(err, execution.ErrConflict) {
		t.Fatalf("reservation after cancellation: %v", err)
	}
}

func engineRequest(s *Server, method, path, credential, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.TLS = &tls.ConnectionState{}
	r.Header.Set("Authorization", "Bearer "+credential)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestEngineNativeRouteRequiresTLSAndAttemptCredential(t *testing.T) {
	// Unknown credentials and unencrypted requests must fail before parsing model
	// data or consulting any upstream. This route is the Substrate destination.
	s := New(Config{}, nil, nil, nil, nil)
	for _, tc := range []struct {
		tls  bool
		auth string
		want int
	}{
		{false, "Bearer attempt.token", 403}, {true, "", 401}, {true, "Bearer inert-placeholder", 401},
	} {
		r := httptest.NewRequest("POST", "/engine/model/v1/responses", strings.NewReader(`{"input":"hello"}`))
		if tc.tls {
			r.TLS = &tls.ConnectionState{}
		}
		r.Header.Set("Authorization", tc.auth)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("TLS=%v status=%d want=%d", tc.tls, w.Code, tc.want)
		}
	}
}

func TestEngineAdmissionAndEveryRouteUseCurrentAuthority(t *testing.T) {
	s, a, p, _ := engineFixture(t)
	ctx := context.Background()
	token, err := s.EngineCredential(ctx, a.Specification)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		mutate  func()
		restore func()
		want    int
	}{
		{"fence", func() { p.run.FenceToken++ }, func() { p.run.FenceToken-- }, 403},
		{"cancelled", func() { p.run.Status = workcontrol.RunCancelled }, func() { p.run.Status = workcontrol.RunRunning }, 403},
		{"provider outage", func() { p.err = errors.New("outage") }, func() { p.err = nil }, 503},
		{"context revoked", func() {
			s.config.ContextCheck = func(context.Context, execution.Execution) error { return errors.New("revoked") }
		}, func() { s.config.ContextCheck = func(context.Context, execution.Execution) error { return nil } }, 403},
		{"no context validator", func() { s.config.ContextCheck = nil }, func() { s.config.ContextCheck = func(context.Context, execution.Execution) error { return nil } }, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.mutate()
			defer tc.restore()
			if err := s.AdmitEngine(ctx, a.Specification); err == nil {
				t.Fatal("dispatch admitted")
			}
			for _, route := range []struct{ method, path, body string }{
				{"GET", "/engine/input", ""}, {"GET", "/engine/authority", ""},
				{"POST", "/engine/model/v1/responses", `{"input":"hello"}`},
				{"GET", "/engine/git/info/refs?service=git-upload-pack", ""},
				{"POST", "/engine/tools/read-context-source", `{"uri":"context://external"}`},
			} {
				w := engineRequest(s, route.method, route.path, token, route.body)
				if w.Code != tc.want {
					t.Fatalf("%s status=%d want=%d", route.path, w.Code, tc.want)
				}
			}
		})
	}
	changed := a.Specification
	changed.AuthorityFence++
	if _, err := s.EngineCredential(ctx, changed); err == nil {
		t.Fatal("issued credential for changed specification")
	}
	for _, route := range []string{"/engine/model/v1/chat/completions", "/engine/model/v1/responses/prior", "/engine/git/git-receive-pack", "/engine/memory", "/engine/delegate", "/engine/tools/exec"} {
		w := engineRequest(s, "POST", route, token, `{}`)
		if w.Code != 404 {
			t.Fatalf("bypass %s: %d", route, w.Code)
		}
	}
	if w := engineRequest(s, "GET", "/engine/input", a.ID+"."+strings.Repeat("0", 64), ""); w.Code != 403 {
		t.Fatalf("wrong MAC: %d", w.Code)
	}
}

func TestEngineResponsesOverTLSRotateCredentialsAndRetainUnknownRetryBudget(t *testing.T) {
	s, a, _, restart := engineFixture(t)
	ctx := context.Background()
	token, err := s.EngineCredential(ctx, a.Specification)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "upstream-token")
	calls := 0
	want := "first-upstream-secret"
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.TLS == nil || r.Header.Get("Authorization") != "Bearer "+want {
			t.Error("upstream TLS/credential")
		}
		var payload map[string]any
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["model"] != "pinned" || payload["store"] != false || payload["max_output_tokens"] != float64(100) {
			t.Error("model bounds were not enforced")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls <= 2 {
			fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n")
		} else {
			fmt.Fprint(w, "data: {\"type\":\"response.in_progress\"}\n\n")
		}
	}))
	defer upstream.Close()
	s.client = upstream.Client()
	s.config.Model = Model{Endpoint: upstream.URL, Name: "pinned", TokenFile: path, MaxOutputTokens: 100, InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000}
	gateway := httptest.NewTLSServer(s)
	defer gateway.Close()
	for _, secret := range []string{want, "rotated-upstream-secret"} {
		want = secret
		if err := os.WriteFile(path, []byte(secret), 0600); err != nil {
			t.Fatal(err)
		}
		r, err := http.NewRequest("POST", gateway.URL+"/engine/model/v1/responses", strings.NewReader(`{"model":"override","store":true,"stream":true,"input":"hello","tools":[{"type":"function","name":"exec_command","parameters":{"type":"object"}}]}`))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		res, err := gateway.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 || !strings.Contains(string(body), "response.completed") {
			t.Fatalf("TLS streaming: %d %v", res.StatusCode, err)
		}
		if strings.Contains(string(body), secret) {
			t.Fatal("secret leaked")
		}
	}
	// An incomplete stream and each harness retry receive separate reservations.
	for range 2 {
		if w := engineRequest(s, "POST", "/engine/model/v1/responses", token, `{"input":"hello","stream":true}`); w.Code != 200 {
			t.Fatalf("retry: %d", w.Code)
		}
	}
	restart()
	totals, err := s.journal.ExecutionModelTotals(ctx, a.ExecutionID)
	if err != nil || totals.Tokens != 6 || totals.PendingCalls != 2 || totals.ReservedTokens < 2000 || totals.ReservedCostMicros != totals.ReservedTokens {
		t.Fatalf("restart usage: %+v %v", totals, err)
	}
	for range 20 {
		w := engineRequest(s, "POST", "/engine/model/v1/responses", token, `{"input":"hello","stream":true}`)
		if w.Code == 429 {
			break
		}
	}
	before := calls
	if w := engineRequest(s, "POST", "/engine/model/v1/responses", token, `{"input":"hello"}`); w.Code != 429 || calls != before {
		t.Fatalf("exhausted budget: %d", w.Code)
	}
	totals, err = s.journal.ExecutionModelTotals(ctx, a.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.journal.ReserveModelCall(ctx, a.ExecutionID, a.ID, 1, 10000-totals.Tokens-totals.ReservedTokens, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitEngine(ctx, a.Specification); err == nil {
		t.Fatal("dispatch ignored retained reservations")
	}
}

func TestEngineRejectsServerSideStateAndRemoteExecution(t *testing.T) {
	s, a, _, _ := engineFixture(t)
	token, err := s.EngineCredential(context.Background(), a.Specification)
	if err != nil {
		t.Fatal(err)
	}
	s.config.Model = Model{Endpoint: "https://unused.invalid", Name: "pinned", MaxOutputTokens: 100}
	for _, body := range []string{
		`{"input":"hello","background":true}`,
		`{"input":"hello","prompt":{"id":"unselected-prompt"}}`,
		`{"input":"hello","previous_response_id":"prior"}`,
		`{"input":"hello","conversation":"prior"}`,
		`{"input":"hello","tools":[{"type":"mcp","server_url":"https://other.invalid"}]}`,
		`{"input":"hello","tools":[{"type":"web_search"}]}`,
		`{"input":[{"type":"input_file","file_url":"https://other.invalid"}]}`,
		`{"input":"hello","unknown_future_field":true}`,
		`{"input":"hello","tools":[{"type":"web_search","external_web_access":true}],"tool_choice":"web_search"}`,
	} {
		w := engineRequest(s, "POST", "/engine/model/v1/responses", token, body)
		if w.Code != 400 {
			t.Fatalf("unsupported request status=%d", w.Code)
		}
	}
	totals, err := s.journal.ExecutionModelTotals(context.Background(), a.ExecutionID)
	if err != nil || totals.PendingCalls != 0 {
		t.Fatal("invalid request reached reservation")
	}
}

func bindEngineForCalls(t *testing.T, j *sqlite.Store, spec rt.Specification) {
	t.Helper()
	ctx := context.Background()
	engine, err := j.EnsureEngineAttempt(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	engine.InstanceID, engine.ContextID, engine.Route, engine.Revision, engine.Phase = "instance", "context", "route", spec.Engine.Revision, "ready"
	engine, err = j.UpdateEngineAttempt(ctx, engine)
	if err != nil {
		t.Fatal(err)
	}
	engine.Phase = "sending"
	if _, err = j.UpdateEngineAttempt(ctx, engine); err != nil {
		t.Fatal(err)
	}
}

func TestEngineGatewaySecretCannotBeReusedByAnotherAttempt(t *testing.T) {
	s, a, _, restart := engineFixture(t)
	ctx := context.Background()
	restart()
	e, err := s.journal.GetExecution(ctx, a.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	spec := a.Specification
	spec.IdempotencyKey = "another-attempt"
	if _, _, _, err = s.journal.EnsureRuntimeAttempt(ctx, e.ID, e.Version, spec); !errors.Is(err, execution.ErrConflict) {
		t.Fatalf("shared template accepted another attempt: %v", err)
	}
}

func TestEngineCancellationInterruptsAnActiveStream(t *testing.T) {
	s, a, _, _ := engineFixture(t)
	ctx := context.Background()
	token, err := s.EngineCredential(ctx, a.Specification)
	if err != nil {
		t.Fatal(err)
	}
	started, stopped := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.in_progress\"}\n\n")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer upstream.Close()
	s.client = upstream.Client()
	s.config.Model = Model{Endpoint: upstream.URL, Name: "pinned", MaxOutputTokens: 100}
	done := make(chan struct{})
	go func() {
		defer close(done)
		engineRequest(s, "POST", "/engine/model/v1/responses", token, `{"input":"hello","stream":true}`)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not start")
	}
	engine, err := s.journal.GetEngineAttempt(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	engine.CancelRequested = true
	if _, err = s.journal.UpdateEngineAttempt(ctx, engine); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(8 * time.Second):
		t.Fatal("revoked stream continued")
	}
	<-done
	totals, err := s.journal.ExecutionModelTotals(ctx, a.ExecutionID)
	if err != nil || totals.PendingCalls != 1 || totals.ReservedTokens < 1000 {
		t.Fatalf("interrupted usage: %+v %v", totals, err)
	}
}

func TestEngineSourceReadsStayBoundToRepositoryAndNeverPublish(t *testing.T) {
	s, a, _, _ := engineFixture(t)
	token, err := s.EngineCredential(context.Background(), a.Specification)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.config.SourceURL = a.Specification.RepositoryURI
	s.client = &http.Client{Transport: credentialTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != a.Specification.RepositoryURI+"/info/refs?service=git-upload-pack" {
			t.Error("source escaped original binding")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/x-git-upload-pack-advertisement"}}, Body: io.NopCloser(strings.NewReader("fixture refs"))}, nil
	})}
	if w := engineRequest(s, "GET", "/engine/git/info/refs?service=git-upload-pack", token, ""); w.Code != 200 {
		t.Fatalf("source read: %d", w.Code)
	}
	for _, path := range []string{"/engine/git/info/refs?service=git-receive-pack", "/engine/git/info/refs?service=git-upload-pack&url=https://other.invalid"} {
		if w := engineRequest(s, "GET", path, token, ""); w.Code != 403 {
			t.Fatalf("source bypass: %d", w.Code)
		}
	}
	s.config.SourceURL = "https://other.invalid/repo"
	if w := engineRequest(s, "GET", "/engine/git/info/refs?service=git-upload-pack", token, ""); w.Code != 409 {
		t.Fatalf("changed source binding: %d", w.Code)
	}
	if calls != 1 {
		t.Fatalf("source calls: %d", calls)
	}
}

func TestEngineSourceFetchKeepsTheCompressedRequestEncoding(t *testing.T) {
	// Git compresses large upload-pack requests, such as a full clone that
	// wants every advertised ref. Dropping the encoding corrupts the request.
	s, a, _, _ := engineFixture(t)
	token, err := s.EngineCredential(context.Background(), a.Specification)
	if err != nil {
		t.Fatal(err)
	}
	s.config.SourceURL = a.Specification.RepositoryURI
	s.client = &http.Client{Transport: credentialTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != a.Specification.RepositoryURI+"/git-upload-pack" || r.Header.Get("Content-Encoding") != "gzip" ||
			r.Header.Get("Accept") != "application/x-git-upload-pack-result" {
			return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader("malformed request"))}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/x-git-upload-pack-result"}}, Body: io.NopCloser(strings.NewReader("pack"))}, nil
	})}
	request := func(encoding string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/engine/git/git-upload-pack", strings.NewReader("compressed wants"))
		r.TLS = &tls.ConnectionState{}
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/x-git-upload-pack-request")
		r.Header.Set("Accept", "application/x-git-upload-pack-result")
		r.Header.Set("Content-Encoding", encoding)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := request("gzip"); w.Code != 200 || w.Body.String() != "pack" {
		t.Fatalf("compressed fetch: %d %q", w.Code, w.Body.String())
	}
	if w := request("br"); w.Code != 400 {
		t.Fatalf("unsupported request encoding forwarded: %d", w.Code)
	}
}

func TestNativeNormalizationRemovesSearchDeclarationButRejectsInvocation(t *testing.T) {
	for _, choice := range []any{"auto", map[string]any{"type": "web_search"}} {
		body := map[string]any{"client_metadata": map[string]any{"session": "untrusted metadata"}, "tools": []any{map[string]any{"type": "function", "name": "exec_command"}, map[string]any{"type": "web_search", "external_web_access": true}}, "tool_choice": choice}
		normalizeNativeModel(body)
		if _, ok := body["client_metadata"]; ok {
			t.Fatal("forwarded transport metadata")
		}
		if len(body["tools"].([]any)) != 1 || !localTools(body["tools"]) {
			t.Fatal("native remote search was forwarded")
		}
		_, explicit := choice.(map[string]any)
		if forbiddenModelInput(body) != explicit {
			t.Fatal("explicit remote search choice was accepted")
		}
	}
}

func TestEngineGatewayCredentialExpiresIndependentlyOfItsCache(t *testing.T) {
	s, a, _, _ := engineFixture(t)
	ctx := context.Background()
	e, err := s.journal.GetExecution(ctx, a.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	spec := a.Specification
	selected := *spec.Engine
	selected.Template = "expiring-attempt"
	spec.Engine = &selected
	spec.IdempotencyKey = "expiring-attempt"
	expires := time.Now().Add(2 * time.Second)
	spec.CredentialReferences = []rt.CredentialReference{{Kind: "engine-gateway", Reference: "secret://trial/expiring-attempt/gateway", ExpiresAt: expires}}
	_, a, _, err = s.journal.EnsureRuntimeAttempt(ctx, e.ID, e.Version, spec)
	if err != nil {
		t.Fatal(err)
	}
	bindEngineForCalls(t, s.journal, a.Specification)
	token, err := s.EngineCredential(ctx, a.Specification)
	if err != nil {
		t.Fatal(err)
	}
	w := engineRequest(s, "GET", "/engine/authority", token, "")
	var authority struct {
		ExpiresAt time.Time `json:"expires_at"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &authority) != nil || authority.ExpiresAt.After(expires) {
		t.Fatal("authority exceeds the credential expiry")
	}
	<-time.After(time.Until(expires) + 10*time.Millisecond)
	if w := engineRequest(s, "GET", "/engine/authority", token, ""); w.Code != 403 {
		t.Fatalf("expired cached credential: %d", w.Code)
	}
	if err := s.AdmitEngine(ctx, a.Specification); err == nil {
		t.Fatal("expired credential admitted dispatch")
	}
	if _, err = s.journal.ReserveModelCall(ctx, a.ExecutionID, a.ID, 1, 10, 0); !errors.Is(err, execution.ErrConflict) {
		t.Fatalf("expired credential reserved a call: %v", err)
	}
}

func TestEngineCostBudgetDeniesBeforeUpstream(t *testing.T) {
	s, a, _, _ := engineFixture(t, 1000000, 1)
	token, err := s.EngineCredential(context.Background(), a.Specification)
	if err != nil {
		t.Fatal(err)
	}
	s.config.Model = Model{Endpoint: "https://unused.invalid", Name: "pinned", MaxOutputTokens: 100, InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000}
	for range 2 {
		if w := engineRequest(s, "POST", "/engine/model/v1/responses", token, `{"input":"hello"}`); w.Code != 429 {
			t.Fatalf("cost budget: %d", w.Code)
		}
	}
	totals, err := s.journal.ExecutionModelTotals(context.Background(), a.ExecutionID)
	if err != nil || totals.PendingCalls != 0 {
		t.Fatal("denied cost acquired a reservation")
	}
}
