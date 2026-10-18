package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

// The run lifecycle below mirrors the work-control contract Marina's
// workflow already uses: idempotent mutations, an exact run version and fence
// for every change to an active run, and completion only with admissible,
// independent evidence on the active plan's source revision.

const runColumns = `id, work_id, agent_id, status, version, fence_token, task_version, acceptance_digest, plan,
	active_plan_revision_id, result_summary, lease_expires_at, started_at, finished_at, updated_at`

func scanRun(row scanner) (workcontrol.Run, error) {
	var r workcontrol.Run
	var lease, finished sql.NullInt64
	var started, updated int64
	if err := row.Scan(&r.ID, &r.WorkItemID, &r.AgentID, &r.Status, &r.Version, &r.FenceToken, &r.TaskVersion, &r.AcceptanceCriteriaDigest,
		&r.Plan, &r.ActivePlanRevisionID, &r.ResultSummary, &lease, &started, &finished, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return workcontrol.Run{}, notFound("run")
		}
		return workcontrol.Run{}, err
	}
	if lease.Valid {
		value := fromMillis(lease.Int64)
		r.LeaseExpiresAt = &value
	}
	if finished.Valid {
		value := fromMillis(finished.Int64)
		r.FinishedAt = &value
	}
	r.StartedAt, r.UpdatedAt = fromMillis(started), fromMillis(updated)
	return r, nil
}

func runByID(ctx context.Context, db execer, id string) (workcontrol.Run, error) {
	return scanRun(db.QueryRowContext(ctx, "SELECT "+runColumns+" FROM platform_runs WHERE id = ?", id))
}

func guardRun(run workcontrol.Run, agentID string, expectedVersion, fence int64) error {
	if run.AgentID != agentID || run.Version != expectedVersion || run.FenceToken != fence {
		return conflict("run %s changed: expected version %d and fence %d", run.ID, expectedVersion, fence)
	}
	return nil
}

// setWorkStatus moves the Run resource's ledger status with its run.
func setWorkStatus(ctx context.Context, db execer, workID, status string, now time.Time) error {
	_, err := db.ExecContext(ctx, "UPDATE platform_work SET status = ?, version = version + 1, updated_at = ? WHERE id = ?", status, millis(now), workID)
	return err
}

// --- provider information -------------------------------------------------

func (p *Platform) Health(ctx context.Context) error { return p.store.db.PingContext(ctx) }

func (p *Platform) GetProviderInfo(context.Context) (workcontrol.ProviderInfo, error) {
	return workcontrol.ProviderInfo{Provider: "marina", ProtocolVersion: "marina.dev/v1", Capabilities: platformCapabilities(), ServerTime: p.store.now(), Ready: true}, nil
}

func (p *Platform) CheckCompatibility(ctx context.Context, required []string) (workcontrol.ProviderInfo, error) {
	info, _ := p.GetProviderInfo(ctx)
	for _, capability := range required {
		if !containsString(info.Capabilities, capability) {
			return info, fmt.Errorf("the platform ledger does not offer %s", capability)
		}
	}
	return info, nil
}

func platformCapabilities() []string {
	return append(workcontrol.WorkerCapabilities(), workcontrol.CapabilityRunCompleteLanded, workcontrol.CapabilityCommandList, workcontrol.CapabilityCommandAck,
		workcontrol.CapabilityEventAppend, workcontrol.CapabilityDecisionRequest, workcontrol.CapabilityConversationList, workcontrol.CapabilityConversationContext,
		workcontrol.CapabilityConversationQuestion, workcontrol.CapabilityConversationOutcome, workcontrol.CapabilityConversationPRD, workcontrol.CapabilityConversationPRDTasks,
		workcontrol.CapabilityChannelRead)
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

// RegisterAgent accepts Marina's own logical identity.
func (p *Platform) RegisterAgent(_ context.Context, registration workcontrol.AgentRegistration) (workcontrol.Agent, error) {
	if registration.ID == "" {
		return workcontrol.Agent{}, invalid("agent id is required")
	}
	now := p.store.now()
	return workcontrol.Agent{ID: registration.ID, InstallationID: registration.InstallationID, Name: registration.Name, Kind: registration.Kind,
		Status: "active", Capabilities: registration.Capabilities, LastSeenAt: now, CreatedAt: now}, nil
}

// --- discovery and claims -------------------------------------------------

// ListReadyWork returns Runs whose dependencies have all succeeded.
func (p *Platform) ListReadyWork(ctx context.Context) ([]workcontrol.WorkItem, error) {
	rows, err := p.store.db.QueryContext(ctx, "SELECT "+workColumns+" FROM platform_work WHERE status = 'ready' AND cancel_requested = 0 ORDER BY created_at, id LIMIT 100")
	if err != nil {
		return nil, err
	}
	var candidates []PlatformWork
	for rows.Next() {
		w, err := scanWork(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, w)
	}
	rows.Close()
	var ready []workcontrol.WorkItem
	for _, w := range candidates {
		eligible := true
		for _, dependency := range w.DependsOn {
			var status string
			err := p.store.db.QueryRowContext(ctx, "SELECT status FROM platform_work WHERE id = ?", PlatformWorkID(w.Namespace, dependency)).Scan(&status)
			eligible = eligible && err == nil && status == "completed"
		}
		if eligible {
			ready = append(ready, w.Item)
		}
	}
	return ready, nil
}

func (p *Platform) GetWork(ctx context.Context, id string) (workcontrol.WorkDetail, error) {
	w, err := p.Work(ctx, id)
	if err != nil {
		return workcontrol.WorkDetail{}, err
	}
	detail := workcontrol.WorkDetail{Work: w.Item}
	rows, err := p.store.db.QueryContext(ctx, "SELECT "+runColumns+" FROM platform_runs WHERE work_id = ? ORDER BY started_at, id", id)
	if err != nil {
		return detail, err
	}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			rows.Close()
			return detail, err
		}
		detail.Runs = append(detail.Runs, run)
	}
	rows.Close()
	for _, run := range detail.Runs {
		revisions, err := p.planRevisions(ctx, run.ID)
		if err != nil {
			return detail, err
		}
		detail.PlanRevisions = append(detail.PlanRevisions, revisions...)
		decisions, err := p.decisionsFor(ctx, run.ID)
		if err != nil {
			return detail, err
		}
		detail.Decisions = append(detail.Decisions, decisions...)
	}
	return detail, nil
}

