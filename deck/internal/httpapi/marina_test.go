package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/marina"
	"github.com/jmlero/the-platform/deck/internal/store"
)

// marinaDouble records the actions Deck forwards and answers with a status
// the test chooses.
type marinaDouble struct {
	mu       sync.Mutex
	requests []string
	refuse   int
	artifact []byte
}

func (m *marinaDouble) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	m.requests = append(m.requests, r.Method+" "+r.URL.Path+" "+r.Header.Get("Idempotency-Key")+" "+strings.TrimSpace(string(body)))
	w.Header().Set("Content-Type", "application/json")
	if m.refuse != 0 {
		w.WriteHeader(m.refuse)
		_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"Conflict","message":"decision has expired","code":409}`))
		return
	}
	if strings.Contains(r.URL.Path, "/artifacts/") {
		_, _ = w.Write(m.artifact)
		return
	}
	_, _ = w.Write([]byte(`{"apiVersion":"marina.dev/v1","metadata":{"name":"x"}}`))
}

func newMarinaHandler(t *testing.T) (*store.Store, http.Handler, *marinaDouble) {
	t.Helper()
	appStore, _ := newTestStoreAndHandler(t)
	double := &marinaDouble{}
	server := httptest.NewServer(double)
	t.Cleanup(server.Close)
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("deck-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := marina.New(marina.Config{URL: server.URL, TokenFile: token})
	if err != nil {
		t.Fatal(err)
	}
	return appStore, New(appStore, slog.New(slog.NewTextHandler(io.Discard, nil)), WithMarina(client)).Handler(), double
}

// marinaRunWaitingForApproval puts one Marina-owned run in waiting_decision.
func marinaRunWaitingForApproval(t *testing.T, s *store.Store) (domain.Run, domain.Decision) {
	t.Helper()
	ctx := context.Background()
	if err := s.EnsureMarinaAgent(ctx, []string{"prj_default"}); err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Marina work", AcceptanceCriteria: "Done", Status: domain.TaskReady})
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.BeginMarinaRun(ctx, task.ID, task.Version, "factory", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	guard := store.Mutation{ActorID: "marina", IdempotencyKey: "plan", Guard: &store.RunGuard{AgentID: run.AgentID, ExpectedVersion: run.Version, FenceToken: run.FenceToken}}
	_, decision, err := s.SubmitPlanRevision(ctx, guard, run.ID, domain.PlanRevisionInput{Plan: "Plan", ContextDigest: "sha256:x", RiskClass: "low", VerificationStrategy: "v", RollbackApproach: "r"},
		domain.DecisionRequest{ID: run.ID + "-plan-1", Required: true})
	if err != nil {
		t.Fatal(err)
	}
	run, _ = s.GetRun(ctx, run.ID)
	return run, *decision
}

func TestPlanAnswersAreForwardedToMarinaBeforeDeckRecordsThem(t *testing.T) {
	s, handler, double := newMarinaHandler(t)
	_, decision := marinaRunWaitingForApproval(t, s)
	double.refuse = http.StatusConflict
	response := requestJSON(t, handler, http.MethodPost, "/api/v1/operator/decisions/"+decision.ID+"/answer", "", map[string]string{"answer": "Approve plan"})
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "decision has expired") {
		t.Fatalf("refused answer = %d %s", response.Code, response.Body)
	}
	if current, _ := s.GetDecision(context.Background(), decision.ID); current.Status != "pending" {
		t.Fatalf("Deck recorded an answer Marina refused: %+v", current)
	}
	double.refuse = 0
	response = requestJSON(t, handler, http.MethodPost, "/api/v1/operator/decisions/"+decision.ID+"/answer", "", map[string]string{"answer": "Approve plan"})
	if response.Code != http.StatusOK {
		t.Fatalf("answer = %d %s", response.Code, response.Body)
	}
	want := "POST /apis/marina.dev/v1/namespaces/factory/decisions/" + decision.ID + "/answer deck-" + decision.ID + ` {"answer":"Approve plan"}`
	if double.requests[len(double.requests)-1] != want {
		t.Fatalf("forwarded %q", double.requests)
	}
	if current, _ := s.GetDecision(context.Background(), decision.ID); current.Status != "answered" {
		t.Fatalf("decision = %+v", current)
	}
}

func TestOnlyCancelIsForwardedAndARepeatedCancelReturnsTheSameCommand(t *testing.T) {
	s, handler, double := newMarinaHandler(t)
	run, _ := marinaRunWaitingForApproval(t, s)
	path := "/api/v1/operator/runs/" + run.ID + "/commands"
	for _, kind := range []string{"pause", "resume", "replan", "retry"} {
		response := requestJSON(t, handler, http.MethodPost, path, "", map[string]string{"type": kind, "reason": "r"})
		assertAPIError(t, response, http.StatusConflict, errorCodeConflict)
	}
	if len(double.requests) != 0 {
		t.Fatalf("unsupported commands reached Marina: %v", double.requests)
	}
	first := requestJSON(t, handler, http.MethodPost, path, "", map[string]string{"type": "cancel", "reason": "No longer needed"})
	again := requestJSON(t, handler, http.MethodPost, path, "", map[string]string{"type": "cancel", "reason": "No longer needed"})
	if first.Code != http.StatusCreated || again.Code != http.StatusOK || len(double.requests) != 1 ||
		double.requests[0] != "POST /apis/marina.dev/v1/namespaces/factory/runs/"+run.ID+"/cancel deck-cancel-"+run.ID+" " {
		t.Fatalf("cancel = %d %d %v", first.Code, again.Code, double.requests)
	}
	detail, _ := s.GetTaskDetail(context.Background(), run.TaskID)
	if len(detail.Commands) != 1 || detail.Commands[0].Status != "pending" {
		t.Fatalf("commands = %+v", detail.Commands)
	}
}

func TestEvidenceDownloadIsProxiedFromMarinaWithItsDigestChecked(t *testing.T) {
	s, handler, double := newMarinaHandler(t)
	run, _ := marinaRunWaitingForApproval(t, s)
	ctx := context.Background()
	double.artifact = []byte(`{"tests":44}`)
	sum := sha256.Sum256(double.artifact)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	run, _ = s.GetRun(ctx, run.ID)
	if _, err := s.RecordEvidence(ctx, domain.EvidenceInput{ID: "ev-1", RunID: run.ID, PlanRevisionID: *run.ActivePlanRevisionID, Type: "test", Producer: "marina",
		Verifier: "marina/verification", SubjectDigest: "s", SourceRevision: strings.Repeat("a", 40), Command: "unit", Outcome: "passed", ProvenanceURI: "marina:x",
		ProvenanceDigest: "sha256:p", ObservedAt: 1, ArtifactURI: "marina:factory/runs/" + run.ID + "/artifacts/" + digest, ArtifactDigest: digest}); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/operator/tasks/"+run.TaskID+"/artifacts/"+digest, nil))
	if response.Code != http.StatusOK || response.Body.String() != `{"tests":44}` || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("download = %d %s", response.Code, response.Body)
	}
	double.artifact = []byte(`tampered`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/operator/tasks/"+run.TaskID+"/artifacts/"+digest, nil))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("tampered download = %d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/operator/tasks/"+run.TaskID+"/artifacts/sha256:"+strings.Repeat("0", 64), nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("unregistered download = %d", response.Code)
	}
}

func TestWithoutMarinaTheProviderIsUnavailableAndNothingIsForwarded(t *testing.T) {
	s, handler := newTestStoreAndHandler(t)
	run, decision := marinaRunWaitingForApproval(t, s)
	response := requestJSON(t, handler, http.MethodGet, "/api/v1/bootstrap", "", nil)
	var bootstrap bootstrapResponse
	if err := json.Unmarshal(response.Body.Bytes(), &bootstrap); err != nil || bootstrap.ProviderStatus.Connectivity != "unavailable" {
		t.Fatalf("bootstrap = %+v %v", bootstrap.ProviderStatus, err)
	}
	response = requestJSON(t, handler, http.MethodPost, "/api/v1/operator/decisions/"+decision.ID+"/answer", "", map[string]string{"answer": "Approve plan"})
	assertAPIError(t, response, http.StatusServiceUnavailable, errorCodeServiceUnavailable)
	response = requestJSON(t, handler, http.MethodPost, "/api/v1/operator/runs/"+run.ID+"/commands", "", map[string]string{"type": "cancel", "reason": "r"})
	assertAPIError(t, response, http.StatusServiceUnavailable, errorCodeServiceUnavailable)
}
