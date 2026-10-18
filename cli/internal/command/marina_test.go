package command

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	marinaTestToken     = "marina-test-token"
	marinaTestNamespace = "factory-default"
	marinaRunsPath      = "/apis/marina.dev/v1/namespaces/factory-default/runs"
)

// fixture returns a canonical example from Marina's public contract.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "marina", "api", "fixtures", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return contents
}

func fixtureObject(t *testing.T, name string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(fixture(t, name), &value); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return value
}

func withPhase(t *testing.T, name, phase string) map[string]any {
	t.Helper()
	object := fixtureObject(t, name)
	object["status"] = map[string]any{"phase": phase}
	return object
}

func listOf(kind string, items ...map[string]any) map[string]any {
	return map[string]any{"apiVersion": "marina.dev/v1", "kind": kind, "items": items}
}

func writeMarinaJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if raw, ok := value.([]byte); ok {
		_, _ = w.Write(raw)
		return
	}
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

// fakeMarina serves the handler and rejects any request without the test token.
func fakeMarina(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+marinaTestToken {
			t.Errorf("Authorization = %q", got)
			writeMarinaJSON(t, w, http.StatusUnauthorized, map[string]any{"apiVersion": "v1", "kind": "Status", "status": "Failure", "reason": "Unauthorized", "code": 401})
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func marinaEnvironment(serverURL string) func(string) string {
	return func(name string) string {
		switch name {
		case "MARINA_URL":
			return serverURL
		case "MARINA_TOKEN":
			return marinaTestToken
		case "MARINA_NAMESPACE":
			return marinaTestNamespace
		default:
			return ""
		}
	}
}

func decodeRequest(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Content-Type = %q", contentType)
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return body
}

func field(t *testing.T, value any, path ...string) any {
	t.Helper()
	for _, key := range path {
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%v is not an object at %q", value, key)
		}
		value = object[key]
	}
	return value
}

func TestCapabilitiesShowsWhatTheInstallationOffers(t *testing.T) {
	server := fakeMarina(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/apis/marina.dev/v1/capabilities" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		writeMarinaJSON(t, w, http.StatusOK, fixture(t, "capabilities.json"))
	})

	code, stdout, stderr := runForTest(t, []string{"capabilities"}, marinaEnvironment(server.URL))
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"repository-change, conversation", "openai/gpt-6-luna", "implement, conversation", "search_traces", "120 minutes", "400000 tokens", "11 attempts"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q: %q", want, stdout)
		}
	}
}

