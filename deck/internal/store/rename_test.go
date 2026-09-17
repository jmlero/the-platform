package store

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestReopenLedgerAfterProductRenamePreservesWork(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "deck.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Retain this task", Status: domain.TaskBacklog})
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce the migration metadata and default project in an existing installation.
	for _, query := range []string{
		`UPDATE schema_migrations SET version = '006_appdeck_ledger.sql' WHERE version = '006_deck_ledger.sql'`,
		`DELETE FROM schema_migrations WHERE version = '007_deck_name.sql'`,
		`UPDATE projects SET name = 'AppDeck', slug = 'appdeck' WHERE id = 'prj_default'`,
	} {
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var title, name, slug string
	if err := s.db.QueryRowContext(ctx, `SELECT title FROM tasks WHERE id = ?`, task.ID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != task.Title {
		t.Fatalf("task changed: %q", title)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT name, slug FROM projects WHERE id = 'prj_default'`).Scan(&name, &slug); err != nil {
		t.Fatal(err)
	}
	if name != "Deck" || slug != "deck" {
		t.Fatalf("project = %q / %q", name, slug)
	}
}

func TestImportPreviousExportEmitsCurrentSchema(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Retained export", Status: domain.TaskBacklog}); err != nil {
		t.Fatal(err)
	}
	current, err := s.ExportProject(ctx, defaultProjectID)
	if err != nil {
		t.Fatal(err)
	}
	previous := bytes.Replace(current, []byte(`"deck-export/v1"`), []byte(`"appdeck-export/v1"`), 1)
	target, err := Open(ctx, filepath.Join(t.TempDir(), "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if err := target.ImportProject(ctx, previous); err != nil {
		t.Fatal(err)
	}
	actual, err := target.ExportProject(ctx, defaultProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, current) {
		t.Fatal("previous export did not round-trip to the current schema")
	}
}
