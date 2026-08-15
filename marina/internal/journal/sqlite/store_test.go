package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/reconcile"
)

func TestJournalPersistsLifecycleAndRecoversWithoutDuplicateWork(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	databasePath := filepath.Join(t.TempDir(), "marina.db")
	store := openStore(t, ctx, databasePath, &now)

	input := validExecution(now, "run-1")
	record, created, err := store.EnsureExecution(ctx, input)
	if err != nil {
		t.Fatalf("ensure execution: %v", err)
	}
	if !created || record.State != execution.StatePlanning || record.Version != 1 {
		t.Fatalf("created execution = %#v, created = %t", record, created)
	}

	replayed, created, err := store.EnsureExecution(ctx, input)
	if err != nil {
		t.Fatalf("replay execution: %v", err)
	}
	if created || replayed.ID != record.ID {
		t.Fatalf("replayed ID = %q, created = %t; want %q, false", replayed.ID, created, record.ID)
	}
	conflicting := input
	conflicting.ExternalWorkID = "task-other"
	if _, _, err := store.EnsureExecution(ctx, conflicting); !errors.Is(err, execution.ErrConflict) {
		t.Fatalf("conflicting external run error = %v, want conflict", err)
	}

	record = transition(t, ctx, store, record, execution.StateRunning, "plan approved")
	checkpointInput := execution.CheckpointInput{
		Key:            "runtime-started",
		Phase:          "implementation",
		RuntimeHandle:  "job/marina-run-1",
		SourceRevision: "abc123",
		Payload:        map[string]any{"workspace": "repo", "attempt": 1},
	}
	checkpointVersion := record.Version
	record, checkpoint, checkpointCreated, err := store.SaveCheckpoint(ctx, record.ID, checkpointVersion, checkpointInput)
	if err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	if !checkpointCreated || checkpoint.Key != checkpointInput.Key || record.RuntimeHandle != checkpointInput.RuntimeHandle {
		t.Fatalf("checkpoint = %#v, execution = %#v, created = %t", checkpoint, record, checkpointCreated)
	}
	replayedRecord, replayedCheckpoint, checkpointCreated, err := store.SaveCheckpoint(
		ctx,
		record.ID,
		checkpointVersion,
		checkpointInput,
	)
	if err != nil {
		t.Fatalf("replay checkpoint with stale version: %v", err)
	}
	if checkpointCreated || replayedRecord.Version != record.Version || replayedCheckpoint.ID != checkpoint.ID {
		t.Fatalf("checkpoint replay changed state: record=%#v checkpoint=%#v created=%t", replayedRecord, replayedCheckpoint, checkpointCreated)
	}
	changedCheckpoint := checkpointInput
	changedCheckpoint.Phase = "different"
	if _, _, _, err := store.SaveCheckpoint(ctx, record.ID, record.Version, changedCheckpoint); !errors.Is(err, execution.ErrConflict) {
		t.Fatalf("changed checkpoint replay error = %v, want conflict", err)
	}

	record = transition(t, ctx, store, record, execution.StateVerifying, "implementation finished")
	if _, err := store.Transition(ctx, record.ID, execution.Transition{
		ExpectedVersion: record.Version,
		Target:          execution.StateCompleted,
		Message:         "verification passed",
	}); !errors.Is(err, execution.ErrEvidenceRequired) {
		t.Fatalf("completion without evidence error = %v, want evidence required", err)
	}

	evidenceInput := execution.EvidenceInput{
		Key:            "go-test-abc123",
		Kind:           "test",
		SourceRevision: "abc123",
		Environment:    "agent-job",
		Command:        "go test ./...",
		Summary:        "all packages passed",
		URI:            "artifact://run-1/test.log",
		ObservedAt:     now.Add(2 * time.Minute),
	}
	evidenceVersion := record.Version
	record, evidenceRecord, evidenceCreated, err := store.AddEvidence(ctx, record.ID, evidenceVersion, evidenceInput)
	if err != nil {
		t.Fatalf("add evidence: %v", err)
	}
	if !evidenceCreated || evidenceRecord.SourceRevision != "abc123" {
		t.Fatalf("evidence = %#v, created = %t", evidenceRecord, evidenceCreated)
	}
	replayedRecord, replayedEvidence, evidenceCreated, err := store.AddEvidence(
		ctx,
		record.ID,
		evidenceVersion,
		evidenceInput,
	)
	if err != nil {
		t.Fatalf("replay evidence with stale version: %v", err)
	}
	if evidenceCreated || replayedRecord.Version != record.Version || replayedEvidence.ID != evidenceRecord.ID {
		t.Fatalf("evidence replay changed state: record=%#v evidence=%#v created=%t", replayedRecord, replayedEvidence, evidenceCreated)
	}

	record = transition(t, ctx, store, record, execution.StateCompleted, "verification passed")
	if record.FinishedAt == nil {
		t.Fatal("completed execution has no finish time")
	}
	if active, err := store.ListActiveExecutions(ctx, 10); err != nil || len(active) != 0 {
		t.Fatalf("active after completion = %#v, error = %v", active, err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close journal: %v", err)
	}
	store = openStore(t, ctx, databasePath, &now)
	t.Cleanup(func() { _ = store.Close() })

	persisted, err := store.GetExecution(ctx, record.ID)
	if err != nil {
		t.Fatalf("get execution after restart: %v", err)
	}
	if persisted.State != execution.StateCompleted || persisted.Version != record.Version || persisted.RuntimeHandle != checkpointInput.RuntimeHandle {
		t.Fatalf("persisted execution = %#v, want completed version %d", persisted, record.Version)
	}
	latest, err := store.LatestCheckpoint(ctx, record.ID)
	if err != nil || latest.ID != checkpoint.ID {
		t.Fatalf("latest checkpoint = %#v, error = %v", latest, err)
	}
	evidenceRecords, err := store.ListEvidence(ctx, record.ID)
	if err != nil || len(evidenceRecords) != 1 || evidenceRecords[0].SourceRevision != "abc123" {
		t.Fatalf("persisted evidence = %#v, error = %v", evidenceRecords, err)
	}
	events, err := store.ListEvents(ctx, record.ID, 100)
	if err != nil {
		t.Fatalf("list persisted events: %v", err)
	}
	if len(events) != 6 {
		t.Fatalf("event count = %d, want 6: %#v", len(events), events)
	}

	replayed, created, err = store.EnsureExecution(ctx, input)
	if err != nil || created || replayed.ID != record.ID {
		t.Fatalf("ensure after restart = %#v, created = %t, error = %v", replayed, created, err)
	}
}

func TestJournalGuardsVersionsTransitionsLeasesAndBudgets(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	store := openStore(t, ctx, ":memory:", &now)
	t.Cleanup(func() { _ = store.Close() })

	record, _, err := store.EnsureExecution(ctx, validExecution(now, "run-guards"))
	if err != nil {
		t.Fatalf("ensure execution: %v", err)
	}
	if _, err := store.Transition(ctx, record.ID, execution.Transition{
		ExpectedVersion: record.Version + 1,
		Target:          execution.StateRunning,
		Message:         "start",
	}); !errors.Is(err, execution.ErrConflict) {
		t.Fatalf("stale transition error = %v, want conflict", err)
	}
	if _, err := store.Transition(ctx, record.ID, execution.Transition{
		ExpectedVersion: record.Version,
		Target:          execution.StateCompleted,
		Message:         "skip lifecycle",
	}); !errors.Is(err, execution.ErrInvalidTransition) {
		t.Fatalf("invalid transition error = %v, want invalid transition", err)
	}
	if _, err := store.RenewLease(ctx, record.ID, record.Version, record.LeaseExpiresAt); !errors.Is(err, execution.ErrInvalidInput) {
		t.Fatalf("non-extending lease error = %v, want invalid input", err)
	}
	renewedExpiry := now.Add(20 * time.Minute)
	record, err = store.RenewLease(ctx, record.ID, record.Version, renewedExpiry)
	if err != nil {
		t.Fatalf("renew lease: %v", err)
	}
	if !record.LeaseExpiresAt.Equal(renewedExpiry) || record.Version != 2 {
		t.Fatalf("renewed execution = %#v", record)
	}
	if _, err := store.RenewLease(ctx, record.ID, 1, now.Add(30*time.Minute)); !errors.Is(err, execution.ErrConflict) {
		t.Fatalf("stale lease error = %v, want conflict", err)
	}
	reconciledExpiry := now.Add(5 * time.Minute)
	record, err = store.ReconcileLease(ctx, record.ID, record.Version, reconciledExpiry, "provider reported a shorter lease")
	if err != nil {
		t.Fatalf("reconcile shorter lease: %v", err)
	}
	if !record.LeaseExpiresAt.Equal(reconciledExpiry) || record.Version != 3 {
		t.Fatalf("reconciled execution = %#v", record)
	}
	replayed, err := store.ReconcileLease(ctx, record.ID, 2, reconciledExpiry, "provider reported a shorter lease")
	if err != nil || replayed.Version != record.Version {
		t.Fatalf("replay reconciled lease = %#v, error = %v", replayed, err)
	}

	now = record.DeadlineAt
	if _, err := store.Transition(ctx, record.ID, execution.Transition{
		ExpectedVersion: record.Version,
		Target:          execution.StateRunning,
		Message:         "start after deadline",
	}); !errors.Is(err, execution.ErrBudgetExhausted) {
		t.Fatalf("expired budget transition error = %v, want budget exhausted", err)
	}
	if _, err := store.RenewLease(ctx, record.ID, record.Version, now.Add(time.Minute)); !errors.Is(err, execution.ErrBudgetExhausted) {
		t.Fatalf("expired budget lease error = %v, want budget exhausted", err)
	}
}

func TestJournalRejectsUnboundedExecution(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	store := openStore(t, ctx, ":memory:", &now)
	t.Cleanup(func() { _ = store.Close() })

	input := validExecution(now, "run-unbounded")
	input.Budget.MaxTokens = 0
	input.Budget.MaxCostMicros = 0
	if _, _, err := store.EnsureExecution(ctx, input); !errors.Is(err, execution.ErrInvalidInput) {
		t.Fatalf("unbounded execution error = %v, want invalid input", err)
	}
}

func TestRestartPlannerUsesPersistedActiveStateWithoutClaiming(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	databasePath := filepath.Join(t.TempDir(), "marina.db")
	store := openStore(t, ctx, databasePath, &now)
	record, _, err := store.EnsureExecution(ctx, validExecution(now, "run-recovery"))
	if err != nil {
		t.Fatalf("ensure execution: %v", err)
	}
	record = transition(t, ctx, store, record, execution.StateRunning, "runtime started")
	if err := store.Close(); err != nil {
		t.Fatalf("close before restart: %v", err)
	}

	store = openStore(t, ctx, databasePath, &now)
	t.Cleanup(func() { _ = store.Close() })
	planner, err := reconcile.NewPlanner(store, 5*time.Minute)
	if err != nil {
		t.Fatalf("new recovery planner: %v", err)
	}
	planner.WithClock(func() time.Time { return now })
	intents, err := planner.Plan(ctx, 100)
	if err != nil {
		t.Fatalf("plan after restart: %v", err)
	}
	if len(intents) != 1 || intents[0].ExecutionID != record.ID || intents[0].Action != execution.RecoveryInspectRuntime {
		t.Fatalf("restart intents = %#v", intents)
	}

	persisted, err := store.GetExecution(ctx, record.ID)
	if err != nil {
		t.Fatalf("get execution after planning: %v", err)
	}
	if persisted.Version != record.Version || persisted.State != record.State {
		t.Fatalf("planning mutated execution: before=%#v after=%#v", record, persisted)
	}
}

func TestProviderOutboxPreservesPerExecutionOrderAndPrioritizesCommandAcks(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	store := openStore(t, ctx, ":memory:", &now)
	t.Cleanup(func() { _ = store.Close() })
	record, _, err := store.EnsureExecution(ctx, validExecution(now, "run-outbox-order"))
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := store.EnqueueOutbox(ctx, record.ID, execution.OutboxInput{Kind: "event", IdempotencyKey: "event-1", Payload: map[string]any{"type": "progress"}})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := store.EnqueueOutbox(ctx, record.ID, execution.OutboxInput{Kind: "event", IdempotencyKey: "event-2", Payload: map[string]any{"type": "failure"}})
	if err != nil {
		t.Fatal(err)
	}
	ready, err := store.ListReadyOutbox(ctx, 10)
	if err != nil || len(ready) != 1 || ready[0].ID != first.ID {
		t.Fatalf("initial ready outbox = %#v, %v", ready, err)
	}
	if err := store.RecordOutboxFailure(ctx, first.ID, execution.FailurePermanent, "provider rejected projection", 5); err != nil {
		t.Fatal(err)
	}
	ready, err = store.ListReadyOutbox(ctx, 10)
	if err != nil || len(ready) != 0 {
		t.Fatalf("later event crossed exhausted predecessor %s: %#v, %v", second.ID, ready, err)
	}
	ack, _, err := store.EnqueueOutbox(ctx, record.ID, execution.OutboxInput{Kind: "command_ack", IdempotencyKey: "ack-1", Payload: map[string]any{"command_id": "command-1"}})
	if err != nil {
		t.Fatal(err)
	}
	ready, err = store.ListReadyOutbox(ctx, 10)
	if err != nil || len(ready) != 1 || ready[0].ID != ack.ID {
		t.Fatalf("pre-emptive command acknowledgement = %#v, %v", ready, err)
	}
}

func TestCompleteOutboxPublicationCommitsAuthorityWithPublishedMark(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	store := openStore(t, ctx, ":memory:", &now)
	t.Cleanup(func() { _ = store.Close() })
	record, _, err := store.EnsureExecution(ctx, validExecution(now, "run-outbox-atomic"))
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := store.EnqueueOutbox(ctx, record.ID, execution.OutboxInput{Kind: "event", IdempotencyKey: "event-atomic", Payload: map[string]any{"type": "progress"}})
	if err != nil {
		t.Fatal(err)
	}
	lease := now.Add(20 * time.Minute)
	updated, err := store.CompleteOutboxPublication(ctx, item.ID, record.ID, record.Version, record.ExternalRunVersion+1,
		record.FenceToken, lease, "provider projection acknowledged")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ExternalRunVersion != record.ExternalRunVersion+1 || !updated.LeaseExpiresAt.Equal(lease) {
		t.Fatalf("authority after publication = %#v", updated)
	}
	ready, err := store.ListReadyOutbox(ctx, 10)
	if err != nil || len(ready) != 0 {
		t.Fatalf("published item still ready: %#v, %v", ready, err)
	}

	second, _, err := store.EnqueueOutbox(ctx, record.ID, execution.OutboxInput{Kind: "event", IdempotencyKey: "event-rewind", Payload: map[string]any{"type": "later"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteOutboxPublication(ctx, second.ID, updated.ID, updated.Version, record.ExternalRunVersion,
		updated.FenceToken, lease, "rewind must not publish"); !errors.Is(err, execution.ErrConflict) {
		t.Fatalf("rewind publication = %v", err)
	}
	ready, err = store.ListReadyOutbox(ctx, 10)
	if err != nil || len(ready) != 1 || ready[0].ID != second.ID || ready[0].Status != "pending" {
		t.Fatalf("rewind left predecessor unpublished: %#v, %v", ready, err)
	}
	reloaded, err := store.GetExecution(ctx, updated.ID)
	if err != nil || reloaded.ExternalRunVersion != updated.ExternalRunVersion || reloaded.Version != updated.Version {
		t.Fatalf("rewind mutated authority: %#v, %v", reloaded, err)
	}
}

func openStore(t *testing.T, ctx context.Context, path string, now *time.Time) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(ctx, path, sqlite.WithClock(func() time.Time { return *now }))
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	return store
}

func validExecution(now time.Time, runID string) execution.NewExecution {
	return execution.NewExecution{
		WorkProvider:   "appdeck",
		ExternalWorkID: "task-1",
		ExternalRunID:  runID,
		AgentID:        "marina-1",
		LeaseExpiresAt: now.Add(15 * time.Minute),
		Budget: execution.Budget{
			DeadlineAt:  now.Add(time.Hour),
			MaxAttempts: 3,
			MaxTokens:   100_000,
		},
	}
}

type journalStore interface {
	Transition(context.Context, string, execution.Transition) (execution.Execution, error)
}

func transition(
	t *testing.T,
	ctx context.Context,
	store journalStore,
	record execution.Execution,
	target execution.State,
	message string,
) execution.Execution {
	t.Helper()
	updated, err := store.Transition(ctx, record.ID, execution.Transition{
		ExpectedVersion: record.Version,
		Target:          target,
		Message:         message,
	})
	if err != nil {
		t.Fatalf("transition %s to %s: %v", record.State, target, err)
	}
	return updated
}
