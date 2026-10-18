package platformapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/flow"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kubeapi"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

const (
	apiVersion = group + "/" + version
	finalizer  = "marina.dev/cancel"
	// creatorAnnotation records the authenticated creator of an HTTP request.
	creatorAnnotation = "marina.dev/created-by"
)

// Controller reconciles marina.dev resources with Marina's ledger: it accepts
// Projects, Runs and Turns, applies cancellation, publishes Decisions and
// mirrors journal state into status. Resources are the request record; the
// journal is the execution record (decision 0033).
type Controller struct {
	Kube     *kubeapi.Client
	Journal  *sqlite.Store
	Platform *sqlite.Platform
	// Projects are the installed execution profiles, by project ID
	// ("namespace.project").
	Projects map[string]flow.Config
	AgentID  string
	Now      func() time.Time
}

func (c *Controller) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().UTC()
}

// Cycle runs one reconciliation pass and returns the problems it deferred.
func (c *Controller) Cycle(ctx context.Context) []error {
	var problems []error
	add := func(err error) {
		if err != nil {
			problems = append(problems, err)
		}
	}
	projects, err := list[Project](ctx, c.Kube, "projects")
	add(err)
	workloads, err := list[Workload](ctx, c.Kube, "workloads")
	add(err)
	for i := range projects {
		add(c.project(ctx, &projects[i]))
	}
	for i := range workloads {
		add(c.workload(ctx, &workloads[i]))
	}
	if runs, err := list[Run](ctx, c.Kube, "runs"); err != nil {
		add(err)
	} else {
		for i := range runs {
			add(c.run(ctx, &runs[i], projects, workloads))
		}
	}
	if turns, err := list[Turn](ctx, c.Kube, "turns"); err != nil {
		add(err)
	} else {
		for i := range turns {
			add(c.turn(ctx, &turns[i], projects, workloads))
		}
	}
	add(c.decisions(ctx))
	return problems
}

func projectID(namespace, name string) string { return sqlite.PlatformWorkID(namespace, name) }

// --- projects and workloads ----------------------------------------------

func (c *Controller) project(ctx context.Context, p *Project) error {
	id := projectID(p.Metadata.Namespace, p.Metadata.Name)
	status := ProjectStatus{ObservedGeneration: p.Metadata.Generation}
	if _, installed := c.Projects[id]; !installed {
		status.Conditions = []Condition{condition("Ready", "False", "NotInstalled", "Marina has no execution profile for "+id, p.Metadata.Generation)}
		return c.putStatus(ctx, "projects", p.Metadata, p, &p.Status, &status)
	}
	repository, err := repositoryName(p.Spec.Repository.URL)
	if err != nil {
		status.Conditions = []Condition{condition("Ready", "False", "InvalidRepository", err.Error(), p.Metadata.Generation)}
		return c.putStatus(ctx, "projects", p.Metadata, p, &p.Status, &status)
	}
	var expected int64
	if binding, err := c.Journal.CurrentProjectBinding(ctx, id); err == nil {
		expected = binding.Version
	}
	if err := c.Platform.RequestRepository(ctx, p.Metadata.Namespace, p.Metadata.Name, id, p.Metadata.Generation,
		workcontrol.RepositoryRequestInput{ExpectedRevision: p.Metadata.Generation, ExpectedBindingVersion: expected, Repository: repository, BaseBranch: p.Spec.Repository.BaseBranch}); err != nil {
		return err
	}
	recorded, err := c.Platform.Project(ctx, id)
	if err != nil {
		return err
	}
	switch {
	case recorded.Result != nil && recorded.Result.Status == "rejected":
		status.Conditions = []Condition{condition("Ready", "False", "Rejected", recorded.Result.Reason, p.Metadata.Generation)}
	case recorded.Result != nil && recorded.Result.Binding != nil:
		status.BindingVersion = recorded.Result.Binding.Version
		status.Conditions = []Condition{condition("Ready", "True", "Bound", "Repository "+recorded.Result.Binding.Repository+" is bound", p.Metadata.Generation)}
	default:
		status.Conditions = []Condition{condition("Ready", "False", "Binding", "Marina is checking the repository", p.Metadata.Generation)}
	}
	return c.putStatus(ctx, "projects", p.Metadata, p, &p.Status, &status)
}

