package workerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kubernetes"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func TestDeniedModelBudgetLeavesOneDurableExplanation(t *testing.T) {
	for _, workload := range []string{"repository", "analysis"} {
		t.Run(workload, func(t *testing.T) {
			for _, limit := range []string{"token", "cost"} {
				t.Run(limit, func(t *testing.T) {
					ctx := context.Background()
					journal, err := sqlite.Open(ctx, ":memory:")
					if err != nil {
						t.Fatal(err)
					}
					defer journal.Close()
					now := time.Now().UTC()
					lease := now.Add(time.Minute)
					budget := execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 2, MaxTokens: 100000, MaxCostMicros: 100000}
					if limit == "token" {
						budget.MaxTokens = 1
					} else {
						budget.MaxCostMicros = 1
					}
					e, _, err := journal.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", LeaseExpiresAt: lease, Budget: budget})
					if err != nil {
						t.Fatal(err)
					}
					e, attempt, _, err := journal.EnsureRuntimeAttempt(ctx, e.ID, e.Version, gatewayWorkloadSpec(workload, runtimeapi.Specification{IdempotencyKey: "job", RepositoryURI: "https://git.example/repo.git", SourceRevision: strings.Repeat("a", 40), ContextURI: "journal://context", ContextDigest: "sha256:context", HarnessVersion: "test", Limits: runtimeapi.Limits{CPUMillis: 1000, MemoryBytes: 1024, EphemeralStorageBytes: 1024, WallTimeSeconds: 60, MaxProcesses: 64}, NetworkPolicy: "restricted", FilesystemPolicy: "ephemeral", AuthorityFence: 1, DeadlineAt: lease, PolicyVersion: "test"}))
					if err != nil {
						t.Fatal(err)
					}
					input, _ := json.Marshal(gatewayWorkloadInput(workload, attempt.Specification))
					_, _, _, err = journal.SaveCheckpoint(ctx, e.ID, e.Version, execution.CheckpointInput{Key: "worker-input:" + attempt.ID, Phase: "worker_input", Payload: map[string]any{"input": string(input)}})
					if err != nil {
						t.Fatal(err)
					}
					key := []byte(strings.Repeat("s", 32))
					p := &provider{run: workcontrol.Run{ID: "run", WorkItemID: "task", AgentID: "agent", TaskVersion: 1, Version: 1, FenceToken: 1, Status: workcontrol.RunRunning, LeaseExpiresAt: &lease}}
					calls := 0
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
					defer upstream.Close()
					s := New(Config{SigningKey: key, Model: Model{Endpoint: upstream.URL, Name: "fixture", MaxOutputTokens: 100, InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000}}, journal, nil, p, nil)
					for range 2 {
						r := httptest.NewRequest("POST", "/workers/"+attempt.ID+"/model/v1/responses", strings.NewReader(`{"input":"bounded task"}`))
						r.Header.Set("Authorization", "Bearer "+kubernetes.Token(key, attempt.Specification))
						w := httptest.NewRecorder()
						s.ServeHTTP(w, r)
						if w.Code != 429 {
							t.Fatalf("denied model request returned %d: %s", w.Code, w.Body.String())
						}
					}
					items, err := journal.ListReadyOutbox(ctx, 20)
					if err != nil || len(items) != 1 || items[0].Payload["type"] != "model.budget_exhausted" || !strings.Contains(items[0].Payload["message"].(string), limit+" budget") {
						t.Fatalf("missing durable %s denial: %+v, %v", limit, items, err)
					}
					if len(items[0].IdempotencyKey) > 128 || strings.ContainsAny(items[0].IdempotencyKey, " \t\r\n") {
						t.Fatalf("denial cannot be published through the provider contract: %q", items[0].IdempotencyKey)
					}
					if calls != 0 {
						t.Fatalf("denied requests reached model: %d", calls)
					}
					tokens, cost, err := journal.ModelUsage(ctx, attempt.ID)
					if err != nil || tokens != 0 || cost != 0 {
						t.Fatalf("denial consumed budget: %d %d %v", tokens, cost, err)
					}
				})
			}
		})
	}
}
