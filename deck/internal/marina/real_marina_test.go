package marina_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRealMarinaWorkflow drives a real Deck process configured with a real
// Marina process (the API sandbox build) only through Deck's operator API.
// Deck calls Marina; Marina never calls Deck (MAR-08).
func TestRealMarinaWorkflow(t *testing.T) {
	marinaBinary, deckBinary := os.Getenv("MARINA_TEST_BINARY"), os.Getenv("DECK_TEST_BINARY")
	if marinaBinary == "" || deckBinary == "" {
		t.Skip("run make -C deck conformance")
	}
	dir := t.TempDir()
	marinaURL := startProcess(t, marinaBinary, nil, "/apis/marina.dev/v1/capabilities", "api-sandbox", "--dir", filepath.Join(dir, "marina"))
	token := filepath.Join(dir, "token")
	write(t, token, "user:system:serviceaccount:marina-system:deck")
	projects := filepath.Join(dir, "projects.json")
	write(t, projects, "[]")
	deckEnv := []string{"DECK_DATA_DIR=" + filepath.Join(dir, "deck"), "DECK_MARINA_URL=" + marinaURL, "DECK_MARINA_TOKEN_FILE=" + token, "DECK_MARINA_PROJECTS_FILE=" + projects}
	deck := &deckProcess{t: t, binary: deckBinary, env: deckEnv}
	deck.start()

	var project struct {
		Project  struct{ ID string }   `json:"project"`
		Channels []struct{ ID string } `json:"channels"`
	}
	deck.call("POST", "/api/v1/operator/projects", map[string]string{"repository": "example/acceptance", "base_branch": "main"}, &project)
	// The mapping is reread every cycle, so a new project needs no restart.
	write(t, projects, fmt.Sprintf(`[{"deck_project":%q,"namespace":"factory","project":"acceptance","workloads":{"repository/v2":"land-on-main"},"conversation_workload":"chat"}]`, project.Project.ID))
	deck.waitFor("the Marina repository binding", func() bool {
		var repository struct{ Status string }
		deck.call("GET", "/api/v1/operator/projects/"+project.Project.ID+"/repository", nil, &repository)
		return repository.Status == "connected"
	})

	landed := deck.readyTask(project.Project.ID, "Reject an empty range")
	decision := deck.pendingDecision(landed)
	deck.call("POST", "/api/v1/operator/decisions/"+decision+"/answer", map[string]string{"answer": "Approve plan"}, nil)
	detail := deck.waitForStatus(landed, "done")
	if detail.Task.Landing == nil || len(detail.Task.Landing.Commit) != 40 || len(detail.Evidence) < 4 {
		t.Fatalf("landed task = %+v, %d evidence", detail.Task.Landing, len(detail.Evidence))
	}

	blocked := deck.readyTask(project.Project.ID, "Please block this")
	deck.call("POST", "/api/v1/operator/decisions/"+deck.pendingDecision(blocked)+"/answer", map[string]string{"answer": "Approve plan"}, nil)
	if detail := deck.waitForStatus(blocked, "blocked"); !strings.Contains(detail.Runs[0].ResultSummary, "block") {
		t.Fatalf("blocked reason = %q", detail.Runs[0].ResultSummary)
	}

	cancelled := deck.readyTask(project.Project.ID, "Cancel me")
	deck.pendingDecision(cancelled)
	run := deck.task(cancelled).Runs[0].ID
	deck.call("POST", "/api/v1/operator/runs/"+run+"/commands", map[string]string{"type": "cancel", "reason": "No longer needed"}, nil)
	deck.waitForStatus(cancelled, "cancelled")

	restarted := deck.readyTask(project.Project.ID, "Survive a Deck restart")
	restartDecision := deck.pendingDecision(restarted)
	deck.stop()
	deck.start()
	deck.call("POST", "/api/v1/operator/decisions/"+restartDecision+"/answer", map[string]string{"answer": "Approve plan"}, nil)
	deck.waitForStatus(restarted, "done")
	var runs struct {
		Items []struct {
			Metadata struct{ Name string } `json:"metadata"`
		} `json:"items"`
	}
	marinaGet(t, marinaURL+"/apis/marina.dev/v1/namespaces/factory/runs", &runs)
	count := 0
	for _, item := range runs.Items {
		if item.Metadata.Name == deck.task(restarted).Runs[0].ID {
			count++
		}
	}
	if len(deck.task(restarted).Runs) != 1 || count != 1 || len(runs.Items) != 4 {
		t.Fatalf("restart duplicated work: %d Deck runs, %d Marina runs", len(deck.task(restarted).Runs), len(runs.Items))
	}

	channel := project.Channels[0].ID
	hello := deck.invoke(channel, "Hello")
	deck.waitForMessage(hello, "Sandbox reply to: Hello")

	clarify := deck.invoke(channel, "Please clarify the design")
	var questions struct {
		Items []struct {
			ID     string
			Status string
		} `json:"items"`
	}
	deck.waitFor("a clarification question", func() bool {
		deck.call("GET", "/api/v1/operator/threads/"+clarify+"/questions", nil, &questions)
		return len(questions.Items) == 1 && questions.Items[0].Status == "pending"
	})
	deck.call("POST", "/api/v1/operator/conversation/questions/"+questions.Items[0].ID+"/answer", map[string]string{"answer": "Store", "request_id": "answer-store"}, nil)
	deck.waitForMessage(clarify, "Sandbox reply about Store")

	deck.invoke(channel, "Please propose a task")
	deck.waitFor("a proposed backlog task", func() bool {
		var bootstrap struct {
			Tasks []struct{ Title, Status string }
		}
		deck.call("GET", "/api/v1/bootstrap", nil, &bootstrap)
		for _, task := range bootstrap.Tasks {
			if task.Title == "Sandbox proposal" && task.Status == "backlog" {
				return true
			}
		}
		return false
	})
}

