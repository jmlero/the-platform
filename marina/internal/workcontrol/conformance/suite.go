package conformance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

type Subject interface {
	workcontrol.HealthChecker
	workcontrol.ProviderInformation
	workcontrol.AgentRegistry
	workcontrol.WorkDiscovery
	workcontrol.WorkInspection
	workcontrol.Acquisition
	workcontrol.Planning
	workcontrol.RunControl
	workcontrol.EventPublisher
}

type Result struct {
	Provider workcontrol.ProviderInfo
	Agent    workcontrol.Agent
	Work     workcontrol.WorkItem
	Run      workcontrol.Run
}

var lifecycleCapabilities = []string{
	workcontrol.CapabilityProviderInfo,
	workcontrol.CapabilityAgentRegister,
	workcontrol.CapabilityWorkReadyList,
	workcontrol.CapabilityWorkInspect,
	workcontrol.CapabilityWorkClaim,
	workcontrol.CapabilityLeaseRenew,
	workcontrol.CapabilityPlanSubmit,
	workcontrol.CapabilityProgress,
	workcontrol.CapabilityRunBlock,
}

// Verify exercises one isolated provider lifecycle through Marina's public
// capability ports. The caller seeds one ready task through provider-specific
// setup and passes its returned ID; the suite never assumes fixture IDs can be
// chosen by the provider.
func Verify(
	ctx context.Context,
	subject Subject,
	fixtures Fixtures,
	seededWorkID string,
) (Result, error) {
	if subject == nil {
		return Result{}, errors.New("conformance subject is required")
	}
	if strings.TrimSpace(seededWorkID) == "" {
		return Result{}, errors.New("seeded work ID is required")
	}
	if err := subject.Health(ctx); err != nil {
		return Result{}, fmt.Errorf("provider health: %w", err)
	}
	provider, err := subject.GetProviderInfo(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("provider information: %w", err)
	}
	if err := verifyProvider(provider, fixtures.Provider); err != nil {
		return Result{}, err
	}

	agent, err := subject.RegisterAgent(ctx, workcontrol.AgentRegistration{
		ID:           fixtures.Agent.ID,
		Name:         fixtures.Agent.Name,
		Kind:         fixtures.Agent.Kind,
		Capabilities: append([]string(nil), fixtures.Agent.Capabilities...),
	})
	if err != nil {
		return Result{}, fmt.Errorf("register logical agent: %w", err)
	}
	if agent.ID != fixtures.Agent.ID || agent.Name != fixtures.Agent.Name || agent.Kind != fixtures.Agent.Kind {
		return Result{}, fmt.Errorf("registered agent differs from fixture: %#v", agent)
	}

	ready, err := subject.ListReadyWork(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("discover ready work: %w", err)
	}
	work, ok := findWork(ready, seededWorkID)
	if !ok {
		return Result{}, fmt.Errorf("seeded work %q was not discoverable", seededWorkID)
	}
	if err := verifyWork(work, fixtures.Task); err != nil {
		return Result{}, err
	}

	run, err := subject.ClaimWork(ctx, work.ID, workcontrol.Claim{
		AgentID: agent.ID,
		Lease:   2 * time.Minute,
	})
	if err != nil {
		return Result{}, fmt.Errorf("claim seeded work: %w", err)
	}
	if run.ID == "" || run.WorkItemID != work.ID || run.AgentID != agent.ID || string(run.Status) != fixtures.Run.Status {
		return Result{}, fmt.Errorf("claimed run differs from fixture semantics: %#v", run)
	}
	if run.LeaseExpiresAt == nil || !run.LeaseExpiresAt.After(time.Now().UTC()) {
		return Result{}, fmt.Errorf("claimed run has no future lease: %#v", run.LeaseExpiresAt)
	}

	detail, err := subject.GetWork(ctx, work.ID)
	if err != nil {
		return Result{}, fmt.Errorf("inspect claimed work: %w", err)
	}
	observed, ok := findRun(detail.Runs, run.ID)
	if detail.Work.ID != work.ID || !ok {
		return Result{}, fmt.Errorf("claimed run %q is not attached to work %q", run.ID, work.ID)
	}
	initialLease := observed.LeaseExpiresAt
	if initialLease == nil {
		return Result{}, errors.New("inspected run has no lease")
	}
	if err := subject.RenewRunLease(ctx, run.ID, workcontrol.LeaseRenewal{
		AgentID: agent.ID,
		Lease:   5 * time.Minute,
	}); err != nil {
		return Result{}, fmt.Errorf("renew exact run lease: %w", err)
	}
	detail, err = subject.GetWork(ctx, work.ID)
	if err != nil {
		return Result{}, fmt.Errorf("inspect renewed run: %w", err)
	}
	observed, ok = findRun(detail.Runs, run.ID)
	if !ok || observed.LeaseExpiresAt == nil || !observed.LeaseExpiresAt.After(*initialLease) {
		return Result{}, fmt.Errorf("run lease did not advance: before=%v after=%v", initialLease, observed.LeaseExpiresAt)
	}

	if err := subject.SubmitRunPlan(ctx, run.ID, workcontrol.PlanSubmission{
		Plan:            fixtures.Plan.Summary,
		RequireApproval: false,
	}); err != nil {
		return Result{}, fmt.Errorf("submit conformance plan: %w", err)
	}
	progressMessage := "The exact provider run was observed."
	if value, ok := fixtures.Event.Payload["message"].(string); ok && strings.TrimSpace(value) != "" {
		progressMessage = value
	}
	if err := subject.ReportRunProgress(ctx, run.ID, workcontrol.Progress{
		Message: progressMessage,
		Payload: map[string]any{
			"phase":          "contract_verification",
			"correlation_id": fixtures.Event.CorrelationID,
		},
	}); err != nil {
		return Result{}, fmt.Errorf("publish conformance progress: %w", err)
	}
	if err := subject.BlockRun(ctx, run.ID, "conformance lifecycle completed without runtime mutation"); err != nil {
		return Result{}, fmt.Errorf("stop conformance run: %w", err)
	}
	detail, err = subject.GetWork(ctx, work.ID)
	if err != nil {
		return Result{}, fmt.Errorf("inspect stopped run: %w", err)
	}
	observed, ok = findRun(detail.Runs, run.ID)
	if !ok || observed.Status != workcontrol.RunBlocked {
		return Result{}, fmt.Errorf("stopped run status = %q, want %q", observed.Status, workcontrol.RunBlocked)
	}
	if len(detail.Runs) != 1 {
		return Result{}, fmt.Errorf("work has %d runs after one claim, want 1", len(detail.Runs))
	}

	return Result{Provider: provider, Agent: agent, Work: work, Run: observed}, nil
}