func repositoryName(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" {
		return "", errors.New("the repository must be an https://github.com/owner/name URL")
	}
	name := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	if strings.Count(name, "/") != 1 {
		return "", errors.New("the repository must be an https://github.com/owner/name URL")
	}
	return name, nil
}

func (c *Controller) workload(ctx context.Context, w *Workload) error {
	status := WorkloadStatus{ObservedGeneration: w.Metadata.Generation, Digest: digest(w.Spec)}
	if err := c.validWorkload(w); err != nil {
		status.Conditions = []Condition{condition("Accepted", "False", "Invalid", err.Error(), w.Metadata.Generation)}
	} else {
		status.Conditions = []Condition{condition("Accepted", "True", "Valid", "", w.Metadata.Generation)}
	}
	return c.putStatus(ctx, "workloads", w.Metadata, w, &w.Status, &status)
}

// validWorkload checks a Workload against every installed profile in its
// namespace: its models must be installed and its choices allowed.
func (c *Controller) validWorkload(w *Workload) error {
	matched := false
	for id, config := range c.Projects {
		if !strings.HasPrefix(id, w.Metadata.Namespace+".") {
			continue
		}
		matched = true
		switch w.Spec.Type {
		case "repository-change":
			choices, err := repositoryChoices(w.Spec.RepositoryChange, config)
			if err != nil {
				return err
			}
			if _, err := config.WithChoices(&choices); err != nil {
				return err
			}
		case "conversation":
			if w.Spec.Conversation == nil || w.Spec.Conversation.Model != config.Model.Name || config.Conversation == nil {
				return errors.New("conversation workloads need the installed chat model and an installed chat profile")
			}
		default:
			return errors.New("unknown workload type")
		}
	}
	if !matched {
		return errors.New("no installed project uses this namespace")
	}
	return nil
}

func repositoryChoices(r *RepositoryChange, config flow.Config) (workcontrol.FactoryChoices, error) {
	if r == nil {
		return workcontrol.FactoryChoices{}, errors.New("a repository-change workload needs repositoryChange")
	}
	if r.Model != config.Model.Name {
		return workcontrol.FactoryChoices{}, fmt.Errorf("model %q is not installed", r.Model)
	}
	review := r.Review == nil || r.Review.Enabled
	if review && r.Review != nil && r.Review.Model != "" && (config.ReviewerModel == nil || r.Review.Model != config.ReviewerModel.Name) {
		return workcontrol.FactoryChoices{}, fmt.Errorf("review model %q is not installed", r.Review.Model)
	}
	delivery := map[string]string{"land-on-main": "fast_forward", "pull-request": "pull_request"}[r.Delivery.Mode]
	choices := workcontrol.FactoryChoices{AllowedPaths: r.AllowedPaths, RequireApproval: r.PlanApproval == "required", Review: review,
		Delivery: delivery, RequiredCI: r.Delivery.RequiredChecks, RepairFailures: 3}
	if r.Repair != nil {
		choices.RepairFailures = r.Repair.MaxFailures
	}
	for _, check := range r.Checks {
		directory := check.Directory
		if directory == "" {
			directory = "."
		}
		choices.Checks = append(choices.Checks, workcontrol.FactoryCheck{ID: check.ID, Directory: directory, Command: check.Command})
	}
	if r.Limits != nil {
		choices.MaxTokens, choices.BudgetMinutes = r.Limits.MaxTokens, r.Limits.MaxTimeMinutes
	}
	return choices, nil
}

// --- runs ----------------------------------------------------------------

var runPhases = map[string]string{"ready": "Pending", "planning": "Planning", "waiting_decision": "WaitingDecision", "running": "Running",
	"verifying": "Verifying", "completed": "Succeeded", "blocked": "Blocked", "failed": "Failed", "cancelled": "Cancelled"}

func terminalPhase(phase string) bool {
	return phase == "Succeeded" || phase == "Blocked" || phase == "Failed" || phase == "Cancelled"
}

