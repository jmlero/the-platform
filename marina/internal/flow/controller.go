package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/worker"
)

type Provider interface {
	workcontrol.WorkInspection
	workcontrol.VersionedPlanning
	workcontrol.DecisionInspection
	workcontrol.Completion
	workcontrol.RunControl
}

type Controller struct {
	BindingConfig func(context.Context, execution.Execution) (Config, error)
	Config        Config
	Journal       *sqlite.Store
	Provider      Provider
	Runtime       runtimeapi.Adapter
	Artifacts     *artifacts.Store
	Source        func(execution.Execution) (sourcecontrol.Port, error)
}

// Every value used in an external write is derived from this durable state,
// the frozen task, or the versioned installation configuration.
type state struct {
	Analysis             *analysis.Profile `json:",omitempty"`
	ProfileDigest        string            `json:",omitempty"`
	ValidationObservedAt time.Time         `json:",omitempty"`
	Step                 string
	ConfigDigest         string
	ContextDigest        string
	Base                 string
	Head                 string
	Branch               string
	PlanID               string
	DecisionID           string
	Implementer          string
	Verifier             string
	ImplementationDigest string
	VerificationDigest   string
	PR                   sourcecontrol.PullRequest
	Reason               string
	EvidenceIDs          []string
}

func (c *Controller) load(ctx context.Context, e execution.Execution) (state, error) {
	cp, err := c.Journal.LatestPhaseCheckpoint(ctx, e.ID, "repository_flow")
	if errors.Is(err, execution.ErrNotFound) {
		return state{Step: "prepare", ConfigDigest: c.Config.Digest(), Branch: "marina/" + e.ID}, nil
	}
	if err != nil {
		return state{}, err
	}
	var s state
	b, _ := cp.Payload["state"].(string)
	err = json.Unmarshal([]byte(b), &s)
	return s, err
}

func (c *Controller) save(ctx context.Context, e execution.Execution, s state) error {
	e, err := c.Journal.GetExecution(ctx, e.ID)
	if err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, checkpoint, _, err := c.Journal.SaveCheckpoint(ctx, e.ID, e.Version, execution.CheckpointInput{Key: fmt.Sprintf("flow:%d", e.Version), Phase: "repository_flow", SourceRevision: s.Head, Payload: map[string]any{"state": string(b)}})
	if err != nil {
		return err
	}
	message := map[string]string{"plan": "Repository pinned; preparing approval", "approval": "Plan submitted", "implement": "Implementing the approved change", "branch": "Implementation result saved", "publish": "Publishing the scoped change", "verify": "Verifying the published commit in a clean worker", "pull_request": "Independent checks passed; opening pull request", "ci": "Waiting for required CI and review checks", "evidence": "Recording verification evidence", "complete": "Verification evidence ready"}[s.Step]
	if message == "" {
		message = s.Step
	}
	if s.Reason != "" {
		message = s.Reason
	}
	if s.Analysis != nil && s.Reason == "" {
		message = map[string]string{
			"plan":              "Diagnostic inputs frozen; preparing approval",
			"approval":          "Diagnostic analysis plan submitted",
			"analyze":           "Analyzing the frozen diagnostic records",
			"analysis_verify":   "Independently checking diagnostic findings and source quotes",
			"analysis_evidence": "Recording diagnostic validation evidence",
			"complete":          "Diagnostic evidence ready",
		}[s.Step]
	}
	totals, err := c.Journal.ExecutionModelTotals(ctx, e.ID)
	if err != nil {
		return err
	}
	workload, harness, profile := "repository/v1", "codex/v1", s.ConfigDigest
	if s.Analysis != nil {
		workload = analysis.Kind
		harness = s.Analysis.HarnessVersion
		profile = s.ProfileDigest
	}
	_, _, err = c.Journal.EnqueueOutbox(ctx, e.ID, execution.OutboxInput{Kind: "event", IdempotencyKey: fmt.Sprintf("workflow:%s:%d", e.ID, checkpoint.ID), Payload: map[string]any{"type": "execution.progress", "message": message, "source_revision": s.Head, "execution_id": e.ID, "workload": workload, "harness": harness, "profile_digest": profile, "reported_tokens": totals.Tokens, "reported_cost_micros": totals.CostMicros, "pending_model_calls": totals.PendingCalls, "reserved_tokens": totals.ReservedTokens, "reserved_cost_micros": totals.ReservedCostMicros, "currency": c.Config.Model.Currency, "occurred_at": checkpoint.CreatedAt.UnixMilli()}})
	return err
}

