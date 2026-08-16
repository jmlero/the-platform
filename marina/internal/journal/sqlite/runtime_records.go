package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
)

const runtimeAttemptColumns = `
	id, execution_id, attempt_number, idempotency_key, specification_json,
	specification_digest, provider_handle, state, heartbeat_at, usage_json,
	output_refs_json, deadline_at, cleanup_state, outputs_collected,
	failure_class, failure_message, version, created_at, updated_at
`

func scanRuntimeAttempt(source scanner) (runtimeapi.Attempt, error) {
	var attempt runtimeapi.Attempt
	var specificationJSON, state, usageJSON, outputJSON, cleanupState string
	var heartbeat sql.NullInt64
	var deadline, created, updated int64
	var outputsCollected int
	if err := source.Scan(
		&attempt.ID, &attempt.ExecutionID, &attempt.Number, &attempt.IdempotencyKey,
		&specificationJSON, &attempt.SpecificationHash, &attempt.ProviderHandle, &state,
		&heartbeat, &usageJSON, &outputJSON, &deadline, &cleanupState, &outputsCollected,
		&attempt.FailureClass, &attempt.FailureMessage, &attempt.Version, &created, &updated,
	); err != nil {
		return runtimeapi.Attempt{}, wrapNotFound(err)
	}
	if err := json.Unmarshal([]byte(specificationJSON), &attempt.Specification); err != nil {
		return runtimeapi.Attempt{}, fmt.Errorf("decode runtime specification: %w", err)
	}
	if err := json.Unmarshal([]byte(usageJSON), &attempt.Usage); err != nil {
		return runtimeapi.Attempt{}, fmt.Errorf("decode runtime usage: %w", err)
	}
	if err := json.Unmarshal([]byte(outputJSON), &attempt.OutputReferences); err != nil {
		return runtimeapi.Attempt{}, fmt.Errorf("decode runtime outputs: %w", err)
	}
	attempt.State = runtimeapi.State(state)
	attempt.CleanupState = runtimeapi.CleanupState(cleanupState)
	attempt.OutputsCollected = outputsCollected == 1
	attempt.DeadlineAt = time.UnixMilli(deadline).UTC()
	attempt.CreatedAt = time.UnixMilli(created).UTC()
	attempt.UpdatedAt = time.UnixMilli(updated).UTC()
	if heartbeat.Valid {
		value := time.UnixMilli(heartbeat.Int64).UTC()
		attempt.HeartbeatAt = &value
	}
	return attempt, nil
}