func TestRunsCreateSendsRunWithContextDigest(t *testing.T) {
	contextPath := filepath.Join(t.TempDir(), "goals.md")
	content := "Keep the public interface stable\n"
	if err := os.WriteFile(contextPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	server := fakeMarina(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != marinaRunsPath {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		body := decodeRequest(t, r)
		checks := map[string]any{
			"apiVersion":              "marina.dev/v1",
			"kind":                    "Run",
			"metadata.name":           "task-0042",
			"spec.project":            "acceptance",
			"spec.workload":           "land-on-main",
			"spec.title":              "Reject an empty range",
			"spec.description":        "clamp(min, max) must reject min > max.",
			"spec.acceptanceCriteria": "A unit test covers min > max and passes.",
			"spec.budget.timeMinutes": float64(60),
			"spec.budget.maxTokens":   float64(200000),
			"spec.budget.maxAttempts": float64(7),
		}
		for path, want := range checks {
			if got := field(t, body, strings.Split(path, ".")...); got != want {
				t.Errorf("%s = %#v, want %#v", path, got, want)
			}
		}
		items, _ := field(t, body, "spec", "context").([]any)
		if len(items) != 1 {
			t.Fatalf("context = %#v", items)
		}
		item := items[0].(map[string]any)
		want := map[string]any{"name": "Project goals", "kind": "document", "trust": "data", "digest": "sha256:" + hex.EncodeToString(sum[:]), "content": content}
		for key, value := range want {
			if item[key] != value {
				t.Errorf("context %s = %#v, want %#v", key, item[key], value)
			}
		}
		if _, ok := body["status"]; ok {
			t.Error("request carries status")
		}
		writeMarinaJSON(t, w, http.StatusCreated, withPhase(t, "run-created.json", "Pending"))
	})

	code, stdout, stderr := runForTest(t, []string{
		"runs", "create", "task-0042",
		"--project", "acceptance",
		"--workload", "land-on-main",
		"--title", "Reject an empty range",
		"--description", "clamp(min, max) must reject min > max.",
		"--criteria", "A unit test covers min > max and passes.",
		"--minutes", "60", "--tokens", "200000", "--attempts", "7",
		"--context", "Project goals=" + contextPath,
	}, marinaEnvironment(server.URL))
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Created run task-0042") || !strings.Contains(stdout, "Pending") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func runCreateArgs(name string) []string {
	return []string{"runs", "create", name, "--project", "acceptance", "--title", "Reject an empty range", "--criteria", "A unit test covers min > max and passes.", "--minutes", "60", "--tokens", "200000", "--attempts", "7"}
}

func TestRunsCreateReplayWithSameSpecSucceeds(t *testing.T) {
	server := fakeMarina(t, func(w http.ResponseWriter, _ *http.Request) {
		writeMarinaJSON(t, w, http.StatusOK, fixture(t, "run-succeeded.json"))
	})

	code, stdout, stderr := runForTest(t, runCreateArgs("task-0042"), marinaEnvironment(server.URL))
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Run task-0042 already exists with the same spec") || !strings.Contains(stdout, "Succeeded") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestRunsCreateConflictShowsStatusMessageAndFails(t *testing.T) {
	server := fakeMarina(t, func(w http.ResponseWriter, _ *http.Request) {
		writeMarinaJSON(t, w, http.StatusConflict, fixture(t, "status-already-exists.json"))
	})

	code, stdout, stderr := runForTest(t, runCreateArgs("task-0042"), marinaEnvironment(server.URL))
	if code != 1 {
		t.Fatalf("code = %d, stdout = %q", code, stdout)
	}
	if !strings.Contains(stderr, `runs.marina.dev "task-0042" already exists with a different spec`) || !strings.Contains(stderr, "AlreadyExists") || !strings.Contains(stderr, "409") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRunsCreateValidatesBeforeCallingMarina(t *testing.T) {
	cases := map[string][]string{
		"missing budget": {"runs", "create", "task-1", "--project", "acceptance", "--title", "T", "--criteria", "C"},
		"invalid name":   append([]string{"runs", "create", "Task_1"}, runCreateArgs("x")[3:]...),
		"missing name":   runCreateArgs("")[:2],
		"missing title":  {"runs", "create", "task-1", "--project", "acceptance", "--criteria", "C", "--minutes", "1", "--tokens", "1", "--attempts", "1"},
		"bad context":    append(runCreateArgs("task-1"), "--context", "no-separator"),
		"missing file":   append(runCreateArgs("task-1"), "--context", "Doc="+filepath.Join(t.TempDir(), "absent")),
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			server := fakeMarina(t, func(http.ResponseWriter, *http.Request) { t.Error("Marina was called") })
			code, _, stderr := runForTest(t, args, marinaEnvironment(server.URL))
			if code != 2 {
				t.Fatalf("code = %d, stderr = %q", code, stderr)
			}
		})
	}
}

func TestRunsListShowsPhaseTokensAndResult(t *testing.T) {
	failed := withPhase(t, "run-created.json", "Blocked")
	failed["metadata"] = map[string]any{"name": "task-0043"}
	failed["status"].(map[string]any)["reason"] = "required check test failed"
	server := fakeMarina(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != marinaRunsPath {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		writeMarinaJSON(t, w, http.StatusOK, listOf("RunList", fixtureObject(t, "run-succeeded.json"), failed))
	})

	code, stdout, stderr := runForTest(t, []string{"runs", "list"}, marinaEnvironment(server.URL))
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "PHASE") {
		t.Fatalf("stdout = %q", stdout)
	}
	if fields := strings.Fields(lines[1]); strings.Join(fields, " ") != "task-0042 acceptance Succeeded 48210 7c1e0f2a9b8d4c3e5f6a7b8c9d0e1f2a3b4c5d6e" {
		t.Fatalf("succeeded row = %q", lines[1])
	}
	if !strings.Contains(lines[2], "Blocked") || !strings.Contains(lines[2], "required check test failed") {
		t.Fatalf("blocked row = %q", lines[2])
	}
}

