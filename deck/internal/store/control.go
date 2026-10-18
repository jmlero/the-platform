package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

const commandColumns = `
	id, run_id, sequence, type, status, reason, requested_by, result,
	run_version, fence_token, created_at, updated_at
`

func scanCommand(source interface{ Scan(...any) error }) (domain.Command, error) {
	var command domain.Command
	err := source.Scan(&command.ID, &command.RunID, &command.Sequence, &command.Type,
		&command.Status, &command.Reason, &command.RequestedBy, &command.Result,
		&command.RunVersion, &command.FenceToken, &command.CreatedAt, &command.UpdatedAt)
	return command, err
}

func validCommandType(value string) bool {
	switch value {
	case domain.CommandPause, domain.CommandResume, domain.CommandCancel,
		domain.CommandReplan, domain.CommandRetry, domain.CommandReduceAuthority:
		return true
	default:
		return false
	}
}

func (s *Store) CreateCommand(ctx context.Context, runID, commandType, reason, requestedBy string) (domain.Command, error) {
	commandType = strings.TrimSpace(commandType)
	reason = strings.TrimSpace(reason)
	requestedBy = strings.TrimSpace(requestedBy)
	if !validCommandType(commandType) || reason == "" || requestedBy == "" {
		return domain.Command{}, fmt.Errorf("command type, reason, and requester are required: %w", domain.ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Command{}, fmt.Errorf("begin command: %w", err)
	}
	defer tx.Rollback()
	run, err := getRunTx(ctx, tx, runID)
	if err != nil {
		return domain.Command{}, err
	}
	if run.Status == domain.RunCompleted && commandType != domain.CommandRetry {
		return domain.Command{}, fmt.Errorf("completed run accepts only retry: %w", domain.ErrInvalidTransition)
	}
	var sequence int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence), 0) + 1 FROM commands WHERE run_id = ?", runID).Scan(&sequence); err != nil {
		return domain.Command{}, fmt.Errorf("allocate command sequence: %w", err)
	}
	now := nowMillis()
	nextVersion := run.Version
	nextFence := run.FenceToken
	if !runFinished(run) {
		nextVersion++
		switch commandType {
		case domain.CommandPause, domain.CommandCancel, domain.CommandReplan, domain.CommandReduceAuthority:
			nextFence++
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE runs SET version = ?, fence_token = ?, updated_at = ?
			WHERE id = ? AND version = ?
		`, nextVersion, nextFence, now, run.ID, run.Version)
		if err != nil {
			return domain.Command{}, fmt.Errorf("fence run for command: %w", err)
		}
		if rows, _ := result.RowsAffected(); rows != 1 {
			return domain.Command{}, domain.ErrConflict
		}
	}
	command := domain.Command{
		ID: newID("cmd"), RunID: run.ID, Sequence: sequence, Type: commandType,
		Status: "pending", Reason: reason, RequestedBy: requestedBy,
		RunVersion: nextVersion, FenceToken: nextFence, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO commands (id, run_id, sequence, type, status, reason, requested_by,
			result, run_version, fence_token, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'pending', ?, ?, '', ?, ?, ?, ?)
	`, command.ID, command.RunID, command.Sequence, command.Type, command.Reason,
		command.RequestedBy, command.RunVersion, command.FenceToken, now, now); err != nil {
		return domain.Command{}, fmt.Errorf("insert command: %w", err)
	}
	event, err := insertEventTx(ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
		"command.requested", "Operator requested "+command.Type,
		map[string]any{"command_id": command.ID, "sequence": command.Sequence, "reason": command.Reason,
			"run_version": command.RunVersion, "fence_token": command.FenceToken})
	if err != nil {
		return domain.Command{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Command{}, fmt.Errorf("commit command: %w", err)
	}
	s.publish(event)
	return command, nil
}

func runFinished(run domain.Run) bool {
	switch run.Status {
	case domain.RunCompleted, domain.RunBlocked, domain.RunFailed, domain.RunCancelled:
		return true
	default:
		return false
	}
}

func (s *Store) ListCommands(ctx context.Context, runID string, afterSequence int64, limit int) ([]domain.Command, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+commandColumns+" FROM commands WHERE run_id = ? AND sequence > ? ORDER BY sequence LIMIT ?", runID, afterSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("list commands: %w", err)
	}
	defer rows.Close()
	commands := make([]domain.Command, 0)
	for rows.Next() {
		command, scanErr := scanCommand(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan command: %w", scanErr)
		}
		commands = append(commands, command)
	}
	return commands, rows.Err()
}