func runtimeAttemptByID(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (runtimeapi.Attempt, error) {
	return scanRuntimeAttempt(queryer.QueryRowContext(ctx,
		"SELECT "+runtimeAttemptColumns+" FROM runtime_attempts WHERE id = ?", id,
	))
}

func runtimeAttemptByKey(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, key string) (runtimeapi.Attempt, error) {
	return scanRuntimeAttempt(queryer.QueryRowContext(ctx,
		"SELECT "+runtimeAttemptColumns+" FROM runtime_attempts WHERE idempotency_key = ?", key,
	))
}

func (store *Store) EnsureRuntimeAttempt(
	ctx context.Context,
	executionID string,
	expectedExecutionVersion int64,
	specification runtimeapi.Specification,
) (execution.Execution, runtimeapi.Attempt, bool, error) {
	executionID = strings.TrimSpace(executionID)
	specification.IdempotencyKey = strings.TrimSpace(specification.IdempotencyKey)
	if executionID == "" || specification.IdempotencyKey == "" {
		return execution.Execution{}, runtimeapi.Attempt{}, false, execution.ErrInvalidInput
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return execution.Execution{}, runtimeapi.Attempt{}, false, fmt.Errorf("begin runtime attempt: %w", err)
	}
	defer transaction.Rollback()
	record, err := executionByID(ctx, transaction, executionID)
	if err != nil {
		return execution.Execution{}, runtimeapi.Attempt{}, false, err
	}
	existing, err := runtimeAttemptByKey(ctx, transaction, specification.IdempotencyKey)
	if err == nil {
		candidate := specification
		candidate.ExecutionID = existing.ExecutionID
		candidate.AttemptID = existing.ID
		digest, digestErr := candidate.Digest()
		if digestErr != nil {
			return execution.Execution{}, runtimeapi.Attempt{}, false, digestErr
		}
		if existing.ExecutionID != executionID || existing.SpecificationHash != digest {
			return execution.Execution{}, runtimeapi.Attempt{}, false, execution.ErrConflict
		}
		if err := transaction.Commit(); err != nil {
			return execution.Execution{}, runtimeapi.Attempt{}, false, err
		}
		return record, existing, false, nil
	}
	if !errors.Is(err, execution.ErrNotFound) {
		return execution.Execution{}, runtimeapi.Attempt{}, false, err
	}
	if expectedExecutionVersion < 1 || record.Version != expectedExecutionVersion || record.State.Terminal() {
		return execution.Execution{}, runtimeapi.Attempt{}, false, execution.ErrConflict
	}
	if record.BudgetExpired(store.now()) {
		return execution.Execution{}, runtimeapi.Attempt{}, false, execution.ErrBudgetExhausted
	}
	id, err := newID("rtm")
	if err != nil {
		return execution.Execution{}, runtimeapi.Attempt{}, false, err
	}
	specification.ExecutionID = executionID
	specification.AttemptID = id
	if specification.AuthorityFence != record.FenceToken || specification.DeadlineAt.After(record.DeadlineAt) {
		return execution.Execution{}, runtimeapi.Attempt{}, false, fmt.Errorf("runtime authority exceeds the execution: %w", execution.ErrConflict)
	}
	if err := specification.Validate(store.now()); err != nil {
		return execution.Execution{}, runtimeapi.Attempt{}, false, fmt.Errorf("validate runtime specification: %w", err)
	}
	digest, err := specification.Digest()
	if err != nil {
		return execution.Execution{}, runtimeapi.Attempt{}, false, err
	}
	encoded, err := json.Marshal(specification)
	if err != nil {
		return execution.Execution{}, runtimeapi.Attempt{}, false, fmt.Errorf("encode runtime specification: %w", err)
	}
	now := store.now()
	var number int
	if err := transaction.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(attempt_number), 0) + 1 FROM runtime_attempts WHERE execution_id = ?",
		executionID,
	).Scan(&number); err != nil {
		return execution.Execution{}, runtimeapi.Attempt{}, false, fmt.Errorf("number runtime attempt: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO runtime_attempts (
			id, execution_id, attempt_number, idempotency_key, specification_json,
			specification_digest, state, deadline_at, cleanup_state, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, 'pending', ?, ?)
	`, id, executionID, number, specification.IdempotencyKey, string(encoded), digest,
		specification.DeadlineAt.UnixMilli(), now.UnixMilli(), now.UnixMilli()); err != nil {
		return execution.Execution{}, runtimeapi.Attempt{}, false, fmt.Errorf("insert runtime attempt: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
		UPDATE executions SET attempt = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ? AND finished_at IS NULL
	`, max(record.Attempt, number), now.UnixMilli(), executionID, expectedExecutionVersion)
	if err != nil {
		return execution.Execution{}, runtimeapi.Attempt{}, false, fmt.Errorf("advance execution runtime attempt: %w", err)
	}
	if err := requireOneMutation(result); err != nil {
		return execution.Execution{}, runtimeapi.Attempt{}, false, err
	}
	if err := insertEvent(ctx, transaction, executionID, "runtime.attempt_created", "immutable runtime attempt persisted before provider start", map[string]any{
		"runtime_attempt_id": id, "attempt": number, "specification_digest": digest,
	}, now); err != nil {
		return execution.Execution{}, runtimeapi.Attempt{}, false, err
	}
	attempt, err := runtimeAttemptByID(ctx, transaction, id)
	if err != nil {
		return execution.Execution{}, runtimeapi.Attempt{}, false, err
	}
	updated, err := executionByID(ctx, transaction, executionID)
	if err != nil {
		return execution.Execution{}, runtimeapi.Attempt{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return execution.Execution{}, runtimeapi.Attempt{}, false, err
	}
	return updated, attempt, true, nil
}

