package deck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// The real Deck process owns a different database and is accessed only over
// HTTP. Deterministic source/runtime doubles exercise controller recovery without
// publishing customer code or spending money on a model.
func TestRealRepositoryWorkflow(t *testing.T) {
	baseURL := os.Getenv("DECK_CONFORMANCE_URL")
	if baseURL == "" {
		t.Skip("run make conformance for the real-process workflow")
	}
	client, err := New(baseURL, &http.Client{Timeout: 3 * time.Second}, WithBearerTokenFile(os.Getenv("DECK_CONFORMANCE_TOKEN_FILE")))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err = waitForProvider(ctx, client); err != nil {
		t.Fatal(err)
	}
	var approved workcontrol.ContextSource
	for _, scenario := range []string{"completed", "profile_completed", "promoted_completed", "verification_failed", "head_changed", "pr_closed", "ci_missing", "missing_authority", "context_first", "context_reuse", "context_revised", "context_revoked"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			db := filepath.Join(directory, "journal.db")
			journal, err := sqlite.Open(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { journal.Close() }()
			identity, err := journal.EnsureInstallationIdentity(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.RegisterAgent(ctx, workcontrol.AgentRegistration{ID: identity.LogicalAgentID, InstallationID: identity.InstallationID, Name: "Repository workflow proof", Kind: "controller", Capabilities: workcontrol.WorkerCapabilities()})
			if err != nil {
				t.Fatal(err)
			}
			binding, err := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "prj_default", Provider: "deck", Repository: "https://git.internal/acme/repo.git", Runtime: "test", PolicyVersion: "test", ContextConfig: "test", ArtifactConfig: "test", DeliveryConfig: "test", BudgetMinutes: 30, MaxConcurrency: 1})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "context_first" {
				approved = registerApprovedContext(t, ctx, client, identity.LogicalAgentID, workcontrol.ContextSource{}, "Keep the API stable")
			}
			config := flow.Config{ProjectID: "prj_default", PublicURL: "https://marina.internal", PolicyVersion: "test", MaxTokens: 100000, BudgetMinutes: 30, AllowedPaths: []string{"src"}, Checks: []worker.Check{{ID: "regression", Directory: ".", Command: []string{"go", "test", "./..."}}}, RequiredCI: []string{"required-test"}, Git: github.Config{Repository: "acme/repo", BaseBranch: "main", CloneURL: "https://git.internal/acme/repo.git"}}
			config.Kubernetes.Image = "worker@sha256:" + strings.Repeat("d", 64)
			var taskResponse taskDTO
			authority := []string{"source.read", "source.write", "pull_request.create"}
			if scenario == "missing_authority" {
				authority = []string{} // Generic/legacy work still exercises Marina's own authority check.
			}
			input := map[string]any{"project_id": "prj_default", "title": "Fix bounded fixture " + scenario, "description": "Keep the public interface unchanged", "acceptance_criteria": "Configured regression check passes", "status": "ready", "priority": "medium", "budget_minutes": 30, "budget": map[string]any{"time_minutes": 30, "max_attempts": 2, "max_tokens": 100000, "max_cost_minor": 0, "currency": "EUR", "max_concurrency": 1, "authority": authority}}
			if scenario == "promoted_completed" {
				var channel struct {
					ID string `json:"id"`
				}
				var message struct {
					ID string `json:"id"`
				}
				if err = client.do(ctx, "POST", "/api/v1/operator/channels", map[string]any{"project_id": "prj_default", "name": "workflow-promotion"}, &channel); err != nil {
					t.Fatal(err)
				}
				if err = client.do(ctx, "POST", "/api/v1/operator/channels/"+channel.ID+"/messages", map[string]any{"body": "Fix the bounded fixture"}, &message); err != nil {
					t.Fatal(err)
				}
				input["status"] = "backlog"
				if err = client.do(ctx, "POST", "/api/v1/operator/messages/"+message.ID+"/promote", input, &taskResponse); err != nil {
					t.Fatal(err)
				}
				err = client.do(ctx, "POST", "/api/v1/tasks/"+taskResponse.ID+"/move", map[string]string{"status": "ready"}, &taskResponse)
			} else if scenario == "profile_completed" {
				if err = client.PublishWorkloads(ctx, "prj_default", workcontrol.WorkloadCatalog{Profiles: config.WorkloadProfiles(), ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}); err != nil {
					t.Fatal(err)
				}
				invoke := map[string]any{"profile_id": "repository/v1", "profile_digest": config.Digest(), "task": input}
				err = client.doIdempotent(ctx, "POST", "/api/v1/operator/projects/prj_default/workloads/invoke", "repository-invocation", invoke, &taskResponse)
			} else {
				err = client.do(ctx, "POST", "/api/v1/tasks", input, &taskResponse)
			}
			if err != nil {
				t.Fatal(err)
			}
			task := taskResponse.toDomain()
			if strings.HasPrefix(scenario, "context_") {
				if task.Context == nil || len(task.Context.Sources) != 1 {
					t.Fatalf("project decision not selected: %#v", task.Context)
				}
				if scenario == "context_reuse" {
					approved = registerApprovedContext(t, ctx, client, identity.LogicalAgentID, approved, "Document all API additions")
				}
			}
			snapshot, _ := json.Marshal(task)
			criteria := sha256.Sum256([]byte(task.AcceptanceCriteria))
			intent, _, err := journal.EnsureAcquisitionIntent(ctx, execution.NewAcquisitionIntent{ProjectID: task.ProjectID, WorkID: task.ID, TaskVersion: task.Version, TaskDigest: artifacts.Digest(snapshot), TaskSnapshot: string(snapshot), AcceptanceCriteriaDigest: hex.EncodeToString(criteria[:]), ClaimKey: "workflow:" + task.ID, AgentID: identity.LogicalAgentID, LeaseSeconds: 120, BudgetMinutes: 30})
			if err != nil {
				t.Fatal(err)
			}
			run, err := client.ClaimWork(ctx, task.ID, workcontrol.Claim{IdempotencyKey: "workflow:" + task.ID, AgentID: identity.LogicalAgentID, ExpectedTaskVersion: task.Version, Lease: 2 * time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			e, _, err := journal.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ProjectID: task.ProjectID, ExternalWorkID: task.ID, ExternalRunID: run.ID, AgentID: run.AgentID, ExternalRunVersion: run.Version, FenceToken: run.FenceToken, TaskVersion: task.Version, TaskDigest: artifacts.Digest(snapshot), BindingVersion: binding.Version, LeaseExpiresAt: *run.LeaseExpiresAt, Budget: execution.Budget{DeadlineAt: time.Now().Add(30 * time.Minute), MaxAttempts: 2, MaxTokens: 100000}})
			if err != nil {
				t.Fatal(err)
			}
			store, err := artifacts.New(filepath.Join(directory, "artifacts"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = journal.RecordAcquisitionClaim(ctx, intent.ID, run.Version, run.FenceToken, run.ID, *run.LeaseExpiresAt); err != nil {
				t.Fatal(err)
			}
			if err = journal.MarkAcquisitionBound(ctx, intent.ID, run.ID); err != nil {
				t.Fatal(err)
			}
			source := &workflowSource{head: strings.Repeat("a", 40), scenario: scenario}
			runtime := &workflowRuntime{journal: journal, artifacts: store, scenario: scenario, observations: map[string]runtimeapi.Observation{}}
			controller := &flow.Controller{Config: config, Journal: journal, Provider: client, Runtime: runtime, Artifacts: store, Source: func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }}
			for cycle := 0; cycle < 26; cycle++ {
				control, err := reconcile.NewControlController(client, journal, binding, identity, 2*time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				if result := control.WithIntakeEnabled(false).Cycle(ctx); len(result.Errors) > 0 {
					t.Fatalf("control cycle %d: %v", cycle, result.Errors)
				}
				if scenario == "context_revoked" && cycle == 3 {
					if err = client.do(ctx, "POST", fmt.Sprintf("/api/v1/operator/projects/prj_default/context/sources/%s/%d/revoke", approved.ID, approved.Revision), nil, nil); err != nil {
						t.Fatal(err)
					}
				}
				failures := controller.Cycle(ctx)
				for _, err := range failures {
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("cycle %d: %v", cycle, err)
					}
				}
				// Restart at every boundary, including after a simulated lost commit response.
				if err = journal.Close(); err != nil {
					t.Fatal(err)
				}
				journal, err = sqlite.Open(ctx, db)
				if err != nil {
					t.Fatal(err)
				}
				controller.Journal = journal
				runtime.journal = journal
				e, err = journal.GetExecution(ctx, e.ID)
				if err != nil {
					t.Fatal(err)
				}
				if e.State.Terminal() {
					break
				}
			}
			want := execution.StateCompleted
			if scenario == "ci_missing" {
				want = execution.StateRunning
			} else if scenario != "completed" && scenario != "profile_completed" && scenario != "promoted_completed" && !strings.HasPrefix(scenario, "context_") || scenario == "context_revoked" {
				want = execution.StateBlocked
			}
			if e.State != want {
				t.Fatalf("state=%s want=%s failure=%s", e.State, want, e.FailureMessage)
			}
			detail, err := client.GetWork(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "completed" || scenario == "profile_completed" || scenario == "promoted_completed" || (strings.HasPrefix(scenario, "context_") && scenario != "context_revoked") {
				if detail.Runs[0].Status != workcontrol.RunCompleted || !strings.Contains(detail.Runs[0].ResultSummary, "https://git.internal/pr/1") {
					t.Fatalf("result not projected: %#v", detail.Runs)
				}
				if runtime.starts != 2 || source.prCreates != 1 || source.commits != 1 {
					t.Fatalf("duplicate effects: starts=%d PRs=%d commits=%d", runtime.starts, source.prCreates, source.commits)
				}
				evidence, err := journal.ListEvidence(ctx, e.ID)
				if err != nil || len(evidence) != 1 || evidence[0].SourceRevision != strings.Repeat("b", 40) {
					t.Fatalf("wrong evidence: %#v %v", evidence, err)
				}
			}
			if strings.HasPrefix(scenario, "context_") {
				manifest, err := journal.ExecutionContextPackage(ctx, e.ID)
				if err != nil {
					t.Fatal(err)
				}
				expected := "Keep the API stable"
				if scenario == "context_revised" || scenario == "context_revoked" {
					expected = "Document all API additions"
				}
				found := false
				for _, item := range manifest.Inputs {
					if strings.Contains(item.Content, expected) && item.Owner == "deck" && item.Trust == "trusted" {
						found = true
					}
				}
				if !found {
					t.Fatalf("saved decision not reused: %s", expected)
				}
				if scenario == "context_revoked" && runtime.starts != 0 {
					t.Fatal("revoked context reached worker")
				}
			}
			if scenario == "missing_authority" && (runtime.starts != 0 || source.commits != 0) {
				t.Fatal("unauthorized task executed")
			}
		})
	}
	t.Run("repository_association", func(t *testing.T) { verifyRealRepositoryAssociation(t, ctx, client) })
}

