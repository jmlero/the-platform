package conformance

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

type subjectStub struct {
	info             workcontrol.ProviderInfo
	work             workcontrol.WorkItem
	agent            workcontrol.Agent
	run              workcontrol.Run
	plan             string
	progressMessages []string
}

func (subject *subjectStub) Health(context.Context) error {
	if !subject.info.Ready {
		return errors.New("provider not ready")
	}
	return nil
}

func (subject *subjectStub) GetProviderInfo(context.Context) (workcontrol.ProviderInfo, error) {
	return subject.info, nil
}

func (subject *subjectStub) RegisterAgent(
	_ context.Context,
	registration workcontrol.AgentRegistration,
) (workcontrol.Agent, error) {
	subject.agent = workcontrol.Agent{
		ID:             registration.ID,
		InstallationID: registration.InstallationID,
		Name:           registration.Name,
		Kind:           registration.Kind,
		Status:         "idle",
		Capabilities:   append([]string(nil), registration.Capabilities...),
		LastSeenAt:     time.Now().UTC(),
		CreatedAt:      time.Now().UTC(),
	}
	return subject.agent, nil
}

func (subject *subjectStub) ListReadyWork(context.Context) ([]workcontrol.WorkItem, error) {
	if subject.work.Status != workcontrol.WorkReady {
		return []workcontrol.WorkItem{}, nil
	}
	return []workcontrol.WorkItem{subject.work}, nil
}

func (subject *subjectStub) GetWork(context.Context, string) (workcontrol.WorkDetail, error) {
	runs := []workcontrol.Run{}
	if subject.run.ID != "" {
		runs = append(runs, subject.run)
	}
	return workcontrol.WorkDetail{Work: subject.work, Runs: runs}, nil
}

func (subject *subjectStub) ClaimWork(
	_ context.Context,
	workID string,
	claim workcontrol.Claim,
) (workcontrol.Run, error) {
	if workID != subject.work.ID || subject.run.ID != "" {
		return workcontrol.Run{}, errors.New("claim conflict")
	}
	now := time.Now().UTC()
	expires := now.Add(claim.Lease)
	subject.run = workcontrol.Run{
		ID:         "run-real-1",
		WorkItemID: workID,
		AgentID:    claim.AgentID,
		Version:    1, FenceToken: 1, TaskVersion: subject.work.Version, AcceptanceCriteriaDigest: "digest",
		Status:         workcontrol.RunPlanning,
		LeaseExpiresAt: &expires,
		StartedAt:      now,
		UpdatedAt:      now,
	}
	subject.work.Status = workcontrol.WorkStatus(workcontrol.RunPlanning)
	return subject.run, nil
}

func (subject *subjectStub) RenewRunLease(
	_ context.Context,
	runID string,
	renewal workcontrol.LeaseRenewal,
) error {
	if runID != subject.run.ID || renewal.AgentID != subject.run.AgentID {
		return errors.New("run not found")
	}
	expires := time.Now().UTC().Add(renewal.Lease)
	subject.run.LeaseExpiresAt = &expires
	subject.run.Version++
	return nil
}

func (subject *subjectStub) SubmitRunPlan(
	_ context.Context,
	runID string,
	submission workcontrol.PlanSubmission,
) error {
	if runID != subject.run.ID || submission.RequireApproval {
		return errors.New("invalid conformance plan")
	}
	subject.plan = submission.Plan
	subject.run.Plan = submission.Plan
	subject.run.Status = workcontrol.RunRunning
	subject.run.Version++
	subject.work.Status = workcontrol.WorkStatus(workcontrol.RunRunning)
	return nil
}

func (subject *subjectStub) ReportRunProgress(
	_ context.Context,
	runID string,
	progress workcontrol.Progress,
) error {
	if runID != subject.run.ID || subject.run.Status != workcontrol.RunRunning {
		return errors.New("run is not running")
	}
	subject.progressMessages = append(subject.progressMessages, progress.Message)
	subject.run.Version++
	return nil
}

