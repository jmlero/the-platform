package demo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/store"
)

const AgentID = "agt_demo"

type Agent struct {
	store  *store.Store
	logger *slog.Logger
	delay  time.Duration
}

func NewAgent(store *store.Store, logger *slog.Logger) *Agent {
	return &Agent{store: store, logger: logger, delay: 1400 * time.Millisecond}
}

func (a *Agent) Run(ctx context.Context) {
	_, err := a.store.RegisterAgent(ctx, domain.Agent{
		ID: AgentID, Name: "Atlas", Kind: "demo",
		Status: "idle", Capabilities: []string{"planning", "implementation", "verification"},
	})
	if err != nil {
		a.logger.Error("register demo agent", "error", err)
		return
	}

	ticker := time.NewTicker(a.delay)
	defer ticker.Stop()
	for {
		if err := a.step(ctx); err != nil && !errors.Is(err, context.Canceled) {
			a.logger.Warn("demo agent step", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *Agent) step(ctx context.Context) error {
	active, err := a.store.GetActiveRunForAgent(ctx, AgentID)
	if errors.Is(err, domain.ErrNotFound) {
		task, err := a.store.NextReadyTask(ctx)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = a.store.ClaimTask(ctx, task.ID, AgentID, 20*time.Second)
		return err
	}
	if err != nil {
		return err
	}

	if err := a.store.HeartbeatRun(ctx, active.Run.ID, AgentID, 20*time.Second); err != nil {
		return err
	}

	switch active.Run.Status {
	case domain.RunPlanning:
		plan := fmt.Sprintf(
			"1. Inspect the task context and acceptance criteria.\n2. Execute the smallest useful change for %q.\n3. Verify the result against every acceptance criterion.\n4. Attach a concise evidence report.",
			active.Task.Title,
		)
		_, err := a.store.SubmitPlan(ctx, active.Run.ID, plan, true)
		return err

	case domain.RunWaitingDecision:
		decision, err := a.store.LatestDecisionForRun(ctx, active.Run.ID)
		if err != nil {
			return err
		}
		if decision.Status == "pending" {
			return nil
		}
		if strings.Contains(strings.ToLower(decision.Answer), "approve") {
			return a.store.StartRun(ctx, active.Run.ID)
		}
		return a.store.BlockRun(ctx, active.Run.ID, "Plan changes were requested; task needs replanning")

	case domain.RunRunning:
		count, err := a.store.CountRunEvents(ctx, active.Run.ID, "run.progress")
		if err != nil {
			return err
		}
		updates := []struct {
			message string
			stage   string
			percent int
		}{
			{"Context and constraints inspected", "inspect", 25},
			{"Primary task work completed", "execute", 65},
			{"Acceptance checks prepared", "verify", 90},
		}
		if count < len(updates) {
			update := updates[count]
			return a.store.ReportProgress(ctx, active.Run.ID, update.message, map[string]any{
				"stage": update.stage, "percent": update.percent,
			})
		}
		return a.store.BeginVerification(ctx, active.Run.ID)

	case domain.RunVerifying:
		evidence := "# Verification report\n\n- The agent followed the approved plan.\n- Each simulated execution stage emitted a durable event.\n- The task lifecycle and final artifact are visible in AppDeck.\n- The run completed within its configured time budget."
		return a.store.CompleteRun(
			ctx,
			active.Run.ID,
			"Approved plan executed and acceptance criteria verified.",
			evidence,
		)
	}
	return nil
}
