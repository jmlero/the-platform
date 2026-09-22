package deck

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol/github"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/worker"
)

// Both profiles cross real Deck HTTP and reopen the journal after each boundary.
// Runtime and Git are deterministic doubles; live Jobs are a separate gate.
func TestRealAgentFailureSemantics(t *testing.T) {
	if os.Getenv("DECK_CONFORMANCE_URL") == "" {
		t.Skip("run make conformance")
	}
	for _, workload := range []string{"repository", "analysis"} {
		for _, scenario := range []string{"lost_invocation", "lost_start", "lost_complete", "deck_outage", "active_cancel", "active_profile_change"} {
			t.Run(workload+"/"+scenario, func(t *testing.T) {
				ctx := context.Background()
				transport := &agentFailureTransport{loseCompletion: scenario == "lost_complete", loseInvocation: scenario == "lost_invocation"}
				client, err := New(os.Getenv("DECK_CONFORMANCE_URL"), &http.Client{Transport: transport, Timeout: 3 * time.Second}, WithBearerTokenFile(os.Getenv("DECK_CONFORMANCE_TOKEN_FILE")))
				if err != nil {
					t.Fatal(err)
				}
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
				_, err = client.RegisterAgent(ctx, workcontrol.AgentRegistration{ID: identity.LogicalAgentID, InstallationID: identity.InstallationID, Name: "Shared failure proof", Kind: "controller", Capabilities: workcontrol.WorkerCapabilities()})
				if err != nil {
					t.Fatal(err)
				}
				config := flow.Config{ProjectID: "prj_default", PublicURL: "https://marina.internal", PolicyVersion: "test", MaxTokens: 100000, BudgetMinutes: 5,
					AllowedPaths: []string{"src"}, Checks: []worker.Check{{ID: "regression", Directory: ".", Command: []string{"go", "test", "./..."}}}, RequiredCI: []string{"required-test"},
					Git: github.Config{Repository: "acme/repo", BaseBranch: "main", CloneURL: "https://git.internal/acme/repo.git"}}
				config.Kubernetes.Image = "worker@sha256:" + strings.Repeat("a", 64)
				if workload == "analysis" {
					config.Git = github.Config{}
					config.Analysis = &analysis.Profile{Version: 1, Revision: 1, Image: "analysis@sha256:" + strings.Repeat("b", 64), HarnessVersion: analysis.HarnessWithContext, Tools: []string{"read_context_source"}, MaxTokens: 20000, BudgetMinutes: 5, Fixture: analysis.DiagnosticFixture()}
				}
				binding, err := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "prj_default", Provider: "deck", Runtime: "test", PolicyVersion: "test", ContextConfig: config.Digest(), ArtifactConfig: "test", BudgetMinutes: 5, MaxConcurrency: 1})
				if err != nil {
					t.Fatal(err)
				}
				profiles := config.WorkloadProfiles()
				if len(profiles) != 1 {
					t.Fatalf("profiles: %+v", profiles)
				}
				profile := profiles[0]
				if err = client.PublishWorkloads(ctx, "prj_default", workcontrol.WorkloadCatalog{Profiles: profiles, ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}); err != nil {
					t.Fatal(err)
				}
				input := map[string]any{"profile_id": profile.ID, "profile_digest": profile.Digest, "task": map[string]any{"title": "Shared failure " + workload + " " + scenario, "acceptance_criteria": "Bounded outcome with independent evidence", "budget": map[string]any{"time_minutes": 5, "max_attempts": 2, "max_tokens": 20000, "max_concurrency": 1, "authority": profile.Authority}}}
				var task, replay taskDTO
				key := "shared:" + workload + ":" + scenario
				endpoint := "/api/v1/operator/projects/prj_default/workloads/invoke"
				if err = client.doIdempotent(ctx, "POST", endpoint, key, input, &task); err != nil && !(scenario == "lost_invocation" && errors.Is(err, context.DeadlineExceeded)) {
					t.Fatal(err)
				}
				if err = client.doIdempotent(ctx, "POST", endpoint, key, input, &replay); err != nil || (task.ID != "" && task.ID != replay.ID) {
					t.Fatalf("duplicate invocation: %+v %v", replay, err)
				}
				task = replay
				store, err := artifacts.New(filepath.Join(dir, "artifacts"))
				if err != nil {
					t.Fatal(err)
				}
				coding := &workflowRuntime{journal: journal, artifacts: store, observations: map[string]runtimeapi.Observation{}}
				analyzing := &analysisRuntime{journal: journal, artifacts: store, observations: map[string]runtimeapi.Observation{}, provider: client}
				var adapter runtimeapi.Adapter = coding
				if workload == "analysis" {
					adapter = analyzing
				}
				runtime := &agentFailureRuntime{Adapter: adapter, scenario: scenario, observations: map[string]runtimeapi.Observation{}}
				source := &workflowSource{head: strings.Repeat("a", 40)}
				controller := &flow.Controller{Config: config, Journal: journal, Provider: client, Runtime: runtime, Artifacts: store, Source: func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }}
				var e execution.Execution
				injected := false
				for cycle := 0; cycle < 50; cycle++ {
					control, err := reconcile.NewControlController(client, journal, binding, identity, 2*time.Minute)
					if err != nil {
						t.Fatal(err)
					}
					result := control.WithIntakeEnabled(cycle == 0).WithIntakeWorkID(task.ID).WithExecutionLimits(2, 100000).WithCommandHandler(controller.Command).Cycle(ctx)
					for _, err := range result.Errors {
						var conflict interface{ MutationConflict() bool }
						staleAfterCompletion := transport.lost && errors.As(err, &conflict) && conflict.MutationConflict()
						if !strings.Contains(err.Error(), "waiting for worker termination") && !staleAfterCompletion {
							t.Fatal(err)
						}
					}
					// Production runs lease reconciliation after commands/outbox and
					// before workflow advancement. It observes committed completion.
					if e.ID != "" {
						lease, err := reconcile.NewLeaseReconciler(client, journal, 2*time.Minute)
						if err != nil {
							t.Fatal(err)
						}
						if _, err = lease.Reconcile(ctx, e.ID, time.Second); err != nil {
							t.Fatal(err)
						}
					}
					for _, err := range controller.Cycle(ctx) {
						if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "waiting for worker termination") {
							t.Fatal(err)
						}
					}
					if e.ID == "" {
						active, err := journal.ListActiveExecutions(ctx, 10)
						if err != nil || len(active) != 1 {
							t.Fatalf("active: %+v %v", active, err)
						}
						e = active[0]
					}
					if runtime.starts > 0 && !injected {
						injected = true
						switch scenario {
						case "active_cancel":
							if err = client.do(ctx, "POST", "/api/v1/operator/runs/"+e.ExternalRunID+"/commands", map[string]any{"type": "cancel", "reason": "Shared active cancellation proof"}, nil); err != nil {
								t.Fatal(err)
							}
						case "active_profile_change":
							controller.Config.PolicyVersion = "changed"
						case "deck_outage":
							transport.outage = true
							starts := runtime.starts
							if failures := controller.Cycle(ctx); len(failures) == 0 {
								t.Fatal("provider outage was ignored")
							}
							if runtime.starts != starts || source.commits != 0 {
								t.Fatal("provider outage allowed side effects")
							}
							transport.outage = false
						}
					}
					if runtime.cancels == 1 {
						var detail struct {
							Commands []struct {
								Status string `json:"status"`
							} `json:"commands"`
						}
						err := client.do(ctx, "GET", "/api/v1/operator/tasks/"+task.ID, nil, &detail)
						if err != nil {
							t.Fatal(err)
						}
						for _, command := range detail.Commands {
							if command.Status == "completed" {
								t.Fatal("cancellation acknowledged before observed termination")
							}
						}
					}
					if err = journal.Close(); err != nil {
						t.Fatal(err)
					}
					journal, err = sqlite.Open(ctx, db)
					if err != nil {
						t.Fatal(err)
					}
					controller.Journal, coding.journal, analyzing.journal = journal, journal, journal
					e, err = journal.GetExecution(ctx, e.ID)
					if err != nil {
						t.Fatal(err)
					}
					if e.State.Terminal() {
						break
					}
				}
				want, starts := execution.StateCompleted, 1
				if workload == "repository" {
					starts = 2
				}
				if scenario == "active_cancel" {
					want, starts = execution.StateCancelled, 1
				}
				if scenario == "active_profile_change" {
					want, starts = execution.StateBlocked, 1
				}
				if e.State != want || runtime.starts != starts {
					t.Fatalf("state %s want %s, starts %d want %d", e.State, want, runtime.starts, starts)
				}
				if strings.HasPrefix(scenario, "active_") && runtime.cancels < 2 {
					t.Fatal("worker termination was not observed")
				}
				if (scenario == "lost_complete" || scenario == "lost_invocation") && !transport.lost {
					t.Fatal("completion response not dropped")
				}
				if want == execution.StateCompleted && workload == "repository" && (source.commits != 1 || source.prCreates != 1) {
					t.Fatal("duplicate Git effects")
				}
				if failures := controller.Cycle(ctx); len(failures) != 0 {
					t.Fatal(failures)
				}
				attempts, err := journal.ListRuntimeAttempts(ctx, e.ID)
				if err != nil {
					t.Fatal(err)
				}
				for _, attempt := range attempts {
					if attempt.CleanupState != runtimeapi.CleanupCompleted {
						t.Fatal("runtime cleanup incomplete")
					}
				}
				detail, err := client.GetWork(ctx, task.ID)
				if err != nil || len(detail.Runs) != 1 {
					t.Fatalf("duplicate provider run: %+v %v", detail.Runs, err)
				}
			})
		}
	}
}

