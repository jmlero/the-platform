package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
)

// ConversationEngineJournal gives the existing kagent adapter its durable
// attempt contract without creating a repository task or an execution run.
type ConversationEngineJournal struct{ store *Store }

func (s *Store) ConversationEngineJournal() *ConversationEngineJournal {
	return &ConversationEngineJournal{store: s}
}

func (j *ConversationEngineJournal) GetRuntimeAttempt(ctx context.Context, id string) (rt.Attempt, error) {
	turn, err := j.store.GetConversationTurn(ctx, id)
	if err != nil {
		return rt.Attempt{}, err
	}
	if turn.SpecificationDigest == "" {
		return rt.Attempt{}, execution.ErrNotFound
	}
	return rt.Attempt{ID: id, ExecutionID: id, Number: 1, IdempotencyKey: turn.Specification.IdempotencyKey,
		Specification: turn.Specification, SpecificationHash: turn.SpecificationDigest,
		State: rt.StateRunning, DeadlineAt: turn.DeadlineAt}, nil
}

func (j *ConversationEngineJournal) EngineAttemptByKey(ctx context.Context, key string) (rt.Attempt, error) {
	var id string
	err := j.store.db.QueryRowContext(ctx, `SELECT message_id FROM conversation_turns WHERE CASE WHEN specification_json='' THEN NULL ELSE json_extract(specification_json,'$.idempotency_key') END=?`, key).Scan(&id)
	if err != nil {
		return rt.Attempt{}, wrapNotFound(err)
	}
	return j.GetRuntimeAttempt(ctx, id)
}

func (j *ConversationEngineJournal) EnsureEngineAttempt(ctx context.Context, spec rt.Specification) (rt.EngineAttempt, error) {
	attempt, err := j.GetRuntimeAttempt(ctx, spec.AttemptID)
	if err != nil {
		return rt.EngineAttempt{}, err
	}
	digest, err := spec.Digest()
	if err != nil || attempt.SpecificationHash != digest || spec.Workload != "conversation/v1" {
		return rt.EngineAttempt{}, execution.ErrConflict
	}
	record := rt.EngineAttempt{AttemptID: spec.AttemptID, SpecificationHash: digest, MessageID: "marina-" + spec.AttemptID,
		Phase: "creating", State: rt.StateStarting, Version: 1}
	encoded, err := json.Marshal(record)
	if err != nil {
		return record, err
	}
	_, err = j.store.db.ExecContext(ctx, `INSERT INTO conversation_engine_attempts(attempt_id,record_json,version) VALUES(?,?,1) ON CONFLICT(attempt_id) DO NOTHING`, spec.AttemptID, string(encoded))
	if err != nil {
		return record, err
	}
	return j.GetEngineAttempt(ctx, spec.AttemptID)
}

func (j *ConversationEngineJournal) GetEngineAttempt(ctx context.Context, id string) (rt.EngineAttempt, error) {
	var encoded string
	var record rt.EngineAttempt
	if err := j.store.db.QueryRowContext(ctx, `SELECT record_json FROM conversation_engine_attempts WHERE attempt_id=?`, id).Scan(&encoded); err != nil {
		return record, wrapNotFound(err)
	}
	err := json.Unmarshal([]byte(encoded), &record)
	return record, err
}

func (j *ConversationEngineJournal) UpdateEngineAttempt(ctx context.Context, next rt.EngineAttempt) (rt.EngineAttempt, error) {
	prior, err := j.GetEngineAttempt(ctx, next.AttemptID)
	if err != nil {
		return next, err
	}
	if !prior.CanUpdate(next) {
		return next, execution.ErrConflict
	}
	next.Version++
	encoded, err := json.Marshal(next)
	if err != nil {
		return next, err
	}
	result, err := j.store.db.ExecContext(ctx, `UPDATE conversation_engine_attempts SET record_json=?,version=? WHERE attempt_id=? AND version=?`, string(encoded), next.Version, next.AttemptID, prior.Version)
	if err != nil {
		return next, err
	}
	if err := requireOneMutation(result); err != nil {
		return next, err
	}
	return next, nil
}

func (j *ConversationEngineJournal) RecordRuntimeArtifact(ctx context.Context, artifact rt.Artifact) (rt.Artifact, bool, error) {
	if artifact.ID == "" || artifact.RuntimeAttemptID == "" || artifact.ExecutionID != artifact.RuntimeAttemptID || artifact.Digest == "" || artifact.SizeBytes < 0 {
		return artifact, false, execution.ErrInvalidInput
	}
	result, err := j.store.db.ExecContext(ctx, `INSERT INTO conversation_artifacts(id,attempt_id,name,kind,uri,digest,size_bytes,created_at)
		VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, artifact.ID, artifact.RuntimeAttemptID, artifact.Name, artifact.Kind,
		artifact.URI, artifact.Digest, artifact.SizeBytes, artifact.CreatedAt.UnixMilli())
	if err != nil {
		return artifact, false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return artifact, false, err
	}
	items, err := j.ListRuntimeArtifacts(ctx, artifact.RuntimeAttemptID)
	if err != nil {
		return artifact, false, err
	}
	for _, saved := range items {
		if saved.ID == artifact.ID {
			if saved.Digest != artifact.Digest || saved.URI != artifact.URI || saved.SizeBytes != artifact.SizeBytes || saved.Kind != artifact.Kind {
				return artifact, false, execution.ErrConflict
			}
			return saved, count == 1, nil
		}
	}
	return artifact, false, errors.New("conversation artifact was not retained")
}

func (j *ConversationEngineJournal) ListRuntimeArtifacts(ctx context.Context, attemptID string) ([]rt.Artifact, error) {
	rows, err := j.store.db.QueryContext(ctx, `SELECT id,name,kind,uri,digest,size_bytes,created_at FROM conversation_artifacts WHERE attempt_id=? ORDER BY id`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []rt.Artifact
	for rows.Next() {
		var a rt.Artifact
		var timestamp int64
		if err := rows.Scan(&a.ID, &a.Name, &a.Kind, &a.URI, &a.Digest, &a.SizeBytes, &timestamp); err != nil {
			return nil, err
		}
		a.RuntimeAttemptID, a.ExecutionID = attemptID, attemptID
		a.CreatedAt = time.UnixMilli(timestamp).UTC()
		items = append(items, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list conversation artifacts: %w", err)
	}
	return items, nil
}
