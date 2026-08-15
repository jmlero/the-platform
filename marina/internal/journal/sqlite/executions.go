package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
)

const executionColumns = `
	id, work_provider, project_id, external_work_id, external_run_id, agent_id,
	external_run_version, fence_token, task_version, task_digest, binding_version, state,
	attempt, lease_expires_at, deadline_at, max_attempts, max_tokens,
	max_cost_micros, used_tokens, used_cost_micros, runtime_handle,
	failure_class, failure_message, command_cursor, retry_attempt, next_eligible_at,
	last_error_class, exhaustion_reason, version, created_at, updated_at, finished_at
`

type scanner interface {
	Scan(...any) error
}

func scanExecution(source scanner) (execution.Execution, error) {
	var record execution.Execution
	var state string
	var failureClass string
	var leaseExpiresAt int64
	var deadlineAt int64
	var createdAt int64
	var updatedAt int64
	var finishedAt sql.NullInt64
	var nextEligibleAt sql.NullInt64
	err := source.Scan(
		&record.ID,
		&record.WorkProvider,
		&record.ProjectID,
		&record.ExternalWorkID,
		&record.ExternalRunID,
		&record.AgentID,
		&record.ExternalRunVersion,
		&record.FenceToken,
		&record.TaskVersion,
		&record.TaskDigest,
		&record.BindingVersion,
		&state,
		&record.Attempt,
		&leaseExpiresAt,
		&deadlineAt,
		&record.MaxAttempts,
		&record.MaxTokens,
		&record.MaxCostMicros,
		&record.UsedTokens,
		&record.UsedCostMicros,
		&record.RuntimeHandle,
		&failureClass,
		&record.FailureMessage,
		&record.CommandCursor,
		&record.RetryAttempt,
		&nextEligibleAt,
		&record.LastErrorClass,
		&record.ExhaustionReason,
		&record.Version,
		&createdAt,
		&updatedAt,
		&finishedAt,
	)
	if err != nil {
		return execution.Execution{}, wrapNotFound(err)
	}
	record.State = execution.State(state)
	record.FailureClass = execution.FailureClass(failureClass)
	record.LeaseExpiresAt = time.UnixMilli(leaseExpiresAt).UTC()
	record.DeadlineAt = time.UnixMilli(deadlineAt).UTC()
	record.CreatedAt = time.UnixMilli(createdAt).UTC()
	record.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	if finishedAt.Valid {
		value := time.UnixMilli(finishedAt.Int64).UTC()
		record.FinishedAt = &value
	}
	if nextEligibleAt.Valid {
		value := time.UnixMilli(nextEligibleAt.Int64).UTC()
		record.NextEligibleAt = &value
	}
	return record, nil
}

func executionByID(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, executionID string) (execution.Execution, error) {
	return scanExecution(queryer.QueryRowContext(ctx,
		"SELECT "+executionColumns+" FROM executions WHERE id = ?",
		executionID,
	))
}

func validateNewExecution(input execution.NewExecution, now time.Time) (execution.NewExecution, error) {
	input.WorkProvider = strings.TrimSpace(input.WorkProvider)
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.ExternalWorkID = strings.TrimSpace(input.ExternalWorkID)
	input.ExternalRunID = strings.TrimSpace(input.ExternalRunID)
	input.AgentID = strings.TrimSpace(input.AgentID)
	input.TaskDigest = strings.TrimSpace(input.TaskDigest)
	if input.ProjectID == "" {
		input.ProjectID = "prj_default"
	}
	if input.ExternalRunVersion == 0 {
		input.ExternalRunVersion = 1
	}
	if input.FenceToken == 0 {
		input.FenceToken = 1
	}
	if input.TaskVersion == 0 {
		input.TaskVersion = 1
	}
	if input.TaskDigest == "" && input.ExternalWorkID != "" {
		input.TaskDigest = "legacy:" + input.ExternalWorkID
	}
	if input.WorkProvider == "" || input.ProjectID == "" || input.ExternalWorkID == "" || input.ExternalRunID == "" || input.AgentID == "" || input.TaskDigest == "" {
		return execution.NewExecution{}, fmt.Errorf("provider, work ID, run ID, and agent ID are required: %w", execution.ErrInvalidInput)
	}
	if input.ExternalRunVersion < 1 || input.FenceToken < 1 || input.TaskVersion < 1 {
		return execution.NewExecution{}, fmt.Errorf("provider authority and task version must be positive: %w", execution.ErrInvalidInput)
	}
	if input.BindingVersion == 0 {
		input.BindingVersion = 1
	}
	if input.LeaseExpiresAt.IsZero() || !input.LeaseExpiresAt.After(now) {
		return execution.NewExecution{}, fmt.Errorf("lease expiry must be in the future: %w", execution.ErrInvalidInput)
	}
	if input.Budget.DeadlineAt.IsZero() || !input.Budget.DeadlineAt.After(now) {
		return execution.NewExecution{}, fmt.Errorf("budget deadline must be in the future: %w", execution.ErrInvalidInput)
	}
	if input.Budget.MaxAttempts < 1 || input.Budget.MaxTokens < 0 || input.Budget.MaxCostMicros < 0 {
		return execution.NewExecution{}, fmt.Errorf("execution limits must be non-negative and attempts must be positive: %w", execution.ErrInvalidInput)
	}
	if input.Budget.MaxTokens == 0 && input.Budget.MaxCostMicros == 0 {
		return execution.NewExecution{}, fmt.Errorf("a token or cost ceiling is required: %w", execution.ErrInvalidInput)
	}
	input.LeaseExpiresAt = input.LeaseExpiresAt.UTC()
	input.Budget.DeadlineAt = input.Budget.DeadlineAt.UTC()
	return input, nil
}

