package main

import (
	"context"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectGatewayRoutesByDurableAttemptAndPreservesCredential(t *testing.T) {
	ctx := context.Background()
	journal, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	now := time.Now().UTC()
	ids := map[string]string{}
	for _, project := range []string{"first", "second"} {
		binding, err := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: project, Provider: "deck", Repository: "test://" + project, Runtime: "test", PolicyVersion: "test", ContextConfig: "test", ArtifactConfig: "test", DeliveryConfig: "test", BudgetMinutes: 10, MaxConcurrency: 1})
		if err != nil {
			t.Fatal(err)
		}
		e, _, err := journal.EnsureExecution(ctx, execution.NewExecution{ProjectID: project, BindingVersion: binding.Version, WorkProvider: "deck", ExternalWorkID: "task-" + project, ExternalRunID: "run-" + project, AgentID: "agent", LeaseExpiresAt: now.Add(time.Minute), Budget: execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 3, MaxTokens: 10000}})
		if err != nil {
			t.Fatal(err)
		}
		e, err = journal.Transition(ctx, e.ID, execution.Transition{ExpectedVersion: e.Version, Target: execution.StateRunning, Message: "Fixture"})
		if err != nil {
			t.Fatal(err)
		}
		_, attempt, _, err := journal.EnsureRuntimeAttempt(ctx, e.ID, e.Version, runtimeapi.Specification{IdempotencyKey: "job-" + project, RepositoryURI: "https://github.com/example/" + project + ".git", SourceRevision: strings.Repeat("a", 40), ContextURI: "journal://context", ContextDigest: "sha256:context", HarnessVersion: "test", Limits: runtimeapi.Limits{CPUMillis: 1000, MemoryBytes: 1024, EphemeralStorageBytes: 1024, WallTimeSeconds: 60, MaxProcesses: 64}, NetworkPolicy: "restricted", FilesystemPolicy: "ephemeral", AuthorityFence: 1, DeadlineAt: now.Add(time.Minute), PolicyVersion: "test"})
		if err != nil {
			t.Fatal(err)
		}
		ids[project] = attempt.ID
	}
	handlers := map[string]http.Handler{}
	for _, project := range []string{"first", "second"} {
		handlers[project] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer fixture-token" {
				t.Error("routing replaced credential")
			}
			w.Write([]byte(project))
		})
	}
	gateway := projectGateway(journal, handlers, nil, handlers["first"])
	for project, id := range ids {
		r := httptest.NewRequest("GET", "/workers/"+id+"/input", nil)
		r.Header.Set("Authorization", "Bearer fixture-token")
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.String() != project {
			t.Fatalf("wrong project gateway: %d %s", w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "/engine/input", nil)
	r.Header.Set("Authorization", "Bearer unknown."+strings.Repeat("0", 64))
	w := httptest.NewRecorder()
	gateway.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("unknown attempt forwarded: %d", w.Code)
	}
	delete(handlers, "second")
	r = httptest.NewRequest("GET", "/workers/"+ids["second"]+"/input", nil)
	r.Header.Set("Authorization", "Bearer fixture-token")
	w = httptest.NewRecorder()
	gateway.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("removed project fell back to other gateway: %d", w.Code)
	}
}
