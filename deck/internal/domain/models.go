package domain

import "errors"

var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrInvalidInput      = errors.New("invalid input")
	ErrInvalidTransition = errors.New("invalid state transition")
)

const (
	TaskBacklog         = "backlog"
	TaskReady           = "ready"
	TaskPlanning        = "planning"
	TaskWaitingDecision = "waiting_decision"
	TaskRunning         = "running"
	TaskVerifying       = "verifying"
	TaskDone            = "done"
	TaskBlocked         = "blocked"
	TaskFailed          = "failed"
	TaskCancelled       = "cancelled"
)

var TaskStatuses = []string{
	TaskBacklog,
	TaskReady,
	TaskPlanning,
	TaskWaitingDecision,
	TaskRunning,
	TaskVerifying,
	TaskDone,
	TaskBlocked,
	TaskFailed,
	TaskCancelled,
}

const (
	RunPlanning        = "planning"
	RunWaitingDecision = "waiting_decision"
	RunRunning         = "running"
	RunVerifying       = "verifying"
	RunCompleted       = "completed"
	RunBlocked         = "blocked"
	RunFailed          = "failed"
	RunCancelled       = "cancelled"
)

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	CreatedAt   int64  `json:"created_at"`
}

type Task struct {
	Review             *TaskReview      `json:"review,omitempty"`
	DependsOn          []string         `json:"depends_on,omitempty"`
	Archived           bool             `json:"archived,omitempty"`
	Workload           *WorkloadProfile `json:"workload,omitempty"`
	Context            *TaskContext     `json:"context,omitempty"`
	ID                 string           `json:"id"`
	ProjectID          string           `json:"project_id"`
	Title              string           `json:"title"`
	Description        string           `json:"description"`
	AcceptanceCriteria string           `json:"acceptance_criteria"`
	Status             string           `json:"status"`
	Priority           string           `json:"priority"`
	BudgetMinutes      int              `json:"budget_minutes"`
	Budget             Budget           `json:"budget"`
	DisplayState       string           `json:"display_state"`
	AssignedAgentID    *string          `json:"assigned_agent_id,omitempty"`
	Version            int              `json:"version"`
	CreatedAt          int64            `json:"created_at"`
	UpdatedAt          int64            `json:"updated_at"`
}

type Budget struct {
	TimeMinutes    int      `json:"time_minutes"`
	DeadlineAt     *int64   `json:"deadline_at,omitempty"`
	MaxAttempts    int      `json:"max_attempts"`
	MaxTokens      int64    `json:"max_tokens"`
	MaxCostMinor   int64    `json:"max_cost_minor"`
	Currency       string   `json:"currency"`
	MaxConcurrency int      `json:"max_concurrency"`
	Authority      []string `json:"authority"`
}

