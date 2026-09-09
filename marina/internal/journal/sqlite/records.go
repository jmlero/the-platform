package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
)

func scanCheckpoint(source scanner) (execution.Checkpoint, error) {
	var record execution.Checkpoint
	var payload string
	var createdAt int64
	err := source.Scan(
		&record.ID,
		&record.ExecutionID,
		&record.Key,
		&record.Phase,
		&record.RuntimeHandle,
		&record.SourceRevision,
		&payload,
		&createdAt,
	)
	if err != nil {
		return execution.Checkpoint{}, wrapNotFound(err)
	}
	record.Payload = unmarshalPayload(payload)
	record.CreatedAt = time.UnixMilli(createdAt).UTC()
	return record, nil
}

func checkpointByKey(
	ctx context.Context,
	transaction *sql.Tx,
	executionID string,
	key string,
) (execution.Checkpoint, error) {
	return scanCheckpoint(transaction.QueryRowContext(ctx, `
		SELECT id, execution_id, checkpoint_key, phase, runtime_handle,
			source_revision, payload_json, created_at
		FROM checkpoints
		WHERE execution_id = ? AND checkpoint_key = ?
	`, executionID, key))
}

func validateCheckpointInput(input execution.CheckpointInput) (execution.CheckpointInput, string, error) {
	input.Key = strings.TrimSpace(input.Key)
	input.Phase = strings.TrimSpace(input.Phase)
	input.RuntimeHandle = strings.TrimSpace(input.RuntimeHandle)
	input.SourceRevision = strings.TrimSpace(input.SourceRevision)
	if input.Key == "" || input.Phase == "" {
		return execution.CheckpointInput{}, "", fmt.Errorf("checkpoint key and phase are required: %w", execution.ErrInvalidInput)
	}
	encoded, err := marshalPayload(input.Payload)
	if err != nil {
		return execution.CheckpointInput{}, "", fmt.Errorf("encode checkpoint payload: %w", err)
	}
	return input, encoded, nil
}

func checkpointMatches(existing execution.Checkpoint, input execution.CheckpointInput, encoded string) bool {
	existingPayload, err := marshalPayload(existing.Payload)
	if err != nil {
		return false
	}
	return existing.Key == input.Key &&
		existing.Phase == input.Phase &&
		existing.RuntimeHandle == input.RuntimeHandle &&
		existing.SourceRevision == input.SourceRevision &&
		existingPayload == encoded
}