func (c *Controller) run(ctx context.Context, r *Run, projects []Project, workloads []Workload) error {
	id := sqlite.PlatformWorkID(r.Metadata.Namespace, r.Metadata.Name)
	work, err := c.Platform.Work(ctx, id)
	if err != nil && !errors.Is(err, execution.ErrNotFound) {
		return err
	}
	if errors.Is(err, execution.ErrNotFound) {
		if r.Metadata.DeletionTimestamp != "" {
			return c.dropFinalizer(ctx, "runs", r.Metadata)
		}
		input, reason := c.acceptRun(ctx, r, projects, workloads)
		if reason != "" {
			status := RunStatus{ObservedGeneration: r.Metadata.Generation, Phase: "Pending", Reason: reason,
				Conditions: []Condition{condition("Accepted", "False", "NotAccepted", reason, r.Metadata.Generation)}}
			return c.putStatus(ctx, "runs", r.Metadata, r, &r.Status, &status)
		}
		if err := c.addFinalizer(ctx, "runs", &r.Metadata); err != nil {
			return err
		}
		if work, _, err = c.Platform.SubmitWork(ctx, input); err != nil {
			return err
		}
	}
	if (r.Spec.Cancel || r.Metadata.DeletionTimestamp != "") && !work.CancelRequested {
		if err := c.Platform.RequestCancel(ctx, id, creator(r.Metadata), "cancel requested"); err != nil {
			return err
		}
		if work, err = c.Platform.Work(ctx, id); err != nil {
			return err
		}
	}
	status, err := c.runStatus(ctx, r, work)
	if err != nil {
		return err
	}
	if r.Metadata.DeletionTimestamp != "" {
		if terminalPhase(status.Phase) {
			return c.dropFinalizer(ctx, "runs", r.Metadata)
		}
		return nil
	}
	return c.putStatus(ctx, "runs", r.Metadata, r, &r.Status, &status)
}

func creator(meta ObjectMeta) string {
	if by := meta.Annotations[creatorAnnotation]; by != "" {
		return by
	}
	return "kubernetes"
}

// acceptRun maps a Run to a frozen work item, or explains why it must wait.
func (c *Controller) acceptRun(ctx context.Context, r *Run, projects []Project, workloads []Workload) (sqlite.NewPlatformWork, string) {
	project, ok := find(projects, r.Metadata.Namespace, r.Spec.Project, func(p Project) ObjectMeta { return p.Metadata })
	if !ok {
		return sqlite.NewPlatformWork{}, "Project " + r.Spec.Project + " does not exist"
	}
	id := projectID(project.Metadata.Namespace, project.Metadata.Name)
	config, installed := c.Projects[id]
	if !installed {
		return sqlite.NewPlatformWork{}, "Project " + r.Spec.Project + " is not installed"
	}
	binding, reason := c.boundRepository(ctx, id, project)
	if reason != "" {
		return sqlite.NewPlatformWork{}, reason
	}
	name := r.Spec.Workload
	if name == "" {
		name = project.Spec.DefaultWorkload
	}
	workload, ok := find(workloads, r.Metadata.Namespace, name, func(w Workload) ObjectMeta { return w.Metadata })
	if !ok || workload.Spec.Type != "repository-change" {
		return sqlite.NewPlatformWork{}, "Workload " + name + " is not a repository-change workload in this namespace"
	}
	choices, err := repositoryChoices(workload.Spec.RepositoryChange, config)
	if err == nil {
		_, err = config.WithChoices(&choices)
	}
	if err != nil {
		return sqlite.NewPlatformWork{}, "Workload " + name + " is not allowed: " + err.Error()
	}
	now := c.now()
	selection, resolved, err := frozenContext(id, r.Spec.Context, &binding, now)
	if err != nil {
		return sqlite.NewPlatformWork{}, err.Error()
	}
	authority := []string{"source.read", "source.write", "pull_request.create"}
	if choices.Delivery == "fast_forward" {
		authority = []string{"source.read", "source.write", "source.fast_forward"}
	}
	item := workcontrol.WorkItem{Title: r.Spec.Title, Description: r.Spec.Description, AcceptanceCriteria: r.Spec.AcceptanceCriteria, Priority: "normal",
		BudgetMinutes: r.Spec.Budget.TimeMinutes, Context: selection, Choices: &choices,
		Budget: workcontrol.Budget{TimeMinutes: r.Spec.Budget.TimeMinutes, MaxAttempts: r.Spec.Budget.MaxAttempts, MaxTokens: r.Spec.Budget.MaxTokens,
			MaxCostMinor: r.Spec.Budget.MaxCostMinor, Currency: r.Spec.Budget.Currency, MaxConcurrency: 1, Authority: authority}}
	if p := r.Spec.Publish; p != nil {
		if sha256Digest(p.Content) != p.Digest {
			return sqlite.NewPlatformWork{}, "The publish digest does not match its content"
		}
		document := workcontrol.PRDDocument{PRDDraft: workcontrol.PRDDraft{Title: r.Spec.Title, Path: p.Path, Content: p.Content}, MessageID: r.Metadata.UID,
			Repository: binding, ApprovalDigest: p.Digest, ApprovedBy: "operator:" + creator(r.Metadata), ApprovedAt: now.UnixMilli()}
		if err := document.Validate(); err != nil {
			return sqlite.NewPlatformWork{}, "Publishing supports one Markdown document under docs/prd/: " + err.Error()
		}
		document.Digest = document.ContentDigest()
		item.PRD = &document
	}
	return sqlite.NewPlatformWork{Namespace: r.Metadata.Namespace, Name: r.Metadata.Name, UID: r.Metadata.UID, ProjectID: id, Item: item,
		DependsOn: r.Spec.DependsOn, Context: resolved}, ""
}

