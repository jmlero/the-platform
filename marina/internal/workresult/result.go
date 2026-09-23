// Package workresult defines the bounded output accepted from either worker
// transport. A producing agent never decides whether its own result is evidence.
package workresult

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"reflect"
	"strings"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/jmlero/the-platform/marina/internal/analysis"
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
	Mode    string `json:"mode"`
	Delete  bool   `json:"delete,omitempty"`
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
	Analysis       *analysis.Report `json:"analysis,omitempty"`
	InputDigest    string           `json:"input_digest,omitempty"`
	ProfileDigest  string           `json:"profile_digest,omitempty"`
	Stage          string           `json:"stage"`
	SourceRevision string           `json:"source_revision"`
	Summary        string           `json:"summary"`
	Changes        []Change         `json:"changes,omitempty"`
	Checks         []CheckResult    `json:"checks,omitempty"`
	Tokens         int64            `json:"tokens"`
	ObservedAt     time.Time        `json:"observed_at"`
}

type Expected struct {
	Stage          string
	SourceRevision string
	InputDigest    string
	ProfileDigest  string
	AllowedPaths   []string
	Checks         []Check
	NotBefore      time.Time
	Now            time.Time
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
		if result.Analysis != nil || result.InputDigest != "" || result.ProfileDigest != "" || len(result.Checks) != 0 || len(result.Changes) == 0 || len(result.Changes) > 30 {
			return Result{}, errors.New("invalid implementation result")
		}
		seen := map[string]bool{}
		total := 0
		for _, change := range result.Changes {
			total += len(change.Content)
			if seen[change.Path] || !AllowedPath(want.AllowedPaths, change.Path) || (change.Mode != "100644" && change.Mode != "100755") ||
				(change.Delete && len(change.Content) != 0) || total > 2<<20 {
				return Result{}, errors.New("change exceeds the authorized boundary")
			}
			seen[change.Path] = true
		}
	case "verify":
		if result.Analysis != nil || result.InputDigest != "" || result.ProfileDigest != "" || len(result.Changes) != 0 || len(result.Checks) != len(want.Checks) || len(want.Checks) == 0 {
			return Result{}, errors.New("incomplete verification result")
		}
		for index, check := range result.Checks {
			expected := want.Checks[index]
			if check.ID != expected.ID || check.Directory != expected.Directory || !reflect.DeepEqual(check.Command, expected.Command) ||
				check.OutputDigest != artifacts.Digest([]byte(check.Output)) || check.ExitCode < 0 {
				return Result{}, errors.New("verification result differs from trusted checks")
			}
		}
	case "analyze":
		if result.Analysis == nil || result.InputDigest != want.InputDigest || result.ProfileDigest != want.ProfileDigest ||
			result.SourceRevision != "" || len(result.Changes) != 0 || len(result.Checks) != 0 {
			return Result{}, errors.New("analysis result differs from frozen input and profile")
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