func (p *Platform) ClaimWork(ctx context.Context, workID string, claim workcontrol.Claim) (workcontrol.Run, error) {
	var run workcontrol.Run
	err := p.mutation(ctx, claim.IdempotencyKey, "claim", workID, claim, &run, func(tx *sql.Tx) (any, error) {
		w, err := scanWork(tx.QueryRowContext(ctx, "SELECT "+workColumns+" FROM platform_work WHERE id = ?", workID))
		if err != nil {
			return nil, err
		}
		if w.Status != workReady || w.CancelRequested || (claim.ExpectedTaskVersion > 0 && w.Version != claim.ExpectedTaskVersion) {
			return nil, conflict("run %s is not available to claim", workID)
		}
		var fence int64
		if err := tx.QueryRowContext(ctx, "SELECT next_fence FROM platform_work WHERE id = ?", workID).Scan(&fence); err != nil {
			return nil, err
		}
		lease := claim.Lease
		if lease <= 0 {
			lease = 30 * time.Second
		}
		now := p.store.now()
		criteria := sha256.Sum256([]byte(w.Item.AcceptanceCriteria))
		id, err := newID("prun")
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO platform_runs (id, work_id, agent_id, status, version, fence_token, task_version, acceptance_digest, lease_expires_at, started_at, updated_at)
			VALUES (?, ?, ?, 'planning', 1, ?, ?, ?, ?, ?, ?)`, id, workID, claim.AgentID, fence, w.Version, hex.EncodeToString(criteria[:]),
			millis(now.Add(lease)), millis(now), millis(now)); err != nil {
			return nil, err
		}
		result, err := tx.ExecContext(ctx, "UPDATE platform_work SET status = 'planning', version = version + 1, next_fence = next_fence + 1, updated_at = ? WHERE id = ? AND version = ?",
			millis(now), workID, w.Version)
		if err != nil {
			return nil, err
		}
		if err := requireOneMutation(result); err != nil {
			return nil, conflict("run %s changed during the claim", workID)
		}
		if err := p.appendEvent(ctx, tx, workID, "run.claimed", "Marina claimed the run and started planning", map[string]any{"run_id": id, "fence_token": fence}, ""); err != nil {
			return nil, err
		}
		return runByID(ctx, tx, id)
	})
	return run, err
}

// LookupClaim returns the current state of the run a claim key created.
func (p *Platform) LookupClaim(ctx context.Context, _ string, key string) (workcontrol.Run, error) {
	var result string
	if err := p.store.db.QueryRowContext(ctx, "SELECT result_json FROM platform_mutations WHERE key = ? AND operation = 'claim'", key).Scan(&result); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return workcontrol.Run{}, notFound("claim")
		}
		return workcontrol.Run{}, err
	}
	var claimed workcontrol.Run
	if err := json.Unmarshal([]byte(result), &claimed); err != nil {
		return workcontrol.Run{}, err
	}
	return runByID(ctx, p.store.db, claimed.ID)
}

func (p *Platform) ListAssignedRuns(ctx context.Context, project, agentID, _ string, limit int) (workcontrol.ActiveRunPage, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	rows, err := p.store.db.QueryContext(ctx, "SELECT r.id FROM platform_runs r JOIN platform_work w ON w.id = r.work_id WHERE r.agent_id = ? AND w.project_id = ? AND r.status IN "+runActiveStatusList+" ORDER BY r.started_at LIMIT ?",
		agentID, project, limit)
	if err != nil {
		return workcontrol.ActiveRunPage{}, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return workcontrol.ActiveRunPage{}, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	var page workcontrol.ActiveRunPage
	for _, id := range ids {
		run, err := runByID(ctx, p.store.db, id)
		if err != nil {
			return page, err
		}
		w, err := p.Work(ctx, run.WorkItemID)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, workcontrol.ActiveRun{Run: run, Work: w.Item})
	}
	return page, nil
}

// --- active run changes -----------------------------------------------------

func (p *Platform) RenewRunLease(ctx context.Context, runID string, renewal workcontrol.LeaseRenewal) error {
	return p.mutation(ctx, renewal.IdempotencyKey, "lease", runID, renewal, nil, func(tx *sql.Tx) (any, error) {
		run, err := runByID(ctx, tx, runID)
		if err != nil {
			return nil, err
		}
		if err := guardRun(run, renewal.AgentID, renewal.ExpectedVersion, renewal.FenceToken); err != nil {
			return nil, err
		}
		if !run.Status.Active() {
			return nil, conflict("run %s is no longer active", runID)
		}
		lease := renewal.Lease
		if lease <= 0 {
			lease = 30 * time.Second
		}
		now := p.store.now()
		_, err = tx.ExecContext(ctx, "UPDATE platform_runs SET lease_expires_at = ?, version = version + 1, updated_at = ? WHERE id = ?", millis(now.Add(lease)), millis(now), runID)
		return struct{}{}, err
	})
}

// transition moves an active run from one of the allowed states, with the
// matching Run status and a client event.
func (p *Platform) transition(ctx context.Context, operation, runID string, m workcontrol.Mutation, request any, from []workcontrol.RunStatus,
	to workcontrol.RunStatus, workStatus, eventType, message string, extra func(*sql.Tx, workcontrol.Run, time.Time) error) error {
	return p.mutation(ctx, m.IdempotencyKey, operation, runID, request, nil, func(tx *sql.Tx) (any, error) {
		run, err := runByID(ctx, tx, runID)
		if err != nil {
			return nil, err
		}
		if err := guardRun(run, m.AgentID, m.ExpectedVersion, m.FenceToken); err != nil {
			return nil, err
		}
		allowed := false
		for _, status := range from {
			allowed = allowed || run.Status == status
		}
		if !allowed {
			return nil, conflict("run %s cannot move from %s to %s", runID, run.Status, to)
		}
		now := p.store.now()
		if extra != nil {
			if err := extra(tx, run, now); err != nil {
				return nil, err
			}
		}
		finished := any(nil)
		lease := any(nil)
		if to.Terminal() {
			finished = millis(now)
		} else if run.LeaseExpiresAt != nil {
			lease = millis(*run.LeaseExpiresAt)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE platform_runs SET status = ?, version = version + 1, updated_at = ?, finished_at = COALESCE(?, finished_at), lease_expires_at = ? WHERE id = ?",
			string(to), millis(now), finished, lease, runID); err != nil {
			return nil, err
		}
		if err := setWorkStatus(ctx, tx, run.WorkItemID, workStatus, now); err != nil {
			return nil, err
		}
		if to.Terminal() || to == workcontrol.RunVerifying {
			if _, err := tx.ExecContext(ctx, "UPDATE platform_decisions SET status = 'superseded', updated_at = ? WHERE run_id = ? AND status = 'pending'", millis(now), runID); err != nil {
				return nil, err
			}
		}
		return struct{}{}, p.appendEvent(ctx, tx, run.WorkItemID, eventType, message, nil, "")
	})
}

func (p *Platform) StartRun(ctx context.Context, runID string, m workcontrol.Mutation) error {
	return p.transition(ctx, "start", runID, m, m, []workcontrol.RunStatus{workcontrol.RunWaitingDecision}, workcontrol.RunRunning, "running",
		"run.started", "Plan approved; execution started", func(tx *sql.Tx, run workcontrol.Run, now time.Time) error {
			decision, err := latestPlanDecision(ctx, tx, run.ID)
			if err != nil {
				return err
			}
			if decision.Status != workcontrol.DecisionAnswered || decision.PlanRevisionID != run.ActivePlanRevisionID {
				return conflict("the active plan is not approved")
			}
			if !strings.EqualFold(decision.Answer, decision.Recommendation) {
				return conflict("the plan was not approved")
			}
			return nil
		})
}

func (p *Platform) BeginVerification(ctx context.Context, runID string, m workcontrol.Mutation) error {
	return p.transition(ctx, "verify", runID, m, m, []workcontrol.RunStatus{workcontrol.RunRunning}, workcontrol.RunVerifying, "verifying",
		"run.verifying", "Execution finished; verifying acceptance criteria", nil)
}

func (p *Platform) BlockRun(ctx context.Context, runID string, block workcontrol.RunBlock) error {
	if strings.TrimSpace(block.Reason) == "" {
		return invalid("a block reason is required")
	}
	m := workcontrol.Mutation{IdempotencyKey: block.IdempotencyKey, AgentID: block.AgentID, ExpectedVersion: block.ExpectedVersion, FenceToken: block.FenceToken}
	return p.transition(ctx, "block", runID, m, block, []workcontrol.RunStatus{workcontrol.RunPlanning, workcontrol.RunWaitingDecision, workcontrol.RunRunning, workcontrol.RunVerifying},
		workcontrol.RunBlocked, "blocked", "run.blocked", block.Reason, func(tx *sql.Tx, run workcontrol.Run, now time.Time) error {
			_, err := tx.ExecContext(ctx, "UPDATE platform_runs SET result_summary = ? WHERE id = ?", block.Reason, run.ID)
			return err
		})
}

func (p *Platform) ReportRunProgress(ctx context.Context, runID string, progress workcontrol.Progress) error {
	return p.mutation(ctx, progress.IdempotencyKey, "progress", runID, progress, nil, func(tx *sql.Tx) (any, error) {
		run, err := runByID(ctx, tx, runID)
		if err != nil {
			return nil, err
		}
		if err := guardRun(run, progress.AgentID, progress.ExpectedVersion, progress.FenceToken); err != nil {
			return nil, err
		}
		if run.Status != workcontrol.RunRunning || strings.TrimSpace(progress.Message) == "" {
			return nil, conflict("progress needs a running run and a message")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE platform_runs SET version = version + 1, updated_at = ? WHERE id = ?", millis(p.store.now()), runID); err != nil {
			return nil, err
		}
		return struct{}{}, p.appendEvent(ctx, tx, run.WorkItemID, "run.progress", progress.Message, progress.Payload, "")
	})
}

// AppendRunEvent records Marina's own projected progress, deduplicated by its
// producer identity, without changing the run version.
func (p *Platform) AppendRunEvent(ctx context.Context, runID string, event workcontrol.ProjectedEvent) error {
	return p.mutation(ctx, event.IdempotencyKey, "event", runID, event, nil, func(tx *sql.Tx) (any, error) {
		run, err := runByID(ctx, tx, runID)
		if err != nil {
			return nil, err
		}
		if run.AgentID != event.AgentID || run.FenceToken != event.FenceToken {
			return nil, conflict("event for run %s has a stale fence", runID)
		}
		message := event.Message
		if message == "" {
			message = event.Type
		}
		return struct{}{}, p.appendEvent(ctx, tx, run.WorkItemID, event.Type, message, event.Payload, event.Producer+":"+event.ProducerEventID)
	})
}

// --- plans and decisions -------------------------------------------------

func (p *Platform) planRevisions(ctx context.Context, runID string) ([]workcontrol.PlanRevision, error) {
	rows, err := p.store.db.QueryContext(ctx, "SELECT revision_json FROM platform_plan_revisions WHERE run_id = ? ORDER BY revision", runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []workcontrol.PlanRevision
	for rows.Next() {
		var encoded string
		var revision workcontrol.PlanRevision
		if err := rows.Scan(&encoded); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(encoded), &revision); err != nil {
			return nil, err
		}
		out = append(out, revision)
	}
	return out, rows.Err()
}

func (p *Platform) SubmitPlanRevision(ctx context.Context, runID string, submission workcontrol.PlanRevisionSubmission) (workcontrol.PlanRevisionResult, error) {
	var result workcontrol.PlanRevisionResult
	if strings.TrimSpace(submission.Plan) == "" || strings.TrimSpace(submission.ContextDigest) == "" {
		return result, invalid("a plan and its context digest are required")
	}
	err := p.mutation(ctx, submission.IdempotencyKey, "plan", runID, submission, &result, func(tx *sql.Tx) (any, error) {
		run, err := runByID(ctx, tx, runID)
		if err != nil {
			return nil, err
		}
		if err := guardRun(run, submission.AgentID, submission.ExpectedVersion, submission.FenceToken); err != nil {
			return nil, err
		}
		if run.Status != workcontrol.RunPlanning {
			return nil, conflict("run %s is not planning", runID)
		}
		w, err := scanWork(tx.QueryRowContext(ctx, "SELECT "+workColumns+" FROM platform_work WHERE id = ?", run.WorkItemID))
		if err != nil {
			return nil, err
		}
		var number int
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(revision), 0) + 1 FROM platform_plan_revisions WHERE run_id = ?", runID).Scan(&number); err != nil {
			return nil, err
		}
		now := p.store.now()
		id, err := newID("ppln")
		if err != nil {
			return nil, err
		}
		revision := workcontrol.PlanRevision{ID: id, RunID: runID, Revision: number, Plan: strings.TrimSpace(submission.Plan), ContextDigest: submission.ContextDigest,
			RiskClass: submission.RiskClass, VerificationStrategy: submission.VerificationStrategy, ExpectedSideEffects: submission.ExpectedSideEffects,
			RollbackApproach: submission.RollbackApproach, SourceRevision: submission.SourceRevision, PolicyVersion: submission.PolicyVersion, CreatedAt: now}
		encoded, _ := json.Marshal(revision)
		if _, err := tx.ExecContext(ctx, "INSERT INTO platform_plan_revisions (id, run_id, revision, revision_json, created_at) VALUES (?, ?, ?, ?, ?)",
			id, runID, number, string(encoded), millis(now)); err != nil {
			return nil, err
		}
		next, workStatus := workcontrol.RunRunning, "running"
		if submission.RequireApproval {
			next, workStatus = workcontrol.RunWaitingDecision, "waiting_decision"
		}
		if _, err := tx.ExecContext(ctx, "UPDATE platform_runs SET plan = ?, active_plan_revision_id = ?, status = ?, version = version + 1, updated_at = ? WHERE id = ?",
			revision.Plan, id, string(next), millis(now), runID); err != nil {
			return nil, err
		}
		if err := setWorkStatus(ctx, tx, run.WorkItemID, workStatus, now); err != nil {
			return nil, err
		}
		if err := p.appendEvent(ctx, tx, run.WorkItemID, "plan.submitted", fmt.Sprintf("Plan revision %d submitted", number),
			map[string]any{"revision": number, "requires_approval": submission.RequireApproval}, ""); err != nil {
			return nil, err
		}
		out := workcontrol.PlanRevisionResult{PlanRevision: revision}
		if submission.RequireApproval {
			decision := workcontrol.Decision{TaskID: run.WorkItemID, RunID: runID, PlanRevisionID: id,
				Question: fmt.Sprintf("Approve plan revision %d for %q?", number, w.Item.Title), Options: []string{"Approve plan", "Request changes"},
				Recommendation: "Approve plan", RiskClass: submission.RiskClass, EvidenceSummary: submission.DecisionEvidence,
				EstimatedCostMinor: submission.EstimatedCostMinor, Currency: submission.Currency, WaitingConsequence: submission.WaitingConsequence,
				RejectingConsequence: submission.RejectingConsequence, ExpiresAt: submission.DecisionExpiresAt}
			created, err := p.insertDecision(ctx, tx, w.Namespace, fmt.Sprintf("%s-plan-%d", w.Name, number), "Run", run.WorkItemID, decision)
			if err != nil {
				return nil, err
			}
			out.Decision = &created
		}
		return out, nil
	})
	return result, err
}

func (p *Platform) RequestEngineQuestion(ctx context.Context, runID string, question workcontrol.EngineQuestion) (workcontrol.Decision, error) {
	var decision workcontrol.Decision
	err := p.mutation(ctx, question.IdempotencyKey, "question", runID, question, &decision, func(tx *sql.Tx) (any, error) {
		run, err := runByID(ctx, tx, runID)
		if err != nil {
			return nil, err
		}
		if err := guardRun(run, question.AgentID, question.ExpectedVersion, question.FenceToken); err != nil {
			return nil, err
		}
		if run.Status != workcontrol.RunRunning || strings.TrimSpace(question.Question) == "" || len(question.Options) > 6 {
			return nil, conflict("a question needs a running run, text and at most six options")
		}
		w, err := scanWork(tx.QueryRowContext(ctx, "SELECT "+workColumns+" FROM platform_work WHERE id = ?", run.WorkItemID))
		if err != nil {
			return nil, err
		}
		expires := question.ExpiresAt
		mode := "choice"
		if len(question.Options) == 0 {
			mode = "text"
		}
		return p.insertDecision(ctx, tx, w.Namespace, fmt.Sprintf("%s-question-%s", w.Name, shortDigest(question.OperationID)), "Run", run.WorkItemID,
			workcontrol.Decision{TaskID: run.WorkItemID, RunID: runID, InteractionID: question.OperationID, AnswerMode: mode, Question: question.Question,
				Options: question.Options, ExpiresAt: &expires})
	})
	return decision, err
}

func shortDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:4])
}

func (p *Platform) insertDecision(ctx context.Context, tx *sql.Tx, namespace, name, ownerKind, ownerID string, decision workcontrol.Decision) (workcontrol.Decision, error) {
	id, err := newID("pdec")
	if err != nil {
		return workcontrol.Decision{}, err
	}
	now := p.store.now()
	decision.ID, decision.Status, decision.CreatedAt = id, workcontrol.DecisionPending, now
	encoded, _ := json.Marshal(decision)
	expires := any(nil)
	if decision.ExpiresAt != nil {
		expires = millis(*decision.ExpiresAt)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO platform_decisions (id, namespace, name, owner_kind, owner_id, run_id, decision_json, status, expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?)`, id, namespace, name, ownerKind, ownerID, decision.RunID, string(encoded), expires, millis(now), millis(now)); err != nil {
		return workcontrol.Decision{}, err
	}
	return decision, p.appendEvent(ctx, tx, ownerID, "decision.requested", decision.Question, map[string]any{"decision": name}, "")
}

const decisionColumns = "id, namespace, name, owner_kind, owner_id, decision_json, status, answer, answered_by, answered_at"

// PlatformDecision is a Decision resource as the ledger holds it.
type PlatformDecision struct {
	Namespace, Name, OwnerKind, OwnerID string
	Decision                            workcontrol.Decision
}

func scanDecision(row scanner) (PlatformDecision, error) {
	var d PlatformDecision
	var id, encoded, status, answer, by string
	var answeredAt sql.NullInt64
	if err := row.Scan(&id, &d.Namespace, &d.Name, &d.OwnerKind, &d.OwnerID, &encoded, &status, &answer, &by, &answeredAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PlatformDecision{}, notFound("decision")
		}
		return PlatformDecision{}, err
	}
	if err := json.Unmarshal([]byte(encoded), &d.Decision); err != nil {
		return PlatformDecision{}, err
	}
	d.Decision.ID, d.Decision.Status, d.Decision.Answer, d.Decision.AnsweredBy = id, workcontrol.DecisionStatus(status), answer, by
	if answeredAt.Valid {
		value := fromMillis(answeredAt.Int64)
		d.Decision.AnsweredAt = &value
	}
	return d, nil
}

