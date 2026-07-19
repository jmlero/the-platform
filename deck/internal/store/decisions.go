package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

const decisionColumns = `
	id, task_id, run_id, question, options_json, recommendation,
	status, answer, created_at, answered_at
`

func (s *Store) ListDecisions(ctx context.Context, status string) ([]domain.Decision, error) {
	query := "SELECT " + decisionColumns + " FROM decisions"
	args := make([]any, 0, 1)
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list decisions: %w", err)
	}
	defer rows.Close()
	decisions := make([]domain.Decision, 0)
	for rows.Next() {
		decision, err := scanDecision(rows)
		if err != nil {
			return nil, fmt.Errorf("scan decision: %w", err)
		}
		decisions = append(decisions, decision)
	}
	return decisions, rows.Err()
}

func (s *Store) GetDecision(ctx context.Context, id string) (domain.Decision, error) {
	decision, err := scanDecision(s.db.QueryRowContext(ctx,
		"SELECT "+decisionColumns+" FROM decisions WHERE id = ?", id,
	))
	if err != nil {
		return domain.Decision{}, normalizeStoreError(err)
	}
	return decision, nil
}

func (s *Store) LatestDecisionForRun(ctx context.Context, runID string) (domain.Decision, error) {
	decision, err := scanDecision(s.db.QueryRowContext(ctx, `
		SELECT `+decisionColumns+` FROM decisions
		WHERE run_id = ? ORDER BY created_at DESC LIMIT 1
	`, runID))
	if err != nil {
		return domain.Decision{}, normalizeStoreError(err)
	}
	return decision, nil
}

func (s *Store) AnswerDecision(ctx context.Context, id, answer string) (domain.Decision, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return domain.Decision{}, fmt.Errorf("answer is required: %w", domain.ErrInvalidInput)
	}
	decision, err := s.GetDecision(ctx, id)
	if err != nil {
		return domain.Decision{}, err
	}
	if decision.Status != "pending" {
		return domain.Decision{}, fmt.Errorf("decision has already been answered: %w", domain.ErrConflict)
	}
	now := nowMillis()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Decision{}, fmt.Errorf("begin answer decision: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE decisions SET status = 'answered', answer = ?, answered_at = ?
		WHERE id = ? AND status = 'pending'
	`, answer, now, id)
	if err != nil {
		return domain.Decision{}, fmt.Errorf("answer decision: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return domain.Decision{}, fmt.Errorf("decision changed concurrently: %w", domain.ErrConflict)
	}
	event, err := insertEventTx(
		ctx, tx, &decision.TaskID, decision.RunID, nil,
		"decision.answered", "Human answered: "+answer,
		map[string]any{"decision_id": decision.ID, "answer": answer},
	)
	if err != nil {
		return domain.Decision{}, fmt.Errorf("create decision answer event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Decision{}, fmt.Errorf("commit decision answer: %w", err)
	}
	decision.Status = "answered"
	decision.Answer = answer
	decision.AnsweredAt = &now
	s.publish(event)
	return decision, nil
}

func (s *Store) listDecisionsForTask(ctx context.Context, taskID string) ([]domain.Decision, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+decisionColumns+` FROM decisions
		WHERE task_id = ? ORDER BY created_at DESC
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task decisions: %w", err)
	}
	defer rows.Close()
	decisions := make([]domain.Decision, 0)
	for rows.Next() {
		decision, err := scanDecision(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task decision: %w", err)
		}
		decisions = append(decisions, decision)
	}
	return decisions, rows.Err()
}

func (s *Store) listArtifactsForTask(ctx context.Context, taskID string) ([]domain.Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, run_id, name, kind, uri, content, created_at
		FROM artifacts WHERE task_id = ? ORDER BY created_at DESC
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task artifacts: %w", err)
	}
	defer rows.Close()
	artifacts := make([]domain.Artifact, 0)
	for rows.Next() {
		var artifact domain.Artifact
		if err := rows.Scan(
			&artifact.ID,
			&artifact.TaskID,
			&artifact.RunID,
			&artifact.Name,
			&artifact.Kind,
			&artifact.URI,
			&artifact.Content,
			&artifact.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan artifact: %w", err)
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func nullableString(value *string) any {
	if value == nil {
		return sql.NullString{}
	}
	return *value
}