func (store *Store) EnsureExecution(
	ctx context.Context,
	input execution.NewExecution,
) (execution.Execution, bool, error) {
	now := store.now()
	validated, err := validateNewExecution(input, now)
	if err != nil {
		return execution.Execution{}, false, err
	}
	executionID, err := newID("exec")
	if err != nil {
		return execution.Execution{}, false, err
	}

	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return execution.Execution{}, false, fmt.Errorf("begin execution transaction: %w", err)
	}
	defer transaction.Rollback()

	result, err := transaction.ExecContext(ctx, `
		INSERT OR IGNORE INTO executions (
			id, work_provider, project_id, external_work_id, external_run_id, agent_id,
			external_run_version, fence_token, task_version, task_digest, binding_version, state,
			attempt, lease_expires_at, deadline_at, max_attempts, max_tokens,
			max_cost_micros, version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, 1, ?, ?)
	`,
		executionID,
		validated.WorkProvider,
		validated.ProjectID,
		validated.ExternalWorkID,
		validated.ExternalRunID,
		validated.AgentID,
		validated.ExternalRunVersion,
		validated.FenceToken,
		validated.TaskVersion,
		validated.TaskDigest,
		validated.BindingVersion,
		execution.StatePlanning,
		validated.LeaseExpiresAt.UnixMilli(),
		validated.Budget.DeadlineAt.UnixMilli(),
		validated.Budget.MaxAttempts,
		validated.Budget.MaxTokens,
		validated.Budget.MaxCostMicros,
		now.UnixMilli(),
		now.UnixMilli(),
	)
	if err != nil {
		return execution.Execution{}, false, fmt.Errorf("insert execution: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return execution.Execution{}, false, fmt.Errorf("inspect execution insert: %w", err)
	}

	if rowsAffected == 0 {
		existing, queryErr := scanExecution(transaction.QueryRowContext(ctx,
			"SELECT "+executionColumns+" FROM executions WHERE work_provider = ? AND external_run_id = ?",
			validated.WorkProvider,
			validated.ExternalRunID,
		))
		if queryErr != nil {
			return execution.Execution{}, false, fmt.Errorf("load existing execution: %w", queryErr)
		}
		if existing.ProjectID != validated.ProjectID || existing.ExternalWorkID != validated.ExternalWorkID || existing.AgentID != validated.AgentID || existing.TaskVersion != validated.TaskVersion || existing.TaskDigest != validated.TaskDigest || existing.BindingVersion != validated.BindingVersion {
			return execution.Execution{}, false, fmt.Errorf("external run is already owned by another work item or agent: %w", execution.ErrConflict)
		}
		if err := transaction.Commit(); err != nil {
			return execution.Execution{}, false, fmt.Errorf("commit existing execution lookup: %w", err)
		}
		return existing, false, nil
	}

	if err := insertEvent(ctx, transaction, executionID, "execution.created", "execution journal initialized", map[string]any{
		"agent_id":         validated.AgentID,
		"external_run_id":  validated.ExternalRunID,
		"external_work_id": validated.ExternalWorkID,
		"state":            execution.StatePlanning,
		"version":          1,
	}, now); err != nil {
		return execution.Execution{}, false, err
	}
	record, err := executionByID(ctx, transaction, executionID)
	if err != nil {
		return execution.Execution{}, false, fmt.Errorf("load created execution: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return execution.Execution{}, false, fmt.Errorf("commit execution creation: %w", err)
	}
	return record, true, nil
}

func (store *Store) GetExecution(ctx context.Context, executionID string) (execution.Execution, error) {
	executionID = strings.TrimSpace(executionID)
	if executionID == "" {
		return execution.Execution{}, fmt.Errorf("execution ID is required: %w", execution.ErrInvalidInput)
	}
	record, err := executionByID(ctx, store.db, executionID)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("get execution: %w", err)
	}
	return record, nil
}