func TestNamespaceFlagSelectsTheNamespacePath(t *testing.T) {
	server := fakeMarina(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/marina.dev/v1/namespaces/team-b/runs" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		writeMarinaJSON(t, w, http.StatusOK, listOf("RunList"))
	})

	code, stdout, stderr := runForTest(t, []string{"-n", "team-b", "runs", "list"}, marinaEnvironment(server.URL))
	if code != 0 || !strings.Contains(stdout, "No runs.") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestRunsGetShowsEvidence(t *testing.T) {
	server := fakeMarina(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != marinaRunsPath+"/task-0042" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		writeMarinaJSON(t, w, http.StatusOK, fixture(t, "run-succeeded.json"))
	})

	code, stdout, stderr := runForTest(t, []string{"runs", "get", "task-0042"}, marinaEnvironment(server.URL))
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"Succeeded", "48210", "3/7", "7c1e0f2a9b8d4c3e5f6a7b8c9d0e1f2a3b4c5d6e", "review", "Independent review passed the exact patch", "landing", "main fast-forwarded"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q: %q", want, stdout)
		}
	}
}

func TestRunsGetWritesTheFullObjectAsJSON(t *testing.T) {
	server := fakeMarina(t, func(w http.ResponseWriter, _ *http.Request) {
		writeMarinaJSON(t, w, http.StatusOK, fixture(t, "run-succeeded.json"))
	})

	code, stdout, stderr := runForTest(t, []string{"--json", "runs", "get", "task-0042"}, marinaEnvironment(server.URL))
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	var got, want any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}
	_ = json.Unmarshal(fixture(t, "run-succeeded.json"), &want)
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestRunsEventsReadsOnePageAfterSequence(t *testing.T) {
	server := fakeMarina(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != marinaRunsPath+"/task-0042/events" || r.URL.Query().Get("after") != "4" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		if accept := r.Header.Get("Accept"); accept != "application/json" {
			t.Errorf("Accept = %q", accept)
		}
		writeMarinaJSON(t, w, http.StatusOK, fixture(t, "event-page.json"))
	})

	code, stdout, stderr := runForTest(t, []string{"runs", "events", "task-0042", "--after", "4"}, marinaEnvironment(server.URL))
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "5 2026-10-17T18:12:00Z run.progress Implementing the approved change") ||
		!strings.Contains(stdout, "6 2026-10-17T18:31:00Z run.progress Reviewing the candidate patch before commit") {
		t.Fatalf("stdout = %q", stdout)
	}
}

// writeEventStream writes the fixture events as server-sent events, with a
// keepalive comment the client must ignore.
func writeEventStream(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(fixture(t, "event-page.json"), &page); err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ": keepalive\n\n")
	for _, item := range page.Items {
		var event struct {
			Sequence int    `json:"sequence"`
			Type     string `json:"type"`
		}
		_ = json.Unmarshal(item, &event)
		_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Type, item)
	}
	w.(http.Flusher).Flush()
}

func TestRunsEventsFollowStreamsUntilTheRunIsTerminal(t *testing.T) {
	previous := marinaFollowPollInterval
	marinaFollowPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { marinaFollowPollInterval = previous })

	var polls, drains atomic.Int32
	server := fakeMarina(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == marinaRunsPath+"/task-0042/events" && r.Header.Get("Accept") == "text/event-stream":
			if r.URL.Query().Get("after") != "4" {
				t.Errorf("stream after = %q", r.URL.Query().Get("after"))
			}
			writeEventStream(t, w)
			<-r.Context().Done()
		case r.URL.Path == marinaRunsPath+"/task-0042/events":
			drains.Add(1)
			if r.URL.Query().Get("after") != "6" {
				t.Errorf("drain after = %q", r.URL.Query().Get("after"))
			}
			writeMarinaJSON(t, w, http.StatusOK, map[string]any{"items": []any{}, "nextAfter": 6})
		case r.URL.Path == marinaRunsPath+"/task-0042":
			if polls.Add(1) < 4 {
				writeMarinaJSON(t, w, http.StatusOK, withPhase(t, "run-created.json", "Running"))
				return
			}
			writeMarinaJSON(t, w, http.StatusOK, fixture(t, "run-succeeded.json"))
		default:
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
	})

	code, stdout, stderr := runForTest(t, []string{"runs", "events", "task-0042", "--after", "4", "--follow"}, marinaEnvironment(server.URL))
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "5 2026-10-17T18:12:00Z run.progress") || !strings.Contains(stdout, "6 2026-10-17T18:31:00Z run.progress") {
		t.Fatalf("stdout = %q", stdout)
	}
	if strings.Count(stdout, "run.progress") != 2 || !strings.Contains(stdout, "Succeeded") {
		t.Fatalf("stdout = %q", stdout)
	}
	if drains.Load() == 0 {
		t.Fatal("remaining events were not read after the terminal phase")
	}
}

