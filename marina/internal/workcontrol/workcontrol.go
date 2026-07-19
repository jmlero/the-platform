package workcontrol

import (
	"context"
	"time"
)

const (
	ProtocolVersion      = "1.0"
	ProtocolMajor        = 1
	ProtocolMinimumMinor = 0

	CapabilityProviderInfo  = "provider.info"
	CapabilityAgentRegister = "agent.register"
	CapabilityWorkReadyList = "work.ready.list"
	CapabilityWorkInspect   = "work.inspect"
	CapabilityWorkClaim     = "work.claim.atomic"
	CapabilityLeaseRenew    = "run.lease.renew"
	CapabilityPlanSubmit    = "run.plan.submit"
	CapabilityRunStart      = "run.start"
	CapabilityProgress      = "run.progress.append"
	CapabilityRunVerify     = "run.verify"
	CapabilityRunComplete   = "run.complete"
	CapabilityRunBlock      = "run.block"
)

type ProviderInfo struct {
	Provider        string
	ProtocolVersion string
	Capabilities    []string
	ServerTime      time.Time
	Ready           bool
	Readiness       map[string]string
}

// ReconciliationCapabilities are the capabilities Marina's currently active
// lease and budget controllers need. Work discovery and claims remain disabled
// until their later safety gates are implemented.
func ReconciliationCapabilities() []string {
	return []string{
		CapabilityProviderInfo,
		CapabilityWorkInspect,
		CapabilityLeaseRenew,
		CapabilityRunBlock,
	}
}

type WorkStatus string

const (
	WorkReady WorkStatus = "ready"
)

type WorkItem struct {
	ID                 string
	ProjectID          string
	Title              string
	Description        string
	AcceptanceCriteria string
	Status             WorkStatus
	Priority           string
	BudgetMinutes      int
	Version            int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type RunStatus string

const (
	RunPlanning        RunStatus = "planning"
	RunWaitingDecision RunStatus = "waiting_decision"
	RunRunning         RunStatus = "running"
	RunVerifying       RunStatus = "verifying"
	RunCompleted       RunStatus = "completed"
	RunBlocked         RunStatus = "blocked"
	RunFailed          RunStatus = "failed"
	RunCancelled       RunStatus = "cancelled"
)

func (status RunStatus) Active() bool {
	switch status {
	case RunPlanning, RunWaitingDecision, RunRunning, RunVerifying:
		return true
	default:
		return false
	}
}

func (status RunStatus) Terminal() bool {
	switch status {
	case RunCompleted, RunBlocked, RunFailed, RunCancelled:
		return true
	default:
		return false
	}
}

type AgentRegistration struct {
	ID           string
	Name         string
	Kind         string
	Capabilities []string
}

type Agent struct {
	ID           string
	Name         string
	Kind         string
	Status       string
	Capabilities []string
	LastSeenAt   time.Time
	CreatedAt    time.Time
}

type Claim struct {
	AgentID string
	Lease   time.Duration
}

type LeaseRenewal struct {
	AgentID string
	Lease   time.Duration
}

type Progress struct {
	Message string
	Payload map[string]any
}

type PlanSubmission struct {
	Plan            string
	RequireApproval bool
}

type Run struct {
	ID             string
	WorkItemID     string
	AgentID        string
	Status         RunStatus
	Plan           string
	ResultSummary  string
	LeaseExpiresAt *time.Time
	StartedAt      time.Time
	FinishedAt     *time.Time
	UpdatedAt      time.Time
}

type WorkDetail struct {
	Work WorkItem
	Runs []Run
}

// HealthChecker reports whether the provider can safely support the
// capabilities currently enabled by Marina.
type HealthChecker interface {
	Health(context.Context) error
}

// ProviderInformation supports protocol and capability negotiation without
// enabling any mutation.
type ProviderInformation interface {
	GetProviderInfo(context.Context) (ProviderInfo, error)
}

// AgentRegistry owns stable logical-agent registration.
type AgentRegistry interface {
	RegisterAgent(context.Context, AgentRegistration) (Agent, error)
}

// WorkDiscovery lists work that is eligible for bounded acquisition.
type WorkDiscovery interface {
	ListReadyWork(context.Context) ([]WorkItem, error)
}

// WorkInspection reads the authoritative task and provider-run ledger.
type WorkInspection interface {
	GetWork(context.Context, string) (WorkDetail, error)
}

// Acquisition creates one provider run for one eligible work item. It remains
// disabled in the controller until persisted intents and idempotency exist.
type Acquisition interface {
	ClaimWork(context.Context, string, Claim) (Run, error)
}

// Planning submits the provider-facing plan representation for an exact run.
// Immutable structured revisions will replace the prototype Markdown payload
// when the planning vertical slice reaches that safety gate.
type Planning interface {
	SubmitRunPlan(context.Context, string, PlanSubmission) error
}

// RunControl protects and stops authority already bound to an exact run.
type RunControl interface {
	RenewRunLease(context.Context, string, LeaseRenewal) error
	BlockRun(context.Context, string, string) error
}

// EventPublisher appends human-facing progress for an already-bound run. A
// later durable outbox will call this boundary; controllers must not call it
// directly before that safety gate exists.
type EventPublisher interface {
	ReportRunProgress(context.Context, string, Progress) error
}
