// Package workresult defines the bounded output accepted from either worker
// transport. A producing agent never decides whether its own result is evidence.
package workresult

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"reflect"
	"strings"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"google.golang.org/protobuf/encoding/protojson"
)

const MaxResultBytes = 4 << 20

type Check struct {
	ID        string   `json:"id"`
	Directory string   `json:"directory"`
	Command   []string `json:"command"`
}

type Change struct {
	Path    string `json:"path"`
	Content []byte `json:"content,omitempty"`
	// Text is the complete file as UTF-8. A native engine writes it instead of
	// base64 content; Parse turns it into Content.
	Text   *string `json:"text,omitempty"`
	Mode   string  `json:"mode"`
	Delete bool    `json:"delete,omitempty"`
}

type CheckResult struct {
	ID           string   `json:"id"`
	Command      []string `json:"command"`
	Directory    string   `json:"directory"`
	ExitCode     int      `json:"exit_code"`
	Output       string   `json:"output"`
	OutputDigest string   `json:"output_digest"`
}

type Result struct {
	BlockingReason string        `json:"blocking_reason,omitempty"`
	InputDigest    string        `json:"input_digest,omitempty"`
	ProfileDigest  string        `json:"profile_digest,omitempty"`
	Stage          string        `json:"stage"`
	SourceRevision string        `json:"source_revision"`
	Summary        string        `json:"summary"`
	Changes        []Change      `json:"changes,omitempty"`
	Checks         []CheckResult `json:"checks,omitempty"`
	Tokens         int64         `json:"tokens"`
	ObservedAt     time.Time     `json:"observed_at"`
	Repair         *RepairPlan   `json:"repair,omitempty"`
	Review         *Review       `json:"review,omitempty"`
}

// RepairPlan is a proposal, not authority. Marina records it before starting
// implementation and compares it with the exact failure and earlier attempts.
type RepairPlan struct {
	Diagnosis     string `json:"diagnosis"`
	Approach      string `json:"approach"`
	FailureDigest string `json:"failure_digest"`
	CanRepair     bool   `json:"can_repair"`
	Disputed      bool   `json:"disputed,omitempty"`
}

type Expected struct {
	AllowBlockReason bool
	Stage            string
	SourceRevision   string
	InputDigest      string
	ProfileDigest    string
	PatchDigest      string
	AllowedPaths     []string
	Checks           []Check
	NotBefore        time.Time
	Now              time.Time
}

func AllowedPath(scopes []string, name string) bool {
	if name == "" || name == "." || path.IsAbs(name) || path.Clean(name) != name || strings.ContainsAny(name, "\\\x00\r\n") || name == ".." || strings.HasPrefix(name, "../") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".git" || part == ".codex" || part == ".marina" || part == "AGENTS.md" {
			return false
		}
	}
	if strings.HasPrefix(name, ".github/workflows/") {
		return false
	}
	for _, scope := range scopes {
		if scope == "." || name == scope || strings.HasPrefix(name, strings.TrimSuffix(scope, "/")+"/") {
			return true
		}
	}
	return false
}

