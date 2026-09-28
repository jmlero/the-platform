package flow

import (
	"context"
	"strings"
	"testing"

	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/worker"
)

type credentialOrderRuntime struct {
	runtimeapi.Adapter
	prepared  *bool
	inspected bool
}

func (r *credentialOrderRuntime) InspectByKey(context.Context, string) (runtimeapi.Observation, error) {
	r.inspected = true
	if !*r.prepared {
		return runtimeapi.Observation{}, runtimeapi.ErrConflict
	}
	return runtimeapi.Observation{Found: true, ProviderHandle: "kagent:existing", State: runtimeapi.StateRunning}, nil
}

func TestEngineCredentialIsInstalledBeforeRecoveryCanDispatch(t *testing.T) {
	prepared := false
	runtime := &credentialOrderRuntime{prepared: &prepared}
	controller := &Controller{PrepareEngine: func(context.Context, runtimeapi.Specification) error {
		prepared = true
		return nil
	}}
	spec := runtimeapi.Specification{IdempotencyKey: "one", Engine: &runtimeapi.EngineSelection{Backend: "kagent"}}
	observation, err := controller.inspectOrStart(context.Background(), runtime, spec)
	if err != nil || !runtime.inspected || !observation.Found {
		t.Fatalf("credential was not installed before dispatch-capable inspection: %#v %v", observation, err)
	}
}

func TestNativePromptBindsRepositoryAndStructuredResult(t *testing.T) {
	input := worker.Input{Stage: "implement", SourceURL: "https://marina.example/engine/git", SourceRevision: strings.Repeat("a", 40),
		Title: "Small change", AllowedPaths: []string{"app"}}
	prompt, err := enginePrompt(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"https://marina.example/engine/git", input.SourceRevision, `"allowed_paths":["app"]`, "text with the complete new file as a UTF-8 string", "Do not push"} {
		if !strings.Contains(string(prompt), required) {
			t.Fatalf("native prompt missing %q", required)
		}
	}
	input.Title = strings.Repeat("x", 4<<20)
	if _, err := enginePrompt(input); err == nil {
		t.Fatal("oversized native prompt accepted")
	}
}
