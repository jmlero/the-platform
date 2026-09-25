package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

const channelMessageColumns = "id, channel_id, project_id, author_type, author_id, body, references_json, promoted_task_id, created_at, thread_id, invoked_agent_id, invocation_request_id"

// InvokeConversation records an operator request and its selected conversation
// agent atomically. Dispatch is a separate consumer capability.
func (s *Store) InvokeConversation(ctx context.Context, in domain.ConversationInvocationInput) (domain.ConversationInvocation, error) {
	in.ChannelID = strings.TrimSpace(in.ChannelID)
	in.ThreadID = strings.TrimSpace(in.ThreadID)
	in.MessageID = strings.TrimSpace(in.MessageID)
	in.AgentID = strings.TrimSpace(in.AgentID)
	in.Body = strings.TrimSpace(in.Body)
	in.RequestID = strings.TrimSpace(in.RequestID)
	in.AuthorID = strings.TrimSpace(in.AuthorID)
	if in.ChannelID == "" || in.AgentID == "" || in.AuthorID == "" || in.RequestID == "" || len(in.RequestID) > 128 || (in.MessageID == "") == (in.Body == "") {
		return domain.ConversationInvocation{}, fmt.Errorf("channel, agent, request ID, and one message or body are required: %w", domain.ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ConversationInvocation{}, err
	}
	defer tx.Rollback()
	// Acquire the SQLite writer before inspecting the idempotency key or thread.
	if _, err := tx.ExecContext(ctx, "UPDATE channels SET id = id WHERE id = ?", in.ChannelID); err != nil {
		return domain.ConversationInvocation{}, err
	}
	var projectID string
	if err := tx.QueryRowContext(ctx, "SELECT project_id FROM channels WHERE id = ?", in.ChannelID).Scan(&projectID); err != nil {
		return domain.ConversationInvocation{}, normalizeStoreError(err)
	}
	var priorID string
	err = tx.QueryRowContext(ctx, "SELECT id FROM channel_messages WHERE invocation_request_id = ?", in.RequestID).Scan(&priorID)
	if err != nil && err != sql.ErrNoRows {
		return domain.ConversationInvocation{}, err
	}
	if err == nil {
		message, err := scanChannelMessage(tx.QueryRowContext(ctx, "SELECT "+channelMessageColumns+" FROM channel_messages WHERE id = ?", priorID))
		if err != nil {
			return domain.ConversationInvocation{}, err
		}
		if message.ChannelID != in.ChannelID || message.InvokedAgentID != in.AgentID || message.AuthorID != in.AuthorID || (in.ThreadID != "" && in.ThreadID != message.ThreadID) || (in.MessageID != "" && in.MessageID != message.ID) || (in.Body != "" && in.Body != message.Body) {
			return domain.ConversationInvocation{}, fmt.Errorf("invocation request ID already used for another request: %w", domain.ErrConflict)
		}
		if err := tx.Rollback(); err != nil {
			return domain.ConversationInvocation{}, err
		}
		thread, err := s.GetThread(ctx, message.ThreadID)
		if err != nil {
			return domain.ConversationInvocation{}, err
		}
		return domain.ConversationInvocation{Thread: thread, Message: message}, nil
	}
	var capabilitiesJSON, installationID string
	if err := tx.QueryRowContext(ctx, "SELECT capabilities_json, installation_id FROM agents WHERE id = ?", in.AgentID).Scan(&capabilitiesJSON, &installationID); err != nil {
		return domain.ConversationInvocation{}, normalizeStoreError(err)
	}
	if installationID == "" || !slices.Contains(unmarshalStrings(capabilitiesJSON), "conversation.invoke."+projectID) {
		return domain.ConversationInvocation{}, fmt.Errorf("agent is not configured for conversation: %w", domain.ErrInvalidInput)
	}
	var message domain.ChannelMessage
	var thread domain.ConversationThread
	var events []domain.Event
	var threadCreated bool
	if in.MessageID != "" {
		message, err = scanChannelMessage(tx.QueryRowContext(ctx, "SELECT "+channelMessageColumns+" FROM channel_messages WHERE id = ?", in.MessageID))
		if err != nil {
			return domain.ConversationInvocation{}, err
		}
		if message.ChannelID != in.ChannelID || message.AuthorType != "human" || message.InvokedAgentID != "" {
			return domain.ConversationInvocation{}, fmt.Errorf("message cannot be invoked in this channel: %w", domain.ErrConflict)
		}
		thread, threadCreated, err = ensureThreadTx(ctx, tx, in.ChannelID, message.ID)
		if err != nil {
			return domain.ConversationInvocation{}, err
		}
	} else if in.ThreadID != "" {
		thread, err = scanThread(tx.QueryRowContext(ctx, "SELECT "+threadColumns+" FROM conversation_threads WHERE id = ?", in.ThreadID))
		if err != nil {
			return domain.ConversationInvocation{}, err
		}
		if thread.ChannelID != in.ChannelID {
			return domain.ConversationInvocation{}, fmt.Errorf("thread belongs to another channel: %w", domain.ErrInvalidInput)
		}
	} else {
		thread = domain.ConversationThread{ID: newID("thr"), ChannelID: in.ChannelID, ProjectID: projectID, TaskIDs: []string{}, SelectedAgentID: in.AgentID, CreatedAt: nowMillis()}
		threadCreated = true
	}
	if thread.SelectedAgentID != "" && thread.SelectedAgentID != in.AgentID {
		return domain.ConversationInvocation{}, fmt.Errorf("thread already selected a different agent: %w", domain.ErrConflict)
	}
	if in.MessageID == "" {
		message = domain.ChannelMessage{ID: newID("msg"), ChannelID: in.ChannelID, ProjectID: projectID, ThreadID: thread.ID, InvokedAgentID: in.AgentID, InvocationRequestID: in.RequestID, AuthorType: "human", AuthorID: in.AuthorID, Body: in.Body, CreatedAt: nowMillis()}
		if in.ThreadID == "" {
			thread.RootMessageID = message.ID
			thread.CreatedAt = message.CreatedAt
		}
		insertThreadID := any(message.ThreadID)
		if in.ThreadID == "" {
			insertThreadID = nil
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO channel_messages ("+channelMessageColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", message.ID, message.ChannelID, message.ProjectID, message.AuthorType, message.AuthorID, message.Body, marshalJSON(message.References), nil, message.CreatedAt, insertThreadID, message.InvokedAgentID, message.InvocationRequestID); err != nil {
			return domain.ConversationInvocation{}, err
		}
		if in.ThreadID == "" {
			if _, err := tx.ExecContext(ctx, "INSERT INTO conversation_threads ("+threadColumns+") VALUES (?, ?, ?, ?, ?, ?)", thread.ID, thread.ProjectID, thread.ChannelID, thread.RootMessageID, thread.CreatedAt, thread.SelectedAgentID); err != nil {
				return domain.ConversationInvocation{}, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE channel_messages SET thread_id = ? WHERE id = ?", thread.ID, message.ID); err != nil {
				return domain.ConversationInvocation{}, err
			}
		}
		event, err := insertEventTx(ctx, tx, nil, nil, nil, "channel.message.created", "Channel message posted", map[string]any{"project_id": projectID, "channel_id": in.ChannelID, "message_id": message.ID, "thread_id": thread.ID})
		if err != nil {
			return domain.ConversationInvocation{}, err
		}
		events = append(events, event)
	} else {
		if in.ThreadID != "" && in.ThreadID != thread.ID {
			return domain.ConversationInvocation{}, fmt.Errorf("message belongs to another thread: %w", domain.ErrInvalidInput)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE channel_messages SET thread_id = ?, invoked_agent_id = ?, invocation_request_id = ? WHERE id = ?", thread.ID, in.AgentID, in.RequestID, message.ID); err != nil {
			return domain.ConversationInvocation{}, err
		}
		message.ThreadID, message.InvokedAgentID, message.InvocationRequestID = thread.ID, in.AgentID, in.RequestID
	}
	if thread.SelectedAgentID == "" {
		if _, err := tx.ExecContext(ctx, "UPDATE conversation_threads SET selected_agent_id = ? WHERE id = ?", in.AgentID, thread.ID); err != nil {
			return domain.ConversationInvocation{}, err
		}
		thread.SelectedAgentID = in.AgentID
	}
	if threadCreated {
		event, err := insertEventTx(ctx, tx, nil, nil, nil, "conversation.thread.created", "Conversation thread created", map[string]any{"project_id": projectID, "channel_id": in.ChannelID, "thread_id": thread.ID, "message_id": thread.RootMessageID})
		if err != nil {
			return domain.ConversationInvocation{}, err
		}
		events = append(events, event)
	}
	event, err := insertEventTx(ctx, tx, nil, nil, nil, "conversation.agent.invoked", "Conversation agent requested", map[string]any{"project_id": projectID, "channel_id": in.ChannelID, "thread_id": thread.ID, "message_id": message.ID, "agent_id": in.AgentID})
	if err != nil {
		return domain.ConversationInvocation{}, err
	}
	events = append(events, event)
	if err := tx.Commit(); err != nil {
		return domain.ConversationInvocation{}, err
	}
	for _, event := range events {
		s.publish(event)
	}
	thread, err = s.GetThread(ctx, thread.ID)
	if err != nil {
		return domain.ConversationInvocation{}, err
	}
	return domain.ConversationInvocation{Thread: thread, Message: message}, nil
}
