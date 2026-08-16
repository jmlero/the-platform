package contextpack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

const SchemaVersion = "marina-context/v1"

var ErrInvalidInput = errors.New("invalid context package input")

type Trust string

const (
	TrustTrusted   Trust = "trusted"
	TrustUntrusted Trust = "untrusted"
)

type Kind string

const (
	KindTaskSnapshot           Kind = "task_snapshot"
	KindRepositoryInstructions Kind = "repository_instructions"
	KindArchitectureDecision   Kind = "architecture_decision"
	KindPolicy                 Kind = "policy"
	KindSourceRevision         Kind = "source_revision"
	KindTool                   Kind = "tool"
	KindExternal               Kind = "external"
)

type Input struct {
	Kind       Kind       `json:"kind"`
	URI        string     `json:"uri"`
	Digest     string     `json:"digest"`
	Trust      Trust      `json:"trust"`
	Scope      string     `json:"scope"`
	ObservedAt time.Time  `json:"observed_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

type BuildInput struct {
	ProjectID      string            `json:"project_id"`
	ExecutionID    string            `json:"execution_id"`
	PlanRevisionID string            `json:"plan_revision_id"`
	PolicyVersion  string            `json:"policy_version"`
	SourceRevision string            `json:"source_revision"`
	HarnessVersion string            `json:"harness_version"`
	ToolVersions   map[string]string `json:"tool_versions"`
	Inputs         []Input           `json:"inputs"`
}

type Package struct {
	SchemaVersion  string            `json:"schema_version"`
	ProjectID      string            `json:"project_id"`
	ExecutionID    string            `json:"execution_id"`
	PlanRevisionID string            `json:"plan_revision_id"`
	PolicyVersion  string            `json:"policy_version"`
	SourceRevision string            `json:"source_revision"`
	HarnessVersion string            `json:"harness_version"`
	ToolVersions   map[string]string `json:"tool_versions"`
	Inputs         []Input           `json:"inputs"`
	CreatedAt      time.Time         `json:"created_at"`
	Digest         string            `json:"digest"`
}

type HarnessView struct {
	TrustedInstructionReferences []string `json:"trusted_instruction_references"`
	UntrustedDataReferences      []string `json:"untrusted_data_references"`
}

func Build(now time.Time, input BuildInput) (Package, error) {
	now = now.UTC()
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.ExecutionID = strings.TrimSpace(input.ExecutionID)
	input.PlanRevisionID = strings.TrimSpace(input.PlanRevisionID)
	input.PolicyVersion = strings.TrimSpace(input.PolicyVersion)
	input.SourceRevision = strings.TrimSpace(input.SourceRevision)
	input.HarnessVersion = strings.TrimSpace(input.HarnessVersion)
	if input.ProjectID == "" || input.ExecutionID == "" || input.PlanRevisionID == "" ||
		input.PolicyVersion == "" || !fullRevision(input.SourceRevision) || input.HarnessVersion == "" || now.IsZero() {
		return Package{}, fmt.Errorf("context identity and versions are required: %w", ErrInvalidInput)
	}
	if len(input.ToolVersions) == 0 || len(input.Inputs) == 0 {
		return Package{}, fmt.Errorf("tool versions and context inputs are required: %w", ErrInvalidInput)
	}
	for tool, version := range input.ToolVersions {
		if strings.TrimSpace(tool) == "" || strings.TrimSpace(version) == "" {
			return Package{}, fmt.Errorf("tool names and versions are required: %w", ErrInvalidInput)
		}
	}
	inputs := slices.Clone(input.Inputs)
	required := map[Kind]bool{
		KindTaskSnapshot: false, KindRepositoryInstructions: false, KindArchitectureDecision: false,
		KindPolicy: false, KindSourceRevision: false, KindTool: false,
	}
	for index := range inputs {
		item := &inputs[index]
		item.URI = strings.TrimSpace(item.URI)
		item.Digest = strings.TrimSpace(item.Digest)
		item.Scope = strings.TrimSpace(item.Scope)
		item.ObservedAt = item.ObservedAt.UTC()
		item.ExpiresAt = item.ExpiresAt.UTC()
		if item.RevokedAt != nil {
			value := item.RevokedAt.UTC()
			item.RevokedAt = &value
		}
		if item.URI == "" || !strings.HasPrefix(item.Digest, "sha256:") || item.Scope == "" ||
			(item.Trust != TrustTrusted && item.Trust != TrustUntrusted) || item.ObservedAt.IsZero() ||
			item.ObservedAt.After(now) || item.ExpiresAt.IsZero() || !item.ExpiresAt.After(now) || item.RevokedAt != nil {
			return Package{}, fmt.Errorf("context input %d has invalid provenance, freshness, trust, or revocation: %w", index, ErrInvalidInput)
		}
		if item.Kind == KindExternal && item.Trust != TrustUntrusted {
			return Package{}, fmt.Errorf("external context must remain untrusted data: %w", ErrInvalidInput)
		}
		if _, ok := required[item.Kind]; ok {
			required[item.Kind] = true
		}
	}
	for kind, present := range required {
		if !present {
			return Package{}, fmt.Errorf("required context kind %q is missing: %w", kind, ErrInvalidInput)
		}
	}
	slices.SortFunc(inputs, func(left, right Input) int {
		return strings.Compare(string(left.Kind)+"\x00"+left.URI+"\x00"+left.Digest,
			string(right.Kind)+"\x00"+right.URI+"\x00"+right.Digest)
	})
	manifest := Package{
		SchemaVersion: SchemaVersion, ProjectID: input.ProjectID, ExecutionID: input.ExecutionID,
		PlanRevisionID: input.PlanRevisionID, PolicyVersion: input.PolicyVersion,
		SourceRevision: input.SourceRevision, HarnessVersion: input.HarnessVersion,
		ToolVersions: maps.Clone(input.ToolVersions), Inputs: inputs, CreatedAt: now,
	}
	digest, err := manifest.contentDigest()
	if err != nil {
		return Package{}, err
	}
	manifest.Digest = digest
	return manifest, nil
}

func fullRevision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func (manifest Package) Validate() error {
	if manifest.SchemaVersion != SchemaVersion || manifest.Digest == "" {
		return ErrInvalidInput
	}
	want, err := manifest.contentDigest()
	if err != nil {
		return err
	}
	if want != manifest.Digest {
		return fmt.Errorf("context package digest mismatch: %w", ErrInvalidInput)
	}
	return nil
}

func (manifest Package) ValidateAt(now time.Time) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	now = now.UTC()
	if now.IsZero() {
		return ErrInvalidInput
	}
	for index, item := range manifest.Inputs {
		if item.RevokedAt != nil || item.ExpiresAt.IsZero() || !now.Before(item.ExpiresAt.UTC()) ||
			item.ObservedAt.IsZero() || item.ObservedAt.After(now) {
			return fmt.Errorf("context input %d is stale or revoked: %w", index, ErrInvalidInput)
		}
	}
	return nil
}

func (manifest Package) contentDigest() (string, error) {
	canonical := manifest
	canonical.Digest = ""
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode context package: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (manifest Package) HarnessView() HarnessView {
	view := HarnessView{
		TrustedInstructionReferences: make([]string, 0),
		UntrustedDataReferences:      make([]string, 0),
	}
	for _, item := range manifest.Inputs {
		if item.Trust == TrustUntrusted {
			view.UntrustedDataReferences = append(view.UntrustedDataReferences, item.URI)
		} else {
			view.TrustedInstructionReferences = append(view.TrustedInstructionReferences, item.URI)
		}
	}
	return view
}
