package sqlite

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jmlero/the-platform/marina/internal/execution"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
)

func (s *Store) EnsureEngineAttempt(ctx context.Context, spec rt.Specification) (rt.EngineAttempt, error) {
	a, err := s.GetRuntimeAttempt(ctx, spec.AttemptID)
	if err != nil {
		return rt.EngineAttempt{}, err
	}
	digest, err := spec.Digest()
	if err != nil {
		return rt.EngineAttempt{}, err
	}
	if spec.Engine == nil || a.SpecificationHash != digest {
		return rt.EngineAttempt{}, execution.ErrConflict
	}
	r := rt.EngineAttempt{AttemptID: a.ID, SpecificationHash: digest, MessageID: "marina-" + a.ID, Phase: "creating", State: rt.StateStarting, Version: 1}
	data, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO engine_attempts(attempt_id,record_json,version) VALUES(?,?,1) ON CONFLICT(attempt_id) DO NOTHING`, a.ID, string(data))
	if err != nil {
		return r, err
	}
	return s.GetEngineAttempt(ctx, a.ID)
}
func (s *Store) GetEngineAttempt(ctx context.Context, id string) (rt.EngineAttempt, error) {
	var data string
	var r rt.EngineAttempt
	if err := s.db.QueryRowContext(ctx, `SELECT record_json FROM engine_attempts WHERE attempt_id=?`, id).Scan(&data); err != nil {
		return r, wrapNotFound(err)
	}
	err := json.Unmarshal([]byte(data), &r)
	return r, err
}
func (s *Store) UpdateEngineAttempt(ctx context.Context, n rt.EngineAttempt) (rt.EngineAttempt, error) {
	r, err := s.GetEngineAttempt(ctx, n.AttemptID)
	if err != nil {
		return n, err
	}
	if !r.CanUpdate(n) {
		return n, execution.ErrConflict
	}
	n.Version++
	data, err := json.Marshal(n)
	if err != nil {
		return n, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE engine_attempts SET record_json=?,version=? WHERE attempt_id=? AND version=?`, string(data), n.Version, n.AttemptID, r.Version)
	if err != nil {
		return n, err
	}
	if err = requireOneMutation(result); err != nil {
		return n, err
	}
	return n, nil
}

// EngineAttemptByKey only looks up a recorded runtime key. It cannot claim or
// synthesize a replacement runtime attempt after a reconnect.
func (s *Store) EngineAttemptByKey(ctx context.Context, key string) (rt.Attempt, error) {
	a, err := runtimeAttemptByKey(ctx, s.db, key)
	if err == nil && a.Specification.Engine == nil {
		err = execution.ErrConflict
	}
	if errors.Is(err, execution.ErrNotFound) {
		return rt.Attempt{}, err
	}
	return a, err
}
