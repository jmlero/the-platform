package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

// MarinaAgentID is the logical agent Deck records for work executed by Marina.
const MarinaAgentID = "marina"

// MarinaRun links a Deck run to the Marina Run of the same name.
type MarinaRun struct {
	Run          domain.Run
	ProjectID    string
	Namespace    string
	Spec         json.RawMessage
	Created      bool
	LastSequence int64
}

// MarinaTurn links an invoked chat message to its Marina Turn.
type MarinaTurn struct {
	Name                string
	InvocationMessageID string
	ProjectID           string
	Namespace           string
	Spec                json.RawMessage
	Created             bool
}

// EnsureMarinaAgent registers Marina as the executor and as the chat agent of
// the mapped projects, so the existing agent picker offers it.
func (s *Store) EnsureMarinaAgent(ctx context.Context, projectIDs []string) error {
	capabilities := make([]string, 0, len(projectIDs))
	for _, id := range projectIDs {
		capabilities = append(capabilities, "conversation.invoke."+id)
	}
	_, err := s.RegisterAgent(ctx, domain.Agent{ID: MarinaAgentID, InstallationID: MarinaAgentID, Name: "Marina", Kind: "marina", Status: "idle", Capabilities: capabilities})
	return err
}

// RecordMarinaContact keeps the provider status truthful: a successful cycle
// marks Marina reachable for the project, a failed one records the reason.
func (s *Store) RecordMarinaContact(ctx context.Context, projectID, problem string) error {
	if problem != "" {
		return s.RecordProjectionRejected(ctx, projectID, MarinaAgentID, problem, nil)
	}
	now := nowMillis()
	if _, err := s.db.ExecContext(ctx, "UPDATE agents SET last_seen_at = ? WHERE id = ?", now, MarinaAgentID); err != nil {
		return err
	}
	return s.RecordProjectionAccepted(ctx, projectID, MarinaAgentID, now)
}

// MarinaReadyTasks lists Ready tasks of the given projects whose dependencies
// are satisfied, in priority order.
func (s *Store) MarinaReadyTasks(ctx context.Context, projectIDs []string) ([]domain.Task, error) {
	if len(projectIDs) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskColumns+` FROM tasks
		WHERE status = 'ready' AND archived = 0 AND project_id IN (SELECT value FROM json_each(?)) AND `+dependenciesEligibleSQL+`
		ORDER BY CASE priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END, created_at`, marshalJSON(projectIDs))
	if err != nil {
		return nil, fmt.Errorf("list ready Marina work: %w", err)
	}
	defer rows.Close()
	var tasks []domain.Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

var nameUnsafe = regexp.MustCompile(`[^a-z0-9-]+`)

// MarinaName derives a deterministic Marina object name from a Deck ID.
func MarinaName(id string, suffix string) string {
	base := strings.Trim(nameUnsafe.ReplaceAllString(strings.ToLower(id), "-"), "-")
	if len(base) > 40 {
		sum := sha256.Sum256([]byte(id))
		base = strings.Trim(base[:31], "-") + "-" + hex.EncodeToString(sum[:4])
	}
	name := "deck-" + base
	if suffix != "" {
		name += "-" + suffix
	}
	return name
}

// BeginMarinaRun claims a Ready task for Marina and records the exact Run spec
// in the same transaction. The run is named deterministically, so the create
// call can be retried until Marina confirms it, without a second Run.
func (s *Store) BeginMarinaRun(ctx context.Context, taskID string, taskVersion int, namespace string, spec json.RawMessage) (domain.Run, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Run{}, err
	}
	defer tx.Rollback()
	var attempt int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) + 1 FROM marina_runs WHERE task_id = ?", taskID).Scan(&attempt); err != nil {
		return domain.Run{}, err
	}
	name := MarinaName(taskID, fmt.Sprint(attempt))
	run, event, err := claimTaskTx(ctx, tx, taskID, taskVersion, MarinaAgentID, name, nil, "Submitted to Marina as run "+name)
	if err != nil {
		return domain.Run{}, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO marina_runs (run_id, task_id, namespace, spec_json, created_at) VALUES (?, ?, ?, ?, ?)",
		run.ID, taskID, namespace, string(spec), nowMillis()); err != nil {
		return domain.Run{}, fmt.Errorf("record Marina run: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Run{}, err
	}
	s.publish(event)
	return run, nil
}