type workflowRuntime struct {
	runtimeapi.Adapter
	journal      *sqlite.Store
	artifacts    *artifacts.Store
	scenario     string
	starts       int
	observations map[string]runtimeapi.Observation
}

func (r *workflowRuntime) InspectByKey(_ context.Context, key string) (runtimeapi.Observation, error) {
	return r.observations[key], nil
}
func (r *workflowRuntime) Start(ctx context.Context, spec runtimeapi.Specification) (runtimeapi.Observation, error) {
	checkpoint, err := r.journal.GetCheckpoint(ctx, spec.ExecutionID, "worker-input:"+spec.AttemptID)
	if err != nil {
		return runtimeapi.Observation{}, err
	}
	var input worker.Input
	if err = json.Unmarshal([]byte(checkpoint.Payload["input"].(string)), &input); err != nil {
		return runtimeapi.Observation{}, err
	}
	if input.Context == nil || input.Context.Digest != spec.ContextDigest {
		return runtimeapi.Observation{}, errors.New("worker did not receive the persisted context package")
	}
	if err = input.Context.ValidateAt(time.Now()); err != nil {
		return runtimeapi.Observation{}, err
	}
	r.starts++
	stage := "implement"
	if strings.HasSuffix(spec.IdempotencyKey, ":verify") {
		stage = "verify"
	}
	result := worker.Result{Stage: stage, SourceRevision: spec.SourceRevision, ObservedAt: time.Now().UTC(), Summary: "Fixed fixture"}
	if stage == "implement" {
		result.Changes = []worker.Change{{Path: "src/fix.go", Content: []byte("package fixture\n"), Mode: "100644"}}
	} else {
		exit := 0
		if r.scenario == "verification_failed" {
			exit = 1
		}
		result.Checks = []worker.CheckResult{{ID: "regression", Directory: ".", Command: []string{"go", "test", "./..."}, ExitCode: exit, Output: "test output", OutputDigest: artifacts.Digest([]byte("test output"))}}
	}
	b, _ := json.Marshal(result)
	digest, err := r.artifacts.Put(b)
	if err != nil {
		return runtimeapi.Observation{}, err
	}
	_, _, err = r.journal.RecordRuntimeArtifact(ctx, runtimeapi.Artifact{ID: "result:" + spec.AttemptID, ExecutionID: spec.ExecutionID, RuntimeAttemptID: spec.AttemptID, Name: stage + ".json", Kind: "worker_result", URI: "https://marina.internal/artifacts/" + digest, Digest: digest, SizeBytes: int64(len(b)), CreatedAt: result.ObservedAt})
	if err != nil {
		return runtimeapi.Observation{}, err
	}
	observation := runtimeapi.Observation{Found: true, ProviderHandle: "job/" + spec.AttemptID, State: runtimeapi.StateSucceeded}
	r.observations[spec.IdempotencyKey] = observation
	return observation, nil
}
func (r *workflowRuntime) Cleanup(context.Context, string) error { return nil }

