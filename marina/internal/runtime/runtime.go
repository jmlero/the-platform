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
	if specification.Engine != nil {
		if err := specification.Engine.Validate(); err != nil {
			return err
		}
		for _, digest := range []string{specification.ProfileDigest, strings.TrimPrefix(specification.ContextDigest, "sha256:")} {
			if !validEngineDigest(digest) {
				return fmt.Errorf("engine inputs must be digest-bound: %w", ErrInvalidInput)
			}
		}
	}
	if len(specification.Tools) > 1 || len(specification.Tools) == 1 && (specification.Tools[0] != "read_context_source" || specification.Workload != "diagnostic-analysis/v1" || specification.HarnessVersion != "langchain-create-agent/v2") {
		return fmt.Errorf("unsupported runtime tool: %w", ErrInvalidInput)
	}
	required := []string{
		specification.ExecutionID, specification.AttemptID, specification.IdempotencyKey,
		specification.ContextURI,
		specification.ContextDigest, specification.HarnessVersion, specification.NetworkPolicy,
		specification.FilesystemPolicy, specification.PolicyVersion,
	}
	switch specification.Workload {
	case "", "repository/v1":
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

type RecoveryAction string

const (
	RecoveryNone                  RecoveryAction = "none"
	RecoveryStart                 RecoveryAction = "start"
	RecoveryInspect               RecoveryAction = "inspect"
	RecoveryInspectByKey          RecoveryAction = "inspect_by_key"
	RecoveryBindDiscovered        RecoveryAction = "bind_discovered"
	RecoveryFailDisappeared       RecoveryAction = "fail_provider_disappeared"
	RecoveryResume                RecoveryAction = "resume"
	RecoveryFailUnsupportedResume RecoveryAction = "fail_unsupported_resume"
	RecoveryCancel                RecoveryAction = "cancel"
	RecoveryForceCancel           RecoveryAction = "force_cancel"
	RecoveryCollectArtifacts      RecoveryAction = "collect_artifacts"
	RecoveryCleanup               RecoveryAction = "cleanup"
)

type RecoveryRequest struct {
	Resume         bool
	Cancel         bool
	CancelDeadline time.Time
}

type RecoveryPlan struct {
	Action RecoveryAction
	Reason string
}

func PlanRecovery(attempt Attempt, observation Observation, request RecoveryRequest, capabilities Capabilities, now time.Time) RecoveryPlan {
	if attempt.CleanupState == CleanupCompleted {
		return RecoveryPlan{Action: RecoveryNone, Reason: "runtime cleanup is complete"}
	}
	if request.Cancel && !attempt.State.Terminal() {
		if !request.CancelDeadline.IsZero() && !now.Before(request.CancelDeadline) {
			return RecoveryPlan{Action: RecoveryForceCancel, Reason: "graceful cancellation deadline expired"}
		}
		return RecoveryPlan{Action: RecoveryCancel, Reason: "cancellation was requested"}
	}
	if attempt.State.Terminal() {
		if !attempt.OutputsCollected {
			return RecoveryPlan{Action: RecoveryCollectArtifacts, Reason: "terminal runtime outputs have not been collected"}
		}
		return RecoveryPlan{Action: RecoveryCleanup, Reason: "terminal runtime requires idempotent cleanup"}
	}
	if request.Resume && attempt.State == StatePaused {
		if !capabilities.Resume {
			return RecoveryPlan{Action: RecoveryFailUnsupportedResume, Reason: "adapter does not support safe resume"}
		}
		return RecoveryPlan{Action: RecoveryResume, Reason: "paused runtime can be resumed"}
	}
	if observation.Found && attempt.ProviderHandle == "" {
		return RecoveryPlan{Action: RecoveryBindDiscovered, Reason: "provider object was discovered by the persisted start key"}
	}
	switch attempt.State {
	case StatePending:
		return RecoveryPlan{Action: RecoveryStart, Reason: "persisted runtime attempt has not started"}
	case StateStarting:
		if !observation.Found {
			return RecoveryPlan{Action: RecoveryInspectByKey, Reason: "start response may have been lost; inspect by idempotency key"}
		}
		return RecoveryPlan{Action: RecoveryInspect, Reason: "runtime start is still converging"}
	case StateRunning, StatePaused:
		if attempt.ProviderHandle != "" && !observation.Found {
			return RecoveryPlan{Action: RecoveryFailDisappeared, Reason: "previously bound provider runtime disappeared"}
		}
		return RecoveryPlan{Action: RecoveryInspect, Reason: "active runtime must be inspected"}
	default:
		return RecoveryPlan{Action: RecoveryNone, Reason: "runtime has no recovery action"}
	}
}
