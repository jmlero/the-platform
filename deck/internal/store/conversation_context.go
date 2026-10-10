package store

import (
	"context"
	"encoding/json"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

// ResolveConversationContext reads the selection captured when the operator
// invoked the agent. Edits cannot replace it; revocation remains observable.
func (s *Store) ResolveConversationContext(ctx context.Context, invocationMessageID string) (domain.ResolvedContext, error) {
	var projectID, encoded string
	if err := s.db.QueryRowContext(ctx, `SELECT m.project_id, c.context_json FROM conversation_context c JOIN channel_messages m ON m.id = c.invocation_message_id WHERE m.id = ? AND m.invoked_agent_id <> ''`, invocationMessageID).Scan(&projectID, &encoded); err != nil {
		return domain.ResolvedContext{}, normalizeStoreError(err)
	}
	var selection *domain.TaskContext
	if err := json.Unmarshal([]byte(encoded), &selection); err != nil {
		return domain.ResolvedContext{}, err
	}
	return resolveContext(ctx, s.db, projectID, selection)
}
