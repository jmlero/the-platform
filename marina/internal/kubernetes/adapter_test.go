package kubernetes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
)

func TestStartReusesExactJobAfterLostResponseAndRejectsChangedSpecification(t *testing.T) {
	var job map[string]any
	starts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/secrets") {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if r.Method == http.MethodGet {
			if job == nil {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(job)
			return
		}
		if r.Method == http.MethodPost {
			starts++
			if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
				t.Error(err)
			}
			job["metadata"].(map[string]any)["uid"] = "uid-1"
			w.WriteHeader(503) // The API committed the Job, but its response was lost.
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()
	adapter, err := New(Config{ServerURL: server.URL, Namespace: "workers", Image: "worker@sha256:" + strings.Repeat("a", 64),
		WorkerURL: "https://marina.internal", ServiceAccount: "worker", SigningKey: []byte(strings.Repeat("k", 32))}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	spec := testSpecification()
	if _, err := adapter.Start(context.Background(), spec); err == nil {
		t.Fatal("lost response must be ambiguous")
	}
	observed, err := adapter.Start(context.Background(), spec)
	if err != nil || !observed.Found || starts != 1 {
		t.Fatalf("recovery = %#v, %v, starts=%d", observed, err, starts)
	}
	spec.SourceRevision = strings.Repeat("b", 40)
	if _, err := adapter.Start(context.Background(), spec); err == nil {
		t.Fatal("reused a Job with different immutable inputs")
	}
	template := job["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	if template["automountServiceAccountToken"] != false || template["restartPolicy"] != "Never" || template["serviceAccountName"] != "worker" {
		t.Fatalf("worker identity/isolation = %#v", template)
	}
}

func testSpecification() runtimeapi.Specification {
	return runtimeapi.Specification{ExecutionID: "execution", AttemptID: "attempt", IdempotencyKey: "start:attempt",
		RepositoryURI: "https://git.example/repo.git", SourceRevision: strings.Repeat("a", 40),
		ContextURI: "https://marina.internal/context", ContextDigest: "sha256:context", HarnessVersion: "worker/v1",
		Limits:        runtimeapi.Limits{CPUMillis: 1000, MemoryBytes: 1073741824, EphemeralStorageBytes: 1073741824, WallTimeSeconds: 600, MaxProcesses: 128},
		NetworkPolicy: "worker-restricted", FilesystemPolicy: "ephemeral", AuthorityFence: 1,
		DeadlineAt: time.Now().UTC().Add(10 * time.Minute), PolicyVersion: "v1"}
}

func TestAnalysisWorkerUsesOnlyAnInstalledPinnedImage(t *testing.T) {
	config := Config{ServerURL: "http://localhost:8080", Namespace: "workers", Image: "coding@sha256:" + strings.Repeat("a", 64), WorkerURL: "http://localhost:8090", ServiceAccount: "worker", SigningKey: []byte(strings.Repeat("k", 32)), AdditionalImages: []string{"analysis@sha256:" + strings.Repeat("b", 64)}}
	adapter, err := New(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := testSpecification()
	spec.WorkerImage = config.AdditionalImages[0]
	manifest := adapter.manifest(spec, "job", "digest")
	pod := manifest["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	if got := pod["containers"].([]any)[0].(map[string]any)["image"]; got != spec.WorkerImage {
		t.Fatalf("analysis image=%v", got)
	}
	spec.WorkerImage = "unapproved@sha256:" + strings.Repeat("c", 64)
	if _, err = adapter.Start(context.Background(), spec); err == nil {
		t.Fatal("unapproved worker image admitted")
	}
}
