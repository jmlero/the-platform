package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
)

func (store *Store) EnsureInstallationIdentity(ctx context.Context) (execution.InstallationIdentity, error) {
	installationID, err := newID("ins")
	if err != nil {
		return execution.InstallationIdentity{}, err
	}
	agentID, err := newID("agt")
	if err != nil {
		return execution.InstallationIdentity{}, err
	}
	now := store.now()
	if _, err := store.db.ExecContext(ctx, `INSERT OR IGNORE INTO installation_identity
		(singleton, installation_id, logical_agent_id, created_at) VALUES (1, ?, ?, ?)`,
		installationID, agentID, now.UnixMilli()); err != nil {
		return execution.InstallationIdentity{}, fmt.Errorf("ensure installation identity: %w", err)
	}
	var result execution.InstallationIdentity
	var created int64
	if err := store.db.QueryRowContext(ctx, `SELECT installation_id, logical_agent_id, created_at
		FROM installation_identity WHERE singleton = 1`).Scan(&result.InstallationID, &result.LogicalAgentID, &created); err != nil {
		return execution.InstallationIdentity{}, fmt.Errorf("load installation identity: %w", err)
	}
	result.CreatedAt = time.UnixMilli(created).UTC()
	return result, nil
}

func validateBinding(input execution.ProjectBinding) (execution.ProjectBinding, error) {
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Repository = strings.TrimSpace(input.Repository)
	input.Runtime = strings.TrimSpace(input.Runtime)
	input.PolicyVersion = strings.TrimSpace(input.PolicyVersion)
	input.ContextConfig = strings.TrimSpace(input.ContextConfig)
	input.ArtifactConfig = strings.TrimSpace(input.ArtifactConfig)
	input.DeliveryConfig = strings.TrimSpace(input.DeliveryConfig)
	if input.ProjectID == "" || input.Provider == "" || input.Repository == "" || input.Runtime == "" ||
		input.PolicyVersion == "" || input.ContextConfig == "" || input.ArtifactConfig == "" || input.DeliveryConfig == "" ||
		input.BudgetMinutes < 1 || input.MaxConcurrency < 1 {
		return execution.ProjectBinding{}, fmt.Errorf("complete bounded project binding is required: %w", execution.ErrInvalidInput)
	}
	return input, nil
}

func scanBinding(source scanner) (execution.ProjectBinding, error) {
	var result execution.ProjectBinding
	var created, updated int64
	err := source.Scan(&result.ProjectID, &result.Version, &result.Provider, &result.Repository, &result.Runtime,
		&result.PolicyVersion, &result.ContextConfig, &result.ArtifactConfig, &result.DeliveryConfig,
		&result.BudgetMinutes, &result.MaxConcurrency, &created, &updated)
	if err != nil {
		return result, wrapNotFound(err)
	}
	result.CreatedAt = time.UnixMilli(created).UTC()
	result.UpdatedAt = time.UnixMilli(updated).UTC()
	return result, nil
}

const bindingColumns = `project_id, version, provider, repository, runtime, policy_version,
	context_config, artifact_config, delivery_config, budget_minutes, max_concurrency, created_at, updated_at`