func verifyProvider(actual workcontrol.ProviderInfo, fixture ProviderFixture) error {
	if actual.Provider != fixture.Provider || actual.ProtocolVersion != fixture.ProtocolVersion {
		return fmt.Errorf(
			"provider identity is %q protocol %q, want %q protocol %q",
			actual.Provider,
			actual.ProtocolVersion,
			fixture.Provider,
			fixture.ProtocolVersion,
		)
	}
	if !actual.Ready || actual.ServerTime.IsZero() {
		return fmt.Errorf("provider is not ready or omitted server time: %#v", actual)
	}
	available := make(map[string]struct{}, len(actual.Capabilities))
	for _, capability := range actual.Capabilities {
		if _, duplicate := available[capability]; duplicate {
			return fmt.Errorf("provider advertised duplicate capability %q", capability)
		}
		available[capability] = struct{}{}
	}
	for _, required := range lifecycleCapabilities {
		if _, ok := available[required]; !ok {
			return fmt.Errorf("provider is missing conformance capability %q", required)
		}
	}
	return nil
}

func verifyWork(actual workcontrol.WorkItem, fixture TaskFixture) error {
	if actual.ProjectID != fixture.ProjectID ||
		actual.Title != fixture.Title ||
		actual.Description != fixture.Description ||
		actual.AcceptanceCriteria != fixture.AcceptanceCriteria ||
		string(actual.Status) != fixture.Status ||
		actual.Priority != fixture.Priority ||
		actual.BudgetMinutes != fixture.BudgetMinutes {
		return fmt.Errorf("discovered work differs from fixture: %#v", actual)
	}
	if actual.Version < 1 || actual.CreatedAt.IsZero() || actual.UpdatedAt.IsZero() {
		return fmt.Errorf("discovered work omitted provider-managed fields: %#v", actual)
	}
	return nil
}

func findWork(items []workcontrol.WorkItem, id string) (workcontrol.WorkItem, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return workcontrol.WorkItem{}, false
}

func findRun(runs []workcontrol.Run, id string) (workcontrol.Run, bool) {
	for _, run := range runs {
		if run.ID == id {
			return run, true
		}
	}
	return workcontrol.Run{}, false
}
