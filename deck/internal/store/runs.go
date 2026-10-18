package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

const runColumns = `
	id, task_id, agent_id, status, plan, result_summary,
	lease_expires_at, started_at, finished_at, updated_at,
	version, fence_token, task_version, acceptance_criteria_digest,
	active_plan_revision_id
`

func scanRun(scanner interface{ Scan(...any) error }) (domain.Run, error) {
	var run domain.Run
	var leaseExpiresAt, finishedAt sql.NullInt64
	var activePlanRevisionID sql.NullString
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
		&run.Version,
		&run.FenceToken,
		&run.TaskVersion,
		&run.AcceptanceCriteriaDigest,
		&activePlanRevisionID,
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
	if activePlanRevisionID.Valid {
		value := activePlanRevisionID.String
		run.ActivePlanRevisionID = &value
	}
	return run, nil
}

func scanAgent(scanner interface{ Scan(...any) error }) (domain.Agent, error) {
	var agent domain.Agent
	var capabilities string
	if err := scanner.Scan(
		&agent.ID,
		&agent.InstallationID,
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
	var answeredAt, expiresAt sql.NullInt64
	var planRevisionID sql.NullString
	if err := scanner.Scan(
		&decision.ID,
		&decision.TaskID,
		&runID,
		&planRevisionID,
		&decision.Question,
		&options,
		&decision.Recommendation,
		&decision.RiskClass,
		&decision.EvidenceSummary,
		&decision.EstimatedCostMinor,
		&decision.Currency,
		&decision.WaitingConsequence,
		&decision.RejectingConsequence,
		&decision.Status,
		&decision.Answer,
		&expiresAt,
		&decision.CreatedAt,
		&answeredAt,
		&decision.AnsweredBy,
		&decision.InteractionID,
		&decision.AnswerMode,
	); err != nil {
		return domain.Decision{}, err
	}
	if runID.Valid {
		decision.RunID = stringPtr(runID.String)
	}
	if planRevisionID.Valid {
		decision.PlanRevisionID = planRevisionID.String
	}
	if expiresAt.Valid {
		value := expiresAt.Int64
		decision.ExpiresAt = &value
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
	agent.InstallationID = strings.TrimSpace(agent.InstallationID)
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
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO agents (id, installation_id, name, kind, status, capabilities_json, last_seen_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			installation_id = CASE WHEN agents.installation_id = '' THEN excluded.installation_id ELSE agents.installation_id END,
			name = excluded.name,
			kind = excluded.kind,
			capabilities_json = excluded.capabilities_json,
			last_seen_at = excluded.last_seen_at
		WHERE agents.installation_id = '' OR excluded.installation_id = agents.installation_id
	`, agent.ID, agent.InstallationID, agent.Name, agent.Kind, agent.Status, marshalJSON(agent.Capabilities), now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: agents.installation_id") {
			return domain.Agent{}, fmt.Errorf("installation already owns another logical agent: %w", domain.ErrConflict)
		}
		return domain.Agent{}, fmt.Errorf("register agent: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return domain.Agent{}, fmt.Errorf("logical agent belongs to another installation: %w", domain.ErrConflict)
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
		SELECT id, installation_id, name, kind, status, capabilities_json, last_seen_at, created_at
		FROM agents WHERE id = ?
	`, id))
	if err != nil {
		return domain.Agent{}, normalizeStoreError(err)
	}
	return agent, nil
}

func (s *Store) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, installation_id, name, kind, status, capabilities_json, last_seen_at, created_at
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

// claimTaskTx moves a Ready task whose dependencies are satisfied into a new
// planning run. A nil lease records a run whose liveness is the executor's.
func claimTaskTx(ctx context.Context, tx *sql.Tx, taskID string, expectedTaskVersion int, agentID, runID string, leaseExpires *int64, message string) (domain.Run, domain.Event, error) {
	if _, err := getAgentTx(ctx, tx, agentID); err != nil {
		return domain.Run{}, domain.Event{}, fmt.Errorf("claiming agent: %w", err)
	}
	now := nowMillis()
	currentTask, err := getTaskTx(ctx, tx, taskID)
	if err != nil {
		return domain.Run{}, domain.Event{}, err
	}
	if expectedTaskVersion == 0 {
		expectedTaskVersion = currentTask.Version
	}
	if expectedTaskVersion < 1 || currentTask.Version != expectedTaskVersion {
		return domain.Run{}, domain.Event{}, fmt.Errorf("task version is stale: %w", domain.ErrConflict)
	}
	var fenceToken int64
	if err := tx.QueryRowContext(ctx, "SELECT next_fence_token FROM tasks WHERE id = ?", taskID).Scan(&fenceToken); err != nil {
		return domain.Run{}, domain.Event{}, fmt.Errorf("load task fence: %w", err)
	}
	digestValue := sha256.Sum256([]byte(currentTask.AcceptanceCriteria))
	acceptanceDigest := hex.EncodeToString(digestValue[:])
	run := domain.Run{
		ID: runID, TaskID: taskID, AgentID: agentID, Status: domain.RunPlanning,
		Version: 1, FenceToken: fenceToken, TaskVersion: currentTask.Version,
		AcceptanceCriteriaDigest: acceptanceDigest,
		LeaseExpiresAt:           leaseExpires, StartedAt: now, UpdatedAt: now,
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'planning', assigned_agent_id = ?, version = version + 1,
			next_fence_token = next_fence_token + 1, updated_at = ?
		WHERE id = ? AND status = 'ready' AND version = ? AND archived = 0 AND `+dependenciesEligibleSQL+`
	`, agentID, now, taskID, expectedTaskVersion)
	if err != nil {
		return domain.Run{}, domain.Event{}, fmt.Errorf("claim task: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return domain.Run{}, domain.Event{}, fmt.Errorf("read claim result: %w", err)
	}
	if rows == 0 {
		return domain.Run{}, domain.Event{}, fmt.Errorf("task is not available to claim: %w", domain.ErrConflict)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO runs (
			id, task_id, agent_id, status, plan, result_summary,
			lease_expires_at, started_at, updated_at, version, fence_token,
			task_version, acceptance_criteria_digest
		) VALUES (?, ?, ?, 'planning', '', '', ?, ?, ?, 1, ?, ?, ?)
	`, run.ID, run.TaskID, run.AgentID, nullableInt64(leaseExpires), now, now, fenceToken, currentTask.Version, acceptanceDigest)
	if err != nil {
		return domain.Run{}, domain.Event{}, fmt.Errorf("create run: %w", err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO run_context(run_id, context_json) VALUES (?, ?)", run.ID, marshalJSON(currentTask.Context)); err != nil {
		return domain.Run{}, domain.Event{}, err
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE agents SET status = 'active', last_seen_at = ? WHERE id = ?",
		now, agentID,
	); err != nil {
		return domain.Run{}, domain.Event{}, fmt.Errorf("activate agent: %w", err)
	}
	payload := map[string]any{"run_version": 1, "fence_token": fenceToken, "task_version": currentTask.Version, "acceptance_criteria_digest": acceptanceDigest}
	if leaseExpires != nil {
		payload["lease_expires_at"] = *leaseExpires
	}
	event, err := insertEventTx(ctx, tx, &taskID, &run.ID, &agentID, "run.claimed", message, payload)
	if err != nil {
		return domain.Run{}, domain.Event{}, fmt.Errorf("create claim event: %w", err)
	}
	return run, event, nil
}

func (s *Store) StartRun(ctx context.Context, mutation Mutation, runID string) error {
	projectID, err := s.ProjectIDForRun(ctx, runID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin start run: %w", err)
	}
	defer tx.Rollback()
	attempt, replayResult, replayed, err := beginIdempotentMutation(
		ctx,
		tx,
		mutation,
		projectID,
		operationStartRun,
		runID,
		struct {
			RunID string    `json:"run_id"`
			Guard *RunGuard `json:"guard,omitempty"`
		}{RunID: runID, Guard: mutation.Guard},
	)
	if err != nil {
		return err
	}
	if replayed {
		var original struct{}
		return decodeIdempotencyResult(replayResult, &original)
	}
	run, err := getRunTx(ctx, tx, runID)
	if err != nil {
		return err
	}
	if err := validateRunGuard(run, mutation.Guard); err != nil {
		return err
	}
	if run.Status != domain.RunWaitingDecision {
		return fmt.Errorf("run is not waiting for approval: %w", domain.ErrInvalidTransition)
	}
	decision, err := latestDecisionForRunTx(ctx, tx, runID)
	if err != nil {
		return fmt.Errorf("load plan decision: %w", err)
	}
	if decision.Status != "answered" {
		return fmt.Errorf("plan decision is still pending: %w", domain.ErrConflict)
	}
	if decision.ExpiresAt != nil && *decision.ExpiresAt <= nowMillis() {
		return fmt.Errorf("plan decision has expired: %w", domain.ErrConflict)
	}
	if run.ActivePlanRevisionID == nil || decision.PlanRevisionID == "" || *run.ActivePlanRevisionID != decision.PlanRevisionID {
		return fmt.Errorf("plan decision does not authorize the active revision: %w", domain.ErrConflict)
	}
	if !strings.EqualFold(decision.Answer, decision.Recommendation) {
		return fmt.Errorf("plan was not approved: %w", domain.ErrInvalidTransition)
	}
	now := nowMillis()
	result, err := tx.ExecContext(ctx, `
		UPDATE runs SET status = 'running', version = version + 1, updated_at = ?
		WHERE id = ? AND status = 'waiting_decision' AND version = ?
	`, now, runID, run.Version)
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
	if err := finishIdempotentMutation(ctx, tx, attempt, struct{}{}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit run start: %w", err)
	}
	s.publish(event)
	return nil
}

func (s *Store) BeginVerification(ctx context.Context, mutation Mutation, runID string) error {
	projectID, err := s.ProjectIDForRun(ctx, runID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin verification: %w", err)
	}
	defer tx.Rollback()
	attempt, replayResult, replayed, err := beginIdempotentMutation(
		ctx,
		tx,
		mutation,
		projectID,
		operationBeginVerification,
		runID,
		struct {
			RunID string    `json:"run_id"`
			Guard *RunGuard `json:"guard,omitempty"`
		}{RunID: runID, Guard: mutation.Guard},
	)
	if err != nil {
		return err
	}
	if replayed {
		var original struct{}
		return decodeIdempotencyResult(replayResult, &original)
	}
	run, err := getRunTx(ctx, tx, runID)
	if err != nil {
		return err
	}
	if err := validateRunGuard(run, mutation.Guard); err != nil {
		return err
	}
	now := nowMillis()
	result, err := tx.ExecContext(ctx,
		"UPDATE runs SET status = 'verifying', version = version + 1, updated_at = ? WHERE id = ? AND status = 'running' AND version = ?",
		now, runID, run.Version,
	)
	if err != nil {
		return fmt.Errorf("verify run: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("run cannot enter verification: %w", domain.ErrInvalidTransition)
	}
	if err := closeEngineQuestionsTx(ctx, tx, runID, "superseded"); err != nil {
		return err
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
	if err := finishIdempotentMutation(ctx, tx, attempt, struct{}{}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit verification: %w", err)
	}
	s.publish(event)
	return nil
}

func (s *Store) BlockRun(ctx context.Context, mutation Mutation, runID, reason string) error {
	return s.endRun(ctx, mutation, runID, reason, domain.RunBlocked)
}

// FailRun ends a run whose executor reported failure without a result.
func (s *Store) FailRun(ctx context.Context, mutation Mutation, runID, reason string) error {
	return s.endRun(ctx, mutation, runID, reason, domain.RunFailed)
}

func (s *Store) endRun(ctx context.Context, mutation Mutation, runID, reason, status string) error {
	operation, label := operationBlockRun, "blocked"
	if status == domain.RunFailed {
		operation, label = operationFailRun, "failed"
	}
	projectID, err := s.ProjectIDForRun(ctx, runID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin block run: %w", err)
	}
	defer tx.Rollback()
	attempt, replayResult, replayed, err := beginIdempotentMutation(
		ctx,
		tx,
		mutation,
		projectID,
		operation,
		runID,
		struct {
			RunID  string    `json:"run_id"`
			Reason string    `json:"reason"`
			Guard  *RunGuard `json:"guard,omitempty"`
		}{RunID: runID, Reason: reason, Guard: mutation.Guard},
	)
	if err != nil {
		return err
	}
	if replayed {
		var original struct{}
		return decodeIdempotencyResult(replayResult, &original)
	}
	run, err := getRunTx(ctx, tx, runID)
	if err != nil {
		return err
	}
	if err := validateRunGuard(run, mutation.Guard); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("block reason is required: %w", domain.ErrInvalidInput)
	}
	now := nowMillis()
	result, err := tx.ExecContext(ctx, `
		UPDATE runs SET status = ?, result_summary = ?, lease_expires_at = NULL,
			finished_at = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ? AND status IN ('planning', 'waiting_decision', 'running', 'verifying')
	`, status, reason, now, now, runID, run.Version)
	if err != nil {
		return fmt.Errorf("block run: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read block run result: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("run is no longer active: %w", domain.ErrConflict)
	}
	if err := closeEngineQuestionsTx(ctx, tx, runID, "cancelled"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = ?, assigned_agent_id = NULL,
			version = version + 1, updated_at = ? WHERE id = ?
	`, status, now, run.TaskID); err != nil {
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
		"run."+label, "Run "+label+": "+reason, map[string]any{"reason": reason},
	)
	if err != nil {
		return fmt.Errorf("create blocked event: %w", err)
	}
	if err := finishIdempotentMutation(ctx, tx, attempt, struct{}{}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit blocked run: %w", err)
	}
	s.publish(event)
	return nil
}

func (s *Store) GetRun(ctx context.Context, id string) (domain.Run, error) {
	run, err := scanRun(s.db.QueryRowContext(ctx,
		"SELECT "+runColumns+" FROM runs WHERE id = ?", id,
	))
	if err != nil {
		return domain.Run{}, normalizeStoreError(err)
	}
	return run, nil
}

func getRunTx(ctx context.Context, tx *sql.Tx, id string) (domain.Run, error) {
	run, err := scanRun(tx.QueryRowContext(ctx,
		"SELECT "+runColumns+" FROM runs WHERE id = ?", id,
	))
	if err != nil {
		return domain.Run{}, normalizeStoreError(err)
	}
	return run, nil
}

func getTaskTx(ctx context.Context, tx *sql.Tx, id string) (domain.Task, error) {
	task, err := scanTask(tx.QueryRowContext(ctx,
		"SELECT "+taskColumns+" FROM tasks WHERE id = ?", id,
	))
	if err != nil {
		return domain.Task{}, normalizeStoreError(err)
	}
	return task, nil
}

func getAgentTx(ctx context.Context, tx *sql.Tx, id string) (domain.Agent, error) {
	agent, err := scanAgent(tx.QueryRowContext(ctx, `
		SELECT id, installation_id, name, kind, status, capabilities_json, last_seen_at, created_at
		FROM agents WHERE id = ?
	`, id))
	if err != nil {
		return domain.Agent{}, normalizeStoreError(err)
	}
	return agent, nil
}

func latestDecisionForRunTx(ctx context.Context, tx *sql.Tx, runID string) (domain.Decision, error) {
	decision, err := scanDecision(tx.QueryRowContext(ctx, `
		SELECT `+decisionColumns+` FROM decisions
		WHERE run_id = ? ORDER BY created_at DESC LIMIT 1
	`, runID))
	if err != nil {
		return domain.Decision{}, normalizeStoreError(err)
	}
	return decision, nil
}

func (s *Store) ProjectIDForRun(ctx context.Context, id string) (string, error) {
	var projectID string
	if err := s.db.QueryRowContext(ctx, `
		SELECT t.project_id
		FROM runs r
		JOIN tasks t ON t.id = r.task_id
		WHERE r.id = ?
	`, id).Scan(&projectID); err != nil {
		return "", normalizeStoreError(err)
	}
	return projectID, nil
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