type taskDetail struct {
	Task struct {
		Status  string
		Landing *struct{ Commit string } `json:"landing"`
	} `json:"task"`
	Runs []struct {
		ID            string
		ResultSummary string `json:"result_summary"`
	} `json:"runs"`
	Decisions []struct{ ID, Status string } `json:"decisions"`
	Evidence  []struct{ ID string }         `json:"evidence"`
}

type deckProcess struct {
	t       *testing.T
	binary  string
	env     []string
	url     string
	process *exec.Cmd
}

func (d *deckProcess) start() {
	d.url = startProcessWith(d.t, d.binary, d.env, "/healthz", &d.process)
}

func (d *deckProcess) stop() {
	_ = d.process.Process.Signal(os.Interrupt)
	_ = d.process.Wait()
}

func (d *deckProcess) call(method, path string, body, out any) {
	d.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	}
	request, _ := http.NewRequest(method, d.url+path, reader)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		d.t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode >= 300 {
		d.t.Fatalf("%s %s = %d %s", method, path, response.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			d.t.Fatalf("%s %s: %v", method, path, err)
		}
	}
}

func (d *deckProcess) waitFor(what string, done func() bool) {
	d.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			d.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (d *deckProcess) readyTask(project, title string) string {
	d.t.Helper()
	var task struct{ ID string }
	d.call("POST", "/api/v1/tasks", map[string]any{"project_id": project, "title": title, "acceptance_criteria": "A unit test covers it", "status": "ready",
		"budget": map[string]any{"time_minutes": 30, "max_tokens": 10000, "max_attempts": 3, "authority": []string{"source.read", "source.write", "source.fast_forward"}}}, &task)
	return task.ID
}

func (d *deckProcess) task(id string) taskDetail {
	d.t.Helper()
	var detail taskDetail
	d.call("GET", "/api/v1/operator/tasks/"+id, nil, &detail)
	return detail
}

func (d *deckProcess) pendingDecision(task string) string {
	d.t.Helper()
	var id string
	d.waitFor("a plan decision on "+task, func() bool {
		detail := d.task(task)
		for _, decision := range detail.Decisions {
			if decision.Status == "pending" {
				id = decision.ID
			}
		}
		return id != "" && detail.Task.Status == "waiting_decision"
	})
	return id
}

func (d *deckProcess) waitForStatus(task, status string) taskDetail {
	d.t.Helper()
	var detail taskDetail
	d.waitFor(task+" to be "+status, func() bool {
		detail = d.task(task)
		return detail.Task.Status == status
	})
	return detail
}

func (d *deckProcess) invoke(channel, body string) string {
	d.t.Helper()
	var invocation struct {
		Thread struct{ ID string } `json:"thread"`
	}
	d.call("POST", "/api/v1/operator/channels/"+channel+"/invocations", map[string]string{"agent_id": "marina", "request_id": "request-" + body, "body": body}, &invocation)
	return invocation.Thread.ID
}

func (d *deckProcess) waitForMessage(thread, text string) {
	d.t.Helper()
	d.waitFor("a reply containing "+text, func() bool {
		var page struct{ Items []struct{ Body string } }
		d.call("GET", "/api/v1/operator/threads/"+thread+"/messages?limit=50", nil, &page)
		for _, message := range page.Items {
			if strings.Contains(message.Body, text) {
				return true
			}
		}
		return false
	})
}

func write(t *testing.T, path, content string) {
	t.Helper()
	// Replace atomically so a reader never sees a partial file.
	if err := os.WriteFile(path+".tmp", []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		t.Fatal(err)
	}
}

func marinaGet(t *testing.T, url string, out any) {
	t.Helper()
	request, _ := http.NewRequest("GET", url, nil)
	request.Header.Set("Authorization", "Bearer user:system:serviceaccount:marina-system:deck")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

var logsMu sync.Mutex

func startProcess(t *testing.T, binary string, env []string, ready string, args ...string) string {
	var process *exec.Cmd
	return startProcessWith(t, binary, env, ready, &process, args...)
}

// startProcessWith starts binary on a free loopback port and waits for ready.
// Marina takes --addr; Deck takes DECK_ADDR.
func startProcessWith(t *testing.T, binary string, env []string, ready string, out **exec.Cmd, args ...string) string {
	t.Helper()
	address := freeAddress(t)
	if len(args) > 0 {
		args = append(args, "--addr", address)
	}
	process := exec.Command(binary, args...)
	process.Env = append(cleanEnvironment(), append(env, "DECK_ADDR="+address)...)
	var logs bytes.Buffer
	writer := lockedWriter{&logs}
	process.Stdout, process.Stderr = writer, writer
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	*out = process
	t.Cleanup(func() {
		_ = process.Process.Kill()
		_, _ = process.Process.Wait()
		if t.Failed() {
			logsMu.Lock()
			t.Logf("%s logs:\n%s", filepath.Base(binary), logs.String())
			logsMu.Unlock()
		}
	})
	url := "http://" + address
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if response, err := client.Get(url + ready); err == nil {
			response.Body.Close()
			return url
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not become ready", binary)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type lockedWriter struct{ buffer *bytes.Buffer }

func (w lockedWriter) Write(p []byte) (int, error) {
	logsMu.Lock()
	defer logsMu.Unlock()
	return w.buffer.Write(p)
}

func cleanEnvironment() []string {
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "DECK_") && !strings.HasPrefix(entry, "APPDECK_") {
			env = append(env, entry)
		}
	}
	return env
}
