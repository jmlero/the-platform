package flow

import (
	"context"
	"errors"
	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"time"
)

func (c Config) WorkloadProfiles() []workcontrol.WorkloadProfile {
	profiles := []workcontrol.WorkloadProfile{}
	kinds := []string{"instruction", "goal", "document", "decision", "message"}
	if c.RepositoryEnabled() {
		kinds = append(kinds, "git")
		profiles = append(profiles, workcontrol.WorkloadProfile{ID: "repository/v1", Digest: c.Digest(), Harness: "codex/v1", Authority: []string{"source.read", "source.write", "pull_request.create"}, MaxTokens: c.MaxTokens, BudgetMinutes: c.BudgetMinutes, MinAttempts: 2, Tools: []string{}, ContextKinds: kinds})
	}
	if c.Analysis != nil {
		analysisKinds := []string{}
		if c.Analysis.HarnessVersion == analysis.HarnessWithContext {
			analysisKinds = kinds
		}
		tools := c.Analysis.Tools
		if tools == nil {
			tools = []string{}
		}
		profiles = append(profiles, workcontrol.WorkloadProfile{ID: analysis.Kind, Digest: c.AnalysisDigest(), Harness: c.Analysis.HarnessVersion, Authority: []string{c.AnalysisAuthority()}, MaxTokens: c.Analysis.MaxTokens, BudgetMinutes: c.Analysis.BudgetMinutes, MinAttempts: 1, Tools: tools, ContextKinds: analysisKinds})
	}
	return profiles
}
func (c *Controller) PublishWorkloads(ctx context.Context) error {
	provider, ok := c.Provider.(workcontrol.WorkloadPublication)
	if !ok {
		return errors.New("provider lacks workload publication")
	}
	config := c.Config
	if config.RepositoryEnabled() && c.Journal != nil {
		binding, err := c.Journal.CurrentProjectBinding(ctx, config.ProjectID)
		if err != nil {
			return err
		}
		selected, profile, err := c.Journal.RepositorySnapshot(ctx, config.ProjectID, binding.Version)
		if err != nil {
			return err
		}
		if profile != config.Digest() {
			return errors.New("installed profile differs from current repository binding")
		}
		config = withRepository(config, selected)
	}
	return provider.PublishWorkloads(ctx, c.Config.ProjectID, workcontrol.WorkloadCatalog{Profiles: config.WorkloadProfiles(), ExpiresAt: time.Now().Add(2 * time.Minute).UnixMilli()})
}
func (c Config) MatchesWorkload(task workcontrol.WorkItem) bool {
	if task.Workload == nil {
		return true
	}
	for _, p := range c.WorkloadProfiles() {
		if p.ID == task.Workload.ID && p.Digest == task.Workload.Digest {
			return analysis.Digest(p) == analysis.Digest(*task.Workload)
		}
	}
	return false
}
