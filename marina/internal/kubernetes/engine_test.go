package kubernetes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
)

// fakeEngineAPI stores Secrets, ModelConfigs and AgentTemplates by path and
// reports an AgentTemplate Ready only after readyAfter status reads.
type fakeEngineAPI struct {
	mu         sync.Mutex
	objects    map[string]map[string]any
	creates    map[string]int
	deletes    int
	reads      int
	readyAfter int
	loseCreate bool
}

func (f *fakeEngineAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	switch r.Method {
	case http.MethodPost:
		var object map[string]any
		if err := json.NewDecoder(r.Body).Decode(&object); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		key := path + "/" + object["metadata"].(map[string]any)["name"].(string)
		if _, ok := f.objects[key]; ok {
			w.WriteHeader(http.StatusConflict)
			return
		}
		object["metadata"].(map[string]any)["uid"] = "uid-" + key
		object["metadata"].(map[string]any)["generation"] = 1
		f.objects[key] = object
		f.creates[kindOf(path)]++
		if f.loseCreate {
			f.loseCreate = false
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(object)
	case http.MethodGet:
		object, ok := f.objects[path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if kindOf(path) == "agenttemplates" {
			f.reads++
			if f.reads > f.readyAfter {
				harness := object["metadata"].(map[string]any)["labels"].(map[string]any)["kagent.dev/harness"]
				revision := strings.Repeat("c", 64)
				object["status"] = map[string]any{"observedGeneration": 1, "harnesses": []any{map[string]any{
					"harness": harness, "desiredRevision": revision, "latestSuccessfulRevision": revision,
					"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}}}
			}
		}
		_ = json.NewEncoder(w).Encode(object)
	case http.MethodDelete:
		if _, ok := f.objects[path]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var options struct {
			Preconditions struct {
				UID string `json:"uid"`
			} `json:"preconditions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&options)
		if options.Preconditions.UID != f.objects[path]["metadata"].(map[string]any)["uid"] {
			w.WriteHeader(http.StatusConflict)
			return
		}
		delete(f.objects, path)
		f.deletes++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func kindOf(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "namespaces" && i+2 < len(parts) {
			return parts[i+2]
		}
	}
	return ""
}

func engineTestAdapter(t *testing.T, api *fakeEngineAPI) *Adapter {
	t.Helper()
	api.objects, api.creates = map[string]map[string]any{}, map[string]int{}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	adapter, err := New(Config{ServerURL: server.URL, Namespace: "workers", Image: "worker@sha256:" + strings.Repeat("a", 64),
		WorkerURL: "https://marina.internal", ServiceAccount: "worker", SigningKey: []byte(strings.Repeat("k", 32))}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func testEngineTemplate() runtimeapi.EngineTemplate {
	return runtimeapi.EngineTemplate{Namespace: "kint-engine", Name: runtimeapi.EngineTemplateName("execution:implement"), Harness: "kint-codex",
		Description: "Bounded repository implementation attempt", SystemPrompt: "Follow the frozen task.",
		Model: "marina-governed", BaseURL: "https://marina.marina-system.svc/engine/model/v1"}
}

func TestEngineTemplateIsCreatedOnceAndReturnsItsReadyRevision(t *testing.T) {
	api := &fakeEngineAPI{loseCreate: true}
	adapter := engineTestAdapter(t, api)
	template := testEngineTemplate()
	ctx := context.Background()
	// The first Secret creation commits but its response is lost.
	if _, err := adapter.EnsureEngineTemplate(ctx, template); err == nil || errors.Is(err, runtimeapi.ErrEngineTemplatePending) {
		t.Fatalf("lost create response = %v, want an ambiguous error", err)
	}
	if _, err := adapter.EnsureEngineTemplate(ctx, template); !errors.Is(err, runtimeapi.ErrEngineTemplatePending) {
		t.Fatalf("unprepared template = %v, want pending", err)
	}
	revision, err := adapter.EnsureEngineTemplate(ctx, template)
	if err != nil || revision != strings.Repeat("c", 64) {
		t.Fatalf("prepared revision = %q, %v", revision, err)
	}
	for _, kind := range []string{"secrets", "modelconfigs", "agenttemplates"} {
		if api.creates[kind] != 1 {
			t.Fatalf("%s created %d times", kind, api.creates[kind])
		}
	}
	secret := api.objects["/api/v1/namespaces/kint-engine/secrets/"+template.Name]
	data := secret["data"].(map[string]any)
	placeholder, _ := base64.StdEncoding.DecodeString(data["gateway"].(string))
	if len(data) != 1 || len(placeholder) < 32 || secret["type"] != "Opaque" || ownerLabel(secret) != "marina" {
		t.Fatalf("attempt Secret = %#v", secret)
	}
	model := api.objects["/apis/kagent.dev/v1alpha3/namespaces/kint-engine/modelconfigs/"+template.Name+"-model"]["spec"].(map[string]any)
	if model["apiKeySecret"] != template.Name || model["apiKeySecretKey"] != "gateway" || model["provider"] != "OpenAI" ||
		model["openAI"].(map[string]any)["baseUrl"] != template.BaseURL || model["openAI"].(map[string]any)["apiFormat"] != "responses" {
		t.Fatalf("ModelConfig = %#v", model)
	}
	agent := api.objects["/apis/kagent.dev/v1alpha3/namespaces/kint-engine/agenttemplates/"+template.Name]
	if agent["metadata"].(map[string]any)["labels"].(map[string]any)["kagent.dev/harness"] != "kint-codex" || ownerLabel(agent) != "marina" ||
		agent["spec"].(map[string]any)["modelConfig"].(map[string]any)["name"] != template.Name+"-model" {
		t.Fatalf("AgentTemplate = %#v", agent)
	}
}

func TestEngineTemplateNeverAdoptsForeignOrChangedResources(t *testing.T) {
	api := &fakeEngineAPI{}
	adapter := engineTestAdapter(t, api)
	template := testEngineTemplate()
	if _, err := adapter.EnsureEngineTemplate(context.Background(), template); !errors.Is(err, runtimeapi.ErrEngineTemplatePending) {
		t.Fatal(err)
	}
	changed := template
	changed.SystemPrompt = "Different instructions."
	if _, err := adapter.EnsureEngineTemplate(context.Background(), changed); err == nil || errors.Is(err, runtimeapi.ErrEngineTemplatePending) {
		t.Fatalf("changed template adopted: %v", err)
	}
	foreign := template
	foreign.Name = "kint16-chat-24"
	if _, err := adapter.EnsureEngineTemplate(context.Background(), foreign); err == nil {
		t.Fatal("created a template outside Marina's name prefix")
	}
	key := "/apis/kagent.dev/v1alpha3/namespaces/kint-engine/agenttemplates/" + template.Name
	delete(api.objects[key]["metadata"].(map[string]any)["labels"].(map[string]any), "the-platform.dev/owner")
	if _, err := adapter.EnsureEngineTemplate(context.Background(), template); err == nil || errors.Is(err, runtimeapi.ErrEngineTemplatePending) {
		t.Fatalf("unowned template adopted: %v", err)
	}
	if err := adapter.DeleteEngineTemplate(context.Background(), template.Namespace, template.Name); err == nil {
		t.Fatal("deleted a template Marina does not own")
	}
	if _, ok := api.objects[key]; !ok {
		t.Fatal("unowned template was removed")
	}
}

func TestDeleteEngineTemplateRemovesTheAttemptResourcesIdempotently(t *testing.T) {
	api := &fakeEngineAPI{}
	adapter := engineTestAdapter(t, api)
	template := testEngineTemplate()
	if _, err := adapter.EnsureEngineTemplate(context.Background(), template); !errors.Is(err, runtimeapi.ErrEngineTemplatePending) {
		t.Fatal(err)
	}
	for range 2 {
		if err := adapter.DeleteEngineTemplate(context.Background(), template.Namespace, template.Name); err != nil {
			t.Fatal(err)
		}
	}
	if len(api.objects) != 0 || api.deletes != 3 {
		t.Fatalf("remaining=%d deletes=%d", len(api.objects), api.deletes)
	}
	if err := adapter.DeleteEngineTemplate(context.Background(), "kint-engine", "kint-engine-postgresql"); err == nil {
		t.Fatal("accepted a name outside Marina's prefix")
	}
}

func ownerLabel(object map[string]any) any {
	return object["metadata"].(map[string]any)["labels"].(map[string]any)["the-platform.dev/owner"]
}