// boundRepository returns the project's current binding when the Project
// resource's repository is the one Marina bound.
func (c *Controller) boundRepository(ctx context.Context, id string, project Project) (workcontrol.RepositoryBinding, string) {
	current, err := c.Journal.CurrentProjectBinding(ctx, id)
	if err != nil {
		return workcontrol.RepositoryBinding{}, "Project " + project.Metadata.Name + " has no repository binding yet"
	}
	binding, _, err := c.Journal.RepositorySnapshot(ctx, id, current.Version)
	if err != nil {
		return workcontrol.RepositoryBinding{}, "Project " + project.Metadata.Name + " has no repository binding yet"
	}
	name, _ := repositoryName(project.Spec.Repository.URL)
	if binding.Repository != name || binding.BaseBranch != project.Spec.Repository.BaseBranch {
		return workcontrol.RepositoryBinding{}, "Project " + project.Metadata.Name + " is still binding its repository"
	}
	return binding, ""
}

func (c *Controller) runStatus(ctx context.Context, r *Run, work sqlite.PlatformWork) (RunStatus, error) {
	status := RunStatus{ObservedGeneration: r.Metadata.Generation, Phase: runPhases[work.Status],
		Conditions: []Condition{condition("Accepted", "True", "Accepted", "", r.Metadata.Generation)}}
	if r.Status != nil && r.Status.ObservedGeneration > 0 && r.Metadata.Generation > r.Status.ObservedGeneration && !r.Spec.Cancel {
		status.Conditions = append(status.Conditions, condition("SpecChanged", "True", "Ignored", "Marina keeps the spec it accepted", r.Metadata.Generation))
	}
	detail, err := c.Platform.GetWork(ctx, work.ID)
	if err != nil {
		return status, err
	}
	status.StartedAt = rfc3339(work.CreatedAt)
	if len(detail.Runs) > 0 {
		last := detail.Runs[len(detail.Runs)-1]
		status.StartedAt = rfc3339(last.StartedAt)
		if last.FinishedAt != nil {
			status.FinishedAt = rfc3339(*last.FinishedAt)
		}
		if last.Status == workcontrol.RunBlocked || last.Status == workcontrol.RunFailed {
			status.Reason = last.ResultSummary
		}
	}
	executions, err := c.Journal.ExecutionsForWork(ctx, work.ProjectID, work.ID)
	if err != nil {
		return status, err
	}
	if len(executions) > 0 {
		e := executions[len(executions)-1]
		totals, err := c.Journal.ExecutionModelTotals(ctx, e.ID)
		if err != nil {
			return status, err
		}
		attempts, err := c.Journal.ListRuntimeAttempts(ctx, e.ID)
		if err != nil {
			return status, err
		}
		cost, reserved := totals.CostMicros/10000, totals.ReservedCostMicros/10000
		status.Usage = &Usage{Tokens: totals.Tokens, ReservedTokens: totals.ReservedTokens, CostMinor: &cost, ReservedCostMinor: &reserved,
			Currency: r.Spec.Budget.Currency, Attempts: len(attempts), MaxAttempts: e.MaxAttempts}
		if checkpoint, err := c.Journal.LatestPhaseCheckpoint(ctx, e.ID, "repository_flow"); err == nil {
			var flowState struct {
				Base, Head, Branch string
				PR                 struct {
					ID  string
					URL string
				}
			}
			encoded, _ := checkpoint.Payload["state"].(string)
			if json.Unmarshal([]byte(encoded), &flowState) == nil && flowState.Base != "" {
				status.Revision = &Revision{Base: flowState.Base, Head: flowState.Head, Branch: flowState.Branch}
				if flowState.PR.URL != "" {
					status.PullRequest = &PullRequest{URL: flowState.PR.URL}
				}
			}
		}
	}
	evidence, err := c.Platform.Evidence(ctx, work.ID)
	if err != nil {
		return status, err
	}
	kinds := map[string]string{"semantic_review": "review", "test": "verification", "ci": "ci", "landing": "landing"}
	for _, item := range evidence {
		kind, known := kinds[item.Type]
		if !known || len(item.SourceRevision) != 40 {
			continue
		}
		status.Evidence = append(status.Evidence, Evidence{ID: item.ID, Kind: kind, Passed: item.Outcome == "passed", SourceRevision: item.SourceRevision,
			ArtifactDigest: digestOrEmpty(item.ArtifactDigest), Summary: item.Type + " " + item.Outcome, RecordedAt: rfc3339(item.ObservedAt)})
	}
	if landing, err := c.Platform.Landing(ctx, work.ID); err == nil && landing != nil {
		status.Landing = &Landing{Commit: landing.Commit, ObservedAt: rfc3339(time.UnixMilli(landing.ObservedAt))}
	}
	decisions, err := c.Platform.ListDecisions(ctx, work.ID)
	if err != nil {
		return status, err
	}
	for _, d := range decisions {
		status.Decisions = append(status.Decisions, d.Name)
	}
	status.LastEventSequence, err = c.Platform.LastEventSequence(ctx, work.ID)
	return status, err
}

