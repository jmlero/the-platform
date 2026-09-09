package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jmlero/the-platform/marina/internal/execution"
)

func TestAcquisitionSnapshotSurvivesRestartAndCannotBeReplaced(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "journal.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "project", Provider: "appdeck",
		Repository: "disabled://test", Runtime: "disabled:test", PolicyVersion: "test", ContextConfig: "test",
		ArtifactConfig: "test", DeliveryConfig: "test", BudgetMinutes: 20, MaxConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	input := execution.NewAcquisitionIntent{ProjectID: "project", WorkID: "task", TaskVersion: 1,
		TaskDigest: "digest", AcceptanceCriteriaDigest: "criteria", ClaimKey: "claim", AgentID: "agent",
		LeaseSeconds: 60, BudgetMinutes: 20, TaskSnapshot: `{"Title":"original","Budget":{"MaxAttempts":2}}`}
	if _, _, err = store.EnsureAcquisitionIntent(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot, err := store.AcquiredTaskSnapshot(ctx, "project", "task", 1)
	if err != nil || snapshot != input.TaskSnapshot {
		t.Fatalf("snapshot = %q, %v", snapshot, err)
	}
	input.TaskSnapshot = `{"Title":"edited"}`
	if _, _, err = store.EnsureAcquisitionIntent(ctx, input); !errors.Is(err, execution.ErrConflict) {
		t.Fatalf("replaced frozen authorization: %v", err)
	}
}
