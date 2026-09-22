package workerapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/contextpack"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kubernetes"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestContextToolEnforcesFrozenAllowlistAndLiveRevocation(t *testing.T) {
	ctx := context.Background()
	j, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	now := time.Now().UTC()
	lease := now.Add(time.Minute)
	e, _, err := j.EnsureExecution(ctx, execution.NewExecution{ProjectID: "project", WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", LeaseExpiresAt: lease, Budget: execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 2, MaxTokens: 20000}})
	if err != nil {
		t.Fatal(err)
	}
	build := contextpack.BuildInput{ProjectID: "project", ExecutionID: e.ID, PolicyVersion: "test", SourceRevision: strings.Repeat("a", 64), HarnessVersion: analysis.HarnessWithContext, ToolVersions: map[string]string{"worker": "pinned"}}
	for _, kind := range []contextpack.Kind{contextpack.KindTaskSnapshot, contextpack.KindPolicy, contextpack.KindSourceRevision, contextpack.KindTool, contextpack.KindExternal} {
		build.Inputs = append(build.Inputs, contextpack.Input{Kind: kind, URI: "context://" + string(kind), Content: "frozen input", Digest: artifacts.Digest([]byte("frozen input")), Trust: contextpack.TrustUntrusted, Scope: "project", ObservedAt: now, ExpiresAt: lease})
	}
	manifest, err := contextpack.Build(now, build)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = j.RecordContextPackage(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	spec := runtimeapi.Specification{IdempotencyKey: "tool-attempt", Workload: analysis.Kind, InputDigest: strings.Repeat("b", 64), ProfileDigest: strings.Repeat("c", 64), WorkerImage: "analysis@sha256:" + strings.Repeat("d", 64), ContextURI: "journal://input", ContextDigest: manifest.Digest, HarnessVersion: analysis.HarnessWithContext, Tools: []string{"read_context_source"}, Limits: runtimeapi.Limits{CPUMillis: 1000, MemoryBytes: 1024, EphemeralStorageBytes: 1024, WallTimeSeconds: 60, MaxProcesses: 64}, NetworkPolicy: "gateway-only", FilesystemPolicy: "ephemeral", AuthorityFence: 1, DeadlineAt: lease, PolicyVersion: "test"}
	e, a, _, err := j.EnsureRuntimeAttempt(ctx, e.ID, e.Version, spec)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := artifacts.New(t.TempDir())
	key := []byte(strings.Repeat("k", 32))
	p := &provider{run: workcontrol.Run{ID: "run", WorkItemID: "task", AgentID: "agent", TaskVersion: 1, Version: 1, FenceToken: 1, Status: workcontrol.RunRunning, LeaseExpiresAt: &lease}}
	revoked := false
	server := New(Config{SigningKey: key, ContextCheck: func(context.Context, execution.Execution) error {
		if revoked {
			return errors.New("revoked")
		}
		return nil
	}}, j, store, p, nil)
	request := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/workers/"+a.ID+"/tools/read-context-source", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+kubernetes.Token(key, a.Specification))
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	w := request(`{"uri":"context://external"}`)
	if w.Code != 200 {
		t.Fatalf("allowed: %d %s", w.Code, w.Body.String())
	}
	var source contextpack.Input
	if json.Unmarshal(w.Body.Bytes(), &source) != nil || source.Content != "frozen input" || source.Trust != "untrusted" {
		t.Fatalf("source: %s", w.Body.String())
	}
	bare := httptest.NewRequest("POST", "/workers/"+a.ID+"/tools/read-context-source", strings.NewReader(`{"uri":"context://external"}`))
	bare.Header.Set("Authorization", kubernetes.Token(key, a.Specification))
	w = httptest.NewRecorder()
	server.ServeHTTP(w, bare)
	if w.Code != 401 {
		t.Fatalf("missing Bearer scheme: %d", w.Code)
	}
	for _, body := range []string{`{"uri":"https://attacker.invalid"}`, `{"uri":"context://policy"}`, `{"uri":"context://external","url":"https://attacker.invalid"}`} {
		w = request(body)
		if w.Code != 400 && w.Code != 403 {
			t.Fatalf("unsafe args: %d", w.Code)
		}
	}
	revoked = true
	w = request(`{"uri":"context://external"}`)
	if w.Code != 403 {
		t.Fatalf("revoked: %d", w.Code)
	}
	revoked = false
	expired := now.Add(-time.Minute)
	p.run.LeaseExpiresAt = &expired
	w = request(`{"uri":"context://external"}`)
	if w.Code != 403 {
		t.Fatalf("expired lease: %d", w.Code)
	}
	p.run.LeaseExpiresAt = &lease
	p.run.Status = workcontrol.RunCancelled
	w = request(`{"uri":"context://external"}`)
	if w.Code != 403 {
		t.Fatalf("cancelled: %d", w.Code)
	}
	decisions := 0
	for {
		outbox, err := j.ListReadyOutbox(ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(outbox) == 0 {
			break
		}
		for _, item := range outbox {
			decisions++
			if err = j.MarkOutboxPublished(ctx, item.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	raw, err := j.ExportDiagnostics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics struct {
		ToolDecisions []struct {
			AttemptID string `json:"attempt_id"`
		} `json:"tool_decisions"`
		RuntimeAttempts []struct {
			Workload string `json:"workload"`
		} `json:"runtime_attempts"`
	}
	if err = json.Unmarshal(raw, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if len(diagnostics.ToolDecisions) < 2 || diagnostics.ToolDecisions[0].AttemptID != a.ID || diagnostics.RuntimeAttempts[0].Workload != analysis.Kind {
		t.Fatal("diagnostics lack correlated workload/tool decisions")
	}
	if decisions < 2 {
		t.Fatalf("missing durable tool decisions: %d", decisions)
	}
}
