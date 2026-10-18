package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The contract every client relies on, exercised over HTTP against the real
// API, authentication and controller (MAR-07).

type client struct {
	t     *testing.T
	base  string
	token string
}

func start(t *testing.T) client {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	dir := t.TempDir()
	s, err := Start(ctx, listener, filepath.Join(dir, "marina.db"), filepath.Join(dir, "artifacts"), 20*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = s.Close() })
	return client{t: t, base: "http://" + listener.Addr().String(), token: "user:system:serviceaccount:marina-system:deck"}
}

func (c client) do(method, path string, body any, headers ...string) (int, map[string]any) {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	}
	request, _ := http.NewRequest(method, c.base+path, reader)
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		c.t.Fatal(err)
	}
	defer response.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(response.Body).Decode(&out)
	return response.StatusCode, out
}

const ns = "/apis/marina.dev/v1/namespaces/" + Namespace

func (c client) waitFor(path string, done func(map[string]any) bool) map[string]any {
	c.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if code, object := c.do("GET", path, nil); code == 200 && done(object) {
			return object
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, last := c.do("GET", path, nil)
	c.t.Fatalf("%s did not reach the expected state: %v", path, last["status"])
	return nil
}

func phaseIs(phase string) func(map[string]any) bool {
	return func(object map[string]any) bool {
		status, _ := object["status"].(map[string]any)
		return status != nil && status["phase"] == phase
	}
}

func runSpec(name, title string) map[string]any {
	return map[string]any{"apiVersion": "marina.dev/v1", "kind": "Run", "metadata": map[string]any{"name": name},
		"spec": map[string]any{"project": Project, "title": title, "acceptanceCriteria": "A unit test covers it", "budget": map[string]any{"timeMinutes": 30, "maxTokens": 10000, "maxAttempts": 7}}}
}

func TestClientsAreAuthenticatedAndAuthorizedByKubernetesIdentity(t *testing.T) {
	c := start(t)
	if code, body := c.do("GET", "/apis/marina.dev/v1/capabilities", nil); code != 200 || body["kind"] != "Capabilities" {
		t.Fatalf("capabilities = %d %v", code, body)
	}
	anonymous := c
	anonymous.token = ""
	if code, body := anonymous.do("GET", ns+"/runs", nil); code != 401 || body["kind"] != "Status" {
		t.Fatalf("anonymous = %d %v", code, body)
	}
	viewer := c
	viewer.token = "user:viewer"
	if code, _ := viewer.do("GET", ns+"/runs", nil); code != 200 {
		t.Fatalf("viewer list = %d", code)
	}
	if code, _ := viewer.do("POST", ns+"/runs", runSpec("task-v", "Viewer write")); code != 403 {
		t.Fatalf("viewer create = %d", code)
	}
}

func TestARunIsApprovedVerifiedAndLandedWithEvidenceAndEvents(t *testing.T) {
	c := start(t)
	if code, body := c.do("POST", ns+"/runs", runSpec("task-1", "Reject an empty range")); code != 201 {
		t.Fatalf("create = %d %v", code, body)
	}
	if code, _ := c.do("POST", ns+"/runs", runSpec("task-1", "Reject an empty range")); code != 200 {
		t.Fatalf("exact replay = %d", code)
	}
	if code, body := c.do("POST", ns+"/runs", runSpec("task-1", "Another title")); code != 409 || body["reason"] != "AlreadyExists" {
		t.Fatalf("changed spec = %d %v", code, body)
	}
	waiting := c.waitFor(ns+"/runs/task-1", phaseIs("WaitingDecision"))
	decisions := waiting["status"].(map[string]any)["decisions"].([]any)
	name := decisions[0].(string)
	decision := c.waitFor(ns+"/decisions/"+name, phaseIs("Open"))
	if decision["spec"].(map[string]any)["kind"] != "plan-approval" {
		t.Fatalf("decision = %v", decision["spec"])
	}
	if code, _ := c.do("POST", ns+"/decisions/"+name+"/answer", map[string]any{"answer": "Approve plan"}); code != 400 {
		t.Fatalf("answer without an idempotency key = %d", code)
	}
	if code, body := c.do("POST", ns+"/decisions/"+name+"/answer", map[string]any{"answer": "approve plan"}, "Idempotency-Key", "a1"); code != 200 {
		t.Fatalf("answer = %d %v", code, body)
	}
	done := c.waitFor(ns+"/runs/task-1", phaseIs("Succeeded"))
	status := done["status"].(map[string]any)
	kinds := map[string]bool{}
	for _, item := range status["evidence"].([]any) {
		evidence := item.(map[string]any)
		kinds[evidence["kind"].(string)] = evidence["passed"] == true
	}
	if !kinds["review"] || !kinds["verification"] || !kinds["ci"] || !kinds["landing"] || status["landing"].(map[string]any)["commit"] != headSHA {
		t.Fatalf("status = %v", status)
	}
	code, page := c.do("GET", ns+"/runs/task-1/events?after=0", nil)
	items := page["items"].([]any)
	if code != 200 || len(items) < 5 || items[0].(map[string]any)["type"] != "run.accepted" {
		t.Fatalf("events = %d %v", code, page)
	}
	for i, item := range items {
		if seq := item.(map[string]any)["sequence"].(float64); int(seq) != i+1 {
			t.Fatalf("event %d has sequence %v", i, seq)
		}
	}
	types := fmt.Sprint(items)
	for _, want := range []string{"plan.submitted", "decision.answered", "run.started", "run.completed"} {
		if !strings.Contains(types, want) {
			t.Fatalf("events lack %s: %v", want, types)
		}
	}
	request, _ := http.NewRequest("GET", c.base+ns+"/runs/task-1/events?after=2", nil)
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "text/event-stream")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := http.DefaultClient.Do(request.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	first, _ := bufio.NewReader(response.Body).ReadString('\n')
	response.Body.Close()
	if first != "id: 3\n" {
		t.Fatalf("stream after 2 began with %q", first)
	}
}

func TestRunsBlockOrCancelWithAReason(t *testing.T) {
	c := start(t)
	c.do("POST", ns+"/runs", runSpec("task-b", "Please block this"))
	c.waitFor(ns+"/runs/task-b", phaseIs("WaitingDecision"))
	decision := c.waitFor(ns+"/runs/task-b", phaseIs("WaitingDecision"))["status"].(map[string]any)["decisions"].([]any)[0].(string)
	c.do("POST", ns+"/decisions/"+decision+"/answer", map[string]any{"answer": "Approve plan"}, "Idempotency-Key", "a")
	blocked := c.waitFor(ns+"/runs/task-b", phaseIs("Blocked"))
	if !strings.Contains(blocked["status"].(map[string]any)["reason"].(string), "block") {
		t.Fatalf("blocked = %v", blocked["status"])
	}
	c.do("POST", ns+"/runs", runSpec("task-c", "Cancel me"))
	c.waitFor(ns+"/runs/task-c", phaseIs("WaitingDecision"))
	if code, _ := c.do("POST", ns+"/runs/task-c/cancel", nil, "Idempotency-Key", "c1"); code != 200 {
		t.Fatalf("cancel = %d", code)
	}
	c.waitFor(ns+"/runs/task-c", phaseIs("Cancelled"))
	if code, _ := c.do("DELETE", ns+"/runs/task-c", nil); code != 200 {
		t.Fatalf("delete = %d", code)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if code, _ := c.do("GET", ns+"/runs/task-c", nil); code == 404 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a deleted terminal run was kept")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func turnSpec(name, body string) map[string]any {
	return map[string]any{"apiVersion": "marina.dev/v1", "kind": "Turn", "metadata": map[string]any{"name": name},
		"spec": map[string]any{"project": Project, "workload": "chat", "thread": map[string]any{"id": "thr-1", "messages": []any{map[string]any{"id": "m1", "role": "human", "body": body}}},
			"budget": map[string]any{"timeMinutes": 15, "maxTokens": 40000}}}
}

func TestTurnsReplyAskAndProposeThroughTheirResources(t *testing.T) {
	c := start(t)
	c.do("POST", ns+"/turns", turnSpec("turn-1", "Hello"))
	reply := c.waitFor(ns+"/turns/turn-1", phaseIs("Succeeded"))
	if reply["status"].(map[string]any)["reply"].(map[string]any)["answer"] != "Sandbox reply to: Hello" {
		t.Fatalf("reply = %v", reply["status"])
	}
	c.do("POST", ns+"/turns", turnSpec("turn-2", "Please clarify the design"))
	waiting := c.waitFor(ns+"/turns/turn-2", phaseIs("WaitingDecision"))
	name := waiting["status"].(map[string]any)["decisions"].([]any)[0].(string)
	c.waitFor(ns+"/decisions/"+name, phaseIs("Open"))
	if code, _ := c.do("POST", ns+"/decisions/"+name+"/answer", map[string]any{"answer": "Store"}, "Idempotency-Key", "q"); code != 200 {
		t.Fatalf("answer = %d", code)
	}
	clarified := c.waitFor(ns+"/turns/turn-2", phaseIs("Succeeded"))
	if !strings.Contains(clarified["status"].(map[string]any)["reply"].(map[string]any)["answer"].(string), "Store") {
		t.Fatalf("clarified reply = %v", clarified["status"])
	}
	c.do("POST", ns+"/turns", turnSpec("turn-3", "Please propose a task"))
	proposed := c.waitFor(ns+"/turns/turn-3", phaseIs("Succeeded"))
	if proposed["status"].(map[string]any)["proposals"].(map[string]any)["task"].(map[string]any)["title"] != "Sandbox proposal" {
		t.Fatalf("proposal = %v", proposed["status"])
	}
}
