package flow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kagent"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol/github"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

type flowTemplates struct {
	pending int
	ensured []runtimeapi.EngineTemplate
	deleted []string
}

func (f *flowTemplates) EnsureEngineTemplate(_ context.Context, t runtimeapi.EngineTemplate) (string, error) {
	f.ensured = append(f.ensured, t)
	if f.pending > 0 {
		f.pending--
		return "", runtimeapi.ErrEngineTemplatePending
	}
	sum := sha256.Sum256([]byte(t.Name))
	return hex.EncodeToString(sum[:]), nil
}

func (f *flowTemplates) DeleteEngineTemplate(_ context.Context, namespace, name string) error {
	f.deleted = append(f.deleted, namespace+"/"+name)
	return nil
}

// terminalEngine reports every attempt as already finished, so the test
// observes provisioning, the frozen specification and cleanup only.
type terminalEngine struct {
	runtimeapi.Adapter
	started []runtimeapi.Specification
	cleaned []string
}

func (e *terminalEngine) InspectByKey(context.Context, string) (runtimeapi.Observation, error) {
	return runtimeapi.Observation{}, nil
}
func (e *terminalEngine) Start(_ context.Context, spec runtimeapi.Specification) (runtimeapi.Observation, error) {
	e.started = append(e.started, spec)
	return runtimeapi.Observation{Found: true, ProviderHandle: "kagent:" + spec.AttemptID, State: runtimeapi.StateFailed}, nil
}
func (e *terminalEngine) CollectArtifacts(context.Context, string) ([]runtimeapi.Artifact, error) {
	return nil, nil
}
func (e *terminalEngine) Cleanup(_ context.Context, handle string) error {
	e.cleaned = append(e.cleaned, handle)
	return nil
}

type authorizedRun struct {
	Provider
	run workcontrol.Run
}

func (p authorizedRun) GetWork(context.Context, string) (workcontrol.WorkDetail, error) {
	return workcontrol.WorkDetail{Runs: []workcontrol.Run{p.run}}, nil
}

// engineFlow is a claimed execution at its implement step whose controller is
// configured for native Codex; each test wires the runtimes it observes.
type engineFlow struct {
	ctx      context.Context
	journal  *sqlite.Store
	path     string
	deadline time.Time
	c        *Controller
	e        execution.Execution
	task     workcontrol.WorkItem
	s        state
}

