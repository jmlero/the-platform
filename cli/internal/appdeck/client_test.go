package appdeck

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientUsesOperatorAPI(t *testing.T) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r, "secret")
		writeTestJSON(t, w, http.StatusOK, Health{Status: "ok", Service: "appdeck", Time: "now"})
	})
	mux.HandleFunc("GET /api/v1/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r, "secret")
		writeTestJSON(t, w, http.StatusOK, Bootstrap{Tasks: []Task{{ID: "tsk_1", Title: "First"}}})
	})
	mux.HandleFunc("GET /api/v1/operator/tasks/tsk_1", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r, "secret")
		writeTestJSON(t, w, http.StatusOK, TaskDetail{Task: Task{ID: "tsk_1", Title: "First"}})
	})
	mux.HandleFunc("POST /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r, "secret")
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("Content-Type = %q", contentType)
		}
		var input CreateTaskInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("decode create task: %v", err)
		}
		if input.Title != "Created from CLI" {
			t.Fatalf("title = %q", input.Title)
		}
		writeTestJSON(t, w, http.StatusCreated, Task{ID: "tsk_2", Title: input.Title})
	})
	mux.HandleFunc("POST /api/v1/operator/runs/run_1/commands", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r, "secret")
		var input CreateCommandInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("decode command: %v", err)
		}
		if input.Type != "pause" || input.Reason != "operator requested a checkpoint" {
			t.Fatalf("command input = %#v", input)
		}
		writeTestJSON(t, w, http.StatusCreated, Command{
			ID: "cmd_1", RunID: "run_1", Sequence: 1, Type: input.Type,
			Status: "pending", Reason: input.Reason,
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client, err := New(server.URL+"/", "secret", server.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx := context.Background()
	health, err := client.Health(ctx)
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if health.Service != "appdeck" {
		t.Fatalf("service = %q", health.Service)
	}
	bootstrap, err := client.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Tasks) != 1 || bootstrap.Tasks[0].ID != "tsk_1" {
		t.Fatalf("tasks = %#v", bootstrap.Tasks)
	}
	detail, err := client.Task(ctx, "tsk_1")
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if detail.Task.Title != "First" {
		t.Fatalf("task = %#v", detail.Task)
	}
	created, err := client.CreateTask(ctx, CreateTaskInput{Title: "Created from CLI"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if created.ID != "tsk_2" {
		t.Fatalf("created = %#v", created)
	}
	command, err := client.CreateCommand(ctx, "run_1", CreateCommandInput{
		Type: "pause", Reason: "operator requested a checkpoint",
	})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	if command.ID != "cmd_1" || command.Sequence != 1 || command.Status != "pending" {
		t.Fatalf("command = %#v", command)
	}
}

func TestClientReturnsStructuredAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-ID", "req_header")
		writeTestJSON(t, w, http.StatusUnprocessableEntity, map[string]any{
			"code":      "invalid_input",
			"message":   "title is required",
			"retryable": false,
		})
	}))
	t.Cleanup(server.Close)
	client, err := New(server.URL, "", server.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.CreateTask(context.Background(), CreateTaskInput{})
	var apiError *APIError
	if !errors.As(err, &apiError) {
		t.Fatalf("error = %v, want APIError", err)
	}
	if apiError.StatusCode != http.StatusUnprocessableEntity || apiError.Code != "invalid_input" {
		t.Fatalf("API error = %#v", apiError)
	}
	if apiError.RequestID != "req_header" {
		t.Fatalf("request ID = %q", apiError.RequestID)
	}
}

func TestNewRejectsUnsafeBaseURLs(t *testing.T) {
	tests := []struct {
		url   string
		token string
	}{
		{url: ""},
		{url: "localhost:8080"},
		{url: "ftp://localhost:8080"},
		{url: "http://user:secret@localhost:8080"},
		{url: "http://localhost:8080?token=secret"},
		{url: "http://localhost:8080#fragment"},
		{url: "http://appdeck.example", token: "secret"},
	}
	for _, test := range tests {
		t.Run(test.url, func(t *testing.T) {
			if _, err := New(test.url, test.token, nil); err == nil {
				t.Fatalf("New(%q) succeeded", test.url)
			}
		})
	}
}

func TestNewAllowsBearerTokensOnProtectedOrLoopbackURLs(t *testing.T) {
	for _, rawURL := range []string{
		"https://appdeck.example",
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"http://[::1]:8080",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if _, err := New(rawURL, "secret", nil); err != nil {
				t.Fatalf("New(%q): %v", rawURL, err)
			}
		})
	}
}

func TestCredentialedClientDoesNotFollowRedirects(t *testing.T) {
	var serverURL string
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/healthz" {
			t.Fatalf("credentialed client followed redirect to %s", r.URL)
		}
		w.Header().Set("Location", serverURL+"/redirected")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)
	serverURL = server.URL

	client, err := New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.Health(context.Background())
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("redirect error = %v, want HTTP %d APIError", err, http.StatusTemporaryRedirect)
	}
	if calls != 1 {
		t.Fatalf("request calls = %d, want 1", calls)
	}
}

func TestTaskRejectsPathCharacters(t *testing.T) {
	client, err := New("http://localhost:8080", "", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.Task(context.Background(), "../other"); err == nil {
		t.Fatal("Task accepted an unsafe resource ID")
	}
	if _, err := client.CreateCommand(context.Background(), "../other", CreateCommandInput{
		Type: "cancel", Reason: "stop",
	}); err == nil {
		t.Fatal("CreateCommand accepted an unsafe resource ID")
	}
}

func requireBearer(t *testing.T, r *http.Request, token string) {
	t.Helper()
	if authorization := r.Header.Get("Authorization"); authorization != "Bearer "+token {
		t.Fatalf("Authorization = %q", authorization)
	}
	if userAgent := r.Header.Get("User-Agent"); userAgent != "platformctl" {
		t.Fatalf("User-Agent = %q", userAgent)
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
