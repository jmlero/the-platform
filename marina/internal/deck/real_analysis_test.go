package deck

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jmlero/the-platform/marina/internal/kubernetes"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/flow"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/reconcile"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/worker"
)

func TestRealAnalysisWorkflow(t *testing.T) {
	if os.Getenv("DECK_CONFORMANCE_URL") == "" {
		t.Skip("run make conformance")
	}
	client, err := New(os.Getenv("DECK_CONFORMANCE_URL"), &http.Client{Timeout: 3 * time.Second}, WithBearerTokenFile(os.Getenv("DECK_CONFORMANCE_TOKEN_FILE")))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err = waitForProvider(ctx, client); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"api_invocation", "api_cancel", "context_revised", "context_revoked", "completed", "missing_finding", "mismatched_digest", "missing_result", "corrupt_result", "corrupt_validation", "profile_changed", "wrong_authority"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			db := filepath.Join(dir, "journal.db")
			journal, err := sqlite.Open(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { journal.Close() }()
			identity, err := journal.EnsureInstallationIdentity(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.RegisterAgent(ctx, workcontrol.AgentRegistration{ID: identity.LogicalAgentID, InstallationID: identity.InstallationID, Name: "Analysis proof", Kind: "controller", Capabilities: workcontrol.WorkerCapabilities()})
			if err != nil {
				t.Fatal(err)
			}
			config := flow.Config{ProjectID: "prj_default", PublicURL: "https://marina.internal", PolicyVersion: "analysis/v1", MaxTokens: 20000, BudgetMinutes: 5, Analysis: &analysis.Profile{Version: 1, Revision: 1, Image: "analysis@sha256:" + strings.Repeat("a", 64), HarnessVersion: analysis.Harness, MaxTokens: 20000, BudgetMinutes: 5, Fixture: analysis.DiagnosticFixture()}}
			binding, err := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "prj_default", Provider: "deck", Runtime: "test", PolicyVersion: config.PolicyVersion, ContextConfig: config.Digest(), ArtifactConfig: "test", BudgetMinutes: 5, MaxConcurrency: 1})
			if err != nil {
				t.Fatal(err)
			}
			isAPI := scenario == "api_invocation" || scenario == "api_cancel" || scenario == "context_revised" || scenario == "context_revoked"
			var selectedSource workcontrol.ContextSource
			if isAPI {
				config.Analysis.Tools = []string{"read_context_source"}
				config.Analysis.HarnessVersion = analysis.HarnessWithContext
				sourceInput := workcontrol.ContextSourceInput{ID: "agent-" + strings.ReplaceAll(scenario, "_", "-"), Kind: "instruction", Title: "Analysis guidance", Content: "Keep the frozen diagnostic report concise.", Trust: "guidance", State: "active"}
				if err = client.do(ctx, "PUT", "/api/v1/operator/projects/prj_default/context/sources", sourceInput, &selectedSource); err != nil {
					t.Fatal(err)
				}
			}
			grant := config.AnalysisAuthority()
			if scenario == "wrong_authority" {
				grant = "analysis.run." + strings.Repeat("f", 64)
			}
			var dto taskDTO
			taskInput := map[string]any{"project_id": "prj_default", "title": "Analyze synthetic diagnostics " + scenario, "acceptance_criteria": "Find provider outage and token denial with exact source evidence; ignore healthy record", "status": "ready", "budget_minutes": 5, "budget": map[string]any{"time_minutes": 5, "max_attempts": 1, "max_tokens": 20000, "max_concurrency": 1, "authority": []string{grant}}}
			if isAPI {
				if err = client.PublishWorkloads(ctx, "prj_default", workcontrol.WorkloadCatalog{Profiles: config.WorkloadProfiles(), ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}); err != nil {
					t.Fatal(err)
				}
				var catalog workcontrol.WorkloadCatalog
				var discovery struct {
					Capabilities []string `json:"capabilities"`
				}
				if err = client.do(ctx, "GET", "/api/v1/operator/provider", nil, &discovery); err != nil || !slices.Contains(discovery.Capabilities, "workload.invoke") {
					t.Fatalf("operator capabilities: %+v %v", discovery, err)
				}
				if err = client.do(ctx, "GET", "/api/v1/operator/projects/prj_default/workloads", nil, &catalog); err != nil || len(catalog.Profiles) != 1 {
					t.Fatalf("catalog: %+v %v", catalog, err)
				}
				invoke := map[string]any{"profile_id": analysis.Kind, "profile_digest": catalog.Profiles[0].Digest, "task": taskInput}
				endpoint := "/api/v1/operator/projects/prj_default/workloads/invoke"
				if err = client.doIdempotent(ctx, "POST", endpoint, "invoke:"+scenario, invoke, &dto); err != nil {
					t.Fatal(err)
				}
				var replay taskDTO
				if err = client.doIdempotent(ctx, "POST", endpoint, "invoke:"+scenario, invoke, &replay); err != nil || replay.ID != dto.ID || replay.Workload == nil {
					t.Fatalf("invocation replay: %+v %v", replay, err)
				}
			} else {
				err = client.do(ctx, "POST", "/api/v1/tasks", taskInput, &dto)
			}
			if err != nil {
				t.Fatal(err)
			}
			store, err := artifacts.New(filepath.Join(dir, "artifacts"))
			if err != nil {
				t.Fatal(err)
			}
			runtime := &analysisRuntime{journal: journal, artifacts: store, artifactRoot: filepath.Join(dir, "artifacts"), scenario: scenario, observations: map[string]runtimeapi.Observation{}, provider: client}
			controller := &flow.Controller{Config: config, Journal: journal, Provider: client, Runtime: runtime, Artifacts: store}
			runtime.contextCheck = controller.CheckContext
			var e execution.Execution
			for cycle := 0; cycle < 24; cycle++ {
				control, err := reconcile.NewControlController(client, journal, binding, identity, 2*time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				if result := control.WithIntakeEnabled(cycle == 0).WithExecutionLimits(2, 20000).WithCommandHandler(controller.Command).Cycle(ctx); len(result.Errors) > 0 {
					t.Fatal(result.Errors)
				}
				if e.ID != "" {
					current, loadErr := journal.GetExecution(ctx, e.ID)
					if loadErr != nil {
						t.Fatal(loadErr)
					}
					if current.State.Terminal() {
						e = current
						break
					}
				}
				active, err := journal.ListActiveExecutions(ctx, 10)
				if err != nil || len(active) != 1 {
					t.Fatalf("active executions=%v: %v", active, err)
				}
				e = active[0]
				if cycle == 3 && scenario == "context_revised" {
					input := selectedSource.ContextSourceInput
					input.ExpectedRevision = selectedSource.Revision
					input.Content = "New guidance must not enter the active run."
					if err = client.do(ctx, "PUT", "/api/v1/operator/projects/prj_default/context/sources", input, nil); err != nil {
						t.Fatal(err)
					}
				}
				if cycle == 3 && scenario == "context_revoked" {
					if err = client.do(ctx, "POST", fmt.Sprintf("/api/v1/operator/projects/prj_default/context/sources/%s/%d/revoke", selectedSource.ID, selectedSource.Revision), nil, nil); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "profile_changed" && cycle == 2 {
					controller.Config.Analysis.Revision++
				}
				if scenario == "corrupt_validation" {
					checkpoint, checkpointErr := journal.LatestPhaseCheckpoint(ctx, e.ID, "repository_flow")
					if checkpointErr == nil {
						var saved struct{ Step, VerificationDigest string }
						if err := json.Unmarshal([]byte(checkpoint.Payload["state"].(string)), &saved); err != nil {
							t.Fatal(err)
						}
						if saved.Step == "complete" {
							if err := os.WriteFile(filepath.Join(dir, "artifacts", strings.TrimPrefix(saved.VerificationDigest, "sha256:")), []byte("corrupted validation"), 0600); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
				if failures := controller.Cycle(ctx); len(failures) > 0 {
					t.Fatalf("cycle %d: %v", cycle, failures)
				}
				events, err := client.GetWork(ctx, dto.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(events.Runs) == 0 {
					t.Fatal("task was not claimed")
				}
				e, err = journal.GetExecution(ctx, e.ID)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "api_cancel" && cycle == 2 {
					if err = client.do(ctx, "POST", "/api/v1/operator/runs/"+e.ExternalRunID+"/commands", map[string]any{"type": "cancel", "reason": "API cancellation proof"}, nil); err != nil {
						t.Fatal(err)
					}
				}
				if err = journal.Close(); err != nil {
					t.Fatal(err)
				}
				journal, err = sqlite.Open(ctx, db)
				if err != nil {
					t.Fatal(err)
				}
				controller.Journal = journal
				runtime.journal = journal
				if e.State.Terminal() {
					break
				}
			}
			want := execution.StateBlocked
			if scenario == "api_cancel" {
				want = execution.StateCancelled
			}
			if scenario == "completed" || scenario == "api_invocation" || scenario == "context_revised" {
				want = execution.StateCompleted
			}
			if e.State != want {
				t.Fatalf("state=%s want=%s reason=%s", e.State, want, e.FailureMessage)
			}
			if scenario == "completed" || scenario == "api_invocation" || scenario == "context_revised" {
				if runtime.starts != 1 {
					t.Fatalf("starts=%d", runtime.starts)
				}
				evidence, err := journal.ListEvidence(ctx, e.ID)
				if err != nil || len(evidence) != 1 || evidence[0].SourceRevision != config.Analysis.Fixture.InputDigest() {
					t.Fatalf("evidence=%+v: %v", evidence, err)
				}
				detail, err := client.GetWork(ctx, dto.ID)
				if err != nil || detail.Runs[0].Status != workcontrol.RunCompleted || !strings.Contains(detail.Runs[0].ResultSummary, "analysis") {
					t.Fatalf("result=%+v: %v", detail, err)
				}
			}
			if isAPI {
				input := selectedSource.ContextSourceInput
				input.ExpectedRevision = selectedSource.Revision
				if scenario == "context_revised" {
					input.ExpectedRevision++
				}
				input.State = "inactive"
				if err = client.do(ctx, "PUT", "/api/v1/operator/projects/prj_default/context/sources", input, nil); err != nil {
					t.Fatal(err)
				}
			}
			if (scenario == "wrong_authority" || scenario == "profile_changed" || scenario == "context_revoked" || scenario == "api_cancel") && runtime.starts != 0 {
				t.Fatal("changed/unauthorized profile reached worker")
			}
		})
	}
}

type analysisRuntime struct {
	provider     workcontrol.WorkInspection
	contextCheck func(context.Context, execution.Execution) error
	runtimeapi.Adapter
	journal      *sqlite.Store
	artifacts    *artifacts.Store
	artifactRoot string
	scenario     string
	starts       int
	observations map[string]runtimeapi.Observation
}

func (r *analysisRuntime) InspectByKey(_ context.Context, key string) (runtimeapi.Observation, error) {
	return r.observations[key], nil
}
func (r *analysisRuntime) Cleanup(context.Context, string) error { return nil }
func (r *analysisRuntime) Start(ctx context.Context, s runtimeapi.Specification) (runtimeapi.Observation, error) {
	if s.RepositoryURI != "" || s.SourceRevision != "" || s.Workload != analysis.Kind {
		return runtimeapi.Observation{}, fmt.Errorf("analysis acquired repository semantics")
	}
	cp, err := r.journal.GetCheckpoint(ctx, s.ExecutionID, "worker-input:"+s.AttemptID)
	if err != nil {
		return runtimeapi.Observation{}, err
	}
	var input worker.Input
	if err = json.Unmarshal([]byte(cp.Payload["input"].(string)), &input); err != nil {
		return runtimeapi.Observation{}, err
	}
	if input.Analysis == nil || input.Analysis.ProfileDigest != s.ProfileDigest || input.SourceURL != "" || strings.Contains(strings.Join(input.Harness, " "), "codex") {
		return runtimeapi.Observation{}, fmt.Errorf("incorrect analysis input")
	}
	if r.scenario == "api_invocation" || r.scenario == "context_revised" {
		if input.Analysis.Context == nil || input.Analysis.Context.Digest != s.ContextDigest || len(s.Tools) != 1 {
			return runtimeapi.Observation{}, fmt.Errorf("missing frozen context/tools")
		}
		found := false
		for _, source := range input.Analysis.Context.Inputs {
			if source.Content == "New guidance must not enter the active run." {
				return runtimeapi.Observation{}, fmt.Errorf("new source revision leaked into active work")
			}
			if source.Content == "Keep the frozen diagnostic report concise." {
				found = true
			}
		}
		if !found {
			return runtimeapi.Observation{}, fmt.Errorf("selected guidance not delivered")
		}
		key := []byte(strings.Repeat("k", 32))
		gateway := workerapi.New(workerapi.Config{SigningKey: key, ContextCheck: r.contextCheck}, r.journal, r.artifacts, r.provider, nil)
		uri := ""
		for _, source := range input.Context.Inputs {
			if source.Content == "Keep the frozen diagnostic report concise." {
				uri = source.URI
				break
			}
		}
		args, _ := json.Marshal(map[string]string{"uri": uri})
		request := httptest.NewRequest("POST", "/workers/"+s.AttemptID+"/tools/read-context-source", strings.NewReader(string(args)))
		request.Header.Set("Authorization", "Bearer "+kubernetes.Token(key, s))
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, request)
		if response.Code != 200 || !strings.Contains(response.Body.String(), "Keep the frozen diagnostic report concise.") {
			return runtimeapi.Observation{}, fmt.Errorf("real Deck authority/context tool: %d %s", response.Code, response.Body.String())
		}

	}
	report := analysis.Report{Summary: "Synthetic analysis: two blockers", Findings: analysis.DiagnosticFixture().Expected}
	if r.scenario == "missing_finding" {
		report.Findings = report.Findings[:1]
	}
	result := worker.Result{Stage: "analyze", ProfileDigest: s.ProfileDigest, InputDigest: s.InputDigest, Analysis: &report, ObservedAt: time.Now().UTC()}
	if r.scenario == "mismatched_digest" {
		result.InputDigest = strings.Repeat("c", 64)
	}
	b, _ := json.Marshal(result)
	digest, err := r.artifacts.Put(b)
	if err != nil {
		return runtimeapi.Observation{}, err
	}
	if r.scenario == "corrupt_result" {
		if err = os.WriteFile(filepath.Join(r.artifactRoot, strings.TrimPrefix(digest, "sha256:")), []byte("corrupted result"), 0600); err != nil {
			return runtimeapi.Observation{}, err
		}
	}
	if r.scenario != "missing_result" {
		_, _, err = r.journal.RecordRuntimeArtifact(ctx, runtimeapi.Artifact{ID: "result:" + s.AttemptID, ExecutionID: s.ExecutionID, RuntimeAttemptID: s.AttemptID, Name: "analysis.json", Kind: "worker_result", URI: "https://marina.internal/artifacts/" + digest, Digest: digest, SizeBytes: int64(len(b)), CreatedAt: result.ObservedAt})
		if err != nil {
			return runtimeapi.Observation{}, err
		}
	}
	r.starts++
	observation := runtimeapi.Observation{Found: true, ProviderHandle: "job/" + s.AttemptID, State: runtimeapi.StateSucceeded}
	r.observations[s.IdempotencyKey] = observation
	return observation, nil
}