const marinaRunColumns = "m.namespace, m.spec_json, m.created, m.last_sequence, t.project_id, r.id, r.task_id, r.agent_id, r.status, r.plan, r.result_summary, r.lease_expires_at, r.started_at, r.finished_at, r.updated_at, r.version, r.fence_token, r.task_version, r.acceptance_criteria_digest, r.active_plan_revision_id"

func scanMarinaRun(scanner interface{ Scan(...any) error }) (MarinaRun, error) {
	var item MarinaRun
	var spec string
	var leaseExpiresAt, finishedAt sql.NullInt64
	var activePlan sql.NullString
	run := &item.Run
	err := scanner.Scan(&item.Namespace, &spec, &item.Created, &item.LastSequence, &item.ProjectID,
		&run.ID, &run.TaskID, &run.AgentID, &run.Status, &run.Plan, &run.ResultSummary, &leaseExpiresAt, &run.StartedAt,
		&finishedAt, &run.UpdatedAt, &run.Version, &run.FenceToken, &run.TaskVersion, &run.AcceptanceCriteriaDigest, &activePlan)
	if err != nil {
		return item, normalizeStoreError(err)
	}
	item.Spec = json.RawMessage(spec)
	if finishedAt.Valid {
		run.FinishedAt = &finishedAt.Int64
	}
	if activePlan.Valid {
		run.ActivePlanRevisionID = &activePlan.String
	}
	return item, nil
}

// ActiveMarinaRuns lists runs Marina has not finished, or has not yet
// confirmed creating.
func (s *Store) ActiveMarinaRuns(ctx context.Context) ([]MarinaRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+marinaRunColumns+` FROM marina_runs m JOIN runs r ON r.id = m.run_id JOIN tasks t ON t.id = r.task_id
		WHERE m.created = 0 OR r.status IN ('planning', 'waiting_decision', 'running', 'verifying') ORDER BY m.created_at, m.run_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []MarinaRun
	for rows.Next() {
		item, err := scanMarinaRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetMarinaRun returns the Marina link of a Deck run, or ErrNotFound for a run
// Marina does not own.
func (s *Store) GetMarinaRun(ctx context.Context, runID string) (MarinaRun, error) {
	return scanMarinaRun(s.db.QueryRowContext(ctx, `SELECT `+marinaRunColumns+` FROM marina_runs m JOIN runs r ON r.id = m.run_id JOIN tasks t ON t.id = r.task_id WHERE m.run_id = ?`, runID))
}

func (s *Store) MarkMarinaRunCreated(ctx context.Context, runID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE marina_runs SET created = 1 WHERE run_id = ?", runID)
	return err
}

// AdvanceMarinaRunSequence records the last mirrored Marina event; it never
// moves backwards.
func (s *Store) AdvanceMarinaRunSequence(ctx context.Context, runID string, sequence int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE marina_runs SET last_sequence = ? WHERE run_id = ? AND last_sequence < ?", sequence, runID, sequence)
	return err
}

// CloseDecision ends a pending decision that Marina expired or closed.
func (s *Store) CloseDecision(ctx context.Context, id, status string) error {
	if status != "expired" && status != "superseded" && status != "cancelled" {
		return domain.ErrInvalidInput
	}
	_, err := s.db.ExecContext(ctx, "UPDATE decisions SET status = ? WHERE id = ? AND status = 'pending'", status, id)
	return err
}

// SetRepositoryBinding records the repository Marina bound for a project.
// Marina's Project is authoritative; Deck keeps a safe copy that new tasks
// freeze when they become Ready.
func (s *Store) SetRepositoryBinding(ctx context.Context, projectID string, binding domain.RepositoryBinding) error {
	if binding.Version < 1 || !validRepositoryInput(domain.RepositoryRequestInput{Repository: binding.Repository, BaseBranch: binding.BaseBranch}) {
		return domain.ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := getRepository(ctx, tx, projectID)
	if err != nil {
		return err
	}
	if current.Status == "connected" && current.Binding != nil && *current.Binding == binding {
		return nil
	}
	next := domain.RepositoryRequest{RepositoryRequestInput: domain.RepositoryRequestInput{Repository: binding.Repository, BaseBranch: binding.BaseBranch,
		ExpectedRevision: current.Revision, ExpectedBindingVersion: binding.Version}, Revision: current.Revision + 1, Status: "connected", Binding: &binding}
	if _, err := tx.ExecContext(ctx, `INSERT INTO repository_requests(project_id, document_json) VALUES (?, ?)
		ON CONFLICT(project_id) DO UPDATE SET document_json = excluded.document_json`, projectID, marshalJSON(next)); err != nil {
		return err
	}
	return tx.Commit()
}

// PendingMarinaInvocations lists chat requests addressed to Marina in the given
// projects that have no Turn and no outcome yet.
func (s *Store) PendingMarinaInvocations(ctx context.Context, projectIDs []string) ([]domain.ChannelMessage, error) {
	if len(projectIDs) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+channelMessageColumns+` FROM channel_messages m
		WHERE m.invoked_agent_id = ? AND m.invocation_request_id <> '' AND m.project_id IN (SELECT value FROM json_each(?))
		AND NOT EXISTS (SELECT 1 FROM marina_turns t WHERE t.invocation_message_id = m.id)
		AND NOT EXISTS (SELECT 1 FROM conversation_outcomes o WHERE o.invocation_message_id = m.id)
		ORDER BY m.rowid`, MarinaAgentID, marshalJSON(projectIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []domain.ChannelMessage
	for rows.Next() {
		message, err := scanChannelMessage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, message)
	}
	return items, rows.Err()
}

// ThreadUpTo returns the invocation's thread through the invocation itself,
// oldest first, keeping the newest limit messages.
func (s *Store) ThreadUpTo(ctx context.Context, invocationID string, limit int) ([]domain.ChannelMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+channelMessageColumns+` FROM (
		SELECT m.*, m.rowid AS position FROM channel_messages m JOIN channel_messages i ON i.id = ?
		WHERE m.thread_id = i.thread_id AND m.rowid <= i.rowid ORDER BY m.rowid DESC LIMIT ?) ORDER BY position`, invocationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []domain.ChannelMessage
	for rows.Next() {
		message, err := scanChannelMessage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, message)
	}
	return items, rows.Err()
}

// BeginMarinaTurn records the exact Turn spec before Marina is called.
func (s *Store) BeginMarinaTurn(ctx context.Context, invocationID, name, namespace string, spec json.RawMessage) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO marina_turns (name, invocation_message_id, namespace, spec_json, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(invocation_message_id) DO NOTHING`, name, invocationID, namespace, string(spec), nowMillis())
	return err
}

