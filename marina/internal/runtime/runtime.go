package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

var (
	ErrInvalidInput      = errors.New("invalid runtime input")
	ErrConflict          = errors.New("runtime conflict")
	ErrUnsupportedResume = errors.New("runtime resume is unsupported")
)

type State string

const (
	StatePending   State = "pending"
	StateStarting  State = "starting"
	StateRunning   State = "running"
	StatePaused    State = "paused"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
	StateTimedOut  State = "timed_out"
)

func (state State) Valid() bool {
	switch state {
	case StatePending, StateStarting, StateRunning, StatePaused, StateSucceeded,
		StateFailed, StateCancelled, StateTimedOut:
		return true
	default:
		return false
	}
}

func (state State) Terminal() bool {
	switch state {
	case StateSucceeded, StateFailed, StateCancelled, StateTimedOut:
		return true
	default:
		return false
	}
}

func (state State) CanObserve(target State) bool {
	if state == target {
		return true
	}
	switch state {
	case StatePending:
		return target == StateStarting || target == StateRunning || target.Terminal()
	case StateStarting:
		return target == StateRunning || target.Terminal()
	case StateRunning:
		return target == StatePaused || target.Terminal()
	case StatePaused:
		return target == StateRunning || target == StateFailed || target == StateCancelled || target == StateTimedOut
	default:
		return false
	}
}

type CleanupState string

const (
	CleanupPending   CleanupState = "pending"
	CleanupRunning   CleanupState = "running"
	CleanupCompleted CleanupState = "completed"
	CleanupFailed    CleanupState = "failed"
)

func (state CleanupState) Valid() bool {
	switch state {
	case CleanupPending, CleanupRunning, CleanupCompleted, CleanupFailed:
		return true
	default:
		return false
	}
}

type Limits struct {
	CPUMillis             int64 `json:"cpu_millis"`
	MemoryBytes           int64 `json:"memory_bytes"`
	EphemeralStorageBytes int64 `json:"ephemeral_storage_bytes"`
	WallTimeSeconds       int64 `json:"wall_time_seconds"`
	MaxProcesses          int   `json:"max_processes"`
}