func (c *Controller) run(ctx context.Context, e execution.Execution) (workcontrol.Run, error) {
	d, err := c.Provider.GetWork(ctx, e.ExternalWorkID)
	if err != nil {
		return workcontrol.Run{}, err
	}
	for _, r := range d.Runs {
		if r.ID == e.ExternalRunID && r.AgentID == e.AgentID && r.TaskVersion == e.TaskVersion && r.FenceToken == e.FenceToken {
			return r, nil
		}
	}
	return workcontrol.Run{}, errors.New("provider authority no longer matches execution")
}

func guard(e execution.Execution, r workcontrol.Run, step string) workcontrol.Mutation {
	return workcontrol.Mutation{IdempotencyKey: fmt.Sprintf("%s:%s:%d", e.ID, step, r.Version), AgentID: e.AgentID, ExpectedVersion: r.Version, FenceToken: r.FenceToken}
}

func (c *Controller) Authorize(ctx context.Context, e execution.Execution, fence int64) error {
	current, err := c.Journal.GetExecution(ctx, e.ID)
	if err != nil {
		return err
	}
	if current.FenceToken != fence || !current.AuthoritySafe(time.Now(), time.Second) {
		return errors.New("execution authority unavailable")
	}
	r, err := c.run(ctx, current)
	if err != nil {
		return err
	}
	if !r.Status.Active() || r.LeaseExpiresAt == nil || !time.Now().Add(time.Second).Before(*r.LeaseExpiresAt) {
		return errors.New("live provider authority unavailable")
	}
	return c.CheckContext(ctx, e)
}

func (c *Controller) CheckContext(ctx context.Context, e execution.Execution) error {
	if manifest, err := c.Journal.ExecutionContextPackage(ctx, e.ID); err == nil {
		if err = manifest.ValidateAt(time.Now()); err != nil {
			return contextProblem("The saved context contains stale or revoked inputs")
		}
	} else if !errors.Is(err, execution.ErrNotFound) {
		return err
	}
	snapshot, err := c.Journal.AcquiredTaskSnapshot(ctx, e.ProjectID, e.ExternalWorkID, e.TaskVersion)
	if err != nil {
		return err
	}
	var task workcontrol.WorkItem
	if err = json.Unmarshal([]byte(snapshot), &task); err != nil {
		return err
	}
	profileConfig := c.Config
	if c.BindingConfig != nil && !analysisRequested(task) {
		profileConfig, err = c.BindingConfig(ctx, e)
		if err != nil {
			return err
		}
	}
	if !profileConfig.MatchesWorkload(task) {
		return contextProblem("The selected workload profile is unavailable or changed")
	}
	if analysisRequested(task) {
		if c.Config.Analysis == nil || len(task.Budget.Authority) != 1 || task.Budget.Authority[0] != c.Config.AnalysisAuthority() {
			return contextProblem("The authorized analysis profile is unavailable or changed")
		}
	}
	if task.Context != nil {
		reader, ok := c.Provider.(workcontrol.ContextInspection)
		if !ok {
			return contextProblem("Provider cannot validate selected context")
		}
		resolved, err := reader.GetRunContext(ctx, e.ExternalRunID)
		if err != nil {
			return fmt.Errorf("%w: %v", workcontrol.ErrContextUnavailable, err)
		}
		if err = workcontrol.ValidateResolvedContext(task.Context, resolved); err != nil {
			return contextProblem(err.Error())
		}
		if err = workcontrol.CheckContextValidity(time.Now(), resolved); err != nil {
			return contextProblem(err.Error())
		}
	}
	return nil
}

