package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

type planSubmissionResult struct {
	Revision domain.PlanRevision `json:"revision"`
	Decision *domain.Decision    `json:"decision,omitempty"`
}

func (s *Store) SubmitPlanRevision(
	ctx context.Context,
	mutation Mutation,
	runID string,
	input domain.PlanRevisionInput,
	request domain.DecisionRequest,
) (domain.PlanRevision, *domain.Decision, error) {
	input.Plan = strings.TrimSpace(input.Plan)
	input.ContextDigest = strings.TrimSpace(input.ContextDigest)
	input.RiskClass = strings.ToLower(strings.TrimSpace(input.RiskClass))
	input.VerificationStrategy = strings.TrimSpace(input.VerificationStrategy)
	input.RollbackApproach = strings.TrimSpace(input.RollbackApproach)
	input.SourceRevision = strings.TrimSpace(input.SourceRevision)
	input.PolicyVersion = strings.TrimSpace(input.PolicyVersion)
	if input.ExpectedSideEffects == nil {
		input.ExpectedSideEffects = []string{}
	}
	if input.Plan == "" || input.ContextDigest == "" || input.VerificationStrategy == "" || input.RollbackApproach == "" {
		return domain.PlanRevision{}, nil, fmt.Errorf("plan, context digest, verification strategy, and rollback approach are required: %w", domain.ErrInvalidInput)
	}
	if !validRiskClass(input.RiskClass) {
		return domain.PlanRevision{}, nil, fmt.Errorf("invalid plan risk class %q: %w", input.RiskClass, domain.ErrInvalidInput)
	}
	request.Risk = strings.ToLower(strings.TrimSpace(request.Risk))
	if request.Risk == "" {
		request.Risk = input.RiskClass
	}
	if !validRiskClass(request.Risk) {
		return domain.PlanRevision{}, nil, fmt.Errorf("invalid decision risk class %q: %w", request.Risk, domain.ErrInvalidInput)
	}
	request.Currency = strings.ToUpper(strings.TrimSpace(request.Currency))
	if request.Currency == "" {
		request.Currency = "USD"
	}
	if len(request.Currency) != 3 || request.EstimatedCostMinor < 0 {
		return domain.PlanRevision{}, nil, fmt.Errorf("invalid decision cost: %w", domain.ErrInvalidInput)
	}

	projectID, err := s.ProjectIDForRun(ctx, runID)
	if err != nil {
		return domain.PlanRevision{}, nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.PlanRevision{}, nil, fmt.Errorf("begin submit plan revision: %w", err)
	}
	defer tx.Rollback()
	attempt, replayResult, replayed, err := beginIdempotentMutation(
		ctx, tx, mutation, projectID, operationSubmitPlan, runID,
		struct {
			RunID   string                   `json:"run_id"`
			Input   domain.PlanRevisionInput `json:"input"`
			Request domain.DecisionRequest   `json:"request"`
			Guard   *RunGuard                `json:"guard,omitempty"`
		}{runID, input, request, mutation.Guard},
	)
	if err != nil {
		return domain.PlanRevision{}, nil, err
	}
	if replayed {
		var original planSubmissionResult
		if err := decodeIdempotencyResult(replayResult, &original); err != nil {
			return domain.PlanRevision{}, nil, err
		}
		return original.Revision, original.Decision, nil
	}
	run, err := getRunTx(ctx, tx, runID)
	if err != nil {
		return domain.PlanRevision{}, nil, err
	}
	if err := validateRunGuard(run, mutation.Guard); err != nil {
		return domain.PlanRevision{}, nil, err
	}
	if run.Status != domain.RunPlanning {
		return domain.PlanRevision{}, nil, fmt.Errorf("run is no longer planning: %w", domain.ErrConflict)
	}
	task, err := getTaskTx(ctx, tx, run.TaskID)
	if err != nil {
		return domain.PlanRevision{}, nil, err
	}
	var revisionNumber int
	if err := tx.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(revision), 0) + 1 FROM plan_revisions WHERE run_id = ?", runID,
	).Scan(&revisionNumber); err != nil {
		return domain.PlanRevision{}, nil, fmt.Errorf("number plan revision: %w", err)
	}
	now := nowMillis()
	revision := domain.PlanRevision{
		ID: newID("pln"), RunID: runID, Revision: revisionNumber,
		Plan: input.Plan, ContextDigest: input.ContextDigest, RiskClass: input.RiskClass,
		VerificationStrategy: input.VerificationStrategy,
		ExpectedSideEffects:  input.ExpectedSideEffects, RollbackApproach: input.RollbackApproach,
		SourceRevision: input.SourceRevision, PolicyVersion: input.PolicyVersion, CreatedAt: now,
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO plan_revisions (
			id, run_id, revision, plan, context_digest, risk_class,
			verification_strategy, expected_side_effects_json, rollback_approach,
			source_revision, policy_version, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, revision.ID, revision.RunID, revision.Revision, revision.Plan, revision.ContextDigest,
		revision.RiskClass, revision.VerificationStrategy, marshalJSON(revision.ExpectedSideEffects),
		revision.RollbackApproach, revision.SourceRevision, revision.PolicyVersion, revision.CreatedAt,
	); err != nil {
		return domain.PlanRevision{}, nil, fmt.Errorf("insert plan revision: %w", err)
	}

	nextRunStatus, nextTaskStatus := domain.RunRunning, domain.TaskRunning
	if request.Required {
		nextRunStatus, nextTaskStatus = domain.RunWaitingDecision, domain.TaskWaitingDecision
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE runs
		SET plan = ?, active_plan_revision_id = ?, status = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND status = 'planning' AND version = ?
	`, revision.Plan, revision.ID, nextRunStatus, now, run.ID, run.Version)
	if err != nil {
		return domain.PlanRevision{}, nil, fmt.Errorf("activate plan revision: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return domain.PlanRevision{}, nil, fmt.Errorf("run changed while submitting plan: %w", domain.ErrConflict)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE tasks SET status = ?, version = version + 1, updated_at = ? WHERE id = ?",
		nextTaskStatus, now, run.TaskID,
	); err != nil {
		return domain.PlanRevision{}, nil, fmt.Errorf("update task for plan: %w", err)
	}
	if request.Required {
		if _, err := tx.ExecContext(ctx, "UPDATE agents SET status = 'waiting', last_seen_at = ? WHERE id = ?", now, run.AgentID); err != nil {
			return domain.PlanRevision{}, nil, fmt.Errorf("mark agent waiting: %w", err)
		}
	}
	planEvent, err := insertEventTx(ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
		"plan.revision.created", fmt.Sprintf("Plan revision %d submitted", revision.Revision),
		map[string]any{"plan_revision_id": revision.ID, "revision": revision.Revision, "risk_class": revision.RiskClass, "requires_approval": request.Required},
	)
	if err != nil {
		return domain.PlanRevision{}, nil, fmt.Errorf("create plan revision event: %w", err)
	}

	var decision *domain.Decision
	var decisionEvent domain.Event
	if request.Required {
		decisionID := request.ID
		if decisionID == "" {
			decisionID = newID("dec")
		}
		created := domain.Decision{
			ID: decisionID, TaskID: run.TaskID, RunID: &run.ID, PlanRevisionID: revision.ID,
			Question: fmt.Sprintf("Approve plan revision %d for “%s”?", revision.Revision, task.Title),
			Options:  []string{"Approve plan", "Request changes"}, Recommendation: "Approve plan",
			RiskClass: request.Risk, EvidenceSummary: strings.TrimSpace(request.EvidenceSummary),
			EstimatedCostMinor: request.EstimatedCostMinor, Currency: request.Currency,
			WaitingConsequence:   strings.TrimSpace(request.WaitingConsequence),
			RejectingConsequence: strings.TrimSpace(request.RejectingConsequence),
			Status:               "pending", ExpiresAt: request.ExpiresAt, CreatedAt: now,
		}
		if created.WaitingConsequence == "" {
			created.WaitingConsequence = "Execution remains paused until this decision is answered or expires."
		}
		if created.RejectingConsequence == "" {
			created.RejectingConsequence = "The run returns to planning and requires a new immutable plan revision."
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO decisions (
				id, task_id, run_id, plan_revision_id, question, options_json, recommendation,
				risk_class, evidence_summary, estimated_cost_minor, currency,
				waiting_consequence, rejecting_consequence, status, answer, expires_at,
				created_at, answered_by
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', '', ?, ?, '')
		`, created.ID, created.TaskID, created.RunID, created.PlanRevisionID, created.Question,
			marshalJSON(created.Options), created.Recommendation, created.RiskClass,
			created.EvidenceSummary, created.EstimatedCostMinor, created.Currency,
			created.WaitingConsequence, created.RejectingConsequence,
			nullableInt64(created.ExpiresAt), created.CreatedAt,
		); err != nil {
			return domain.PlanRevision{}, nil, fmt.Errorf("create plan decision: %w", err)
		}
		decisionEvent, err = insertEventTx(ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
			"decision.requested", "Plan revision is waiting for a focused decision",
			map[string]any{"decision_id": created.ID, "plan_revision_id": revision.ID, "expires_at": created.ExpiresAt},
		)
		if err != nil {
			return domain.PlanRevision{}, nil, fmt.Errorf("create decision event: %w", err)
		}
		decision = &created
	}
	saved := planSubmissionResult{Revision: revision, Decision: decision}
	if err := finishIdempotentMutation(ctx, tx, attempt, saved); err != nil {
		return domain.PlanRevision{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return domain.PlanRevision{}, nil, fmt.Errorf("commit plan revision: %w", err)
	}
	s.publish(planEvent)
	if decision != nil {
		s.publish(decisionEvent)
	}
	return revision, decision, nil
}

