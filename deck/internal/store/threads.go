package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

const threadColumns = "id, project_id, channel_id, root_message_id, created_at, selected_agent_id"

func scanThread(row interface{ Scan(...any) error }) (domain.ConversationThread, error) {
	thread := domain.ConversationThread{TaskIDs: []string{}}
	err := row.Scan(&thread.ID, &thread.ProjectID, &thread.ChannelID, &thread.RootMessageID, &thread.CreatedAt, &thread.SelectedAgentID)
	return thread, normalizeStoreError(err)
}

// ensureThreadTx serializes creation on the canonical message, so retrying a
// request from another device resolves to the same persisted thread.
func ensureThreadTx(ctx context.Context, tx *sql.Tx, channelID, messageID string) (domain.ConversationThread, bool, error) {
	if _, err := tx.ExecContext(ctx, "UPDATE channel_messages SET id = id WHERE id = ?", messageID); err != nil {
		return domain.ConversationThread{}, false, err
	}
	var projectID, actualChannel string
	var threadID sql.NullString
	var createdAt int64
	if err := tx.QueryRowContext(ctx, "SELECT project_id, channel_id, thread_id, created_at FROM channel_messages WHERE id = ?", messageID).Scan(&projectID, &actualChannel, &threadID, &createdAt); err != nil {
		return domain.ConversationThread{}, false, normalizeStoreError(err)
	}
	if channelID != actualChannel {
		return domain.ConversationThread{}, false, fmt.Errorf("message does not belong to this channel: %w", domain.ErrInvalidInput)
	}
	if threadID.Valid {
		thread, err := scanThread(tx.QueryRowContext(ctx, "SELECT "+threadColumns+" FROM conversation_threads WHERE id = ?", threadID.String))
		return thread, false, err
	}
	thread := domain.ConversationThread{ID: newID("thr"), ProjectID: projectID, ChannelID: channelID, RootMessageID: messageID, TaskIDs: []string{}, CreatedAt: createdAt}
	if _, err := tx.ExecContext(ctx, "INSERT INTO conversation_threads ("+threadColumns+") VALUES (?, ?, ?, ?, ?, ?)", thread.ID, thread.ProjectID, thread.ChannelID, thread.RootMessageID, thread.CreatedAt, thread.SelectedAgentID); err != nil {
		return domain.ConversationThread{}, false, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE channel_messages SET thread_id = ? WHERE id = ?", thread.ID, messageID); err != nil {
		return domain.ConversationThread{}, false, err
	}
	return thread, true, nil
}

func (s *Store) CreateThread(ctx context.Context, channelID, messageID string) (domain.ConversationThread, error) {
	channelID, messageID = strings.TrimSpace(channelID), strings.TrimSpace(messageID)
	if channelID == "" || messageID == "" {
		return domain.ConversationThread{}, fmt.Errorf("channel and root message are required: %w", domain.ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ConversationThread{}, err
	}
	defer tx.Rollback()
	thread, created, err := ensureThreadTx(ctx, tx, channelID, messageID)
	if err != nil {
		return domain.ConversationThread{}, err
	}
	var event domain.Event
	if created {
		event, err = insertEventTx(ctx, tx, nil, nil, nil, "conversation.thread.created", "Conversation thread created", map[string]any{"project_id": thread.ProjectID, "channel_id": channelID, "thread_id": thread.ID, "message_id": thread.RootMessageID})
		if err != nil {
			return domain.ConversationThread{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return domain.ConversationThread{}, err
	}
	if created {
		s.publish(event)
	}
	return s.GetThread(ctx, thread.ID)
}

func (s *Store) GetThread(ctx context.Context, id string) (domain.ConversationThread, error) {
	thread, err := scanThread(s.db.QueryRowContext(ctx, "SELECT "+threadColumns+" FROM conversation_threads WHERE id = ?", id))
	if err != nil {
		return domain.ConversationThread{}, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT task_id FROM thread_tasks WHERE thread_id = ? ORDER BY task_id", id)
	if err != nil {
		return domain.ConversationThread{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return domain.ConversationThread{}, err
		}
		thread.TaskIDs = append(thread.TaskIDs, taskID)
	}
	return thread, rows.Err()
}

func (s *Store) GetTaskThread(ctx context.Context, taskID string) (domain.ConversationThread, error) {
	var id string
	if err := s.db.QueryRowContext(ctx, "SELECT thread_id FROM thread_tasks WHERE task_id = ?", taskID).Scan(&id); err != nil {
		return domain.ConversationThread{}, normalizeStoreError(err)
	}
	return s.GetThread(ctx, id)
}

// ListChannelThreads is ordered by immutable root append order, not activity.
func (s *Store) ListChannelThreads(ctx context.Context, channelID string) ([]domain.ConversationThread, error) {
	if _, err := s.GetChannel(ctx, channelID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT t.id FROM conversation_threads t JOIN channel_messages m ON m.id = t.root_message_id WHERE t.channel_id = ? ORDER BY m.rowid", channelID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	threads := make([]domain.ConversationThread, 0, len(ids))
	for _, id := range ids {
		thread, err := s.GetThread(ctx, id)
		if err != nil {
			return nil, err
		}
		threads = append(threads, thread)
	}
	return threads, nil
}

func linkThreadTaskTx(ctx context.Context, tx *sql.Tx, threadID, taskID string) (bool, error) {
	// Acquire the writer before reads to serialize conflicting origin assignments.
	if _, err := tx.ExecContext(ctx, "UPDATE conversation_threads SET id = id WHERE id = ?", threadID); err != nil {
		return false, err
	}
	var threadProject, taskProject string
	if err := tx.QueryRowContext(ctx, "SELECT project_id FROM conversation_threads WHERE id = ?", threadID).Scan(&threadProject); err != nil {
		return false, normalizeStoreError(err)
	}
	if err := tx.QueryRowContext(ctx, "SELECT project_id FROM tasks WHERE id = ?", taskID).Scan(&taskProject); err != nil {
		return false, normalizeStoreError(err)
	}
	if threadProject != taskProject {
		return false, fmt.Errorf("task does not belong to the thread project: %w", domain.ErrInvalidInput)
	}
	var existing string
	err := tx.QueryRowContext(ctx, "SELECT thread_id FROM thread_tasks WHERE task_id = ?", taskID).Scan(&existing)
	if err == nil {
		if existing != threadID {
			return false, fmt.Errorf("task already has an originating thread: %w", domain.ErrConflict)
		}
		return false, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO thread_tasks (task_id, thread_id) VALUES (?, ?)", taskID, threadID)
	return err == nil, err
}

func (s *Store) LinkThreadTask(ctx context.Context, threadID, taskID string) error {
	if strings.TrimSpace(threadID) == "" || strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("thread and task are required: %w", domain.ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	created, err := linkThreadTaskTx(ctx, tx, threadID, taskID)
	if err != nil {
		return err
	}
	var event domain.Event
	if created {
		event, err = insertEventTx(ctx, tx, &taskID, nil, nil, "conversation.task.linked", "Task linked to conversation", map[string]any{"thread_id": threadID})
		if err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if created {
		s.publish(event)
	}
	return nil
}

func (s *Store) PageThreadMessages(ctx context.Context, threadID string, limit int, cursor string) (ChannelMessagePage, error) {
	thread, err := s.GetThread(ctx, threadID)
	if err != nil {
		return ChannelMessagePage{}, err
	}
	return s.pageChannelMessages(ctx, thread.ChannelID, threadID, limit, cursor)
}
