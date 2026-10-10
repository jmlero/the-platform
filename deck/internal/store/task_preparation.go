package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

// Landed prerequisites use Marina's exact source observation. Other results
// retain human acceptance and the explicit source-availability attestation.
const dependenciesEligibleSQL = `NOT EXISTS (
 SELECT 1 FROM json_each(tasks.depends_on_json) d LEFT JOIN tasks p ON p.id = d.value
 WHERE p.id IS NULL OR p.status != 'done' OR (
 json_extract(p.landing_json, '$.run_id') IS NULL AND (
 json_extract(p.review_json, '$.accepted_at') IS NULL
 OR EXISTS (SELECT 1 FROM evidence_records e WHERE e.run_id = json_extract(p.review_json, '$.run_id') AND (e.admission_status IN ('stale','rejected') OR (e.expires_at IS NOT NULL AND e.expires_at <= CAST(unixepoch('subsec') * 1000 AS INTEGER))))
 OR (EXISTS (SELECT 1 FROM json_each(p.authority_json) a WHERE a.value = 'source.write')
 AND (COALESCE(json_extract(p.review_json, '$.source_available'), 0) != 1
 OR COALESCE(json_extract(p.context_json, '$.repository.repository'), '') != COALESCE(json_extract(tasks.context_json, '$.repository.repository'), '')
 OR COALESCE(json_extract(p.context_json, '$.repository.base_branch'), '') != COALESCE(json_extract(tasks.context_json, '$.repository.base_branch'), '')))
 )) OR (json_extract(p.landing_json, '$.run_id') IS NOT NULL AND (
 json_extract(p.landing_json, '$.repository') != COALESCE(json_extract(tasks.context_json, '$.repository.repository'), '')
 OR json_extract(p.landing_json, '$.base_branch') != COALESCE(json_extract(tasks.context_json, '$.repository.base_branch'), '')
 OR EXISTS (SELECT 1 FROM json_each(p.landing_json, '$.evidence_ids') selected
 LEFT JOIN evidence_records e ON e.id = selected.value
 WHERE e.id IS NULL OR e.admission_status != 'current' OR e.outcome != 'passed'
 OR (e.expires_at IS NOT NULL AND e.expires_at <= CAST(unixepoch('subsec') * 1000 AS INTEGER))
 OR e.source_revision != json_extract(p.landing_json, '$.commit'))
 ))

)`

func (s *Store) AcceptTask(ctx context.Context, id string, version int, actor string, sourceAvailable bool) (domain.Task, error) {
	if version < 1 || strings.TrimSpace(actor) == "" {
		return domain.Task{}, domain.ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()
	task, err := getTaskTx(ctx, tx, id)
	if err != nil {
		return task, err
	}
	if task.Review != nil && task.Status == domain.TaskDone && task.Review.TaskVersion == version && task.Review.AcceptedBy == actor && task.Review.SourceAvailable == sourceAvailable {
		if err := tx.Rollback(); err != nil {
			return task, err
		}
		return s.GetTask(ctx, id)
	}
	if task.Version != version || task.Status != domain.TaskDone || task.Archived || task.Landing != nil {
		return task, fmt.Errorf("only a current, finished result can be accepted: %w", domain.ErrConflict)
	}
	var runID, status, digest string
	err = tx.QueryRowContext(ctx, "SELECT id, status, acceptance_criteria_digest FROM runs WHERE task_id = ? ORDER BY started_at DESC, rowid DESC LIMIT 1", id).Scan(&runID, &status, &digest)
	if err != nil || status != domain.RunCompleted {
		return task, fmt.Errorf("a verified completed run is required: %w", domain.ErrConflict)
	}
	currentDigest := sha256.Sum256([]byte(task.AcceptanceCriteria))
	if hex.EncodeToString(currentDigest[:]) != digest {
		return task, fmt.Errorf("acceptance criteria changed after execution; a new verified result is required: %w", domain.ErrConflict)
	}
	var stale int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM evidence_records WHERE run_id = ? AND (admission_status IN ('stale', 'rejected') OR (expires_at IS NOT NULL AND expires_at <= ?))", runID, nowMillis()).Scan(&stale); err != nil {
		return task, err
	}
	if stale > 0 {
		return task, fmt.Errorf("result evidence is stale; resolve it before accepting: %w", domain.ErrConflict)
	}
	task.Review = &domain.TaskReview{RunID: runID, AcceptedBy: actor, AcceptedAt: nowMillis(), TaskVersion: version, SourceAvailable: sourceAvailable}
	result, err := tx.ExecContext(ctx, "UPDATE tasks SET review_json = ?, version = version + 1, updated_at = ? WHERE id = ? AND version = ?", marshalJSON(task.Review), task.Review.AcceptedAt, id, version)
	if err != nil {
		return task, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return task, err
	}
	if n != 1 {
		return task, domain.ErrConflict
	}
	event, err := insertEventTx(ctx, tx, &id, &runID, nil, "task.accepted", "Result accepted by operator", map[string]any{"accepted_by": actor, "source_available": sourceAvailable})
	if err != nil {
		return task, err
	}
	if err = tx.Commit(); err != nil {
		return task, err
	}
	s.publish(event)
	return s.GetTask(ctx, id)
}