func (store *Store) EnsureProjectBinding(ctx context.Context, input execution.ProjectBinding) (execution.ProjectBinding, error) {
	validated, err := validateBinding(input)
	if err != nil {
		return execution.ProjectBinding{}, err
	}
	now := store.now()
	_, err = store.db.ExecContext(ctx, `INSERT INTO project_bindings (`+bindingColumns+`)
		VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET
			version = project_bindings.version + 1, provider = excluded.provider, repository = excluded.repository,
			runtime = excluded.runtime, policy_version = excluded.policy_version, context_config = excluded.context_config,
			artifact_config = excluded.artifact_config, delivery_config = excluded.delivery_config,
			budget_minutes = excluded.budget_minutes, max_concurrency = excluded.max_concurrency, updated_at = excluded.updated_at
		WHERE project_bindings.provider <> excluded.provider OR project_bindings.repository <> excluded.repository OR
			project_bindings.runtime <> excluded.runtime OR project_bindings.policy_version <> excluded.policy_version OR
			project_bindings.context_config <> excluded.context_config OR project_bindings.artifact_config <> excluded.artifact_config OR
			project_bindings.delivery_config <> excluded.delivery_config OR project_bindings.budget_minutes <> excluded.budget_minutes OR
			project_bindings.max_concurrency <> excluded.max_concurrency`, validated.ProjectID, validated.Provider,
		validated.Repository, validated.Runtime, validated.PolicyVersion, validated.ContextConfig, validated.ArtifactConfig,
		validated.DeliveryConfig, validated.BudgetMinutes, validated.MaxConcurrency, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return execution.ProjectBinding{}, fmt.Errorf("ensure project binding: %w", err)
	}
	return scanBinding(store.db.QueryRowContext(ctx, "SELECT "+bindingColumns+" FROM project_bindings WHERE project_id = ?", validated.ProjectID))
}

func (store *Store) GetProjectBinding(ctx context.Context, projectID string) (execution.ProjectBinding, error) {
	return scanBinding(store.db.QueryRowContext(ctx, "SELECT "+bindingColumns+" FROM project_bindings WHERE project_id = ?", projectID))
}

const acquisitionColumns = `id, project_id, work_id, task_version, task_digest, acceptance_criteria_digest, claim_key, agent_id,
	lease_seconds, budget_minutes, status, external_run_id, external_version, fence_token, lease_expires_at,
	retry_attempt, next_eligible_at, last_error_class, exhaustion_reason, created_at, updated_at`

func scanAcquisition(source scanner) (execution.AcquisitionIntent, error) {
	var result execution.AcquisitionIntent
	var lease, next sql.NullInt64
	var created, updated int64
	err := source.Scan(&result.ID, &result.ProjectID, &result.WorkID, &result.TaskVersion, &result.TaskDigest,
		&result.AcceptanceCriteriaDigest, &result.ClaimKey, &result.AgentID, &result.LeaseSeconds, &result.BudgetMinutes, &result.Status, &result.ExternalRunID,
		&result.ExternalVersion, &result.FenceToken, &lease, &result.RetryAttempt, &next,
		&result.LastErrorClass, &result.ExhaustionReason, &created, &updated)
	if err != nil {
		return result, wrapNotFound(err)
	}
	if lease.Valid {
		value := time.UnixMilli(lease.Int64).UTC()
		result.LeaseExpiresAt = &value
	}
	if next.Valid {
		value := time.UnixMilli(next.Int64).UTC()
		result.NextEligibleAt = &value
	}
	result.CreatedAt = time.UnixMilli(created).UTC()
	result.UpdatedAt = time.UnixMilli(updated).UTC()
	return result, nil
}

