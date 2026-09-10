package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

const operationalStateColumns = `
	id, project_id, task_id, run_id, environment, rollout_state,
	observation_started_at, observation_ends_at, rollback_state,
	policy_version, source_revision, outcome, updated_at
`

func scanOperationalState(scanner interface{ Scan(...any) error }) (domain.OperationalState, error) {
	var state domain.OperationalState
	var observationStartedAt, observationEndsAt sql.NullInt64
	if err := scanner.Scan(
		&state.ID, &state.ProjectID, &state.TaskID, &state.RunID, &state.Environment,
		&state.RolloutState, &observationStartedAt, &observationEndsAt,
		&state.RollbackState, &state.PolicyVersion, &state.SourceRevision,
		&state.Outcome, &state.UpdatedAt,
	); err != nil {
		return domain.OperationalState{}, err
	}
	if observationStartedAt.Valid {
		value := observationStartedAt.Int64
		state.ObservationStartedAt = &value
	}
	if observationEndsAt.Valid {
		value := observationEndsAt.Int64
		state.ObservationEndsAt = &value
	}
	return state, nil
}

func (s *Store) RecordOperationalStateGuarded(ctx context.Context, mutation Mutation, input domain.OperationalStateInput) (domain.OperationalState, error) {
	input.RunID = strings.TrimSpace(input.RunID)
	input.Environment = strings.TrimSpace(input.Environment)
	input.RolloutState = strings.TrimSpace(input.RolloutState)
	input.RollbackState = strings.TrimSpace(input.RollbackState)
	input.PolicyVersion = strings.TrimSpace(input.PolicyVersion)
	input.SourceRevision = strings.TrimSpace(input.SourceRevision)
	input.Outcome = strings.TrimSpace(input.Outcome)
	if input.RunID == "" || input.Environment == "" || input.RolloutState == "" ||
		input.RollbackState == "" || input.PolicyVersion == "" || input.SourceRevision == "" {
		return domain.OperationalState{}, fmt.Errorf("run, environment, rollout, rollback, policy, and source revision are required: %w", domain.ErrInvalidInput)
	}
	if input.ObservationStartedAt != nil && input.ObservationEndsAt != nil && *input.ObservationEndsAt < *input.ObservationStartedAt {
		return domain.OperationalState{}, fmt.Errorf("observation window ends before it starts: %w", domain.ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.OperationalState{}, fmt.Errorf("begin operational state: %w", err)
	}
	defer tx.Rollback()
	run, err := getRunTx(ctx, tx, input.RunID)
	if err != nil {
		return domain.OperationalState{}, err
	}
	task, err := getTaskTx(ctx, tx, run.TaskID)
	if err != nil {
		return domain.OperationalState{}, err
	}
	if err := validateRunGuard(run, mutation.Guard); err != nil {
		return domain.OperationalState{}, err
	}
	attempt, replayResult, replayed, err := beginIdempotentMutation(
		ctx, tx, mutation, task.ProjectID, operationReportOperational, run.ID+":"+input.Environment,
		struct {
			Input domain.OperationalStateInput `json:"input"`
			Guard *RunGuard                    `json:"guard,omitempty"`
		}{input, mutation.Guard},
	)
	if err != nil {
		return domain.OperationalState{}, err
	}
	if replayed {
		var original domain.OperationalState
		if err := decodeIdempotencyResult(replayResult, &original); err != nil {
			return domain.OperationalState{}, err
		}
		return original, nil
	}
	now := nowMillis()
	state := domain.OperationalState{
		ID: newID("ops"), ProjectID: task.ProjectID, TaskID: task.ID, RunID: run.ID,
		Environment: input.Environment, RolloutState: input.RolloutState,
		ObservationStartedAt: input.ObservationStartedAt, ObservationEndsAt: input.ObservationEndsAt,
		RollbackState: input.RollbackState, PolicyVersion: input.PolicyVersion,
		SourceRevision: input.SourceRevision, Outcome: input.Outcome, UpdatedAt: now,
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO operational_states (
			id, project_id, task_id, run_id, environment, rollout_state,
			observation_started_at, observation_ends_at, rollback_state,
			policy_version, source_revision, outcome, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, environment) DO UPDATE SET
			rollout_state = excluded.rollout_state,
			observation_started_at = excluded.observation_started_at,
			observation_ends_at = excluded.observation_ends_at,
			rollback_state = excluded.rollback_state,
			policy_version = excluded.policy_version,
			source_revision = excluded.source_revision,
			outcome = excluded.outcome,
			updated_at = excluded.updated_at
	`, state.ID, state.ProjectID, state.TaskID, state.RunID, state.Environment,
		state.RolloutState, nullableInt64(state.ObservationStartedAt), nullableInt64(state.ObservationEndsAt),
		state.RollbackState, state.PolicyVersion, state.SourceRevision, state.Outcome, state.UpdatedAt); err != nil {
		return domain.OperationalState{}, fmt.Errorf("record operational state: %w", err)
	}
	loaded, err := scanOperationalState(tx.QueryRowContext(ctx,
		"SELECT "+operationalStateColumns+" FROM operational_states WHERE run_id = ? AND environment = ?",
		run.ID, state.Environment,
	))
	if err != nil {
		return domain.OperationalState{}, fmt.Errorf("load operational state: %w", err)
	}
	if err := finishIdempotentMutation(ctx, tx, attempt, loaded); err != nil {
		return domain.OperationalState{}, err
	}
	event, err := insertEventTx(ctx, tx, &task.ID, &run.ID, &run.AgentID,
		"operation.state.recorded", "Operational state updated for "+state.Environment,
		map[string]any{
			"operational_state_id": loaded.ID, "environment": loaded.Environment,
			"rollout_state": loaded.RolloutState, "rollback_state": loaded.RollbackState,
			"policy_version": loaded.PolicyVersion, "source_revision": loaded.SourceRevision,
		},
	)
	if err != nil {
		return domain.OperationalState{}, fmt.Errorf("create operational state event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.OperationalState{}, fmt.Errorf("commit operational state: %w", err)
	}
	s.publish(event)
	return loaded, nil
}

func (s *Store) listOperationalStatesForTask(ctx context.Context, taskID string) ([]domain.OperationalState, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+operationalStateColumns+" FROM operational_states WHERE task_id = ? ORDER BY updated_at DESC, id",
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("list operational states: %w", err)
	}
	defer rows.Close()
	states := make([]domain.OperationalState, 0)
	for rows.Next() {
		state, scanErr := scanOperationalState(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan operational state: %w", scanErr)
		}
		states = append(states, state)
	}
	return states, rows.Err()
}
