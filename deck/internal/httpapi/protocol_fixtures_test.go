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
	"lease": {
		required: []string{"run_id", "agent_id", "version", "fence_token", "expires_at", "renewed_at"},
	},
	"plan": {
		required: []string{
			"id", "run_id", "revision", "task_version", "context_digest", "risk_class", "summary",
			"steps", "verification_strategy", "expected_side_effects", "rollback_approach", "policy_version", "created_at",
		},
	},
	"command": {
		required: []string{"id", "run_id", "sequence", "type", "status", "reason", "requested_by", "result", "run_version", "fence_token", "created_at", "updated_at"},
	},
	"event": {
		required: []string{
			"id", "sequence", "type", "schema_version", "producer", "project_id", "task_id", "run_id",
			"execution_id", "correlation_id", "causation_id", "occurred_at", "ingested_at", "severity",
			"task_version", "source_revision", "policy_version", "payload",
		},
		optional: []string{"attempt_id"},
	},
	"budget": {
		required: []string{
			"deadline_at", "max_attempts", "max_tokens", "max_cost_minor", "currency", "max_concurrency", "authority",
		},
	},
	"evidence": {
		required: []string{
			"id", "run_id", "type", "producer", "verifier", "subject_digest", "source_revision", "command",
			"outcome", "provenance_uri", "provenance_digest", "observed_at", "policy_version",
		},
		optional: []string{"expires_at"},
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
		case "provider_info":
			var fixture providerInfoResponse
			decodeFixture(t, path, &fixture)
			if fixture.Provider != providerName || fixture.ProtocolVersion != protocolVersion || !fixture.Ready {
				t.Fatalf("provider fixture = %#v", fixture)
			}
			if !reflect.DeepEqual(fixture.Capabilities, protocolCapabilities) {
				t.Fatalf("provider fixture capabilities = %#v, want %#v", fixture.Capabilities, protocolCapabilities)
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

	for _, required := range []string{"task", "run", "lease", "plan", "command", "event", "budget", "evidence"} {
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