func (s *Store) AcknowledgeCommand(ctx context.Context, mutation Mutation, runID, commandID, status, resultText string) error {
	projectID, err := s.ProjectIDForRun(ctx, runID)
	if err != nil {
		return err
	}
	status = strings.TrimSpace(status)
	resultText = strings.TrimSpace(resultText)
	if status != "acknowledged" && status != "rejected" && status != "completed" {
		return fmt.Errorf("invalid command acknowledgement status: %w", domain.ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin command acknowledgement: %w", err)
	}
	defer tx.Rollback()
	attempt, replayResult, replayed, err := beginIdempotentMutation(ctx, tx, mutation, projectID,
		operationAcknowledgeCommand, commandID, map[string]any{"run_id": runID, "command_id": commandID,
			"status": status, "result": resultText, "guard": mutation.Guard})
	if err != nil {
		return err
	}
	if replayed {
		var original struct{}
		return decodeIdempotencyResult(replayResult, &original)
	}
	run, err := getRunTx(ctx, tx, runID)
	if err != nil {
		return err
	}
	if err := validateRunGuard(run, mutation.Guard); err != nil {
		return err
	}
	command, err := scanCommand(tx.QueryRowContext(ctx, "SELECT "+commandColumns+" FROM commands WHERE id = ? AND run_id = ?", commandID, runID))
	if err != nil {
		return normalizeStoreError(err)
	}
	if command.Status != "pending" && command.Status != "acknowledged" {
		return fmt.Errorf("command is already terminal: %w", domain.ErrConflict)
	}
	now := nowMillis()
	if _, err := tx.ExecContext(ctx, "UPDATE commands SET status = ?, result = ?, updated_at = ? WHERE id = ?", status, resultText, now, commandID); err != nil {
		return fmt.Errorf("update command: %w", err)
	}
	nextStatus := run.Status
	finishedAt := any(nil)
	lease := any(nil)
	if command.Type == domain.CommandCancel && status == "completed" {
		nextStatus = domain.RunCancelled
		finishedAt = now
	} else {
		if run.LeaseExpiresAt != nil {
			lease = *run.LeaseExpiresAt
		}
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE runs SET status = ?, version = version + 1, lease_expires_at = ?,
			finished_at = COALESCE(?, finished_at), updated_at = ?
		WHERE id = ? AND version = ? AND fence_token = ?
	`, nextStatus, lease, finishedAt, now, run.ID, run.Version, run.FenceToken)
	if err != nil {
		return fmt.Errorf("advance run after command: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return domain.ErrConflict
	}
	if nextStatus == domain.RunCancelled {
		if err := closeEngineQuestionsTx(ctx, tx, runID, "cancelled"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status = 'cancelled', assigned_agent_id = NULL, version = version + 1, updated_at = ? WHERE id = ?", now, run.TaskID); err != nil {
			return fmt.Errorf("cancel task: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE agents SET status = 'idle', last_seen_at = ? WHERE id = ?", now, run.AgentID); err != nil {
			return fmt.Errorf("idle cancelled agent: %w", err)
		}
	}
	event, err := insertEventTx(ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
		"command."+status, "Command "+command.Type+" "+status,
		map[string]any{"command_id": command.ID, "sequence": command.Sequence, "result": resultText})
	if err != nil {
		return err
	}
	if err := finishIdempotentMutation(ctx, tx, attempt, struct{}{}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit command acknowledgement: %w", err)
	}
	s.publish(event)
	return nil
}

func (s *Store) AppendProjectedEvent(ctx context.Context, mutation Mutation, runID string, projected domain.ProjectedEvent) error {
	projectID, err := s.ProjectIDForRun(ctx, runID)
	if err != nil {
		return err
	}
	projected.Producer = strings.TrimSpace(projected.Producer)
	projected.ProducerEventID = strings.TrimSpace(projected.ProducerEventID)
	projected.ExecutionID = strings.TrimSpace(projected.ExecutionID)
	projected.Type = strings.TrimSpace(projected.Type)
	projected.Message = strings.TrimSpace(projected.Message)
	if projected.Producer == "" || projected.ProducerEventID == "" || projected.ExecutionID == "" || projected.Type == "" || projected.Sequence < 1 {
		return fmt.Errorf("projected event identity, sequence, and type are required: %w", domain.ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin projected event: %w", err)
	}
	defer tx.Rollback()
	attempt, replayResult, replayed, err := beginIdempotentMutation(ctx, tx, mutation, projectID,
		operationAppendProjectedEvent, projected.ProducerEventID, map[string]any{"run_id": runID, "event": projected, "guard": mutation.Guard})
	if err != nil {
		return err
	}
	if replayed {
		var original struct{}
		if err := decodeIdempotencyResult(replayResult, &original); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return s.RecordProjectionAccepted(ctx, projectID, projected.Producer, projected.OccurredAt)
	}
	run, err := getRunTx(ctx, tx, runID)
	if err != nil {
		return err
	}
	if err := validateRunGuard(run, mutation.Guard); err != nil {
		return err
	}
	encoded := marshalJSON(projected.Payload)
	var existingRunID, existingExecutionID, existingType, existingMessage, existingPayload string
	var existingSequence, existingOccurredAt int64
	existingErr := tx.QueryRowContext(ctx, `SELECT run_id, execution_id, sequence, type, message, payload_json, occurred_at
		FROM projected_events WHERE producer = ? AND producer_event_id = ?`, projected.Producer, projected.ProducerEventID).
		Scan(&existingRunID, &existingExecutionID, &existingSequence, &existingType, &existingMessage, &existingPayload, &existingOccurredAt)
	if existingErr == nil {
		if existingRunID != runID || existingExecutionID != projected.ExecutionID || existingSequence != projected.Sequence ||
			existingType != projected.Type || existingMessage != projected.Message || existingPayload != encoded || existingOccurredAt != projected.OccurredAt {
			return fmt.Errorf("producer event ID was reused with different content: %w", domain.ErrConflict)
		}
		if err := finishIdempotentMutation(ctx, tx, attempt, struct{}{}); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return s.RecordProjectionAccepted(ctx, projectID, projected.Producer, projected.OccurredAt)
	}
	if !errors.Is(existingErr, sql.ErrNoRows) {
		return fmt.Errorf("inspect projected event replay: %w", existingErr)
	}
	var latestSequence int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) FROM projected_events
		WHERE producer = ? AND execution_id = ?`, projected.Producer, projected.ExecutionID).Scan(&latestSequence); err != nil {
		return fmt.Errorf("inspect projected event sequence: %w", err)
	}
	if projected.Sequence <= latestSequence {
		return fmt.Errorf("projected event sequence must advance beyond %d: %w", latestSequence, domain.ErrConflict)
	}
	now := nowMillis()
	projected.ID = newID("pev")
	projected.IngestedAt = now
	result, err := tx.ExecContext(ctx, `
		INSERT INTO projected_events (id, run_id, producer, producer_event_id, execution_id,
			sequence, type, message, payload_json, occurred_at, ingested_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, projected.ID, runID, projected.Producer, projected.ProducerEventID, projected.ExecutionID,
		projected.Sequence, projected.Type, projected.Message, encoded, projected.OccurredAt, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("projected event identity or sequence already exists: %w", domain.ErrConflict)
		}
		return fmt.Errorf("append projected event: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return domain.ErrConflict
	}
	event, err := insertEventTx(ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
		projected.Type, projected.Message, projected.Payload)
	if err != nil {
		return err
	}
	update, err := tx.ExecContext(ctx, "UPDATE runs SET version = version + 1, updated_at = ? WHERE id = ? AND version = ?", now, run.ID, run.Version)
	if err != nil {
		return fmt.Errorf("advance run after projection: %w", err)
	}
	if rows, _ := update.RowsAffected(); rows != 1 {
		return domain.ErrConflict
	}
	if err := finishIdempotentMutation(ctx, tx, attempt, struct{}{}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit projected event: %w", err)
	}
	s.publish(event)
	return s.RecordProjectionAccepted(ctx, projectID, projected.Producer, projected.OccurredAt)
}