func digestOrEmpty(value string) string {
	if strings.HasPrefix(value, "sha256:") && len(value) == 71 {
		return value
	}
	return ""
}

// --- turns ---------------------------------------------------------------

func (c *Controller) turn(ctx context.Context, t *Turn, projects []Project, workloads []Workload) error {
	id := sqlite.PlatformWorkID(t.Metadata.Namespace, t.Metadata.Name)
	stored, err := c.Platform.Turn(ctx, id)
	if err != nil && !errors.Is(err, execution.ErrNotFound) {
		return err
	}
	if errors.Is(err, execution.ErrNotFound) {
		if t.Metadata.DeletionTimestamp != "" {
			return c.dropFinalizer(ctx, "turns", t.Metadata)
		}
		input, reason := c.acceptTurn(ctx, t, projects, workloads)
		if reason != "" {
			status := TurnStatus{ObservedGeneration: t.Metadata.Generation, Phase: "Pending", Reason: reason,
				Conditions: []Condition{condition("Accepted", "False", "NotAccepted", reason, t.Metadata.Generation)}}
			return c.putStatus(ctx, "turns", t.Metadata, t, &t.Status, &status)
		}
		if err := c.addFinalizer(ctx, "turns", &t.Metadata); err != nil {
			return err
		}
		if stored, _, err = c.Platform.SubmitTurn(ctx, input); err != nil {
			return err
		}
	}
	if (t.Spec.Cancel || t.Metadata.DeletionTimestamp != "") && !stored.CancelRequested {
		if err := c.Platform.RequestTurnCancel(ctx, id, creator(t.Metadata)); err != nil {
			return err
		}
	}
	status, err := c.turnStatus(ctx, t, stored)
	if err != nil {
		return err
	}
	if t.Metadata.DeletionTimestamp != "" {
		if status.Phase == "Succeeded" || status.Phase == "Failed" || status.Phase == "Cancelled" {
			return c.dropFinalizer(ctx, "turns", t.Metadata)
		}
		return nil
	}
	return c.putStatus(ctx, "turns", t.Metadata, t, &t.Status, &status)
}

