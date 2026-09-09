package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
)

func TestModelReservationSurvivesUnknownResponseAndPreventsOverspend(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	record, _, err := store.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "appdeck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", LeaseExpiresAt: now.Add(time.Minute), Budget: execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 3, MaxTokens: 100, MaxCostMicros: 100}})
	if err != nil {
		t.Fatal(err)
	}
	_, attempt, _, err := store.EnsureRuntimeAttempt(ctx, record.ID, record.Version, runtimeapi.Specification{IdempotencyKey: "model-attempt", RepositoryURI: "https://git.example/repo", SourceRevision: strings.Repeat("a", 40), ContextURI: "journal://context", ContextDigest: "sha256:context", HarnessVersion: "test", Limits: runtimeapi.Limits{CPUMillis: 1000, MemoryBytes: 1024, EphemeralStorageBytes: 1024, WallTimeSeconds: 60, MaxProcesses: 256}, NetworkPolicy: "gateway-only", FilesystemPolicy: "ephemeral", AuthorityFence: 1, DeadlineAt: now.Add(time.Minute), PolicyVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.ReserveModelCall(ctx, record.ID, attempt.ID, 1, 60, 60)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveModelCall(ctx, record.ID, attempt.ID, 1, 50, 50); !errors.Is(err, execution.ErrBudgetExhausted) {
		t.Fatalf("unknown call released its reservation: %v", err)
	}
	if err := store.FinishModelCall(ctx, id, 20, 20); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishModelCall(ctx, id, 20, 20); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveModelCall(ctx, record.ID, attempt.ID, 1, 50, 50); err != nil {
		t.Fatalf("actual usage did not release unused reservation: %v", err)
	}
	updated, err := store.GetExecution(ctx, record.ID)
	if err != nil || updated.UsedTokens != 20 || updated.UsedCostMicros != 20 {
		t.Fatalf("actual budget usage: %#v %v", updated, err)
	}
	tokens, cost, err := store.ModelUsage(ctx, attempt.ID)
	if err != nil || tokens != 70 || cost != 70 {
		t.Fatalf("actual and reserved usage: %d %d %v", tokens, cost, err)
	}
	if _, err = store.RecordRuntimeObservation(ctx, attempt.ID, attempt.Version, runtimeapi.Observation{Found: true, ProviderHandle: "job/uid", State: runtimeapi.StateCancelled}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ReserveModelCall(ctx, record.ID, attempt.ID, 1, 10, 10); !errors.Is(err, execution.ErrConflict) {
		t.Fatalf("terminated attempt received model budget: %v", err)
	}
}
