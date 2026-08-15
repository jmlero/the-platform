package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestGuardedAuthoritySnapshotAndCancellation(t *testing.T) {
	ctx := context.Background()
	appStore, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer appStore.Close()
	if _, err := appStore.RegisterAgent(ctx, domain.Agent{ID: "agent-stable", InstallationID: "installation-1", Name: "Marina", Kind: "controller"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	task, err := appStore.CreateTask(ctx, domain.CreateTaskInput{Title: "Guard authority", AcceptanceCriteria: "one exact outcome", Status: domain.TaskReady, Priority: "high", BudgetMinutes: 10})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	run, err := appStore.ClaimTaskVersioned(ctx, testMutation("claim-guarded"), task.ID, task.Version, "agent-stable", time.Minute)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	digest := sha256.Sum256([]byte(task.AcceptanceCriteria))
	if run.Version != 1 || run.FenceToken != 1 || run.TaskVersion != task.Version || run.AcceptanceCriteriaDigest != hex.EncodeToString(digest[:]) {
		t.Fatalf("claimed authority snapshot = %#v", run)
	}
	changed := "later acceptance criteria must not alter the active run"
	currentTask, err := appStore.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appStore.UpdateTask(ctx, task.ID, domain.UpdateTaskInput{AcceptanceCriteria: &changed, Version: &currentTask.Version}); err != nil {
		t.Fatalf("update task: %v", err)
	}
	persisted, err := appStore.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.TaskVersion != run.TaskVersion || persisted.AcceptanceCriteriaDigest != run.AcceptanceCriteriaDigest {
		t.Fatalf("run snapshot changed: %#v", persisted)
	}

	heartbeat := testMutation("heartbeat-guarded")
	heartbeat.Guard = &RunGuard{AgentID: run.AgentID, ExpectedVersion: run.Version, FenceToken: run.FenceToken}
	if err := appStore.HeartbeatRun(ctx, heartbeat, run.ID, run.AgentID, 2*time.Minute); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if err := appStore.HeartbeatRun(ctx, heartbeat, run.ID, run.AgentID, 2*time.Minute); err != nil {
		t.Fatalf("heartbeat replay: %v", err)
	}
	stale := testMutation("heartbeat-stale")
	stale.Guard = heartbeat.Guard
	if err := appStore.HeartbeatRun(ctx, stale, run.ID, run.AgentID, 2*time.Minute); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale heartbeat = %v", err)
	}

	current, _ := appStore.GetRun(ctx, run.ID)
	command, err := appStore.CreateCommand(ctx, run.ID, domain.CommandCancel, "operator stopped work", "operator:test")
	if err != nil {
		t.Fatalf("create cancel: %v", err)
	}
	if command.Sequence != 1 || command.RunVersion != current.Version+1 || command.FenceToken != current.FenceToken+1 {
		t.Fatalf("cancel command = %#v", command)
	}
	oldFence := testMutation("heartbeat-old-fence-guarded")
	oldFence.Guard = &RunGuard{AgentID: run.AgentID, ExpectedVersion: current.Version, FenceToken: current.FenceToken}
	if err := appStore.HeartbeatRun(ctx, oldFence, run.ID, run.AgentID, time.Minute); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("old fence heartbeat = %v", err)
	}
	ack := testMutation("ack-cancel")
	ack.Guard = &RunGuard{AgentID: run.AgentID, ExpectedVersion: command.RunVersion, FenceToken: command.FenceToken}
	if err := appStore.AcknowledgeCommand(ctx, ack, run.ID, command.ID, "completed", "cancelled safely"); err != nil {
		t.Fatalf("ack cancel: %v", err)
	}
	finished, _ := appStore.GetRun(ctx, run.ID)
	if finished.Status != domain.RunCancelled || finished.LeaseExpiresAt != nil {
		t.Fatalf("cancelled run = %#v", finished)
	}
	commands, err := appStore.ListCommands(ctx, run.ID, 0, 10)
	if err != nil || len(commands) != 1 || commands[0].Status != "completed" {
		t.Fatalf("commands = %#v, %v", commands, err)
	}
}

func TestProjectedEventsDeduplicateProducerIdentityAndSequence(t *testing.T) {
	ctx := context.Background()
	appStore, err := Open(ctx, filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer appStore.Close()
	_, _ = appStore.RegisterAgent(ctx, domain.Agent{ID: "agent-events", InstallationID: "installation-events", Name: "Marina"})
	task, _ := appStore.CreateTask(ctx, domain.CreateTaskInput{Title: "Project once", Status: domain.TaskReady, Priority: "high", BudgetMinutes: 10})
	run, err := appStore.ClaimTaskVersioned(ctx, testMutation("claim-events"), task.ID, task.Version, "agent-events", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	event := domain.ProjectedEvent{Producer: "marina", ProducerEventID: "outbox-1", ExecutionID: "execution-1", Sequence: 1,
		Type: "execution.claimed", Message: "claimed durably", Payload: map[string]any{"phase": "planning"}, OccurredAt: time.Now().UTC().UnixMilli()}
	first := testMutation("append-event-1")
	first.Guard = &RunGuard{AgentID: run.AgentID, ExpectedVersion: run.Version, FenceToken: run.FenceToken}
	if err := appStore.AppendProjectedEvent(ctx, first, run.ID, event); err != nil {
		t.Fatalf("append: %v", err)
	}
	current, _ := appStore.GetRun(ctx, run.ID)
	replay := testMutation("append-event-2")
	replay.Guard = &RunGuard{AgentID: run.AgentID, ExpectedVersion: current.Version, FenceToken: current.FenceToken}
	if err := appStore.AppendProjectedEvent(ctx, replay, run.ID, event); err != nil {
		t.Fatalf("producer replay: %v", err)
	}
	afterReplay, _ := appStore.GetRun(ctx, run.ID)
	if afterReplay.Version != current.Version {
		t.Fatalf("producer replay advanced run version")
	}
	changed := event
	changed.Message = "different content"
	conflict := testMutation("append-event-3")
	conflict.Guard = replay.Guard
	if err := appStore.AppendProjectedEvent(ctx, conflict, run.ID, changed); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed producer replay = %v", err)
	}
	count, _ := appStore.CountRunEvents(ctx, run.ID, event.Type)
	if count != 1 {
		t.Fatalf("timeline events = %d, want 1", count)
	}
	outOfOrder := event
	outOfOrder.ProducerEventID = "outbox-older"
	outOfOrder.Message = "late older event"
	outOfOrderMutation := testMutation("append-event-out-of-order")
	outOfOrderMutation.Guard = &RunGuard{AgentID: afterReplay.AgentID, ExpectedVersion: afterReplay.Version, FenceToken: afterReplay.FenceToken}
	if err := appStore.AppendProjectedEvent(ctx, outOfOrderMutation, run.ID, outOfOrder); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("out-of-order producer sequence = %v", err)
	}
}

