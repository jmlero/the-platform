package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
)

func TestRuntimeAttemptsPersistSpecificationObservationArtifactsAndCleanup(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 16, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "runtime-attempts.db")
	store := openStore(t, ctx, path, &now)
	record, _, err := store.EnsureExecution(ctx, validExecution(now, "run-runtime"))
	if err != nil {
		t.Fatal(err)
	}
	specification := validRuntimeSpecification(now)
	updated, attempt, created, err := store.EnsureRuntimeAttempt(ctx, record.ID, record.Version, specification)
	if err != nil {
		t.Fatalf("ensure runtime attempt: %v", err)
	}
	if !created || attempt.Number != 1 || attempt.State != runtimeapi.StatePending || updated.Attempt != 1 {
		t.Fatalf("created attempt = %#v, execution = %#v, created %t", attempt, updated, created)
	}
	replayedExecution, replayed, created, err := store.EnsureRuntimeAttempt(ctx, record.ID, record.Version, specification)
	if err != nil || created || replayed.ID != attempt.ID || replayedExecution.Version != updated.Version {
		t.Fatalf("replayed attempt = %#v, execution %#v, created %t, err %v", replayed, replayedExecution, created, err)
	}
	changed := specification
	changed.HarnessVersion = "different"
	if _, _, _, err := store.EnsureRuntimeAttempt(ctx, record.ID, updated.Version, changed); !errors.Is(err, execution.ErrConflict) {
		t.Fatalf("changed idempotency payload error = %v, want conflict", err)
	}
	attempt, err = store.MarkRuntimeStarting(ctx, attempt.ID, attempt.Version)
	if err != nil || attempt.State != runtimeapi.StateStarting {
		t.Fatalf("persist start intent = %#v, err %v", attempt, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, ctx, path, &now)

	heartbeat := now.Add(time.Minute)
	attempt, err = store.RecordRuntimeObservation(ctx, attempt.ID, attempt.Version, runtimeapi.Observation{
		Found: true, ProviderHandle: "local/process-42", State: runtimeapi.StateRunning,
		HeartbeatAt: &heartbeat, Usage: runtimeapi.Usage{Tokens: 1200, CostMicros: 3300, CPUTimeMillis: 500},
		OutputReferences: []runtimeapi.OutputReference{{Kind: "log", URI: "artifact://runtime/log", Digest: "sha256:log", SizeBytes: 42}},
	})
	if err != nil {
		t.Fatalf("record observation: %v", err)
	}
	if attempt.ProviderHandle != "local/process-42" || attempt.State != runtimeapi.StateRunning || attempt.HeartbeatAt == nil || attempt.Usage.Tokens != 1200 {
		t.Fatalf("observed attempt = %#v", attempt)
	}
	artifact, created, err := store.RecordRuntimeArtifact(ctx, runtimeapi.Artifact{
		ID: "artifact-runtime-1", ExecutionID: record.ID, RuntimeAttemptID: attempt.ID,
		Name: "test.log", Kind: "verification", URI: "artifact://runtime/test.log",
		Digest: "sha256:test-log", SizeBytes: 200,
	})
	if err != nil || !created || artifact.Digest != "sha256:test-log" {
		t.Fatalf("record artifact = %#v, created %t, err %v", artifact, created, err)
	}
	if _, created, err := store.RecordRuntimeArtifact(ctx, artifact); err != nil || created {
		t.Fatalf("replay artifact created=%t err=%v", created, err)
	}

	attempt, err = store.RecordRuntimeObservation(ctx, attempt.ID, attempt.Version, runtimeapi.Observation{
		Found: true, ProviderHandle: attempt.ProviderHandle, State: runtimeapi.StateSucceeded,
		HeartbeatAt: &heartbeat, Usage: attempt.Usage, OutputReferences: attempt.OutputReferences,
	})
	if err != nil {
		t.Fatalf("record terminal observation: %v", err)
	}
	attempt, err = store.MarkRuntimeCleanup(ctx, attempt.ID, attempt.Version, runtimeapi.CleanupCompleted, "cleanup confirmed")
	if err != nil || attempt.CleanupState != runtimeapi.CleanupCompleted {
		t.Fatalf("cleanup attempt = %#v, err %v", attempt, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, ctx, path, &now)
	t.Cleanup(func() { _ = store.Close() })
	loaded, err := store.GetRuntimeAttempt(ctx, attempt.ID)
	if err != nil || loaded.ProviderHandle != attempt.ProviderHandle || loaded.CleanupState != runtimeapi.CleanupCompleted {
		t.Fatalf("loaded attempt = %#v, err %v", loaded, err)
	}
	artifacts, err := store.ListRuntimeArtifacts(ctx, record.ID)
	if err != nil || len(artifacts) != 1 || artifacts[0].ID != artifact.ID {
		t.Fatalf("persisted artifacts = %#v, err %v", artifacts, err)
	}
}

func validRuntimeSpecification(now time.Time) runtimeapi.Specification {
	return runtimeapi.Specification{
		IdempotencyKey: "runtime-start-1", RepositoryURI: "https://example.invalid/repository.git",
		SourceRevision: "abc123", ContextURI: "artifact://contexts/one", ContextDigest: "sha256:context",
		HarnessVersion: "harness-v1", PolicyVersion: "policy-v1", AuthorityFence: 1,
		DeadlineAt: now.Add(30 * time.Minute), NetworkPolicy: "deny-by-default", FilesystemPolicy: "workspace-only",
		Limits: runtimeapi.Limits{CPUMillis: 1000, MemoryBytes: 256 << 20, EphemeralStorageBytes: 1 << 30,
			WallTimeSeconds: 1200, MaxProcesses: 32},
		CredentialReferences: []runtimeapi.CredentialReference{{Kind: "source", Reference: "secret://source/task", ExpiresAt: now.Add(20 * time.Minute)}},
	}
}

func TestRuntimeAttemptLimitSurvivesRestartAndAllowsIdempotentReplay(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.September, 21, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "attempt-limit.db")
	store := openStore(t, ctx, path, &now)
	input := validExecution(now, "run-attempt-limit")
	input.Budget.MaxAttempts = 2
	record, _, err := store.EnsureExecution(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	specification := validRuntimeSpecification(now)
	var last runtimeapi.Attempt
	for _, key := range []string{"implementation", "verification"} {
		specification.IdempotencyKey = key
		var created bool
		record, last, created, err = store.EnsureRuntimeAttempt(ctx, record.ID, record.Version, specification)
		if err != nil || !created {
			t.Fatalf("admit %s: created=%t err=%v", key, created, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, ctx, path, &now)
	t.Cleanup(func() { _ = store.Close() })
	replayed, attempt, created, err := store.EnsureRuntimeAttempt(ctx, record.ID, 1, specification)
	if err != nil || created || attempt.ID != last.ID || replayed.Version != record.Version {
		t.Fatalf("replay at cap: execution=%#v attempt=%#v created=%t err=%v", replayed, attempt, created, err)
	}
	specification.IdempotencyKey = "third-attempt"
	if _, _, _, err := store.EnsureRuntimeAttempt(ctx, record.ID, record.Version, specification); !errors.Is(err, execution.ErrBudgetExhausted) {
		t.Fatalf("third attempt error=%v, want budget exhausted", err)
	}
	attempts, err := store.ListRuntimeAttempts(ctx, record.ID)
	if err != nil || len(attempts) != 2 {
		t.Fatalf("attempts after denial=%#v err=%v", attempts, err)
	}
	unchanged, err := store.GetExecution(ctx, record.ID)
	if err != nil || unchanged.Attempt != 2 || unchanged.Version != record.Version {
		t.Fatalf("denial changed execution: %#v err=%v", unchanged, err)
	}
}
