package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
)

func TestArtifactRetentionCommandRequiresDryRunAndPreservesJournal(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "marina.db")
	directory := filepath.Join(root, "artifacts")
	ctx := context.Background()
	journal, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "p", Provider: "deck", Repository: "test://repo", Runtime: "test", PolicyVersion: "test", ContextConfig: "test", ArtifactConfig: directory, DeliveryConfig: "test", BudgetMinutes: 10, MaxConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	journal.Close()
	store, err := artifacts.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := store.Put([]byte("orphan"))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	name := filepath.Join(directory, strings.TrimPrefix(digest, "sha256:"))
	if err := os.Chtimes(name, old, old); err != nil {
		t.Fatal(err)
	}
	args := []string{"artifact-retention", "--journal-path", path, "--artifact-directory", directory}
	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d error=%s", code, &stderr)
	}
	if _, err := store.Get(digest); err != nil {
		t.Fatal("dry run deleted orphan", err)
	}
	plan := filepath.Join(root, "plan.json")
	if err := os.WriteFile(plan, stdout.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := run(append(args, "--apply-plan", plan), &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d error=%s", code, &stderr)
	}
	if _, err := store.Get(digest); !os.IsNotExist(err) {
		t.Fatalf("orphan remains: %v", err)
	}
	if !strings.Contains(stdout.String(), `"deleted":1`) {
		t.Fatalf("result=%s", &stdout)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