func (subject *subjectStub) BlockRun(_ context.Context, runID string, block workcontrol.RunBlock) error {
	if runID != subject.run.ID || strings.TrimSpace(block.Reason) == "" {
		return errors.New("cannot block run")
	}
	subject.run.Status = workcontrol.RunBlocked
	subject.run.ResultSummary = block.Reason
	subject.run.Version++
	subject.work.Status = workcontrol.WorkStatus(workcontrol.RunBlocked)
	return nil
}

func TestVerifyExercisesOneExactLifecycle(t *testing.T) {
	now := time.Now().UTC()
	fixtures := Fixtures{
		Provider: ProviderFixture{Provider: "appdeck", ProtocolVersion: workcontrol.ProtocolVersion},
		Agent: AgentFixture{
			ID: "agent-fixture", Name: "Marina conformance agent", Kind: "marina",
			InstallationID: "installation-conformance",
			Capabilities:   []string{"work-control.conformance"},
		},
		Task: TaskFixture{
			ProjectID: "prj_default", Title: "Contract task", Description: "Exercise provider",
			AcceptanceCriteria: "One exact run", Status: "ready", Priority: "high", BudgetMinutes: 45,
		},
		Run:   RunFixture{Status: "planning"},
		Plan:  PlanFixture{Summary: "Verify without runtime mutation"},
		Event: EventFixture{CorrelationID: "correlation-fixture", Payload: map[string]any{"message": "observed"}},
	}
	subject := &subjectStub{
		info: workcontrol.ProviderInfo{
			Provider: "appdeck", ProtocolVersion: workcontrol.ProtocolVersion,
			Capabilities: append([]string(nil), lifecycleCapabilities...),
			ServerTime:   now, Ready: true, Readiness: map[string]string{"store": "available"},
		},
		work: workcontrol.WorkItem{
			ID: "task-real-1", ProjectID: fixtures.Task.ProjectID, Title: fixtures.Task.Title,
			Description: fixtures.Task.Description, AcceptanceCriteria: fixtures.Task.AcceptanceCriteria,
			Status: workcontrol.WorkReady, Priority: fixtures.Task.Priority, BudgetMinutes: fixtures.Task.BudgetMinutes,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		},
	}

	result, err := Verify(context.Background(), subject, fixtures, subject.work.ID)
	if err != nil {
		t.Fatalf("verify conformance: %v", err)
	}
	if result.Run.Status != workcontrol.RunBlocked || result.Work.ID != "task-real-1" {
		t.Fatalf("result = %#v", result)
	}
	if subject.plan != fixtures.Plan.Summary {
		t.Fatalf("plan = %q", subject.plan)
	}
	if !reflect.DeepEqual(subject.progressMessages, []string{"observed"}) {
		t.Fatalf("progress messages = %#v", subject.progressMessages)
	}
}

func TestVerifyRejectsMissingCapabilityBeforeMutation(t *testing.T) {
	now := time.Now().UTC()
	subject := &subjectStub{
		info: workcontrol.ProviderInfo{
			Provider: "appdeck", ProtocolVersion: workcontrol.ProtocolVersion,
			Capabilities: []string{workcontrol.CapabilityProviderInfo},
			ServerTime:   now, Ready: true,
		},
	}
	fixtures := Fixtures{Provider: ProviderFixture{Provider: "appdeck", ProtocolVersion: workcontrol.ProtocolVersion}}

	_, err := Verify(context.Background(), subject, fixtures, "task-real-1")
	if err == nil || !strings.Contains(err.Error(), workcontrol.CapabilityAgentRegister) {
		t.Fatalf("error = %v", err)
	}
	if subject.agent.ID != "" || subject.run.ID != "" {
		t.Fatalf("suite mutated provider before capability validation: %#v", subject)
	}
}