func latestPlanDecision(ctx context.Context, db execer, runID string) (workcontrol.Decision, error) {
	d, err := scanDecision(db.QueryRowContext(ctx, "SELECT "+decisionColumns+" FROM platform_decisions WHERE run_id = ? AND json_extract(decision_json, '$.PlanRevisionID') <> '' ORDER BY created_at DESC, id DESC LIMIT 1", runID))
	return d.Decision, err
}

func (p *Platform) decisionsFor(ctx context.Context, runID string) ([]workcontrol.Decision, error) {
	rows, err := p.store.db.QueryContext(ctx, "SELECT "+decisionColumns+" FROM platform_decisions WHERE run_id = ? ORDER BY created_at, id", runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []workcontrol.Decision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d.Decision)
	}
	return out, rows.Err()
}

func (p *Platform) GetDecision(ctx context.Context, id string) (workcontrol.Decision, error) {
	d, err := scanDecision(p.store.db.QueryRowContext(ctx, "SELECT "+decisionColumns+" FROM platform_decisions WHERE id = ?", id))
	return d.Decision, err
}

// FindDecision returns a Decision resource by its namespace and name.
func (p *Platform) FindDecision(ctx context.Context, namespace, name string) (PlatformDecision, error) {
	return scanDecision(p.store.db.QueryRowContext(ctx, "SELECT "+decisionColumns+" FROM platform_decisions WHERE namespace = ? AND name = ?", namespace, name))
}

