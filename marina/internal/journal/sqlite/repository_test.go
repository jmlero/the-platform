package sqlite

import (
	"context"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"path/filepath"
	"testing"
)

func TestRepositoryBindingChangeIsDurableIdempotentAndRetainsHistory(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "journal.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	b, err := s.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "p", Provider: "deck", Repository: "https://github.com/a/one.git", Runtime: "test", PolicyVersion: "p1", ContextConfig: "cfg", ArtifactConfig: "a", DeliveryConfig: "github", BudgetMinutes: 1, MaxConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	original := workcontrol.RepositoryBinding{Version: b.Version, Repository: "a/one", CloneURL: b.Repository, BaseBranch: "main"}
	if err = s.RecordRepositorySnapshot(ctx, "p", "template", original); err != nil {
		t.Fatal(err)
	}
	changed := workcontrol.RepositoryBinding{Repository: "a/two", CloneURL: "https://github.com/a/two.git", BaseBranch: "main"}
	result, err := s.ApplyRepositoryRequest(ctx, "p", 1, 1, "template", "cfg2", changed)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.RepositoryReceipt(ctx, "p", 1)
	if err != nil || replay.Binding.Version != result.Binding.Version {
		t.Fatal("lost-response recovery", err)
	}
	old, profile, err := s.RepositorySnapshot(ctx, "p", 1)
	if err != nil || old != original || profile != "template" {
		t.Fatal("old binding lost", err)
	}
}
