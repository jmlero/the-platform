package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewerSchemaRefusesAnUnsafeDowngrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "future.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "INSERT INTO schema_migrations(version,applied_at) VALUES('999_future.sql',1)"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(ctx, path)
	if err == nil {
		opened.Close()
		t.Fatal("newer schema accepted by older binary")
	}
	if !strings.Contains(err.Error(), "schema") {
		t.Fatal(err)
	}
}
