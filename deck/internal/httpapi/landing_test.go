package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/store"
)

func TestLandedCompletionReleasesDependenciesWithoutHumanAcceptance(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "landing.db")
	s, err := store.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := New(s, logger).Handler()
	request, err := s.RequestRepository(ctx, "prj_default", domain.RepositoryRequestInput{Repository: "example/repo", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RecordRepositoryResult(ctx, "prj_default", domain.RepositoryResult{RequestRevision: request.Revision, Status: "connected", Binding: &domain.RepositoryBinding{Repository: "example/repo", BaseBranch: "main", CloneURL: "https://github.com/example/repo.git", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	budget := domain.Budget{TimeMinutes: 30, MaxAttempts: 3, Authority: []string{"source.read", "source.write", "source.fast_forward"}}
	task, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Land a prerequisite", AcceptanceCriteria: "Tests and review pass", Status: domain.TaskReady, Budget: &budget})
	if err != nil {
		t.Fatal(err)
	}
	dependent, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Use the prerequisite"})
	if err != nil {
		t.Fatal(err)
	}
	dependent, err = s.SetTaskDependencies(ctx, dependent.ID, dependent.Version, []string{task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.MoveTask(ctx, dependent.ID, domain.TaskReady); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RegisterAgent(ctx, domain.Agent{ID: "landing-agent", Name: "Landing agent"}); err != nil {
		t.Fatal(err)
	}
	run, err := s.ClaimTask(ctx, store.Mutation{ActorID: "service:test", IdempotencyKey: "landing-claim"}, task.ID, "landing-agent", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SubmitPlan(ctx, store.Mutation{ActorID: "service:test", IdempotencyKey: "landing-plan"}, run.ID, "Review, test and land", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BeginVerification(ctx, store.Mutation{ActorID: "service:test", IdempotencyKey: "landing-verify"}, run.ID); err != nil {
		t.Fatal(err)
	}
	run, err = s.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("a", 40)
	ids := []string{}
	for _, kind := range []string{"test", "semantic_review", "landing"} {
		item, err := s.RecordEvidence(ctx, domain.EvidenceInput{ID: "landing-" + kind, RunID: run.ID, PlanRevisionID: *run.ActivePlanRevisionID, Type: kind, Producer: "builder", Verifier: "independent-" + kind, SubjectDigest: commit, SourceRevision: commit, Command: kind, Outcome: "passed", ProvenanceURI: "artifact://" + kind, ProvenanceDigest: "sha256:" + kind, ArtifactURI: "artifact://" + kind, ArtifactDigest: "sha256:" + kind, ObservedAt: time.Now().UnixMilli()})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, item.ID)
	}
	run, err = s.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"agent_id": run.AgentID, "expected_version": run.Version, "fence_token": run.FenceToken, "summary": "Verified commit landed", "evidence_ids": ids, "landing": map[string]any{"repository": "example/repo", "base_branch": "main", "commit": commit, "observed_at": time.Now().UnixMilli()}}
	for _, scenario := range []string{"wrong commit", "wrong repository", "missing landing evidence"} {
		t.Run(scenario, func(t *testing.T) {
			bad := map[string]any{}
			for k, v := range body {
				bad[k] = v
			}
			landing := map[string]any{}
			for k, v := range body["landing"].(map[string]any) {
				landing[k] = v
			}
			bad["landing"] = landing
			switch scenario {
			case "wrong commit":
				landing["commit"] = strings.Repeat("b", 40)
			case "wrong repository":
				landing["repository"] = "other/repo"
			case "missing landing evidence":
				bad["evidence_ids"] = ids[:2]
			}
			response := requestJSON(t, handler, http.MethodPost, "/api/v1/runs/"+run.ID+"/complete", "bad-"+scenario, bad)
			if response.Code == http.StatusNoContent {
				t.Fatal("unproved landing completed a task")
			}
		})
	}
	for i := 0; i < 2; i++ {
		response := requestJSON(t, handler, http.MethodPost, "/api/v1/runs/"+run.ID+"/complete", "landing-complete", body)
		if response.Code != http.StatusNoContent {
			t.Fatalf("landed completion: %d %s", response.Code, response.Body.String())
		}
		if i == 0 {
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = store.Open(ctx, database)
			if err != nil {
				t.Fatal(err)
			}
			handler = New(s, logger).Handler()
		}
	}
	changed := make(map[string]any, len(body))
	for key, value := range body {
		changed[key] = value
	}
	changed["landing"] = map[string]any{"repository": "example/repo", "base_branch": "main", "commit": strings.Repeat("b", 40), "observed_at": time.Now().UnixMilli()}
	if response := requestJSON(t, handler, http.MethodPost, "/api/v1/runs/"+run.ID+"/complete", "landing-complete", changed); response.Code != http.StatusConflict {
		t.Fatalf("changed landing replay accepted: %d %s", response.Code, response.Body.String())
	}
	task, err = s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(task)
	var wire map[string]any
	json.Unmarshal(encoded, &wire)
	if task.Status != domain.TaskDone || task.Review != nil || wire["landing"] == nil {
		t.Fatalf("landing lost or recorded as human acceptance: %s", encoded)
	}
	next, err := s.NextReadyTask(ctx)
	if err != nil || next.ID != dependent.ID {
		t.Fatalf("landed prerequisite still needs operator confirmation: %+v %v", next, err)
	}
	if _, err = s.ClaimTask(ctx, store.Mutation{ActorID: "service:test", IdempotencyKey: "landing-dependent"}, dependent.ID, "landing-agent", time.Hour); err != nil {
		t.Fatal(err)
	}
	bundle, err := s.ExportProject(ctx, task.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := store.Open(ctx, filepath.Join(t.TempDir(), "restore.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.ImportProject(ctx, bundle); err != nil {
		t.Fatal(err)
	}
	copy, err := restored.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	copyJSON, _ := json.Marshal(copy)
	var copyWire map[string]any
	json.Unmarshal(copyJSON, &copyWire)
	if copyWire["landing"] == nil {
		t.Fatal("export/import lost landing")
	}
}
