// Package sandbox runs the real marina.dev/v1 HTTP API, authentication and
// resource controller against an in-memory Kubernetes API, with a simulated
// executor in place of agents. Client contract tests (MAR-07) start it as a
// real process; it never runs agents, touches Git or calls models.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/conversation"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/flow"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kubeapi"
	"github.com/jmlero/the-platform/marina/internal/platformapi"
	"github.com/jmlero/the-platform/marina/internal/platformapi/kubefake"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol/github"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

const (
	Namespace = "factory"
	Project   = "acceptance"
	agentID   = "marina-sandbox"
	headSHA   = "7c1e0f2a9b8d4c3e5f6a7b8c9d0e1f2a3b4c5d6e"
	baseSHA   = "151d521493f3e6610f09adb343f02cf5fba86a6c"
)

// Sandbox holds a running API sandbox.
type Sandbox struct {
	Kube       *kubefake.Server
	Platform   *sqlite.Platform
	controller *platformapi.Controller
	journal    *sqlite.Store
	server     *http.Server
	kube       *http.Server
	tick       time.Duration
	log        *slog.Logger
}

// Start serves the API on listener with an in-memory Kubernetes API. Tokens
// "user:<name>" authenticate as <name>; the user "viewer" may only read.
func Start(ctx context.Context, listener net.Listener, journalPath, artifactDir string, tick time.Duration, log *slog.Logger) (*Sandbox, error) {
	fake := kubefake.New()
	fake.Allow = func(user, verb, _, _, _ string) bool {
		return user != "viewer" || verb == "get" || verb == "list"
	}
	kubeListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	kubeServer := &http.Server{Handler: fake, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = kubeServer.Serve(kubeListener) }()
	kube, err := kubeapi.New(kubeapi.Config{ServerURL: "http://" + kubeListener.Addr().String()}, &http.Client{Timeout: 10 * time.Second})
	if err != nil {
		return nil, err
	}
	seed(fake)
	s, err := StartWith(ctx, listener, kube, journalPath, artifactDir, tick, log)
	if err != nil {
		return nil, err
	}
	s.Kube, s.kube = fake, kubeServer
	return s, nil
}

// StartWith serves the API against a real Kubernetes API server, where the
// marina.dev resources are installed and created with kubectl.
func StartWith(ctx context.Context, listener net.Listener, kube *kubeapi.Client, journalPath, artifactDir string, tick time.Duration, log *slog.Logger) (*Sandbox, error) {
	journal, err := sqlite.Open(ctx, journalPath)
	if err != nil {
		return nil, err
	}
	store, err := artifacts.New(artifactDir)
	if err != nil {
		return nil, err
	}
	id := sqlite.PlatformWorkID(Namespace, Project)
	binding, err := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: id, Provider: "marina", Repository: "https://github.com/example/acceptance.git",
		Runtime: "sandbox", PolicyVersion: "sandbox", ContextConfig: "sandbox", ArtifactConfig: artifactDir, DeliveryConfig: "sandbox", BudgetMinutes: 120, MaxConcurrency: 1})
	if err != nil {
		return nil, err
	}
	if err := journal.RecordRepositorySnapshot(ctx, id, "sandbox", workcontrol.RepositoryBinding{Version: binding.Version, Repository: "example/acceptance",
		CloneURL: "https://github.com/example/acceptance.git", BaseBranch: "main"}); err != nil {
		return nil, err
	}
	config := flow.Config{ProjectID: id, MaxTokens: 400000, BudgetMinutes: 120, Delivery: "fast_forward", RequiredCI: []string{"test"},
		AllowedPaths: []string{"src", "tests", "docs"}, Checks: []workresult.Check{{ID: "unit", Directory: ".", Command: []string{"npm", "test"}}},
		Git:   github.Config{Repository: "example/acceptance", CloneURL: "https://github.com/example/acceptance.git", BaseBranch: "main"},
		Model: workerapi.Model{Name: "sandbox-model"}, ReviewerModel: &workerapi.Model{Name: "sandbox-reviewer"},
		Conversation: &conversation.Profile{BudgetMinutes: 15, MaxTokens: 40000}}
	platform := journal.Platform()
	controller := &platformapi.Controller{Kube: kube, Journal: journal, Platform: platform, Projects: map[string]flow.Config{id: config}, AgentID: agentID}
	api := &platformapi.Server{Kube: kube, Platform: platform, Artifacts: store, Poll: tick,
		Capabilities: func(context.Context) (platformapi.Capabilities, error) {
			return platformapi.Capabilities{WorkloadTypes: []string{"repository-change", "conversation"},
				Models: []platformapi.ModelCapability{{Name: "sandbox-model", Roles: []string{"implement", "conversation"}}, {Name: "sandbox-reviewer", Roles: []string{"review"}}},
				Tools:  []platformapi.ToolCapability{}, Limits: platformapi.Limits{MaxTimeMinutes: 120, MaxTokens: 400000, MaxAttempts: 11}}, nil
		}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.Handle("/apis/", api.Handler(platformapi.NewAuthorizer(kube)))
	s := &Sandbox{Platform: platform, controller: controller, journal: journal, tick: tick, log: log,
		server: &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}}
	go func() { _ = s.server.Serve(listener) }()
	go s.loop(ctx)
	return s, nil
}