type CredentialReference struct {
	Kind      string    `json:"kind"`
	Reference string    `json:"reference"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Specification struct {
	Engine *EngineSelection `json:"engine,omitempty"`
	Tools  []string         `json:"tools,omitempty"`
	Stage  string           `json:"stage,omitempty"`
	// Empty Workload preserves the original repository specification and digest.
	Workload             string                `json:"workload,omitempty"`
	InputDigest          string                `json:"input_digest,omitempty"`
	ProfileDigest        string                `json:"profile_digest,omitempty"`
	WorkerImage          string                `json:"worker_image,omitempty"`
	ExecutionID          string                `json:"execution_id"`
	AttemptID            string                `json:"attempt_id"`
	IdempotencyKey       string                `json:"idempotency_key"`
	RepositoryURI        string                `json:"repository_uri"`
	SourceRevision       string                `json:"source_revision"`
	ContextURI           string                `json:"context_uri"`
	ContextDigest        string                `json:"context_digest"`
	HarnessVersion       string                `json:"harness_version"`
	Limits               Limits                `json:"limits"`
	CredentialReferences []CredentialReference `json:"credential_references"`
	NetworkPolicy        string                `json:"network_policy"`
	FilesystemPolicy     string                `json:"filesystem_policy"`
	AuthorityFence       int64                 `json:"authority_fence"`
	DeadlineAt           time.Time             `json:"deadline_at"`
	PolicyVersion        string                `json:"policy_version"`
}

func (specification Specification) Validate(now time.Time) error {
	if specification.Stage != "" && ((specification.Stage != "repair_plan" && specification.Stage != "review") || specification.Engine == nil ||
		(specification.Workload != "" && specification.Workload != "repository/v1" && specification.Workload != "repository/v2")) {
		return fmt.Errorf("unsupported runtime stage: %w", ErrInvalidInput)
	}
	if specification.Engine != nil {
		if err := specification.Engine.Validate(); err != nil {
			return err
		}
		for _, digest := range []string{specification.ProfileDigest, strings.TrimPrefix(specification.ContextDigest, "sha256:")} {
			if !validEngineDigest(digest) {
				return fmt.Errorf("engine inputs must be digest-bound: %w", ErrInvalidInput)
			}
		}
		if messageDigest, ok := strings.CutPrefix(specification.ContextURI, "artifact://sha256:"); !ok || !validEngineDigest(messageDigest) {
			return fmt.Errorf("engine message must be an immutable artifact: %w", ErrInvalidInput)
		}
	}
	if len(specification.Tools) > 1 || len(specification.Tools) == 1 && (specification.Tools[0] != "read_context_source" || specification.Workload != "diagnostic-analysis/v1" || (specification.HarnessVersion != "langchain-create-agent/v2" && specification.HarnessVersion != "langchain-create-agent/kagent-byo-v1")) {
		return fmt.Errorf("unsupported runtime tool: %w", ErrInvalidInput)
	}
	required := []string{
		specification.ExecutionID, specification.AttemptID, specification.IdempotencyKey,
		specification.ContextURI,
		specification.ContextDigest, specification.HarnessVersion, specification.NetworkPolicy,
		specification.FilesystemPolicy, specification.PolicyVersion,
	}
	switch specification.Workload {
	case "", "repository/v1", "repository/v2":
		required = append(required, specification.RepositoryURI, specification.SourceRevision)
	case "diagnostic-analysis/v1":
		for _, digest := range []string{specification.InputDigest, specification.ProfileDigest} {
			decoded, err := hex.DecodeString(digest)
			if err != nil || len(decoded) != 32 {
				return fmt.Errorf("analysis inputs and profile must be digest-bound: %w", ErrInvalidInput)
			}
		}
		if specification.RepositoryURI != "" || specification.SourceRevision != "" || specification.WorkerImage == "" {
			return fmt.Errorf("analysis requires its worker image and no repository fields: %w", ErrInvalidInput)
		}
	case "conversation/v1":
		if specification.Engine == nil || specification.HarnessVersion != "conversation-byo/v1" || specification.WorkerImage != "" ||
			specification.RepositoryURI != "" || specification.SourceRevision != "" || len(specification.Tools) != 0 ||
			specification.NetworkPolicy != "gateway-only" || specification.FilesystemPolicy != "ephemeral" ||
			!validEngineDigest(specification.InputDigest) {
			return fmt.Errorf("conversation requires a pinned, read-only engine profile: %w", ErrInvalidInput)
		}
		if _, err := specification.EngineGatewayReference(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported workload: %w", ErrInvalidInput)
	}
	for _, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("runtime specification fields are required: %w", ErrInvalidInput)
		}
	}
	if specification.AuthorityFence < 1 || specification.DeadlineAt.IsZero() || !specification.DeadlineAt.After(now) {
		return fmt.Errorf("runtime authority and future deadline are required: %w", ErrInvalidInput)
	}
	limits := specification.Limits
	if limits.CPUMillis < 1 || limits.MemoryBytes < 1 || limits.EphemeralStorageBytes < 1 ||
		limits.WallTimeSeconds < 1 || limits.MaxProcesses < 1 {
		return fmt.Errorf("all runtime limits must be positive: %w", ErrInvalidInput)
	}
	seenKinds := make(map[string]struct{}, len(specification.CredentialReferences))
	for _, credential := range specification.CredentialReferences {
		kind := strings.TrimSpace(credential.Kind)
		reference := strings.TrimSpace(credential.Reference)
		if kind == "" || !strings.HasPrefix(reference, "secret://") || credential.ExpiresAt.IsZero() ||
			credential.ExpiresAt.After(specification.DeadlineAt) || !credential.ExpiresAt.After(now) {
			return fmt.Errorf("credentials must be scoped references that expire within runtime authority: %w", ErrInvalidInput)
		}
		if _, duplicate := seenKinds[kind]; duplicate {
			return fmt.Errorf("credential kind %q is duplicated: %w", kind, ErrInvalidInput)
		}
		seenKinds[kind] = struct{}{}
	}
	return nil
}

func (specification Specification) Digest() (string, error) {
	canonical := specification
	canonical.DeadlineAt = canonical.DeadlineAt.UTC()
	canonical.CredentialReferences = slices.Clone(canonical.CredentialReferences)
	for index := range canonical.CredentialReferences {
		canonical.CredentialReferences[index].ExpiresAt = canonical.CredentialReferences[index].ExpiresAt.UTC()
	}
	slices.SortFunc(canonical.CredentialReferences, func(left, right CredentialReference) int {
		return strings.Compare(left.Kind+"\x00"+left.Reference, right.Kind+"\x00"+right.Reference)
	})
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode runtime specification: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

type Usage struct {
	Tokens        int64 `json:"tokens"`
	CostMicros    int64 `json:"cost_micros"`
	CPUTimeMillis int64 `json:"cpu_time_millis"`
}

type OutputReference struct {
	Kind      string `json:"kind"`
	URI       string `json:"uri"`
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"size_bytes"`
}

