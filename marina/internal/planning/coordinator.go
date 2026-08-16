package planning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/policy"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

var (
	ErrPolicyDenied     = errors.New("plan submission denied by policy")
	ErrDecisionMismatch = errors.New("decision does not belong to the expected plan")
)

type Provider interface {
	workcontrol.VersionedPlanning
	workcontrol.DecisionInspection
}

type Coordinator struct {
	journal  execution.Journal
	provider Provider
	clock    func() time.Time
}

func New(journal execution.Journal, provider Provider, clock func() time.Time) *Coordinator {
	if clock == nil {
		clock = time.Now
	}
	return &Coordinator{journal: journal, provider: provider, clock: clock}
}

type SubmitInput struct {
	ExecutionID   string
	RunID         string
	Policy        policy.Policy
	PolicyRequest policy.Request
	Plan          workcontrol.PlanRevisionSubmission
}

func (coordinator *Coordinator) Submit(ctx context.Context, input SubmitInput) (workcontrol.PlanRevisionResult, error) {
	if coordinator == nil || coordinator.journal == nil || coordinator.provider == nil {
		return workcontrol.PlanRevisionResult{}, errors.New("planning coordinator is not configured")
	}
	input.ExecutionID = strings.TrimSpace(input.ExecutionID)
	input.RunID = strings.TrimSpace(input.RunID)
	if input.ExecutionID == "" || input.RunID == "" || strings.TrimSpace(input.Plan.IdempotencyKey) == "" {
		return workcontrol.PlanRevisionResult{}, errors.New("execution, run, and stable plan idempotency key are required")
	}
	if input.Plan.PolicyVersion != input.Policy.Version || input.Plan.RiskClass != string(input.Policy.RiskClass) ||
		input.PolicyRequest.PolicyVersion != input.Policy.Version {
		return workcontrol.PlanRevisionResult{}, errors.New("plan and policy request must bind the exact policy version and risk class")
	}
	input.PolicyRequest.Action = policy.ActionExternalMutation
	decision := policy.Evaluate(coordinator.clock().UTC(), input.Policy, input.PolicyRequest)
	if decision.Verdict != policy.VerdictAllow {
		return workcontrol.PlanRevisionResult{}, fmt.Errorf("%s: %w", decision.Reason, ErrPolicyDenied)
	}

	record, err := coordinator.journal.GetExecution(ctx, input.ExecutionID)
	if err != nil {
		return workcontrol.PlanRevisionResult{}, err
	}
	if record.ProjectID != input.Policy.ProjectID || record.ExternalRunID != input.RunID ||
		record.AgentID != input.Plan.AgentID || record.ExternalRunVersion != input.Plan.ExpectedVersion ||
		record.FenceToken != input.Plan.FenceToken {
		return workcontrol.PlanRevisionResult{}, errors.New("plan submission does not match the journaled provider authority")
	}
	record, _, _, err = coordinator.journal.SaveCheckpoint(ctx, input.ExecutionID, record.Version, execution.CheckpointInput{
		Key: "plan-intent:" + input.Plan.IdempotencyKey, Phase: "plan_submission_intent",
		SourceRevision: input.Plan.SourceRevision,
		Payload: map[string]any{
			"run_id": input.RunID, "context_digest": input.Plan.ContextDigest,
			"policy_version": input.Plan.PolicyVersion, "idempotency_key": input.Plan.IdempotencyKey,
		},
	})
	if err != nil {
		return workcontrol.PlanRevisionResult{}, fmt.Errorf("persist plan submission intent: %w", err)
	}

	result, err := coordinator.provider.SubmitPlanRevision(ctx, input.RunID, input.Plan)
	if err != nil {
		return workcontrol.PlanRevisionResult{}, err
	}
	if strings.TrimSpace(result.PlanRevision.ID) == "" {
		return workcontrol.PlanRevisionResult{}, errors.New("provider returned an empty plan revision ID")
	}
	if result.PlanRevision.RunID != input.RunID || result.PlanRevision.Plan != strings.TrimSpace(input.Plan.Plan) ||
		result.PlanRevision.ContextDigest != input.Plan.ContextDigest || result.PlanRevision.RiskClass != input.Plan.RiskClass ||
		result.PlanRevision.SourceRevision != input.Plan.SourceRevision || result.PlanRevision.PolicyVersion != input.Plan.PolicyVersion {
		return workcontrol.PlanRevisionResult{}, errors.New("provider returned a plan revision with mismatched immutable bindings")
	}
	if result.Decision != nil && (result.Decision.RunID != input.RunID || result.Decision.PlanRevisionID != result.PlanRevision.ID) {
		return workcontrol.PlanRevisionResult{}, errors.New("provider returned a decision for a different run or plan revision")
	}
	payload := map[string]any{
		"plan_revision_id": result.PlanRevision.ID,
		"revision":         result.PlanRevision.Revision,
	}
	if result.Decision != nil {
		payload["decision_id"] = result.Decision.ID
		payload["decision_status"] = string(result.Decision.Status)
	}
	_, _, _, err = coordinator.journal.SaveCheckpoint(ctx, input.ExecutionID, record.Version, execution.CheckpointInput{
		Key: "plan-result:" + result.PlanRevision.ID, Phase: "plan_submitted",
		SourceRevision: input.Plan.SourceRevision, Payload: payload,
	})
	if err != nil {
		return workcontrol.PlanRevisionResult{}, fmt.Errorf("persist plan submission result: %w", err)
	}
	if input.Plan.RequireApproval && result.Decision == nil {
		return workcontrol.PlanRevisionResult{}, errors.New("provider omitted a required decision")
	}
	return result, nil
}

