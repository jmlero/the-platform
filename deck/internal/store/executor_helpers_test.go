package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

// CountRunEvents counts one run's timeline events of a type.
func (s *Store) CountRunEvents(ctx context.Context, runID, eventType string) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE run_id = ? AND type = ?", runID, eventType).Scan(&count); err != nil {
		return 0, fmt.Errorf("count run events: %w", err)
	}
	return count, nil
}

// NextReadyTask is the first task the Marina dispatcher would submit for the
// default project.
func (s *Store) NextReadyTask(ctx context.Context) (domain.Task, error) {
	tasks, err := s.MarinaReadyTasks(ctx, []string{defaultProjectID})
	if err != nil {
		return domain.Task{}, err
	}
	if len(tasks) == 0 {
		return domain.Task{}, domain.ErrNotFound
	}
	return tasks[0], nil
}

// The helpers below play an executor inside store tests: they claim with a
// lease and submit a minimal plan the way the retired provider API did.
// Marina-owned work uses BeginMarinaRun instead.

const operationClaimTask = "task.claim"

func (s *Store) ClaimTask(
	ctx context.Context,
	mutation Mutation,
	taskID string,
	agentID string,
	lease time.Duration,
) (domain.Run, error) {
	return s.ClaimTaskVersioned(ctx, mutation, taskID, 0, agentID, lease)
}

func (s *Store) ClaimTaskVersioned(
	ctx context.Context,
	mutation Mutation,
	taskID string,
	expectedTaskVersion int,
	agentID string,
	lease time.Duration,
) (domain.Run, error) {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return domain.Run{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Run{}, fmt.Errorf("begin claim: %w", err)
	}
	defer tx.Rollback()
	attempt, replayResult, replayed, err := beginIdempotentMutation(
		ctx,
		tx,
		mutation,
		task.ProjectID,
		operationClaimTask,
		taskID,
		struct {
			TaskID              string `json:"task_id"`
			ExpectedTaskVersion int    `json:"expected_task_version"`
			AgentID             string `json:"agent_id"`
			LeaseNanos          int64  `json:"lease_nanos"`
		}{TaskID: taskID, ExpectedTaskVersion: expectedTaskVersion, AgentID: agentID, LeaseNanos: int64(lease)},
	)
	if err != nil {
		return domain.Run{}, err
	}
	if replayed {
		var original domain.Run
		if err := decodeIdempotencyResult(replayResult, &original); err != nil {
			return domain.Run{}, err
		}
		return original, nil
	}
	if lease <= 0 {
		lease = 30 * time.Second
	}
	leaseExpires := time.Now().UTC().Add(lease).UnixMilli()
	run, event, err := claimTaskTx(ctx, tx, taskID, expectedTaskVersion, agentID, newID("run"), &leaseExpires, "Agent claimed the task and started planning")
	if err != nil {
		return domain.Run{}, err
	}
	if err := finishIdempotentMutation(ctx, tx, attempt, run); err != nil {
		return domain.Run{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Run{}, fmt.Errorf("commit claim: %w", err)
	}
	s.publish(event)
	return run, nil
}

// SubmitPlan is the protocol-v1 compatibility entry point. Plans are still
// persisted as immutable revisions; missing structured fields are explicit
// compatibility defaults rather than an in-place Markdown update.
func (s *Store) SubmitPlan(
	ctx context.Context,
	mutation Mutation,
	runID string,
	plan string,
	requireApproval bool,
) (*domain.Decision, error) {
	run, err := s.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	_, decision, err := s.SubmitPlanRevision(ctx, mutation, runID, domain.PlanRevisionInput{
		Plan:                 plan,
		ContextDigest:        run.AcceptanceCriteriaDigest,
		RiskClass:            "medium",
		VerificationStrategy: "Verify the task acceptance criteria.",
		ExpectedSideEffects:  []string{},
		RollbackApproach:     "Stop the run and request a revised plan.",
	}, domain.DecisionRequest{Required: requireApproval, Risk: "medium"})
	return decision, err
}
