package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

const taskColumns = `
	id, project_id, title, description, acceptance_criteria, status, priority,
	budget_minutes, deadline_at, max_attempts, max_tokens, max_cost_minor,
	currency, max_concurrency, authority_json,
	assigned_agent_id, version, created_at, updated_at
`

func scanTask(scanner interface{ Scan(...any) error }) (domain.Task, error) {
	var task domain.Task
	var assignedAgentID sql.NullString
	var deadlineAt sql.NullInt64
	var authority string
	if err := scanner.Scan(
		&task.ID,
		&task.ProjectID,
		&task.Title,
		&task.Description,
		&task.AcceptanceCriteria,
		&task.Status,
		&task.Priority,
		&task.BudgetMinutes,
		&deadlineAt,
		&task.Budget.MaxAttempts,
		&task.Budget.MaxTokens,
		&task.Budget.MaxCostMinor,
		&task.Budget.Currency,
		&task.Budget.MaxConcurrency,
		&authority,
		&assignedAgentID,
		&task.Version,
		&task.CreatedAt,
		&task.UpdatedAt,
	); err != nil {
		return domain.Task{}, err
	}
	task.Budget.TimeMinutes = task.BudgetMinutes
	if deadlineAt.Valid {
		value := deadlineAt.Int64
		task.Budget.DeadlineAt = &value
	}
	task.Budget.Authority = unmarshalStrings(authority)
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
	budget := domain.Budget{TimeMinutes: input.BudgetMinutes}
	if input.Budget != nil {
		budget = *input.Budget
	}
	budget, err := normalizeBudget(budget, input.BudgetMinutes)
	if err != nil {
		return input, err
	}
	input.BudgetMinutes = budget.TimeMinutes
	input.Budget = &budget
	if input.Title == "" {
		return input, fmt.Errorf("title is required: %w", domain.ErrInvalidInput)
	}
	if input.Status != domain.TaskBacklog && input.Status != domain.TaskReady && input.Status != domain.TaskDone {
		return input, fmt.Errorf("new tasks must start in backlog or ready: %w", domain.ErrInvalidInput)
	}
	if !validPriority(input.Priority) {
		return input, fmt.Errorf("unknown priority %q: %w", input.Priority, domain.ErrInvalidInput)
	}
	if input.Status == domain.TaskReady {
		if err := validateRepositoryReadiness(input.AcceptanceCriteria, budget); err != nil {
			return input, err
		}
	}
	return input, nil
}

