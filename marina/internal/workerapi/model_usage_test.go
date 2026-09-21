package workerapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kubernetes"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/worker"
)

type interruptedModelBody struct{ io.Reader }

func (body interruptedModelBody) Read(p []byte) (int, error) {
	n, err := body.Reader.Read(p)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

func TestModelBudgetRequiresCompleteTerminalUsage(t *testing.T) {
	for _, tc := range []struct {
		name, payload              string
		stream, interrupted, final bool
	}{
		{name: "interrupted progress", stream: true, interrupted: true, payload: `{"type":"response.in_progress","response":{"status":"in_progress","usage":{"input_tokens":2,"output_tokens":1}}}`},
		{name: "progress without terminal event", stream: true, payload: `{"type":"response.in_progress","response":{"usage":{"input_tokens":2,"output_tokens":1}}}`},
		{name: "missing counts", stream: true, payload: `{"type":"response.completed","response":{"status":"completed","usage":{}}}`},
		{name: "missing output count", stream: true, payload: `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":2}}}`},
		{name: "completed stream", stream: true, final: true, payload: `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":2,"output_tokens":1}}}`},
		{name: "terminal event before disconnect", stream: true, interrupted: true, final: true, payload: `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":2,"output_tokens":1}}}`},
		{name: "incomplete response with final usage", stream: true, final: true, payload: `{"type":"response.incomplete","response":{"status":"incomplete","usage":{"input_tokens":2,"output_tokens":1}}}`},
		{name: "failed response with final usage", stream: true, final: true, payload: `{"type":"response.failed","response":{"status":"failed","usage":{"input_tokens":2,"output_tokens":1}}}`},
		{name: "completed JSON", final: true, payload: `{"status":"completed","usage":{"input_tokens":2,"output_tokens":1}}`},
		{name: "nonterminal JSON", payload: `{"status":"in_progress","usage":{"input_tokens":2,"output_tokens":1}}`},
		{name: "unidentified JSON usage", payload: `{"usage":{"input_tokens":2,"output_tokens":1}}`},
		{name: "interrupted JSON", interrupted: true, payload: `{"status":"completed","usage":{"input_tokens":2,"output_tokens":1}}`},
		{name: "negative usage", payload: `{"status":"completed","usage":{"input_tokens":-2,"output_tokens":1}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "journal.db")
			journal, err := sqlite.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { journal.Close() }()
			now := time.Now().UTC()
			lease := now.Add(time.Minute)
			record, _, err := journal.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", LeaseExpiresAt: lease, Budget: execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 2, MaxTokens: 10000, MaxCostMicros: 10000}})
			if err != nil {
				t.Fatal(err)
			}
			record, attempt, _, err := journal.EnsureRuntimeAttempt(ctx, record.ID, record.Version, runtimeapi.Specification{IdempotencyKey: "job", RepositoryURI: "https://git.example/repo.git", SourceRevision: strings.Repeat("a", 40), ContextURI: "journal://context", ContextDigest: "sha256:context", HarnessVersion: "test", Limits: runtimeapi.Limits{CPUMillis: 1000, MemoryBytes: 1024, EphemeralStorageBytes: 1024, WallTimeSeconds: 60, MaxProcesses: 64}, NetworkPolicy: "restricted", FilesystemPolicy: "ephemeral", AuthorityFence: 1, DeadlineAt: lease, PolicyVersion: "test"})
			if err != nil {
				t.Fatal(err)
			}
			input, _ := json.Marshal(worker.Input{Stage: "implement", SourceRevision: attempt.Specification.SourceRevision})
			_, _, _, err = journal.SaveCheckpoint(ctx, record.ID, record.Version, execution.CheckpointInput{Key: "worker-input:" + attempt.ID, Phase: "worker_input", Payload: map[string]any{"input": string(input)}})
			if err != nil {
				t.Fatal(err)
			}
			key := []byte(strings.Repeat("s", 32))
			p := &provider{run: workcontrol.Run{ID: "run", WorkItemID: "task", AgentID: "agent", TaskVersion: 1, Version: 1, FenceToken: 1, Status: workcontrol.RunRunning, LeaseExpiresAt: &lease}}
			client := &http.Client{Transport: credentialTransport(func(r *http.Request) (*http.Response, error) {
				payload, contentType := tc.payload, "application/json"
				if tc.stream {
					payload, contentType = "data: "+payload+"\n\n", "text/event-stream"
				}
				var body io.Reader = strings.NewReader(payload)
				if tc.interrupted {
					body = interruptedModelBody{body}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(body)}, nil
			})}
			server := New(Config{SigningKey: key, Model: Model{Endpoint: "https://model.example/v1", Name: "fixture", MaxOutputTokens: 100, InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000}}, journal, nil, p, client)
			r := httptest.NewRequest("POST", "/workers/"+attempt.ID+"/model/v1/responses", strings.NewReader(`{"input":"hello"}`))
			r.Header.Set("Authorization", "Bearer "+kubernetes.Token(key, attempt.Specification))
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("model response: %d %s", w.Code, w.Body.String())
			}
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
			journal, err = sqlite.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			tokens, cost, err := journal.ModelUsage(ctx, attempt.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.final {
				if tokens != 3 || cost != 3 {
					t.Fatalf("terminal usage not recorded: tokens=%d cost=%d", tokens, cost)
				}
			} else if tokens <= 1000 || cost != tokens {
				t.Fatalf("unknown final usage released its reservation: tokens=%d cost=%d", tokens, cost)
			}
		})
	}
}