func (store *Store) ListActiveExecutions(ctx context.Context, limit int) ([]execution.Execution, error) {
	limit = normalizedLimit(limit)
	rows, err := store.db.QueryContext(ctx,
		"SELECT "+executionColumns+" FROM executions WHERE finished_at IS NULL ORDER BY updated_at, id LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list active executions: %w", err)
	}
	defer rows.Close()

	records := make([]execution.Execution, 0)
	for rows.Next() {
		record, scanErr := scanExecution(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan active execution: %w", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active executions: %w", err)
	}
	return records, nil
}

func (store *Store) ListCommandExecutions(ctx context.Context, limit int) ([]execution.Execution, error) {
	limit = normalizedLimit(limit)
	rows, err := store.db.QueryContext(ctx,
		"SELECT "+executionColumns+" FROM executions ORDER BY updated_at, id LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list command executions: %w", err)
	}
	defer rows.Close()
	records := make([]execution.Execution, 0)
	for rows.Next() {
		record, scanErr := scanExecution(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan command execution: %w", scanErr)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func validateTransition(record execution.Execution, input execution.Transition, now time.Time) error {
	if input.ExpectedVersion < 1 {
		return fmt.Errorf("expected version must be positive: %w", execution.ErrInvalidInput)
	}
	if record.Version != input.ExpectedVersion {
		return fmt.Errorf("expected version %d, found %d: %w", input.ExpectedVersion, record.Version, execution.ErrConflict)
	}
	if !input.Target.Valid() || !record.State.CanTransition(input.Target) {
		return fmt.Errorf("cannot move from %s to %s: %w", record.State, input.Target, execution.ErrInvalidTransition)
	}
	if strings.TrimSpace(input.Message) == "" {
		return fmt.Errorf("transition message is required: %w", execution.ErrInvalidInput)
	}
	if input.Target == execution.StateFailed {
		if !input.FailureClass.Valid() || strings.TrimSpace(input.FailureMessage) == "" {
			return fmt.Errorf("failed transitions require a classified failure: %w", execution.ErrInvalidInput)
		}
	} else if input.FailureClass != "" && !input.FailureClass.Valid() {
		return fmt.Errorf("failure class is invalid: %w", execution.ErrInvalidInput)
	}
	if input.Target != execution.StateFailed && !input.Target.Terminal() && record.BudgetExpired(now) {
		return execution.ErrBudgetExhausted
	}
	return nil
}

func (store *Store) Transition(
	ctx context.Context,
	executionID string,
	input execution.Transition,
) (execution.Execution, error) {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("begin transition transaction: %w", err)
	}
	defer transaction.Rollback()

	record, err := executionByID(ctx, transaction, executionID)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("load execution for transition: %w", err)
	}
	now := store.now()
	if err := validateTransition(record, input, now); err != nil {
		return execution.Execution{}, err
	}
	if input.Target == execution.StateCompleted {
		var evidenceCount int
		if err := transaction.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM evidence WHERE execution_id = ? AND source_revision <> ''",
			executionID,
		).Scan(&evidenceCount); err != nil {
			return execution.Execution{}, fmt.Errorf("count completion evidence: %w", err)
		}
		if evidenceCount == 0 {
			return execution.Execution{}, execution.ErrEvidenceRequired
		}
	}

	finishedAt := any(nil)
	if input.Target.Terminal() {
		finishedAt = now.UnixMilli()
	}
	result, err := transaction.ExecContext(ctx, `
		UPDATE executions
		SET state = ?, failure_class = ?, failure_message = ?, version = version + 1,
			updated_at = ?, finished_at = ?
		WHERE id = ? AND version = ?
	`,
		input.Target,
		input.FailureClass,
		strings.TrimSpace(input.FailureMessage),
		now.UnixMilli(),
		finishedAt,
		executionID,
		input.ExpectedVersion,
	)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("update execution state: %w", err)
	}
	if err := requireOneMutation(result); err != nil {
		return execution.Execution{}, err
	}
	if err := insertEvent(ctx, transaction, executionID, "execution.transitioned", strings.TrimSpace(input.Message), map[string]any{
		"failure_class": input.FailureClass,
		"from":          record.State,
		"to":            input.Target,
		"version":       record.Version + 1,
	}, now); err != nil {
		return execution.Execution{}, err
	}
	var outboxSequence int64
	if err := transaction.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence), 0) + 1 FROM provider_outbox WHERE execution_id = ?", executionID).Scan(&outboxSequence); err != nil {
		return execution.Execution{}, fmt.Errorf("allocate transition projection: %w", err)
	}
	outboxID, err := newID("out")
	if err != nil {
		return execution.Execution{}, err
	}
	payload, err := marshalPayload(map[string]any{"type": "execution.transitioned", "message": strings.TrimSpace(input.Message),
		"from": record.State, "to": input.Target, "failure_class": input.FailureClass,
		"failure_message": strings.TrimSpace(input.FailureMessage), "occurred_at": now.UnixMilli()})
	if err != nil {
		return execution.Execution{}, err
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO provider_outbox
		(id, execution_id, sequence, kind, idempotency_key, payload_json, status, created_at)
		VALUES (?, ?, ?, 'event', ?, ?, 'pending', ?)`, outboxID, executionID, outboxSequence,
		fmt.Sprintf("projection:transition:%s:%d", executionID, record.Version+1), payload, now.UnixMilli()); err != nil {
		return execution.Execution{}, fmt.Errorf("persist transition projection: %w", err)
	}
	updated, err := executionByID(ctx, transaction, executionID)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("load transitioned execution: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return execution.Execution{}, fmt.Errorf("commit execution transition: %w", err)
	}
	return updated, nil
}

func (store *Store) RenewLease(
	ctx context.Context,
	executionID string,
	expectedVersion int64,
	leaseExpiresAt time.Time,
) (execution.Execution, error) {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("begin lease transaction: %w", err)
	}
	defer transaction.Rollback()

	record, err := executionByID(ctx, transaction, executionID)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("load execution for lease renewal: %w", err)
	}
	now := store.now()
	if expectedVersion < 1 || record.Version != expectedVersion {
		return execution.Execution{}, fmt.Errorf("lease renewal version mismatch: %w", execution.ErrConflict)
	}
	if record.State.Terminal() {
		return execution.Execution{}, fmt.Errorf("terminal execution lease cannot be renewed: %w", execution.ErrInvalidTransition)
	}
	if record.BudgetExpired(now) {
		return execution.Execution{}, execution.ErrBudgetExhausted
	}
	leaseExpiresAt = leaseExpiresAt.UTC()
	if !leaseExpiresAt.After(now) || !leaseExpiresAt.After(record.LeaseExpiresAt) {
		return execution.Execution{}, fmt.Errorf("renewed lease must extend the current lease: %w", execution.ErrInvalidInput)
	}

	result, err := transaction.ExecContext(ctx, `
		UPDATE executions
		SET lease_expires_at = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ? AND finished_at IS NULL
	`, leaseExpiresAt.UnixMilli(), now.UnixMilli(), executionID, expectedVersion)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("renew execution lease: %w", err)
	}
	if err := requireOneMutation(result); err != nil {
		return execution.Execution{}, err
	}
	if err := insertEvent(ctx, transaction, executionID, "execution.lease_renewed", "external work lease renewed", map[string]any{
		"lease_expires_at": leaseExpiresAt.Format(time.RFC3339Nano),
		"version":          record.Version + 1,
	}, now); err != nil {
		return execution.Execution{}, err
	}
	updated, err := executionByID(ctx, transaction, executionID)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("load renewed execution: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return execution.Execution{}, fmt.Errorf("commit execution lease: %w", err)
	}
	return updated, nil
}

func (store *Store) ReconcileLease(
	ctx context.Context,
	executionID string,
	expectedVersion int64,
	leaseExpiresAt time.Time,
	reason string,
) (execution.Execution, error) {
	leaseExpiresAt = leaseExpiresAt.UTC()
	reason = strings.TrimSpace(reason)
	if leaseExpiresAt.IsZero() || reason == "" {
		return execution.Execution{}, fmt.Errorf("provider lease expiry and reconciliation reason are required: %w", execution.ErrInvalidInput)
	}

	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("begin lease reconciliation transaction: %w", err)
	}
	defer transaction.Rollback()
	record, err := executionByID(ctx, transaction, executionID)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("load execution for lease reconciliation: %w", err)
	}
	if record.LeaseExpiresAt.UnixMilli() == leaseExpiresAt.UnixMilli() {
		if err := transaction.Commit(); err != nil {
			return execution.Execution{}, fmt.Errorf("commit lease reconciliation replay: %w", err)
		}
		return record, nil
	}
	if expectedVersion < 1 || record.Version != expectedVersion {
		return execution.Execution{}, fmt.Errorf("lease reconciliation version mismatch: %w", execution.ErrConflict)
	}
	if record.State.Terminal() {
		return execution.Execution{}, fmt.Errorf("terminal execution lease cannot be reconciled: %w", execution.ErrInvalidTransition)
	}

	now := store.now()
	result, err := transaction.ExecContext(ctx, `
		UPDATE executions
		SET lease_expires_at = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ? AND finished_at IS NULL
	`, leaseExpiresAt.UnixMilli(), now.UnixMilli(), executionID, expectedVersion)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("reconcile execution lease: %w", err)
	}
	if err := requireOneMutation(result); err != nil {
		return execution.Execution{}, err
	}
	if err := insertEvent(ctx, transaction, executionID, "execution.lease_reconciled", reason, map[string]any{
		"lease_expires_at": leaseExpiresAt.Format(time.RFC3339Nano),
		"previous_expiry":  record.LeaseExpiresAt.Format(time.RFC3339Nano),
		"version":          record.Version + 1,
	}, now); err != nil {
		return execution.Execution{}, err
	}
	updated, err := executionByID(ctx, transaction, executionID)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("load lease-reconciled execution: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return execution.Execution{}, fmt.Errorf("commit lease reconciliation: %w", err)
	}
	return updated, nil
}

func (store *Store) UpdateProviderAuthority(
	ctx context.Context,
	executionID string,
	expectedVersion int64,
	externalRunVersion int64,
	fenceToken int64,
	leaseExpiresAt time.Time,
	reason string,
) (execution.Execution, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("begin authority update: %w", err)
	}
	defer tx.Rollback()
	updated, err := store.applyProviderAuthorityTx(ctx, tx, executionID, expectedVersion, externalRunVersion, fenceToken, leaseExpiresAt, reason)
	if err != nil {
		return execution.Execution{}, err
	}
	if err := tx.Commit(); err != nil {
		return execution.Execution{}, fmt.Errorf("commit authority update: %w", err)
	}
	return updated, nil
}

func (store *Store) applyProviderAuthorityTx(
	ctx context.Context,
	tx *sql.Tx,
	executionID string,
	expectedVersion int64,
	externalRunVersion int64,
	fenceToken int64,
	leaseExpiresAt time.Time,
	reason string,
) (execution.Execution, error) {
	reason = strings.TrimSpace(reason)
	leaseExpiresAt = leaseExpiresAt.UTC()
	if externalRunVersion < 1 || fenceToken < 1 || leaseExpiresAt.IsZero() || reason == "" {
		return execution.Execution{}, fmt.Errorf("provider version, fence, lease, and reason are required: %w", execution.ErrInvalidInput)
	}
	record, err := executionByID(ctx, tx, executionID)
	if err != nil {
		return execution.Execution{}, err
	}
	if record.ExternalRunVersion == externalRunVersion && record.FenceToken == fenceToken && record.LeaseExpiresAt.Equal(leaseExpiresAt) {
		return record, nil
	}
	if record.Version != expectedVersion || externalRunVersion < record.ExternalRunVersion || fenceToken < record.FenceToken {
		return execution.Execution{}, execution.ErrConflict
	}
	now := store.now()
	result, err := tx.ExecContext(ctx, `
		UPDATE executions SET external_run_version = ?, fence_token = ?, lease_expires_at = ?,
			version = version + 1, updated_at = ? WHERE id = ? AND version = ?
	`, externalRunVersion, fenceToken, leaseExpiresAt.UnixMilli(), now.UnixMilli(), executionID, expectedVersion)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("update provider authority: %w", err)
	}
	if err := requireOneMutation(result); err != nil {
		return execution.Execution{}, err
	}
	if err := insertEvent(ctx, tx, executionID, "execution.authority_reconciled", reason,
		map[string]any{"external_run_version": externalRunVersion, "fence_token": fenceToken,
			"lease_expires_at": leaseExpiresAt.Format(time.RFC3339Nano)}, now); err != nil {
		return execution.Execution{}, err
	}
	return executionByID(ctx, tx, executionID)
}

func normalizedLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func requireOneMutation(result sql.Result) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect guarded mutation: %w", err)
	}
	if rowsAffected != 1 {
		return execution.ErrConflict
	}
	return nil
}

func isNotFound(err error) bool {
	return errors.Is(err, execution.ErrNotFound)
}
