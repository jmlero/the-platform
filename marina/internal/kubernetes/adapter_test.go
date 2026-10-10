package kubernetes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
)

func TestEngineCredentialWritesOnlyExclusiveSecretWithVersionPrecondition(t *testing.T) {
	spec := testSpecification()
	spec.Engine = &runtimeapi.EngineSelection{Backend: "kagent", Version: "pinned", Endpoint: "engine", Namespace: "trial", Harness: "codex", Template: "attempt-1", Revision: strings.Repeat("a", 64)}
	spec.CredentialReferences = []runtimeapi.CredentialReference{{Kind: "engine-gateway", Reference: "secret://trial/attempt-1/gateway", ExpiresAt: spec.DeadlineAt}}
	key := []byte(strings.Repeat("k", 32))
	credential := spec.AttemptID + "." + runtimeapi.CredentialToken(key, spec)
	data := base64.StdEncoding.EncodeToString([]byte("dummy"))
	patches := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/trial/secrets/attempt-1" {
			t.Errorf("unexpected Secret path %q", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": "Secret", "type": "Opaque", "metadata": map[string]string{"name": "attempt-1", "namespace": "trial", "resourceVersion": "7"}, "data": map[string]string{"gateway": data}})
		case http.MethodPatch:
			patches++
			if r.Header.Get("Content-Type") != "application/json-patch+json" {
				t.Errorf("patch content type %q", r.Header.Get("Content-Type"))
			}
			var ops []map[string]string
			if err := json.NewDecoder(r.Body).Decode(&ops); err != nil || len(ops) != 2 || ops[0]["op"] != "test" || ops[0]["path"] != "/metadata/resourceVersion" || ops[0]["value"] != "7" || ops[1]["op"] != "replace" || ops[1]["path"] != "/data/gateway" {
				t.Errorf("unsafe Secret patch: %v %v", ops, err)
			}
			data = ops[1]["value"]
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()
	adapter, err := New(Config{ServerURL: server.URL, Namespace: "workers", Image: "worker@sha256:" + strings.Repeat("a", 64), WorkerURL: "https://marina.internal", ServiceAccount: "worker", SigningKey: key}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := adapter.InstallEngineCredential(context.Background(), spec, credential); err != nil {
			t.Fatal(err)
		}
	}
	if patches != 1 || data != base64.StdEncoding.EncodeToString([]byte(credential)) {
		t.Fatal("credential write was not idempotent")
	}
	if err := adapter.InstallEngineCredential(context.Background(), spec, "wrong"); err == nil {
		t.Fatal("unbound credential accepted")
	}
}

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
	return runtimeapi.Specification{ExecutionID: "execution", AttemptID: "attempt", IdempotencyKey: "execution:verify",
		RepositoryURI: "https://git.example/repo.git", SourceRevision: strings.Repeat("a", 40),
		ContextURI: "https://marina.internal/context", ContextDigest: "sha256:context", HarnessVersion: "worker/v1",
		Limits:        runtimeapi.Limits{CPUMillis: 1000, MemoryBytes: 1073741824, EphemeralStorageBytes: 1073741824, WallTimeSeconds: 600, MaxProcesses: 128},
		NetworkPolicy: "worker-restricted", FilesystemPolicy: "ephemeral", AuthorityFence: 1,
		DeadlineAt: time.Now().UTC().Add(10 * time.Minute), PolicyVersion: "v1"}
}
