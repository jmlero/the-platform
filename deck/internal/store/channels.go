package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func (s *Store) CreateChannel(ctx context.Context, projectID, name string) (domain.Channel, error) {
	projectID = strings.TrimSpace(projectID)
	name = strings.TrimSpace(name)
	if projectID == "" || name == "" {
		return domain.Channel{}, fmt.Errorf("project and channel name are required: %w", domain.ErrInvalidInput)
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects WHERE id = ?", projectID).Scan(&exists); err != nil {
		return domain.Channel{}, fmt.Errorf("inspect channel project: %w", err)
	}
	if exists != 1 {
		return domain.Channel{}, domain.ErrNotFound
	}
	channel := domain.Channel{ID: newID("chn"), ProjectID: projectID, Name: name, CreatedAt: nowMillis()}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO channels (id, project_id, name, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(project_id, name) DO NOTHING
	`, channel.ID, channel.ProjectID, channel.Name, channel.CreatedAt)
	if err != nil {
		return domain.Channel{}, fmt.Errorf("create channel: %w", err)
	}
	loaded, err := scanChannel(s.db.QueryRowContext(ctx,
		"SELECT id, project_id, name, created_at FROM channels WHERE project_id = ? AND name = ?",
		projectID, name,
	))
	if err != nil {
		return domain.Channel{}, normalizeStoreError(err)
	}
	return loaded, nil
}

func scanChannel(scanner interface{ Scan(...any) error }) (domain.Channel, error) {
	var channel domain.Channel
	err := scanner.Scan(&channel.ID, &channel.ProjectID, &channel.Name, &channel.CreatedAt)
	return channel, err
}

func (s *Store) GetChannel(ctx context.Context, id string) (domain.Channel, error) {
	channel, err := scanChannel(s.db.QueryRowContext(ctx,
		"SELECT id, project_id, name, created_at FROM channels WHERE id = ?", id,
	))
	if err != nil {
		return domain.Channel{}, normalizeStoreError(err)
	}
	return channel, nil
}

func (s *Store) ListChannels(ctx context.Context, projectID string) ([]domain.Channel, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, name, created_at FROM channels
		WHERE project_id = ? ORDER BY name, id
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()
	channels := make([]domain.Channel, 0)
	for rows.Next() {
		channel, scanErr := scanChannel(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan channel: %w", scanErr)
		}
		channels = append(channels, channel)
	}
	return channels, rows.Err()
}

func (s *Store) PostChannelMessage(ctx context.Context, input domain.ChannelMessageInput) (domain.ChannelMessage, error) {
	input.ChannelID = strings.TrimSpace(input.ChannelID)
	input.ThreadID = strings.TrimSpace(input.ThreadID)
	input.AuthorType = strings.ToLower(strings.TrimSpace(input.AuthorType))
	input.AuthorID = strings.TrimSpace(input.AuthorID)
	input.Body = strings.TrimSpace(input.Body)
	if input.ChannelID == "" || input.AuthorID == "" || input.Body == "" {
		return domain.ChannelMessage{}, fmt.Errorf("channel, author, and body are required: %w", domain.ErrInvalidInput)
	}
	if input.AuthorType != "human" && input.AuthorType != "agent" && input.AuthorType != "system" {
		return domain.ChannelMessage{}, fmt.Errorf("unknown channel author type: %w", domain.ErrInvalidInput)
	}
	channel, err := scanChannel(s.db.QueryRowContext(ctx,
		"SELECT id, project_id, name, created_at FROM channels WHERE id = ?", input.ChannelID,
	))
	if err != nil {
		return domain.ChannelMessage{}, normalizeStoreError(err)
	}
	if err := s.validateCanonicalReferences(ctx, channel.ProjectID, input.References); err != nil {
		return domain.ChannelMessage{}, err
	}
	// Thread ownership is immutable; validate before opening the write transaction
	// to avoid SQLite read-to-write lock upgrades across concurrent replies.
	if input.ThreadID != "" {
		var threadChannel string
		if err := s.db.QueryRowContext(ctx, "SELECT channel_id FROM conversation_threads WHERE id = ?", input.ThreadID).Scan(&threadChannel); err != nil {
			return domain.ChannelMessage{}, normalizeStoreError(err)
		}
		if threadChannel != channel.ID {
			return domain.ChannelMessage{}, fmt.Errorf("thread does not belong to this channel: %w", domain.ErrInvalidInput)
		}
	}
	message := domain.ChannelMessage{
		ID: newID("msg"), ChannelID: channel.ID, ProjectID: channel.ProjectID, ThreadID: input.ThreadID,
		AuthorType: input.AuthorType, AuthorID: input.AuthorID, Body: input.Body,
		References: input.References, CreatedAt: nowMillis(),
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ChannelMessage{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO channel_messages (
			id, channel_id, project_id, author_type, author_id, body, references_json, created_at, thread_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, message.ID, message.ChannelID, message.ProjectID, message.AuthorType,
		message.AuthorID, message.Body, marshalJSON(message.References), message.CreatedAt, nullableText(message.ThreadID)); err != nil {
		return domain.ChannelMessage{}, fmt.Errorf("post channel message: %w", err)
	}
	event, err := insertEventTx(ctx, tx, nil, nil, nil, "channel.message.created", "Channel message posted", map[string]any{"project_id": channel.ProjectID, "channel_id": channel.ID, "message_id": message.ID, "thread_id": message.ThreadID})
	if err != nil {
		return domain.ChannelMessage{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.ChannelMessage{}, err
	}
	s.publish(event)
	return message, nil
}

func (s *Store) validateCanonicalReferences(ctx context.Context, projectID string, refs domain.CanonicalReferences) error {
	checks := []struct {
		kind  string
		ids   []string
		query string
	}{
		{"task", refs.TaskIDs, "SELECT COUNT(*) FROM tasks WHERE id = ? AND project_id = ?"},
		{"run", refs.RunIDs, "SELECT COUNT(*) FROM runs r JOIN tasks t ON t.id = r.task_id WHERE r.id = ? AND t.project_id = ?"},
		{"decision", refs.DecisionIDs, "SELECT COUNT(*) FROM decisions d JOIN tasks t ON t.id = d.task_id WHERE d.id = ? AND t.project_id = ?"},
		{"artifact", refs.ArtifactIDs, "SELECT COUNT(*) FROM artifacts a JOIN tasks t ON t.id = a.task_id WHERE a.id = ? AND t.project_id = ?"},
		{"evidence", refs.EvidenceIDs, "SELECT COUNT(*) FROM evidence_records e JOIN tasks t ON t.id = e.task_id WHERE e.id = ? AND t.project_id = ?"},
	}
	for _, check := range checks {
		for _, id := range check.ids {
			var count int
			if err := s.db.QueryRowContext(ctx, check.query, id, projectID).Scan(&count); err != nil {
				return fmt.Errorf("validate %s reference: %w", check.kind, err)
			}
			if count != 1 {
				return fmt.Errorf("%s reference %q is not in the channel project: %w", check.kind, id, domain.ErrInvalidInput)
			}
		}
	}
	return nil
}

func scanChannelMessage(scanner interface{ Scan(...any) error }) (domain.ChannelMessage, error) {
	var message domain.ChannelMessage
	var references, prdJSON, prdTasksJSON string
	var promotedTaskID, threadID, requestID sql.NullString
	if err := scanner.Scan(
		&message.ID, &message.ChannelID, &message.ProjectID, &message.AuthorType,
		&message.AuthorID, &message.Body, &references, &promotedTaskID, &message.CreatedAt, &threadID, &message.InvokedAgentID, &requestID, &prdJSON, &prdTasksJSON,
	); err != nil {
		return domain.ChannelMessage{}, err
	}
	if err := json.Unmarshal([]byte(prdTasksJSON), &message.PRDTasks); err != nil {
		return domain.ChannelMessage{}, err
	}
	if err := json.Unmarshal([]byte(prdJSON), &message.PRD); err != nil {
		return domain.ChannelMessage{}, err
	}
	message.ThreadID = threadID.String
	message.InvocationRequestID = requestID.String
	if references != "" {
		if err := json.Unmarshal([]byte(references), &message.References); err != nil {
			return domain.ChannelMessage{}, fmt.Errorf("decode message references: %w", err)
		}
	}
	if promotedTaskID.Valid {
		value := promotedTaskID.String
		message.PromotedTaskID = &value
	}
	return message, nil
}

// Channel cursors name a persisted message, never a transient row offset.
// SQLite insertion order keeps concurrent appends and equal timestamps stable.
type ChannelMessagePage struct {
	Items      []domain.ChannelMessage `json:"items"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

func (s *Store) PageChannelMessages(ctx context.Context, channelID string, limit int, cursor string) (ChannelMessagePage, error) {
	return s.pageChannelMessages(ctx, channelID, "", limit, cursor)
}

func (s *Store) pageChannelMessages(ctx context.Context, channelID, threadID string, limit int, cursor string) (ChannelMessagePage, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var after int64
	if cursor != "" {
		id, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return ChannelMessagePage{}, fmt.Errorf("invalid channel cursor: %w", domain.ErrInvalidInput)
		}
		if err = s.db.QueryRowContext(ctx, "SELECT rowid FROM channel_messages WHERE id = ? AND channel_id = ? AND (? = '' OR thread_id = ?)", string(id), channelID, threadID, threadID).Scan(&after); err != nil {
			if err == sql.ErrNoRows {
				return ChannelMessagePage{}, fmt.Errorf("cursor does not belong to this conversation: %w", domain.ErrInvalidInput)
			}
			return ChannelMessagePage{}, err
		}
	}
	rows, err := s.db.QueryContext(ctx, `
 SELECT id, channel_id, project_id, author_type, author_id, body, references_json, promoted_task_id, created_at, thread_id, invoked_agent_id, invocation_request_id, prd_json, prd_tasks_json
 FROM channel_messages WHERE channel_id = ? AND (? = '' OR thread_id = ?) AND rowid > ? ORDER BY rowid LIMIT ?`, channelID, threadID, threadID, after, limit+1)
	if err != nil {
		return ChannelMessagePage{}, fmt.Errorf("list channel messages: %w", err)
	}
	defer rows.Close()
	page := ChannelMessagePage{Items: make([]domain.ChannelMessage, 0)}
	for rows.Next() {
		message, err := scanChannelMessage(rows)
		if err != nil {
			return ChannelMessagePage{}, err
		}
		page.Items = append(page.Items, message)
	}
	if err = rows.Err(); err != nil {
		return ChannelMessagePage{}, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(page.Items[limit-1].ID))
	}
	return page, nil
}