const marinaTurnColumns = "t.name, t.invocation_message_id, m.project_id, t.namespace, t.spec_json, t.created"

func scanMarinaTurn(scanner interface{ Scan(...any) error }) (MarinaTurn, error) {
	var item MarinaTurn
	var spec string
	if err := scanner.Scan(&item.Name, &item.InvocationMessageID, &item.ProjectID, &item.Namespace, &spec, &item.Created); err != nil {
		return item, normalizeStoreError(err)
	}
	item.Spec = json.RawMessage(spec)
	return item, nil
}

func (s *Store) ActiveMarinaTurns(ctx context.Context) ([]MarinaTurn, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+marinaTurnColumns+` FROM marina_turns t JOIN channel_messages m ON m.id = t.invocation_message_id
		WHERE t.finished = 0 ORDER BY t.created_at, t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []MarinaTurn
	for rows.Next() {
		item, err := scanMarinaTurn(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetMarinaTurn returns the Turn of an invoked message, or ErrNotFound.
func (s *Store) GetMarinaTurn(ctx context.Context, invocationID string) (MarinaTurn, error) {
	return scanMarinaTurn(s.db.QueryRowContext(ctx, `SELECT `+marinaTurnColumns+` FROM marina_turns t JOIN channel_messages m ON m.id = t.invocation_message_id
		WHERE t.invocation_message_id = ?`, invocationID))
}

func (s *Store) MarkMarinaTurnCreated(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE marina_turns SET created = 1 WHERE name = ?", name)
	return err
}

func (s *Store) FinishMarinaTurn(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE marina_turns SET finished = 1 WHERE name = ?", name)
	return err
}

// FindEngineQuestion returns the run's decision for one Marina question.
func (s *Store) FindEngineQuestion(ctx context.Context, runID, operationID string) (domain.Decision, error) {
	decision, err := scanDecision(s.db.QueryRowContext(ctx, "SELECT "+decisionColumns+" FROM decisions WHERE run_id = ? AND interaction_id = ?", runID, operationID))
	return decision, normalizeStoreError(err)
}
