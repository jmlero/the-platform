package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

type portableRunContext struct {
	RunID     string              `json:"run_id"`
	Selection *domain.TaskContext `json:"selection"`
}

type portableConversationContext struct {
	InvocationMessageID string              `json:"invocation_message_id"`
	Selection           *domain.TaskContext `json:"selection"`
}

func (s *Store) exportConversationContexts(ctx context.Context, projectID string) ([]portableConversationContext, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.invocation_message_id, c.context_json FROM conversation_context c JOIN channel_messages m ON m.id = c.invocation_message_id WHERE m.project_id = ? ORDER BY c.invocation_message_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []portableConversationContext
	for rows.Next() {
		var item portableConversationContext
		var encoded string
		if err := rows.Scan(&item.InvocationMessageID, &encoded); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(encoded), &item.Selection); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) exportRunContexts(ctx context.Context, projectID string) ([]portableRunContext, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.run_id, c.context_json FROM run_context c JOIN runs r ON r.id = c.run_id JOIN tasks t ON t.id = r.task_id WHERE t.project_id = ? ORDER BY c.run_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []portableRunContext
	for rows.Next() {
		var item portableRunContext
		var encoded string
		if err = rows.Scan(&item.RunID, &encoded); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(encoded), &item.Selection); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func importContext(ctx context.Context, tx *sql.Tx, bundle portableBundle) error {
	for _, source := range bundle.ContextSources {
		if source.ProjectID != bundle.Project.ID || source.Revision < 1 {
			return domain.ErrInvalidInput
		}
		if _, err := validateContextSource(source.ContextSourceInput); err != nil {
			return err
		}
		digest, revoked := source.Digest, source.RevokedAt
		source.Digest, source.RevokedAt, source.Superseded = "", 0, false
		if contextHash(source) != digest {
			return fmt.Errorf("imported context digest mismatch: %w", domain.ErrInvalidInput)
		}
		source.Digest = digest
		if _, err := tx.ExecContext(ctx, "INSERT INTO context_sources(project_id, id, revision, document_json, revoked_at) VALUES (?, ?, ?, ?, ?)", source.ProjectID, source.ID, source.Revision, marshalJSON(source), revoked); err != nil {
			return err
		}
	}
	for _, item := range bundle.RunContexts {
		var projectID string
		if err := tx.QueryRowContext(ctx, "SELECT t.project_id FROM runs r JOIN tasks t ON t.id = r.task_id WHERE r.id = ?", item.RunID).Scan(&projectID); err != nil || projectID != bundle.Project.ID {
			return domain.ErrInvalidInput
		}
		if _, err := resolveContext(ctx, tx, projectID, item.Selection); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO run_context(run_id, context_json) VALUES (?, ?)", item.RunID, marshalJSON(item.Selection)); err != nil {
			return err
		}
	}
	for _, item := range bundle.ConversationContexts {
		var projectID, agentID string
		if err := tx.QueryRowContext(ctx, "SELECT project_id, invoked_agent_id FROM channel_messages WHERE id = ?", item.InvocationMessageID).Scan(&projectID, &agentID); err != nil || projectID != bundle.Project.ID || agentID == "" {
			return domain.ErrInvalidInput
		}
		if _, err := resolveContext(ctx, tx, projectID, item.Selection); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO conversation_context(invocation_message_id, context_json) VALUES (?, ?)", item.InvocationMessageID, marshalJSON(item.Selection)); err != nil {
			return err
		}
	}
	for _, item := range bundle.Tasks {
		if item.Task.ProjectID != bundle.Project.ID {
			return domain.ErrInvalidInput
		}
		if _, err := resolveContext(ctx, tx, bundle.Project.ID, item.Task.Context); err != nil {
			return err
		}
	}
	return nil
}