type Action string

const (
	ActionWait         Action = "wait"
	ActionStartRuntime Action = "start_runtime"
	ActionReplan       Action = "replan"
	ActionStop         Action = "stop"
)

func Decide(now time.Time, decision workcontrol.Decision) Action {
	if decision.Status == workcontrol.DecisionPending && decision.ExpiresAt != nil && !now.Before(*decision.ExpiresAt) {
		return ActionReplan
	}
	switch decision.Status {
	case workcontrol.DecisionPending:
		return ActionWait
	case workcontrol.DecisionAnswered:
		switch decision.Answer {
		case "Approve plan":
			return ActionStartRuntime
		case "Request changes":
			return ActionReplan
		default:
			return ActionStop
		}
	case workcontrol.DecisionExpired, workcontrol.DecisionSuperseded:
		return ActionReplan
	default:
		return ActionStop
	}
}

func (coordinator *Coordinator) ReconcileDecision(
	ctx context.Context,
	executionID string,
	decisionID string,
	planRevisionID string,
) (Action, error) {
	record, err := coordinator.journal.GetExecution(ctx, strings.TrimSpace(executionID))
	if err != nil {
		return "", err
	}
	decision, err := coordinator.provider.GetDecision(ctx, strings.TrimSpace(decisionID))
	if err != nil {
		return "", err
	}
	if decision.ID != decisionID || decision.PlanRevisionID != planRevisionID {
		return "", ErrDecisionMismatch
	}
	action := Decide(coordinator.clock().UTC(), decision)
	identity := sha256.Sum256([]byte(string(decision.Status) + "\x00" + decision.Answer))
	phase := "decision_" + string(decision.Status)
	_, _, _, err = coordinator.journal.SaveCheckpoint(ctx, record.ID, record.Version, execution.CheckpointInput{
		Key: "decision:" + decision.ID + ":" + hex.EncodeToString(identity[:8]), Phase: phase,
		Payload: map[string]any{
			"decision_id": decision.ID, "plan_revision_id": decision.PlanRevisionID,
			"status": string(decision.Status), "answer": decision.Answer, "action": string(action),
		},
	})
	if err != nil {
		return "", fmt.Errorf("persist decision checkpoint: %w", err)
	}
	return action, nil
}