// ListDecisions returns the Decisions of a Run or Turn, oldest first.
func (p *Platform) ListDecisions(ctx context.Context, ownerID string) ([]PlatformDecision, error) {
	rows, err := p.store.db.QueryContext(ctx, "SELECT "+decisionColumns+" FROM platform_decisions WHERE owner_id = ? ORDER BY created_at, id", ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlatformDecision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// AnswerDecision records a client's answer once. An exact replay returns the
// recorded answer; a different, late or superseded answer is a conflict.
// "Request changes" on a plan sends the run back to planning.
func (p *Platform) AnswerDecision(ctx context.Context, namespace, name, answer, answeredBy string) (PlatformDecision, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return PlatformDecision{}, invalid("an answer is required")
	}
	tx, err := p.store.db.BeginTx(ctx, nil)
	if err != nil {
		return PlatformDecision{}, err
	}
	defer tx.Rollback()
	d, err := scanDecision(tx.QueryRowContext(ctx, "SELECT "+decisionColumns+" FROM platform_decisions WHERE namespace = ? AND name = ?", namespace, name))
	if err != nil {
		return PlatformDecision{}, err
	}
	if d.Decision.Status == workcontrol.DecisionAnswered && strings.EqualFold(d.Decision.Answer, answer) {
		return d, nil
	}
	if d.Decision.Status != workcontrol.DecisionPending {
		return d, conflict("decision %s is %s", name, d.Decision.Status)
	}
	now := p.store.now()
	if d.Decision.ExpiresAt != nil && !now.Before(*d.Decision.ExpiresAt) {
		if _, err := tx.ExecContext(ctx, "UPDATE platform_decisions SET status = 'expired', updated_at = ? WHERE id = ?", millis(now), d.Decision.ID); err != nil {
			return d, err
		}
		if err := tx.Commit(); err != nil {
			return d, err
		}
		d.Decision.Status = workcontrol.DecisionExpired
		return d, conflict("decision %s has expired", name)
	}
	if d.Decision.AnswerMode != "text" || len(d.Decision.Options) > 0 {
		matched := false
		for _, option := range d.Decision.Options {
			if strings.EqualFold(option, answer) {
				answer, matched = option, true
			}
		}
		if !matched {
			return d, invalid("answer is not one of %v", d.Decision.Options)
		}
	}
	if d.Decision.RunID != "" {
		run, err := runByID(ctx, tx, d.Decision.RunID)
		if err != nil {
			return d, err
		}
		if d.Decision.PlanRevisionID != "" && run.ActivePlanRevisionID != d.Decision.PlanRevisionID {
			return d, conflict("decision %s belongs to a superseded plan", name)
		}
		if d.Decision.PlanRevisionID != "" && strings.EqualFold(answer, "Request changes") {
			if _, err := tx.ExecContext(ctx, "UPDATE platform_runs SET status = 'planning', version = version + 1, updated_at = ? WHERE id = ? AND status = 'waiting_decision'", millis(now), run.ID); err != nil {
				return d, err
			}
			if err := setWorkStatus(ctx, tx, run.WorkItemID, "planning", now); err != nil {
				return d, err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE platform_decisions SET status = 'answered', answer = ?, answered_by = ?, answered_at = ?, updated_at = ? WHERE id = ? AND status = 'pending'",
		answer, answeredBy, millis(now), millis(now), d.Decision.ID); err != nil {
		return d, err
	}
	if err := p.appendEvent(ctx, tx, d.OwnerID, "decision.answered", "Answered: "+answer, map[string]any{"decision": name}, ""); err != nil {
		return d, err
	}
	if err := tx.Commit(); err != nil {
		return d, err
	}
	d.Decision.Status, d.Decision.Answer, d.Decision.AnsweredBy, d.Decision.AnsweredAt = workcontrol.DecisionAnswered, answer, answeredBy, &now
	return d, nil
}

// --- evidence and completion ---------------------------------------------

type platformEvidence struct {
	workcontrol.EvidenceSubmission
	RunID     string
	Admission string
}

func (p *Platform) RecordEvidence(ctx context.Context, runID string, m workcontrol.Mutation, input workcontrol.EvidenceSubmission) (string, error) {
	if input.PlanRevisionID == "" || input.Type == "" || input.Producer == "" || input.Verifier == "" || input.SourceRevision == "" ||
		input.ArtifactDigest == "" || input.ObservedAt.IsZero() || (input.Outcome != "passed" && input.Outcome != "failed") {
		return "", invalid("structured evidence fields are required")
	}
	var id string
	err := p.mutation(ctx, m.IdempotencyKey, "evidence", runID, struct {
		M workcontrol.Mutation
		E workcontrol.EvidenceSubmission
	}{m, input}, &id, func(tx *sql.Tx) (any, error) {
		run, err := runByID(ctx, tx, runID)
		if err != nil {
			return nil, err
		}
		if err := guardRun(run, m.AgentID, m.ExpectedVersion, m.FenceToken); err != nil {
			return nil, err
		}
		admission := "current"
		if run.ActivePlanRevisionID != input.PlanRevisionID {
			admission = "superseded"
		} else if !input.ExpiresAt.IsZero() && !p.store.now().Before(input.ExpiresAt) {
			admission = "stale"
		}
		evidenceID := input.ID
		if evidenceID == "" {
			if evidenceID, err = newID("pevd"); err != nil {
				return nil, err
			}
		}
		encoded, _ := json.Marshal(platformEvidence{EvidenceSubmission: input, RunID: runID, Admission: admission})
		if _, err := tx.ExecContext(ctx, "INSERT INTO platform_evidence (id, run_id, evidence_json, created_at) VALUES (?, ?, ?, ?)", evidenceID, runID, string(encoded), millis(p.store.now())); err != nil {
			return nil, err
		}
		return evidenceID, p.appendEvent(ctx, tx, run.WorkItemID, "evidence.recorded", input.Type+" "+input.Outcome, map[string]any{"evidence_id": evidenceID, "type": input.Type, "outcome": input.Outcome}, "")
	})
	return id, err
}

func (p *Platform) CompleteRun(ctx context.Context, runID string, m workcontrol.Mutation, summary string, evidenceIDs []string) error {
	return p.complete(ctx, runID, m, summary, evidenceIDs, nil)
}

func (p *Platform) CompleteLandedRun(ctx context.Context, runID string, m workcontrol.Mutation, summary string, evidenceIDs []string, landing workcontrol.LandingObservation) error {
	return p.complete(ctx, runID, m, summary, evidenceIDs, &landing)
}

// complete enforces the platform invariant: only passed, current evidence for
// the active plan's source revision, from a verifier other than its producer.
func (p *Platform) complete(ctx context.Context, runID string, m workcontrol.Mutation, summary string, evidenceIDs []string, landing *workcontrol.LandingObservation) error {
	if strings.TrimSpace(summary) == "" || len(evidenceIDs) == 0 {
		return invalid("a summary and evidence are required")
	}
	request := struct {
		M       workcontrol.Mutation
		Summary string
		IDs     []string
		Landing *workcontrol.LandingObservation
	}{m, summary, evidenceIDs, landing}
	return p.transition(ctx, "complete", runID, m, request, []workcontrol.RunStatus{workcontrol.RunVerifying}, workcontrol.RunCompleted, "completed",
		"run.completed", summary, func(tx *sql.Tx, run workcontrol.Run, now time.Time) error {
			var revisionJSON string
			if err := tx.QueryRowContext(ctx, "SELECT revision_json FROM platform_plan_revisions WHERE id = ?", run.ActivePlanRevisionID).Scan(&revisionJSON); err != nil {
				return conflict("run %s has no active plan", runID)
			}
			var revision workcontrol.PlanRevision
			if err := json.Unmarshal([]byte(revisionJSON), &revision); err != nil {
				return err
			}
			ids := append([]string(nil), evidenceIDs...)
			sort.Strings(ids)
			for _, id := range ids {
				var encoded string
				if err := tx.QueryRowContext(ctx, "SELECT evidence_json FROM platform_evidence WHERE id = ? AND run_id = ?", id, runID).Scan(&encoded); err != nil {
					return conflict("evidence %s is not recorded for this run", id)
				}
				var item platformEvidence
				if err := json.Unmarshal([]byte(encoded), &item); err != nil {
					return err
				}
				if item.Admission != "current" || item.Outcome != "passed" || item.Producer == item.Verifier || item.PlanRevisionID != revision.ID ||
					(revision.SourceRevision != "" && landing == nil && item.SourceRevision != revision.SourceRevision) ||
					(!item.ExpiresAt.IsZero() && !now.Before(item.ExpiresAt)) {
					return conflict("evidence %s is not admissible independent verification", id)
				}
			}
			if landing != nil {
				w, err := scanWork(tx.QueryRowContext(ctx, "SELECT "+workColumns+" FROM platform_work WHERE id = ?", run.WorkItemID))
				if err != nil {
					return err
				}
				criteria := sha256.Sum256([]byte(w.Item.AcceptanceCriteria))
				if hex.EncodeToString(criteria[:]) != run.AcceptanceCriteriaDigest || landing.Commit == "" {
					return conflict("landing needs the authorized acceptance criteria and a commit")
				}
				encoded, _ := json.Marshal(landing)
				if _, err := tx.ExecContext(ctx, "UPDATE platform_runs SET landing_json = ? WHERE id = ?", string(encoded), runID); err != nil {
					return err
				}
			}
			encodedIDs, _ := json.Marshal(ids)
			_, err := tx.ExecContext(ctx, "UPDATE platform_runs SET result_summary = ?, evidence_ids_json = ? WHERE id = ?", summary, string(encodedIDs), runID)
			return err
		})
}

// --- commands ------------------------------------------------------------

// RequestCancel asks Marina to stop a Run. A Run not yet claimed is cancelled
// at once; an active run gets a cancel command for the workflow to apply.
func (p *Platform) RequestCancel(ctx context.Context, workID, requestedBy, reason string) error {
	tx, err := p.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	w, err := scanWork(tx.QueryRowContext(ctx, "SELECT "+workColumns+" FROM platform_work WHERE id = ?", workID))
	if err != nil {
		return err
	}
	if w.CancelRequested {
		return nil
	}
	now := p.store.now()
	if _, err := tx.ExecContext(ctx, "UPDATE platform_work SET cancel_requested = 1, updated_at = ? WHERE id = ?", millis(now), workID); err != nil {
		return err
	}
	if w.Status == workReady {
		if err := setWorkStatus(ctx, tx, workID, "cancelled", now); err != nil {
			return err
		}
	} else if run, err := scanRun(tx.QueryRowContext(ctx, "SELECT "+runColumns+" FROM platform_runs WHERE work_id = ? AND status IN "+runActiveStatusList+" ORDER BY started_at DESC LIMIT 1", workID)); err == nil {
		id, err := newID("pcmd")
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO platform_commands (id, run_id, sequence, type, status, reason, requested_by, run_version, fence_token, created_at, updated_at)
			VALUES (?, ?, (SELECT COALESCE(MAX(sequence), 0) + 1 FROM platform_commands WHERE run_id = ?), 'cancel', 'pending', ?, ?, ?, ?, ?, ?)`,
			id, run.ID, run.ID, reason, requestedBy, run.Version, run.FenceToken, millis(now), millis(now)); err != nil {
			return err
		}
	} else if !isPlatformNotFound(err) {
		return err
	}
	if err := p.appendEvent(ctx, tx, workID, "run.cancel_requested", "Cancellation requested", map[string]any{"by": requestedBy}, ""); err != nil {
		return err
	}
	return tx.Commit()
}

func isPlatformNotFound(err error) bool {
	var platform *PlatformError
	return errors.As(err, &platform) && platform.Kind == "not_found"
}

func (p *Platform) ListRunCommands(ctx context.Context, runID string, after int64, limit int) ([]workcontrol.Command, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	rows, err := p.store.db.QueryContext(ctx, `SELECT id, run_id, sequence, type, status, reason, requested_by, result, run_version, fence_token, created_at, updated_at
		FROM platform_commands WHERE run_id = ? AND sequence > ? ORDER BY sequence LIMIT ?`, runID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []workcontrol.Command
	for rows.Next() {
		var c workcontrol.Command
		var created, updated int64
		if err := rows.Scan(&c.ID, &c.RunID, &c.Sequence, &c.Type, &c.Status, &c.Reason, &c.RequestedBy, &c.Result, &c.RunVersion, &c.FenceToken, &created, &updated); err != nil {
			return nil, err
		}
		c.CreatedAt, c.UpdatedAt = fromMillis(created), fromMillis(updated)
		out = append(out, c)
	}
	return out, rows.Err()
}

// AcknowledgeRunCommand records the workflow's result. An applied cancel ends
// the run as cancelled.
func (p *Platform) AcknowledgeRunCommand(ctx context.Context, runID, commandID string, ack workcontrol.CommandAcknowledgement) error {
	return p.mutation(ctx, ack.IdempotencyKey, "ack", commandID, ack, nil, func(tx *sql.Tx) (any, error) {
		run, err := runByID(ctx, tx, runID)
		if err != nil {
			return nil, err
		}
		if run.AgentID != ack.AgentID || run.FenceToken != ack.FenceToken {
			return nil, conflict("command acknowledgement has a stale fence")
		}
		var commandType, status string
		if err := tx.QueryRowContext(ctx, "SELECT type, status FROM platform_commands WHERE id = ? AND run_id = ?", commandID, runID).Scan(&commandType, &status); err != nil {
			return nil, notFound("command")
		}
		if status == "completed" || status == "rejected" {
			return nil, conflict("command %s is already %s", commandID, status)
		}
		if ack.Status != "acknowledged" && ack.Status != "rejected" && ack.Status != "completed" {
			return nil, invalid("acknowledgement status must be acknowledged, rejected or completed")
		}
		now := p.store.now()
		if _, err := tx.ExecContext(ctx, "UPDATE platform_commands SET status = ?, result = ?, updated_at = ? WHERE id = ?", ack.Status, ack.Result, millis(now), commandID); err != nil {
			return nil, err
		}
		if commandType == "cancel" && ack.Status == "completed" && run.Status.Active() {
			if _, err := tx.ExecContext(ctx, "UPDATE platform_runs SET status = 'cancelled', finished_at = ?, lease_expires_at = NULL, version = version + 1, updated_at = ? WHERE id = ?",
				millis(now), millis(now), runID); err != nil {
				return nil, err
			}
			if err := setWorkStatus(ctx, tx, run.WorkItemID, "cancelled", now); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE platform_decisions SET status = 'cancelled', updated_at = ? WHERE run_id = ? AND status = 'pending'", millis(now), runID); err != nil {
				return nil, err
			}
			if err := p.appendEvent(ctx, tx, run.WorkItemID, "run.cancelled", "Run cancelled", nil, ""); err != nil {
				return nil, err
			}
		}
		return struct{}{}, nil
	})
}

// --- context -------------------------------------------------------------

// GetRunContext returns the context the client froze into the Run.
func (p *Platform) GetRunContext(ctx context.Context, runID string) (workcontrol.ResolvedContext, error) {
	run, err := runByID(ctx, p.store.db, runID)
	if err != nil {
		return workcontrol.ResolvedContext{}, err
	}
	var frozen string
	if err := p.store.db.QueryRowContext(ctx, "SELECT context_json FROM platform_work WHERE id = ?", run.WorkItemID).Scan(&frozen); err != nil {
		return workcontrol.ResolvedContext{}, err
	}
	var resolved workcontrol.ResolvedContext
	if err := json.Unmarshal([]byte(frozen), &resolved); err != nil {
		return workcontrol.ResolvedContext{}, err
	}
	if resolved.Sources == nil {
		resolved.Sources = []workcontrol.ContextSource{}
	}
	if resolved.Messages == nil {
		resolved.Messages = []workcontrol.ContextMessage{}
	}
	return resolved, nil
}