func seed(fake *kubefake.Server) {
	fake.Put("projects", map[string]any{"apiVersion": "marina.dev/v1", "kind": "Project", "metadata": map[string]any{"name": Project, "namespace": Namespace},
		"spec": map[string]any{"displayName": "Acceptance repository", "defaultWorkload": "land-on-main", "repository": map[string]any{"provider": "github",
			"url": "https://github.com/example/acceptance.git", "baseBranch": "main", "credentialSecret": map[string]any{"name": "acceptance-git", "key": "token"}}}})
	fake.Put("workloads", map[string]any{"apiVersion": "marina.dev/v1", "kind": "Workload", "metadata": map[string]any{"name": "land-on-main", "namespace": Namespace},
		"spec": map[string]any{"type": "repository-change", "repositoryChange": map[string]any{"model": "sandbox-model", "allowedPaths": []any{"src", "tests", "docs"},
			"checks": []any{map[string]any{"id": "unit", "command": []any{"npm", "test"}}}, "planApproval": "required", "review": map[string]any{"enabled": true, "model": "sandbox-reviewer"},
			"delivery": map[string]any{"mode": "land-on-main", "requiredChecks": []any{"test"}}, "repair": map[string]any{"maxFailures": 3}}}})
	fake.Put("workloads", map[string]any{"apiVersion": "marina.dev/v1", "kind": "Workload", "metadata": map[string]any{"name": "chat", "namespace": Namespace},
		"spec": map[string]any{"type": "conversation", "conversation": map[string]any{"model": "sandbox-model", "repositoryTools": true, "proposals": map[string]any{"task": true, "prd": true, "prdTasks": true}}}})
}

// Close stops the servers and closes the journal.
func (s *Sandbox) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errs := []error{s.server.Shutdown(ctx), s.journal.Close()}
	if s.kube != nil {
		errs = append(errs, s.kube.Shutdown(ctx))
	}
	return errors.Join(errs...)
}

