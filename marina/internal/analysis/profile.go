// Package analysis defines the bounded diagnostic workload independently of
// repositories, runtime providers, and any agent framework's internal types.
package analysis

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jmlero/the-platform/marina/internal/contextpack"
	"regexp"
	"strings"
)

const Kind = "diagnostic-analysis/v1"
const Harness = "langchain-create-agent/v1"
const HarnessWithContext = "langchain-create-agent/v2"
const HarnessBYO = "langchain-create-agent/kagent-byo-v1"

// Profile is an operator-installed immutable definition, never supplied by a
// worker. Expected findings stay in Marina and are not sent to the harness.
type Profile struct {
	Tools          []string `json:"tools,omitempty"`
	Version        int      `json:"version"`
	Revision       int      `json:"revision"`
	Image          string   `json:"image"`
	HarnessVersion string   `json:"harness_version"`
	MaxTokens      int64    `json:"max_tokens"`
	BudgetMinutes  int      `json:"budget_minutes"`
	Fixture        Fixture  `json:"fixture"`
}
type Source struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}
type Finding struct {
	Code        string `json:"code"`
	SourceID    string `json:"source_id"`
	Quote       string `json:"quote"`
	Explanation string `json:"explanation"`
}
type Fixture struct {
	ID       string    `json:"id"`
	Revision int       `json:"revision"`
	Sources  []Source  `json:"sources"`
	Expected []Finding `json:"expected"`
}
type Report struct {
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
}

// Input contains data and the frozen provenance binding, but no expected answers,
// upstream credentials, repository URL, or mutable external source.
type Input struct {
	Context       *contextpack.Package `json:"context,omitempty"`
	Tools         []string             `json:"tools,omitempty"`
	ToolURL       string               `json:"tool_url,omitempty"`
	Version       int                  `json:"version"`
	ProfileDigest string               `json:"profile_digest"`
	InputDigest   string               `json:"input_digest"`
	Sources       []Source             `json:"sources"`
	Model         string               `json:"model"`
	ModelURL      string               `json:"model_url"`
}

type Validation struct {
	ContextDigest string   `json:"context_digest,omitempty"`
	Version       int      `json:"version"`
	Validator     string   `json:"validator"`
	ProfileDigest string   `json:"profile_digest"`
	InputDigest   string   `json:"input_digest"`
	OutputDigest  string   `json:"output_digest"`
	Report        Report   `json:"report"`
	Sources       []Source `json:"sources"`
	Checks        []string `json:"checks"`
}

func Digest(value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (p Profile) Digest() string      { return Digest(p) }
func (f Fixture) InputDigest() string { return Digest(f.Sources) }

var imagePin = regexp.MustCompile(`^.+@sha256:[a-f0-9]{64}$`)
var identifier = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func (p Profile) Validate() error {
	if p.Version != 1 || p.Revision < 1 || (p.HarnessVersion != Harness && p.HarnessVersion != HarnessWithContext && p.HarnessVersion != HarnessBYO) || !imagePin.MatchString(p.Image) || p.MaxTokens < 10000 || p.MaxTokens > 1000000 || p.BudgetMinutes < 1 || p.BudgetMinutes > 60 {
		return errors.New("analysis profile requires version 1, the supported harness, a pinned image and bounded limits")
	}
	if len(p.Tools) > 1 || len(p.Tools) == 1 && (p.Tools[0] != "read_context_source" || p.HarnessVersion == Harness) {
		return errors.New("unsupported analysis tool")
	}
	f := p.Fixture
	if !identifier.MatchString(f.ID) || f.Revision < 1 || len(f.Sources) < 1 || len(f.Sources) > 16 || len(f.Expected) < 1 || len(f.Expected) > 16 {
		return errors.New("analysis fixture requires versioned inputs and expected findings")
	}
	seen := map[string]bool{}
	size := 0
	for _, s := range f.Sources {
		size += len(s.Content)
		if !identifier.MatchString(s.ID) || seen[s.ID] || len(s.Content) == 0 || len(s.Content) > 16<<10 {
			return errors.New("invalid diagnostic input")
		}
		seen[s.ID] = true
	}
	if size > 64<<10 {
		return errors.New("diagnostic inputs exceed 64 KiB")
	}
	return ValidateReport(f, Report{Summary: "Fixture consistency check", Findings: f.Expected})
}

func ValidateReport(f Fixture, r Report) error {
	if strings.TrimSpace(r.Summary) == "" || len(r.Summary) > 2000 || len(r.Findings) != len(f.Expected) {
		return errors.New("report must contain a bounded summary and every expected finding")
	}
	sources := map[string]string{}
	for _, s := range f.Sources {
		sources[s.ID] = s.Content
	}
	expected := map[string]Finding{}
	for _, want := range f.Expected {
		key := want.Code + ":" + want.SourceID
		if (want.Code != "provider_unavailable" && want.Code != "budget_exhausted") || want.Quote == "" || sources[want.SourceID] == "" || !strings.Contains(sources[want.SourceID], want.Quote) {
			return errors.New("expected finding lacks source-bound evidence")
		}
		if _, duplicate := expected[key]; duplicate {
			return errors.New("duplicate expected finding")
		}
		expected[key] = want
	}
	for _, got := range r.Findings {
		key := got.Code + ":" + got.SourceID
		want, ok := expected[key]
		if !ok || len(got.Quote) > 1000 || !strings.Contains(got.Quote, want.Quote) || !strings.Contains(sources[got.SourceID], got.Quote) || strings.TrimSpace(got.Explanation) == "" || len(got.Explanation) > 2000 {
			return errors.New("finding is missing, duplicated, unsupported, or has mismatched evidence")
		}
		delete(expected, key)
	}
	return nil
}

//go:embed fixtures/blocked-runs-v1.json
var diagnosticFixture []byte

// DiagnosticFixture is synthetic input, not a connection to production signals.
func DiagnosticFixture() Fixture {
	var fixture Fixture
	if err := json.Unmarshal(diagnosticFixture, &fixture); err != nil {
		panic(err)
	}
	return fixture
}
