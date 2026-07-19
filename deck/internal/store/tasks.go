package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

const taskColumns = `
	id, project_id, title, description, acceptance_criteria, status, priority,
	budget_minutes, assigned_agent_id, version, created_at, updated_at
`

func scanTask(scanner interface{ Scan(...any) error }) (domain.Task, error) {
	var task domain.Task
	var assignedAgentID sql.NullString
	if err := scanner.Scan(
		&task.ID,
		&task.ProjectID,
		&task.Title,
		&task.Description,
		&task.AcceptanceCriteria,
		&task.Status,
		&task.Priority,
		&task.BudgetMinutes,
		&assignedAgentID,
		&task.Version,
		&task.CreatedAt,
		&task.UpdatedAt,
	); err != nil {
		return domain.Task{}, err
	}
	if assignedAgentID.Valid {
		task.AssignedAgentID = stringPtr(assignedAgentID.String)
	}
	return task, nil
}

func validateTaskInput(input domain.CreateTaskInput) (domain.CreateTaskInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.AcceptanceCriteria = strings.TrimSpace(input.AcceptanceCriteria)
	if input.ProjectID == "" {
		input.ProjectID = defaultProjectID
	}
	if input.Status == "" {
		input.Status = domain.TaskBacklog
	}
	if input.Priority == "" {
		input.Priority = "medium"
	}
	if input.BudgetMinutes <= 0 {
		input.BudgetMinutes = 30
	}
	if input.Title == "" {
		return input, fmt.Errorf("title is required: %w", domain.ErrInvalidInput)
	}
	if input.Status != domain.TaskBacklog && input.Status != domain.TaskReady && input.Status != domain.TaskDone {
		return input, fmt.Errorf("new tasks must start in backlog or ready: %w", domain.ErrInvalidInput)
	}
	if !validPriority(input.Priority) {
		return input, fmt.Errorf("unknown priority %q: %w", input.Priority, domain.ErrInvalidInput)
	}
	return input, nil
}

func validPriority(value string) bool {
	switch value {
	case "low", "medium", "high", "urgent":
		return true
	default:
		return false
	}
}

func validTaskStatus(value string) bool {
	for _, status := range domain.TaskStatuses {
		if status == value {
			return true
		}
	}
	return false
}

