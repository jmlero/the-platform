package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestWorkloadAPIRequiresExplicitAuthorityAndIdempotency(t *testing.T) {
	s, h := newTestStoreAndHandler(t)
	p := domain.WorkloadProfile{ID: "diagnostic-analysis/v1", Digest: strings.Repeat("a", 64), Harness: "langchain-create-agent/v2", Authority: []string{"analysis.run." + strings.Repeat("a", 64)}, MaxTokens: 20000, BudgetMinutes: 5, MinAttempts: 1, Tools: []string{}, ContextKinds: []string{"document"}}
	if err := s.PublishWorkloads(context.Background(), "prj_default", "local-prototype", domain.WorkloadCatalog{Profiles: []domain.WorkloadProfile{p}, ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/operator/projects/prj_default/workloads"
	w := requestJSON(t, h, "GET", path, "", nil)
	if w.Code != 200 {
		t.Fatalf("discover: %d %s", w.Code, w.Body.String())
	}
	in := domain.WorkloadInvocation{ProfileID: p.ID, ProfileDigest: p.Digest, Task: domain.CreateTaskInput{Title: "Analyze", AcceptanceCriteria: "Validated findings", Budget: &domain.Budget{TimeMinutes: 5, MaxTokens: 10000, MaxAttempts: 1, MaxConcurrency: 1}}}
	w = requestJSON(t, h, "POST", path+"/invoke", "invoke", in)
	if w.Code != 422 {
		t.Fatalf("missing authority: %d %s", w.Code, w.Body.String())
	}
	in.Task.Budget.Authority = p.Authority
	w = requestJSON(t, h, "POST", path+"/invoke", "", in)
	if w.Code != 422 {
		t.Fatalf("missing key: %d", w.Code)
	}
	w = requestJSON(t, h, "POST", path+"/invoke", "invoke", in)
	if w.Code != 201 {
		t.Fatalf("invoke: %d %s", w.Code, w.Body.String())
	}
	var first domain.Task
	json.Unmarshal(w.Body.Bytes(), &first)
	w = requestJSON(t, h, "POST", path+"/invoke", "invoke", in)
	var replay domain.Task
	json.Unmarshal(w.Body.Bytes(), &replay)
	if w.Code != 201 || replay.ID != first.ID || replay.Workload == nil {
		t.Fatalf("replay: %d %s", w.Code, w.Body.String())
	}
}