func (c *Controller) acceptTurn(ctx context.Context, t *Turn, projects []Project, workloads []Workload) (sqlite.NewPlatformTurn, string) {
	project, ok := find(projects, t.Metadata.Namespace, t.Spec.Project, func(p Project) ObjectMeta { return p.Metadata })
	if !ok {
		return sqlite.NewPlatformTurn{}, "Project " + t.Spec.Project + " does not exist"
	}
	id := projectID(project.Metadata.Namespace, project.Metadata.Name)
	config, installed := c.Projects[id]
	if !installed || config.Conversation == nil {
		return sqlite.NewPlatformTurn{}, "Project " + t.Spec.Project + " has no installed chat profile"
	}
	repositoryTools := true
	if t.Spec.Workload != "" {
		workload, ok := find(workloads, t.Metadata.Namespace, t.Spec.Workload, func(w Workload) ObjectMeta { return w.Metadata })
		if !ok || workload.Spec.Type != "conversation" || workload.Spec.Conversation == nil {
			return sqlite.NewPlatformTurn{}, "Workload " + t.Spec.Workload + " is not a conversation workload in this namespace"
		}
		if workload.Spec.Conversation.RepositoryTools != nil {
			repositoryTools = *workload.Spec.Conversation.RepositoryTools
		}
	}
	var binding *workcontrol.RepositoryBinding
	if repositoryTools && config.RepositoryEnabled() {
		bound, reason := c.boundRepository(ctx, id, project)
		if reason != "" {
			return sqlite.NewPlatformTurn{}, reason
		}
		binding = &bound
	}
	_, resolved, err := frozenContext(id, t.Spec.Context, binding, c.now())
	if err != nil {
		return sqlite.NewPlatformTurn{}, err.Error()
	}
	messages := make([]sqlite.TurnMessage, 0, len(t.Spec.Thread.Messages))
	for _, m := range t.Spec.Thread.Messages {
		messages = append(messages, sqlite.TurnMessage{ID: m.ID, Role: m.Role, Body: m.Body})
	}
	return sqlite.NewPlatformTurn{Namespace: t.Metadata.Namespace, Name: t.Metadata.Name, UID: t.Metadata.UID, ProjectID: id, AgentID: c.AgentID,
		ThreadID: t.Spec.Thread.ID, Messages: messages, Context: resolved}, ""
}

func (c *Controller) turnStatus(ctx context.Context, t *Turn, stored sqlite.PlatformTurn) (TurnStatus, error) {
	status := TurnStatus{ObservedGeneration: t.Metadata.Generation, Phase: "Pending", StartedAt: rfc3339(stored.CreatedAt),
		Conditions: []Condition{condition("Accepted", "True", "Accepted", "", t.Metadata.Generation)}}
	if journaled, err := c.Journal.GetConversationTurn(ctx, stored.ID); err == nil {
		status.Phase = "Running"
		status.Usage = &Usage{Tokens: journaled.UsedTokens, ReservedTokens: journaled.ReservedTokens}
		if journaled.Status == "blocked" {
			status.Phase, status.Reason = "Failed", journaled.Reason
		}
	}
	decisions, err := c.Platform.ListDecisions(ctx, stored.ID)
	if err != nil {
		return status, err
	}
	for _, d := range decisions {
		status.Decisions = append(status.Decisions, d.Name)
		if d.Decision.Status == workcontrol.DecisionPending && (status.Phase == "Running" || status.Phase == "Pending") {
			status.Phase = "WaitingDecision"
		}
	}

	if o := stored.Outcome; o != nil {
		status.Phase, status.FinishedAt = "Succeeded", rfc3339(stored.UpdatedAt)
		if stored.Status == "failed" {
			status.Phase = "Failed"
		}
		status.Reply = &struct {
			Answer string `json:"answer"`
		}{Answer: o.Body}
		proposals := Proposals{}
		if o.Task != nil {
			proposals.Task = &TaskProposal{Title: o.Task.Title, Description: o.Task.Description, AcceptanceCriteria: o.Task.AcceptanceCriteria}
		}
		if o.PRD != nil {
			proposals.PRD = &PRDProposal{Path: o.PRD.Path, Title: o.PRD.Title, Content: o.PRD.Content, Digest: sha256Digest(o.PRD.Content)}
		}
		if o.PRDTasks != nil {
			tasks := PRDTasksProposal{Source: o.PRDTasks.PublicationTaskID}
			for _, task := range o.PRDTasks.Tasks {
				tasks.Tasks = append(tasks.Tasks, TaskProposal{Title: task.Title, Description: task.Description, AcceptanceCriteria: task.AcceptanceCriteria})
			}
			proposals.PRDTasks = &tasks
		}
		if proposals.Task != nil || proposals.PRD != nil || proposals.PRDTasks != nil {
			status.Proposals = &proposals
		}
	}
	if stored.CancelRequested {
		status.Phase = "Cancelled"
	}
	status.LastEventSequence, err = c.Platform.LastEventSequence(ctx, stored.ID)
	return status, err
}

