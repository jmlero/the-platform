package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

const runColumns = `
	id, task_id, agent_id, status, plan, result_summary,
	lease_expires_at, started_at, finished_at, updated_at
`

func scanRun(scanner interface{ Scan(...any) error }) (domain.Run, error) {
	var run domain.Run
	var leaseExpiresAt, finishedAt sql.NullInt64
	if err := scanner.Scan(
		&run.ID,
		&run.TaskID,
		&run.AgentID,
		&run.Status,
		&run.Plan,
		&run.ResultSummary,
		&leaseExpiresAt,
		&run.StartedAt,
		&finishedAt,
		&run.UpdatedAt,
	); err != nil {
		return domain.Run{}, err
	}
	if leaseExpiresAt.Valid {
		value := leaseExpiresAt.Int64
		run.LeaseExpiresAt = &value
	}
	if finishedAt.Valid {
		value := finishedAt.Int64
		run.FinishedAt = &value
	}
	return run, nil
}

func scanAgent(scanner interface{ Scan(...any) error }) (domain.Agent, error) {
	var agent domain.Agent
	var capabilities string
	if err := scanner.Scan(
		&agent.ID,
		&agent.Name,
		&agent.Kind,
		&agent.Status,
		&capabilities,
		&agent.LastSeenAt,
		&agent.CreatedAt,
	); err != nil {
		return domain.Agent{}, err
	}
	agent.Capabilities = unmarshalStrings(capabilities)
	return agent, nil
}

func scanDecision(scanner interface{ Scan(...any) error }) (domain.Decision, error) {
	var decision domain.Decision
	var runID sql.NullString
	var options string
	var answeredAt sql.NullInt64
	if err := scanner.Scan(
		&decision.ID,
		&decision.TaskID,
		&runID,
		&decision.Question,
		&options,
		&decision.Recommendation,
		&decision.Status,
		&decision.Answer,
		&decision.CreatedAt,
		&answeredAt,
	); err != nil {
		return domain.Decision{}, err
	}
	if runID.Valid {
		decision.RunID = stringPtr(runID.String)
	}
	if answeredAt.Valid {
		value := answeredAt.Int64
		decision.AnsweredAt = &value
	}
	decision.Options = unmarshalStrings(options)
	return decision, nil
}