func TestRunsEventsFollowStopsWhenInterrupted(t *testing.T) {
	previous := marinaFollowPollInterval
	marinaFollowPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { marinaFollowPollInterval = previous })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var once sync.Once
	server := fakeMarina(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == marinaRunsPath+"/task-0042/events" {
			writeEventStream(t, w)
			once.Do(cancel)
			<-r.Context().Done()
			return
		}
		writeMarinaJSON(t, w, http.StatusOK, withPhase(t, "run-created.json", "Running"))
	})

	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, []string{"runs", "events", "task-0042", "--follow"}, &stdout, &stderr, marinaEnvironment(server.URL), "test")
	}()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("code = %d, stderr = %q", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("follow did not stop after interruption")
	}
}

func TestRunsCancelSendsAGeneratedIdempotencyKey(t *testing.T) {
	keys := make(chan string, 2)
	server := fakeMarina(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != marinaRunsPath+"/task-0042/cancel" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		keys <- r.Header.Get("Idempotency-Key")
		writeMarinaJSON(t, w, http.StatusOK, withPhase(t, "run-created.json", "Cancelled"))
	})

	for range 2 {
		code, stdout, stderr := runForTest(t, []string{"runs", "cancel", "task-0042"}, marinaEnvironment(server.URL))
		if code != 0 || !strings.Contains(stdout, "Cancel requested for run task-0042") || !strings.Contains(stdout, "Cancelled") {
			t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
		}
	}
	first, second := <-keys, <-keys
	if len(first) < 32 || first == second {
		t.Fatalf("keys = %q, %q", first, second)
	}
}

func TestTurnsCreateSendsAOneMessageThreadThatReplaysIdentically(t *testing.T) {
	bodies := make(chan map[string]any, 2)
	server := fakeMarina(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/apis/marina.dev/v1/namespaces/factory-default/turns" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		bodies <- decodeRequest(t, r)
		writeMarinaJSON(t, w, http.StatusCreated, withPhase(t, "turn-succeeded.json", "Pending"))
	})
	args := []string{"turns", "create", "msg-68a3a542", "--project", "acceptance", "--workload", "chat", "--message", "Which file defines isStrictlyWithinRange?"}

	for range 2 {
		code, stdout, stderr := runForTest(t, args, marinaEnvironment(server.URL))
		if code != 0 || !strings.Contains(stdout, "Created turn msg-68a3a542") {
			t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
		}
	}
	first, second := <-bodies, <-bodies
	if field(t, first, "kind") != "Turn" || field(t, first, "metadata", "name") != "msg-68a3a542" || field(t, first, "spec", "workload") != "chat" {
		t.Fatalf("body = %#v", first)
	}
	if id, _ := field(t, first, "spec", "thread", "id").(string); id == "" {
		t.Fatalf("thread = %#v", field(t, first, "spec", "thread"))
	}
	messages, _ := field(t, first, "spec", "thread", "messages").([]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	message := messages[0].(map[string]any)
	if message["role"] != "human" || message["body"] != "Which file defines isStrictlyWithinRange?" || message["id"] == "" {
		t.Fatalf("message = %#v", message)
	}
	if field(t, first, "spec", "budget", "timeMinutes") != float64(15) || field(t, first, "spec", "budget", "maxTokens") != float64(40000) {
		t.Fatalf("budget = %#v", field(t, first, "spec", "budget"))
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("replay changed the spec:\n%s\n%s", firstJSON, secondJSON)
	}
}

func TestTurnsGetShowsPhaseAndReply(t *testing.T) {
	server := fakeMarina(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/apis/marina.dev/v1/namespaces/factory-default/turns/msg-68a3a542" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		writeMarinaJSON(t, w, http.StatusOK, fixture(t, "turn-succeeded.json"))
	})

	code, stdout, stderr := runForTest(t, []string{"turns", "get", "msg-68a3a542"}, marinaEnvironment(server.URL))
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Succeeded") || !strings.Contains(stdout, "src/clamp.cjs defines it at line 62.") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestDecisionsListShowsQuestions(t *testing.T) {
	open := fixtureObject(t, "decision-plan-approval.json")
	delete(open["spec"].(map[string]any), "response")
	open["status"] = map[string]any{"phase": "Open"}
	server := fakeMarina(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/apis/marina.dev/v1/namespaces/factory-default/decisions" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		writeMarinaJSON(t, w, http.StatusOK, listOf("DecisionList", open))
	})

	code, stdout, stderr := runForTest(t, []string{"decisions", "list"}, marinaEnvironment(server.URL))
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"task-0042-plan-1", "Run/task-0042", "plan-approval", "Open", "Approve this plan?"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q: %q", want, stdout)
		}
	}
}

