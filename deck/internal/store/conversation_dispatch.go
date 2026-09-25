package store

import (
	"context"
	"fmt"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

// The event ID, not the message row ID, is the dispatch cursor: an operator can
// explicitly invoke a message that Marina already saw in ordinary history.
func (s *Store) ListConversationInvocations(ctx context.Context, projectID string, afterID int64, limit int) ([]domain.ConversationDispatch, error) {
	if projectID == "" || afterID < 0 {
		return nil, fmt.Errorf("project and nonnegative cursor required: %w", domain.ErrInvalidInput)
	}
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.created_at,m.id FROM events e JOIN channel_messages m
		ON m.id=json_extract(e.payload_json,'$.message_id')
		WHERE e.type='conversation.agent.invoked' AND e.id>? AND m.project_id=?
		ORDER BY e.id LIMIT ?`, afterID, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.ConversationDispatch, 0)
	for rows.Next() {
		var item domain.ConversationDispatch
		var messageID string
		if err := rows.Scan(&item.EventID, &item.InvokedAt, &messageID); err != nil {
			return nil, err
		}
		item.Message, err = scanChannelMessage(s.db.QueryRowContext(ctx, "SELECT "+channelMessageColumns+" FROM channel_messages WHERE id=?", messageID))
		if err != nil {
			return nil, err
		}
		if item.Message.ProjectID != projectID || item.Message.InvokedAgentID == "" || item.Message.InvocationRequestID == "" {
			return nil, fmt.Errorf("invocation feed identity changed: %w", domain.ErrConflict)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
