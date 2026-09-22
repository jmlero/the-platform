package workerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kubernetes"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/worker"
)

func TestAnalysisGatewayDeniesGitAndBindsReportToFrozenInputs(t *testing.T) {
	ctx := context.Background()
	journal, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	now := time.Now().UTC()
	lease := now.Add(time.Minute)
	e, _, err := journal.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", LeaseExpiresAt: lease, Budget: execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 1, MaxTokens: 20000}})
	if err != nil {
		t.Fatal(err)
	}
	f := analysis.DiagnosticFixture()
	pin := strings.Repeat("a", 64)
	e, a, _, err := journal.EnsureRuntimeAttempt(ctx, e.ID, e.Version, runtimeapi.Specification{IdempotencyKey: "analyze", Workload: analysis.Kind, InputDigest: f.InputDigest(), ProfileDigest: pin, WorkerImage: "analysis@sha256:" + pin, ContextURI: "journal://input", ContextDigest: pin, HarnessVersion: analysis.Harness, Limits: runtimeapi.Limits{CPUMillis: 1000, MemoryBytes: 1024, EphemeralStorageBytes: 1024, WallTimeSeconds: 60, MaxProcesses: 64}, NetworkPolicy: "gateway-only", FilesystemPolicy: "ephemeral", AuthorityFence: 1, DeadlineAt: lease, PolicyVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	input := worker.Input{Stage: "analyze", Analysis: &analysis.Input{Version: 1, ProfileDigest: pin, InputDigest: f.InputDigest(), Sources: f.Sources}}
	b, _ := json.Marshal(input)
	_, _, _, err = journal.SaveCheckpoint(ctx, e.ID, e.Version, execution.CheckpointInput{Key: "worker-input:" + a.ID, Phase: "worker_input", Payload: map[string]any{"input": string(b)}})
	if err != nil {
		t.Fatal(err)
	}
	store, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("k", 32))
	p := &provider{run: workcontrol.Run{ID: "run", WorkItemID: "task", AgentID: "agent", TaskVersion: 1, Version: 1, FenceToken: 1, Status: workcontrol.RunRunning, LeaseExpiresAt: &lease}}
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed","usage":{"input_tokens":100,"output_tokens":50}}`))
	}))
	defer upstream.Close()
	server := New(Config{SigningKey: key, SourceURL: upstream.URL, PublicURL: "https://marina.internal", Model: Model{Endpoint: upstream.URL, Name: "fixture", MaxOutputTokens: 1000}}, journal, store, p, nil)
	request := func(method, path string, b []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/workers/"+a.ID+path, strings.NewReader(string(b)))
		r.Header.Set("Authorization", "Bearer "+kubernetes.Token(key, a.Specification))
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/git/info/refs?service=git-upload-pack", nil); w.Code != 403 {
		t.Fatalf("Git read returned %d", w.Code)
	}
	if w := request("POST", "/model/v1/responses", []byte(`{"input":"synthetic diagnostics","tools":[{"type":"function","name":"shell"}]}`)); w.Code != 403 {
		t.Fatalf("analysis shell tool returned %d", w.Code)
	}
	if calls != 0 {
		t.Fatal("denied analysis calls reached an upstream")
	}
	if w := request("POST", "/model/v1/responses", []byte(`{"input":"synthetic diagnostics","tools":[{"type":"function","name":"DiagnosticReport","parameters":{"type":"object"}}]}`)); w.Code != 200 {
		t.Fatalf("model returned %d: %s", w.Code, w.Body.String())
	}
	tokens, _, err := journal.ModelUsage(ctx, a.ID)
	if err != nil || tokens != 150 {
		t.Fatalf("usage=%d: %v", tokens, err)
	}
	result := worker.Result{Stage: "analyze", ProfileDigest: pin, InputDigest: f.InputDigest(), Analysis: &analysis.Report{Summary: "Synthetic diagnosis", Findings: f.Expected}, ObservedAt: now}
	result.ProfileDigest = strings.Repeat("b", 64)
	b, _ = json.Marshal(result)
	if w := request("POST", "/result", b); w.Code != 409 {
		t.Fatalf("mismatched profile returned %d", w.Code)
	}
	result.ProfileDigest = pin
	b, _ = json.Marshal(result)
	for range 2 {
		if w := request("POST", "/result", b); w.Code != 200 {
			t.Fatalf("report returned %d: %s", w.Code, w.Body.String())
		}
	}
	result.Analysis.Summary = "changed"
	b, _ = json.Marshal(result)
	if w := request("POST", "/result", b); w.Code != 409 {
		t.Fatalf("changed report returned %d", w.Code)
	}
}
