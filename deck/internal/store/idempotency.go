package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

const (
	operationClaimTask            = "task.claim"
	operationHeartbeatRun         = "run.heartbeat"
	operationSubmitPlan           = "run.plan.submit"
	operationStartRun             = "run.start"
	operationReportProgress       = "run.progress"
	operationBeginVerification    = "run.verify"
	operationCompleteRun          = "run.complete"
	operationBlockRun             = "run.block"
	operationAcknowledgeCommand   = "command.ack"
	operationAppendProjectedEvent = "event.append"
	operationRecordEvidence       = "evidence.record"
	operationReportOperational    = "operation.report"
)

// Mutation identifies one retriable request. AppDeck scopes keys by canonical
// project, stable caller identity, and operation; the resource and request body
// are part of the digest so reuse for different input is rejected.
type Mutation struct {
	ActorID        string
	IdempotencyKey string
	Guard          *RunGuard
}

// RunGuard proves that a caller still owns the current authority epoch. It is
// optional only for AppDeck's in-process demo; service handlers require it.
type RunGuard struct {
	AgentID         string `json:"agent_id"`
	ExpectedVersion int64  `json:"expected_version"`
	FenceToken      int64  `json:"fence_token"`
}

func validateRunGuard(run domain.Run, guard *RunGuard) error {
	if guard == nil {
		return nil
	}
	if strings.TrimSpace(guard.AgentID) == "" || guard.ExpectedVersion < 1 || guard.FenceToken < 1 {
		return fmt.Errorf("agent_id, expected_version, and fence_token are required: %w", domain.ErrInvalidInput)
	}
	if run.AgentID != guard.AgentID || run.Version != guard.ExpectedVersion || run.FenceToken != guard.FenceToken {
		return fmt.Errorf("run authority is stale: %w", domain.ErrConflict)
	}
	return nil
}

type idempotencyAttempt struct {
	projectID string
	actorID   string
	operation string
	key       string
}

func NewMutation(actorID, idempotencyKey string) (Mutation, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" || len(actorID) > 160 {
		return Mutation{}, fmt.Errorf("idempotency actor must contain 1 to 160 characters: %w", domain.ErrInvalidInput)
	}
	trimmedKey := strings.TrimSpace(idempotencyKey)
	if idempotencyKey != trimmedKey || trimmedKey == "" || len(trimmedKey) > 128 {
		return Mutation{}, fmt.Errorf("Idempotency-Key must contain 1 to 128 visible ASCII characters: %w", domain.ErrInvalidInput)
	}
	idempotencyKey = trimmedKey
	for _, character := range idempotencyKey {
		if character < 0x21 || character > 0x7e {
			return Mutation{}, fmt.Errorf("Idempotency-Key must contain only visible ASCII characters: %w", domain.ErrInvalidInput)
		}
	}
	return Mutation{ActorID: actorID, IdempotencyKey: idempotencyKey}, nil
}

func beginIdempotentMutation(
	ctx context.Context,
	tx *sql.Tx,
	mutation Mutation,
	projectID string,
	operation string,
	resourceID string,
	payload any,
) (idempotencyAttempt, json.RawMessage, bool, error) {
	validated, err := NewMutation(mutation.ActorID, mutation.IdempotencyKey)
	if err != nil {
		return idempotencyAttempt{}, nil, false, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return idempotencyAttempt{}, nil, false, fmt.Errorf("encode idempotency request: %w", err)
	}
	digest := sha256.Sum256(encoded)
	requestHash := hex.EncodeToString(digest[:])
	attempt := idempotencyAttempt{
		projectID: projectID,
		actorID:   validated.ActorID,
		operation: operation,
		key:       validated.IdempotencyKey,
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO idempotency_records (
			project_id, actor_id, operation, idempotency_key,
			resource_id, request_hash, result_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, NULL, ?)
		ON CONFLICT(project_id, actor_id, operation, idempotency_key) DO NOTHING
	`, projectID, validated.ActorID, operation, validated.IdempotencyKey, resourceID, requestHash, nowMillis())
	if err != nil {
		return idempotencyAttempt{}, nil, false, fmt.Errorf("reserve idempotency key: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return idempotencyAttempt{}, nil, false, fmt.Errorf("read idempotency reservation: %w", err)
	}
	if inserted == 1 {
		return attempt, nil, false, nil
	}

	var existingHash string
	var storedResult sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT request_hash, result_json
		FROM idempotency_records
		WHERE project_id = ? AND actor_id = ? AND operation = ? AND idempotency_key = ?
	`, projectID, validated.ActorID, operation, validated.IdempotencyKey).Scan(&existingHash, &storedResult); err != nil {
		return idempotencyAttempt{}, nil, false, fmt.Errorf("load idempotency result: %w", err)
	}
	if existingHash != requestHash {
		return idempotencyAttempt{}, nil, false, fmt.Errorf(
			"Idempotency-Key %q was already used with a different request: %w",
			validated.IdempotencyKey,
			domain.ErrConflict,
		)
	}
	if !storedResult.Valid {
		return idempotencyAttempt{}, nil, false, fmt.Errorf("idempotency result is incomplete")
	}
	return attempt, json.RawMessage(storedResult.String), true, nil
}

func finishIdempotentMutation(
	ctx context.Context,
	tx *sql.Tx,
	attempt idempotencyAttempt,
	resultValue any,
) error {
	encoded, err := json.Marshal(resultValue)
	if err != nil {
		return fmt.Errorf("encode idempotency result: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE idempotency_records
		SET result_json = ?
		WHERE project_id = ? AND actor_id = ? AND operation = ?
		  AND idempotency_key = ? AND result_json IS NULL
	`, string(encoded), attempt.projectID, attempt.actorID, attempt.operation, attempt.key)
	if err != nil {
		return fmt.Errorf("store idempotency result: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read idempotency result update: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("idempotency result was not reserved")
	}
	return nil
}

func decodeIdempotencyResult(encoded json.RawMessage, destination any) error {
	if err := json.Unmarshal(encoded, destination); err != nil {
		return fmt.Errorf("decode stored idempotency result: %w", err)
	}
	return nil
}
