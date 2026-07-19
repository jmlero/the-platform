package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

type Fixtures struct {
	Provider ProviderFixture
	Agent    AgentFixture
	Task     TaskFixture
	Run      RunFixture
	Plan     PlanFixture
	Event    EventFixture
	Errors   map[string]ErrorFixture
}

type ProviderFixture struct {
	Provider        string            `json:"provider"`
	ProtocolVersion string            `json:"protocol_version"`
	Capabilities    []string          `json:"capabilities"`
	ServerTime      int64             `json:"server_time"`
	Ready           bool              `json:"ready"`
	Readiness       map[string]string `json:"readiness"`
}

type AgentFixture struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities"`
	LastSeenAt   int64    `json:"last_seen_at"`
	CreatedAt    int64    `json:"created_at"`
}

type TaskFixture struct {
	ID                 string `json:"id"`
	ProjectID          string `json:"project_id"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
	Status             string `json:"status"`
	Priority           string `json:"priority"`
	BudgetMinutes      int    `json:"budget_minutes"`
	Version            int    `json:"version"`
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `json:"updated_at"`
}

type RunFixture struct {
	ID             string `json:"id"`
	TaskID         string `json:"task_id"`
	AgentID        string `json:"agent_id"`
	Status         string `json:"status"`
	Plan           string `json:"plan"`
	ResultSummary  string `json:"result_summary"`
	LeaseExpiresAt int64  `json:"lease_expires_at"`
	StartedAt      int64  `json:"started_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

type PlanFixture struct {
	ID                   string            `json:"id"`
	RunID                string            `json:"run_id"`
	Revision             int               `json:"revision"`
	TaskVersion          int               `json:"task_version"`
	ContextDigest        string            `json:"context_digest"`
	RiskClass            string            `json:"risk_class"`
	Summary              string            `json:"summary"`
	Steps                []PlanStepFixture `json:"steps"`
	VerificationStrategy []string          `json:"verification_strategy"`
	ExpectedSideEffects  []string          `json:"expected_side_effects"`
	RollbackApproach     string            `json:"rollback_approach"`
	PolicyVersion        string            `json:"policy_version"`
	CreatedAt            int64             `json:"created_at"`
}

type PlanStepFixture struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type EventFixture struct {
	ID             string         `json:"id"`
	Sequence       int64          `json:"sequence"`
	Type           string         `json:"type"`
	SchemaVersion  string         `json:"schema_version"`
	Producer       string         `json:"producer"`
	ProjectID      string         `json:"project_id"`
	TaskID         string         `json:"task_id"`
	RunID          string         `json:"run_id"`
	ExecutionID    string         `json:"execution_id"`
	AttemptID      string         `json:"attempt_id,omitempty"`
	CorrelationID  string         `json:"correlation_id"`
	CausationID    string         `json:"causation_id"`
	OccurredAt     int64          `json:"occurred_at"`
	IngestedAt     int64          `json:"ingested_at"`
	Severity       string         `json:"severity"`
	TaskVersion    int            `json:"task_version"`
	SourceRevision string         `json:"source_revision"`
	PolicyVersion  string         `json:"policy_version"`
	Payload        map[string]any `json:"payload"`
}

type ErrorFixture struct {
	Error      string `json:"error"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable"`
	RequestID  string `json:"request_id"`
	HTTPStatus int    `json:"-"`
}

type fixtureManifest struct {
	ProtocolVersion string                 `json:"protocol_version"`
	Fixtures        []fixtureManifestEntry `json:"fixtures"`
}

type fixtureManifestEntry struct {
	Kind                 string `json:"kind"`
	Schema               string `json:"schema"`
	Path                 string `json:"path"`
	ImplementationStatus string `json:"implementation_status"`
	HTTPStatus           int    `json:"http_status,omitempty"`
}

var requiredFixtureKinds = []string{
	"agent",
	"budget",
	"command",
	"event",
	"evidence",
	"lease",
	"plan",
	"provider_info",
	"run",
	"task",
}

var expectedErrorCodes = []string{
	"conflict",
	"internal_error",
	"invalid_input",
	"invalid_transition",
	"not_found",
}

func LoadFixtures(root string) (Fixtures, error) {
	if strings.TrimSpace(root) == "" {
		return Fixtures{}, fmt.Errorf("fixture directory is required")
	}
	var manifest fixtureManifest
	if err := decodeFile(filepath.Join(root, "manifest.json"), &manifest); err != nil {
		return Fixtures{}, fmt.Errorf("load fixture manifest: %w", err)
	}
	if manifest.ProtocolVersion != workcontrol.ProtocolVersion {
		return Fixtures{}, fmt.Errorf(
			"fixture protocol is %q, want %q",
			manifest.ProtocolVersion,
			workcontrol.ProtocolVersion,
		)
	}

	fixtures := Fixtures{Errors: make(map[string]ErrorFixture)}
	seen := make(map[string]struct{}, len(manifest.Fixtures))
	for _, entry := range manifest.Fixtures {
		if entry.Kind == "" || entry.Schema == "" || entry.Path == "" {
			return Fixtures{}, fmt.Errorf("fixture manifest entry is incomplete: %#v", entry)
		}
		if _, duplicate := seen[entry.Kind]; duplicate {
			return Fixtures{}, fmt.Errorf("fixture kind %q is duplicated", entry.Kind)
		}
		seen[entry.Kind] = struct{}{}
		if entry.ImplementationStatus != "implemented" && entry.ImplementationStatus != "planned" {
			return Fixtures{}, fmt.Errorf("fixture %q has invalid implementation status %q", entry.Kind, entry.ImplementationStatus)
		}
		path, err := fixturePath(root, entry.Path)
		if err != nil {
			return Fixtures{}, fmt.Errorf("fixture %q: %w", entry.Kind, err)
		}

		switch entry.Kind {
		case "provider_info":
			err = decodeFile(path, &fixtures.Provider)
		case "agent":
			err = decodeFile(path, &fixtures.Agent)
		case "task":
			err = decodeFile(path, &fixtures.Task)
		case "run":
			err = decodeFile(path, &fixtures.Run)
		case "plan":
			err = decodeFile(path, &fixtures.Plan)
		case "event":
			err = decodeFile(path, &fixtures.Event)
		default:
			if strings.HasPrefix(entry.Kind, "error.") {
				code := strings.TrimPrefix(entry.Kind, "error.")
				var fixture ErrorFixture
				err = decodeFile(path, &fixture)
				fixture.HTTPStatus = entry.HTTPStatus
				if err == nil && fixture.Code != code {
					err = fmt.Errorf("error code is %q, want %q", fixture.Code, code)
				}
				fixtures.Errors[code] = fixture
			} else {
				var object map[string]json.RawMessage
				err = decodeFile(path, &object)
				if err == nil && len(object) == 0 {
					err = fmt.Errorf("fixture is an empty object")
				}
			}
		}
		if err != nil {
			return Fixtures{}, fmt.Errorf("load fixture %q: %w", entry.Kind, err)
		}
	}

	for _, kind := range requiredFixtureKinds {
		if _, ok := seen[kind]; !ok {
			return Fixtures{}, fmt.Errorf("fixture manifest is missing %q", kind)
		}
	}
	errorCodes := make([]string, 0, len(fixtures.Errors))
	for code, fixture := range fixtures.Errors {
		if fixture.Message == "" || fixture.Error != fixture.Message || fixture.RequestID == "" || fixture.HTTPStatus < 400 {
			return Fixtures{}, fmt.Errorf("error fixture %q is incomplete", code)
		}
		errorCodes = append(errorCodes, code)
	}
	sort.Strings(errorCodes)
	if !equalStrings(errorCodes, expectedErrorCodes) {
		return Fixtures{}, fmt.Errorf("fixture error codes are %v, want %v", errorCodes, expectedErrorCodes)
	}
	if fixtures.Provider.Provider == "" || fixtures.Provider.ProtocolVersion != manifest.ProtocolVersion {
		return Fixtures{}, fmt.Errorf("provider fixture is incomplete")
	}
	if fixtures.Agent.ID == "" || fixtures.Task.Title == "" || fixtures.Run.Status == "" || fixtures.Plan.Summary == "" {
		return Fixtures{}, fmt.Errorf("core lifecycle fixtures are incomplete")
	}
	return fixtures, nil
}

func fixturePath(root string, relative string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe fixture path %q", relative)
	}
	return filepath.Join(root, clean), nil
}

func decodeFile(path string, destination any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("trailing JSON: %v", err)
	}
	return nil
}

func equalStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