func validRiskClass(value string) bool {
	switch value {
	case "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func scanPlanRevision(scanner interface{ Scan(...any) error }) (domain.PlanRevision, error) {
	var revision domain.PlanRevision
	var sideEffects string
	if err := scanner.Scan(
		&revision.ID, &revision.RunID, &revision.Revision, &revision.Plan,
		&revision.ContextDigest, &revision.RiskClass, &revision.VerificationStrategy,
		&sideEffects, &revision.RollbackApproach, &revision.SourceRevision,
		&revision.PolicyVersion, &revision.CreatedAt,
	); err != nil {
		return domain.PlanRevision{}, err
	}
	revision.ExpectedSideEffects = unmarshalStrings(sideEffects)
	return revision, nil
}

const planRevisionColumns = `
	id, run_id, revision, plan, context_digest, risk_class, verification_strategy,
	expected_side_effects_json, rollback_approach, source_revision, policy_version, created_at
`

func (s *Store) ListPlanRevisions(ctx context.Context, runID string) ([]domain.PlanRevision, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+planRevisionColumns+" FROM plan_revisions WHERE run_id = ? ORDER BY revision", runID,
	)
	if err != nil {
		return nil, fmt.Errorf("list plan revisions: %w", err)
	}
	defer rows.Close()
	revisions := make([]domain.PlanRevision, 0)
	for rows.Next() {
		revision, scanErr := scanPlanRevision(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan plan revision: %w", scanErr)
		}
		revisions = append(revisions, revision)
	}
	return revisions, rows.Err()
}

func (s *Store) listPlanRevisionsForTask(ctx context.Context, taskID string) ([]domain.PlanRevision, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.run_id, p.revision, p.plan, p.context_digest, p.risk_class,
			p.verification_strategy, p.expected_side_effects_json, p.rollback_approach,
			p.source_revision, p.policy_version, p.created_at
		FROM plan_revisions p
		JOIN runs r ON r.id = p.run_id
		WHERE r.task_id = ?
		ORDER BY p.created_at DESC, p.revision DESC
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task plan revisions: %w", err)
	}
	defer rows.Close()
	revisions := make([]domain.PlanRevision, 0)
	for rows.Next() {
		revision, scanErr := scanPlanRevision(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan task plan revision: %w", scanErr)
		}
		revisions = append(revisions, revision)
	}
	return revisions, rows.Err()
}