type Attempt struct {
	ID                string            `json:"id"`
	ExecutionID       string            `json:"execution_id"`
	Number            int               `json:"number"`
	IdempotencyKey    string            `json:"idempotency_key"`
	Specification     Specification     `json:"specification"`
	SpecificationHash string            `json:"specification_digest"`
	ProviderHandle    string            `json:"provider_handle"`
	State             State             `json:"state"`
	HeartbeatAt       *time.Time        `json:"heartbeat_at,omitempty"`
	Usage             Usage             `json:"usage"`
	OutputReferences  []OutputReference `json:"output_references"`
	DeadlineAt        time.Time         `json:"deadline_at"`
	CleanupState      CleanupState      `json:"cleanup_state"`
	OutputsCollected  bool              `json:"outputs_collected"`
	FailureClass      string            `json:"failure_class"`
	FailureMessage    string            `json:"failure_message"`
	Version           int64             `json:"version"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type Observation struct {
	Found            bool              `json:"found"`
	ProviderHandle   string            `json:"provider_handle"`
	State            State             `json:"state"`
	HeartbeatAt      *time.Time        `json:"heartbeat_at,omitempty"`
	Usage            Usage             `json:"usage"`
	OutputReferences []OutputReference `json:"output_references"`
	Question         *Question         `json:"question,omitempty"`
	EngineStatus     string            `json:"engine_status,omitempty"`
	FailureClass     string            `json:"failure_class,omitempty"`
	FailureMessage   string            `json:"failure_message,omitempty"`
}

type Question struct {
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Choices []string `json:"choices,omitempty"`
}

// QuestionAnswerer is implemented only by runtimes that can correlate an
// answer with an already recorded pending operation.
type QuestionAnswerer interface {
	AnswerQuestion(context.Context, string, string, string) (Observation, error)
}

type Artifact struct {
	ID               string    `json:"id"`
	ExecutionID      string    `json:"execution_id"`
	RuntimeAttemptID string    `json:"runtime_attempt_id"`
	Name             string    `json:"name"`
	Kind             string    `json:"kind"`
	URI              string    `json:"uri"`
	Digest           string    `json:"digest"`
	SizeBytes        int64     `json:"size_bytes"`
	CreatedAt        time.Time `json:"created_at"`
}

type Capabilities struct {
	Resume bool
}

type Adapter interface {
	Start(context.Context, Specification) (Observation, error)
	Inspect(context.Context, string) (Observation, error)
	InspectByKey(context.Context, string) (Observation, error)
	Resume(context.Context, string) (Observation, error)
	Cancel(context.Context, string, bool) (Observation, error)
	CollectArtifacts(context.Context, string) ([]Artifact, error)
	Cleanup(context.Context, string) error
	Capabilities() Capabilities
}