type workflowSource struct {
	sourcecontrol.Port
	head, scenario     string
	commits, prCreates int
	lost               bool
}

func (s *workflowSource) CurrentHead(_ context.Context, r sourcecontrol.HeadRequest) (sourcecontrol.Revision, error) {
	if r.Ref == "main" {
		return sourcecontrol.Revision{Commit: strings.Repeat("a", 40)}, nil
	}
	if s.scenario == "head_changed" && s.prCreates > 0 {
		return sourcecontrol.Revision{Commit: strings.Repeat("c", 40)}, nil
	}
	return sourcecontrol.Revision{Commit: s.head}, nil
}
func (s *workflowSource) CreateBranch(_ context.Context, r sourcecontrol.BranchRequest) (sourcecontrol.Revision, error) {
	if s.head != r.BaseRevision {
		return sourcecontrol.Revision{}, errors.New("branch already advanced")
	}
	return sourcecontrol.Revision{Commit: s.head}, nil
}
func (s *workflowSource) WriteChanges(context.Context, sourcecontrol.WriteRequest) (sourcecontrol.Revision, error) {
	if s.commits == 0 {
		s.commits++
		s.head = strings.Repeat("b", 40)
		return sourcecontrol.Revision{}, context.DeadlineExceeded
	}
	return sourcecontrol.Revision{Commit: s.head}, nil
}
func (s *workflowSource) CreatePullRequest(context.Context, sourcecontrol.PullRequestRequest) (sourcecontrol.PullRequest, error) {
	if s.prCreates == 0 {
		s.prCreates++
	}
	return sourcecontrol.PullRequest{ID: "1", URL: "https://git.internal/pr/1", HeadRevision: s.head, State: "open"}, nil
}

