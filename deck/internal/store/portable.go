package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

const portableSchemaVersion = "appdeck-export/v1"

type portableTask struct {
	Task           domain.Task `json:"task"`
	NextFenceToken int64       `json:"next_fence_token"`
}

type portableArtifact struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	RunID     string `json:"run_id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	URI       string `json:"uri"`
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt int64  `json:"created_at"`
}

type portableBundle struct {
	SchemaVersion     string                    `json:"schema_version"`
	Project           domain.Project            `json:"project"`
	Agents            []domain.Agent            `json:"agents"`
	Tasks             []portableTask            `json:"tasks"`
	Runs              []domain.Run              `json:"runs"`
	PlanRevisions     []domain.PlanRevision     `json:"plan_revisions"`
	Decisions         []domain.Decision         `json:"decisions"`
	Commands          []domain.Command          `json:"commands"`
	Events            []domain.Event            `json:"events"`
	Artifacts         []portableArtifact        `json:"artifact_metadata"`
	Evidence          []domain.EvidenceRecord   `json:"evidence"`
	Channels          []domain.Channel          `json:"channels"`
	ChannelMessages   []domain.ChannelMessage   `json:"channel_messages"`
	OperationalStates []domain.OperationalState `json:"operational_states"`
}

func (s *Store) ExportProject(ctx context.Context, projectID string) ([]byte, error) {
	var bundle portableBundle
	bundle.SchemaVersion = portableSchemaVersion
	if err := s.db.QueryRowContext(ctx, `
		SELECT id, name, slug, description, created_at FROM projects WHERE id = ?
	`, projectID).Scan(&bundle.Project.ID, &bundle.Project.Name, &bundle.Project.Slug,
		&bundle.Project.Description, &bundle.Project.CreatedAt); err != nil {
		return nil, normalizeStoreError(err)
	}
	var err error
	if bundle.Agents, err = s.exportAgents(ctx, projectID); err != nil {
		return nil, err
	}
	if bundle.Tasks, err = s.exportTasks(ctx, projectID); err != nil {
		return nil, err
	}
	if bundle.Runs, err = s.exportRuns(ctx, projectID); err != nil {
		return nil, err
	}
	if bundle.PlanRevisions, err = s.exportPlans(ctx, projectID); err != nil {
		return nil, err
	}
	if bundle.Decisions, err = s.exportDecisions(ctx, projectID); err != nil {
		return nil, err
	}
	if bundle.Commands, err = s.exportCommands(ctx, projectID); err != nil {
		return nil, err
	}
	if bundle.Events, err = s.exportEvents(ctx, projectID); err != nil {
		return nil, err
	}
	if bundle.Artifacts, err = s.exportArtifacts(ctx, projectID); err != nil {
		return nil, err
	}
	if bundle.Evidence, err = s.exportEvidence(ctx, projectID); err != nil {
		return nil, err
	}
	if bundle.Channels, err = s.ListChannels(ctx, projectID); err != nil {
		return nil, err
	}
	if bundle.ChannelMessages, err = s.exportChannelMessages(ctx, projectID); err != nil {
		return nil, err
	}
	if bundle.OperationalStates, err = s.exportOperationalStates(ctx, projectID); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode project export: %w", err)
	}
	return append(encoded, '\n'), nil
}

func collectExportRows[T any](rows *sql.Rows, scan func(interface{ Scan(...any) error }) (T, error)) ([]T, error) {
	defer rows.Close()
	items := make([]T, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) exportAgents(ctx context.Context, projectID string) ([]domain.Agent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT a.id, a.installation_id, a.name, a.kind, a.status,
			a.capabilities_json, a.last_seen_at, a.created_at
		FROM agents a
		JOIN runs r ON r.agent_id = a.id
		JOIN tasks t ON t.id = r.task_id
		WHERE t.project_id = ? ORDER BY a.id
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("export agents: %w", err)
	}
	return collectExportRows(rows, scanAgent)
}

func (s *Store) exportTasks(ctx context.Context, projectID string) ([]portableTask, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+taskColumns+", next_fence_token FROM tasks WHERE project_id = ? ORDER BY id", projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("export tasks: %w", err)
	}
	defer rows.Close()
	items := make([]portableTask, 0)
	for rows.Next() {
		var item portableTask
		var deadlineAt sql.NullInt64
		var assignedAgentID sql.NullString
		var authority string
		if err := rows.Scan(
			&item.Task.ID, &item.Task.ProjectID, &item.Task.Title, &item.Task.Description,
			&item.Task.AcceptanceCriteria, &item.Task.Status, &item.Task.Priority,
			&item.Task.BudgetMinutes, &deadlineAt, &item.Task.Budget.MaxAttempts,
			&item.Task.Budget.MaxTokens, &item.Task.Budget.MaxCostMinor, &item.Task.Budget.Currency,
			&item.Task.Budget.MaxConcurrency, &authority, &assignedAgentID, &item.Task.Version,
			&item.Task.CreatedAt, &item.Task.UpdatedAt, &item.NextFenceToken,
		); err != nil {
			return nil, fmt.Errorf("scan export task: %w", err)
		}
		item.Task.Budget.TimeMinutes = item.Task.BudgetMinutes
		item.Task.Budget.Authority = unmarshalStrings(authority)
		if deadlineAt.Valid {
			value := deadlineAt.Int64
			item.Task.Budget.DeadlineAt = &value
		}
		if assignedAgentID.Valid {
			value := assignedAgentID.String
			item.Task.AssignedAgentID = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) exportRuns(ctx context.Context, projectID string) ([]domain.Run, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.task_id, r.agent_id, r.status, r.plan, r.result_summary,
			r.lease_expires_at, r.started_at, r.finished_at, r.updated_at,
			r.version, r.fence_token, r.task_version, r.acceptance_criteria_digest,
			r.active_plan_revision_id
		FROM runs r JOIN tasks t ON t.id = r.task_id
		WHERE t.project_id = ? ORDER BY r.id
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("export runs: %w", err)
	}
	return collectExportRows(rows, scanRun)
}

func (s *Store) exportPlans(ctx context.Context, projectID string) ([]domain.PlanRevision, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.run_id, p.revision, p.plan, p.context_digest, p.risk_class,
			p.verification_strategy, p.expected_side_effects_json, p.rollback_approach,
			p.source_revision, p.policy_version, p.created_at
		FROM plan_revisions p JOIN runs r ON r.id = p.run_id JOIN tasks t ON t.id = r.task_id
		WHERE t.project_id = ? ORDER BY p.run_id, p.revision
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("export plans: %w", err)
	}
	return collectExportRows(rows, scanPlanRevision)
}

func (s *Store) exportDecisions(ctx context.Context, projectID string) ([]domain.Decision, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, d.task_id, d.run_id, d.plan_revision_id, d.question, d.options_json,
			d.recommendation, d.risk_class, d.evidence_summary, d.estimated_cost_minor,
			d.currency, d.waiting_consequence, d.rejecting_consequence, d.status,
			d.answer, d.expires_at, d.created_at, d.answered_at, d.answered_by
		FROM decisions d JOIN tasks t ON t.id = d.task_id
		WHERE t.project_id = ? ORDER BY d.id
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("export decisions: %w", err)
	}
	return collectExportRows(rows, scanDecision)
}

func (s *Store) exportCommands(ctx context.Context, projectID string) ([]domain.Command, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.run_id, c.sequence, c.type, c.status, c.reason, c.requested_by,
			c.result, c.run_version, c.fence_token, c.created_at, c.updated_at
		FROM commands c JOIN runs r ON r.id = c.run_id JOIN tasks t ON t.id = r.task_id
		WHERE t.project_id = ? ORDER BY c.run_id, c.sequence
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("export commands: %w", err)
	}
	return collectExportRows(rows, scanCommand)
}

func (s *Store) exportEvents(ctx context.Context, projectID string) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.task_id, e.run_id, e.agent_id, e.type, e.message, e.payload_json, e.created_at
		FROM events e LEFT JOIN tasks t ON t.id = e.task_id
		WHERE t.project_id = ? ORDER BY e.id
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("export events: %w", err)
	}
	return collectExportRows(rows, eventFromRow)
}

func (s *Store) exportArtifacts(ctx context.Context, projectID string) ([]portableArtifact, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.task_id, a.run_id, a.name, a.kind, a.uri, a.digest, a.size_bytes, a.created_at
		FROM artifacts a JOIN tasks t ON t.id = a.task_id
		WHERE t.project_id = ? ORDER BY a.id
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("export artifact metadata: %w", err)
	}
	defer rows.Close()
	items := make([]portableArtifact, 0)
	for rows.Next() {
		var item portableArtifact
		if err := rows.Scan(&item.ID, &item.TaskID, &item.RunID, &item.Name, &item.Kind,
			&item.URI, &item.Digest, &item.SizeBytes, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) exportEvidence(ctx context.Context, projectID string) ([]domain.EvidenceRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.task_id, e.run_id, e.plan_revision_id, e.type, e.producer,
			e.verifier, e.subject_digest, e.source_revision, e.command, e.outcome,
			e.provenance_uri, e.provenance_digest, e.observed_at, e.expires_at,
			e.artifact_uri, e.artifact_digest, e.policy_version, e.admission_status, e.created_at
		FROM evidence_records e JOIN tasks t ON t.id = e.task_id
		WHERE t.project_id = ? ORDER BY e.id
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("export evidence: %w", err)
	}
	return collectExportRows(rows, scanEvidence)
}

func (s *Store) exportChannelMessages(ctx context.Context, projectID string) ([]domain.ChannelMessage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, channel_id, project_id, author_type, author_id, body,
			references_json, promoted_task_id, created_at
		FROM channel_messages WHERE project_id = ? ORDER BY channel_id, rowid
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("export channel messages: %w", err)
	}
	return collectExportRows(rows, scanChannelMessage)
}

func (s *Store) exportOperationalStates(ctx context.Context, projectID string) ([]domain.OperationalState, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+operationalStateColumns+" FROM operational_states WHERE project_id = ? ORDER BY id", projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("export operational states: %w", err)
	}
	return collectExportRows(rows, scanOperationalState)
}

func (s *Store) ImportProject(ctx context.Context, encoded []byte) error {
	var bundle portableBundle
	if err := json.Unmarshal(encoded, &bundle); err != nil {
		return fmt.Errorf("decode project import: %w", domain.ErrInvalidInput)
	}
	if bundle.SchemaVersion != portableSchemaVersion || bundle.Project.ID == "" {
		return fmt.Errorf("unsupported or incomplete project export: %w", domain.ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin project import: %w", err)
	}
	defer tx.Rollback()
	var existing int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE project_id = ?", bundle.Project.ID).Scan(&existing); err != nil {
		return fmt.Errorf("inspect import target: %w", err)
	}
	if existing != 0 {
		return fmt.Errorf("project import requires an empty target project: %w", domain.ErrConflict)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, description, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, slug = excluded.slug,
			description = excluded.description, created_at = excluded.created_at
	`, bundle.Project.ID, bundle.Project.Name, bundle.Project.Slug,
		bundle.Project.Description, bundle.Project.CreatedAt); err != nil {
		return fmt.Errorf("import project: %w", err)
	}
	for _, agent := range bundle.Agents {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO agents (id, installation_id, name, kind, status, capabilities_json, last_seen_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, agent.ID, agent.InstallationID, agent.Name, agent.Kind, agent.Status,
			marshalJSON(agent.Capabilities), agent.LastSeenAt, agent.CreatedAt); err != nil {
			return fmt.Errorf("import agent %s: %w", agent.ID, err)
		}
	}
	for _, portable := range bundle.Tasks {
		task := portable.Task
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tasks (
				id, project_id, title, description, acceptance_criteria, status, priority,
				budget_minutes, deadline_at, max_attempts, max_tokens, max_cost_minor,
				currency, max_concurrency, authority_json, assigned_agent_id, version,
				next_fence_token, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, task.ID, task.ProjectID, task.Title, task.Description, task.AcceptanceCriteria,
			task.Status, task.Priority, task.BudgetMinutes, nullableInt64(task.Budget.DeadlineAt),
			task.Budget.MaxAttempts, task.Budget.MaxTokens, task.Budget.MaxCostMinor,
			task.Budget.Currency, task.Budget.MaxConcurrency, marshalJSON(task.Budget.Authority),
			nullableString(task.AssignedAgentID), task.Version, portable.NextFenceToken,
			task.CreatedAt, task.UpdatedAt); err != nil {
			return fmt.Errorf("import task %s: %w", task.ID, err)
		}
	}
	for _, run := range bundle.Runs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO runs (
				id, task_id, agent_id, status, plan, result_summary, lease_expires_at,
				started_at, finished_at, updated_at, version, fence_token, task_version,
				acceptance_criteria_digest, active_plan_revision_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, run.ID, run.TaskID, run.AgentID, run.Status, run.Plan, run.ResultSummary,
			nullableInt64(run.LeaseExpiresAt), run.StartedAt, nullableInt64(run.FinishedAt),
			run.UpdatedAt, run.Version, run.FenceToken, run.TaskVersion,
			run.AcceptanceCriteriaDigest, nullableString(run.ActivePlanRevisionID)); err != nil {
			return fmt.Errorf("import run %s: %w", run.ID, err)
		}
	}
	for _, plan := range bundle.PlanRevisions {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO plan_revisions (
				id, run_id, revision, plan, context_digest, risk_class, verification_strategy,
				expected_side_effects_json, rollback_approach, source_revision, policy_version, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, plan.ID, plan.RunID, plan.Revision, plan.Plan, plan.ContextDigest, plan.RiskClass,
			plan.VerificationStrategy, marshalJSON(plan.ExpectedSideEffects), plan.RollbackApproach,
			plan.SourceRevision, plan.PolicyVersion, plan.CreatedAt); err != nil {
			return fmt.Errorf("import plan %s: %w", plan.ID, err)
		}
	}
	for _, decision := range bundle.Decisions {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO decisions (
				id, task_id, run_id, plan_revision_id, question, options_json, recommendation,
				risk_class, evidence_summary, estimated_cost_minor, currency, waiting_consequence,
				rejecting_consequence, status, answer, expires_at, created_at, answered_at, answered_by
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, decision.ID, decision.TaskID, nullableString(decision.RunID), nullableText(decision.PlanRevisionID),
			decision.Question, marshalJSON(decision.Options), decision.Recommendation,
			decision.RiskClass, decision.EvidenceSummary, decision.EstimatedCostMinor, decision.Currency,
			decision.WaitingConsequence, decision.RejectingConsequence, decision.Status, decision.Answer,
			nullableInt64(decision.ExpiresAt), decision.CreatedAt, nullableInt64(decision.AnsweredAt),
			decision.AnsweredBy); err != nil {
			return fmt.Errorf("import decision %s: %w", decision.ID, err)
		}
	}
	for _, command := range bundle.Commands {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO commands (
				id, run_id, sequence, type, status, reason, requested_by, result,
				run_version, fence_token, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, command.ID, command.RunID, command.Sequence, command.Type, command.Status,
			command.Reason, command.RequestedBy, command.Result, command.RunVersion,
			command.FenceToken, command.CreatedAt, command.UpdatedAt); err != nil {
			return fmt.Errorf("import command %s: %w", command.ID, err)
		}
	}
	for _, artifact := range bundle.Artifacts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO artifacts (id, task_id, run_id, name, kind, uri, content, digest, size_bytes, created_at)
			VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, ?)
		`, artifact.ID, artifact.TaskID, artifact.RunID, artifact.Name, artifact.Kind,
			artifact.URI, artifact.Digest, artifact.SizeBytes, artifact.CreatedAt); err != nil {
			return fmt.Errorf("import artifact %s: %w", artifact.ID, err)
		}
	}
	for _, evidence := range bundle.Evidence {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO evidence_records (
				id, task_id, run_id, plan_revision_id, type, producer, verifier, subject_digest,
				source_revision, command, outcome, provenance_uri, provenance_digest, observed_at,
				expires_at, artifact_uri, artifact_digest, policy_version, admission_status, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, evidence.ID, evidence.TaskID, evidence.RunID, evidence.PlanRevisionID, evidence.Type,
			evidence.Producer, evidence.Verifier, evidence.SubjectDigest, evidence.SourceRevision,
			evidence.Command, evidence.Outcome, evidence.ProvenanceURI, evidence.ProvenanceDigest,
			evidence.ObservedAt, nullableInt64(evidence.ExpiresAt), evidence.ArtifactURI,
			evidence.ArtifactDigest, evidence.PolicyVersion, evidence.AdmissionStatus, evidence.CreatedAt); err != nil {
			return fmt.Errorf("import evidence %s: %w", evidence.ID, err)
		}
	}
	for _, channel := range bundle.Channels {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO channels (id, project_id, name, created_at) VALUES (?, ?, ?, ?)",
			channel.ID, channel.ProjectID, channel.Name, channel.CreatedAt,
		); err != nil {
			return fmt.Errorf("import channel %s: %w", channel.ID, err)
		}
	}
	for _, message := range bundle.ChannelMessages {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO channel_messages (
				id, channel_id, project_id, author_type, author_id, body,
				references_json, promoted_task_id, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, message.ID, message.ChannelID, message.ProjectID, message.AuthorType, message.AuthorID,
			message.Body, marshalJSON(message.References), nullableString(message.PromotedTaskID),
			message.CreatedAt); err != nil {
			return fmt.Errorf("import channel message %s: %w", message.ID, err)
		}
	}
	for _, state := range bundle.OperationalStates {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO operational_states (
				id, project_id, task_id, run_id, environment, rollout_state,
				observation_started_at, observation_ends_at, rollback_state,
				policy_version, source_revision, outcome, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, state.ID, state.ProjectID, state.TaskID, state.RunID, state.Environment,
			state.RolloutState, nullableInt64(state.ObservationStartedAt), nullableInt64(state.ObservationEndsAt),
			state.RollbackState, state.PolicyVersion, state.SourceRevision, state.Outcome,
			state.UpdatedAt); err != nil {
			return fmt.Errorf("import operational state %s: %w", state.ID, err)
		}
	}
	for _, event := range bundle.Events {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO events (id, task_id, run_id, agent_id, type, message, payload_json, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, event.ID, nullableString(event.TaskID), nullableString(event.RunID), nullableString(event.AgentID),
			event.Type, event.Message, marshalJSON(event.Payload), event.CreatedAt); err != nil {
			return fmt.Errorf("import event %d: %w", event.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit project import: %w", err)
	}
	return nil
}

func nullableText(value string) any {
	if value == "" {
		return sql.NullString{}
	}
	return value
}