func (store *Store) EnsureAcquisitionIntent(ctx context.Context, input execution.NewAcquisitionIntent) (execution.AcquisitionIntent, bool, error) {
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.WorkID = strings.TrimSpace(input.WorkID)
	input.TaskDigest = strings.TrimSpace(input.TaskDigest)
	input.AcceptanceCriteriaDigest = strings.TrimSpace(input.AcceptanceCriteriaDigest)
	input.ClaimKey = strings.TrimSpace(input.ClaimKey)
	input.AgentID = strings.TrimSpace(input.AgentID)
	if input.ProjectID == "" || input.WorkID == "" || input.TaskVersion < 1 || input.TaskDigest == "" || input.AcceptanceCriteriaDigest == "" || input.ClaimKey == "" || input.AgentID == "" || input.LeaseSeconds < 1 || input.BudgetMinutes < 1 {
		return execution.AcquisitionIntent{}, false, execution.ErrInvalidInput
	}
	id, err := newID("acq")
	if err != nil {
		return execution.AcquisitionIntent{}, false, err
	}
	now := store.now()
	result, err := store.db.ExecContext(ctx, `INSERT OR IGNORE INTO acquisition_intents
		(id, project_id, work_id, task_version, task_digest, acceptance_criteria_digest, claim_key, agent_id, lease_seconds, budget_minutes, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`, id, input.ProjectID, input.WorkID, input.TaskVersion,
		input.TaskDigest, input.AcceptanceCriteriaDigest, input.ClaimKey, input.AgentID, input.LeaseSeconds, input.BudgetMinutes, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return execution.AcquisitionIntent{}, false, fmt.Errorf("ensure acquisition intent: %w", err)
	}
	created, _ := result.RowsAffected()
	record, err := scanAcquisition(store.db.QueryRowContext(ctx, "SELECT "+acquisitionColumns+" FROM acquisition_intents WHERE project_id = ? AND work_id = ? AND task_version = ?", input.ProjectID, input.WorkID, input.TaskVersion))
	if err != nil {
		return execution.AcquisitionIntent{}, false, err
	}
	if record.TaskDigest != input.TaskDigest || record.AcceptanceCriteriaDigest != input.AcceptanceCriteriaDigest || record.AgentID != input.AgentID || record.LeaseSeconds != input.LeaseSeconds || record.BudgetMinutes != input.BudgetMinutes {
		return execution.AcquisitionIntent{}, false, execution.ErrConflict
	}
	return record, created == 1, nil
}