func (store *Store) SaveCheckpoint(
	ctx context.Context,
	executionID string,
	expectedVersion int64,
	input execution.CheckpointInput,
) (execution.Execution, execution.Checkpoint, bool, error) {
	validated, encoded, err := validateCheckpointInput(input)
	if err != nil {
		return execution.Execution{}, execution.Checkpoint{}, false, err
	}

	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return execution.Execution{}, execution.Checkpoint{}, false, fmt.Errorf("begin checkpoint transaction: %w", err)
	}
	defer transaction.Rollback()

	record, err := executionByID(ctx, transaction, executionID)
	if err != nil {
		return execution.Execution{}, execution.Checkpoint{}, false, fmt.Errorf("load execution for checkpoint: %w", err)
	}
	existing, err := checkpointByKey(ctx, transaction, executionID, validated.Key)
	if err == nil {
		if !checkpointMatches(existing, validated, encoded) {
			return execution.Execution{}, execution.Checkpoint{}, false, fmt.Errorf("checkpoint key was reused with different content: %w", execution.ErrConflict)
		}
		if err := transaction.Commit(); err != nil {
			return execution.Execution{}, execution.Checkpoint{}, false, fmt.Errorf("commit checkpoint replay: %w", err)
		}
		return record, existing, false, nil
	}
	if !isNotFound(err) {
		return execution.Execution{}, execution.Checkpoint{}, false, fmt.Errorf("check existing checkpoint: %w", err)
	}
	if expectedVersion < 1 || record.Version != expectedVersion {
		return execution.Execution{}, execution.Checkpoint{}, false, fmt.Errorf("checkpoint version mismatch: %w", execution.ErrConflict)
	}
	if record.State.Terminal() {
		return execution.Execution{}, execution.Checkpoint{}, false, fmt.Errorf("terminal execution cannot accept checkpoints: %w", execution.ErrInvalidTransition)
	}
	if record.BudgetExpired(store.now()) {
		return execution.Execution{}, execution.Checkpoint{}, false, execution.ErrBudgetExhausted
	}

	now := store.now()
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO checkpoints (
			execution_id, checkpoint_key, phase, runtime_handle,
			source_revision, payload_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		executionID,
		validated.Key,
		validated.Phase,
		validated.RuntimeHandle,
		validated.SourceRevision,
		encoded,
		now.UnixMilli(),
	)
	if err != nil {
		return execution.Execution{}, execution.Checkpoint{}, false, fmt.Errorf("insert checkpoint: %w", err)
	}
	checkpointID, err := result.LastInsertId()
	if err != nil {
		return execution.Execution{}, execution.Checkpoint{}, false, fmt.Errorf("read checkpoint ID: %w", err)
	}
	mutation, err := transaction.ExecContext(ctx, `
		UPDATE executions
		SET runtime_handle = CASE WHEN ? <> '' THEN ? ELSE runtime_handle END,
			version = version + 1, updated_at = ?
		WHERE id = ? AND version = ? AND finished_at IS NULL
	`,
		validated.RuntimeHandle,
		validated.RuntimeHandle,
		now.UnixMilli(),
		executionID,
		expectedVersion,
	)
	if err != nil {
		return execution.Execution{}, execution.Checkpoint{}, false, fmt.Errorf("advance execution checkpoint version: %w", err)
	}
	if err := requireOneMutation(mutation); err != nil {
		return execution.Execution{}, execution.Checkpoint{}, false, err
	}
	if err := insertEvent(ctx, transaction, executionID, "execution.checkpointed", "durable execution checkpoint recorded", map[string]any{
		"checkpoint_key":  validated.Key,
		"phase":           validated.Phase,
		"source_revision": validated.SourceRevision,
		"version":         record.Version + 1,
	}, now); err != nil {
		return execution.Execution{}, execution.Checkpoint{}, false, err
	}
	checkpoint, err := scanCheckpoint(transaction.QueryRowContext(ctx, `
		SELECT id, execution_id, checkpoint_key, phase, runtime_handle,
			source_revision, payload_json, created_at
		FROM checkpoints WHERE id = ?
	`, checkpointID))
	if err != nil {
		return execution.Execution{}, execution.Checkpoint{}, false, fmt.Errorf("load inserted checkpoint: %w", err)
	}
	updated, err := executionByID(ctx, transaction, executionID)
	if err != nil {
		return execution.Execution{}, execution.Checkpoint{}, false, fmt.Errorf("load checkpointed execution: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return execution.Execution{}, execution.Checkpoint{}, false, fmt.Errorf("commit checkpoint: %w", err)
	}
	return updated, checkpoint, true, nil
}

