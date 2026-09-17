package workerapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kubernetes"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/worker"
)

type credentialTransport func(*http.Request) (*http.Response, error)

func (f credentialTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGatewayCredentialRotationAndEmptyFilesFailClosed(t *testing.T) {
	for _, mode := range []string{"model", "source"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			journal, err := sqlite.Open(ctx, ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			now, lease := time.Now().UTC(), time.Now().UTC().Add(time.Minute)
			record, _, err := journal.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", LeaseExpiresAt: lease, Budget: execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 3, MaxTokens: 10000}})
			if err != nil {
				t.Fatal(err)
			}
			record, err = journal.Transition(ctx, record.ID, execution.Transition{ExpectedVersion: record.Version, Target: execution.StateRunning, Message: "test"})
			if err != nil {
				t.Fatal(err)
			}
			spec := runtimeapi.Specification{IdempotencyKey: "job", RepositoryURI: "https://git.example/owner/repo.git", SourceRevision: strings.Repeat("a", 40), ContextURI: "journal://context", ContextDigest: "sha256:context", HarnessVersion: "test", Limits: runtimeapi.Limits{CPUMillis: 1000, MemoryBytes: 1024, EphemeralStorageBytes: 1024, WallTimeSeconds: 60, MaxProcesses: 64}, NetworkPolicy: "restricted", FilesystemPolicy: "ephemeral", AuthorityFence: 1, DeadlineAt: lease, PolicyVersion: "test"}
			record, attempt, _, err := journal.EnsureRuntimeAttempt(ctx, record.ID, record.Version, spec)
			if err != nil {
				t.Fatal(err)
			}
			input, _ := json.Marshal(worker.Input{Stage: "implement", SourceRevision: spec.SourceRevision})
			_, _, _, err = journal.SaveCheckpoint(ctx, record.ID, record.Version, execution.CheckpointInput{Key: "worker-input:" + attempt.ID, Phase: "worker_input", Payload: map[string]any{"input": string(input)}})
			if err != nil {
				t.Fatal(err)
			}
			store, err := artifacts.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			key := []byte(strings.Repeat("s", 32))
			p := &provider{run: workcontrol.Run{ID: "run", WorkItemID: "task", AgentID: "agent", TaskVersion: 1, Version: 1, FenceToken: 1, Status: workcontrol.RunRunning, LeaseExpiresAt: &lease}}
			path := filepath.Join(t.TempDir(), "token")
			want, calls := "", 0
			client := &http.Client{Transport: credentialTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				header := "Bearer " + want
				if mode == "source" {
					header = "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+want))
				}
				if r.Header.Get("Authorization") != header {
					t.Error("wrong rotated credential")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"usage":{"input_tokens":2,"output_tokens":1}}`))}, nil
			})}
			server := New(Config{SigningKey: key, SourceURL: spec.RepositoryURI, SourceTokenFile: path, Model: Model{Endpoint: "https://model.example/v1", Name: "fixture", TokenFile: path, MaxOutputTokens: 10}}, journal, store, p, client)
			for _, token := range []string{"first-private-token", "replacement-private-token", ""} {
				want = token
				if err := os.WriteFile(path, []byte(token), 0600); err != nil {
					t.Fatal(err)
				}
				method, route := http.MethodPost, "/model/v1/responses"
				if mode == "source" {
					method, route = http.MethodGet, "/git/info/refs?service=git-upload-pack"
				}
				r := httptest.NewRequest(method, "/workers/"+attempt.ID+route, strings.NewReader(`{"input":"hello"}`))
				r.Header.Set("Authorization", "Bearer "+kubernetes.Token(key, attempt.Specification))
				w := httptest.NewRecorder()
				server.ServeHTTP(w, r)
				expected := 200
				if token == "" {
					expected = 503
				}
				if w.Code != expected {
					t.Fatalf("status=%d want=%d", w.Code, expected)
				}
				if strings.Contains(w.Body.String(), "private-token") {
					t.Fatal("credential exposed to worker")
				}
			}
			if calls != 2 {
				t.Fatalf("upstream requests=%d, want 2", calls)
			}
			if mode == "model" {
				tokens, _, err := journal.ModelUsage(ctx, attempt.ID)
				if err != nil || tokens != 6 {
					t.Fatalf("empty credential reserved model budget: tokens=%d err=%v", tokens, err)
				}
			}
		})
	}
}