func (s *Store) SetTaskDependencies(ctx context.Context, id string, version int, ids []string) (domain.Task, error) {
	if len(ids) > 50 || version < 1 {
		return domain.Task{}, domain.ErrInvalidInput
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if ids == nil {
		ids = []string{}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()
	task, err := getTaskTx(ctx, tx, id)
	if err != nil {
		return task, err
	}
	if task.Version != version || task.Status != domain.TaskBacklog || task.Archived {
		return task, fmt.Errorf("return the current task to Backlog before editing dependencies: %w", domain.ErrConflict)
	}
	for _, depID := range ids {
		dep, err := getTaskTx(ctx, tx, depID)
		if err != nil {
			return task, err
		}
		if dep.ProjectID != task.ProjectID || depID == id {
			return task, fmt.Errorf("dependencies must be other tasks in this project: %w", domain.ErrInvalidInput)
		}
		cycle, err := dependencyReaches(ctx, tx, depID, id, map[string]bool{})
		if err != nil {
			return task, err
		}
		if cycle {
			return task, fmt.Errorf("dependency would create a cycle: %w", domain.ErrInvalidInput)
		}
	}
	result, err := tx.ExecContext(ctx, "UPDATE tasks SET depends_on_json = ?, version = version + 1, updated_at = ? WHERE id = ? AND version = ?", marshalJSON(ids), nowMillis(), id, version)
	if err != nil {
		return task, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return task, err
	}
	if n != 1 {
		return task, domain.ErrConflict
	}
	event, err := insertEventTx(ctx, tx, &id, nil, nil, "task.dependencies.updated", "Task dependencies updated", map[string]any{"depends_on": ids})
	if err != nil {
		return task, err
	}
	if err = tx.Commit(); err != nil {
		return task, err
	}
	s.publish(event)
	return s.GetTask(ctx, id)
}

func dependencyReaches(ctx context.Context, tx *sql.Tx, from, to string, seen map[string]bool) (bool, error) {
	if from == to {
		return true, nil
	}
	if seen[from] {
		return false, nil
	}
	seen[from] = true
	task, err := getTaskTx(ctx, tx, from)
	if err != nil {
		return false, err
	}
	for _, id := range task.DependsOn {
		found, err := dependencyReaches(ctx, tx, id, to, seen)
		if err != nil || found {
			return found, err
		}
	}
	return false, nil
}

func (s *Store) TaskDependenciesSatisfied(ctx context.Context, id string) (bool, error) {
	var eligible bool
	err := s.db.QueryRowContext(ctx, "SELECT "+dependenciesEligibleSQL+" FROM tasks WHERE id = ?", id).Scan(&eligible)
	return eligible, err
}