// Parse validates actual result bytes against the frozen attempt and trusted
// checks. A nonzero check exit remains a valid observation, but cannot pass the
// workflow's acceptance gate.
func Parse(data []byte, want Expected) (Result, error) {
	var result Result
	if len(data) == 0 || len(data) > MaxResultBytes {
		return result, errors.New("result size is outside the bounded contract")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return Result{}, fmt.Errorf("decode result: %w", err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Result{}, errors.New("one result document is required")
	}
	if result.Stage != want.Stage || result.SourceRevision != want.SourceRevision || result.ObservedAt.IsZero() ||
		(!want.NotBefore.IsZero() && result.ObservedAt.Before(want.NotBefore)) ||
		(!want.Now.IsZero() && result.ObservedAt.After(want.Now.Add(time.Minute))) || result.Tokens < 0 {
		return Result{}, errors.New("result does not match the immutable attempt")
	}
	switch want.Stage {
	case "implement":
		if result.BlockingReason != "" {
			if !want.AllowBlockReason || len(result.BlockingReason) > 4096 || strings.TrimSpace(result.BlockingReason) == "" || len(result.Changes) != 0 || len(result.Checks) != 0 || result.Review != nil || result.Repair != nil || result.InputDigest != "" || result.ProfileDigest != "" {
				return Result{}, errors.New("only a pinned rebase may return a bounded blocker without changes")
			}
			return result, nil
		}
		if result.Review != nil || result.Repair != nil || result.InputDigest != "" || result.ProfileDigest != "" || len(result.Checks) != 0 || len(result.Changes) == 0 || len(result.Changes) > 30 {
			return Result{}, errors.New("invalid implementation result")
		}
		seen := map[string]bool{}
		total := 0
		for i := range result.Changes {
			change := &result.Changes[i]
			if change.Text != nil {
				if len(change.Content) != 0 || change.Delete {
					return Result{}, errors.New("change must carry either text or content")
				}
				change.Content, change.Text = []byte(*change.Text), nil
			}
			total += len(change.Content)
			if seen[change.Path] || !AllowedPath(want.AllowedPaths, change.Path) || (change.Mode != "100644" && change.Mode != "100755") ||
				(change.Delete && len(change.Content) != 0) || total > 2<<20 {
				return Result{}, errors.New("change exceeds the authorized boundary")
			}
			seen[change.Path] = true
		}
	case "verify":
		if result.BlockingReason != "" || result.Review != nil || result.Repair != nil || result.InputDigest != "" || result.ProfileDigest != "" || len(result.Changes) != 0 || len(result.Checks) != len(want.Checks) || len(want.Checks) == 0 {
			return Result{}, errors.New("incomplete verification result")
		}
		for index, check := range result.Checks {
			expected := want.Checks[index]
			if check.ID != expected.ID || check.Directory != expected.Directory || !reflect.DeepEqual(check.Command, expected.Command) ||
				check.OutputDigest != artifacts.Digest([]byte(check.Output)) || check.ExitCode < 0 {
				return Result{}, errors.New("verification result differs from trusted checks")
			}
		}
	case "repair_plan":
		if result.BlockingReason != "" || result.Review != nil || result.Repair == nil || result.InputDigest != "" || result.ProfileDigest != "" || len(result.Changes) != 0 || len(result.Checks) != 0 || result.Tokens != 0 {
			return Result{}, errors.New("repair planning must return only a diagnosis and proposal")
		}
		plan := result.Repair
		digest, err := hex.DecodeString(strings.TrimPrefix(plan.FailureDigest, "sha256:"))
		if err != nil || len(digest) != 32 || !strings.HasPrefix(plan.FailureDigest, "sha256:") ||
			strings.TrimSpace(plan.Diagnosis) == "" || len(plan.Diagnosis) > 4096 || len(plan.Approach) > 4096 ||
			(plan.CanRepair && strings.TrimSpace(plan.Approach) == "") {
			return Result{}, errors.New("repair planning requires bounded diagnosis, failure digest and a new approach")
		}
	case "review":
		if result.BlockingReason != "" || result.Review == nil || result.Repair != nil || len(result.Changes) != 0 || len(result.Checks) != 0 || result.Tokens != 0 ||
			result.InputDigest != want.InputDigest || result.ProfileDigest != want.ProfileDigest {
			return Result{}, errors.New("review must contain only verdicts bound to immutable inputs")
		}
		if err := result.Review.Validate(want.PatchDigest); err != nil {
			return Result{}, err
		}
	default:
		return Result{}, errors.New("unsupported result stage")
	}
	return result, nil
}

// ParseA2A accepts either Marina's named inline byte artifact or the pinned
// native Codex harness's unnamed text artifact containing one complete result
// document. URLs and free-form success claims are never accepted as evidence.
// The caller stores the validated Result as a digest-bound artifact.
func ParseA2A(envelope []byte, want Expected) (Result, error) {
	if len(envelope) == 0 || len(envelope) > MaxResultBytes {
		return Result{}, errors.New("engine artifact exceeds the bounded contract")
	}
	var artifact a2a.Artifact
	if err := protojson.Unmarshal(envelope, &artifact); err != nil {
		return Result{}, fmt.Errorf("decode engine artifact: %w", err)
	}
	if artifact.Name == "marina-result.json" && len(artifact.Parts) == 1 && artifact.Parts[0].MediaType == "application/json" {
		raw, ok := artifact.Parts[0].Content.(*a2a.Part_Raw)
		if !ok {
			return Result{}, errors.New("engine result must contain inline bytes")
		}
		return Parse(raw.Raw, want)
	}
	if artifact.Name != "" || len(artifact.Parts) == 0 {
		return Result{}, errors.New("engine artifact does not contain the selected result")
	}
	var text strings.Builder
	for _, part := range artifact.Parts {
		if part == nil {
			return Result{}, errors.New("native result contains a missing part")
		}
		value, ok := part.Content.(*a2a.Part_Text)
		if !ok || part.MediaType != "" && part.MediaType != "application/json" {
			return Result{}, errors.New("native result contains a non-text part")
		}
		text.WriteString(value.Text)
		if text.Len() > MaxResultBytes {
			return Result{}, errors.New("native result exceeds the bounded contract")
		}
	}
	return Parse([]byte(text.String()), want)
}
