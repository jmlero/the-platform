package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

// Read models for mirroring a Run's journal state into its resource status.

// ExecutionsForWork returns a project's executions of one external work item,
// oldest first.
func (store *Store) ExecutionsForWork(ctx context.Context, projectID, workID string) ([]execution.Execution, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+executionColumns+" FROM executions WHERE project_id = ? AND external_work_id = ? ORDER BY created_at, id", projectID, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []execution.Execution
	for rows.Next() {
		record, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

// PlatformEvidenceItem is one evidence record of a Run's runs.
type PlatformEvidenceItem struct {
	ID, Type, Outcome, SourceRevision, ArtifactDigest, Admission string
	ObservedAt                                                   time.Time
}

func (p *Platform) Evidence(ctx context.Context, workID string) ([]PlatformEvidenceItem, error) {
	rows, err := p.store.db.QueryContext(ctx, `SELECT e.id, e.evidence_json FROM platform_evidence e JOIN platform_runs r ON r.id = e.run_id
		WHERE r.work_id = ? ORDER BY e.created_at, e.id`, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlatformEvidenceItem
	for rows.Next() {
		var id, encoded string
		if err := rows.Scan(&id, &encoded); err != nil {
			return nil, err
		}
		var item platformEvidence
		if err := json.Unmarshal([]byte(encoded), &item); err != nil {
			return nil, err
		}
		out = append(out, PlatformEvidenceItem{ID: id, Type: item.Type, Outcome: item.Outcome, SourceRevision: item.SourceRevision,
			ArtifactDigest: item.ArtifactDigest, Admission: item.Admission, ObservedAt: item.ObservedAt})
	}
	return out, rows.Err()
}

// Landing returns the landing observation of a Run's completed run, if any.
func (p *Platform) Landing(ctx context.Context, workID string) (*workcontrol.LandingObservation, error) {
	var encoded string
	err := p.store.db.QueryRowContext(ctx, "SELECT landing_json FROM platform_runs WHERE work_id = ? AND landing_json <> '' ORDER BY updated_at DESC LIMIT 1", workID).Scan(&encoded)
	if err != nil {
		return nil, nil
	}
	var landing workcontrol.LandingObservation
	if err := json.Unmarshal([]byte(encoded), &landing); err != nil {
		return nil, err
	}
	return &landing, nil
}

// LastEventSequence is the newest event sequence of a Run or Turn.
func (p *Platform) LastEventSequence(ctx context.Context, owner string) (int64, error) {
	var sequence int64
	err := p.store.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence), 0) FROM platform_events WHERE owner_id = ?", owner).Scan(&sequence)
	return sequence, err
}

// AllDecisions returns every Decision, oldest first, for resource mirroring.
func (p *Platform) AllDecisions(ctx context.Context, limit int) ([]PlatformDecision, error) {
	rows, err := p.store.db.QueryContext(ctx, "SELECT "+decisionColumns+" FROM platform_decisions ORDER BY created_at, id LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlatformDecision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