func (s *Store) RegisterAgent(ctx context.Context, agent domain.Agent) (domain.Agent, error) {
	agent.ID = strings.TrimSpace(agent.ID)
	agent.Name = strings.TrimSpace(agent.Name)
	if agent.ID == "" {
		agent.ID = newID("agt")
	}
	if agent.Name == "" {
		return domain.Agent{}, fmt.Errorf("agent name is required: %w", domain.ErrInvalidInput)
	}
	if agent.Kind == "" {
		agent.Kind = "external"
	}
	if agent.Status == "" {
		agent.Status = "idle"
	}
	now := nowMillis()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents (id, name, kind, status, capabilities_json, last_seen_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			kind = excluded.kind,
			capabilities_json = excluded.capabilities_json,
			last_seen_at = excluded.last_seen_at
	`, agent.ID, agent.Name, agent.Kind, agent.Status, marshalJSON(agent.Capabilities), now, now)
	if err != nil {
		return domain.Agent{}, fmt.Errorf("register agent: %w", err)
	}
	registered, err := s.GetAgent(ctx, agent.ID)
	if err != nil {
		return domain.Agent{}, err
	}
	s.publish(domain.Event{
		AgentID: &registered.ID,
		Type:    "agent.connected", Message: registered.Name + " connected",
		Payload: map[string]any{"status": registered.Status}, CreatedAt: now,
	})
	return registered, nil
}

func (s *Store) GetAgent(ctx context.Context, id string) (domain.Agent, error) {
	agent, err := scanAgent(s.db.QueryRowContext(ctx, `
		SELECT id, name, kind, status, capabilities_json, last_seen_at, created_at
		FROM agents WHERE id = ?
	`, id))
	if err != nil {
		return domain.Agent{}, normalizeStoreError(err)
	}
	return agent, nil
}

func (s *Store) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, kind, status, capabilities_json, last_seen_at, created_at
		FROM agents
		ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'waiting' THEN 1 ELSE 2 END, name
	`)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()
	agents := make([]domain.Agent, 0)
	for rows.Next() {
		agent, err := scanAgent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

func (s *Store) ClaimTask(ctx context.Context, taskID, agentID string, lease time.Duration) (domain.Run, error) {
	if lease <= 0 {
		lease = 30 * time.Second
	}
	if _, err := s.GetAgent(ctx, agentID); err != nil {
		return domain.Run{}, fmt.Errorf("claiming agent: %w", err)
	}
	now := nowMillis()
	leaseExpires := time.Now().UTC().Add(lease).UnixMilli()
	run := domain.Run{
		ID: newID("run"), TaskID: taskID, AgentID: agentID, Status: domain.RunPlanning,
		LeaseExpiresAt: &leaseExpires, StartedAt: now, UpdatedAt: now,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Run{}, fmt.Errorf("begin claim: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'planning', assigned_agent_id = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND status = 'ready'
	`, agentID, now, taskID)
	if err != nil {
		return domain.Run{}, fmt.Errorf("claim task: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return domain.Run{}, fmt.Errorf("read claim result: %w", err)
	}
	if rows == 0 {
		return domain.Run{}, fmt.Errorf("task is not available to claim: %w", domain.ErrConflict)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO runs (
			id, task_id, agent_id, status, plan, result_summary,
			lease_expires_at, started_at, updated_at
		) VALUES (?, ?, ?, 'planning', '', '', ?, ?, ?)
	`, run.ID, run.TaskID, run.AgentID, leaseExpires, now, now)
	if err != nil {
		return domain.Run{}, fmt.Errorf("create run: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE agents SET status = 'active', last_seen_at = ? WHERE id = ?",
		now, agentID,
	); err != nil {
		return domain.Run{}, fmt.Errorf("activate agent: %w", err)
	}
	event, err := insertEventTx(
		ctx, tx, &taskID, &run.ID, &agentID,
		"run.claimed", "Agent claimed the task and started planning",
		map[string]any{"lease_expires_at": leaseExpires},
	)
	if err != nil {
		return domain.Run{}, fmt.Errorf("create claim event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Run{}, fmt.Errorf("commit claim: %w", err)
	}
	s.publish(event)
	return run, nil
}

func (s *Store) HeartbeatRun(ctx context.Context, runID, agentID string, lease time.Duration) error {
	if lease <= 0 {
		lease = 30 * time.Second
	}
	now := nowMillis()
	leaseExpires := time.Now().UTC().Add(lease).UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin heartbeat: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE runs
		SET lease_expires_at = ?, updated_at = ?
		WHERE id = ? AND agent_id = ? AND status IN ('planning', 'waiting_decision', 'running', 'verifying')
	`, leaseExpires, now, runID, agentID)
	if err != nil {
		return fmt.Errorf("heartbeat run: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read heartbeat result: %w", err)
	}
	if rows == 0 {
		return domain.ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE agents
		SET status = CASE
			WHEN (SELECT status FROM runs WHERE id = ?) = 'waiting_decision' THEN 'waiting'
			ELSE 'active'
		END,
		last_seen_at = ?
		WHERE id = ?
	`,
		runID, now, agentID,
	); err != nil {
		return fmt.Errorf("heartbeat agent: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit heartbeat: %w", err)
	}
	s.publish(domain.Event{
		RunID: &runID, AgentID: &agentID, Type: "agent.heartbeat",
		Message: "Agent heartbeat", Payload: map[string]any{"lease_expires_at": leaseExpires}, CreatedAt: now,
	})
	return nil
}

func (s *Store) GetActiveRunForAgent(ctx context.Context, agentID string) (domain.ActiveRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			r.id, r.task_id, r.agent_id, r.status, r.plan, r.result_summary,
			r.lease_expires_at, r.started_at, r.finished_at, r.updated_at,
			t.id, t.project_id, t.title, t.description, t.acceptance_criteria, t.status,
			t.priority, t.budget_minutes, t.assigned_agent_id, t.version, t.created_at, t.updated_at
		FROM runs r
		JOIN tasks t ON t.id = r.task_id
		WHERE r.agent_id = ?
		  AND r.status IN ('planning', 'waiting_decision', 'running', 'verifying')
		ORDER BY r.updated_at DESC
		LIMIT 1
	`, agentID)
	var active domain.ActiveRun
	var leaseExpiresAt, finishedAt sql.NullInt64
	var assignedAgentID sql.NullString
	if err := row.Scan(
		&active.Run.ID,
		&active.Run.TaskID,
		&active.Run.AgentID,
		&active.Run.Status,
		&active.Run.Plan,
		&active.Run.ResultSummary,
		&leaseExpiresAt,
		&active.Run.StartedAt,
		&finishedAt,
		&active.Run.UpdatedAt,
		&active.Task.ID,
		&active.Task.ProjectID,
		&active.Task.Title,
		&active.Task.Description,
		&active.Task.AcceptanceCriteria,
		&active.Task.Status,
		&active.Task.Priority,
		&active.Task.BudgetMinutes,
		&assignedAgentID,
		&active.Task.Version,
		&active.Task.CreatedAt,
		&active.Task.UpdatedAt,
	); err != nil {
		return domain.ActiveRun{}, normalizeStoreError(err)
	}
	if leaseExpiresAt.Valid {
		value := leaseExpiresAt.Int64
		active.Run.LeaseExpiresAt = &value
	}
	if finishedAt.Valid {
		value := finishedAt.Int64
		active.Run.FinishedAt = &value
	}
	if assignedAgentID.Valid {
		active.Task.AssignedAgentID = stringPtr(assignedAgentID.String)
	}
	return active, nil
}

func (s *Store) SubmitPlan(ctx context.Context, runID, plan string, requireApproval bool) (*domain.Decision, error) {
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return nil, fmt.Errorf("plan is required: %w", domain.ErrInvalidInput)
	}
	run, err := s.getRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	task, err := s.GetTask(ctx, run.TaskID)
	if err != nil {
		return nil, err
	}
	now := nowMillis()
	nextRunStatus := domain.RunRunning
	nextTaskStatus := domain.TaskRunning
	if requireApproval {
		nextRunStatus = domain.RunWaitingDecision
		nextTaskStatus = domain.TaskWaitingDecision
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin submit plan: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE runs SET plan = ?, status = ?, updated_at = ?
		WHERE id = ? AND status = 'planning'
	`, plan, nextRunStatus, now, runID)
	if err != nil {
		return nil, fmt.Errorf("save plan: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil, fmt.Errorf("run is no longer planning: %w", domain.ErrConflict)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE tasks SET status = ?, version = version + 1, updated_at = ? WHERE id = ?",
		nextTaskStatus, now, run.TaskID,
	); err != nil {
		return nil, fmt.Errorf("update task for plan: %w", err)
	}
	if requireApproval {
		if _, err := tx.ExecContext(ctx,
			"UPDATE agents SET status = 'waiting', last_seen_at = ? WHERE id = ?",
			now, run.AgentID,
		); err != nil {
			return nil, fmt.Errorf("mark agent waiting: %w", err)
		}
	}
	planEvent, err := insertEventTx(
		ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
		"plan.submitted", "Agent submitted a plan for review",
		map[string]any{"requires_approval": requireApproval, "plan": plan},
	)
	if err != nil {
		return nil, fmt.Errorf("create plan event: %w", err)
	}

	var decision *domain.Decision
	var decisionEvent domain.Event
	if requireApproval {
		created := domain.Decision{
			ID: newID("dec"), TaskID: run.TaskID, RunID: &run.ID,
			Question:       fmt.Sprintf("Approve the proposed plan for “%s”?", task.Title),
			Options:        []string{"Approve plan", "Request changes"},
			Recommendation: "Approve plan",
			Status:         "pending",
			CreatedAt:      now,
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO decisions (
				id, task_id, run_id, question, options_json, recommendation,
				status, answer, created_at
			) VALUES (?, ?, ?, ?, ?, ?, 'pending', '', ?)
		`,
			created.ID,
			created.TaskID,
			created.RunID,
			created.Question,
			marshalJSON(created.Options),
			created.Recommendation,
			created.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("create decision: %w", err)
		}
		decisionEvent, err = insertEventTx(
			ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
			"decision.requested", "Agent is waiting for plan approval",
			map[string]any{"decision_id": created.ID},
		)
		if err != nil {
			return nil, fmt.Errorf("create decision event: %w", err)
		}
		decision = &created
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit plan: %w", err)
	}
	s.publish(planEvent)
	if decision != nil {
		s.publish(decisionEvent)
	}
	return decision, nil
}

func (s *Store) StartRun(ctx context.Context, runID string) error {
	run, err := s.getRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status != domain.RunWaitingDecision {
		return fmt.Errorf("run is not waiting for approval: %w", domain.ErrInvalidTransition)
	}
	decision, err := s.LatestDecisionForRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("load plan decision: %w", err)
	}
	if decision.Status != "answered" {
		return fmt.Errorf("plan decision is still pending: %w", domain.ErrConflict)
	}
	if !strings.EqualFold(decision.Answer, decision.Recommendation) {
		return fmt.Errorf("plan was not approved: %w", domain.ErrInvalidTransition)
	}
	now := nowMillis()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin start run: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE runs SET status = 'running', updated_at = ?
		WHERE id = ? AND status = 'waiting_decision'
	`, now, runID)
	if err != nil {
		return fmt.Errorf("start run: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("run cannot start from its current state: %w", domain.ErrInvalidTransition)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE tasks SET status = 'running', version = version + 1, updated_at = ? WHERE id = ?",
		now, run.TaskID,
	); err != nil {
		return fmt.Errorf("start task run: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE agents SET status = 'active', last_seen_at = ? WHERE id = ?",
		now, run.AgentID,
	); err != nil {
		return fmt.Errorf("activate running agent: %w", err)
	}
	event, err := insertEventTx(
		ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
		"run.started", "Plan approved; execution started", nil,
	)
	if err != nil {
		return fmt.Errorf("create run start event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit run start: %w", err)
	}
	s.publish(event)
	return nil
}

func (s *Store) ReportProgress(ctx context.Context, runID, message string, payload map[string]any) error {
	run, err := s.getRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status != domain.RunRunning {
		return fmt.Errorf("run is not executing: %w", domain.ErrInvalidTransition)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin progress event: %w", err)
	}
	defer tx.Rollback()
	event, err := insertEventTx(
		ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
		"run.progress", strings.TrimSpace(message), payload,
	)
	if err != nil {
		return fmt.Errorf("insert progress event: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE runs SET updated_at = ? WHERE id = ?", nowMillis(), runID); err != nil {
		return fmt.Errorf("touch run: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit progress event: %w", err)
	}
	s.publish(event)
	return nil
}

func (s *Store) BeginVerification(ctx context.Context, runID string) error {
	run, err := s.getRun(ctx, runID)
	if err != nil {
		return err
	}
	now := nowMillis()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin verification: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx,
		"UPDATE runs SET status = 'verifying', updated_at = ? WHERE id = ? AND status = 'running'",
		now, runID,
	)
	if err != nil {
		return fmt.Errorf("verify run: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("run cannot enter verification: %w", domain.ErrInvalidTransition)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE tasks SET status = 'verifying', version = version + 1, updated_at = ? WHERE id = ?",
		now, run.TaskID,
	); err != nil {
		return fmt.Errorf("verify task: %w", err)
	}
	event, err := insertEventTx(
		ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
		"run.verifying", "Execution finished; verifying acceptance criteria", nil,
	)
	if err != nil {
		return fmt.Errorf("create verification event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit verification: %w", err)
	}
	s.publish(event)
	return nil
}

func (s *Store) CompleteRun(ctx context.Context, runID, summary, evidence string) error {
	run, err := s.getRun(ctx, runID)
	if err != nil {
		return err
	}
	now := nowMillis()
	artifact := domain.Artifact{
		ID: newID("art"), TaskID: run.TaskID, RunID: run.ID,
		Name: "verification-report.md", Kind: "verification", Content: evidence, CreatedAt: now,
	}
	artifact.URI = "appdeck://artifacts/" + artifact.ID
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin complete run: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE runs
		SET status = 'completed', result_summary = ?, lease_expires_at = NULL,
			finished_at = ?, updated_at = ?
		WHERE id = ? AND status = 'verifying'
	`, strings.TrimSpace(summary), now, now, runID)
	if err != nil {
		return fmt.Errorf("complete run: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("run cannot complete: %w", domain.ErrInvalidTransition)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = 'done', assigned_agent_id = NULL,
			version = version + 1, updated_at = ? WHERE id = ?
	`, now, run.TaskID); err != nil {
		return fmt.Errorf("complete task: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE agents SET status = 'idle', last_seen_at = ? WHERE id = ?",
		now, run.AgentID,
	); err != nil {
		return fmt.Errorf("idle agent: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifacts (id, task_id, run_id, name, kind, uri, content, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		artifact.ID,
		artifact.TaskID,
		artifact.RunID,
		artifact.Name,
		artifact.Kind,
		artifact.URI,
		artifact.Content,
		artifact.CreatedAt,
	); err != nil {
		return fmt.Errorf("create verification artifact: %w", err)
	}
	event, err := insertEventTx(
		ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
		"run.completed", "Run completed with verification evidence",
		map[string]any{"artifact_id": artifact.ID, "summary": summary},
	)
	if err != nil {
		return fmt.Errorf("create completion event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit completion: %w", err)
	}
	s.publish(event)
	return nil
}

func (s *Store) BlockRun(ctx context.Context, runID, reason string) error {
	run, err := s.getRun(ctx, runID)
	if err != nil {
		return err
	}
	now := nowMillis()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin block run: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE runs SET status = 'blocked', result_summary = ?, lease_expires_at = NULL,
			finished_at = ?, updated_at = ? WHERE id = ?
	`, reason, now, now, runID); err != nil {
		return fmt.Errorf("block run: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = 'blocked', assigned_agent_id = NULL,
			version = version + 1, updated_at = ? WHERE id = ?
	`, now, run.TaskID); err != nil {
		return fmt.Errorf("block task: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE agents SET status = 'idle', last_seen_at = ? WHERE id = ?",
		now, run.AgentID,
	); err != nil {
		return fmt.Errorf("idle blocked agent: %w", err)
	}
	event, err := insertEventTx(
		ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
		"run.blocked", "Run blocked: "+reason, map[string]any{"reason": reason},
	)
	if err != nil {
		return fmt.Errorf("create blocked event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit blocked run: %w", err)
	}
	s.publish(event)
	return nil
}

func (s *Store) CountRunEvents(ctx context.Context, runID, eventType string) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM events WHERE run_id = ? AND type = ?", runID, eventType,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count run events: %w", err)
	}
	return count, nil
}

func (s *Store) getRun(ctx context.Context, id string) (domain.Run, error) {
	run, err := scanRun(s.db.QueryRowContext(ctx,
		"SELECT "+runColumns+" FROM runs WHERE id = ?", id,
	))
	if err != nil {
		return domain.Run{}, normalizeStoreError(err)
	}
	return run, nil
}

func (s *Store) listRunsForTask(ctx context.Context, taskID string) ([]domain.Run, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+runColumns+" FROM runs WHERE task_id = ? ORDER BY started_at DESC", taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("list task runs: %w", err)
	}
	defer rows.Close()
	runs := make([]domain.Run, 0)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Store) ExpireLeases(ctx context.Context) (int, error) {
	now := nowMillis()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM runs
		WHERE status IN ('planning', 'waiting_decision', 'running', 'verifying')
		  AND lease_expires_at IS NOT NULL
		  AND lease_expires_at < ?
	`, now)
	if err != nil {
		return 0, fmt.Errorf("find expired leases: %w", err)
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan expired lease: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.failExpiredRun(ctx, id); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

func (s *Store) failExpiredRun(ctx context.Context, runID string) error {
	run, err := s.getRun(ctx, runID)
	if err != nil {
		return err
	}
	now := nowMillis()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin lease expiry: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE runs SET status = 'failed', result_summary = 'Lease expired',
			lease_expires_at = NULL, finished_at = ?, updated_at = ?
		WHERE id = ? AND status IN ('planning', 'waiting_decision', 'running', 'verifying')
	`, now, now, runID)
	if err != nil {
		return fmt.Errorf("expire run lease: %w", err)
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = 'ready', assigned_agent_id = NULL,
			version = version + 1, updated_at = ? WHERE id = ?
	`, now, run.TaskID); err != nil {
		return fmt.Errorf("requeue expired task: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE agents SET status = 'idle' WHERE id = ?", run.AgentID,
	); err != nil {
		return fmt.Errorf("idle expired agent: %w", err)
	}
	event, err := insertEventTx(
		ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
		"run.lease_expired", "Agent lease expired; task returned to ready", nil,
	)
	if err != nil {
		return fmt.Errorf("create lease expiry event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit lease expiry: %w", err)
	}
	s.publish(event)
	return nil
}