func TestExpireLeasesLeavesRenewedRunIntact(t *testing.T) {
	ctx := context.Background()
	appStore, err := Open(ctx, filepath.Join(t.TempDir(), "lease-renew-race.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer appStore.Close()
	if _, err := appStore.RegisterAgent(ctx, domain.Agent{ID: "agent-renew-race", InstallationID: "installation-renew-race", Name: "Marina"}); err != nil {
		t.Fatal(err)
	}
	task, err := appStore.CreateTask(ctx, domain.CreateTaskInput{Title: "Keep renewed lease", Status: domain.TaskReady, Priority: "high", BudgetMinutes: 10})
	if err != nil {
		t.Fatal(err)
	}
	run, err := appStore.ClaimTaskVersioned(ctx, testMutation("claim-renew-race"), task.ID, task.Version, "agent-renew-race", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appStore.db.ExecContext(ctx, "UPDATE runs SET lease_expires_at = ? WHERE id = ?", time.Now().UTC().Add(-time.Minute).UnixMilli(), run.ID); err != nil {
		t.Fatal(err)
	}
	heartbeat := testMutation("heartbeat-after-expiry-list")
	heartbeat.Guard = &RunGuard{AgentID: run.AgentID, ExpectedVersion: run.Version, FenceToken: run.FenceToken}
	if err := appStore.HeartbeatRun(ctx, heartbeat, run.ID, run.AgentID, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	applied, err := appStore.failExpiredRun(ctx, run.ID)
	if err != nil || applied {
		t.Fatalf("failExpiredRun after renewal = %t, %v", applied, err)
	}
	current, err := appStore.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != domain.RunPlanning || current.LeaseExpiresAt == nil || *current.LeaseExpiresAt <= nowMillis() {
		t.Fatalf("renewed run was reaped: %#v", current)
	}
	if count, err := appStore.ExpireLeases(ctx); err != nil || count != 0 {
		t.Fatalf("ExpireLeases after renewal = %d, %v", count, err)
	}
}

func TestClaimLookupReturnsCurrentFencedStateAfterLeaseReaping(t *testing.T) {
	ctx := context.Background()
	appStore, err := Open(ctx, filepath.Join(t.TempDir(), "claim-reaping.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer appStore.Close()
	_, _ = appStore.RegisterAgent(ctx, domain.Agent{ID: "agent-reaping", InstallationID: "installation-reaping", Name: "Marina"})
	task, _ := appStore.CreateTask(ctx, domain.CreateTaskInput{Title: "Reap ambiguous claim", Status: domain.TaskReady, Priority: "high", BudgetMinutes: 10})
	run, err := appStore.ClaimTaskVersioned(ctx, testMutation("claim-before-reaping"), task.ID, task.Version, "agent-reaping", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appStore.db.ExecContext(ctx, "UPDATE runs SET lease_expires_at = ? WHERE id = ?", time.Now().UTC().Add(-time.Minute).UnixMilli(), run.ID); err != nil {
		t.Fatal(err)
	}
	if count, err := appStore.ExpireLeases(ctx); err != nil || count != 1 {
		t.Fatalf("expire leases = %d, %v", count, err)
	}
	recovered, err := appStore.LookupClaim(ctx, task.ProjectID, testMutation("unused").ActorID, "claim-before-reaping")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != domain.RunFailed || recovered.LeaseExpiresAt != nil || recovered.Version != run.Version+1 || recovered.FenceToken != run.FenceToken+1 {
		t.Fatalf("recovered claim authority = %#v", recovered)
	}
}

func TestInstallationIdentityOwnsOneStableLogicalAgent(t *testing.T) {
	ctx := context.Background()
	appStore, err := Open(ctx, filepath.Join(t.TempDir(), "installation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer appStore.Close()
	registration := domain.Agent{ID: "agent-controller", InstallationID: "installation-stable", Name: "Marina", Kind: "controller"}
	first, err := appStore.RegisterAgent(ctx, registration)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := appStore.RegisterAgent(ctx, registration)
	if err != nil || replayed.ID != first.ID || replayed.InstallationID != first.InstallationID {
		t.Fatalf("registration replay = %#v, %v", replayed, err)
	}
	registration.ID = "agent-impostor"
	if _, err := appStore.RegisterAgent(ctx, registration); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second logical agent for installation = %v", err)
	}
}
