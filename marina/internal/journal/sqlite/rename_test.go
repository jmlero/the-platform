package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
)

func TestProviderRenameRetainsBindingAndExecutionIdentity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "marina.db")
	now := time.Now().UTC()
	s := openStore(t, ctx, path, &now)
	binding := execution.ProjectBinding{ProjectID: "project", Provider: "appdeck", Repository: "test://repo", Runtime: "test", PolicyVersion: "test", ContextConfig: "test", ArtifactConfig: "test", DeliveryConfig: "test", BudgetMinutes: 10, MaxConcurrency: 1}
	before, err := s.EnsureProjectBinding(ctx, binding)
	if err != nil {
		t.Fatal(err)
	}
	input := validExecution(now, "rename-run")
	input.WorkProvider = "appdeck"
	input.ProjectID = binding.ProjectID
	record, _, err := s.EnsureExecution(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version = '009_deck_provider.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	binding.Provider = "deck"
	after, err := s.EnsureProjectBinding(ctx, binding)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version {
		t.Fatalf("rename revised binding: %d -> %d", before.Version, after.Version)
	}
	input.WorkProvider = "deck"
	replayed, created, err := s.EnsureExecution(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if created || replayed.ID != record.ID || replayed.Version != record.Version {
		t.Fatalf("rename replaced execution: %#v", replayed)
	}
}
