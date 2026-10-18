package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

// RequestEngineQuestion records one operator-visible question for an exact
// pending engine operation. It does not change the run's authority or status.
func (s *Store) RequestEngineQuestion(ctx context.Context, mutation Mutation, runID string, input domain.EngineQuestion) (domain.Decision, error) {
	input.OperationID = strings.TrimSpace(input.OperationID)
	input.Question = strings.TrimSpace(input.Question)
	if input.OperationID == "" || len(input.OperationID) > 256 || input.Question == "" || len(input.Question) > 2000 ||
		input.ExpiresAt <= time.Now().UnixMilli() || len(input.Options) > 8 {
		return domain.Decision{}, fmt.Errorf("bounded engine question and future expiry are required: %w", domain.ErrInvalidInput)
	}
	seen := map[string]bool{}
	for _, option := range input.Options {
		if option != strings.TrimSpace(option) || option == "" || len(option) > 200 || seen[option] {
			return domain.Decision{}, fmt.Errorf("engine question options must be distinct and bounded: %w", domain.ErrInvalidInput)
		}
		seen[option] = true
	}
	projectID, err := s.ProjectIDForRun(ctx, runID)
	if err != nil {
		return domain.Decision{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Decision{}, err
	}
	defer tx.Rollback()
	attempt, replay, replayed, err := beginIdempotentMutation(ctx, tx, mutation, projectID, operationRequestEngineQuestion, runID,
		struct {
			RunID string
			Input domain.EngineQuestion
			Guard *RunGuard
		}{runID, input, mutation.Guard})
	if err != nil {
		return domain.Decision{}, err
	}
	if replayed {
		var original domain.Decision
		if err := decodeIdempotencyResult(replay, &original); err != nil {
			return domain.Decision{}, err
		}
		return original, nil
	}
	run, err := getRunTx(ctx, tx, runID)
	if err != nil {
		return domain.Decision{}, err
	}
	if err := validateRunGuard(run, mutation.Guard); err != nil {
		return domain.Decision{}, err
	}
	// Marina-owned runs have no Deck lease; a recorded lease must still be live.
	if run.Status != domain.RunRunning || run.LeaseExpiresAt != nil && *run.LeaseExpiresAt <= nowMillis() {
		return domain.Decision{}, fmt.Errorf("engine question requires a live running run: %w", domain.ErrConflict)
	}
	var existing string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM decisions WHERE run_id = ? AND interaction_id = ?", runID, input.OperationID).Scan(&existing); err == nil {
		return domain.Decision{}, fmt.Errorf("engine operation was already projected: %w", domain.ErrConflict)
	} else if err != sql.ErrNoRows {
		return domain.Decision{}, err
	}
	mode := "text"
	if len(input.Options) > 0 {
		mode = "options"
	}
	now := nowMillis()
	decision := domain.Decision{ID: newID("dec"), TaskID: run.TaskID, RunID: &run.ID, InteractionID: input.OperationID,
		AnswerMode: mode, Question: input.Question, Options: input.Options, RiskClass: "low", Currency: "USD",
		WaitingConsequence: "The engine waits for this exact answer.", RejectingConsequence: "Cancel the task to stop work.",
		Status: "pending", ExpiresAt: &input.ExpiresAt, CreatedAt: now}
	if _, err := tx.ExecContext(ctx, `INSERT INTO decisions (
		id, task_id, run_id, question, options_json, recommendation, risk_class,
		evidence_summary, estimated_cost_minor, currency, waiting_consequence,
		rejecting_consequence, status, answer, expires_at, created_at, answered_by,
		interaction_id, answer_mode
	) VALUES (?, ?, ?, ?, ?, '', 'low', '', 0, 'USD', ?, ?, 'pending', '', ?, ?, '', ?, ?)`,
		decision.ID, decision.TaskID, run.ID, decision.Question, marshalJSON(decision.Options),
		decision.WaitingConsequence, decision.RejectingConsequence, input.ExpiresAt, now, input.OperationID, mode); err != nil {
		return domain.Decision{}, err
	}
	event, err := insertEventTx(ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
		"decision.requested", "Engine needs an answer to continue", map[string]any{"decision_id": decision.ID, "interaction_id": input.OperationID})
	if err != nil {
		return domain.Decision{}, err
	}
	if err := finishIdempotentMutation(ctx, tx, attempt, decision); err != nil {
		return domain.Decision{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Decision{}, err
	}
	s.publish(event)
	return decision, nil
}

func closeEngineQuestionsTx(ctx context.Context, tx *sql.Tx, runID, status string) error {
	_, err := tx.ExecContext(ctx, `UPDATE decisions SET status = ?
		WHERE run_id = ? AND interaction_id <> '' AND status = 'pending'`, status, runID)
	return err
}

const decisionColumns = `
	id, task_id, run_id, plan_revision_id, question, options_json, recommendation,
	risk_class, evidence_summary, estimated_cost_minor, currency,
	waiting_consequence, rejecting_consequence,
	status, answer, expires_at, created_at, answered_at, answered_by,
	interaction_id, answer_mode
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
	if decision.ExpiresAt != nil && *decision.ExpiresAt <= now {
		if _, err := tx.ExecContext(ctx,
			"UPDATE decisions SET status = 'expired' WHERE id = ? AND status = 'pending'", id,
		); err != nil {
			return domain.Decision{}, fmt.Errorf("expire decision: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return domain.Decision{}, fmt.Errorf("commit decision expiry: %w", err)
		}
		return domain.Decision{}, fmt.Errorf("decision has expired: %w", domain.ErrConflict)
	}
	if decision.RunID != nil && decision.PlanRevisionID != "" {
		run, runErr := getRunTx(ctx, tx, *decision.RunID)
		if runErr != nil {
			return domain.Decision{}, runErr
		}
		if run.ActivePlanRevisionID == nil || *run.ActivePlanRevisionID != decision.PlanRevisionID {
			if _, err := tx.ExecContext(ctx,
				"UPDATE decisions SET status = 'superseded' WHERE id = ? AND status = 'pending'", id,
			); err != nil {
				return domain.Decision{}, fmt.Errorf("supersede stale decision: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return domain.Decision{}, fmt.Errorf("commit stale decision: %w", err)
			}
			return domain.Decision{}, fmt.Errorf("decision belongs to a superseded plan revision: %w", domain.ErrConflict)
		}
	}
	if decision.InteractionID != "" && decision.RunID != nil {
		run, runErr := getRunTx(ctx, tx, *decision.RunID)
		if runErr != nil {
			return domain.Decision{}, runErr
		}
		if run.Status != domain.RunRunning || run.LeaseExpiresAt != nil && *run.LeaseExpiresAt <= now {
			return domain.Decision{}, fmt.Errorf("engine question belongs to an inactive run: %w", domain.ErrConflict)
		}
	}
	validAnswer := false
	for _, option := range decision.Options {
		if strings.EqualFold(answer, option) {
			answer = option
			validAnswer = true
			break
		}
	}
	if decision.AnswerMode == "text" && len(answer) <= 2000 {
		validAnswer = true
	}
	if !validAnswer {
		return domain.Decision{}, fmt.Errorf("answer is not one of the available options: %w", domain.ErrInvalidInput)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE decisions SET status = 'answered', answer = ?, answered_at = ?, answered_by = 'operator'
		WHERE id = ? AND status = 'pending'
	`, answer, now, id)
	if err != nil {
		return domain.Decision{}, fmt.Errorf("answer decision: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return domain.Decision{}, fmt.Errorf("decision changed concurrently: %w", domain.ErrConflict)
	}
	eventType := "decision.answered"
	eventMessage := "Human answered: " + answer
	if strings.EqualFold(answer, "Request changes") && decision.RunID != nil && decision.PlanRevisionID != "" {
		run, runErr := getRunTx(ctx, tx, *decision.RunID)
		if runErr != nil {
			return domain.Decision{}, runErr
		}
		result, runErr := tx.ExecContext(ctx, `
			UPDATE runs SET status = 'planning', version = version + 1, updated_at = ?
			WHERE id = ? AND status = 'waiting_decision' AND active_plan_revision_id = ? AND version = ?
		`, now, run.ID, decision.PlanRevisionID, run.Version)
		if runErr != nil {
			return domain.Decision{}, fmt.Errorf("return run to planning: %w", runErr)
		}
		if rows, _ := result.RowsAffected(); rows != 1 {
			return domain.Decision{}, fmt.Errorf("run changed before the change request was applied: %w", domain.ErrConflict)
		}
		if _, runErr := tx.ExecContext(ctx,
			"UPDATE tasks SET status = 'planning', version = version + 1, updated_at = ? WHERE id = ?",
			now, run.TaskID,
		); runErr != nil {
			return domain.Decision{}, fmt.Errorf("return task to planning: %w", runErr)
		}
		if _, runErr := tx.ExecContext(ctx,
			"UPDATE agents SET status = 'active', last_seen_at = ? WHERE id = ?", now, run.AgentID,
		); runErr != nil {
			return domain.Decision{}, fmt.Errorf("wake agent for replanning: %w", runErr)
		}
		eventType = "plan.changes_requested"
		eventMessage = "Human requested a new plan revision"
	}
	event, err := insertEventTx(
		ctx, tx, &decision.TaskID, decision.RunID, nil,
		eventType, eventMessage,
		map[string]any{"decision_id": decision.ID, "plan_revision_id": decision.PlanRevisionID, "answer": answer},
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
	decision.AnsweredBy = "operator"
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
		SELECT id, task_id, run_id, name, kind, uri, content, digest, size_bytes, created_at
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
			&artifact.Digest,
			&artifact.SizeBytes,
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

func nullableInt64(value *int64) any {
	if value == nil {
		return sql.NullInt64{}
	}
	return *value
}