func (s *Sandbox) loop(ctx context.Context) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	for {
		for _, problem := range s.controller.Cycle(ctx) {
			s.log.Debug("sandbox reconciliation deferred", "error", problem)
		}
		if err := s.simulate(ctx); err != nil && ctx.Err() == nil {
			s.log.Debug("sandbox executor deferred", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// simulate advances each Run and Turn one step, the way the workflow would,
// through the same ledger calls.
func (s *Sandbox) simulate(ctx context.Context) error {
	p := s.Platform
	id := sqlite.PlatformWorkID(Namespace, Project)
	if project, err := p.Project(ctx, id); err == nil && project.Result == nil {
		current, _ := s.journal.CurrentProjectBinding(ctx, id)
		binding, _, _ := s.journal.RepositorySnapshot(ctx, id, current.Version)
		if err := p.RecordRepositoryResult(ctx, id, workcontrol.RepositoryResult{RequestRevision: project.Request.Revision, Status: "bound", Binding: &binding}); err != nil {
			return err
		}
	}
	ready, err := p.ListReadyWork(ctx)
	if err != nil {
		return err
	}
	for _, item := range ready {
		if _, err := p.ClaimWork(ctx, item.ID, workcontrol.Claim{IdempotencyKey: "claim:" + item.ID, AgentID: agentID, ExpectedTaskVersion: item.Version, Lease: time.Hour}); err != nil {
			return err
		}
	}
	works, err := p.ListWork(ctx, true, 100)
	if err != nil {
		return err
	}
	for _, w := range works {
		if err := s.step(ctx, w); err != nil {
			return fmt.Errorf("%s: %w", w.Name, err)
		}
	}
	turns, err := p.ListTurns(ctx, true, 100)
	if err != nil {
		return err
	}
	for _, t := range turns {
		if err := s.reply(ctx, t); err != nil {
			return fmt.Errorf("%s: %w", t.Name, err)
		}
	}
	return nil
}

func (s *Sandbox) step(ctx context.Context, w sqlite.PlatformWork) error {
	p := s.Platform
	detail, err := p.GetWork(ctx, w.ID)
	if err != nil || len(detail.Runs) == 0 {
		return err
	}
	run := detail.Runs[len(detail.Runs)-1]
	m := func(step string) workcontrol.Mutation {
		return workcontrol.Mutation{IdempotencyKey: run.ID + ":" + step, AgentID: agentID, ExpectedVersion: run.Version, FenceToken: run.FenceToken}
	}
	if commands, _ := p.ListRunCommands(ctx, run.ID, 0, 10); len(commands) > 0 && commands[len(commands)-1].Status == "pending" {
		command := commands[len(commands)-1]
		return p.AcknowledgeRunCommand(ctx, run.ID, command.ID, workcontrol.CommandAcknowledgement{IdempotencyKey: "ack:" + command.ID, AgentID: agentID, FenceToken: run.FenceToken, Status: "completed", Result: "cancelled"})
	}
	switch run.Status {
	case workcontrol.RunPlanning:
		approval := w.Item.Choices != nil && w.Item.Choices.RequireApproval
		_, err := p.SubmitPlanRevision(ctx, run.ID, workcontrol.PlanRevisionSubmission{IdempotencyKey: run.ID + ":plan:" + fmt.Sprint(run.Version), AgentID: agentID,
			ExpectedVersion: run.Version, FenceToken: run.FenceToken, Plan: "Sandbox plan for " + w.Item.Title, ContextDigest: "sha256:sandbox",
			SourceRevision: baseSHA, RequireApproval: approval})
		return err
	case workcontrol.RunWaitingDecision:
		decisions := detail.Decisions
		if len(decisions) == 0 || decisions[len(decisions)-1].Status != workcontrol.DecisionAnswered {
			return nil
		}
		return p.StartRun(ctx, run.ID, m("start"))
	case workcontrol.RunRunning:
		if err := p.AppendRunEvent(ctx, run.ID, workcontrol.ProjectedEvent{IdempotencyKey: run.ID + ":progress", AgentID: agentID, FenceToken: run.FenceToken,
			Producer: "sandbox", ProducerEventID: run.ID + ":implement", Type: "execution.progress", Message: "Implementing the approved change", Payload: map[string]any{"step": "implement"}}); err != nil {
			return err
		}
		return p.BeginVerification(ctx, run.ID, m("verify"))
	case workcontrol.RunVerifying:
		if strings.Contains(strings.ToLower(w.Item.Title), "block") {
			return p.BlockRun(ctx, run.ID, workcontrol.RunBlock{IdempotencyKey: run.ID + ":block", AgentID: agentID, ExpectedVersion: run.Version, FenceToken: run.FenceToken,
				Reason: "Sandbox block requested by the title"})
		}
		now := time.Now().UTC()
		var ids []string
		for _, kind := range []string{"semantic_review", "test", "ci", "landing"} {
			evidenceID, err := p.RecordEvidence(ctx, run.ID, m("evidence-"+kind), workcontrol.EvidenceSubmission{ID: run.ID + "-" + kind, PlanRevisionID: run.ActivePlanRevisionID,
				Type: kind, Producer: "sandbox-implementer", Verifier: "sandbox-" + kind, SubjectDigest: "sha256:subject", SourceRevision: headSHA, Command: kind,
				Outcome: "passed", ObservedAt: now, ArtifactDigest: "sha256:" + strings.Repeat("0", 64)})
			if err != nil {
				return err
			}
			ids = append(ids, evidenceID)
			if detail, err = p.GetWork(ctx, w.ID); err != nil {
				return err
			}
			run = detail.Runs[len(detail.Runs)-1]
		}
		return p.CompleteLandedRun(ctx, run.ID, m("complete"), "Sandbox commit landed on main", ids,
			workcontrol.LandingObservation{Repository: "example/acceptance", BaseBranch: "main", Commit: headSHA, ObservedAt: now.UnixMilli()})
	}
	return nil
}

// reply answers a Turn. A message containing "clarify" first asks a question
// and waits for its answer.
func (s *Sandbox) reply(ctx context.Context, t sqlite.PlatformTurn) error {
	if t.CancelRequested {
		return nil
	}
	body := t.Messages[len(t.Messages)-1].Body
	answer := "Sandbox reply to: " + body
	if strings.Contains(strings.ToLower(body), "clarify") {
		q, err := s.Platform.EnsureConversationQuestion(ctx, workcontrol.ConversationQuestionInput{InvocationMessageID: t.ID, AgentID: agentID, QuestionID: "q1",
			Text: "Which part should I explain?", Choices: []string{"API", "Store"}, ExpiresAt: time.Now().Add(time.Hour).UnixMilli()})
		if err != nil || q.Status == "pending" {
			return err
		}
		answer = "Sandbox reply about " + q.Answer
	}
	outcome := workcontrol.ConversationOutcomeInput{InvocationMessageID: t.ID, AgentID: agentID, Status: "completed", Body: answer, ResultDigest: "sandbox:" + t.ID}
	if strings.Contains(strings.ToLower(body), "propose") {
		outcome.Task = &workcontrol.ConversationTaskInput{Title: "Sandbox proposal", AcceptanceCriteria: "A test passes", Status: "backlog"}
	}
	_, err := s.Platform.PublishConversationOutcome(ctx, outcome)
	return err
}