func (store *Store) LatestCheckpoint(ctx context.Context, executionID string) (execution.Checkpoint, error) {
	record, err := scanCheckpoint(store.db.QueryRowContext(ctx, `
		SELECT id, execution_id, checkpoint_key, phase, runtime_handle,
			source_revision, payload_json, created_at
		FROM checkpoints
		WHERE execution_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, executionID))
	if err != nil {
		return execution.Checkpoint{}, fmt.Errorf("get latest checkpoint: %w", err)
	}
	return record, nil
}

func (store *Store) GetCheckpoint(ctx context.Context, executionID, key string) (execution.Checkpoint, error) {
	return scanCheckpoint(store.db.QueryRowContext(ctx, `SELECT id, execution_id, checkpoint_key, phase,
		runtime_handle, source_revision, payload_json, created_at FROM checkpoints
		WHERE execution_id = ? AND checkpoint_key = ?`, executionID, key))
}

func (store *Store) LatestPhaseCheckpoint(ctx context.Context, executionID, phase string) (execution.Checkpoint, error) {
	return scanCheckpoint(store.db.QueryRowContext(ctx, `SELECT id, execution_id, checkpoint_key, phase,
		runtime_handle, source_revision, payload_json, created_at FROM checkpoints
		WHERE execution_id = ? AND phase = ? ORDER BY id DESC LIMIT 1`, executionID, phase))
}

func scanEvidence(source scanner) (execution.Evidence, error) {
	var record execution.Evidence
	var observedAt int64
	var createdAt int64
	err := source.Scan(
		&record.ID,
		&record.ExecutionID,
		&record.Key,
		&record.Kind,
		&record.SourceRevision,
		&record.Environment,
		&record.Command,
		&record.Summary,
		&record.URI,
		&observedAt,
		&createdAt,
	)
	if err != nil {
		return execution.Evidence{}, wrapNotFound(err)
	}
	record.ObservedAt = time.UnixMilli(observedAt).UTC()
	record.CreatedAt = time.UnixMilli(createdAt).UTC()
	return record, nil
}

func evidenceByKey(
	ctx context.Context,
	transaction *sql.Tx,
	executionID string,
	key string,
) (execution.Evidence, error) {
	return scanEvidence(transaction.QueryRowContext(ctx, `
		SELECT id, execution_id, evidence_key, kind, source_revision,
			environment, command, summary, uri, observed_at, created_at
		FROM evidence
		WHERE execution_id = ? AND evidence_key = ?
	`, executionID, key))
}

func validateEvidenceInput(input execution.EvidenceInput) (execution.EvidenceInput, error) {
	input.Key = strings.TrimSpace(input.Key)
	input.Kind = strings.TrimSpace(input.Kind)
	input.SourceRevision = strings.TrimSpace(input.SourceRevision)
	input.Environment = strings.TrimSpace(input.Environment)
	input.Command = strings.TrimSpace(input.Command)
	input.Summary = strings.TrimSpace(input.Summary)
	input.URI = strings.TrimSpace(input.URI)
	if input.Key == "" || input.Kind == "" || input.SourceRevision == "" || input.Summary == "" || input.ObservedAt.IsZero() {
		return execution.EvidenceInput{}, fmt.Errorf("evidence key, kind, source revision, summary, and observation time are required: %w", execution.ErrInvalidInput)
	}
	input.ObservedAt = input.ObservedAt.UTC()
	return input, nil
}

func evidenceMatches(existing execution.Evidence, input execution.EvidenceInput) bool {
	return existing.Key == input.Key &&
		existing.Kind == input.Kind &&
		existing.SourceRevision == input.SourceRevision &&
		existing.Environment == input.Environment &&
		existing.Command == input.Command &&
		existing.Summary == input.Summary &&
		existing.URI == input.URI &&
		existing.ObservedAt.UnixMilli() == input.ObservedAt.UnixMilli()
}

func (store *Store) AddEvidence(
	ctx context.Context,
	executionID string,
	expectedVersion int64,
	input execution.EvidenceInput,
) (execution.Execution, execution.Evidence, bool, error) {
	validated, err := validateEvidenceInput(input)
	if err != nil {
		return execution.Execution{}, execution.Evidence{}, false, err
	}

	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return execution.Execution{}, execution.Evidence{}, false, fmt.Errorf("begin evidence transaction: %w", err)
	}
	defer transaction.Rollback()

	record, err := executionByID(ctx, transaction, executionID)
	if err != nil {
		return execution.Execution{}, execution.Evidence{}, false, fmt.Errorf("load execution for evidence: %w", err)
	}
	existing, err := evidenceByKey(ctx, transaction, executionID, validated.Key)
	if err == nil {
		if !evidenceMatches(existing, validated) {
			return execution.Execution{}, execution.Evidence{}, false, fmt.Errorf("evidence key was reused with different content: %w", execution.ErrConflict)
		}
		if err := transaction.Commit(); err != nil {
			return execution.Execution{}, execution.Evidence{}, false, fmt.Errorf("commit evidence replay: %w", err)
		}
		return record, existing, false, nil
	}
	if !isNotFound(err) {
		return execution.Execution{}, execution.Evidence{}, false, fmt.Errorf("check existing evidence: %w", err)
	}
	if expectedVersion < 1 || record.Version != expectedVersion {
		return execution.Execution{}, execution.Evidence{}, false, fmt.Errorf("evidence version mismatch: %w", execution.ErrConflict)
	}
	if record.State.Terminal() {
		return execution.Execution{}, execution.Evidence{}, false, fmt.Errorf("terminal execution cannot accept evidence: %w", execution.ErrInvalidTransition)
	}
	if record.BudgetExpired(store.now()) {
		return execution.Execution{}, execution.Evidence{}, false, execution.ErrBudgetExhausted
	}

	now := store.now()
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO evidence (
			execution_id, evidence_key, kind, source_revision, environment,
			command, summary, uri, observed_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		executionID,
		validated.Key,
		validated.Kind,
		validated.SourceRevision,
		validated.Environment,
		validated.Command,
		validated.Summary,
		validated.URI,
		validated.ObservedAt.UnixMilli(),
		now.UnixMilli(),
	)
	if err != nil {
		return execution.Execution{}, execution.Evidence{}, false, fmt.Errorf("insert evidence: %w", err)
	}
	evidenceID, err := result.LastInsertId()
	if err != nil {
		return execution.Execution{}, execution.Evidence{}, false, fmt.Errorf("read evidence ID: %w", err)
	}
	mutation, err := transaction.ExecContext(ctx, `
		UPDATE executions
		SET version = version + 1, updated_at = ?
		WHERE id = ? AND version = ? AND finished_at IS NULL
	`, now.UnixMilli(), executionID, expectedVersion)
	if err != nil {
		return execution.Execution{}, execution.Evidence{}, false, fmt.Errorf("advance execution evidence version: %w", err)
	}
	if err := requireOneMutation(mutation); err != nil {
		return execution.Execution{}, execution.Evidence{}, false, err
	}
	if err := insertEvent(ctx, transaction, executionID, "execution.evidence_added", "source-bound verification evidence recorded", map[string]any{
		"evidence_key":    validated.Key,
		"kind":            validated.Kind,
		"source_revision": validated.SourceRevision,
		"version":         record.Version + 1,
	}, now); err != nil {
		return execution.Execution{}, execution.Evidence{}, false, err
	}
	evidenceRecord, err := scanEvidence(transaction.QueryRowContext(ctx, `
		SELECT id, execution_id, evidence_key, kind, source_revision,
			environment, command, summary, uri, observed_at, created_at
		FROM evidence WHERE id = ?
	`, evidenceID))
	if err != nil {
		return execution.Execution{}, execution.Evidence{}, false, fmt.Errorf("load inserted evidence: %w", err)
	}
	updated, err := executionByID(ctx, transaction, executionID)
	if err != nil {
		return execution.Execution{}, execution.Evidence{}, false, fmt.Errorf("load evidenced execution: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return execution.Execution{}, execution.Evidence{}, false, fmt.Errorf("commit evidence: %w", err)
	}
	return updated, evidenceRecord, true, nil
}

func (store *Store) ListEvidence(ctx context.Context, executionID string) ([]execution.Evidence, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, execution_id, evidence_key, kind, source_revision,
			environment, command, summary, uri, observed_at, created_at
		FROM evidence
		WHERE execution_id = ?
		ORDER BY id
	`, executionID)
	if err != nil {
		return nil, fmt.Errorf("list execution evidence: %w", err)
	}
	defer rows.Close()

	records := make([]execution.Evidence, 0)
	for rows.Next() {
		record, scanErr := scanEvidence(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan execution evidence: %w", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate execution evidence: %w", err)
	}
	return records, nil
}

func scanEvent(source scanner) (execution.Event, error) {
	var record execution.Event
	var payload string
	var createdAt int64
	if err := source.Scan(
		&record.ID,
		&record.ExecutionID,
		&record.Type,
		&record.Message,
		&payload,
		&createdAt,
	); err != nil {
		return execution.Event{}, wrapNotFound(err)
	}
	record.Payload = unmarshalPayload(payload)
	record.CreatedAt = time.UnixMilli(createdAt).UTC()
	return record, nil
}

func (store *Store) ListEvents(ctx context.Context, executionID string, limit int) ([]execution.Event, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, execution_id, type, message, payload_json, created_at
		FROM events
		WHERE execution_id = ?
		ORDER BY id
		LIMIT ?
	`, executionID, normalizedLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list execution events: %w", err)
	}
	defer rows.Close()

	records := make([]execution.Event, 0)
	for rows.Next() {
		record, scanErr := scanEvent(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan execution event: %w", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate execution events: %w", err)
	}
	return records, nil
}
