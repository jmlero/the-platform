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
	"github.com/jmlero/the-platform/marina/internal/workerapi"
	"github.com/jmlero/the-platform/marina/internal/workresult"
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err = waitForProvider(ctx, client); err != nil {
		t.Fatal(err)
	}
	previous, err := client.GetRepository(ctx, "prj_default")
	if err != nil {
		t.Fatal(err)
	}
	version := int64(0)
	if previous.Binding != nil {
		version = previous.Binding.Version
	}
	var repository workcontrol.RepositoryRequest
	if err = client.do(ctx, "PUT", "/api/v1/operator/projects/prj_default/repository", workcontrol.RepositoryRequestInput{ExpectedRevision: previous.Revision, ExpectedBindingVersion: version, Repository: "acme/repo", BaseBranch: "main"}, &repository); err != nil {
		t.Fatal(err)
	}
	if err = client.RecordRepositoryResult(ctx, "prj_default", workcontrol.RepositoryResult{RequestRevision: repository.Revision, Status: "connected", Binding: &workcontrol.RepositoryBinding{Repository: "acme/repo", BaseBranch: "main", CloneURL: "https://git.internal/acme/repo.git", Version: max(1, version)}}); err != nil {
		t.Fatal(err)
	}
	var approved workcontrol.ContextSource
	for _, scenario := range []string{"completed", "profile_completed", "promoted_completed", "verification_failed", "head_changed", "pr_closed", "ci_missing", "missing_authority", "context_first", "context_reuse", "context_revised", "context_revoked", "landing_completed", "landing_profile_completed", "landing_prd", "landing_prd_changed", "landing_moved_base", "landing_rebase", "landing_rebase_conflict", "landing_ci_failed", "landing_ci_missing", "landing_verification_failed", "repair_verification", "repair_ci", "repair_exhausted", "repair_pr_ci", "repair_review", "repair_review_exhausted", "review_invalid"} {
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
			config.Model = workerapi.Model{Endpoint: "https://model.internal/v1", Name: "producer", Currency: "EUR", MaxOutputTokens: 100}
			config.ReviewerModel = &workerapi.Model{Endpoint: config.Model.Endpoint, Name: "reviewer", Currency: "EUR", MaxOutputTokens: 100}
			repair := strings.HasPrefix(scenario, "repair_")
			attemptLimit := 3
			if repair {
				attemptLimit = flow.RepositoryRuntimeLimit
			}
			if strings.HasPrefix(scenario, "landing_rebase") {
				attemptLimit = 6
			}
			if strings.HasPrefix(scenario, "landing_") || (repair && scenario != "repair_pr_ci") {
				config.Delivery = "fast_forward"
			}
			if strings.HasPrefix(scenario, "landing_prd") {
				config.AllowedPaths = []string{"docs/prd"}
			}
			config.RepositoryEngine, config.RepositoryEngineGatewayURL = workflowEngineProfile(), "https://marina.marina-system.svc"
			config.Kubernetes.Image = "worker@sha256:" + strings.Repeat("d", 64)
			var taskResponse taskDTO
			authority := config.RepositoryAuthority()
			if scenario == "missing_authority" {
				authority = []string{} // Generic/legacy work still exercises Marina's own authority check.
			}
			input := map[string]any{"project_id": "prj_default", "title": "Fix bounded fixture " + scenario, "description": "Keep the public interface unchanged", "acceptance_criteria": "Configured regression check passes", "status": "ready", "priority": "medium", "budget_minutes": 30, "budget": map[string]any{"time_minutes": 30, "max_attempts": attemptLimit, "max_tokens": 100000, "max_cost_minor": 0, "currency": "EUR", "max_concurrency": 1, "authority": authority}}
			if strings.HasPrefix(scenario, "landing_prd") {
				taskResponse = approveWorkflowPRD(t, ctx, client, config, scenario, input["budget"])
			} else if scenario == "promoted_completed" {
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
			} else if scenario == "profile_completed" || scenario == "landing_profile_completed" {
				if err = client.PublishWorkloads(ctx, "prj_default", workcontrol.WorkloadCatalog{Profiles: config.WorkloadProfiles(), ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}); err != nil {
					t.Fatal(err)
				}
				invoke := map[string]any{"profile_id": config.RepositoryProfileID(), "profile_digest": config.Digest(), "task": input}
				err = client.doIdempotent(ctx, "POST", "/api/v1/operator/projects/prj_default/workloads/invoke", "repository-invocation-"+scenario, invoke, &taskResponse)
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
			e, _, err := journal.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ProjectID: task.ProjectID, ExternalWorkID: task.ID, ExternalRunID: run.ID, AgentID: run.AgentID, ExternalRunVersion: run.Version, FenceToken: run.FenceToken, TaskVersion: task.Version, TaskDigest: artifacts.Digest(snapshot), BindingVersion: binding.Version, LeaseExpiresAt: *run.LeaseExpiresAt, Budget: execution.Budget{DeadlineAt: time.Now().Add(30 * time.Minute), MaxAttempts: attemptLimit, MaxTokens: 100000}})
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
			source := &workflowSource{head: strings.Repeat("a", 40), main: strings.Repeat("a", 40), scenario: scenario}
			runtime := &workflowRuntime{journal: journal, artifacts: store, scenario: scenario, observations: map[string]runtimeapi.Observation{}}
			controller := &flow.Controller{Config: config, Journal: journal, Provider: client, Runtime: runtime, EngineRuntime: runtime, EngineTemplates: workflowTemplates{}, PrepareEngine: func(context.Context, runtimeapi.Specification) error { return nil }, Artifacts: store, Source: func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }}
			for cycle := 0; cycle < 96; cycle++ {
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
			if scenario == "ci_missing" || scenario == "landing_ci_missing" {
				want = execution.StateRunning
			} else if scenario != "completed" && scenario != "profile_completed" && scenario != "promoted_completed" && scenario != "landing_completed" && scenario != "landing_profile_completed" && scenario != "landing_prd" && scenario != "landing_rebase" && !strings.HasPrefix(scenario, "context_") || scenario == "context_revoked" {
				want = execution.StateBlocked
			}
			if repair && scenario != "repair_exhausted" && scenario != "repair_review_exhausted" {
				want = execution.StateCompleted
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
				if runtime.starts != 3 || source.prCreates != 1 || source.commits != 1 {
					t.Fatalf("duplicate effects: starts=%d PRs=%d commits=%d", runtime.starts, source.prCreates, source.commits)
				}
				evidence, err := journal.ListEvidence(ctx, e.ID)
				if err != nil || len(evidence) != 2 || evidence[0].SourceRevision != strings.Repeat("b", 40) || evidence[0].Kind != "semantic_review" {
					t.Fatalf("wrong evidence: %#v %v", evidence, err)
				}
			}
			if scenario == "landing_completed" || scenario == "landing_profile_completed" || scenario == "landing_prd" {
				if detail.Runs[0].Status != workcontrol.RunCompleted || !strings.Contains(detail.Runs[0].ResultSummary, strings.Repeat("b", 40)) || source.prCreates != 0 || source.lands != 1 || source.main != strings.Repeat("b", 40) || source.commits != 1 {
					t.Fatalf("landing effects: run=%+v source=%+v", detail.Runs[0], source)
				}
				if detail.Work.Landing == nil || detail.Work.Landing.Commit != source.main || len(detail.Work.Landing.EvidenceIDs) != 3 {
					t.Fatal("automatic completion lost exact landing provenance")
				}
			}
			if scenario == "landing_rebase" && (runtime.starts != 6 || source.commits != 2 || source.lands != 1 || detail.Work.Landing == nil || detail.Work.Landing.Commit != source.main) {
				t.Fatalf("rebase failed to rerun both gates: runtime=%+v source=%+v landing=%+v", runtime, source, detail.Work.Landing)
			}
			if scenario == "landing_rebase_conflict" && (runtime.starts != 4 || source.commits != 1 || source.lands != 0 || !strings.Contains(e.FailureMessage, "conflict")) {
				t.Fatal("rebase conflict published or skipped its blocker")
			}
			if repair {
				starts, commits := 7, 2
				if scenario == "repair_exhausted" {
					starts, commits = 11, 3
				} else if scenario == "repair_review" {
					starts, commits = 6, 1
				} else if scenario == "repair_review_exhausted" {
					starts, commits = 8, 0
				}
				if runtime.starts != starts || source.commits != commits || e.MaxAttempts != attemptLimit {
					t.Fatalf("repair duplicated work or changed the budget: starts=%d commits=%d budget=%d", runtime.starts, source.commits, e.MaxAttempts)
				}
				evidence, err := journal.ListEvidence(ctx, e.ID)
				evidenceCount := commits * 2
				if scenario == "repair_review" {
					evidenceCount++
				}
				if scenario == "repair_review_exhausted" {
					evidenceCount = 3
				}
				if scenario == "repair_ci" || scenario == "repair_pr_ci" {
					evidenceCount++
				}
				if scenario != "repair_pr_ci" && scenario != "repair_exhausted" && scenario != "repair_review_exhausted" {
					evidenceCount++
				}
				if err != nil || len(evidence) != evidenceCount || evidence[len(evidence)-1].SourceRevision != source.head {
					t.Fatalf("repair lost source-bound evidence history: %+v %v", evidence, err)
				}
				if scenario == "repair_exhausted" || scenario == "repair_review_exhausted" {
					if detail.Runs[0].Status != workcontrol.RunBlocked || source.lands != 0 || source.main != strings.Repeat("a", 40) {
						t.Fatal("exhausted repair landed or completed")
					}
				} else if detail.Runs[0].Status != workcontrol.RunCompleted || evidence[len(evidence)-1].Summary != "passed" {
					t.Fatal("repair completion did not use fresh passing evidence")
				}
				if scenario == "repair_pr_ci" && source.prCreates != 1 {
					t.Fatal("repair opened another pull request")
				}
				if scenario != "repair_pr_ci" && scenario != "repair_exhausted" && scenario != "repair_review_exhausted" && (source.lands != 1 || source.main != source.head || source.prCreates != 0) {
					t.Fatal("repair did not land the exact checked commit")
				}
			}
			if scenario == "landing_prd_changed" && (source.commits != 0 || source.lands != 0 || runtime.starts != 1) {
				t.Fatal("changed PRD escaped approval gate")
			}
			if scenario == "landing_prd" && (detail.Work.PRD == nil || detail.Work.PRD.Content != workflowPRDContent || runtime.starts != 3) {
				t.Fatal("PRD approval or gates lost")
			}
			if scenario == "review_invalid" && (runtime.starts != 2 || source.commits != 0) {
				t.Fatal("a malformed review committed a candidate")
			}
			if scenario == "landing_moved_base" || scenario == "landing_ci_failed" || scenario == "landing_ci_missing" || scenario == "landing_verification_failed" {
				if source.lands != 0 || source.prCreates != 0 || source.main == strings.Repeat("b", 40) {
					t.Fatalf("unsafe landing effects: %+v", source)
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
	var input worker.Input
	var encoded []byte
	if spec.Engine != nil {
		prompt, err := r.artifacts.Get(strings.TrimPrefix(spec.ContextURI, "artifact://"))
		if err != nil {
			return runtimeapi.Observation{}, err
		}
		_, data, found := strings.Cut(string(prompt), "Task: ")
		if spec.Stage == "review" {
			data, found = string(prompt), true
		}
		if !found {
			return runtimeapi.Observation{}, errors.New("native input missing")
		}
		encoded = []byte(data)
	} else {
		checkpoint, err := r.journal.GetCheckpoint(ctx, spec.ExecutionID, "worker-input:"+spec.AttemptID)
		if err != nil {
			return runtimeapi.Observation{}, err
		}
		encoded = []byte(checkpoint.Payload["input"].(string))
	}
	if err := json.Unmarshal(encoded, &input); err != nil {
		return runtimeapi.Observation{}, err
	}
	if input.Context == nil || input.Context.Digest != spec.ContextDigest {
		return runtimeapi.Observation{}, errors.New("worker did not receive the persisted context package")
	}
	if err := input.Context.ValidateAt(time.Now()); err != nil {
		return runtimeapi.Observation{}, err
	}
	r.starts++
	stage := spec.Stage
	if stage == "" {
		stage = "implement"
	}
	if strings.HasSuffix(spec.IdempotencyKey, ":verify") {
		stage = "verify"
	}
	result := worker.Result{Stage: stage, SourceRevision: spec.SourceRevision, ObservedAt: time.Now().UTC(), Summary: "Fixed fixture"}
	if stage == "implement" {
		if input.Rebase != nil && r.scenario == "landing_rebase_conflict" {
			result.BlockingReason = "The task patch has a conflict with the new base"
		} else {
			result.Changes = []worker.Change{{Path: "src/fix.go", Content: []byte("package fixture\n"), Mode: "100644"}}
			if strings.HasPrefix(r.scenario, "landing_prd") {
				result.Changes = []worker.Change{{Path: "docs/prd/filters.md", Content: []byte(workflowPRDContent), Mode: "100644"}}
				if r.scenario == "landing_prd_changed" {
					result.Changes[0].Content = []byte("changed without approval")
				}
			}
		}
	} else if stage == "repair_plan" {
		var feedback struct {
			Failures []struct {
				ArtifactDigest string `json:"artifact_digest"`
			} `json:"failures"`
		}
		if json.Unmarshal([]byte(input.Feedback), &feedback) != nil || len(feedback.Failures) == 0 {
			return runtimeapi.Observation{}, errors.New("repair has no failure evidence")
		}
		result.Repair = &workresult.RepairPlan{Diagnosis: "The recorded check exposes the boundary error",
			Approach: fmt.Sprintf("Use repair approach %d", len(feedback.Failures)), CanRepair: true,
			FailureDigest: feedback.Failures[len(feedback.Failures)-1].ArtifactDigest}
	} else if stage == "review" {
		result.InputDigest, result.ProfileDigest = spec.InputDigest, spec.ProfileDigest
		result.Review = &workresult.Review{PatchDigest: input.CandidateDigest}
		for _, id := range workresult.ReviewCheckIDs {
			outcome := "passed"
			if id == "tests" && (r.scenario == "repair_review_exhausted" || r.scenario == "repair_review" && !strings.Contains(spec.IdempotencyKey, ":repair:")) {
				outcome = "failed"
			}
			result.Review.Checks = append(result.Review.Checks, workresult.ReviewCheck{ID: id, Outcome: outcome, Reason: "Checked the candidate against its frozen task", Citations: []workresult.Citation{{Path: "src/fix.go", Line: 1}}})
		}
		if r.scenario == "review_invalid" {
			result.Review.Checks = result.Review.Checks[:2]
		}
	} else {
		exit := 0
		if r.scenario == "verification_failed" || r.scenario == "landing_verification_failed" {
			exit = 1
		}
		if r.scenario == "repair_exhausted" || r.scenario == "repair_verification" && !strings.Contains(spec.IdempotencyKey, ":repair:") {
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
	if strings.HasPrefix(r.scenario, "repair_") {
		return runtimeapi.Observation{}, context.DeadlineExceeded
	}
	return observation, nil
}
func (r *workflowRuntime) Cleanup(context.Context, string) error { return nil }

type workflowSource struct {
	sourcecontrol.Port
	head, main, scenario      string
	commits, prCreates, lands int
	lost                      bool
	writes                    map[string]sourcecontrol.Revision
}

func (s *workflowSource) ReadReviewFile(context.Context, string, string, string) ([]byte, bool, error) {
	return []byte("package fixture\n"), true, nil
}

func (s *workflowSource) CurrentHead(_ context.Context, r sourcecontrol.HeadRequest) (sourcecontrol.Revision, error) {
	if r.Ref == "main" {
		return sourcecontrol.Revision{Commit: s.main}, nil
	}
	if s.scenario == "head_changed" && s.prCreates > 0 {
		return sourcecontrol.Revision{Commit: strings.Repeat("c", 40)}, nil
	}
	return sourcecontrol.Revision{Commit: s.head}, nil
}
func (s *workflowSource) CreateBranch(_ context.Context, r sourcecontrol.BranchRequest) (sourcecontrol.Revision, error) {
	if strings.Contains(r.Name, "/rebase-") {
		s.head = r.BaseRevision
	}
	if s.head != r.BaseRevision {
		return sourcecontrol.Revision{}, errors.New("branch already advanced")
	}
	return sourcecontrol.Revision{Commit: s.head}, nil
}
func (s *workflowSource) WriteChanges(_ context.Context, request sourcecontrol.WriteRequest) (sourcecontrol.Revision, error) {
	if prior, ok := s.writes[request.IdempotencyKey]; ok {
		return prior, nil
	}
	if request.ExpectedHead != s.head {
		return sourcecontrol.Revision{}, errors.New("commit is not based on the previous task-branch head")
	}
	if s.writes == nil {
		s.writes = map[string]sourcecontrol.Revision{}
	}
	s.commits++
	s.head = strings.Repeat(string(rune('b'+s.commits-1)), 40)
	if strings.HasPrefix(s.scenario, "landing_rebase") && s.commits > 1 {
		s.head = strings.Repeat("d", 40)
	}
	if (s.scenario == "landing_moved_base" || strings.HasPrefix(s.scenario, "landing_rebase")) && s.commits == 1 {
		s.main = strings.Repeat("c", 40)
	}
	s.writes[request.IdempotencyKey] = sourcecontrol.Revision{Commit: s.head}
	return sourcecontrol.Revision{}, context.DeadlineExceeded
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
	if s.scenario == "ci_missing" || s.scenario == "landing_ci_missing" {
		return nil, nil
	}
	state := "success"
	if s.scenario == "landing_ci_failed" {
		state = "failure"
	}
	if (s.scenario == "repair_ci" || s.scenario == "repair_pr_ci") && s.commits == 1 {
		state = "failure"
	}
	return []sourcecontrol.CheckObservation{{ID: "required-test", HeadRevision: s.head, State: state, ObservedAt: time.Now()}}, nil
}

func (s *workflowSource) FastForward(_ context.Context, request sourcecontrol.FastForwardRequest) (sourcecontrol.Revision, error) {
	if request.ExpectedHead != s.head || request.HeadRef == "" || request.BaseRef != "main" {
		return sourcecontrol.Revision{}, errors.New("unguarded landing")
	}
	if s.main == s.head {
		return sourcecontrol.Revision{Commit: s.main, Ref: "main"}, nil
	}
	if s.main != request.ExpectedBase {
		return sourcecontrol.Revision{}, errors.New("main moved")
	}
	s.lands++
	s.main = s.head
	if s.lost == false {
		s.lost = true
		return sourcecontrol.Revision{}, context.DeadlineExceeded
	}
	return sourcecontrol.Revision{Commit: s.main, Ref: "main"}, nil
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
	current, err := client.GetRepository(ctx, config.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	previousVersion := int64(0)
	if current.Binding != nil {
		previousVersion = current.Binding.Version
	}
	request := workcontrol.RepositoryRequestInput{ExpectedRevision: current.Revision, ExpectedBindingVersion: previousVersion, Repository: "owner/first", BaseBranch: "main"}
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

const workflowPRDContent = "# Saved filters\n\nUsers can save and restore filters.\n"

func approveWorkflowPRD(t *testing.T, ctx context.Context, client *Client, config flow.Config, scenario string, budget any) taskDTO {
	t.Helper()
	if err := client.PublishWorkloads(ctx, "prj_default", workcontrol.WorkloadCatalog{Profiles: config.WorkloadProfiles(), ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	agent := "prd-" + scenario
	if _, err := client.RegisterAgent(ctx, workcontrol.AgentRegistration{ID: agent, InstallationID: agent, Name: "PRD fixture", Kind: "conversation", Capabilities: []string{"conversation.invoke.prj_default"}}); err != nil {
		t.Fatal(err)
	}
	var channel struct {
		ID string `json:"id"`
	}
	if err := client.do(ctx, "POST", "/api/v1/operator/channels", map[string]string{"project_id": "prj_default", "name": scenario}, &channel); err != nil {
		t.Fatal(err)
	}
	var inv struct {
		Message workcontrol.ConversationMessage `json:"message"`
	}
	if err := client.do(ctx, "POST", "/api/v1/operator/channels/"+channel.ID+"/invocations", map[string]string{"agent_id": agent, "request_id": scenario, "body": "Draft a PRD for saved filters"}, &inv); err != nil {
		t.Fatal(err)
	}
	out, err := client.PublishConversationOutcome(ctx, workcontrol.ConversationOutcomeInput{InvocationMessageID: inv.Message.ID, AgentID: agent, Status: "completed", Body: "Review this PRD", ResultDigest: "prd-fixture-" + scenario, PRD: &workcontrol.PRDDraft{Title: "Saved filters", Path: "docs/prd/filters.md", Content: workflowPRDContent}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.PageConversationMessages(ctx, channel.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	// Read the wire message with its additive typed PRD field through the public API.
	var messages struct {
		Items []struct {
			ID  string                   `json:"id"`
			PRD *workcontrol.PRDDocument `json:"prd"`
		} `json:"items"`
	}
	if len(page.Items) == 0 {
		t.Fatal("missing draft thread")
	}
	if err := client.do(ctx, "GET", "/api/v1/operator/channels/"+channel.ID+"/messages", nil, &messages); err != nil {
		t.Fatal(err)
	}
	var draft *workcontrol.PRDDocument
	for _, m := range messages.Items {
		if m.ID == out.MessageID {
			draft = m.PRD
		}
	}
	if draft == nil {
		t.Fatal("PRD missing from canonical thread")
	}
	operator, err := New(os.Getenv("DECK_CONFORMANCE_URL"), &http.Client{Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var task, repeat taskDTO
	approval := map[string]any{"digest": draft.Digest, "profile_digest": config.Digest(), "budget": budget}
	endpoint := "/api/v1/operator/messages/" + out.MessageID + "/prd/approve"
	if err := operator.doIdempotent(ctx, "POST", endpoint, "approve-"+scenario, approval, &task); err != nil {
		t.Fatal(err)
	}
	if err := operator.doIdempotent(ctx, "POST", endpoint, "repeat-"+scenario, approval, &repeat); err != nil || repeat.ID != task.ID {
		t.Fatalf("duplicate approval: %+v %v", repeat, err)
	}
	if task.PRD == nil || task.PRD.Content != workflowPRDContent {
		t.Fatal("approved task lost exact draft")
	}
	return task
}
