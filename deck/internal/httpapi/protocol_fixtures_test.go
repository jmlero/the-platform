package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

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

type fixtureShape struct {
	required []string
	optional []string
}

var fixtureShapes = map[string]fixtureShape{
	"plan": {
		required: []string{
			"id", "run_id", "revision", "plan", "context_digest", "risk_class",
			"verification_strategy", "expected_side_effects", "rollback_approach", "created_at",
		},
		optional: []string{"source_revision", "policy_version"},
	},
	"command": {
		required: []string{"id", "run_id", "sequence", "type", "status", "reason", "requested_by", "result", "run_version", "fence_token", "created_at", "updated_at"},
	},
	"budget": {
		required: []string{
			"time_minutes", "max_attempts", "max_tokens", "max_cost_minor", "currency", "max_concurrency", "authority",
		},
		optional: []string{"deadline_at"},
	},
	"evidence": {
		required: []string{
			"id", "task_id", "run_id", "plan_revision_id", "type", "producer", "verifier", "subject_digest",
			"source_revision", "command", "outcome", "provenance_uri", "provenance_digest", "observed_at",
			"artifact_uri", "artifact_digest", "admission_status", "created_at",
		},
		optional: []string{"expires_at", "policy_version"},
	},
}

func TestProtocolFixturesCoverCanonicalContract(t *testing.T) {
	fixtureRoot := filepath.Join("..", "..", "api", "fixtures", "v1")
	var manifest fixtureManifest
	decodeFixture(t, filepath.Join(fixtureRoot, "manifest.json"), &manifest)
	if manifest.ProtocolVersion != protocolVersion {
		t.Fatalf("fixture protocol version = %q, want %q", manifest.ProtocolVersion, protocolVersion)
	}
	if len(manifest.Fixtures) == 0 {
		t.Fatal("fixture manifest is empty")
	}

	contract, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read OpenAPI contract: %v", err)
	}
	contractText := string(contract)
	seenKinds := make(map[string]struct{}, len(manifest.Fixtures))
	errorCodes := make([]string, 0, len(protocolErrorCodes))

	for _, entry := range manifest.Fixtures {
		if _, duplicate := seenKinds[entry.Kind]; duplicate {
			t.Fatalf("fixture kind %q is listed more than once", entry.Kind)
		}
		seenKinds[entry.Kind] = struct{}{}
		if entry.ImplementationStatus != "implemented" && entry.ImplementationStatus != "planned" {
			t.Fatalf("fixture %q has implementation status %q", entry.Kind, entry.ImplementationStatus)
		}
		if !safeFixturePath(entry.Path) {
			t.Fatalf("fixture %q has unsafe path %q", entry.Kind, entry.Path)
		}
		if !strings.Contains(contractText, "\n    "+entry.Schema+":\n") {
			t.Fatalf("fixture %q references missing OpenAPI schema %q", entry.Kind, entry.Schema)
		}
		wirePath := "./fixtures/v1/" + filepath.ToSlash(entry.Path)
		if !strings.Contains(contractText, wirePath) {
			t.Fatalf("OpenAPI contract does not reference fixture %q", wirePath)
		}

		path := filepath.Join(fixtureRoot, filepath.FromSlash(entry.Path))
		switch entry.Kind {
		case "prd_task_plan":
			var fixture domain.PRDTaskPlan
			decodeFixture(t, path, &fixture)
			if fixture.Validate() != nil || fixture.Digest != fixture.ContentDigest() {
				t.Fatal("invalid PRD task fixture")
			}
		case "prd_document":
			var fixture domain.PRDDocument
			decodeFixture(t, path, &fixture)
			if fixture.Validate() != nil || fixture.Digest != fixture.ContentDigest() || fixture.MessageID == "" {
				t.Fatal("invalid PRD fixture")
			}
		case "task_landing":
			var fixture domain.TaskLanding
			decodeFixture(t, path, &fixture)
			if fixture.RunID == "" || fixture.Repository == "" || fixture.BaseBranch == "" || len(fixture.Commit) != 40 || fixture.ObservedAt <= 0 || len(fixture.EvidenceIDs) < 3 {
				t.Fatal("invalid task landing fixture")
			}
		case "task_review":
			var fixture domain.TaskReview
			decodeFixture(t, path, &fixture)
			if fixture.RunID == "" || fixture.AcceptedBy == "" || fixture.AcceptedAt <= 0 || fixture.TaskVersion < 1 {
				t.Fatal("invalid task review fixture")
			}

		case "thread":
			var fixture domain.ConversationThread
			decodeFixture(t, path, &fixture)
			if fixture.ID != "thr_example" || fixture.RootMessageID != "msg_example" || len(fixture.TaskIDs) != 1 {
				t.Fatalf("invalid thread fixture: %#v", fixture)
			}
		case "channel_message":
			var fixture domain.ChannelMessage
			decodeFixture(t, path, &fixture)
			if fixture.ID != "msg_example" || fixture.ThreadID != "thr_example" || fixture.PromotedTaskID == nil {
				t.Fatalf("invalid message fixture: %#v", fixture)
			}
		case "conversation_question":
			var fixture domain.ConversationQuestion
			decodeFixture(t, path, &fixture)
			if fixture.ID == "" || fixture.ThreadID == "" || fixture.InvocationMessageID == "" || fixture.QuestionID == "" || fixture.Status != "answered" || fixture.AnswerMessageID == "" || fixture.AnswerRequestID == "" {
				t.Fatalf("invalid conversation question fixture: %#v", fixture)
			}
		case "workloads":
			var fixture domain.WorkloadCatalog
			decodeFixture(t, path, &fixture)
			if len(fixture.Profiles) != 1 || fixture.Profiles[0].ID != "repository/v2" || fixture.Profiles[0].MinAttempts != 3 {
				t.Fatal("invalid workloads fixture")
			}
		case "context":
			var fixture domain.ResolvedContext
			decodeFixture(t, path, &fixture)
			if fixture.Selection == nil || len(fixture.Sources) != 1 || len(fixture.Selection.Sources) != 1 || fixture.Selection.Sources[0].Digest != fixture.Sources[0].Digest || fixture.Sources[0].Trust != "guidance" {
				t.Fatalf("context fixture = %#v", fixture)
			}
		case "repository":
			var fixture domain.RepositoryRequest
			decodeFixture(t, path, &fixture)
			if fixture.Status != "connected" || fixture.Binding == nil || fixture.Binding.Version < 1 || fixture.Binding.Repository != fixture.Repository {
				t.Fatalf("repository fixture = %#v", fixture)
			}
		case "agent":
			var fixture domain.Agent
			decodeFixture(t, path, &fixture)
			if fixture.ID == "" || fixture.Name == "" || fixture.CreatedAt == 0 {
				t.Fatalf("agent fixture = %#v", fixture)
			}
		case "task":
			var fixture domain.Task
			decodeFixture(t, path, &fixture)
			if fixture.ID == "" || fixture.ProjectID == "" || fixture.Status != domain.TaskReady || fixture.Version < 1 {
				t.Fatalf("task fixture = %#v", fixture)
			}
		case "run":
			var fixture domain.Run
			decodeFixture(t, path, &fixture)
			if fixture.ID == "" || fixture.TaskID == "" || fixture.AgentID == "" || fixture.Status != domain.RunPlanning {
				t.Fatalf("run fixture = %#v", fixture)
			}
		case "decision":
			var fixture domain.Decision
			decodeFixture(t, path, &fixture)
			if fixture.InteractionID == "" || fixture.AnswerMode != "options" || fixture.Status != "pending" {
				t.Fatalf("decision fixture = %#v", fixture)
			}
		default:
			if strings.HasPrefix(entry.Kind, "error.") {
				var fixture apiErrorResponse
				decodeFixture(t, path, &fixture)
				code := strings.TrimPrefix(entry.Kind, "error.")
				if fixture.Code != code || fixture.Error != fixture.Message || fixture.RequestID == "" || entry.HTTPStatus < 400 {
					t.Fatalf("error fixture %q = %#v with HTTP %d", entry.Kind, fixture, entry.HTTPStatus)
				}
				errorCodes = append(errorCodes, code)
				continue
			}
			shape, ok := fixtureShapes[entry.Kind]
			if !ok {
				t.Fatalf("fixture kind %q has no validator", entry.Kind)
			}
			validateFixtureShape(t, path, shape)
		}
	}

	for _, required := range []string{"task", "run", "plan", "command", "budget", "evidence", "context", "repository"} {
		if _, ok := seenKinds[required]; !ok {
			t.Errorf("fixture manifest is missing %q", required)
		}
	}
	sort.Strings(errorCodes)
	if !reflect.DeepEqual(errorCodes, protocolErrorCodes) {
		t.Fatalf("fixture error codes = %#v, protocol codes = %#v", errorCodes, protocolErrorCodes)
	}
}

func decodeFixture(t *testing.T, path string, destination any) {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		t.Fatalf("decode fixture %s: %v", path, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("fixture %s contains trailing JSON: %v", path, err)
	}
}

func validateFixtureShape(t *testing.T, path string, shape fixtureShape) {
	t.Helper()
	var object map[string]json.RawMessage
	decodeFixture(t, path, &object)
	allowed := make(map[string]struct{}, len(shape.required)+len(shape.optional))
	for _, field := range append(append([]string(nil), shape.required...), shape.optional...) {
		allowed[field] = struct{}{}
	}
	for _, field := range shape.required {
		value, ok := object[field]
		if !ok || len(bytes.TrimSpace(value)) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			t.Errorf("fixture %s is missing required field %q", path, field)
		}
	}
	for field := range object {
		if _, ok := allowed[field]; !ok {
			t.Errorf("fixture %s has unknown field %q", path, field)
		}
	}
}

func safeFixturePath(path string) bool {
	clean := filepath.Clean(filepath.FromSlash(path))
	return path != "" && !filepath.IsAbs(clean) && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}