func (c *Controller) Cycle(ctx context.Context) []error {
	var failures []error
	if err := c.cleanup(ctx); err != nil {
		failures = append(failures, err)
	}
	items, err := c.Journal.ListActiveExecutions(ctx, 100)
	if err != nil {
		return append(failures, err)
	}
	for _, e := range items {
		if e.ProjectID != c.Config.ProjectID {
			continue
		}
		op, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = c.advance(op, e)
		if current, loadErr := c.Journal.GetExecution(op, e.ID); loadErr == nil && !current.State.Terminal() {
			if run, readErr := c.run(op, current); readErr == nil && run.Status.Active() && run.LeaseExpiresAt != nil && run.Version != current.ExternalRunVersion {
				_, syncErr := c.Journal.UpdateProviderAuthority(op, e.ID, current.Version, run.Version, run.FenceToken, *run.LeaseExpiresAt, "workflow mutation acknowledged")
				if err == nil {
					err = syncErr
				}
			}
		}
		cancel()
		if err != nil {
			failures = append(failures, fmt.Errorf("execution %s: %w", e.ID, err))
		}
	}
	return failures
}

func (c *Controller) advance(ctx context.Context, e execution.Execution) error {
	snapshot, err := c.Journal.AcquiredTaskSnapshot(ctx, e.ProjectID, e.ExternalWorkID, e.TaskVersion)
	if err != nil {
		return err
	}
	var task workcontrol.WorkItem
	if err = json.Unmarshal([]byte(snapshot), &task); err != nil {
		return err
	}
	if c.BindingConfig != nil && !analysisRequested(task) {
		config, err := c.BindingConfig(ctx, e)
		if err != nil {
			r, readErr := c.run(ctx, e)
			if readErr != nil {
				return readErr
			}
			return c.block(ctx, e, r, "The original repository binding is unavailable or its installation profile changed; authorize new work after reviewing the installation")
		}
		effective := *c
		effective.Config = config
		effective.BindingConfig = nil
		return effective.advance(ctx, e)
	}
	s, err := c.load(ctx, e)
	if err != nil {
		return err
	}
	r, err := c.run(ctx, e)
	if err != nil {
		return err
	}
	if r.Status == workcontrol.RunCompleted && s.Step == "complete" {
		if s.Analysis != nil {
			return c.transition(ctx, e, execution.StateCompleted, "Validated diagnostic analysis available")
		}
		return c.transition(ctx, e, execution.StateCompleted, "Verified pull request ready for human review")
	}
	if err = c.Authorize(ctx, e, e.FenceToken); err != nil {
		if errors.Is(err, errContextClarification) {
			return c.block(ctx, e, r, err.Error())
		}
		return err
	}
	if s.ConfigDigest != c.Config.Digest() {
		return c.block(ctx, e, r, "Installation configuration changed during execution; create a new task after reviewing the change")
	}
	if s.Reason != "" {
		if s.VerificationDigest != "" && len(s.EvidenceIDs) < len(c.Config.Checks) {
			return c.evidence(ctx, e, r, s)
		}
		return c.block(ctx, e, r, s.Reason)
	}
	if s.Step == "paused" {
		return nil
	}
	if s.Step == "prepare" {
		if analysisRequested(task) {
			return c.prepareAnalysis(ctx, e, r, task, s)
		}
		if strings.TrimSpace(task.AcceptanceCriteria) == "" {
			return c.block(ctx, e, r, "Observable acceptance criteria are required for a repository change")
		}
		for _, authority := range []string{"source.read", "source.write", "pull_request.create"} {
			if !slices.Contains(task.Budget.Authority, authority) {
				return c.block(ctx, e, r, "Task must explicitly authorize source.read, source.write and pull_request.create; create a new task with the required authority")
			}
		}
		if e.MaxAttempts < 2 {
			return c.block(ctx, e, r, "At least two runtime attempts are required: implementation and independent verification")
		}
		if task.Budget.MaxCostMinor > 0 && (task.Budget.Currency != c.Config.Model.Currency || c.Config.Model.InputMicrosPerMillion == 0 || c.Config.Model.OutputMicrosPerMillion == 0) {
			return c.block(ctx, e, r, "Task cost currency must match configured nonzero model price rates")
		}
		source, err := c.Source(e)
		if err != nil {
			return err
		}
		base, err := source.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: c.Config.Git.Repository, Ref: c.Config.Git.BaseBranch})
		if err != nil {
			return err
		}
		s.Base = base.Commit
		s.Head = base.Commit
		s.Step = "plan"
		return c.save(ctx, e, s)
	}
	if s.Step == "plan" && s.ContextDigest == "" {
		s.ContextDigest, err = c.prepareContext(ctx, e, task, s)
		if err != nil {
			if errors.Is(err, errContextClarification) {
				return c.block(ctx, e, r, err.Error())
			}
			return err
		}
		return c.save(ctx, e, s)
	}
	if s.ContextDigest != "" {
		if err = c.publishContext(ctx, e, s); err != nil {
			return err
		}
	}
	if s.Step == "plan" {
		// A lost submission response is recovered from the authoritative ledger.
		if r.ActivePlanRevisionID != "" {
			d, err := c.Provider.GetWork(ctx, e.ExternalWorkID)
			if err != nil {
				return err
			}
			for _, p := range d.PlanRevisions {
				if p.ID == r.ActivePlanRevisionID && p.ContextDigest == c.contextDigest(task, s) {
					s.PlanID = p.ID
					for _, decision := range d.Decisions {
						if decision.PlanRevisionID == p.ID {
							s.DecisionID = decision.ID
						}
					}
					s.Step = "approval"
					return c.save(ctx, e, s)
				}
			}
			return c.block(ctx, e, r, "Active plan differs from the persisted repository workflow")
		}
		g := guard(e, r, "plan")
		submission := workcontrol.PlanRevisionSubmission{IdempotencyKey: g.IdempotencyKey, AgentID: g.AgentID, ExpectedVersion: g.ExpectedVersion, FenceToken: g.FenceToken,
			Plan:          fmt.Sprintf("Implement the bounded task at `%s` in `%s`.\n\nAllowed paths: %s.\n\nRun the configured checks in a separate clean Kubernetes Job at the published commit. Open one PR against `%s`; stop for human review. Failed checks or requested review changes block this task.\n\nModel destination: %s (%s). No merge or deployment is authorized.", s.Base, c.Config.Git.Repository, strings.Join(c.Config.AllowedPaths, ", "), c.Config.Git.BaseBranch, c.Config.Model.Endpoint, c.Config.Model.Name),
			ContextDigest: c.contextDigest(task, s), RiskClass: "medium", VerificationStrategy: "Independent clean checkout; all configured local and required CI checks must pass for the PR head", ExpectedSideEffects: []string{"create scoped Git branch", "publish commit", "open pull request"}, RollbackApproach: "Close the PR and delete its branch; the base branch is not changed", PolicyVersion: c.Config.PolicyVersion, RequireApproval: c.Config.RequireApproval, DecisionExpiresAt: &e.DeadlineAt}
		if s.Analysis != nil {
			submission.Plan = fmt.Sprintf("Analyze the frozen synthetic diagnostic fixture %s revision %d using LangChain create_agent on LangGraph. Profile: %s. Inputs: %s. Model destination: %s (%s). Return a structured report with source quotes; no repository or mutation authority is granted.", s.Analysis.Fixture.ID, s.Analysis.Fixture.Revision, s.ProfileDigest, s.Base, c.Config.Model.Endpoint, c.Config.Model.Name)
			submission.RiskClass = "low"
			submission.SourceRevision = s.Base
			submission.VerificationStrategy = "Independent deterministic validation of required findings and source quotes, bound to input/profile/output digests"
			submission.ExpectedSideEffects = []string{"bounded model inference", "store diagnostic report and validation evidence"}
			submission.RollbackApproach = "Cancel the worker; no source or infrastructure mutations are performed"
		}
		p, err := c.Provider.SubmitPlanRevision(ctx, r.ID, submission)
		if err != nil {
			return err
		}
		s.PlanID = p.PlanRevision.ID
		if p.Decision != nil {
			s.DecisionID = p.Decision.ID
		}
		s.Step = "approval"
		return c.save(ctx, e, s)
	}
	if s.Step == "approval" {
		if s.DecisionID != "" {
			d, err := c.Provider.GetDecision(ctx, s.DecisionID)
			if err != nil {
				return err
			}
			if d.Status == workcontrol.DecisionPending {
				return nil
			}
			if d.Status != workcontrol.DecisionAnswered || d.Answer != "Approve plan" {
				return c.block(ctx, e, r, "Plan was not approved; create a revised task")
			}
		}
		if r.Status != workcontrol.RunRunning {
			if err = c.Provider.StartRun(ctx, r.ID, guard(e, r, "start")); err != nil {
				return err
			}
		}
		if e.State != execution.StateRunning {
			if err = c.transition(ctx, e, execution.StateRunning, "Approved workload started"); err != nil {
				return err
			}
		}
		s.Step = "implement"
		if s.Analysis != nil {
			s.Step = "analyze"
		}
		return c.save(ctx, e, s)
	}
	if s.Step == "implement" || s.Step == "verify" || s.Step == "analyze" {
		stage := s.Step
		attempt, done, result, digest, err := c.worker(ctx, e, task, s, stage)
		if err != nil {
			if errors.Is(err, errInvalidWorkerResult) {
				return c.block(ctx, e, r, err.Error())
			}
			return err
		}
		if !done {
			return nil
		}
		if attempt.State != runtimeapi.StateSucceeded {
			return c.block(ctx, e, r, stage+" worker failed; inspect the Kubernetes Job and create a revised task")
		}
		if stage == "analyze" {
			s.Implementer = attempt.ID
			s.ImplementationDigest = digest
			s.Step = "analysis_verify"
		} else if stage == "implement" {
			s.Implementer = attempt.ID
			s.ImplementationDigest = digest
			s.Step = "branch"
		} else {
			s.Verifier = attempt.ID
			s.VerificationDigest = digest
			for _, check := range result.Checks {
				if check.ExitCode != 0 {
					s.Reason = "Independent verification failed: " + check.ID + ". Result: " + c.artifactURL(digest)
					return c.save(ctx, e, s)
				}
			}
			s.Step = "pull_request"
		}
		return c.save(ctx, e, s)
	}
	if s.Analysis != nil {
		return c.advanceAnalysisResult(ctx, e, r, s)
	}
	if s.Step == "branch" {
		source, err := c.Source(e)
		if err != nil {
			return err
		}
		if _, err = source.CreateBranch(ctx, sourcecontrol.BranchRequest{Repository: c.Config.Git.Repository, Name: s.Branch, BaseRevision: s.Base, IdempotencyKey: e.ID + ":branch", AuthorityFence: e.FenceToken}); err != nil {
			return err
		}
		s.Step = "publish"
		return c.save(ctx, e, s)
	}
	if s.Step == "publish" {
		b, err := c.Artifacts.Get(s.ImplementationDigest)
		if err != nil {
			return err
		}
		var result worker.Result
		if err = json.Unmarshal(b, &result); err != nil {
			return err
		}
		source, err := c.Source(e)
		if err != nil {
			return err
		}
		changes := make([]sourcecontrol.Change, 0, len(result.Changes))
		for _, change := range result.Changes {
			if !worker.AllowedPath(c.Config.AllowedPaths, change.Path) {
				return c.block(ctx, e, r, "Worker output exceeds configured file scope")
			}
			digest, err := c.Artifacts.Put(change.Content)
			if err != nil {
				return err
			}
			op := sourcecontrol.ChangeUpdate
			if change.Delete {
				op = sourcecontrol.ChangeDelete
			}
			changes = append(changes, sourcecontrol.Change{Path: change.Path, ContentDigest: digest, Mode: change.Mode, Operation: op})
		}
		revision, err := source.WriteChanges(ctx, sourcecontrol.WriteRequest{Repository: c.Config.Git.Repository, Branch: s.Branch, ExpectedHead: s.Base, IdempotencyKey: e.ID + ":commit", AuthorityFence: e.FenceToken, Changes: changes})
		if err != nil {
			return err
		}
		s.Head = revision.Commit
		s.Step = "verify"
		return c.save(ctx, e, s)
	}
	if s.Step == "pull_request" {
		source, err := c.Source(e)
		if err != nil {
			return err
		}
		pr, err := source.CreatePullRequest(ctx, sourcecontrol.PullRequestRequest{Repository: c.Config.Git.Repository, BaseRef: c.Config.Git.BaseBranch, HeadRef: s.Branch, ExpectedHead: s.Head, Title: task.Title, Body: fmt.Sprintf("Implements task %s.\n\n%s\n\nIndependent checks passed at `%s`. Verification artifact digest: `%s`.\n\nReview the diff and required CI before merging. No merge or deployment was performed.", task.ID, task.AcceptanceCriteria, s.Head, s.VerificationDigest), IdempotencyKey: e.ID + ":pr", AuthorityFence: e.FenceToken})
		if err != nil {
			return err
		}
		if pr.State != "open" || pr.HeadRevision != s.Head {
			return c.block(ctx, e, r, "Recovered PR is closed or its head changed; human review is required")
		}
		s.PR = pr
		s.Step = "ci"
		return c.save(ctx, e, s)
	}
	if s.Step == "ci" || s.Step == "evidence" || s.Step == "complete" {
		source, err := c.Source(e)
		if err != nil {
			return err
		}
		head, err := source.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: c.Config.Git.Repository, Ref: s.Branch})
		if err != nil {
			return err
		}
		if head.Commit != s.Head {
			return c.block(ctx, e, r, "PR head changed after verification; existing evidence cannot complete this run")
		}
		request := sourcecontrol.ObservationRequest{Repository: c.Config.Git.Repository, PullRequestID: s.PR.ID, HeadRevision: s.Head}
		pr, err := source.GetPullRequest(ctx, request)
		if err != nil {
			return err
		}
		if pr.State != "open" || pr.HeadRevision != s.Head {
			return c.block(ctx, e, r, "PR was closed or changed after verification; human review is required: "+s.PR.URL)
		}
		checks, err := source.ObserveChecks(ctx, request)
		if err != nil {
			return err
		}
		for _, required := range c.Config.RequiredCI {
			found := false
			for _, check := range checks {
				if check.ID == required && check.HeadRevision == s.Head {
					found = true
					if check.State == "failure" || check.State == "failed" || check.State == "cancelled" || check.State == "timed_out" || check.State == "action_required" {
						return c.block(ctx, e, r, "Required CI failed: "+required+". PR: "+s.PR.URL)
					}
					if check.State != "success" {
						return nil
					}
				}
			}
			if !found {
				return nil
			}
		}
		reviews, err := source.ObserveReviews(ctx, request)
		if err != nil {
			return err
		}
		for _, review := range reviews {
			if review.HeadRevision == s.Head && review.State == "CHANGES_REQUESTED" {
				return c.block(ctx, e, r, "PR review requests changes. Create a focused follow-up task: "+s.PR.URL)
			}
		}
		if s.Step == "ci" {
			if r.Status != workcontrol.RunVerifying {
				if err = c.Provider.BeginVerification(ctx, r.ID, guard(e, r, "verify")); err != nil {
					return err
				}
			}
			if e.State != execution.StateVerifying {
				if err = c.transition(ctx, e, execution.StateVerifying, "Independent checks passed; admitting evidence"); err != nil {
					return err
				}
			}
			s.Step = "evidence"
			return c.save(ctx, e, s)
		}
		if s.Step == "evidence" {
			return c.evidence(ctx, e, r, s)
		}
		if err = c.Provider.CompleteRun(ctx, r.ID, guard(e, r, "complete"), "Verified PR ready for human review: "+s.PR.URL+" (commit "+s.Head+")", s.EvidenceIDs); err != nil {
			return err
		}
		return c.transition(ctx, e, execution.StateCompleted, "Verified pull request ready for human review")
	}
	return fmt.Errorf("unknown persisted workflow step %q", s.Step)
}