func (s *Store) CreateTask(ctx context.Context, input domain.CreateTaskInput) (domain.Task, error) {
	validated, err := validateTaskInput(input)
	if err != nil {
		return domain.Task{}, err
	}
	now := nowMillis()
	task := domain.Task{
		ID:                 newID("tsk"),
		ProjectID:          validated.ProjectID,
		Title:              validated.Title,
		Description:        validated.Description,
		AcceptanceCriteria: validated.AcceptanceCriteria,
		Status:             validated.Status,
		Priority:           validated.Priority,
		BudgetMinutes:      validated.BudgetMinutes,
		Version:            1,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, fmt.Errorf("begin create task: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO tasks (
			id, project_id, title, description, acceptance_criteria, status,
			priority, budget_minutes, version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		task.ID,
		task.ProjectID,
		task.Title,
		task.Description,
		task.AcceptanceCriteria,
		task.Status,
		task.Priority,
		task.BudgetMinutes,
		task.Version,
		task.CreatedAt,
		task.UpdatedAt,
	)
	if err != nil {
		return domain.Task{}, fmt.Errorf("insert task: %w", err)
	}
	event, err := insertEventTx(
		ctx,
		tx,
		&task.ID,
		nil,
		nil,
		"task.created",
		fmt.Sprintf("Task created in %s", strings.ReplaceAll(task.Status, "_", " ")),
		map[string]any{"status": task.Status, "priority": task.Priority},
	)
	if err != nil {
		return domain.Task{}, fmt.Errorf("create task event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, fmt.Errorf("commit task: %w", err)
	}
	s.publish(event)
	return task, nil
}

func (s *Store) ListTasks(ctx context.Context) ([]domain.Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+taskColumns+`
		FROM tasks
		ORDER BY
			CASE priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END,
			updated_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	tasks := make([]domain.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tasks: %w", err)
	}
	return tasks, nil
}

func (s *Store) GetTask(ctx context.Context, id string) (domain.Task, error) {
	task, err := scanTask(s.db.QueryRowContext(ctx, `
		SELECT `+taskColumns+` FROM tasks WHERE id = ?
	`, id))
	if err != nil {
		return domain.Task{}, normalizeStoreError(err)
	}
	return task, nil
}

func (s *Store) UpdateTask(ctx context.Context, id string, input domain.UpdateTaskInput) (domain.Task, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return domain.Task{}, err
	}
	if input.Version != nil && *input.Version != task.Version {
		return domain.Task{}, fmt.Errorf("task has changed since it was opened: %w", domain.ErrConflict)
	}
	if input.Title != nil {
		task.Title = strings.TrimSpace(*input.Title)
	}
	if input.Description != nil {
		task.Description = strings.TrimSpace(*input.Description)
	}
	if input.AcceptanceCriteria != nil {
		task.AcceptanceCriteria = strings.TrimSpace(*input.AcceptanceCriteria)
	}
	if input.Priority != nil {
		task.Priority = strings.TrimSpace(*input.Priority)
	}
	if input.BudgetMinutes != nil {
		task.BudgetMinutes = *input.BudgetMinutes
	}
	if task.Title == "" || task.BudgetMinutes <= 0 || !validPriority(task.Priority) {
		return domain.Task{}, fmt.Errorf("invalid task fields: %w", domain.ErrInvalidInput)
	}

	now := nowMillis()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, fmt.Errorf("begin update task: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET title = ?, description = ?, acceptance_criteria = ?, priority = ?,
			budget_minutes = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?
	`,
		task.Title,
		task.Description,
		task.AcceptanceCriteria,
		task.Priority,
		task.BudgetMinutes,
		now,
		task.ID,
		task.Version,
	)
	if err != nil {
		return domain.Task{}, fmt.Errorf("update task: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return domain.Task{}, fmt.Errorf("read update result: %w", err)
	}
	if rows == 0 {
		return domain.Task{}, fmt.Errorf("task changed concurrently: %w", domain.ErrConflict)
	}
	event, err := insertEventTx(
		ctx, tx, &task.ID, nil, nil,
		"task.updated", "Task definition updated",
		map[string]any{"version": task.Version + 1},
	)
	if err != nil {
		return domain.Task{}, fmt.Errorf("create task update event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, fmt.Errorf("commit task update: %w", err)
	}
	task.Version++
	task.UpdatedAt = now
	s.publish(event)
	return task, nil
}

func (s *Store) MoveTask(ctx context.Context, id, target string) (domain.Task, error) {
	if !validTaskStatus(target) {
		return domain.Task{}, fmt.Errorf("unknown task status %q: %w", target, domain.ErrInvalidInput)
	}
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return domain.Task{}, err
	}
	if task.Status == target {
		return task, nil
	}
	if !manualTransitionAllowed(task.Status, target) {
		return domain.Task{}, fmt.Errorf("cannot move task from %s to %s: %w", task.Status, target, domain.ErrInvalidTransition)
	}

	now := nowMillis()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, fmt.Errorf("begin task move: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = ?, assigned_agent_id = NULL, version = version + 1, updated_at = ?
		WHERE id = ? AND status = ?
	`, target, now, id, task.Status)
	if err != nil {
		return domain.Task{}, fmt.Errorf("move task: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return domain.Task{}, fmt.Errorf("read task move result: %w", err)
	}
	if rows == 0 {
		return domain.Task{}, fmt.Errorf("task changed concurrently: %w", domain.ErrConflict)
	}
	event, err := insertEventTx(
		ctx,
		tx,
		&id,
		nil,
		nil,
		"task.moved",
		fmt.Sprintf("Moved from %s to %s", strings.ReplaceAll(task.Status, "_", " "), strings.ReplaceAll(target, "_", " ")),
		map[string]any{"from": task.Status, "to": target},
	)
	if err != nil {
		return domain.Task{}, fmt.Errorf("create move event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, fmt.Errorf("commit task move: %w", err)
	}
	task.Status = target
	task.AssignedAgentID = nil
	task.Version++
	task.UpdatedAt = now
	s.publish(event)
	return task, nil
}

func manualTransitionAllowed(from, to string) bool {
	switch from {
	case domain.TaskBacklog:
		return to == domain.TaskReady || to == domain.TaskCancelled
	case domain.TaskReady:
		return to == domain.TaskBacklog || to == domain.TaskCancelled
	case domain.TaskBlocked, domain.TaskFailed, domain.TaskCancelled, domain.TaskDone:
		return to == domain.TaskBacklog || to == domain.TaskReady
	default:
		return false
	}
}

func (s *Store) NextReadyTask(ctx context.Context) (domain.Task, error) {
	task, err := scanTask(s.db.QueryRowContext(ctx, `
		SELECT `+taskColumns+`
		FROM tasks
		WHERE status = 'ready'
		ORDER BY
			CASE priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END,
			created_at ASC
		LIMIT 1
	`))
	if err != nil {
		return domain.Task{}, normalizeStoreError(err)
	}
	return task, nil
}

func (s *Store) ListEvents(ctx context.Context, limit int, afterID int64) ([]domain.Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `
		SELECT id, task_id, run_id, agent_id, type, message, payload_json, created_at
		FROM events
	`
	args := make([]any, 0, 2)
	if afterID > 0 {
		query += " WHERE id > ? ORDER BY id ASC LIMIT ?"
		args = append(args, afterID, limit)
	} else {
		query += " ORDER BY id DESC LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	events := make([]domain.Event, 0)
	for rows.Next() {
		event, err := eventFromRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) GetTaskDetail(ctx context.Context, id string) (domain.TaskDetail, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return domain.TaskDetail{}, err
	}
	runs, err := s.listRunsForTask(ctx, id)
	if err != nil {
		return domain.TaskDetail{}, err
	}
	events, err := s.listEventsForTask(ctx, id)
	if err != nil {
		return domain.TaskDetail{}, err
	}
	decisions, err := s.listDecisionsForTask(ctx, id)
	if err != nil {
		return domain.TaskDetail{}, err
	}
	artifacts, err := s.listArtifactsForTask(ctx, id)
	if err != nil {
		return domain.TaskDetail{}, err
	}
	return domain.TaskDetail{
		Task: task, Runs: runs, Events: events, Decisions: decisions, Artifacts: artifacts,
	}, nil
}

func (s *Store) listEventsForTask(ctx context.Context, taskID string) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, run_id, agent_id, type, message, payload_json, created_at
		FROM events WHERE task_id = ? ORDER BY id DESC LIMIT 200
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task events: %w", err)
	}
	defer rows.Close()
	events := make([]domain.Event, 0)
	for rows.Next() {
		event, err := eventFromRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task event: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) TaskCount(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks").Scan(&count); err != nil {
		return 0, fmt.Errorf("count tasks: %w", err)
	}
	return count, nil
}

func (s *Store) SeedDemo(ctx context.Context) error {
	count, err := s.TaskCount(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	seed := []domain.CreateTaskInput{
		{
			Title:              "Capture the AppDeck product vision",
			Description:        "Write down the control-plane vision and keep the first release focused.",
			AcceptanceCriteria: "A committed vision document explains principles, lifecycle, architecture, and non-goals.",
			Status:             domain.TaskDone,
			Priority:           "high",
			BudgetMinutes:      30,
		},
		{
			Title:              "Validate the human approval loop",
			Description:        "Have an agent claim this task, propose a plan, wait for a decision, and finish with evidence.",
			AcceptanceCriteria: "The full lifecycle is visible in the timeline and the run includes a verification artifact.",
			Status:             domain.TaskReady,
			Priority:           "urgent",
			BudgetMinutes:      20,
		},
		{
			Title:              "Draft the first MCP tool contract",
			Description:        "Describe the smallest set of tools an external agent needs to participate in AppDeck.",
			AcceptanceCriteria: "The proposal covers discovery, claiming, planning, decisions, heartbeats, events, and completion.",
			Status:             domain.TaskBacklog,
			Priority:           "high",
			BudgetMinutes:      45,
		},
		{
			Title:              "Define run permission budgets",
			Description:        "Model time, attempts, tool permissions, and cost boundaries for a run.",
			AcceptanceCriteria: "A task can express enforceable budgets without storing secrets in its description.",
			Status:             domain.TaskBacklog,
			Priority:           "medium",
			BudgetMinutes:      60,
		},
	}
	for _, input := range seed {
		if _, err := s.CreateTask(ctx, input); err != nil {
			return fmt.Errorf("seed task %q: %w", input.Title, err)
		}
	}
	return nil
}

func isNotFound(err error) bool {
	return errors.Is(err, domain.ErrNotFound)
}
