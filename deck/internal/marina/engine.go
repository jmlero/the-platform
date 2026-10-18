package marina

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/store"
)

const (
	defaultTick      = time.Second
	projectSyncEvery = 30 * time.Second
	catalogLifetime  = 4 * time.Minute
	maxStepsPerCycle = 12
)

// Engine submits Deck's ready work to Marina and mirrors Marina's status into
// Deck's ledger. Deck calls Marina; Marina never calls Deck.
type Engine struct {
	Store        *store.Store
	Client       *Client
	ProjectsFile string
	Log          *slog.Logger
	Tick         time.Duration

	agentProjects string
	synced        map[string]time.Time
	turnBudgets   map[string]TurnBudget
	profiles      map[string]map[string]bool
	problems      map[string]string
}

// Run cycles until ctx ends.
func (e *Engine) Run(ctx context.Context) {
	tick := e.Tick
	if tick <= 0 {
		tick = defaultTick
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		if err := e.Cycle(ctx); err != nil && ctx.Err() == nil {
			e.Log.Warn("Marina cycle deferred", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Cycle runs one pass: project sync, dispatch, and mirroring of Runs and Turns.
// Every step is replay-safe, so a crash at any point is repaired by the next.
func (e *Engine) Cycle(ctx context.Context) error {
	mappings, err := LoadProjects(e.ProjectsFile)
	if err != nil {
		return err
	}
	if e.synced == nil {
		e.synced, e.turnBudgets, e.profiles = map[string]time.Time{}, map[string]TurnBudget{}, map[string]map[string]bool{}
	}
	e.problems = map[string]string{}
	ids := make([]string, 0, len(mappings))
	for _, m := range mappings {
		ids = append(ids, m.DeckProject)
	}
	sort.Strings(ids)
	if key := strings.Join(ids, ","); key != e.agentProjects {
		if err := e.Store.EnsureMarinaAgent(ctx, ids); err != nil {
			return err
		}
		e.agentProjects = key
	}
	for _, m := range mappings {
		if time.Since(e.synced[m.DeckProject]) >= projectSyncEvery {
			if err := e.syncProject(ctx, m); err != nil {
				e.problem(m.DeckProject, "sync project", err)
			} else {
				e.synced[m.DeckProject] = time.Now()
			}
		}
	}
	e.dispatchRuns(ctx, mappings)
	e.mirrorRuns(ctx)
	e.dispatchTurns(ctx, mappings)
	e.mirrorTurns(ctx)
	for _, m := range mappings {
		if err := e.Store.RecordMarinaContact(ctx, m.DeckProject, e.problems[m.DeckProject]); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// problem logs the detail and keeps a safe summary for the provider status.
func (e *Engine) problem(project, action string, err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	e.Log.Warn("Marina "+action, "project", project, "error", err)
	if _, seen := e.problems[project]; seen {
		return
	}
	var status *APIError
	switch {
	case errors.As(err, &status):
		reason := status.Reason
		if reason == "" {
			reason = "HTTP " + strconv.Itoa(status.Code)
		}
		e.problems[project] = "Marina refused to " + action + " (" + reason + ")"
	case strings.HasPrefix(err.Error(), "call Marina"):
		e.problems[project] = "Marina is unreachable"
	default:
		e.problems[project] = "Deck could not " + action
	}
}

// syncProject copies what the UI needs from Marina: the bound repository and
// the workload profiles new tasks may select.
func (e *Engine) syncProject(ctx context.Context, m ProjectMapping) error {
	project, err := e.Client.Project(ctx, m.Namespace, m.Project)
	if err != nil {
		return err
	}
	if project.Ready() {
		if binding, ok := repositoryBinding(project); ok {
			if err := e.Store.SetRepositoryBinding(ctx, m.DeckProject, binding); err != nil {
				return err
			}
		}
	}
	capabilities, err := e.Client.Capabilities(ctx)
	if err != nil {
		return err
	}
	budget := TurnBudget{TimeMinutes: 15, MaxTokens: 40000}
	if m.ConversationWorkload != "" {
		workload, err := e.Client.Workload(ctx, m.Namespace, m.ConversationWorkload)
		if err != nil {
			return err
		}
		if c := workload.Spec.Conversation; c != nil && c.Limits != nil {
			if c.Limits.MaxTimeMinutes > 0 && c.Limits.MaxTimeMinutes <= 60 {
				budget.TimeMinutes = c.Limits.MaxTimeMinutes
			}
			if c.Limits.MaxTokens > 0 {
				budget.MaxTokens = c.Limits.MaxTokens
			}
		}
	}
	e.turnBudgets[m.DeckProject] = budget
	profiles := []domain.WorkloadProfile{}
	matched := map[string]bool{}
	names := make([]string, 0, len(m.Workloads))
	for profile := range m.Workloads {
		names = append(names, profile)
	}
	sort.Strings(names)
	for _, id := range names {
		workload, err := e.Client.Workload(ctx, m.Namespace, m.Workloads[id])
		if err != nil {
			return err
		}
		if profile, ok := workloadProfile(id, workload, capabilities.Limits); ok {
			profiles = append(profiles, profile)
			matched[id] = true
		} else {
			e.Log.Warn("Marina workload does not match its Deck profile", "project", m.DeckProject, "profile", id, "workload", m.Workloads[id])
		}
	}
	e.profiles[m.DeckProject] = matched
	return e.Store.PublishWorkloads(ctx, m.DeckProject, store.MarinaAgentID, domain.WorkloadCatalog{Profiles: profiles, ExpiresAt: time.Now().Add(catalogLifetime).UnixMilli()})
}

func repositoryBinding(project Project) (domain.RepositoryBinding, bool) {
	parsed, err := url.Parse(project.Spec.Repository.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return domain.RepositoryBinding{}, false
	}
	name := strings.TrimSuffix(strings.Trim(parsed.Path, "/"), ".git")
	if strings.Count(name, "/") != 1 {
		return domain.RepositoryBinding{}, false
	}
	version := project.Status.BindingVersion
	if version < 1 {
		version = 1
	}
	return domain.RepositoryBinding{Version: version, Repository: name, CloneURL: project.Spec.Repository.URL, BaseBranch: project.Spec.Repository.BaseBranch}, true
}

// workloadProfile projects a repository-change Workload onto the Deck profile
// it is mapped to; the delivery mode must match the profile's authority.
func workloadProfile(id string, workload Workload, ceiling Limits) (domain.WorkloadProfile, bool) {
	change := workload.Spec.RepositoryChange
	if workload.Spec.Type != "repository-change" || change == nil || !digestPattern.MatchString(workload.Status.Digest) {
		return domain.WorkloadProfile{}, false
	}
	authority := []string{"source.read", "source.write", "source.fast_forward"}
	if change.Delivery.Mode == "pull-request" {
		authority = []string{"source.read", "source.write", "pull_request.create"}
	}
	if (id == "repository/v2") != (change.Delivery.Mode == "land-on-main") {
		return domain.WorkloadProfile{}, false
	}
	limits := ceiling
	if change.Limits != nil {
		if change.Limits.MaxTimeMinutes > 0 {
			limits.MaxTimeMinutes = change.Limits.MaxTimeMinutes
		}
		if change.Limits.MaxTokens > 0 {
			limits.MaxTokens = change.Limits.MaxTokens
		}
	}
	if limits.MaxTimeMinutes < 1 || limits.MaxTokens < 1 {
		return domain.WorkloadProfile{}, false
	}
	return domain.WorkloadProfile{ID: id, Digest: strings.TrimPrefix(workload.Status.Digest, "sha256:"), Authority: authority,
		MaxTokens: limits.MaxTokens, BudgetMinutes: limits.MaxTimeMinutes, MinAttempts: 2, Tools: []string{}, ContextKinds: []string{}}, true
}

// profileFor names the Deck profile a task needs: its selected profile, or
// the one its repository authority implies.
func profileFor(task domain.Task) string {
	if task.Workload != nil {
		return task.Workload.ID
	}
	switch {
	case slices.Contains(task.Budget.Authority, "source.fast_forward"):
		return "repository/v2"
	case slices.Contains(task.Budget.Authority, "pull_request.create"):
		return "repository/v1"
	}
	return ""
}

func parseTime(value string) int64 {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UnixMilli()
	}
	return time.Now().UnixMilli()
}

// mutation identifies one mirrored step; the guard is the run Deck just read.
func mutation(run domain.Run, step string) store.Mutation {
	return store.Mutation{ActorID: store.MarinaAgentID, IdempotencyKey: run.ID + ":" + step,
		Guard: &store.RunGuard{AgentID: run.AgentID, ExpectedVersion: run.Version, FenceToken: run.FenceToken}}
}