func (c *Controller) contextDigest(task workcontrol.WorkItem, s state) string {
	if s.ContextDigest != "" {
		return s.ContextDigest
	}
	b, _ := json.Marshal(struct {
		Task         workcontrol.WorkItem
		Base, Config string
	}{task, s.Base, s.ConfigDigest})
	return artifacts.Digest(b)
}
func (c *Controller) artifactURL(digest string) string {
	return strings.TrimRight(c.Config.PublicURL, "/") + "/artifacts/" + digest
}

func (c *Controller) transition(ctx context.Context, e execution.Execution, target execution.State, message string) error {
	e, err := c.Journal.GetExecution(ctx, e.ID)
	if err != nil {
		return err
	}
	if e.State == target {
		return nil
	}
	_, err = c.Journal.Transition(ctx, e.ID, execution.Transition{ExpectedVersion: e.Version, Target: target, Message: message})
	return err
}

func (c *Controller) block(ctx context.Context, e execution.Execution, r workcontrol.Run, reason string) error {
	if err := c.stop(ctx, e); err != nil {
		return err
	}
	g := guard(e, r, "block")
	if err := c.Provider.BlockRun(ctx, r.ID, workcontrol.RunBlock{IdempotencyKey: g.IdempotencyKey, AgentID: g.AgentID, ExpectedVersion: g.ExpectedVersion, FenceToken: g.FenceToken, Reason: reason}); err != nil {
		return err
	}
	e, err := c.Journal.GetExecution(ctx, e.ID)
	if err != nil {
		return err
	}
	_, err = c.Journal.Transition(ctx, e.ID, execution.Transition{ExpectedVersion: e.Version, Target: execution.StateBlocked, Message: reason, FailureClass: execution.FailureExternal, FailureMessage: reason})
	return err
}

