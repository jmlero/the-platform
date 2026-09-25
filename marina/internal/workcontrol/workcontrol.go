package workcontrol

import (
	"context"
	"time"
)

const (
	ProtocolVersion      = "1.0"
	ProtocolMajor        = 1
	ProtocolMinimumMinor = 0

	CapabilityContextRead     = "context.read"
	CapabilityRepositoryRead  = "repository.read"
	CapabilityRepositoryWrite = "repository.write"
	CapabilityProviderInfo    = "provider.info"
	CapabilityAgentRegister   = "agent.register"
	CapabilityWorkReadyList   = "work.ready.list"
	CapabilityWorkInspect     = "work.inspect"
	CapabilityWorkClaim       = "work.claim.atomic"
	CapabilityLeaseRenew      = "run.lease.renew"
	CapabilityPlanSubmit      = "run.plan.submit"
	CapabilityRunStart        = "run.start"
	CapabilityProgress        = "run.progress.append"
	CapabilityRunVerify       = "run.verify"
	CapabilityRunComplete     = "run.complete"
	CapabilityRunBlock        = "run.block"
	CapabilityClaimLookup     = "work.claim.lookup"
	CapabilityAssignedRuns    = "run.assigned.list"
	CapabilityCommandList     = "command.list"
	CapabilityCommandAck      = "command.ack"
	CapabilityEventAppend     = "event.append"
	CapabilityDecisionInspect = "decision.inspect"
	CapabilityDecisionRequest = "decision.request"
	CapabilityChannelList     = "channel.list"
	CapabilityChannelRead     = "channel.message.list"
	CapabilityConversationList = "conversation.invocation.list"
	CapabilityConversationQuestion = "conversation.question.write"
	CapabilityConversationOutcome = "conversation.outcome.publish"
	CapabilityConversationDelegate = "conversation.task.delegate"
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

func MilestoneOneCapabilities() []string {
	return []string{
		CapabilityProviderInfo, CapabilityAgentRegister, CapabilityWorkReadyList,
		CapabilityWorkInspect, CapabilityWorkClaim, CapabilityClaimLookup,
		CapabilityAssignedRuns, CapabilityLeaseRenew, CapabilityRunBlock,
		CapabilityCommandList, CapabilityCommandAck, CapabilityEventAppend,
	}
}

type WorkStatus string

const (
	WorkReady WorkStatus = "ready"
)

type WorkItem struct {
	Workload           *WorkloadProfile `json:",omitempty"`
	Context            *TaskContext     `json:",omitempty"`
	ID                 string
	ProjectID          string
	Title              string
	Description        string
	AcceptanceCriteria string
	Status             WorkStatus
	Priority           string
	BudgetMinutes      int
	Budget             Budget
	Version            int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Budget is the operator's authorization, preserved without changing currency
// or interpreting a missing limit as permission to exceed a project ceiling.
type Budget struct {
	TimeMinutes    int
	DeadlineAt     *time.Time
	MaxAttempts    int
	MaxTokens      int64
	MaxCostMinor   int64
	Currency       string
	MaxConcurrency int
	Authority      []string
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
	ID             string
	InstallationID string
	Name           string
	Kind           string
	Capabilities   []string
}

type Agent struct {
	ID             string
	InstallationID string
	Name           string
	Kind           string
	Status         string
	Capabilities   []string
	LastSeenAt     time.Time
	CreatedAt      time.Time
}

// Mutation request values carry a caller-owned key that must remain stable
// across retries of the same logical operation. Adapters may create a one-shot
// key for legacy callers, but that cannot recover an ambiguous response.
type Claim struct {
	IdempotencyKey      string
	AgentID             string
	ExpectedTaskVersion int
	Lease               time.Duration
}

type LeaseRenewal struct {
	IdempotencyKey  string
	AgentID         string
	ExpectedVersion int64
	FenceToken      int64
	Lease           time.Duration
}

type Progress struct {
	IdempotencyKey  string
	AgentID         string
	ExpectedVersion int64
	FenceToken      int64
	Message         string
	Payload         map[string]any
}

type PlanSubmission struct {
	IdempotencyKey  string
	AgentID         string
	ExpectedVersion int64
	FenceToken      int64
	Plan            string
	RequireApproval bool
}

type PlanRevisionSubmission struct {
	IdempotencyKey       string
	AgentID              string
	ExpectedVersion      int64
	FenceToken           int64
	Plan                 string
	ContextDigest        string
	RiskClass            string
	VerificationStrategy string
	ExpectedSideEffects  []string
	RollbackApproach     string
	SourceRevision       string
	PolicyVersion        string
	RequireApproval      bool
	DecisionExpiresAt    *time.Time
	DecisionEvidence     string
	EstimatedCostMinor   int64
	Currency             string
	WaitingConsequence   string
	RejectingConsequence string
}

type PlanRevision struct {
	ID                   string
	RunID                string
	Revision             int
	Plan                 string
	ContextDigest        string
	RiskClass            string
	VerificationStrategy string
	ExpectedSideEffects  []string
	RollbackApproach     string
	SourceRevision       string
	PolicyVersion        string
	CreatedAt            time.Time
}

type DecisionStatus string

const (
	DecisionPending    DecisionStatus = "pending"
	DecisionAnswered   DecisionStatus = "answered"
	DecisionCancelled  DecisionStatus = "cancelled"
	DecisionExpired    DecisionStatus = "expired"
	DecisionSuperseded DecisionStatus = "superseded"
)

type Decision struct {
	ID                   string
	TaskID               string
	RunID                string
	PlanRevisionID       string
	InteractionID        string
	AnswerMode           string
	Question             string
	Options              []string
	Recommendation       string
	RiskClass            string
	EvidenceSummary      string
	EstimatedCostMinor   int64
	Currency             string
	WaitingConsequence   string
	RejectingConsequence string
	Status               DecisionStatus
	Answer               string
	ExpiresAt            *time.Time
	CreatedAt            time.Time
	AnsweredAt           *time.Time
	AnsweredBy           string
}

type EngineQuestion struct {
	IdempotencyKey  string
	AgentID         string
	ExpectedVersion int64
	FenceToken      int64
	OperationID     string
	Question        string
	Options         []string
	ExpiresAt       time.Time
}

type QuestionProjection interface {
	RequestEngineQuestion(context.Context, string, EngineQuestion) (Decision, error)
}

type PlanRevisionResult struct {
	PlanRevision PlanRevision
	Decision     *Decision
}

type RunBlock struct {
	IdempotencyKey  string
	AgentID         string
	ExpectedVersion int64
	FenceToken      int64
	Reason          string
}

type Run struct {
	ID                       string
	WorkItemID               string
	AgentID                  string
	Version                  int64
	FenceToken               int64
	TaskVersion              int
	AcceptanceCriteriaDigest string
	Status                   RunStatus
	Plan                     string
	ActivePlanRevisionID     string
	ResultSummary            string
	LeaseExpiresAt           *time.Time
	StartedAt                time.Time
	FinishedAt               *time.Time
	UpdatedAt                time.Time
}

type ActiveRun struct {
	Run  Run
	Work WorkItem
}

type ActiveRunPage struct {
	Items      []ActiveRun
	NextCursor string
}

type Command struct {
	ID          string
	RunID       string
	Sequence    int64
	Type        string
	Status      string
	Reason      string
	RequestedBy string
	Result      string
	RunVersion  int64
	FenceToken  int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CommandAcknowledgement struct {
	IdempotencyKey  string
	AgentID         string
	ExpectedVersion int64
	FenceToken      int64
	Status          string
	Result          string
}

type ProjectedEvent struct {
	IdempotencyKey  string
	AgentID         string
	ExpectedVersion int64
	FenceToken      int64
	Producer        string
	ProducerEventID string
	ExecutionID     string
	Sequence        int64
	Type            string
	Message         string
	Payload         map[string]any
	OccurredAt      time.Time
}

type WorkDetail struct {
	Work          WorkItem
	Runs          []Run
	PlanRevisions []PlanRevision
	Decisions     []Decision
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

type RecoverableAcquisition interface {
	Acquisition
	LookupClaim(context.Context, string, string) (Run, error)
	ListAssignedRuns(context.Context, string, string, string, int) (ActiveRunPage, error)
}

// Planning preserves the prototype plan mutation for protocol conformance.
type Planning interface {
	SubmitRunPlan(context.Context, string, PlanSubmission) error
}

// VersionedPlanning submits an immutable, source/context/policy-bound plan.
type VersionedPlanning interface {
	SubmitPlanRevision(context.Context, string, PlanRevisionSubmission) (PlanRevisionResult, error)
}

type DecisionInspection interface {
	GetDecision(context.Context, string) (Decision, error)
}

// RunControl protects and stops authority already bound to an exact run.
type RunControl interface {
	RenewRunLease(context.Context, string, LeaseRenewal) error
	BlockRun(context.Context, string, RunBlock) error
}

// EventPublisher appends human-facing progress for an already-bound run. A
// later durable outbox will call this boundary; controllers must not call it
// directly before that safety gate exists.
type EventPublisher interface {
	ReportRunProgress(context.Context, string, Progress) error
}

type ProjectionPublisher interface {
	AppendRunEvent(context.Context, string, ProjectedEvent) error
}

type CommandControl interface {
	ListRunCommands(context.Context, string, int64, int) ([]Command, error)
	AcknowledgeRunCommand(context.Context, string, string, CommandAcknowledgement) error
}
