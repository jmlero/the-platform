package marina

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture reads one of Marina's canonical contract examples, so Deck's client
// decodes exactly what the provider publishes.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "marina", "api", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func tokenFile(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestClientDecodesMarinaFixturesAndSendsTheCurrentToken(t *testing.T) {
	var tokens []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens = append(tokens, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/marina.dev/v1/namespaces/factory-default/runs/task-0042":
			_, _ = w.Write(fixture(t, "run-succeeded.json"))
		case "/apis/marina.dev/v1/namespaces/factory-default/decisions/task-0042-plan-1":
			_, _ = w.Write(fixture(t, "decision-plan-approval.json"))
		case "/apis/marina.dev/v1/namespaces/factory-default/turns/msg-68a3a542":
			_, _ = w.Write(fixture(t, "turn-succeeded.json"))
		case "/apis/marina.dev/v1/namespaces/factory-default/runs/task-0042/events":
			if r.URL.Query().Get("after") != "4" {
				t.Errorf("after = %q", r.URL.RawQuery)
			}
			_, _ = w.Write(fixture(t, "event-page.json"))
		case "/apis/marina.dev/v1/namespaces/factory-default/projects/acceptance":
			_, _ = w.Write(fixture(t, "project.json"))
		case "/apis/marina.dev/v1/namespaces/factory-default/workloads/land-on-main":
			_, _ = w.Write(fixture(t, "workload-repository.json"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	path := tokenFile(t, "first")
	client, err := New(Config{URL: server.URL, TokenFile: path})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	run, err := client.Run(ctx, "factory-default", "task-0042")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status.Phase != "Succeeded" || run.Status.Landing == nil || run.Status.Landing.Commit != "7c1e0f2a9b8d4c3e5f6a7b8c9d0e1f2a3b4c5d6e" ||
		len(run.Status.Evidence) != 4 || run.Status.Evidence[0].Kind != "review" || run.Status.Decisions[0] != "task-0042-plan-1" {
		t.Fatalf("run = %+v", run.Status)
	}
	// Projected service account tokens rotate; every request reads the file.
	if err := os.WriteFile(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	decision, err := client.Decision(ctx, "factory-default", "task-0042-plan-1")
	if err != nil || decision.Spec.Kind != "plan-approval" || decision.Spec.Response == nil || decision.Spec.Response.Answer != "approve" || decision.Status.Phase != "Answered" {
		t.Fatalf("decision = %+v, %v", decision, err)
	}
	turn, err := client.Turn(ctx, "factory-default", "msg-68a3a542")
	if err != nil || turn.Status.Phase != "Succeeded" || turn.Status.Reply == nil || turn.Status.Reply.Answer != "src/clamp.cjs defines it at line 62." {
		t.Fatalf("turn = %+v, %v", turn, err)
	}
	page, err := client.RunEvents(ctx, "factory-default", "task-0042", 4)
	if err != nil || len(page.Items) != 2 || page.Items[0].Sequence != 5 || page.NextAfter != 6 {
		t.Fatalf("events = %+v, %v", page, err)
	}
	project, err := client.Project(ctx, "factory-default", "acceptance")
	if err != nil || project.Spec.Repository.URL != "https://github.com/jmlero/marina-acceptance.git" || project.Status.BindingVersion != 3 || !project.Ready() {
		t.Fatalf("project = %+v, %v", project, err)
	}
	workload, err := client.Workload(ctx, "factory-default", "land-on-main")
	if err != nil || workload.Spec.RepositoryChange == nil || workload.Spec.RepositoryChange.Delivery.Mode != "land-on-main" || workload.Status.Digest == "" {
		t.Fatalf("workload = %+v, %v", workload, err)
	}
	if tokens[0] != "Bearer first" || tokens[1] != "Bearer second" {
		t.Fatalf("tokens = %v", tokens)
	}
}

func TestClientSurfacesKubernetesStatusAndKeepsReplayBodiesExact(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(raw)
		bodies = append(bodies, string(raw))
		if r.Header.Get("Idempotency-Key") != "" {
			bodies = append(bodies, "key:"+r.Header.Get("Idempotency-Key"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write(fixture(t, "status-already-exists.json"))
	}))
	defer server.Close()
	client, err := New(Config{URL: server.URL, TokenFile: tokenFile(t, "token")})
	if err != nil {
		t.Fatal(err)
	}
	spec := json.RawMessage(`{"project":"acceptance","title":"T","acceptanceCriteria":"C","budget":{"timeMinutes":1,"maxTokens":1,"maxAttempts":1}}`)
	_, err = client.CreateRun(context.Background(), "factory", "task-0042", spec)
	var status *APIError
	if !errors.As(err, &status) || status.Code != 409 || status.Reason != "AlreadyExists" || !IsAlreadyExists(err) || !strings.Contains(status.Message, "different spec") {
		t.Fatalf("error = %#v", err)
	}
	if !strings.Contains(bodies[0], `"metadata":{"name":"task-0042"}`) || !strings.Contains(bodies[0], string(spec)) {
		t.Fatalf("body = %s", bodies[0])
	}
	if _, err := client.AnswerDecision(context.Background(), "factory", "d1", "Approve plan", "deck-d1"); err == nil || bodies[2] != "key:deck-d1" {
		t.Fatalf("answer = %v %v", err, bodies)
	}
}

func TestClientRefusesUnsafeEndpointsAndRedirects(t *testing.T) {
	for _, raw := range []string{"http://marina.example:8090", "https://user:pass@marina.example", "ftp://marina", "https://marina.example/?x=1", ""} {
		if _, err := New(Config{URL: raw, TokenFile: "/dev/null"}); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("redirect followed with %q", r.Header.Get("Authorization"))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	client, err := New(Config{URL: redirect.URL, TokenFile: tokenFile(t, "secret")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Capabilities(context.Background()); err == nil {
		t.Fatal("redirect was accepted")
	}
	missing, _ := New(Config{URL: redirect.URL, TokenFile: filepath.Join(t.TempDir(), "absent")})
	if _, err := missing.Capabilities(context.Background()); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("missing token = %v", err)
	}
}

func TestArtifactDownloadIsBoundedAndDigestChecked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("report"))
	}))
	defer server.Close()
	client, _ := New(Config{URL: server.URL, TokenFile: tokenFile(t, "token")})
	good := "sha256:" + "a8c1d09bb0b2c3cbd7f1cf3ec9d0ab4e0b4d3a8d23f4f8b4b3c62a3e7c8c3b5e"
	if _, err := client.RunArtifact(context.Background(), "factory", "run", good); err == nil {
		t.Fatal("a digest mismatch was accepted")
	}
	if _, err := client.RunArtifact(context.Background(), "factory", "run", "sha256:../../x"); err == nil {
		t.Fatal("an invalid digest was requested")
	}
	exact := Digest("report")
	body, err := client.RunArtifact(context.Background(), "factory", "run", exact)
	if err != nil || string(body) != "report" {
		t.Fatalf("artifact = %q, %v", body, err)
	}
}
