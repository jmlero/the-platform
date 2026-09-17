package workerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

type provider struct{ run workcontrol.Run }

func (p *provider) GetWork(context.Context, string) (workcontrol.WorkDetail, error) {
	return workcontrol.WorkDetail{Runs: []workcontrol.Run{p.run}}, nil
}

func TestWorkerCredentialCannotOutliveProviderFence(t *testing.T) {
	ctx := context.Background()
	journal, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	now := time.Now().UTC()
	lease := now.Add(time.Minute)
	record, _, err := journal.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", LeaseExpiresAt: lease, Budget: execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 3, MaxTokens: 10000}})
	if err != nil {
		t.Fatal(err)
	}
	record, err = journal.Transition(ctx, record.ID, execution.Transition{ExpectedVersion: record.Version, Target: execution.StateRunning, Message: "start test"})
	if err != nil {
		t.Fatal(err)
	}
	spec := runtimeapi.Specification{IdempotencyKey: "job", RepositoryURI: "https://git.example/owner/repo.git", SourceRevision: strings.Repeat("a", 40), ContextURI: "https://marina.example/context", ContextDigest: "sha256:context", HarnessVersion: "test", Limits: runtimeapi.Limits{CPUMillis: 1000, MemoryBytes: 1024, EphemeralStorageBytes: 1024, WallTimeSeconds: 60, MaxProcesses: 64}, NetworkPolicy: "restricted", FilesystemPolicy: "ephemeral", AuthorityFence: 1, DeadlineAt: now.Add(time.Minute), PolicyVersion: "test"}
	record, attempt, _, err := journal.EnsureRuntimeAttempt(ctx, record.ID, record.Version, spec)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(worker.Input{Stage: "verify", SourceRevision: spec.SourceRevision})
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
	server := New(Config{PublicURL: "https://marina.example", SigningKey: key}, journal, store, p, nil)
	request := func() int {
		r := httptest.NewRequest(http.MethodGet, "/workers/"+attempt.ID+"/input", nil)
		r.Header.Set("Authorization", "Bearer "+kubernetes.Token(key, attempt.Specification))
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w.Code
	}
	if status := request(); status != 200 {
		t.Fatalf("valid worker returned %d", status)
	}
	p.run.FenceToken = 2
	if status := request(); status != 403 {
		t.Fatalf("revoked worker returned %d", status)
	}
}

func TestModelToolsAllowOnlyLocalExecution(t *testing.T) {
	if localTools([]any{map[string]any{"type": "computer_use_preview"}}) {
		t.Fatal("remote provider tool accepted")
	}
	if localTools([]any{map[string]any{"type": "namespace", "tools": []any{map[string]any{"type": "web_search"}}}}) {
		t.Fatal("nested remote provider tool accepted")
	}
	if !localTools([]any{map[string]any{"type": "function", "name": "exec_command"}, map[string]any{"type": "custom", "name": "apply_patch"}}) {
		t.Fatal("coding harness local tools rejected")
	}
}
