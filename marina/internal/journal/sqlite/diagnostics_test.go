package sqlite_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
)

func TestDiagnosticExportIsDeterministicAndRedacted(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	store := openStore(t, ctx, ":memory:", &now)
	t.Cleanup(func() { _ = store.Close() })
	record, _, err := store.EnsureExecution(ctx, validExecution(now, "run-diagnostics"))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = store.SaveCheckpoint(ctx, record.ID, record.Version, execution.CheckpointInput{
		Key: "diagnostic", Phase: "testing", SourceRevision: "abc123",
		Payload: map[string]any{
			"api_token": "super-secret-value",
			"endpoint":  "https://user:password@example.test/path?token=also-secret",
			"message":   "Authorization=Bearer-private",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := store.ExportDiagnostics(ctx)
	if err != nil {
		t.Fatalf("export diagnostics: %v", err)
	}
	second, err := store.ExportDiagnostics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("export is not deterministic:\n%s\n%s", first, second)
	}
	text := string(first)
	for _, forbidden := range []string{"super-secret-value", "also-secret", "Bearer-private", "user:password"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("diagnostic export leaked %q: %s", forbidden, text)
		}
	}
	var exported struct {
		SchemaVersion string         `json:"schema_version"`
		Counts        map[string]int `json:"counts"`
		Executions    []any          `json:"executions"`
		Checkpoints   []struct {
			Payload map[string]any `json:"payload"`
		} `json:"checkpoints"`
	}
	if err := json.Unmarshal(first, &exported); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if exported.SchemaVersion != "marina-diagnostics/v1" || exported.Counts["executions"] != 1 ||
		len(exported.Executions) != 1 || len(exported.Checkpoints) != 1 ||
		exported.Checkpoints[0].Payload["api_token"] != "[REDACTED]" {
		t.Fatalf("unexpected export: %#v", exported)
	}
}