func (s *workflowSource) GetPullRequest(context.Context, sourcecontrol.ObservationRequest) (sourcecontrol.PullRequest, error) {
	state := "open"
	if s.scenario == "pr_closed" {
		state = "closed"
	}
	return sourcecontrol.PullRequest{ID: "1", State: state, HeadRevision: s.head, URL: "https://git.internal/pr/1"}, nil
}
func (s *workflowSource) ObserveChecks(context.Context, sourcecontrol.ObservationRequest) ([]sourcecontrol.CheckObservation, error) {
	if s.scenario == "ci_missing" {
		return nil, nil
	}
	return []sourcecontrol.CheckObservation{{ID: "required-test", HeadRevision: s.head, State: "success", ObservedAt: time.Now()}}, nil
}
func (s *workflowSource) ObserveReviews(context.Context, sourcecontrol.ObservationRequest) ([]sourcecontrol.ReviewObservation, error) {
	return nil, nil
}

func registerApprovedContext(t *testing.T, ctx context.Context, client *Client, agent string, previous workcontrol.ContextSource, planText string) workcontrol.ContextSource {
	t.Helper()
	var task taskDTO
	if err := client.do(ctx, "POST", "/api/v1/tasks", map[string]any{"title": "Approve reusable project decision", "acceptance_criteria": "Project decision is approved", "status": "ready", "budget_minutes": 30}, &task); err != nil {
		t.Fatal(err)
	}
	run, err := client.ClaimWork(ctx, task.ID, workcontrol.Claim{IdempotencyKey: "context-decision:" + task.ID, AgentID: agent, ExpectedTaskVersion: task.Version, Lease: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.SubmitPlanRevision(ctx, run.ID, workcontrol.PlanRevisionSubmission{IdempotencyKey: "context-plan:" + task.ID, AgentID: agent, ExpectedVersion: run.Version, FenceToken: run.FenceToken, Plan: planText, ContextDigest: artifacts.Digest([]byte(planText)), RiskClass: "low", VerificationStrategy: "Check public compatibility", RollbackApproach: "Revert the change", PolicyVersion: "context-proof", RequireApproval: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision == nil {
		t.Fatal("approval decision missing")
	}
	if err = client.do(ctx, "POST", "/api/v1/operator/decisions/"+result.Decision.ID+"/answer", map[string]string{"answer": "Approve plan"}, nil); err != nil {
		t.Fatal(err)
	}
	input := workcontrol.ContextSourceInput{ID: "reusable-decision", ExpectedRevision: previous.Revision, Kind: "decision", Title: "API decision", Reference: result.Decision.ID, SourceRevision: result.PlanRevision.ID, Trust: "guidance", State: "active"}
	var source workcontrol.ContextSource
	if err = client.do(ctx, "PUT", "/api/v1/operator/projects/prj_default/context/sources", input, &source); err != nil {
		t.Fatal(err)
	}
	detail, err := client.GetWork(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	latest := detail.Runs[0]
	if err = client.BlockRun(ctx, run.ID, workcontrol.RunBlock{IdempotencyKey: "context-proof-done:" + task.ID, AgentID: agent, ExpectedVersion: latest.Version, FenceToken: latest.FenceToken, Reason: "Decision recorded for context reuse proof; no repository change requested"}); err != nil {
		t.Fatal(err)
	}
	return source
}

func verifyRealRepositoryAssociation(t *testing.T, ctx context.Context, client *Client) {
	t.Helper()
	gitServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"object":{"sha":"%s"}}`, strings.Repeat("a", 40))
	}))
	defer gitServer.Close()
	path := filepath.Join(t.TempDir(), "repository.db")
	journal, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { journal.Close() }()
	config := flow.Config{ProjectID: "prj_default", Git: github.Config{APIURL: gitServer.URL, CloneURL: gitServer.URL + "/owner/first.git", Repository: "owner/first", BaseBranch: "main"}}
	bindingInput := execution.ProjectBinding{ProjectID: config.ProjectID, Provider: "deck", Repository: config.Git.CloneURL, Runtime: "test", PolicyVersion: "test", ContextConfig: config.Digest(), ArtifactConfig: "test", DeliveryConfig: "test", BudgetMinutes: 30, MaxConcurrency: 1}
	if _, err = flow.InitializeRepository(ctx, journal, config, bindingInput); err != nil {
		t.Fatal(err)
	}
	controller := &flow.Controller{Config: config, Journal: journal, Provider: client}
	request := workcontrol.RepositoryRequestInput{Repository: "owner/first", BaseBranch: "main"}
	var submitted workcontrol.RepositoryRequest
	if err = client.do(ctx, "PUT", "/api/v1/operator/projects/prj_default/repository", request, &submitted); err != nil {
		t.Fatal(err)
	}
	if err = controller.ReconcileRepository(ctx); err != nil {
		t.Fatal(err)
	}
	confirmed, err := client.GetRepository(ctx, "prj_default")
	if err != nil || confirmed.Status != "connected" || confirmed.Binding == nil {
		t.Fatal("repository not confirmed", err)
	}
	var task taskDTO
	if err = client.do(ctx, "POST", "/api/v1/tasks", map[string]any{"title": "Inspect original binding", "acceptance_criteria": "The selected binding is visible", "status": "ready", "budget_minutes": 30}, &task); err != nil {
		t.Fatal(err)
	}
	original := task.toDomain()
	if original.Context == nil || original.Context.Repository == nil {
		t.Fatal("authorized task has no binding")
	}
	encoded, _ := json.Marshal(original)
	if _, _, err = journal.EnsureAcquisitionIntent(ctx, execution.NewAcquisitionIntent{ProjectID: config.ProjectID, WorkID: task.ID, TaskVersion: task.Version, TaskDigest: artifacts.Digest(encoded), TaskSnapshot: string(encoded), AcceptanceCriteriaDigest: "proof", ClaimKey: "association-proof", AgentID: "proof", LeaseSeconds: 60, BudgetMinutes: 30}); err != nil {
		t.Fatal(err)
	}
	request = workcontrol.RepositoryRequestInput{ExpectedRevision: confirmed.Revision, ExpectedBindingVersion: confirmed.Binding.Version, Repository: "owner/second", BaseBranch: "main"}
	if err = client.do(ctx, "PUT", "/api/v1/operator/projects/prj_default/repository", request, &submitted); err != nil {
		t.Fatal(err)
	}
	if err = controller.ReconcileRepository(ctx); err != nil {
		t.Fatal(err)
	}
	journal.Close()
	journal, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	controller.Journal = journal
	latest, err := flow.InitializeRepository(ctx, journal, config, bindingInput)
	if err != nil || latest.Version <= original.Context.Repository.Version {
		t.Fatal("association did not survive restart", err)
	}
	recovered, err := controller.ConfigurationForExecution(ctx, execution.Execution{ProjectID: config.ProjectID, ExternalWorkID: task.ID, TaskVersion: task.Version, BindingVersion: original.Context.Repository.Version})
	if err != nil || recovered.Git.Repository != "owner/first" {
		t.Fatal("authorized work switched repositories", err)
	}
	detail, err := client.GetWork(ctx, task.ID)
	if err != nil || *detail.Work.Context.Repository != *original.Context.Repository {
		t.Fatal("Deck replaced frozen binding", err)
	}
}
