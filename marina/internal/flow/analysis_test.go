package flow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/contextpack"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/worker"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
)

func TestBYODiagnosticCheckpointPreservesGatewayModelAndToolAuthority(t *testing.T) {
	ctx := context.Background()
	j, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	deadline := time.Now().Add(time.Hour)
	e, _, err := j.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", LeaseExpiresAt: deadline, Budget: execution.Budget{DeadlineAt: deadline, MaxAttempts: 1, MaxTokens: 20000}})
	if err != nil {
		t.Fatal(err)
	}
	f := analysis.DiagnosticFixture()
	pin := strings.Repeat("a", 64)
	s := state{Analysis: &analysis.Profile{HarnessVersion: analysis.HarnessBYO, Fixture: f, Tools: []string{"read_context_source"}}, Base: f.InputDigest(), ProfileDigest: pin}
	spec := runtimeapi.Specification{IdempotencyKey: e.ID + ":analyze", Workload: analysis.Kind, InputDigest: s.Base, ProfileDigest: pin,
		WorkerImage: "analysis@sha256:" + pin, ContextURI: "artifact://sha256:" + pin, ContextDigest: pin, HarnessVersion: analysis.HarnessBYO,
		Tools: s.Analysis.Tools, Limits: runtimeapi.Limits{CPUMillis: 1000, MemoryBytes: 1024, EphemeralStorageBytes: 1024, WallTimeSeconds: 60, MaxProcesses: 64},
		NetworkPolicy: "gateway-only", FilesystemPolicy: "ephemeral", AuthorityFence: e.FenceToken, DeadlineAt: e.DeadlineAt, PolicyVersion: "test",
		Engine: &runtimeapi.EngineSelection{Backend: "kagent", Version: "pinned", Endpoint: "engine", Namespace: "trial", Harness: "analysis", Template: "attempt", Revision: pin}}
	e, attempt, _, err := j.EnsureRuntimeAttempt(ctx, e.ID, e.Version, spec)
	if err != nil {
		t.Fatal(err)
	}
	c := Controller{Journal: j, Config: Config{PublicURL: "https://marina.example", Model: workerapi.Model{Name: "fixture"}}}
	// No adapter is installed: stop at dispatch after persisting the real input.
	_, _, _, _, _ = c.worker(ctx, e, workcontrol.WorkItem{}, s, "analyze")
	gateway := workerapi.New(workerapi.Config{}, j, nil, nil, nil)
	input, err := gateway.Input(ctx, attempt)
	if err != nil {
		t.Fatalf("BYO checkpoint denied governed model/tool input: %v", err)
	}
	if input.Analysis == nil || input.Analysis.ModelURL != "https://marina.marina-system.svc/engine/model/v1" || input.Analysis.ToolURL != "https://marina.marina-system.svc/engine/tools/read-context-source" || len(input.Harness) != 0 {
		t.Fatalf("BYO checkpoint does not preserve the engine gateway boundary: %#v", input)
	}
}

func TestAnalysisEvidenceBindsContextForVersionTwo(t *testing.T) {
	store, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := Controller{Artifacts: store}
	fixture := analysis.DiagnosticFixture()
	report := analysis.Report{Summary: "Two blocked runs", Findings: fixture.Expected}
	s := state{Analysis: &analysis.Profile{HarnessVersion: analysis.HarnessWithContext, Fixture: fixture}, ProfileDigest: "profile", Base: fixture.InputDigest(), ContextDigest: "frozen-context"}
	output, _ := json.Marshal(worker.Result{Analysis: &report, ProfileDigest: s.ProfileDigest, InputDigest: s.Base})
	s.ImplementationDigest, err = store.Put(output)
	if err != nil {
		t.Fatal(err)
	}
	proof := analysis.Validation{Version: 1, Validator: "marina:diagnostic-validator/v1", ProfileDigest: s.ProfileDigest, InputDigest: s.Base, OutputDigest: s.ImplementationDigest, Report: report, Sources: fixture.Sources}
	for _, digest := range []string{"", "different-context", s.ContextDigest} {
		proof.ContextDigest = digest
		raw, _ := json.Marshal(proof)
		s.VerificationDigest, err = store.Put(raw)
		if err != nil {
			t.Fatal(err)
		}
		err = c.checkAnalysisEvidence(s)
		if (err == nil) != (digest == s.ContextDigest) {
			t.Fatalf("context %q: %v", digest, err)
		}
	}
	// Completed v1 records predate the optional context binding.
	s.Analysis.HarnessVersion = analysis.Harness
	proof.ContextDigest = ""
	raw, _ := json.Marshal(proof)
	s.VerificationDigest, err = store.Put(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.checkAnalysisEvidence(s); err != nil {
		t.Fatal(err)
	}
}

func TestAnalysisEngineMessageContainsFrozenInputsWithoutExpectedFindings(t *testing.T) {
	fixture := analysis.DiagnosticFixture()
	s := state{Analysis: &analysis.Profile{HarnessVersion: analysis.HarnessBYO, Fixture: fixture, Tools: []string{"read_context_source"}}, ProfileDigest: strings.Repeat("a", 64), Base: fixture.InputDigest(), ContextDigest: "sha256:" + strings.Repeat("b", 64)}
	manifest := contextpack.Package{Digest: s.ContextDigest}
	message, err := analysisEngineMessage(s, manifest, "fixture-model")
	if err != nil {
		t.Fatal(err)
	}
	var input analysis.Input
	if err := json.Unmarshal(message, &input); err != nil {
		t.Fatal(err)
	}
	if input.InputDigest != s.Base || input.ProfileDigest != s.ProfileDigest || input.Context == nil || input.Context.Digest != s.ContextDigest || input.ModelURL != "https://marina.marina-system.svc/engine/model/v1" || input.ToolURL != "https://marina.marina-system.svc/engine/tools/read-context-source" {
		t.Fatalf("unbound engine input: %#v", input)
	}
	if strings.Contains(string(message), `"expected":`) || strings.Contains(string(message), `"repository_uri":`) {
		t.Fatal("validator answers or repository fields reached the BYO harness")
	}
}
