package execution

import (
	"errors"
	"time"
)

var (
	ErrNotFound          = errors.New("execution record not found")
	ErrConflict          = errors.New("execution record conflict")
	ErrInvalidInput      = errors.New("invalid execution input")
	ErrInvalidTransition = errors.New("invalid execution state transition")
	ErrEvidenceRequired  = errors.New("current evidence is required")
	ErrBudgetExhausted   = errors.New("execution budget exhausted")
)

type State string

const (
	StatePlanning        State = "planning"
	StateWaitingDecision State = "waiting_decision"
	StateRunning         State = "running"
	StateVerifying       State = "verifying"
	StateCompleted       State = "completed"
	StateBlocked         State = "blocked"
	StateFailed          State = "failed"
	StateCancelled       State = "cancelled"
)

func (state State) Terminal() bool {
	switch state {
	case StateCompleted, StateBlocked, StateFailed, StateCancelled:
		return true
	default:
		return false
	}
}

func (state State) Valid() bool {
	switch state {
	case StatePlanning, StateWaitingDecision, StateRunning, StateVerifying,
		StateCompleted, StateBlocked, StateFailed, StateCancelled:
		return true
	default:
		return false
	}
}

func (state State) CanTransition(target State) bool {
	switch state {
	case StatePlanning:
		return target == StateWaitingDecision || target == StateRunning || target == StateBlocked || target == StateFailed || target == StateCancelled
	case StateWaitingDecision:
		return target == StateRunning || target == StateBlocked || target == StateFailed || target == StateCancelled
	case StateRunning:
		return target == StateVerifying || target == StateBlocked || target == StateFailed || target == StateCancelled
	case StateVerifying:
		return target == StateRunning || target == StateCompleted || target == StateBlocked || target == StateFailed || target == StateCancelled
	default:
		return false
	}
}

type FailureClass string

const (
	FailureTransient FailureClass = "transient"
	FailurePermanent FailureClass = "permanent"
	FailurePolicy    FailureClass = "policy"
	FailureBudget    FailureClass = "budget"
	FailureLease     FailureClass = "lease"
	FailureRuntime   FailureClass = "runtime"
	FailureExternal  FailureClass = "external"
)

func (class FailureClass) Valid() bool {
	switch class {
	case FailureTransient, FailurePermanent, FailurePolicy, FailureBudget,
		FailureLease, FailureRuntime, FailureExternal:
		return true
	default:
		return false
	}
}

type Budget struct {
	DeadlineAt    time.Time
	MaxAttempts   int
	MaxTokens     int64
	MaxCostMicros int64
}

type NewExecution struct {
	WorkProvider       string
	ProjectID          string
	ExternalWorkID     string
	ExternalRunID      string
	AgentID            string
	ExternalRunVersion int64
	FenceToken         int64
	TaskVersion        int
	TaskDigest         string
	BindingVersion     int64
	LeaseExpiresAt     time.Time
	Budget             Budget
}

type Execution struct {
	ID                 string
	WorkProvider       string
	ProjectID          string
	ExternalWorkID     string
	ExternalRunID      string
	AgentID            string
	ExternalRunVersion int64
	FenceToken         int64
	TaskVersion        int
	TaskDigest         string
	BindingVersion     int64
	State              State
	Attempt            int
	LeaseExpiresAt     time.Time
	DeadlineAt         time.Time
	MaxAttempts        int
	MaxTokens          int64
	MaxCostMicros      int64
	UsedTokens         int64
	UsedCostMicros     int64
	RuntimeHandle      string
	FailureClass       FailureClass
	FailureMessage     string
	CommandCursor      int64
	RetryAttempt       int
	NextEligibleAt     *time.Time
	LastErrorClass     FailureClass
	ExhaustionReason   string
	Version            int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
	FinishedAt         *time.Time
}

func (execution Execution) AuthoritySafe(now time.Time, safetyWindow time.Duration) bool {
	return !execution.State.Terminal() && execution.ExternalRunVersion > 0 && execution.FenceToken > 0 &&
		now.Add(safetyWindow).Before(execution.LeaseExpiresAt) && !execution.BudgetExpired(now)
}

func (execution Execution) LeaseExpired(now time.Time) bool {
	return !execution.State.Terminal() && !now.Before(execution.LeaseExpiresAt)
}

func (execution Execution) BudgetExpired(now time.Time) bool {
	if execution.State.Terminal() {
		return false
	}
	return !now.Before(execution.DeadlineAt) || execution.Attempt > execution.MaxAttempts ||
		(execution.MaxTokens > 0 && execution.UsedTokens >= execution.MaxTokens) ||
		(execution.MaxCostMicros > 0 && execution.UsedCostMicros >= execution.MaxCostMicros)
}

type Transition struct {
	ExpectedVersion int64
	Target          State
	Message         string
	FailureClass    FailureClass
	FailureMessage  string
}

type CheckpointInput struct {
	Key            string
	Phase          string
	RuntimeHandle  string
	SourceRevision string
	Payload        map[string]any
}

type Checkpoint struct {
	ID             int64
	ExecutionID    string
	Key            string
	Phase          string
	RuntimeHandle  string
	SourceRevision string
	Payload        map[string]any
	CreatedAt      time.Time
}

type EvidenceInput struct {
	Key            string
	Kind           string
	SourceRevision string
	Environment    string
	Command        string
	Summary        string
	URI            string
	ObservedAt     time.Time
}

type Evidence struct {
	ID             int64
	ExecutionID    string
	Key            string
	Kind           string
	SourceRevision string
	Environment    string
	Command        string
	Summary        string
	URI            string
	ObservedAt     time.Time
	CreatedAt      time.Time
}

type Event struct {
	ID          int64
	ExecutionID string
	Type        string
	Message     string
	Payload     map[string]any
	CreatedAt   time.Time
}