func TestDecisionsAnswerSendsTheAnswerWithAnIdempotencyKey(t *testing.T) {
	server := fakeMarina(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/apis/marina.dev/v1/namespaces/factory-default/decisions/task-0042-plan-1/answer" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if key := r.Header.Get("Idempotency-Key"); len(key) < 32 {
			t.Errorf("Idempotency-Key = %q", key)
		}
		body, _ := io.ReadAll(r.Body)
		var got, want any
		_ = json.Unmarshal(body, &got)
		_ = json.Unmarshal(fixture(t, "decision-answer.json"), &want)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("body = %s", body)
		}
		writeMarinaJSON(t, w, http.StatusOK, fixture(t, "decision-plan-approval.json"))
	})

	code, stdout, stderr := runForTest(t, []string{"decisions", "answer", "task-0042-plan-1", "approve"}, marinaEnvironment(server.URL))
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Answered decision task-0042-plan-1: approve (Answered)") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestMarinaRequiresHTTPSOutsideLoopback(t *testing.T) {
	code, stdout, stderr := runForTest(t, []string{"--marina-url", "http://marina.example:8090", "runs", "list"}, marinaEnvironment(""))
	if code != 1 || !strings.Contains(stderr, "https") {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout+stderr, marinaTestToken) {
		t.Fatal("token leaked")
	}
}

func TestMarinaTrustsTheInstallationCAFile(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+marinaTestToken {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		writeMarinaJSON(t, w, http.StatusOK, fixture(t, "capabilities.json"))
	}))
	t.Cleanup(server.Close)
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caPath, certificate, 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := runForTest(t, []string{"capabilities"}, marinaEnvironment(server.URL))
	if code != 1 {
		t.Fatalf("untrusted certificate accepted: code = %d, stderr = %q", code, stderr)
	}
	code, stdout, stderr := runForTest(t, []string{"--marina-ca-file", caPath, "capabilities"}, marinaEnvironment(server.URL))
	if code != 0 || !strings.Contains(stdout, "openai/gpt-6-luna") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestMarinaDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Store(true) }))
	t.Cleanup(target.Close)
	server := fakeMarina(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	})

	code, _, stderr := runForTest(t, []string{"runs", "list"}, marinaEnvironment(server.URL))
	if code != 1 || redirected.Load() {
		t.Fatalf("code = %d, redirected = %v, stderr = %q", code, redirected.Load(), stderr)
	}
}

func TestMarinaTokenFileIsSentAsBearerToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marina.token")
	if err := os.WriteFile(path, []byte(marinaTestToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := fakeMarina(t, func(w http.ResponseWriter, _ *http.Request) {
		writeMarinaJSON(t, w, http.StatusOK, fixture(t, "capabilities.json"))
	})
	environment := func(name string) string {
		switch name {
		case "MARINA_TOKEN_FILE":
			return path
		case "MARINA_TOKEN":
			return "ignored-when-a-file-is-configured"
		default:
			return ""
		}
	}

	code, _, stderr := runForTest(t, []string{"--marina-url", server.URL, "capabilities"}, environment)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
}

func TestMarinaTokenFileReadableByOthersIsRejectedBeforeNetwork(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file-mode protection; Windows uses ACLs")
	}
	path := filepath.Join(t.TempDir(), "marina.token")
	if err := os.WriteFile(path, []byte("protected-marina-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	server := fakeMarina(t, func(http.ResponseWriter, *http.Request) { t.Error("Marina was called") })

	code, stdout, stderr := runForTest(t, []string{"--marina-url", server.URL, "--marina-token-file", path, "capabilities"}, emptyEnvironment)
	if code != 1 || !strings.Contains(stderr, "chmod 600") {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout+stderr, "protected-marina-secret") {
		t.Fatal("token leaked")
	}
}