// Source/PR permissions identify repository work without interpreting prose or
// imposing Marina's workflow on general work-control clients.
func validateRepositoryReadiness(criteria string, budget domain.Budget) error {
	repository := slices.ContainsFunc(budget.Authority, func(permission string) bool {
		return strings.HasPrefix(permission, "source.") || strings.HasPrefix(permission, "pull_request.")
	})
	if !repository {
		return nil
	}
	var missing []string
	if strings.TrimSpace(criteria) == "" {
		missing = append(missing, "observable acceptance criteria")
	}
	if budget.MaxAttempts < 2 {
		missing = append(missing, "at least two attempts for implementation and verification")
	}
	for _, permission := range []string{"source.read", "source.write", "pull_request.create"} {
		if !slices.Contains(budget.Authority, permission) {
			missing = append(missing, "explicit "+permission+" authority")
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("repository work is not ready: needs %s; save in backlog to refine: %w", strings.Join(missing, "; "), domain.ErrInvalidInput)
	}
	return nil
}

func normalizeBudget(budget domain.Budget, legacyMinutes int) (domain.Budget, error) {
	if budget.TimeMinutes <= 0 {
		budget.TimeMinutes = legacyMinutes
	}
	if budget.TimeMinutes <= 0 {
		budget.TimeMinutes = 30
	}
	if budget.MaxAttempts <= 0 {
		budget.MaxAttempts = 1
	}
	if budget.Currency == "" {
		budget.Currency = "USD"
	}
	budget.Currency = strings.ToUpper(strings.TrimSpace(budget.Currency))
	if budget.MaxConcurrency <= 0 {
		budget.MaxConcurrency = 1
	}
	if budget.Authority == nil {
		budget.Authority = []string{}
	}
	if len(budget.Currency) != 3 || budget.MaxTokens < 0 || budget.MaxCostMinor < 0 {
		return budget, fmt.Errorf("invalid structured budget: %w", domain.ErrInvalidInput)
	}
	return budget, nil
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
		Budget:             *validated.Budget,
		DisplayState:       strings.ReplaceAll(validated.Status, "_", "-"),
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
			priority, budget_minutes, deadline_at, max_attempts, max_tokens,
			max_cost_minor, currency, max_concurrency, authority_json,
			version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		task.ID,
		task.ProjectID,
		task.Title,
		task.Description,
		task.AcceptanceCriteria,
		task.Status,
		task.Priority,
		task.BudgetMinutes,
		nullableInt64(task.Budget.DeadlineAt),
		task.Budget.MaxAttempts,
		task.Budget.MaxTokens,
		task.Budget.MaxCostMinor,
		task.Budget.Currency,
		task.Budget.MaxConcurrency,
		marshalJSON(task.Budget.Authority),
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
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close tasks: %w", err)
	}
	providers := make(map[string]domain.ProviderStatus)
	for index := range tasks {
		provider, ok := providers[tasks[index].ProjectID]
		if !ok {
			provider, err = s.GetProviderStatus(ctx, tasks[index].ProjectID)
			if err != nil {
				return nil, err
			}
			providers[tasks[index].ProjectID] = provider
		}
		tasks[index].DisplayState, err = s.taskDisplayStateSummary(ctx, tasks[index], provider)
		if err != nil {
			return nil, err
		}
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
	provider, err := s.GetProviderStatus(ctx, task.ProjectID)
	if err != nil {
		return domain.Task{}, err
	}
	task.DisplayState, err = s.taskDisplayStateSummary(ctx, task, provider)
	if err != nil {
		return domain.Task{}, err
	}
	return task, nil
}

func (s *Store) taskDisplayStateSummary(ctx context.Context, task domain.Task, provider domain.ProviderStatus) (string, error) {
	commands := make([]domain.Command, 0, 1)
	var retryCount int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM commands c JOIN runs r ON r.id = c.run_id
		WHERE r.task_id = ? AND c.type = 'retry' AND c.status IN ('pending', 'acknowledged')
	`, task.ID).Scan(&retryCount); err != nil {
		return "", fmt.Errorf("inspect retry state: %w", err)
	}
	if retryCount > 0 {
		commands = append(commands, domain.Command{Type: domain.CommandRetry, Status: "pending"})
	}
	evidence := make([]domain.EvidenceRecord, 0, 1)
	var staleEvidenceCount int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM evidence_records
		WHERE task_id = ? AND admission_status IN ('stale', 'rejected')
	`, task.ID).Scan(&staleEvidenceCount); err != nil {
		return "", fmt.Errorf("inspect stale evidence: %w", err)
	}
	if staleEvidenceCount > 0 {
		evidence = append(evidence, domain.EvidenceRecord{AdmissionStatus: "stale"})
	}
	return taskDisplayState(task, commands, evidence, provider), nil
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
		task.Budget.TimeMinutes = *input.BudgetMinutes
	}
	if input.Budget != nil {
		budget, validateErr := normalizeBudget(*input.Budget, task.BudgetMinutes)
		if validateErr != nil {
			return domain.Task{}, validateErr
		}
		task.Budget = budget
		task.BudgetMinutes = task.Budget.TimeMinutes
	}
	if task.Title == "" || task.BudgetMinutes <= 0 || !validPriority(task.Priority) {
		return domain.Task{}, fmt.Errorf("invalid task fields: %w", domain.ErrInvalidInput)
	}
	if task.Status == domain.TaskReady {
		if err := validateRepositoryReadiness(task.AcceptanceCriteria, task.Budget); err != nil {
			return domain.Task{}, err
		}
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
			budget_minutes = ?, deadline_at = ?, max_attempts = ?, max_tokens = ?,
			max_cost_minor = ?, currency = ?, max_concurrency = ?, authority_json = ?,
			version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?
	`,
		task.Title,
		task.Description,
		task.AcceptanceCriteria,
		task.Priority,
		task.BudgetMinutes,
		nullableInt64(task.Budget.DeadlineAt),
		task.Budget.MaxAttempts,
		task.Budget.MaxTokens,
		task.Budget.MaxCostMinor,
		task.Budget.Currency,
		task.Budget.MaxConcurrency,
		marshalJSON(task.Budget.Authority),
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
	provider, providerErr := s.GetProviderStatus(ctx, task.ProjectID)
	if providerErr == nil {
		task.DisplayState, _ = s.taskDisplayStateSummary(ctx, task, provider)
	}
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
	if target == domain.TaskReady {
		if err := validateRepositoryReadiness(task.AcceptanceCriteria, task.Budget); err != nil {
			return domain.Task{}, err
		}
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
		WHERE id = ? AND status = ? AND version = ?
	`, target, now, id, task.Status, task.Version)
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
	provider, providerErr := s.GetProviderStatus(ctx, task.ProjectID)
	if providerErr == nil {
		task.DisplayState, _ = s.taskDisplayStateSummary(ctx, task, provider)
	}
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
	planRevisions, err := s.listPlanRevisionsForTask(ctx, id)
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
	evidence, err := s.listEvidenceForTask(ctx, id)
	if err != nil {
		return domain.TaskDetail{}, err
	}
	commands, err := s.listCommandsForTask(ctx, id)
	if err != nil {
		return domain.TaskDetail{}, err
	}
	operationalStates, err := s.listOperationalStatesForTask(ctx, id)
	if err != nil {
		return domain.TaskDetail{}, err
	}
	providerStatus, err := s.GetProviderStatus(ctx, task.ProjectID)
	if err != nil {
		return domain.TaskDetail{}, err
	}
	return domain.TaskDetail{
		Task: task, Runs: runs, PlanRevisions: planRevisions, Events: events,
		Decisions: decisions, Artifacts: artifacts, Evidence: evidence, Commands: commands,
		OperationalStates: operationalStates, ProviderStatus: providerStatus,
		DisplayState: taskDisplayState(task, commands, evidence, providerStatus),
		Trail:        buildTaskTrail(events, planRevisions, decisions, commands, evidence, operationalStates),
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
