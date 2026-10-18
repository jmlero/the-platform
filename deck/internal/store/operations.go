package store

import (
	"context"
	"database/sql"
	"fmt"

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
