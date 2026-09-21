package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestBlockOfTerminalRunCannotChangeReclaimedTask(t *testing.T) {
	ctx := context.Background()
	appStore, task, old := createPlanningRun(t, ctx, "review-terminal-block.db")
	defer appStore.Close()
	first := testMutation("review-first-block")
	first.Guard = &RunGuard{AgentID: old.AgentID, ExpectedVersion: old.Version, FenceToken: old.FenceToken}
	if err := appStore.BlockRun(ctx, first, old.ID, "first attempt stopped"); err != nil {
		t.Fatal(err)
	}
	ready, err := appStore.MoveTask(ctx, task.ID, domain.TaskReady)
	if err != nil {
		t.Fatal(err)
	}
	active, err := appStore.ClaimTaskVersioned(ctx, testMutation("review-second-claim"), task.ID, ready.Version, old.AgentID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Replaying the original successful request remains harmless after reclaim.
	if err := appStore.BlockRun(ctx, first, old.ID, "first attempt stopped"); err != nil {
		t.Fatalf("idempotent block replay: %v", err)
	}
	old, err = appStore.GetRun(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	delayed := testMutation("review-delayed-block")
	delayed.Guard = &RunGuard{AgentID: old.AgentID, ExpectedVersion: old.Version, FenceToken: old.FenceToken}
	err = appStore.BlockRun(ctx, delayed, old.ID, "delayed block for finished run")
	currentTask, loadErr := appStore.GetTask(ctx, task.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	currentRun, loadErr := appStore.GetRun(ctx, active.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if !errors.Is(err, domain.ErrConflict) || currentTask.Status != domain.TaskPlanning || currentTask.AssignedAgentID == nil || *currentTask.AssignedAgentID != active.AgentID || currentRun.Status != domain.RunPlanning {
		t.Fatalf("terminal block returned %v; task=%s assigned=%v, new run=%s", err, currentTask.Status, currentTask.AssignedAgentID, currentRun.Status)
	}
}