func (s *Store) PromoteChannelMessage(ctx context.Context, messageID string, input domain.CreateTaskInput) (domain.Task, error) {
	message, err := scanChannelMessage(s.db.QueryRowContext(ctx, `
		SELECT id, channel_id, project_id, author_type, author_id, body,
			references_json, promoted_task_id, created_at, thread_id, invoked_agent_id, invocation_request_id, prd_json, prd_tasks_json
		FROM channel_messages WHERE id = ?
	`, messageID))
	if err != nil {
		return domain.Task{}, normalizeStoreError(err)
	}
	if message.PromotedTaskID != nil {
		return s.GetTask(ctx, *message.PromotedTaskID)
	}
	input.ProjectID = message.ProjectID
	if strings.TrimSpace(input.Description) == "" {
		input.Description = message.Body
	}
	if input.Status == domain.TaskReady && (strings.TrimSpace(input.AcceptanceCriteria) == "" || input.Budget == nil) {
		return domain.Task{}, fmt.Errorf("promoted work requires acceptance criteria and a structured budget: %w", domain.ErrInvalidInput)
	}
	validated, err := validateTaskInput(input)
	if err != nil {
		return domain.Task{}, err
	}
	now := nowMillis()
	task := domain.Task{
		ID: newID("tsk"), ProjectID: validated.ProjectID, Title: validated.Title,
		Description: validated.Description, AcceptanceCriteria: validated.AcceptanceCriteria,
		Status: validated.Status, Priority: validated.Priority,
		BudgetMinutes: validated.BudgetMinutes, Budget: *validated.Budget,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, fmt.Errorf("begin message promotion: %w", err)
	}
	defer tx.Rollback()
	ids := validated.ContextMessageIDs
	if !slices.Contains(ids, message.ID) {
		ids = append(ids, message.ID)
	}
	task.Context, err = selectTaskContext(ctx, tx, task.ProjectID, ids, task.Status == domain.TaskReady)
	if err != nil {
		return domain.Task{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tasks (
			id, project_id, title, description, acceptance_criteria, status,
			priority, budget_minutes, deadline_at, max_attempts, max_tokens,
			max_cost_minor, currency, max_concurrency, authority_json,
			version, created_at, updated_at, context_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, task.ID, task.ProjectID, task.Title, task.Description, task.AcceptanceCriteria,
		task.Status, task.Priority, task.BudgetMinutes, nullableInt64(task.Budget.DeadlineAt),
		task.Budget.MaxAttempts, task.Budget.MaxTokens, task.Budget.MaxCostMinor,
		task.Budget.Currency, task.Budget.MaxConcurrency, marshalJSON(task.Budget.Authority),
		task.Version, task.CreatedAt, task.UpdatedAt, marshalJSON(task.Context)); err != nil {
		return domain.Task{}, fmt.Errorf("create promoted task: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE channel_messages SET promoted_task_id = ?
		WHERE id = ? AND promoted_task_id IS NULL
	`, task.ID, message.ID)
	if err != nil {
		return domain.Task{}, fmt.Errorf("bind promoted task: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return domain.Task{}, fmt.Errorf("message was promoted concurrently: %w", domain.ErrConflict)
	}
	thread, _, err := ensureThreadTx(ctx, tx, message.ChannelID, message.ID)
	if err != nil {
		return domain.Task{}, err
	}
	if _, err := linkThreadTaskTx(ctx, tx, thread.ID, task.ID); err != nil {
		return domain.Task{}, err
	}
	event, err := insertEventTx(ctx, tx, &task.ID, nil, nil,
		"task.promoted_from_channel", "Channel message promoted into bounded work",
		map[string]any{"channel_id": message.ChannelID, "message_id": message.ID, "thread_id": thread.ID},
	)
	if err != nil {
		return domain.Task{}, fmt.Errorf("create promotion event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, fmt.Errorf("commit message promotion: %w", err)
	}
	s.publish(event)
	return task, nil
}
