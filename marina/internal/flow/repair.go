package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

// The public budget counts runtime starts, including the independent verifier.
// With review, two full repairs require 3 + 4 + 4 starts. Frozen task budgets
// remain authoritative and can stop work earlier; the failure limit is separate.
const RepositoryRuntimeLimit = 11
const repositoryFailureLimit = 3

// A failure and the proposal for its next attempt outlive compute. The task,
// original base, context package and execution budget remain unchanged.
type repairFailure struct {
	Number         int       `json:"number"`
	Stage          string    `json:"stage"`
	SourceRevision string    `json:"source_revision"`
	ArtifactDigest string    `json:"artifact_digest"`
	Message        string    `json:"message"`
	Approach       string    `json:"approach"`
	Diagnosis      string    `json:"diagnosis,omitempty"`
	Implementer    string    `json:"implementer"`
	Verifier       string    `json:"verifier"`
	Reviewer       string    `json:"reviewer,omitempty"`
	EvidenceIDs    []string  `json:"evidence_ids,omitempty"`
	NextApproach   string    `json:"next_approach,omitempty"`
	PlanDigest     string    `json:"plan_digest,omitempty"`
	ObservedAt     time.Time `json:"observed_at"`
}

func (c *Controller) failedAttempt(ctx context.Context, e execution.Execution, s state, stage, digest, message string) error {
	approach := "Initial implementation of the approved plan"
	if s.ImplementationDigest != "" {
		base := s.baseRevision()
		if len(s.Failures) > 0 {
			base = s.Failures[len(s.Failures)-1].SourceRevision
		}
		if s.ImplementationBase != "" {
			base = s.ImplementationBase
		}
		result, err := c.checkedResult(s.ImplementationDigest, workresult.Expected{Stage: "implement", SourceRevision: base, AllowedPaths: c.Config.AllowedPaths})
		if err != nil {
			return err
		}
		if strings.TrimSpace(result.Summary) != "" {
			approach = result.Summary
		}
	}
	if s.RepairPlanDigest != "" {
		plan, err := c.savedRepairPlan(s)
		if err != nil {
			return err
		}
		approach = plan.Approach
	}
	s.Failures = append(s.Failures, repairFailure{Number: len(s.Failures) + 1, Stage: stage, SourceRevision: s.Head,
		ArtifactDigest: digest, Message: message, Approach: approach, Implementer: s.Implementer, Verifier: s.Verifier, Reviewer: s.Reviewer, ObservedAt: time.Now().UTC()})
	if s.ReviewEvidenceID != "" {
		s.Failures[len(s.Failures)-1].EvidenceIDs = []string{s.ReviewEvidenceID}
	}
	s.Step = "repair"
	return c.save(ctx, e, s)
}

func (c *Controller) failedCI(ctx context.Context, e execution.Execution, s state, checks []sourcecontrol.CheckObservation, reason string) error {
	data, err := json.Marshal(struct {
		SourceRevision string
		Checks         []sourcecontrol.CheckObservation
	}{s.Head, checks})
	if err != nil {
		return err
	}
	digest, err := c.Artifacts.Put(data)
	if err != nil {
		return err
	}
	return c.failedAttempt(ctx, e, s, "ci", digest, reason)
}

func operationKey(e execution.Execution, s state, stage string) string {
	prefix := e.ID
	if len(s.Rebases) > 0 {
		prefix += fmt.Sprintf(":rebase:%d", len(s.Rebases))
	}
	round := len(s.Failures)
	if s.Step == "repair" {
		round-- // Evidence still belongs to the attempt that just failed.
	}
	if round <= 0 {
		return prefix + ":" + stage
	}
	return fmt.Sprintf("%s:repair:%d:%s", prefix, round, stage)
}

