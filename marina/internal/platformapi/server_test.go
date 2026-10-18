package platformapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/platformapi/kubefake"
)

func serverFixture(t *testing.T) (*Server, *Controller, *kubefake.Server) {
	t.Helper()
	c, fake := controllerFixture(t)
	return &Server{Kube: c.Kube, Platform: c.Platform, Poll: 10 * time.Millisecond, Capabilities: func(context.Context) (Capabilities, error) {
		return Capabilities{WorkloadTypes: []string{"repository-change"}, Models: []ModelCapability{{Name: "implementer", Roles: []string{"implement"}}}, Tools: []ToolCapability{}, Limits: Limits{MaxTokens: 400000}}, nil
	}}, c, fake
}

func call(s *Server, method, path, body string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	r = r.WithContext(context.WithValue(r.Context(), userKey{}, User{Name: "system:serviceaccount:marina-system:deck"}))
	w := httptest.NewRecorder()
	s.serve(w, r)
	return w
}

const runs = "/apis/marina.dev/v1/namespaces/factory/runs"

func runBody(title string) string {
	return `{"apiVersion":"marina.dev/v1","kind":"Run","metadata":{"name":"task-9"},"spec":{"project":"acceptance","title":"` + title + `","acceptanceCriteria":"A test passes","budget":{"timeMinutes":30,"maxTokens":1000,"maxAttempts":7}}}`
}

func TestCreateRecordsTheCallerAndTheNameIsTheIdempotencyKey(t *testing.T) {
	s, _, fake := serverFixture(t)
	if w := call(s, "POST", runs, runBody("Reject an empty range")); w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	created := fake.Get("runs", "factory/task-9")
	if created["metadata"].(map[string]any)["annotations"].(map[string]any)[creatorAnnotation] != "system:serviceaccount:marina-system:deck" {
		t.Fatalf("creator not recorded: %+v", created["metadata"])
	}
	if w := call(s, "POST", runs, runBody("Reject an empty range")); w.Code != http.StatusOK {
		t.Fatalf("exact replay = %d %s", w.Code, w.Body)
	}
	if w := call(s, "POST", runs, runBody("Something else")); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "AlreadyExists") {
		t.Fatalf("changed spec = %d %s", w.Code, w.Body)
	}
	for name, body := range map[string]string{
		"wrong kind":      strings.Replace(runBody("x"), `"kind":"Run"`, `"kind":"Turn"`, 1),
		"other namespace": strings.Replace(runBody("x"), `"name":"task-9"`, `"name":"task-9","namespace":"other"`, 1),
		"bad name":        strings.Replace(runBody("x"), `"task-9"`, `"Task_9"`, 1),
		"not json":        "{",
	} {
		if w := call(s, "POST", runs, body); w.Code < 400 || w.Code >= 500 {
			t.Errorf("%s accepted: %d", name, w.Code)
		}
	}
	if w := call(s, "POST", "/apis/marina.dev/v1/namespaces/factory/decisions", `{}`); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("client created a decision: %d", w.Code)
	}
	if w := call(s, "GET", runs+"/task-9", ""); w.Code != http.StatusOK {
		t.Fatalf("get = %d", w.Code)
	}
	if w := call(s, "GET", runs+"/missing", ""); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"kind":"Status"`) {
		t.Fatalf("missing run = %d %s", w.Code, w.Body)
	}
}

func TestCancelAndAnswerAreIdempotentActions(t *testing.T) {
	s, _, fake := serverFixture(t)
	call(s, "POST", runs, runBody("Reject an empty range"))
	if w := call(s, "POST", runs+"/task-9/cancel", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("cancel without key = %d", w.Code)
	}
	for i := 0; i < 2; i++ {
		if w := call(s, "POST", runs+"/task-9/cancel", "", "Idempotency-Key", "cancel-1"); w.Code != http.StatusOK {
			t.Fatalf("cancel = %d %s", w.Code, w.Body)
		}
	}
	if fake.Get("runs", "factory/task-9")["spec"].(map[string]any)["cancel"] != true {
		t.Fatal("cancel not recorded on the resource")
	}
	fake.Put("decisions", map[string]any{"apiVersion": apiVersion, "kind": "Decision", "metadata": map[string]any{"name": "task-9-plan-1", "namespace": "factory"},
		"spec": map[string]any{"owner": map[string]any{"kind": "Run", "name": "task-9"}, "kind": "plan-approval", "question": "Approve?", "options": []any{"Approve plan", "Request changes"}}})
	decisions := "/apis/marina.dev/v1/namespaces/factory/decisions/task-9-plan-1/answer"
	if w := call(s, "POST", decisions, `{"answer":"maybe"}`, "Idempotency-Key", "a1"); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid option = %d", w.Code)
	}
	if w := call(s, "POST", decisions, `{"answer":"approve plan"}`, "Idempotency-Key", "a1"); w.Code != http.StatusOK {
		t.Fatalf("answer = %d %s", w.Code, w.Body)
	}
	response := fake.Get("decisions", "factory/task-9-plan-1")["spec"].(map[string]any)["response"].(map[string]any)
	if response["answer"] != "Approve plan" || response["answeredBy"] != "system:serviceaccount:marina-system:deck" {
		t.Fatalf("response = %+v", response)
	}
	if w := call(s, "POST", decisions, `{"answer":"Approve plan"}`, "Idempotency-Key", "a2"); w.Code != http.StatusOK {
		t.Fatalf("exact replay = %d", w.Code)
	}
	if w := call(s, "POST", decisions, `{"answer":"Request changes"}`, "Idempotency-Key", "a3"); w.Code != http.StatusConflict {
		t.Fatalf("different answer = %d", w.Code)
	}
}

func TestEventsArePagedAndStreamedAndArtifactsStayWithTheirRun(t *testing.T) {
	s, c, _ := serverFixture(t)
	call(s, "POST", runs, runBody("Reject an empty range"))
	cycle(t, c)
	w := call(s, "GET", runs+"/task-9/events?after=0", "")
	var page EventPage
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) == 0 || page.Items[0].Type != "run.accepted" || page.NextAfter != page.Items[len(page.Items)-1].Sequence {
		t.Fatalf("events = %d %s", w.Code, w.Body)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serve(w, r.WithContext(context.WithValue(r.Context(), userKey{}, User{Name: "viewer"})))
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", server.URL+runs+"/task-9/events", nil)
	request.Header.Set("Accept", "text/event-stream")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	first, _ := reader.ReadString('\n')
	response.Body.Close()
	if response.Header.Get("Content-Type") != "text/event-stream" || first != "id: 1\n" {
		t.Fatalf("stream started with %q (%s)", first, response.Header.Get("Content-Type"))
	}
	if w := call(s, "GET", runs+"/task-9/artifacts/sha256:"+strings.Repeat("a", 64), ""); w.Code != http.StatusNotFound {
		t.Fatalf("foreign artifact = %d", w.Code)
	}
	if w := call(s, "GET", "/apis/marina.dev/v1/capabilities", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"kind":"Capabilities"`) {
		t.Fatalf("capabilities = %d %s", w.Code, w.Body)
	}
}
