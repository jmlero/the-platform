package kubernetes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJobsCannotLaunchAgentAttempts(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	adapter, err := New(Config{ServerURL: server.URL, Namespace: "workers", Image: "verifier@sha256:" + strings.Repeat("a", 64), WorkerURL: server.URL, ServiceAccount: "worker", SigningKey: []byte(strings.Repeat("k", 32))}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"implement", "analyze"} {
		spec := testSpecification()
		spec.IdempotencyKey = "execution:" + stage
		if _, err := adapter.Start(context.Background(), spec); err == nil || !strings.Contains(err.Error(), "verification") {
			t.Fatalf("agent stage %q was admitted: %v", stage, err)
		}
	}
	if calls != 0 {
		t.Fatalf("rejected agent contacted Kubernetes %d times", calls)
	}
}