func (c *Controller) evidence(ctx context.Context, e execution.Execution, r workcontrol.Run, s state) error {
	b, err := c.Artifacts.Get(s.VerificationDigest)
	if err != nil {
		return err
	}
	var result worker.Result
	if err = json.Unmarshal(b, &result); err != nil {
		return err
	}
	index := len(s.EvidenceIDs)
	if index >= len(result.Checks) {
		s.Step = "complete"
		return c.save(ctx, e, s)
	}
	check := result.Checks[index]
	id := fmt.Sprintf("%s:check:%d", e.ID, index)
	outcome := "passed"
	if check.ExitCode != 0 {
		outcome = "failed"
	}
	_, err = c.Provider.RecordEvidence(ctx, r.ID, guard(e, r, fmt.Sprintf("evidence:%d", index)), workcontrol.EvidenceSubmission{ID: id, PlanRevisionID: s.PlanID, Type: "test", Producer: s.Implementer, Verifier: s.Verifier, SubjectDigest: s.Head, SourceRevision: s.Head, Command: strings.Join(check.Command, " "), Outcome: outcome, ProvenanceURI: c.artifactURL(s.VerificationDigest), ProvenanceDigest: s.VerificationDigest, ObservedAt: result.ObservedAt, ExpiresAt: result.ObservedAt.Add(24 * time.Hour), ArtifactURI: c.artifactURL(s.VerificationDigest), ArtifactDigest: s.VerificationDigest, PolicyVersion: c.Config.PolicyVersion})
	if err != nil {
		return err
	}
	e, err = c.Journal.GetExecution(ctx, e.ID)
	if err != nil {
		return err
	}
	_, _, _, err = c.Journal.AddEvidence(ctx, e.ID, e.Version, execution.EvidenceInput{Key: id, Kind: "test", SourceRevision: s.Head, Environment: "independent Kubernetes Job " + s.Verifier, Command: strings.Join(check.Command, " "), Summary: outcome, URI: c.artifactURL(s.VerificationDigest), ObservedAt: result.ObservedAt})
	if err != nil {
		return err
	}
	s.EvidenceIDs = append(s.EvidenceIDs, id)
	return c.save(ctx, e, s)
}
