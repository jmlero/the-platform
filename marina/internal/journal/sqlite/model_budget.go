package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jmlero/the-platform/marina/internal/execution"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
)

// ModelBudgetDenial reads the durable gateway denial even after its outbox
// receipt was published. The workflow uses it instead of a generic engine error.
func (store *Store) ModelBudgetDenial(ctx context.Context, executionID, attemptID string) (string, error) {
	var message string
	err := store.db.QueryRowContext(ctx, `SELECT json_extract(payload_json,'$.message') FROM provider_outbox
		WHERE execution_id=? AND kind='event' AND instr(idempotency_key,?)=1
		AND json_extract(payload_json,'$.type')='model.budget_exhausted' ORDER BY sequence LIMIT 1`, executionID, "model-budget:"+attemptID+":").Scan(&message)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return message, err
}

// ReserveModelCall charges a conservative reservation before issuing a model
// request. An unknown response retains the reservation across process restart.
func (store *Store) ReserveModelCall(ctx context.Context, executionID, attemptID string, fence, tokens, cost int64) (string, error) {
	if attemptID == "" || tokens < 1 || tokens > 1<<40 || cost < 0 || cost > 1<<40 {
		return "", execution.ErrInvalidInput
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	record, err := executionByID(ctx, tx, executionID)
	if err != nil {
		return "", err
	}
	if !record.AuthoritySafe(store.now(), 0) || record.FenceToken != fence {
		return "", execution.ErrConflict
	}
	attempt, err := runtimeAttemptByID(ctx, tx, attemptID)
	if err != nil {
		return "", err
	}
	if attempt.ExecutionID != record.ID || attempt.State.Terminal() || attempt.Specification.AuthorityFence != fence || !store.now().Before(attempt.DeadlineAt) {
		return "", execution.ErrConflict
	}
	if attempt.Specification.Stage == "review" {
		var calls int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_calls WHERE attempt_id=?`, attemptID).Scan(&calls); err != nil {
			return "", err
		}
		if calls != 0 {
			return "", execution.ErrConflict
		}
	}
	if attempt.Specification.Engine != nil {
		ref, err := attempt.Specification.EngineGatewayReference()
		if err != nil || !store.now().Before(ref.ExpiresAt) {
			return "", execution.ErrConflict
		}
		var data string
		var engine rt.EngineAttempt
		if err := tx.QueryRowContext(ctx, `SELECT record_json FROM engine_attempts WHERE attempt_id=?`, attemptID).Scan(&data); err != nil {
			return "", execution.ErrConflict
		}
		if json.Unmarshal([]byte(data), &engine) != nil || !engine.AllowsCalls() {
			return "", execution.ErrConflict
		}
	}
	var pendingTokens, pendingCost int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(reserved_tokens),0),COALESCE(SUM(reserved_cost_micros),0) FROM model_calls WHERE execution_id=? AND finished_at IS NULL`, executionID).Scan(&pendingTokens, &pendingCost); err != nil {
		return "", err
	}
	if record.MaxTokens > 0 && tokens > record.MaxTokens-record.UsedTokens-pendingTokens {
		return "", fmt.Errorf("token budget cannot admit model request: %w", execution.ErrBudgetExhausted)
	}
	if record.MaxCostMicros > 0 && cost > record.MaxCostMicros-record.UsedCostMicros-pendingCost {
		return "", fmt.Errorf("cost budget cannot admit model request: %w", execution.ErrBudgetExhausted)
	}
	id, err := newID("mdl")
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO model_calls(id,execution_id,attempt_id,reserved_tokens,reserved_cost_micros,created_at) VALUES(?,?,?,?,?,?)`, id, executionID, attemptID, tokens, cost, store.now().UnixMilli()); err != nil {
		return "", err
	}
	result, err := tx.ExecContext(ctx, `UPDATE executions SET version=version+1,updated_at=? WHERE id=? AND version=?`, store.now().UnixMilli(), executionID, record.Version)
	if err != nil {
		return "", err
	}
	if err = requireOneMutation(result); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

func (store *Store) FinishModelCall(ctx context.Context, id string, tokens, cost int64) error {
	if tokens < 0 || tokens > 1<<40 || cost < 0 || cost > 1<<40 {
		return execution.ErrInvalidInput
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var executionID string
	var reservedTokens, reservedCost int64
	var usedTokens, usedCost sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT execution_id,reserved_tokens,reserved_cost_micros,used_tokens,used_cost_micros FROM model_calls WHERE id=?`, id).Scan(&executionID, &reservedTokens, &reservedCost, &usedTokens, &usedCost); err != nil {
		return err
	}
	if usedTokens.Valid {
		if usedTokens.Int64 != tokens || usedCost.Int64 != cost {
			return execution.ErrConflict
		}
		return nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE model_calls SET used_tokens=?,used_cost_micros=?,finished_at=? WHERE id=? AND finished_at IS NULL`, tokens, cost, store.now().UnixMilli(), id); err != nil {
		return err
	}
	// Actual upstream usage remains recorded even if it exceeds the estimate;
	// the execution budget then stops subsequent requests and publication.
	if _, err = tx.ExecContext(ctx, `UPDATE executions SET used_tokens=used_tokens+?,used_cost_micros=used_cost_micros+?,version=version+1,updated_at=? WHERE id=?`, tokens, cost, store.now().UnixMilli(), executionID); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *Store) ModelUsage(ctx context.Context, attemptID string) (int64, int64, error) {
	var tokens, cost int64
	err := store.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(COALESCE(used_tokens,reserved_tokens)),0),COALESCE(SUM(COALESCE(used_cost_micros,reserved_cost_micros)),0) FROM model_calls WHERE attempt_id=?`, attemptID).Scan(&tokens, &cost)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return tokens, cost, err
}
