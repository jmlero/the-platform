package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

// The task ledger remains authoritative. Selected events are projected into
// the originating conversation in the same transaction as the source event.
func mirrorTaskEventToThread(ctx context.Context, tx *sql.Tx, taskID, runID *string, kind, message string, payload map[string]any, createdAt int64) error {
	if taskID == nil {
		return nil
	}
	switch kind {
	case "run.claimed", "run.started", "run.progress", "run.verifying", "run.completed", "run.blocked", "run.lease_expired", "run.failed", "run.cancelled", "decision.requested", "decision.answered", "plan.changes_requested", "command.requested", "command.completed", "command.rejected":
	default:
		return nil
	}
	var channelID, projectID, threadID string
	err := tx.QueryRowContext(ctx, `SELECT t.channel_id,t.project_id,t.id FROM thread_tasks tt JOIN conversation_threads t ON t.id=tt.thread_id WHERE tt.task_id=?`, *taskID).Scan(&channelID, &projectID, &threadID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	refs := domain.CanonicalReferences{TaskIDs: []string{*taskID}}
	if runID != nil {
		refs.RunIDs = []string{*runID}
	}
	if kind == "run.completed" {
		if summary, ok := payload["summary"].(string); ok && summary != "" {
			message = "Run completed: " + summary
		}
		if ids, ok := payload["evidence_ids"].([]string); ok {
			refs.EvidenceIDs = ids
		}
		if id, ok := payload["artifact_id"].(string); ok && id != "" {
			refs.ArtifactIDs = []string{id}
		}
	}
	if kind == "decision.requested" {
		if id, ok := payload["decision_id"].(string); ok && id != "" {
			refs.DecisionIDs = []string{id}
		}
	}
	if len(message) > 2000 {
		message = message[:2000]
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_messages(id,channel_id,project_id,author_type,author_id,body,references_json,created_at,thread_id)
		VALUES(?,?,?,?,?,?,?,?,?)`, newID("msg"), channelID, projectID, "system", "deck", message, marshalJSON(refs), createdAt, threadID)
	return err
}
