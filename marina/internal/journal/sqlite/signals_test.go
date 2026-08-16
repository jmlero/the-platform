package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/signals"
)

func TestValidatedSignalsDeduplicateAcrossRestart(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "signals.db")
	store := openStore(t, ctx, path, &now)
	validator, err := signals.NewValidator(signals.Configuration{
		MaxAge: time.Hour, MaxRawBytes: 1024, CorrelationLabels: []string{"service"},
		Sources: map[string]signals.SourceRule{"monitor": {Types: []string{"error"}, Classifications: map[string]string{"error": "availability"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := signals.RawSignal{
		ID: "raw-1", Source: "monitor", ExternalID: "alert-1", ProjectID: "project", Type: "error",
		ObservedAt: now.Add(-time.Minute), ReceivedAt: now, Severity: "high", Confidence: .9,
		Labels: map[string]string{"service": "api"}, Text: "raw diagnostic text",
	}
	validated, err := validator.Validate(now, raw)
	if err != nil {
		t.Fatal(err)
	}
	stored, created, err := store.RecordValidatedSignal(ctx, raw, validated)
	if err != nil || !created || stored.DedupeKey != validated.DedupeKey {
		t.Fatalf("stored = %#v, created %t, err %v", stored, created, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, ctx, path, &now)
	t.Cleanup(func() { _ = store.Close() })
	duplicate := raw
	duplicate.ID = "raw-2"
	duplicate.ExternalID = "alert-2"
	duplicateValidated, err := validator.Validate(now, duplicate)
	if err != nil {
		t.Fatal(err)
	}
	stored, created, err = store.RecordValidatedSignal(ctx, duplicate, duplicateValidated)
	if err != nil || created || stored.ID != validated.ID {
		t.Fatalf("dedupe after restart = %#v, created %t, err %v", stored, created, err)
	}
	loadedRaw, loadedValidated, err := store.GetValidatedSignal(ctx, validated.ID)
	if err != nil || loadedRaw.Text != raw.Text || loadedValidated.Trust != signals.TrustValidatedObservation {
		t.Fatalf("loaded raw = %#v validated = %#v err %v", loadedRaw, loadedValidated, err)
	}
}