func (store *Store) GetRuntimeAttempt(ctx context.Context, id string) (runtimeapi.Attempt, error) {
	attempt, err := runtimeAttemptByID(ctx, store.db, strings.TrimSpace(id))
	if err != nil {
		return runtimeapi.Attempt{}, fmt.Errorf("get runtime attempt: %w", err)
	}
	return attempt, nil
}

func (store *Store) ListRuntimeAttempts(ctx context.Context, executionID string) ([]runtimeapi.Attempt, error) {
	rows, err := store.db.QueryContext(ctx,
		"SELECT "+runtimeAttemptColumns+" FROM runtime_attempts WHERE execution_id = ? ORDER BY attempt_number",
		executionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list runtime attempts: %w", err)
	}
	defer rows.Close()
	items := make([]runtimeapi.Attempt, 0)
	for rows.Next() {
		item, scanErr := scanRuntimeAttempt(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func validateRuntimeObservation(current runtimeapi.Attempt, observation runtimeapi.Observation) error {
	if !observation.Found || !observation.State.Valid() || strings.TrimSpace(observation.ProviderHandle) == "" {
		return runtimeapi.ErrInvalidInput
	}
	if !current.State.CanObserve(observation.State) {
		return runtimeapi.ErrConflict
	}
	if current.ProviderHandle != "" && current.ProviderHandle != strings.TrimSpace(observation.ProviderHandle) {
		return runtimeapi.ErrConflict
	}
	if observation.Usage.Tokens < 0 || observation.Usage.CostMicros < 0 || observation.Usage.CPUTimeMillis < 0 {
		return runtimeapi.ErrInvalidInput
	}
	if current.HeartbeatAt != nil && observation.HeartbeatAt != nil && observation.HeartbeatAt.Before(*current.HeartbeatAt) {
		return runtimeapi.ErrConflict
	}
	for _, output := range observation.OutputReferences {
		if strings.TrimSpace(output.Kind) == "" || strings.TrimSpace(output.URI) == "" || strings.TrimSpace(output.Digest) == "" || output.SizeBytes < 0 {
			return runtimeapi.ErrInvalidInput
		}
	}
	return nil
}

// MarkRuntimeStarting persists the external start intent before the adapter is
// called. A restart can therefore inspect by the stable idempotency key instead
// of issuing a blind duplicate start after an ambiguous response.
func (store *Store) MarkRuntimeStarting(
	ctx context.Context,
	attemptID string,
	expectedVersion int64,
) (runtimeapi.Attempt, error) {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return runtimeapi.Attempt{}, err
	}
	defer transaction.Rollback()
	current, err := runtimeAttemptByID(ctx, transaction, strings.TrimSpace(attemptID))
	if err != nil {
		return runtimeapi.Attempt{}, err
	}
	if current.Version != expectedVersion || current.State != runtimeapi.StatePending {
		return runtimeapi.Attempt{}, runtimeapi.ErrConflict
	}
	now := store.now()
	result, err := transaction.ExecContext(ctx, `
		UPDATE runtime_attempts SET state = 'starting', version = version + 1, updated_at = ?
		WHERE id = ? AND version = ? AND state = 'pending'
	`, now.UnixMilli(), current.ID, expectedVersion)
	if err != nil {
		return runtimeapi.Attempt{}, fmt.Errorf("persist runtime start intent: %w", err)
	}
	if err := requireOneMutation(result); err != nil {
		return runtimeapi.Attempt{}, err
	}
	if err := insertEvent(ctx, transaction, current.ExecutionID, "runtime.starting",
		"runtime start intent persisted before provider call", map[string]any{
			"runtime_attempt_id": current.ID, "idempotency_key": current.IdempotencyKey,
		}, now); err != nil {
		return runtimeapi.Attempt{}, err
	}
	updated, err := runtimeAttemptByID(ctx, transaction, current.ID)
	if err != nil {
		return runtimeapi.Attempt{}, err
	}
	if err := transaction.Commit(); err != nil {
		return runtimeapi.Attempt{}, err
	}
	return updated, nil
}

func (store *Store) RecordRuntimeObservation(
	ctx context.Context,
	attemptID string,
	expectedVersion int64,
	observation runtimeapi.Observation,
) (runtimeapi.Attempt, error) {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return runtimeapi.Attempt{}, err
	}
	defer transaction.Rollback()
	current, err := runtimeAttemptByID(ctx, transaction, attemptID)
	if err != nil {
		return runtimeapi.Attempt{}, err
	}
	if current.Version != expectedVersion {
		return runtimeapi.Attempt{}, runtimeapi.ErrConflict
	}
	if err := validateRuntimeObservation(current, observation); err != nil {
		return runtimeapi.Attempt{}, err
	}
	usage, err := json.Marshal(observation.Usage)
	if err != nil {
		return runtimeapi.Attempt{}, err
	}
	outputs, err := json.Marshal(observation.OutputReferences)
	if err != nil {
		return runtimeapi.Attempt{}, err
	}
	var heartbeat any
	if observation.HeartbeatAt != nil {
		heartbeat = observation.HeartbeatAt.UTC().UnixMilli()
	}
	outputsCollected := current.OutputsCollected || (observation.State.Terminal() && len(observation.OutputReferences) > 0)
	now := store.now()
	result, err := transaction.ExecContext(ctx, `
		UPDATE runtime_attempts SET provider_handle = ?, state = ?, heartbeat_at = ?,
			usage_json = ?, output_refs_json = ?, outputs_collected = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?
	`, strings.TrimSpace(observation.ProviderHandle), observation.State, heartbeat, string(usage), string(outputs),
		boolInt(outputsCollected), now.UnixMilli(), attemptID, expectedVersion)
	if err != nil {
		return runtimeapi.Attempt{}, fmt.Errorf("record runtime observation: %w", err)
	}
	if err := requireOneMutation(result); err != nil {
		return runtimeapi.Attempt{}, err
	}
	if _, err := transaction.ExecContext(ctx, `
		UPDATE executions SET used_tokens = MAX(used_tokens, ?), used_cost_micros = MAX(used_cost_micros, ?),
			runtime_handle = ?, updated_at = ? WHERE id = ?
	`, observation.Usage.Tokens, observation.Usage.CostMicros, strings.TrimSpace(observation.ProviderHandle),
		now.UnixMilli(), current.ExecutionID); err != nil {
		return runtimeapi.Attempt{}, fmt.Errorf("record runtime usage: %w", err)
	}
	if err := insertEvent(ctx, transaction, current.ExecutionID, "runtime.observed", "runtime provider observation persisted", map[string]any{
		"runtime_attempt_id": attemptID, "state": observation.State, "provider_handle": observation.ProviderHandle,
	}, now); err != nil {
		return runtimeapi.Attempt{}, err
	}
	updated, err := runtimeAttemptByID(ctx, transaction, attemptID)
	if err != nil {
		return runtimeapi.Attempt{}, err
	}
	if err := transaction.Commit(); err != nil {
		return runtimeapi.Attempt{}, err
	}
	return updated, nil
}

func (store *Store) MarkRuntimeCleanup(ctx context.Context, attemptID string, expectedVersion int64, state runtimeapi.CleanupState, message string) (runtimeapi.Attempt, error) {
	if !state.Valid() || strings.TrimSpace(message) == "" {
		return runtimeapi.Attempt{}, runtimeapi.ErrInvalidInput
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return runtimeapi.Attempt{}, err
	}
	defer transaction.Rollback()
	current, err := runtimeAttemptByID(ctx, transaction, attemptID)
	if err != nil {
		return runtimeapi.Attempt{}, err
	}
	if current.Version != expectedVersion || !current.State.Terminal() {
		return runtimeapi.Attempt{}, runtimeapi.ErrConflict
	}
	now := store.now()
	result, err := transaction.ExecContext(ctx, `
		UPDATE runtime_attempts SET cleanup_state = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?
	`, state, now.UnixMilli(), attemptID, expectedVersion)
	if err != nil {
		return runtimeapi.Attempt{}, err
	}
	if err := requireOneMutation(result); err != nil {
		return runtimeapi.Attempt{}, err
	}
	if err := insertEvent(ctx, transaction, current.ExecutionID, "runtime.cleanup", strings.TrimSpace(message), map[string]any{
		"runtime_attempt_id": attemptID, "cleanup_state": state,
	}, now); err != nil {
		return runtimeapi.Attempt{}, err
	}
	updated, err := runtimeAttemptByID(ctx, transaction, attemptID)
	if err != nil {
		return runtimeapi.Attempt{}, err
	}
	if err := transaction.Commit(); err != nil {
		return runtimeapi.Attempt{}, err
	}
	return updated, nil
}

func scanRuntimeArtifact(source scanner) (runtimeapi.Artifact, error) {
	var artifact runtimeapi.Artifact
	var created int64
	if err := source.Scan(&artifact.ID, &artifact.ExecutionID, &artifact.RuntimeAttemptID,
		&artifact.Name, &artifact.Kind, &artifact.URI, &artifact.Digest, &artifact.SizeBytes, &created); err != nil {
		return runtimeapi.Artifact{}, wrapNotFound(err)
	}
	artifact.CreatedAt = time.UnixMilli(created).UTC()
	return artifact, nil
}

func (store *Store) RecordRuntimeArtifact(ctx context.Context, artifact runtimeapi.Artifact) (runtimeapi.Artifact, bool, error) {
	artifact.ID = strings.TrimSpace(artifact.ID)
	artifact.ExecutionID = strings.TrimSpace(artifact.ExecutionID)
	artifact.RuntimeAttemptID = strings.TrimSpace(artifact.RuntimeAttemptID)
	artifact.Name = strings.TrimSpace(artifact.Name)
	artifact.Kind = strings.TrimSpace(artifact.Kind)
	artifact.URI = strings.TrimSpace(artifact.URI)
	artifact.Digest = strings.TrimSpace(artifact.Digest)
	if artifact.ID == "" || artifact.ExecutionID == "" || artifact.RuntimeAttemptID == "" || artifact.Name == "" ||
		artifact.Kind == "" || artifact.URI == "" || artifact.Digest == "" || artifact.SizeBytes < 0 {
		return runtimeapi.Artifact{}, false, runtimeapi.ErrInvalidInput
	}
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = store.now()
	}
	result, err := store.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO runtime_artifacts (
			id, execution_id, runtime_attempt_id, name, kind, uri, digest, size_bytes, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, artifact.ID, artifact.ExecutionID, artifact.RuntimeAttemptID, artifact.Name, artifact.Kind,
		artifact.URI, artifact.Digest, artifact.SizeBytes, artifact.CreatedAt.UTC().UnixMilli())
	if err != nil {
		return runtimeapi.Artifact{}, false, fmt.Errorf("record runtime artifact: %w", err)
	}
	created, _ := result.RowsAffected()
	loaded, err := scanRuntimeArtifact(store.db.QueryRowContext(ctx, `
		SELECT id, execution_id, runtime_attempt_id, name, kind, uri, digest, size_bytes, created_at
		FROM runtime_artifacts WHERE id = ?
	`, artifact.ID))
	if err != nil {
		return runtimeapi.Artifact{}, false, err
	}
	if loaded.ExecutionID != artifact.ExecutionID || loaded.RuntimeAttemptID != artifact.RuntimeAttemptID ||
		loaded.Name != artifact.Name || loaded.Kind != artifact.Kind || loaded.URI != artifact.URI ||
		loaded.Digest != artifact.Digest || loaded.SizeBytes != artifact.SizeBytes {
		return runtimeapi.Artifact{}, false, runtimeapi.ErrConflict
	}
	return loaded, created == 1, nil
}

func (store *Store) ListRuntimeArtifacts(ctx context.Context, executionID string) ([]runtimeapi.Artifact, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, execution_id, runtime_attempt_id, name, kind, uri, digest, size_bytes, created_at
		FROM runtime_artifacts WHERE execution_id = ? ORDER BY created_at, id
	`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]runtimeapi.Artifact, 0)
	for rows.Next() {
		item, scanErr := scanRuntimeArtifact(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