type agentFailureTransport struct{ loseCompletion, loseInvocation, lost, outage bool }

func (t *agentFailureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if t.outage {
		return nil, context.DeadlineExceeded
	}
	response, err := http.DefaultTransport.RoundTrip(request)
	drop := (t.loseCompletion && strings.HasSuffix(request.URL.Path, "/complete")) || (t.loseInvocation && strings.HasSuffix(request.URL.Path, "/workloads/invoke"))
	if err == nil && drop && !t.lost && response.StatusCode < 300 {
		t.lost = true
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		return nil, context.DeadlineExceeded
	}
	return response, err
}

type agentFailureRuntime struct {
	runtimeapi.Adapter
	scenario        string
	starts, cancels int
	observations    map[string]runtimeapi.Observation
}

func (r *agentFailureRuntime) Start(ctx context.Context, spec runtimeapi.Specification) (runtimeapi.Observation, error) {
	observation, err := r.Adapter.Start(ctx, spec)
	if err != nil {
		return observation, err
	}
	r.starts++
	if strings.HasPrefix(r.scenario, "active_") {
		observation.State = runtimeapi.StateRunning
	}
	r.observations[spec.IdempotencyKey] = observation
	if r.scenario == "lost_start" {
		return runtimeapi.Observation{}, context.DeadlineExceeded
	}
	return observation, nil
}

func (r *agentFailureRuntime) InspectByKey(_ context.Context, key string) (runtimeapi.Observation, error) {
	return r.observations[key], nil
}
func (r *agentFailureRuntime) Inspect(_ context.Context, handle string) (runtimeapi.Observation, error) {
	for _, value := range r.observations {
		if value.ProviderHandle == handle {
			return value, nil
		}
	}
	return runtimeapi.Observation{}, nil
}
func (r *agentFailureRuntime) Cancel(_ context.Context, handle string, _ bool) (runtimeapi.Observation, error) {
	r.cancels++
	for key, value := range r.observations {
		if value.ProviderHandle == handle {
			if r.cancels > 1 {
				value.State = runtimeapi.StateCancelled
			}
			r.observations[key] = value
			return value, nil
		}
	}
	return runtimeapi.Observation{}, errors.New("unknown worker")
}
