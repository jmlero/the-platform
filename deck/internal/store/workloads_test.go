package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestWorkloadInvocationIsPinnedAndReplaysAfterRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "deck.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	p := domain.WorkloadProfile{ID: "diagnostic-analysis/v1", Digest: strings.Repeat("a", 64), Harness: "langchain-create-agent/v2", Authority: []string{"analysis.run." + strings.Repeat("a", 64)}, MaxTokens: 20000, BudgetMinutes: 5, MinAttempts: 1, Tools: []string{}, ContextKinds: []string{"document"}}
	catalog := domain.WorkloadCatalog{Profiles: []domain.WorkloadProfile{p}, ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}
	if err = s.PublishWorkloads(ctx, "prj_default", "marina", catalog); err != nil {
		t.Fatal(err)
	}
	in := domain.WorkloadInvocation{ProfileID: p.ID, ProfileDigest: p.Digest, Task: domain.CreateTaskInput{Title: "Analyze", AcceptanceCriteria: "Validated report", Budget: &domain.Budget{TimeMinutes: 5, MaxAttempts: 1, MaxTokens: 10000, MaxConcurrency: 1, Authority: p.Authority}}}
	mutation, _ := NewMutation("operator:test", "invoke-once")
	task, err := s.InvokeWorkload(ctx, "prj_default", mutation, in)
	if err != nil {
		t.Fatal(err)
	}
	if task.Workload == nil || task.Workload.Digest != p.Digest || task.Status != domain.TaskReady {
		t.Fatalf("unpinned task: %#v", task)
	}
	s.Close()
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Withdrawal stops new work, but cannot hide a successful lost response.
	catalog.Profiles = []domain.WorkloadProfile{}
	if err = s.PublishWorkloads(ctx, "prj_default", "marina", catalog); err != nil {
		t.Fatal(err)
	}
	replay, err := s.InvokeWorkload(ctx, "prj_default", mutation, in)
	if err != nil || replay.ID != task.ID {
		t.Fatalf("replay: %#v %v", replay, err)
	}
	in.Task.Title = "Changed"
	if _, err = s.InvokeWorkload(ctx, "prj_default", mutation, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed payload: %v", err)
	}
	mutation.IdempotencyKey = "new-invocation"
	if _, err = s.InvokeWorkload(ctx, "prj_default", mutation, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("withdrawn profile: %v", err)
	}
	count, _ := s.TaskCount(ctx)
	if count != 1 {
		t.Fatalf("duplicate tasks: %d", count)
	}
}

func TestWorkloadCatalogRejectsStaleSelectionAndPublisherTakeover(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := domain.WorkloadProfile{ID: "repository/v1", Digest: strings.Repeat("b", 64), Harness: "codex/v1", Authority: []string{"source.read", "source.write", "pull_request.create"}, MaxTokens: 20000, BudgetMinutes: 5, MinAttempts: 2}
	catalog := domain.WorkloadCatalog{Profiles: []domain.WorkloadProfile{p}, ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}
	if err = s.PublishWorkloads(ctx, "prj_default", "marina", catalog); err != nil {
		t.Fatal(err)
	}
	if err = s.PublishWorkloads(ctx, "prj_default", "other", catalog); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("publisher takeover: %v", err)
	}
	in := domain.WorkloadInvocation{ProfileID: p.ID, ProfileDigest: strings.Repeat("c", 64), Task: domain.CreateTaskInput{Title: "Change repository", AcceptanceCriteria: "Verified PR", Budget: &domain.Budget{TimeMinutes: 5, MaxAttempts: 2, MaxTokens: 10000, MaxConcurrency: 1, Authority: p.Authority}}}
	mutation, _ := NewMutation("operator:test", "stale-profile")
	if _, err = s.InvokeWorkload(ctx, "prj_default", mutation, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale digest: %v", err)
	}
	in.ProfileDigest = p.Digest
	in.Task.Budget.MaxTokens = p.MaxTokens + 1
	if _, err = s.InvokeWorkload(ctx, "prj_default", mutation, in); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("excess budget: %v", err)
	}
	in.Task.Budget.MaxTokens = 10000
	catalog.ExpiresAt = time.Now().Add(-time.Second).UnixMilli()
	if _, err = s.db.ExecContext(ctx, "UPDATE workload_catalogs SET catalog_json=? WHERE project_id=?", marshalJSON(catalog), "prj_default"); err != nil {
		t.Fatal(err)
	}
	found, err := s.GetWorkloads(ctx, "prj_default")
	if err != nil || len(found.Profiles) != 0 {
		t.Fatalf("expired discovery: %+v %v", found, err)
	}
	if _, err = s.InvokeWorkload(ctx, "prj_default", mutation, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expired invocation: %v", err)
	}
	if err = s.PublishWorkloads(ctx, "prj_default", "marina", catalog); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expired publication: %v", err)
	}
}

func TestWorkloadCatalogAcceptsRevisionedLangChainBYOWithAnalysisOnlyAuthority(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := domain.WorkloadProfile{ID: "diagnostic-analysis/v1", Digest: strings.Repeat("d", 64),
		Harness: "langchain-create-agent/kagent-byo-v1", Authority: []string{"analysis.run." + strings.Repeat("d", 64)},
		MaxTokens: 20000, BudgetMinutes: 5, MinAttempts: 1, Tools: []string{"read_context_source"}}
	catalog := domain.WorkloadCatalog{Profiles: []domain.WorkloadProfile{p}, ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}
	if err := s.PublishWorkloads(ctx, "prj_default", "marina", catalog); err != nil {
		t.Fatal(err)
	}
}