type CreateTaskInput struct {
	ContextMessageIDs  []string `json:"context_message_ids,omitempty"`
	ProjectID          string   `json:"project_id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria string   `json:"acceptance_criteria"`
	Status             string   `json:"status"`
	Priority           string   `json:"priority"`
	BudgetMinutes      int      `json:"budget_minutes"`
	Budget             *Budget  `json:"budget,omitempty"`
}

type UpdateTaskInput struct {
	ContextMessageIDs  *[]string `json:"context_message_ids,omitempty"`
	Title              *string   `json:"title,omitempty"`
	Description        *string   `json:"description,omitempty"`
	AcceptanceCriteria *string   `json:"acceptance_criteria,omitempty"`
	Priority           *string   `json:"priority,omitempty"`
	BudgetMinutes      *int      `json:"budget_minutes,omitempty"`
	Budget             *Budget   `json:"budget,omitempty"`
	Version            *int      `json:"version,omitempty"`
}

type Agent struct {
	ID             string   `json:"id"`
	InstallationID string   `json:"installation_id,omitempty"`
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Status         string   `json:"status"`
	Capabilities   []string `json:"capabilities"`
	LastSeenAt     int64    `json:"last_seen_at"`
	CreatedAt      int64    `json:"created_at"`
}

type Run struct {
	ID                       string  `json:"id"`
	TaskID                   string  `json:"task_id"`
	AgentID                  string  `json:"agent_id"`
	Version                  int64   `json:"version"`
	FenceToken               int64   `json:"fence_token"`
	TaskVersion              int     `json:"task_version"`
	AcceptanceCriteriaDigest string  `json:"acceptance_criteria_digest"`
	Status                   string  `json:"status"`
	Plan                     string  `json:"plan"`
	ActivePlanRevisionID     *string `json:"active_plan_revision_id,omitempty"`
	ResultSummary            string  `json:"result_summary"`
	LeaseExpiresAt           *int64  `json:"lease_expires_at,omitempty"`
	StartedAt                int64   `json:"started_at"`
	FinishedAt               *int64  `json:"finished_at,omitempty"`
	UpdatedAt                int64   `json:"updated_at"`
}

type PlanRevision struct {
	ID                   string   `json:"id"`
	RunID                string   `json:"run_id"`
	Revision             int      `json:"revision"`
	Plan                 string   `json:"plan"`
	ContextDigest        string   `json:"context_digest"`
	RiskClass            string   `json:"risk_class"`
	VerificationStrategy string   `json:"verification_strategy"`
	ExpectedSideEffects  []string `json:"expected_side_effects"`
	RollbackApproach     string   `json:"rollback_approach"`
	SourceRevision       string   `json:"source_revision,omitempty"`
	PolicyVersion        string   `json:"policy_version,omitempty"`
	CreatedAt            int64    `json:"created_at"`
}

type PlanRevisionInput struct {
	Plan                 string   `json:"plan"`
	ContextDigest        string   `json:"context_digest"`
	RiskClass            string   `json:"risk_class"`
	VerificationStrategy string   `json:"verification_strategy"`
	ExpectedSideEffects  []string `json:"expected_side_effects"`
	RollbackApproach     string   `json:"rollback_approach"`
	SourceRevision       string   `json:"source_revision,omitempty"`
	PolicyVersion        string   `json:"policy_version,omitempty"`
}

type DecisionRequest struct {
	Required             bool   `json:"required"`
	ExpiresAt            *int64 `json:"expires_at,omitempty"`
	Risk                 string `json:"risk"`
	EvidenceSummary      string `json:"evidence_summary"`
	EstimatedCostMinor   int64  `json:"estimated_cost_minor"`
	Currency             string `json:"currency"`
	WaitingConsequence   string `json:"waiting_consequence"`
	RejectingConsequence string `json:"rejecting_consequence"`
}

const (
	CommandPause           = "pause"
	CommandResume          = "resume"
	CommandCancel          = "cancel"
	CommandReplan          = "replan"
	CommandRetry           = "retry"
	CommandReduceAuthority = "reduce_authority"
)

type Command struct {
	ID          string `json:"id"`
	RunID       string `json:"run_id"`
	Sequence    int64  `json:"sequence"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	RequestedBy string `json:"requested_by"`
	Result      string `json:"result,omitempty"`
	RunVersion  int64  `json:"run_version"`
	FenceToken  int64  `json:"fence_token"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type ProjectedEvent struct {
	ID              string         `json:"id"`
	Producer        string         `json:"producer"`
	ProducerEventID string         `json:"producer_event_id"`
	Sequence        int64          `json:"sequence"`
	ExecutionID     string         `json:"execution_id"`
	Type            string         `json:"type"`
	Message         string         `json:"message"`
	Payload         map[string]any `json:"payload"`
	OccurredAt      int64          `json:"occurred_at"`
	IngestedAt      int64          `json:"ingested_at"`
}

type Decision struct {
	ID                   string   `json:"id"`
	TaskID               string   `json:"task_id"`
	RunID                *string  `json:"run_id,omitempty"`
	PlanRevisionID       string   `json:"plan_revision_id,omitempty"`
	InteractionID        string   `json:"interaction_id,omitempty"`
	AnswerMode           string   `json:"answer_mode,omitempty"`
	Question             string   `json:"question"`
	Options              []string `json:"options"`
	Recommendation       string   `json:"recommendation"`
	RiskClass            string   `json:"risk_class"`
	EvidenceSummary      string   `json:"evidence_summary"`
	EstimatedCostMinor   int64    `json:"estimated_cost_minor"`
	Currency             string   `json:"currency"`
	WaitingConsequence   string   `json:"waiting_consequence"`
	RejectingConsequence string   `json:"rejecting_consequence"`
	Status               string   `json:"status"`
	Answer               string   `json:"answer"`
	ExpiresAt            *int64   `json:"expires_at,omitempty"`
	CreatedAt            int64    `json:"created_at"`
	AnsweredAt           *int64   `json:"answered_at,omitempty"`
	AnsweredBy           string   `json:"answered_by,omitempty"`
}

// EngineQuestion is a bounded, single-question request tied to one pending
// operation in an already running attempt. It never grants new authority.
type EngineQuestion struct {
	OperationID string   `json:"operation_id"`
	Question    string   `json:"question"`
	Options     []string `json:"options,omitempty"`
	ExpiresAt   int64    `json:"expires_at"`
}

type Artifact struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	RunID     string `json:"run_id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	URI       string `json:"uri"`
	Content   string `json:"content"`
	Digest    string `json:"digest,omitempty"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt int64  `json:"created_at"`
}

type EvidenceRecord struct {
	ID               string `json:"id"`
	TaskID           string `json:"task_id"`
	RunID            string `json:"run_id"`
	PlanRevisionID   string `json:"plan_revision_id"`
	Type             string `json:"type"`
	Producer         string `json:"producer"`
	Verifier         string `json:"verifier"`
	SubjectDigest    string `json:"subject_digest"`
	SourceRevision   string `json:"source_revision"`
	Command          string `json:"command"`
	Outcome          string `json:"outcome"`
	ProvenanceURI    string `json:"provenance_uri"`
	ProvenanceDigest string `json:"provenance_digest"`
	ObservedAt       int64  `json:"observed_at"`
	ExpiresAt        *int64 `json:"expires_at,omitempty"`
	ArtifactURI      string `json:"artifact_uri"`
	ArtifactDigest   string `json:"artifact_digest"`
	PolicyVersion    string `json:"policy_version,omitempty"`
	AdmissionStatus  string `json:"admission_status"`
	CreatedAt        int64  `json:"created_at"`
}

type EvidenceInput struct {
	ID               string `json:"id,omitempty"`
	RunID            string `json:"run_id"`
	PlanRevisionID   string `json:"plan_revision_id"`
	Type             string `json:"type"`
	Producer         string `json:"producer"`
	Verifier         string `json:"verifier"`
	SubjectDigest    string `json:"subject_digest"`
	SourceRevision   string `json:"source_revision"`
	Command          string `json:"command"`
	Outcome          string `json:"outcome"`
	ProvenanceURI    string `json:"provenance_uri"`
	ProvenanceDigest string `json:"provenance_digest"`
	ObservedAt       int64  `json:"observed_at"`
	ExpiresAt        *int64 `json:"expires_at,omitempty"`
	ArtifactURI      string `json:"artifact_uri"`
	ArtifactDigest   string `json:"artifact_digest"`
	PolicyVersion    string `json:"policy_version,omitempty"`
}

type CanonicalReferences struct {
	TaskIDs     []string `json:"task_ids,omitempty"`
	RunIDs      []string `json:"run_ids,omitempty"`
	DecisionIDs []string `json:"decision_ids,omitempty"`
	ArtifactIDs []string `json:"artifact_ids,omitempty"`
	EvidenceIDs []string `json:"evidence_ids,omitempty"`
}

type Channel struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
}

type ChannelMessage struct {
	ThreadID            string              `json:"thread_id,omitempty"`
	InvokedAgentID      string              `json:"invoked_agent_id,omitempty"`
	InvocationRequestID string              `json:"invocation_request_id,omitempty"`
	ID                  string              `json:"id"`
	ChannelID           string              `json:"channel_id"`
	ProjectID           string              `json:"project_id"`
	AuthorType          string              `json:"author_type"`
	AuthorID            string              `json:"author_id"`
	Body                string              `json:"body"`
	References          CanonicalReferences `json:"references"`
	PromotedTaskID      *string             `json:"promoted_task_id,omitempty"`
	CreatedAt           int64               `json:"created_at"`
}

type ChannelMessageInput struct {
	ThreadID   string              `json:"thread_id,omitempty"`
	ChannelID  string              `json:"channel_id"`
	AuthorType string              `json:"author_type"`
	AuthorID   string              `json:"author_id"`
	Body       string              `json:"body"`
	References CanonicalReferences `json:"references"`
}

type ProviderStatus struct {
	ProjectID               string   `json:"project_id"`
	Connectivity            string   `json:"connectivity"`
	LastSeenAt              *int64   `json:"last_seen_at,omitempty"`
	SyncState               string   `json:"sync_state"`
	SyncLagMillis           int64    `json:"sync_lag_millis"`
	LeaseState              string   `json:"lease_state"`
	LeaseExpiresAt          *int64   `json:"lease_expires_at,omitempty"`
	DegradedDependencies    []string `json:"degraded_dependencies"`
	StaleProjectionCount    int64    `json:"stale_projection_count"`
	RejectedProjectionCount int64    `json:"rejected_projection_count"`
	LastError               string   `json:"last_error,omitempty"`
}

type OperationalState struct {
	ID                   string `json:"id"`
	ProjectID            string `json:"project_id"`
	TaskID               string `json:"task_id"`
	RunID                string `json:"run_id"`
	Environment          string `json:"environment"`
	RolloutState         string `json:"rollout_state"`
	ObservationStartedAt *int64 `json:"observation_started_at,omitempty"`
	ObservationEndsAt    *int64 `json:"observation_ends_at,omitempty"`
	RollbackState        string `json:"rollback_state"`
	PolicyVersion        string `json:"policy_version"`
	SourceRevision       string `json:"source_revision"`
	Outcome              string `json:"outcome,omitempty"`
	UpdatedAt            int64  `json:"updated_at"`
}

type OperationalStateInput struct {
	RunID                string `json:"run_id"`
	Environment          string `json:"environment"`
	RolloutState         string `json:"rollout_state"`
	ObservationStartedAt *int64 `json:"observation_started_at,omitempty"`
	ObservationEndsAt    *int64 `json:"observation_ends_at,omitempty"`
	RollbackState        string `json:"rollback_state"`
	PolicyVersion        string `json:"policy_version"`
	SourceRevision       string `json:"source_revision"`
	Outcome              string `json:"outcome,omitempty"`
}

type TrailEntry struct {
	ID         string         `json:"id"`
	Stage      string         `json:"stage"`
	Type       string         `json:"type"`
	Title      string         `json:"title"`
	ResourceID string         `json:"resource_id,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
	OccurredAt int64          `json:"occurred_at"`
}

type Event struct {
	ID        int64          `json:"id"`
	TaskID    *string        `json:"task_id,omitempty"`
	RunID     *string        `json:"run_id,omitempty"`
	AgentID   *string        `json:"agent_id,omitempty"`
	Type      string         `json:"type"`
	Message   string         `json:"message"`
	Payload   map[string]any `json:"payload"`
	CreatedAt int64          `json:"created_at"`
}

type TaskDetail struct {
	Task              Task               `json:"task"`
	Runs              []Run              `json:"runs"`
	PlanRevisions     []PlanRevision     `json:"plan_revisions"`
	Events            []Event            `json:"events"`
	Decisions         []Decision         `json:"decisions"`
	Artifacts         []Artifact         `json:"artifacts"`
	Evidence          []EvidenceRecord   `json:"evidence"`
	Commands          []Command          `json:"commands"`
	OperationalStates []OperationalState `json:"operational_states"`
	ProviderStatus    ProviderStatus     `json:"provider_status"`
	DisplayState      string             `json:"display_state"`
	Trail             []TrailEntry       `json:"trail"`
}

type ActiveRun struct {
	Run  Run  `json:"run"`
	Task Task `json:"task"`
}