func normalizeApproach(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func (c *Controller) savedRepairPlan(s state) (*workresult.RepairPlan, error) {
	if len(s.Failures) == 0 || s.RepairPlanDigest == "" {
		return nil, errors.New("repair diagnosis is missing")
	}
	failure := s.Failures[len(s.Failures)-1]
	result, err := c.checkedResult(s.RepairPlanDigest, workresult.Expected{Stage: "repair_plan", SourceRevision: failure.SourceRevision})
	if err != nil {
		return nil, err
	}
	plan := result.Repair
	if !plan.CanRepair || plan.Disputed || plan.FailureDigest != failure.ArtifactDigest {
		return nil, errors.New("repair proposal does not match the failure")
	}
	for _, earlier := range s.Failures {
		if normalizeApproach(plan.Approach) == normalizeApproach(earlier.Approach) {
			return nil, errors.New("repair proposal repeats a failed approach")
		}
	}
	return plan, nil
}

func (c *Controller) repairFeedback(s state) (string, error) {
	if len(s.Failures) == 0 {
		return "", errors.New("repair has no failure evidence")
	}
	latest := s.Failures[len(s.Failures)-1]
	data, err := c.Artifacts.Get(latest.ArtifactDigest)
	if err != nil {
		return "", err
	}
	// This is a labelled excerpt of untrusted evidence, not another result or
	// context source. Its original digest and bytes stay in the artifact store.
	if len(data) > 16<<10 {
		data = data[:16<<10]
	}
	encoded, err := json.Marshal(struct {
		Failures        []repairFailure `json:"failures"`
		EvidenceExcerpt string          `json:"evidence_excerpt"`
	}{s.Failures, string(data)})
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func (c *Controller) repairBudget(ctx context.Context, e execution.Execution, s state, startsNeeded int) (string, error) {
	e, err := c.Journal.GetExecution(ctx, e.ID)
	if err != nil {
		return "", err
	}
	if limit := c.Config.failureLimit(); len(s.Failures) >= limit {
		return fmt.Sprintf("The %d-failure limit was reached", limit), nil
	}
	if e.BudgetExpired(time.Now()) || !time.Now().Add(time.Second).Before(e.DeadlineAt) {
		return "The original time, token or cost budget is exhausted", nil
	}
	items, err := c.Journal.ListRuntimeAttempts(ctx, e.ID)
	if err != nil {
		return "", err
	}
	if e.MaxAttempts-len(items) < startsNeeded {
		return "The remaining runtime-attempt budget cannot cover diagnosis, implementation, independent review and fresh verification", nil
	}
	totals, err := c.Journal.ExecutionModelTotals(ctx, e.ID)
	if err != nil {
		return "", err
	}
	if totals.PendingCalls != 0 {
		return "Model usage is uncertain; existing reservations are retained and no repair is admitted", nil
	}
	minimum := c.Config.Model.MaxOutputTokens + 1024
	if e.MaxTokens > 0 && e.MaxTokens-e.UsedTokens < minimum {
		return "The remaining token budget cannot admit another model call", nil
	}
	if e.MaxCostMicros > 0 && e.MaxCostMicros-e.UsedCostMicros <= 0 {
		return "The remaining cost budget cannot admit another model call", nil
	}
	return "", nil
}

func (c *Controller) repairBlock(ctx context.Context, e execution.Execution, r workcontrol.Run, s state, reason string) error {
	var history strings.Builder
	history.WriteString(reason)
	for _, failure := range s.Failures {
		fmt.Fprintf(&history, "\nAttempt %d (%s, commit %s): %s. Approach: %s. Evidence: %s.",
			failure.Number, failure.Stage, failure.SourceRevision, failure.Message, failure.Approach, c.artifactURL(failure.ArtifactDigest))
		if failure.Diagnosis != "" {
			fmt.Fprintf(&history, " Diagnosis: %s.", failure.Diagnosis)
		}
	}
	history.WriteString("\nNarrow the scope, explicitly raise the budget, fix the issue manually, or cancel. A new task is required to change the frozen intent.")
	s.Reason = history.String()
	// Persist the exact blocker before its external publication.
	if err := c.save(ctx, e, s); err != nil {
		return err
	}
	return c.block(ctx, e, r, s.Reason)
}

func (c *Controller) advanceRepair(ctx context.Context, e execution.Execution, r workcontrol.Run, task workcontrol.WorkItem, s state) error {
	if len(s.Failures) == 0 {
		return c.block(ctx, e, r, "Repair state has no recorded failure")
	}
	if s.Step == "repair" {
		failure := &s.Failures[len(s.Failures)-1]
		if failure.Stage == "ci" && !slices.Contains(failure.EvidenceIDs, operationKey(e, s, "ci-failure")) {
			id := operationKey(e, s, "ci-failure")
			key := strings.TrimPrefix(id, e.ID+":")
			_, err := c.Provider.RecordEvidence(ctx, r.ID, guard(e, r, key), workcontrol.EvidenceSubmission{ID: id, PlanRevisionID: s.PlanID,
				Type: "ci", Producer: s.Implementer, Verifier: "github-ci", SubjectDigest: failure.SourceRevision, SourceRevision: failure.SourceRevision,
				Command: "required GitHub CI", Outcome: "failed", ProvenanceURI: c.artifactURL(failure.ArtifactDigest), ProvenanceDigest: failure.ArtifactDigest,
				ObservedAt: failure.ObservedAt, ExpiresAt: failure.ObservedAt.Add(24 * time.Hour), ArtifactURI: c.artifactURL(failure.ArtifactDigest),
				ArtifactDigest: failure.ArtifactDigest, PolicyVersion: c.Config.PolicyVersion})
			if err != nil {
				return err
			}
			e, err = c.Journal.GetExecution(ctx, e.ID)
			if err != nil {
				return err
			}
			_, _, _, err = c.Journal.AddEvidence(ctx, e.ID, e.Version, execution.EvidenceInput{Key: id, Kind: "ci", SourceRevision: failure.SourceRevision,
				Environment: "GitHub CI", Command: "required GitHub CI", Summary: "failed", URI: c.artifactURL(failure.ArtifactDigest), ObservedAt: failure.ObservedAt})
			if err != nil {
				return err
			}
			failure.EvidenceIDs = append(failure.EvidenceIDs, id)
			return c.save(ctx, e, s)
		}
		if s.VerificationDigest != "" && len(s.EvidenceIDs) < len(c.Config.Checks) {
			return c.evidence(ctx, e, r, s)
		}
		if reason, err := c.repairBudget(ctx, e, s, 4); err != nil {
			return err
		} else if reason != "" {
			return c.repairBlock(ctx, e, r, s, reason)
		}
		failure.EvidenceIDs = append(failure.EvidenceIDs, s.EvidenceIDs...)
		s.RepairPlanDigest = ""
		s.EngineStatus, s.QuestionID, s.QuestionDecisionID, s.QuestionRunVersion = "", "", "", 0
		s.Step = "repair_plan"
		return c.save(ctx, e, s)
	}
	// Repair never rewrites a ref. Main movement during landing has its own
	// recorded rebase attempt and requires fresh review and verification.
	source, err := c.Source(e)
	if err != nil {
		return err
	}
	head := sourcecontrol.Revision{Commit: s.Head}
	if s.Head != s.baseRevision() {
		head, err = source.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: c.Config.Git.Repository, Ref: s.Branch})
		if err != nil {
			return err
		}
	}
	base, err := source.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: c.Config.Git.Repository, Ref: c.Config.Git.BaseBranch})
	if err != nil {
		return err
	}
	if head.Commit != s.Head || base.Commit != s.baseRevision() {
		return c.repairBlock(ctx, e, r, s, "Repository refs changed before repair; earlier evidence cannot authorize new work")
	}
	attempt, done, result, digest, err := c.worker(ctx, e, task, s, "repair_plan")
	if errors.Is(err, execution.ErrBudgetExhausted) || errors.Is(err, errInvalidWorkerResult) || errors.Is(err, errEngineQuestionStopped) {
		return c.repairBlock(ctx, e, r, s, "Repair diagnosis could not be admitted: "+err.Error())
	}
	if err != nil || !done {
		return err
	}
	if attempt.State != rt.StateSucceeded || result.Repair == nil || !result.Repair.CanRepair || result.Repair.Disputed {
		reason := "No new, evidence-based repair approach was found"
		if result.Repair != nil {
			reason += ": " + result.Repair.Diagnosis
		}
		return c.repairBlock(ctx, e, r, s, reason)
	}
	s.RepairPlanDigest = digest
	plan, err := c.savedRepairPlan(s)
	if err != nil {
		return c.repairBlock(ctx, e, r, s, err.Error())
	}
	if reason, err := c.repairBudget(ctx, e, s, 3); err != nil {
		return err
	} else if reason != "" {
		return c.repairBlock(ctx, e, r, s, reason)
	}
	failure := &s.Failures[len(s.Failures)-1]
	failure.Diagnosis, failure.NextApproach, failure.PlanDigest = plan.Diagnosis, plan.Approach, digest
	s.Implementer, s.Verifier, s.ImplementationDigest, s.VerificationDigest = "", "", "", ""
	s.EvidenceIDs = nil
	s.Reviewer, s.ReviewDigest, s.ReviewEvidenceID, s.CandidateDigest = "", "", "", ""
	s.EngineStatus, s.QuestionID, s.QuestionDecisionID, s.QuestionRunVersion = "", "", "", 0
	s.Step = "implement"
	return c.save(ctx, e, s)
}
