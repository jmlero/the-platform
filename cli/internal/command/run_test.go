package command

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/cli/internal/appdeck"
)

func TestTasksListFiltersWorkspaceSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/bootstrap" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		writeCommandJSON(t, w, http.StatusOK, appdeck.Bootstrap{Tasks: []appdeck.Task{
			{ID: "tsk_ready", ProjectID: "prj_default", Title: "Ready task", Status: "ready", Priority: "high"},
			{ID: "tsk_backlog", ProjectID: "prj_default", Title: "Backlog task", Status: "backlog", Priority: "medium"},
		}})
	}))
	t.Cleanup(server.Close)

	code, stdout, stderr := runForTest(
		t,
		[]string{"--appdeck-url", server.URL, "tasks", "list", "--status", "ready"},
		emptyEnvironment,
	)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Ready task") || strings.Contains(stdout, "Backlog task") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestTasksGetWritesJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/operator/tasks/tsk_1" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		writeCommandJSON(t, w, http.StatusOK, appdeck.TaskDetail{
			Task: appdeck.Task{ID: "tsk_1", Title: "Inspect me", Status: "running"},
			Runs: []appdeck.Run{{ID: "run_1", Status: "running"}},
		})
	}))
	t.Cleanup(server.Close)

	code, stdout, stderr := runForTest(
		t,
		[]string{"--json", "--appdeck-url", server.URL, "tasks", "get", "tsk_1"},
		emptyEnvironment,
	)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	var detail appdeck.TaskDetail
	if err := json.Unmarshal([]byte(stdout), &detail); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}
	if detail.Task.ID != "tsk_1" || len(detail.Runs) != 1 {
		t.Fatalf("detail = %#v", detail)
	}
}

func TestTasksCreateUsesTokenFile(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "appdeck.token")
	if err := os.WriteFile(tokenPath, []byte("operator-secret\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/tasks" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if authorization := r.Header.Get("Authorization"); authorization != "Bearer operator-secret" {
			t.Fatalf("Authorization = %q", authorization)
		}
		var input appdeck.CreateTaskInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("decode input: %v", err)
		}
		if input.Title != "CLI task" || input.Status != "ready" || input.Priority != "urgent" {
			t.Fatalf("input = %#v", input)
		}
		inputTask := appdeck.Task{ID: "tsk_created", Title: input.Title, Status: input.Status}
		writeCommandJSON(t, w, http.StatusCreated, inputTask)
	}))
	t.Cleanup(server.Close)

	code, stdout, stderr := runForTest(t, []string{
		"--appdeck-url", server.URL,
		"--token-file", tokenPath,
		"tasks", "create",
		"--title", "CLI task",
		"--status", "ready",
		"--priority", "urgent",
	}, emptyEnvironment)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Created tsk_created [ready] CLI task") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestTasksCreateValidatesBeforeCallingAppDeck(t *testing.T) {
	code, _, stderr := runForTest(
		t,
		[]string{"tasks", "create", "--title", "Valid title", "--status", "invalid"},
		emptyEnvironment,
	)
	if code != 2 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "initial status must be backlog or ready") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRunCommandsCreateDurableOperatorCommands(t *testing.T) {
	commandTypes := []string{"pause", "resume", "cancel", "replan", "retry"}
	for index, commandType := range commandTypes {
		t.Run(commandType, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/operator/runs/run_1/commands" {
					t.Fatalf("request = %s %s", r.Method, r.URL.Path)
				}
				var input appdeck.CreateCommandInput
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Fatalf("decode command: %v", err)
				}
				if input.Type != commandType || input.Reason != "operator intent" {
					t.Fatalf("input = %#v", input)
				}
				writeCommandJSON(t, w, http.StatusCreated, appdeck.Command{
					ID: "cmd_1", RunID: "run_1", Sequence: int64(index + 1),
					Type: commandType, Status: "pending", Reason: input.Reason,
				})
			}))
			t.Cleanup(server.Close)

			code, stdout, stderr := runForTest(t, []string{
				"--appdeck-url", server.URL,
				"runs", commandType,
				"--reason", "operator intent",
				"run_1",
			}, emptyEnvironment)
			if code != 0 {
				t.Fatalf("code = %d, stderr = %q", code, stderr)
			}
			if !strings.Contains(stdout, "Requested "+commandType+" for run_1: cmd_1") ||
				!strings.Contains(stdout, "pending") {
				t.Fatalf("stdout = %q", stdout)
			}
		})
	}
}

func TestRunCommandWritesJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeCommandJSON(t, w, http.StatusCreated, appdeck.Command{
			ID: "cmd_json", RunID: "run_json", Sequence: 3, Type: "cancel", Status: "pending",
		})
	}))
	t.Cleanup(server.Close)

	code, stdout, stderr := runForTest(t, []string{
		"--json", "--appdeck-url", server.URL,
		"runs", "cancel", "--reason", "stop safely", "run_json",
	}, emptyEnvironment)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	var command appdeck.Command
	if err := json.Unmarshal([]byte(stdout), &command); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if command.ID != "cmd_json" || command.Type != "cancel" {
		t.Fatalf("command = %#v", command)
	}
}

func TestRunCommandRequiresReasonBeforeCallingAppDeck(t *testing.T) {
	code, _, stderr := runForTest(
		t,
		[]string{"runs", "pause", "run_1"},
		emptyEnvironment,
	)
	if code != 2 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "reason is required") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestHealthUsesEnvironmentConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authorization := r.Header.Get("Authorization"); authorization != "Bearer from-env" {
			t.Fatalf("Authorization = %q", authorization)
		}
		writeCommandJSON(t, w, http.StatusOK, appdeck.Health{Service: "appdeck", Status: "ok", Time: "now"})
	}))
	t.Cleanup(server.Close)
	environment := func(name string) string {
		switch name {
		case "APPDECK_URL":
			return server.URL
		case "APPDECK_TOKEN":
			return "from-env"
		default:
			return ""
		}
	}

	code, stdout, stderr := runForTest(t, []string{"health"}, environment)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if stdout != "appdeck: ok (now)\n" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func runForTest(
	t *testing.T,
	args []string,
	getenv func(string) string,
) (int, string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), args, &stdout, &stderr, getenv, "test")
	return code, stdout.String(), stderr.String()
}

func emptyEnvironment(string) string { return "" }

func writeCommandJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
