package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func (s *Store) exportThreads(ctx context.Context, projectID string) ([]domain.ConversationThread, error) {
	channels, err := s.ListChannels(ctx, projectID)
	if err != nil {
		return nil, err
	}
	threads := []domain.ConversationThread{}
	for _, channel := range channels {
		items, err := s.ListChannelThreads(ctx, channel.ID)
		if err != nil {
			return nil, err
		}
		threads = append(threads, items...)
	}
	return threads, nil
}

func importThreads(ctx context.Context, tx *sql.Tx, bundle portableBundle) error {
	for _, thread := range bundle.Threads {
		if thread.SelectedAgentID != "" {
			var count int
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM agents WHERE id = ?", thread.SelectedAgentID).Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				return fmt.Errorf("selected conversation agent missing: %w", domain.ErrInvalidInput)
			}
		}
		var projectID, channelID string
		if err := tx.QueryRowContext(ctx, "SELECT project_id,channel_id FROM channel_messages WHERE id = ?", thread.RootMessageID).Scan(&projectID, &channelID); err != nil {
			return fmt.Errorf("thread root missing: %w", domain.ErrInvalidInput)
		}
		var channelProject string
		if err := tx.QueryRowContext(ctx, "SELECT project_id FROM channels WHERE id = ?", channelID).Scan(&channelProject); err != nil {
			return err
		}
		if thread.ID == "" || thread.ProjectID != bundle.Project.ID || projectID != thread.ProjectID || channelProject != projectID || channelID != thread.ChannelID {
			return fmt.Errorf("thread root or project mismatch: %w", domain.ErrInvalidInput)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO conversation_threads ("+threadColumns+") VALUES (?, ?, ?, ?, ?, ?)", thread.ID, thread.ProjectID, thread.ChannelID, thread.RootMessageID, thread.CreatedAt, thread.SelectedAgentID); err != nil {
			return fmt.Errorf("invalid imported thread: %w", domain.ErrInvalidInput)
		}
		for _, taskID := range thread.TaskIDs {
			if _, err := linkThreadTaskTx(ctx, tx, thread.ID, taskID); err != nil {
				return err
			}
		}
	}
	for _, message := range bundle.ChannelMessages {
		if (message.InvokedAgentID == "") != (message.InvocationRequestID == "") || (message.InvokedAgentID != "" && (message.ThreadID == "" || message.AuthorType != "human")) {
			return fmt.Errorf("invalid imported invocation: %w", domain.ErrInvalidInput)
		}
		if message.ThreadID == "" {
			if message.InvokedAgentID != "" {
				return fmt.Errorf("invocation thread missing: %w", domain.ErrInvalidInput)
			}
			continue
		}
		var projectID, channelID, selectedAgentID string
		if err := tx.QueryRowContext(ctx, "SELECT project_id,channel_id,selected_agent_id FROM conversation_threads WHERE id = ?", message.ThreadID).Scan(&projectID, &channelID, &selectedAgentID); err != nil {
			return fmt.Errorf("message thread missing: %w", domain.ErrInvalidInput)
		}
		if projectID != bundle.Project.ID || projectID != message.ProjectID || channelID != message.ChannelID || (message.InvokedAgentID != "" && selectedAgentID != message.InvokedAgentID) {
			return fmt.Errorf("message thread mismatch: %w", domain.ErrInvalidInput)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE channel_messages SET thread_id = ? WHERE id = ?", message.ThreadID, message.ID); err != nil {
			return err
		}
	}
	for _, thread := range bundle.Threads {
		var rootThread sql.NullString
		if err := tx.QueryRowContext(ctx, "SELECT thread_id FROM channel_messages WHERE id = ?", thread.RootMessageID).Scan(&rootThread); err != nil {
			return err
		}
		if rootThread.String != thread.ID {
			return fmt.Errorf("thread must contain its root message: %w", domain.ErrInvalidInput)
		}
	}
	// Older exports have no thread records. Reconstruct only the explicit
	// promotion links, retaining all message and task IDs.
	for _, message := range bundle.ChannelMessages {
		if message.PromotedTaskID == nil {
			continue
		}
		if bundle.Threads == nil {
			thread, _, err := ensureThreadTx(ctx, tx, message.ChannelID, message.ID)
			if err != nil {
				return err
			}
			if _, err := linkThreadTaskTx(ctx, tx, thread.ID, *message.PromotedTaskID); err != nil {
				return err
			}
		} else {
			var origin string
			if err := tx.QueryRowContext(ctx, "SELECT thread_id FROM thread_tasks WHERE task_id = ?", *message.PromotedTaskID).Scan(&origin); err != nil || origin != message.ThreadID {
				return fmt.Errorf("promotion must retain its originating thread: %w", domain.ErrInvalidInput)
			}
		}
	}
	return nil
}