func (store *Store) ListUnresolvedAcquisitions(ctx context.Context, limit int) ([]execution.AcquisitionIntent, error) {
	limit = normalizedLimit(limit)
	now := store.now().UnixMilli()
	rows, err := store.db.QueryContext(ctx, "SELECT "+acquisitionColumns+` FROM acquisition_intents
		WHERE status IN ('pending','claimed') AND (next_eligible_at IS NULL OR next_eligible_at <= ?)
		ORDER BY updated_at, id LIMIT ?`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("list acquisition intents: %w", err)
	}
	defer rows.Close()
	result := make([]execution.AcquisitionIntent, 0)
	for rows.Next() {
		item, err := scanAcquisition(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) RecordAcquisitionClaim(ctx context.Context, id string, runVersion, fence int64, runID string, leaseExpiresAt time.Time) (execution.AcquisitionIntent, error) {
	if runVersion < 1 || fence < 1 || strings.TrimSpace(runID) == "" || leaseExpiresAt.IsZero() {
		return execution.AcquisitionIntent{}, execution.ErrInvalidInput
	}
	now := store.now()
	_, err := store.db.ExecContext(ctx, `UPDATE acquisition_intents SET status = 'claimed', external_run_id = ?,
		external_version = ?, fence_token = ?, lease_expires_at = ?, retry_attempt = 0, next_eligible_at = NULL,
		last_error_class = '', exhaustion_reason = '', updated_at = ? WHERE id = ? AND status IN ('pending','claimed')`,
		runID, runVersion, fence, leaseExpiresAt.UnixMilli(), now.UnixMilli(), id)
	if err != nil {
		return execution.AcquisitionIntent{}, fmt.Errorf("record claim result: %w", err)
	}
	return scanAcquisition(store.db.QueryRowContext(ctx, "SELECT "+acquisitionColumns+" FROM acquisition_intents WHERE id = ?", id))
}

func (store *Store) RecordAcquisitionObservation(ctx context.Context, id string, runVersion, fence int64, runID string, leaseExpiresAt *time.Time) error {
	if runVersion < 1 || fence < 1 || strings.TrimSpace(runID) == "" {
		return execution.ErrInvalidInput
	}
	var lease any
	if leaseExpiresAt != nil {
		lease = leaseExpiresAt.UTC().UnixMilli()
	}
	result, err := store.db.ExecContext(ctx, `UPDATE acquisition_intents SET external_run_id = ?, external_version = ?,
		fence_token = ?, lease_expires_at = ?, updated_at = ? WHERE id = ? AND status IN ('pending','claimed')`,
		runID, runVersion, fence, lease, store.now().UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("record observed claim: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return execution.ErrConflict
	}
	return nil
}

func (store *Store) MarkAcquisitionBound(ctx context.Context, id, runID string) error {
	result, err := store.db.ExecContext(ctx, `UPDATE acquisition_intents SET status = 'bound', updated_at = ?
		WHERE id = ? AND status = 'claimed' AND external_run_id = ?`, store.now().UnixMilli(), id, runID)
	if err != nil {
		return fmt.Errorf("bind acquisition: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return execution.ErrConflict
	}
	return nil
}

func (store *Store) RecordAcquisitionFailure(ctx context.Context, id string, class execution.FailureClass, message string, maxAttempts int) error {
	if !class.Valid() || maxAttempts < 1 {
		return execution.ErrInvalidInput
	}
	record, err := scanAcquisition(store.db.QueryRowContext(ctx, "SELECT "+acquisitionColumns+" FROM acquisition_intents WHERE id = ?", id))
	if err != nil {
		return err
	}
	attempt := record.RetryAttempt + 1
	now := store.now()
	status := record.Status
	exhaustion := ""
	var next any
	if attempt >= maxAttempts || class != execution.FailureTransient {
		status = "exhausted"
		exhaustion = strings.TrimSpace(message)
	} else {
		delay := time.Duration(1<<min(attempt-1, 5)) * time.Second
		next = now.Add(delay).UnixMilli()
	}
	_, err = store.db.ExecContext(ctx, `UPDATE acquisition_intents SET status = ?, retry_attempt = ?, next_eligible_at = ?,
		last_error_class = ?, exhaustion_reason = ?, updated_at = ? WHERE id = ?`, status, attempt, next, class, exhaustion, now.UnixMilli(), id)
	return err
}

func (store *Store) CountActiveExecutionsForProject(ctx context.Context, projectID string) (int, error) {
	var count int
	err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM executions WHERE project_id = ? AND finished_at IS NULL`, projectID).Scan(&count)
	return count, err
}

const outboxColumns = `id, execution_id, sequence, kind, idempotency_key, payload_json, status,
	attempts, next_eligible_at, last_error_class, exhaustion_reason, created_at, published_at`

func scanOutbox(source scanner) (execution.OutboxItem, error) {
	var item execution.OutboxItem
	var encoded string
	var next, published sql.NullInt64
	var created int64
	err := source.Scan(&item.ID, &item.ExecutionID, &item.Sequence, &item.Kind, &item.IdempotencyKey,
		&encoded, &item.Status, &item.Attempts, &next, &item.LastErrorClass, &item.ExhaustionReason, &created, &published)
	if err != nil {
		return item, wrapNotFound(err)
	}
	item.Payload = unmarshalPayload(encoded)
	item.CreatedAt = time.UnixMilli(created).UTC()
	if next.Valid {
		v := time.UnixMilli(next.Int64).UTC()
		item.NextEligibleAt = &v
	}
	if published.Valid {
		v := time.UnixMilli(published.Int64).UTC()
		item.PublishedAt = &v
	}
	return item, nil
}

func (store *Store) EnqueueOutbox(ctx context.Context, executionID string, input execution.OutboxInput) (execution.OutboxItem, bool, error) {
	encoded, err := marshalPayload(input.Payload)
	if err != nil {
		return execution.OutboxItem{}, false, err
	}
	if strings.TrimSpace(input.Kind) == "" || strings.TrimSpace(input.IdempotencyKey) == "" {
		return execution.OutboxItem{}, false, execution.ErrInvalidInput
	}
	id, err := newID("out")
	if err != nil {
		return execution.OutboxItem{}, false, err
	}
	now := store.now()
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return execution.OutboxItem{}, false, err
	}
	defer tx.Rollback()
	var sequence int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence), 0) + 1 FROM provider_outbox WHERE execution_id = ?", executionID).Scan(&sequence); err != nil {
		return execution.OutboxItem{}, false, err
	}
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO provider_outbox
		(id, execution_id, sequence, kind, idempotency_key, payload_json, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)`, id, executionID, sequence, input.Kind, input.IdempotencyKey, encoded, now.UnixMilli())
	if err != nil {
		return execution.OutboxItem{}, false, err
	}
	created, _ := result.RowsAffected()
	item, err := scanOutbox(tx.QueryRowContext(ctx, "SELECT "+outboxColumns+" FROM provider_outbox WHERE idempotency_key = ?", input.IdempotencyKey))
	if err != nil {
		return execution.OutboxItem{}, false, err
	}
	if item.ExecutionID != executionID || item.Kind != input.Kind || marshalJSONMap(item.Payload) != marshalJSONMap(input.Payload) {
		return execution.OutboxItem{}, false, execution.ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return execution.OutboxItem{}, false, err
	}
	return item, created == 1, nil
}

func marshalJSONMap(value map[string]any) string { encoded, _ := marshalPayload(value); return encoded }

func (store *Store) ListReadyOutbox(ctx context.Context, limit int) ([]execution.OutboxItem, error) {
	limit = normalizedLimit(limit)
	rows, err := store.db.QueryContext(ctx, "SELECT "+prefixedOutboxColumns("current")+` FROM provider_outbox current
		WHERE current.status = 'pending' AND (current.next_eligible_at IS NULL OR current.next_eligible_at <= ?)
		  AND (current.kind = 'command_ack' OR NOT EXISTS (
			SELECT 1 FROM provider_outbox earlier
			WHERE earlier.execution_id = current.execution_id
			  AND earlier.kind <> 'command_ack' AND earlier.sequence < current.sequence
			  AND earlier.status <> 'published'
		  ))
		ORDER BY CASE current.kind WHEN 'command_ack' THEN 0 ELSE 1 END,
			current.execution_id, current.sequence, current.created_at, current.id LIMIT ?`, store.now().UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]execution.OutboxItem, 0)
	for rows.Next() {
		item, err := scanOutbox(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func prefixedOutboxColumns(alias string) string {
	columns := strings.Split(outboxColumns, ",")
	for index, column := range columns {
		columns[index] = alias + "." + strings.TrimSpace(column)
	}
	return strings.Join(columns, ", ")
}

func (store *Store) MarkOutboxPublished(ctx context.Context, id string) error {
	_, err := store.db.ExecContext(ctx, `UPDATE provider_outbox SET status = 'published', published_at = ?, next_eligible_at = NULL WHERE id = ? AND status = 'pending'`, store.now().UnixMilli(), id)
	return err
}

func (store *Store) CompleteOutboxPublication(
	ctx context.Context,
	outboxID string,
	executionID string,
	expectedVersion int64,
	externalRunVersion int64,
	fenceToken int64,
	leaseExpiresAt time.Time,
	reason string,
) (execution.Execution, error) {
	// AppDeck hashes the mutation guard into the idempotency digest, so the
	// authority advance and published mark must commit together. Otherwise a
	// retry sends the same key with a new guard and stalls the stream.
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return execution.Execution{}, fmt.Errorf("begin outbox publication: %w", err)
	}
	defer tx.Rollback()
	updated, err := store.applyProviderAuthorityTx(ctx, tx, executionID, expectedVersion, externalRunVersion, fenceToken, leaseExpiresAt, reason)
	if err != nil {
		return execution.Execution{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE provider_outbox SET status = 'published', published_at = ?, next_eligible_at = NULL WHERE id = ? AND status = 'pending'`, store.now().UnixMilli(), outboxID); err != nil {
		return execution.Execution{}, fmt.Errorf("mark outbox published: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return execution.Execution{}, fmt.Errorf("commit outbox publication: %w", err)
	}
	return updated, nil
}

func (store *Store) RecordOutboxFailure(ctx context.Context, id string, class execution.FailureClass, message string, maxAttempts int) error {
	item, err := scanOutbox(store.db.QueryRowContext(ctx, "SELECT "+outboxColumns+" FROM provider_outbox WHERE id = ?", id))
	if err != nil {
		return err
	}
	attempts := item.Attempts + 1
	now := store.now()
	status := "pending"
	exhaustion := ""
	var next any
	if attempts >= maxAttempts || class != execution.FailureTransient {
		status = "exhausted"
		exhaustion = strings.TrimSpace(message)
	} else {
		next = now.Add(time.Duration(1<<min(attempts-1, 5)) * time.Second).UnixMilli()
	}
	_, err = store.db.ExecContext(ctx, `UPDATE provider_outbox SET status = ?, attempts = ?, next_eligible_at = ?, last_error_class = ?, exhaustion_reason = ? WHERE id = ?`, status, attempts, next, class, exhaustion, id)
	return err
}

func (store *Store) SetCommandCursor(ctx context.Context, executionID string, expectedVersion, cursor int64) (execution.Execution, error) {
	result, err := store.db.ExecContext(ctx, `UPDATE executions SET command_cursor = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ? AND command_cursor < ?`, cursor, store.now().UnixMilli(), executionID, expectedVersion, cursor)
	if err != nil {
		return execution.Execution{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return execution.Execution{}, execution.ErrConflict
	}
	return store.GetExecution(ctx, executionID)
}

func (store *Store) RecordExecutionRetry(ctx context.Context, executionID string, expectedVersion int64, class execution.FailureClass, message string, maxAttempts int) (execution.Execution, error) {
	record, err := store.GetExecution(ctx, executionID)
	if err != nil {
		return execution.Execution{}, err
	}
	if record.Version != expectedVersion || !class.Valid() || maxAttempts < 1 {
		return execution.Execution{}, execution.ErrConflict
	}
	attempt := record.RetryAttempt + 1
	now := store.now()
	var next any
	exhaustion := ""
	if attempt >= maxAttempts || class != execution.FailureTransient {
		exhaustion = strings.TrimSpace(message)
	} else {
		next = now.Add(time.Duration(1<<min(attempt-1, 5)) * time.Second).UnixMilli()
	}
	result, err := store.db.ExecContext(ctx, `UPDATE executions SET retry_attempt = ?, next_eligible_at = ?, last_error_class = ?,
		exhaustion_reason = ?, version = version + 1, updated_at = ? WHERE id = ? AND version = ?`, attempt, next, class, exhaustion, now.UnixMilli(), executionID, expectedVersion)
	if err != nil {
		return execution.Execution{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return execution.Execution{}, execution.ErrConflict
	}
	return store.GetExecution(ctx, executionID)
}

func (store *Store) ResetExecutionRetry(ctx context.Context, executionID string, expectedVersion int64) (execution.Execution, error) {
	record, err := store.GetExecution(ctx, executionID)
	if err != nil {
		return execution.Execution{}, err
	}
	if record.Version != expectedVersion {
		return execution.Execution{}, execution.ErrConflict
	}
	if record.RetryAttempt == 0 && record.NextEligibleAt == nil && record.LastErrorClass == "" && record.ExhaustionReason == "" {
		return record, nil
	}
	result, err := store.db.ExecContext(ctx, `UPDATE executions SET retry_attempt = 0, next_eligible_at = NULL,
		last_error_class = '', exhaustion_reason = '', version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`, store.now().UnixMilli(), executionID, expectedVersion)
	if err != nil {
		return execution.Execution{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return execution.Execution{}, execution.ErrConflict
	}
	return store.GetExecution(ctx, executionID)
}