func newEngineFlow(t *testing.T) engineFlow {
	t.Helper()
	ctx := context.Background()
	path := t.TempDir() + "/journal.db"
	journal, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { journal.Close() })
	blobs, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Hour)
	if _, err := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "project", Provider: "deck", Repository: "https://github.com/owner/repo.git", Runtime: "test",
		PolicyVersion: "v1", ContextConfig: "cfg", ArtifactConfig: "a", DeliveryConfig: "github", BudgetMinutes: 30, MaxConcurrency: 1}); err != nil {
		t.Fatal(err)
	}
	task := workcontrol.WorkItem{ID: "task", Title: "Fix the bound", AcceptanceCriteria: "Tests pass"}
	snapshot, _ := json.Marshal(task)
	if _, _, err := journal.EnsureAcquisitionIntent(ctx, execution.NewAcquisitionIntent{TaskSnapshot: string(snapshot), ProjectID: "project", WorkID: "task", TaskVersion: 1,
		TaskDigest: "digest", AcceptanceCriteriaDigest: "criteria", ClaimKey: "claim", AgentID: "agent", LeaseSeconds: 30, BudgetMinutes: 30}); err != nil {
		t.Fatal(err)
	}
	e, _, err := journal.EnsureExecution(ctx, execution.NewExecution{ProjectID: "project", WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", TaskVersion: 1, LeaseExpiresAt: deadline,
		Budget: execution.Budget{DeadlineAt: deadline, MaxAttempts: 2, MaxTokens: 20000}})
	if err != nil {
		t.Fatal(err)
	}
	lease := deadline
	provider := authorizedRun{run: workcontrol.Run{ID: "run", WorkItemID: "task", AgentID: e.AgentID, TaskVersion: e.TaskVersion, FenceToken: e.FenceToken, Status: workcontrol.RunRunning, LeaseExpiresAt: &lease}}
	config := Config{ProjectID: "project", PublicURL: "https://marina.marina-system.svc:8090", PolicyVersion: "v1", MaxTokens: 20000, BudgetMinutes: 30,
		AllowedPaths: []string{"src"}, Checks: []workresult.Check{{ID: "check", Directory: ".", Command: []string{"node", "--test"}}},
		Git:                        github.Config{Repository: "owner/repo", CloneURL: "https://github.com/owner/repo.git", BaseBranch: "main"},
		Model:                      workerapi.Model{Name: "fixture", MaxOutputTokens: 100, Currency: "USD"},
		RepositoryEngine:           &runtimeapi.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: "kint-engine-controller.kint-engine.svc:8083", Namespace: "kint-engine", Harness: "kint-codex"},
		RepositoryEngineGatewayURL: "https://marina.marina-system.svc"}
	config.Kubernetes.Image = "worker@sha256:" + strings.Repeat("a", 64)
	config.ReviewerModel = &workerapi.Model{Endpoint: "https://reviewer.example/v1", Name: "independent-fixture", MaxOutputTokens: 100, Currency: "USD"}
	c := &Controller{Config: config, Journal: journal, Provider: provider, Artifacts: blobs,
		PrepareEngine: func(context.Context, runtimeapi.Specification) error { return nil }}
	s := state{Step: "implement", Base: strings.Repeat("b", 40), Head: strings.Repeat("b", 40), ConfigDigest: config.Digest()}
	manifest, err := c.assembleContext(time.Now().UTC(), e, workcontrol.WorkItem{ID: "task", Title: "Fix the bound"}, s, workcontrol.ResolvedContext{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = journal.RecordContextPackage(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	s.ContextDigest = manifest.Digest
	return engineFlow{ctx: ctx, journal: journal, path: path, deadline: deadline, c: c, e: e, task: task, s: s}
}

func TestEachRepositoryEngineAttemptRunsOnItsOwnPreparedTemplate(t *testing.T) {
	f := newEngineFlow(t)
	ctx, journal, deadline, c, e, task, s := f.ctx, f.journal, f.deadline, f.c, f.e, f.task, f.s
	templates := &flowTemplates{pending: 1}
	engine := &terminalEngine{}
	c.EngineRuntime, c.EngineTemplates = engine, templates

	// An unprepared template records no attempt and starts nothing.
	if _, done, _, _, err := c.worker(ctx, e, task, s, "implement"); err != nil || done {
		t.Fatalf("pending template: done=%v err=%v", done, err)
	}
	if attempts, _ := journal.ListRuntimeAttempts(ctx, e.ID); len(attempts) != 0 || len(engine.started) != 0 {
		t.Fatalf("attempt recorded before its template was prepared: %d %d", len(attempts), len(engine.started))
	}
	attempt, done, _, _, err := c.worker(ctx, e, task, s, "implement")
	if err != nil || !done || attempt.State != runtimeapi.StateFailed {
		t.Fatalf("prepared attempt: %+v done=%v err=%v", attempt, done, err)
	}
	name := runtimeapi.EngineTemplateName(e.ID + ":implement")
	selection := attempt.Specification.Engine
	if len(engine.started) != 1 || selection == nil || selection.Template != name || selection.Revision != templates.ensuredRevision(name) ||
		attempt.Specification.CredentialReferences[0].Reference != "secret://kint-engine/"+name+"/gateway" {
		t.Fatalf("attempt did not bind its own prepared template: %+v", attempt.Specification)
	}
	prepared := templates.ensured[len(templates.ensured)-1]
	if prepared.Harness != "kint-codex" || prepared.BaseURL != "https://marina.marina-system.svc/engine/model/v1" || prepared.Namespace != "kint-engine" {
		t.Fatalf("prepared template = %+v", prepared)
	}
	if err := c.cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if len(engine.cleaned) != 1 || strings.Join(templates.deleted, ",") != "kint-engine/"+name {
		t.Fatalf("cleanup kept the attempt template: cleaned=%v deleted=%v", engine.cleaned, templates.deleted)
	}

	// Work stopped while its template was still being prepared leaves no
	// attempt record; stopping it removes the template as well.
	other, _, err := journal.EnsureExecution(ctx, execution.NewExecution{ProjectID: "project", WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run-2", AgentID: "agent", TaskVersion: 1, LeaseExpiresAt: deadline,
		Budget: execution.Budget{DeadlineAt: deadline, MaxAttempts: 2, MaxTokens: 20000}})
	if err != nil {
		t.Fatal(err)
	}
	templates.pending, templates.deleted = 1, nil
	if _, done, _, _, err := c.worker(ctx, other, task, s, "implement"); err != nil || done {
		t.Fatalf("pending template: done=%v err=%v", done, err)
	}
	if err := c.stop(ctx, other); err != nil {
		t.Fatal(err)
	}
	if strings.Join(templates.deleted, ",") != "kint-engine/"+runtimeapi.EngineTemplateName(other.ID+":implement") {
		t.Fatalf("stopped work kept its unrecorded template: %v", templates.deleted)
	}
}

func (f *flowTemplates) ensuredRevision(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

// recordingRuntime stands in for one engine: it starts an attempt once and
// then reports it as still running.
type recordingRuntime struct {
	runtimeapi.Adapter
	prefix    string
	started   int
	inspected []string
}

func (r *recordingRuntime) InspectByKey(context.Context, string) (runtimeapi.Observation, error) {
	return runtimeapi.Observation{}, nil
}
func (r *recordingRuntime) Start(_ context.Context, spec runtimeapi.Specification) (runtimeapi.Observation, error) {
	r.started++
	return runtimeapi.Observation{Found: true, ProviderHandle: r.prefix + spec.AttemptID, State: runtimeapi.StateRunning}, nil
}
func (r *recordingRuntime) Inspect(_ context.Context, handle string) (runtimeapi.Observation, error) {
	r.inspected = append(r.inspected, handle)
	return runtimeapi.Observation{Found: true, ProviderHandle: handle, State: runtimeapi.StateRunning}, nil
}

func TestActiveAttemptStaysOnItsEngineWhenTheProfileChanges(t *testing.T) {
	f := newEngineFlow(t)
	jobs, engine := &recordingRuntime{prefix: "job:"}, &recordingRuntime{prefix: "kagent:"}
	f.c.Runtime, f.c.EngineRuntime, f.c.EngineTemplates = jobs, engine, &flowTemplates{}
	if _, done, _, _, err := f.c.worker(f.ctx, f.e, f.task, f.s, "implement"); err != nil || done || engine.started != 1 {
		t.Fatalf("first native start: done=%v err=%v starts=%d", done, err, engine.started)
	}
	f.c.Config.RepositoryEngine = nil
	attempt, done, _, _, err := f.c.worker(f.ctx, f.e, f.task, f.s, "implement")
	if err != nil || done || len(engine.inspected) != 1 || jobs.started != 0 || len(jobs.inspected) != 0 || attempt.Specification.Engine == nil {
		t.Fatalf("active attempt moved engines: done=%v err=%v engine=%+v jobs=%+v", done, err, engine, jobs)
	}
}