// --- decisions -----------------------------------------------------------

// decisions publishes every ledger Decision as a resource, applies answers
// given on the resource, and mirrors the ledger state into status.
func (c *Controller) decisions(ctx context.Context) error {
	recorded, err := c.Platform.AllDecisions(ctx, 1000)
	if err != nil {
		return err
	}
	resources, err := list[Decision](ctx, c.Kube, "decisions")
	if err != nil {
		return err
	}
	byName := map[string]*Decision{}
	for i := range resources {
		byName[resources[i].Metadata.Namespace+"/"+resources[i].Metadata.Name] = &resources[i]
	}
	var problems []error
	for _, d := range recorded {
		resource, exists := byName[d.Namespace+"/"+d.Name]
		if !exists {
			created := decisionResource(d)
			if err := c.Kube.Do(ctx, http.MethodPost, "/apis/"+apiVersion+"/namespaces/"+d.Namespace+"/decisions", "", created, nil); err != nil && !kubeapi.IsStatus(err, http.StatusConflict) {
				problems = append(problems, err)
			}
			continue
		}
		if resource.Spec.Response != nil && d.Decision.Status == workcontrol.DecisionPending {
			by := resource.Spec.Response.AnsweredBy
			if by == "" {
				by = "kubernetes"
			}
			if answered, err := c.Platform.AnswerDecision(ctx, d.Namespace, d.Name, resource.Spec.Response.Answer, by); err == nil {
				d = answered
			} else {
				d.Decision.Status = workcontrol.DecisionStatus("rejected")
			}
		}
		phase := map[workcontrol.DecisionStatus]string{workcontrol.DecisionPending: "Open", workcontrol.DecisionAnswered: "Answered", workcontrol.DecisionExpired: "Expired"}[d.Decision.Status]
		if phase == "" {
			phase = "Closed"
		}
		status := DecisionStatus{ObservedGeneration: resource.Metadata.Generation, Phase: phase}
		if d.Decision.Status == "rejected" {
			status.Phase = "Open"
			status.Conditions = []Condition{condition("AnswerAccepted", "False", "Rejected", "The answer is not one of the options or came too late", resource.Metadata.Generation)}
		}
		if err := c.putStatus(ctx, "decisions", resource.Metadata, resource, &resource.Status, &status); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

func decisionResource(d sqlite.PlatformDecision) Decision {
	resource := Decision{APIVersion: apiVersion, Kind: "Decision", Metadata: ObjectMeta{Name: d.Name, Namespace: d.Namespace}}
	resource.Spec.Owner.Kind, resource.Spec.Owner.Name = d.OwnerKind, strings.TrimPrefix(d.OwnerID, d.Namespace+".")
	resource.Spec.Kind = "clarification"
	if d.Decision.PlanRevisionID != "" {
		resource.Spec.Kind = "plan-approval"
	}
	resource.Spec.Question, resource.Spec.Options, resource.Spec.Recommendation = d.Decision.Question, d.Decision.Options, d.Decision.Recommendation
	resource.Spec.Details = d.Decision.EvidenceSummary
	if d.Decision.ExpiresAt != nil {
		resource.Spec.ExpiresAt = rfc3339(*d.Decision.ExpiresAt)
	}
	return resource
}

// --- resource access -----------------------------------------------------

func list[T any](ctx context.Context, kube *kubeapi.Client, plural string) ([]T, error) {
	var page struct {
		Items []T `json:"items"`
	}
	if err := kube.Do(ctx, http.MethodGet, "/apis/"+apiVersion+"/"+plural, "", nil, &page); err != nil {
		return nil, fmt.Errorf("list %s: %w", plural, err)
	}
	return page.Items, nil
}

func find[T any](items []T, namespace, name string, meta func(T) ObjectMeta) (T, bool) {
	for _, item := range items {
		if m := meta(item); m.Namespace == namespace && m.Name == name {
			return item, true
		}
	}
	var zero T
	return zero, false
}

// putStatus writes status only when it changed, so an idle cycle makes no
// writes. A conflicting concurrent edit is retried on the next cycle.
func putStatus[O any, S any](ctx context.Context, c *Controller, plural string, meta ObjectMeta, object *O, current **S, next *S) error {
	if *current != nil && reflect.DeepEqual(withoutTimes(**current), withoutTimes(*next)) {
		return nil
	}
	*current = next
	err := c.Kube.Do(ctx, http.MethodPut, "/apis/"+apiVersion+"/namespaces/"+meta.Namespace+"/"+plural+"/"+meta.Name+"/status", "", object, nil)
	if kubeapi.IsStatus(err, http.StatusConflict) || kubeapi.IsStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

func (c *Controller) putStatus(ctx context.Context, plural string, meta ObjectMeta, object any, current any, next any) error {
	switch o := object.(type) {
	case *Project:
		return putStatus(ctx, c, plural, meta, o, current.(**ProjectStatus), next.(*ProjectStatus))
	case *Workload:
		return putStatus(ctx, c, plural, meta, o, current.(**WorkloadStatus), next.(*WorkloadStatus))
	case *Run:
		return putStatus(ctx, c, plural, meta, o, current.(**RunStatus), next.(*RunStatus))
	case *Turn:
		return putStatus(ctx, c, plural, meta, o, current.(**TurnStatus), next.(*TurnStatus))
	case *Decision:
		return putStatus(ctx, c, plural, meta, o, current.(**DecisionStatus), next.(*DecisionStatus))
	}
	return errors.New("unknown resource")
}

// withoutTimes compares status ignoring condition transition times.
func withoutTimes(value any) any {
	encoded, _ := json.Marshal(value)
	var generic map[string]any
	_ = json.Unmarshal(encoded, &generic)
	if conditions, ok := generic["conditions"].([]any); ok {
		for _, item := range conditions {
			if m, ok := item.(map[string]any); ok {
				delete(m, "lastTransitionTime")
			}
		}
	}
	return generic
}

// addFinalizer makes deletion wait for Marina to cancel and finish the work.
// It updates meta to the patched resource version for the status write.
func (c *Controller) addFinalizer(ctx context.Context, plural string, meta *ObjectMeta) error {
	for _, f := range meta.Finalizers {
		if f == finalizer {
			return nil
		}
	}
	patch := map[string]any{"metadata": map[string]any{"finalizers": append(append([]string{}, meta.Finalizers...), finalizer), "resourceVersion": meta.ResourceVersion}}
	var patched struct {
		Metadata ObjectMeta `json:"metadata"`
	}
	if err := c.Kube.Do(ctx, http.MethodPatch, "/apis/"+apiVersion+"/namespaces/"+meta.Namespace+"/"+plural+"/"+meta.Name, "application/merge-patch+json", patch, &patched); err != nil {
		return err
	}
	meta.ResourceVersion, meta.Finalizers = patched.Metadata.ResourceVersion, patched.Metadata.Finalizers
	return nil
}

func (c *Controller) dropFinalizer(ctx context.Context, plural string, meta ObjectMeta) error {
	kept := []string{}
	for _, f := range meta.Finalizers {
		if f != finalizer {
			kept = append(kept, f)
		}
	}
	if len(kept) == len(meta.Finalizers) {
		return nil
	}
	patch := map[string]any{"metadata": map[string]any{"finalizers": kept, "resourceVersion": meta.ResourceVersion}}
	err := c.Kube.Do(ctx, http.MethodPatch, "/apis/"+apiVersion+"/namespaces/"+meta.Namespace+"/"+plural+"/"+meta.Name, "application/merge-patch+json", patch, nil)
	if kubeapi.IsStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

func condition(kind, status, reason, message string, generation int64) Condition {
	return Condition{Type: kind, Status: status, Reason: reason, Message: message, ObservedGeneration: generation, LastTransitionTime: rfc3339(time.Now())}
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func digest(value any) string {
	encoded, _ := json.Marshal(value)
	return sha256Digest(string(encoded))
}
