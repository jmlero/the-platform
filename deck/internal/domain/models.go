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
	ID                 string  `json:"id"`
	ProjectID          string  `json:"project_id"`
	Title              string  `json:"title"`
	Description        string  `json:"description"`
	AcceptanceCriteria string  `json:"acceptance_criteria"`
	Status             string  `json:"status"`
	Priority           string  `json:"priority"`
	BudgetMinutes      int     `json:"budget_minutes"`
	AssignedAgentID    *string `json:"assigned_agent_id,omitempty"`
	Version            int     `json:"version"`
	CreatedAt          int64   `json:"created_at"`
	UpdatedAt          int64   `json:"updated_at"`
}

type CreateTaskInput struct {
	ProjectID          string `json:"project_id"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
	Status             string `json:"status"`
	Priority           string `json:"priority"`
	BudgetMinutes      int    `json:"budget_minutes"`
}

type UpdateTaskInput struct {
	Title              *string `json:"title,omitempty"`
	Description        *string `json:"description,omitempty"`
	AcceptanceCriteria *string `json:"acceptance_criteria,omitempty"`
	Priority           *string `json:"priority,omitempty"`
	BudgetMinutes      *int    `json:"budget_minutes,omitempty"`
	Version            *int    `json:"version,omitempty"`
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
	ID                       string `json:"id"`
	TaskID                   string `json:"task_id"`
	AgentID                  string `json:"agent_id"`
	Version                  int64  `json:"version"`
	FenceToken               int64  `json:"fence_token"`
	TaskVersion              int    `json:"task_version"`
	AcceptanceCriteriaDigest string `json:"acceptance_criteria_digest"`
	Status                   string `json:"status"`
	Plan                     string `json:"plan"`
	ResultSummary            string `json:"result_summary"`
	LeaseExpiresAt           *int64 `json:"lease_expires_at,omitempty"`
	StartedAt                int64  `json:"started_at"`
	FinishedAt               *int64 `json:"finished_at,omitempty"`
	UpdatedAt                int64  `json:"updated_at"`
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
	ID             string   `json:"id"`
	TaskID         string   `json:"task_id"`
	RunID          *string  `json:"run_id,omitempty"`
	Question       string   `json:"question"`
	Options        []string `json:"options"`
	Recommendation string   `json:"recommendation"`
	Status         string   `json:"status"`
	Answer         string   `json:"answer"`
	CreatedAt      int64    `json:"created_at"`
	AnsweredAt     *int64   `json:"answered_at,omitempty"`
}

type Artifact struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	RunID     string `json:"run_id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	URI       string `json:"uri"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
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
	Task      Task       `json:"task"`
	Runs      []Run      `json:"runs"`
	Events    []Event    `json:"events"`
	Decisions []Decision `json:"decisions"`
	Artifacts []Artifact `json:"artifacts"`
}

type ActiveRun struct {
	Run  Run  `json:"run"`
	Task Task `json:"task"`
}
