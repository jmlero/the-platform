package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestWorkloadAPIRequiresExplicitAuthorityAndIdempotency(t *testing.T) {
	s, h := newTestStoreAndHandler(t)
	capabilities := requestJSON(t, h, "GET", "/api/v1/operator/provider", "", nil)
	if capabilities.Code != 200 || !strings.Contains(capabilities.Body.String(), `"workload.invoke"`) {
		t.Fatalf("operator capability discovery: %d %s", capabilities.Code, capabilities.Body.String())
	}
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

func TestWorkloadPublicationRequiresProjectAndCapabilityGrants(t *testing.T) {
	s, _ := newTestStoreAndHandler(t)
	path := filepath.Join(t.TempDir(), "auth.json")
	for _, test := range []struct {
		capability, project string
		status              int
	}{
		{capabilityWorkloadPublish, "prj_other", 403},
		{capabilityProviderInfo, "prj_default", 403},
		{capabilityWorkloadPublish, "prj_default", 200},
	} {
		writeServiceAuthPolicy(t, path, serviceAuthTokenOne, []string{test.capability}, []string{test.project})
		auth, err := NewFileServiceAuthorizer(path)
		if err != nil {
			t.Fatal(err)
		}
		h := New(s, slog.New(slog.NewTextHandler(io.Discard, nil)), WithServiceAuthorizer(auth)).Handler()
		body, _ := json.Marshal(domain.WorkloadCatalog{Profiles: []domain.WorkloadProfile{}, ExpiresAt: time.Now().Add(time.Minute).UnixMilli()})
		r := httptest.NewRequest("PUT", "/api/v1/projects/prj_default/workloads", strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+serviceAuthTokenOne)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("%s/%s: %d %s", test.capability, test.project, w.Code, w.Body.String())
		}
	}
}
