package flow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol/github"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

type dependencyProvider struct {
	Provider
	prerequisite workcontrol.WorkItem
}

func (p dependencyProvider) GetWork(context.Context, string) (workcontrol.WorkDetail, error) {
	return workcontrol.WorkDetail{Work: p.prerequisite}, nil
}

type dependencySource struct {
	sourcecontrol.Port
	contains bool
	err      error
}

func (s dependencySource) ContainsCommit(context.Context, string, string, string) (bool, error) {
	return s.contains, s.err
}

func TestLandedPrerequisiteMustRemainOnAuthorizedMain(t *testing.T) {
	for _, scenario := range []string{"same commit", "ancestor", "missing", "other repository", "provider unavailable", "human accepted v1"} {
		t.Run(scenario, func(t *testing.T) {
			c := Controller{Config: Config{Git: github.Config{Repository: "owner/repo", BaseBranch: "main"}}}
			landing := &workcontrol.TaskLanding{LandingObservation: workcontrol.LandingObservation{Repository: "owner/repo", BaseBranch: "main", Commit: strings.Repeat("a", 40)}}
			base := strings.Repeat("b", 40)
			if scenario == "same commit" {
				base = landing.Commit
			}
			if scenario == "other repository" {
				landing.Repository = "other/repo"
			}
			if scenario == "human accepted v1" {
				landing = nil
			}
			c.Provider = dependencyProvider{prerequisite: workcontrol.WorkItem{ProjectID: "project", Landing: landing}}
			source := dependencySource{contains: scenario == "ancestor"}
			if scenario == "provider unavailable" {
				source.err = context.DeadlineExceeded
			}
			err := c.confirmDependencies(context.Background(), source, workcontrol.WorkItem{ProjectID: "project", DependsOn: []string{"prerequisite"}}, base)
			wantError := scenario == "missing" || scenario == "other repository" || scenario == "provider unavailable"
			if (err != nil) != wantError {
				t.Fatalf("prerequisite admission: %v", err)
			}
			if scenario == "provider unavailable" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("transport failure was lost")
			}
		})
	}
}

func TestPRDSourceCannotBeRemovedFromExecutionBaseByEditingDependencies(t *testing.T) {
	c := Controller{Config: Config{Git: github.Config{Repository: "owner/repo", BaseBranch: "main"}}}
	commit := strings.Repeat("a", 40)
	c.Provider = dependencyProvider{prerequisite: workcontrol.WorkItem{ID: "publication", ProjectID: "project", Status: "done", PRD: &workcontrol.PRDDocument{PRDDraft: workcontrol.PRDDraft{Path: "docs/prd/features.md"}, Digest: "prd-digest"}, Landing: &workcontrol.TaskLanding{LandingObservation: workcontrol.LandingObservation{Repository: "owner/repo", BaseBranch: "main", Commit: commit}}}}
	task := workcontrol.WorkItem{ProjectID: "project", PRDReference: &workcontrol.PRDReference{PublicationTaskID: "publication", Path: "docs/prd/features.md", Revision: commit, Digest: "prd-digest", Repository: workcontrol.RepositoryBinding{Repository: "owner/repo", BaseBranch: "main"}}}
	if err := c.confirmDependencies(context.Background(), dependencySource{contains: false}, task, strings.Repeat("b", 40)); err == nil {
		t.Fatal("missing PRD commit admitted after removing ordinary dependencies")
	}
	if err := c.confirmDependencies(context.Background(), dependencySource{contains: true}, task, strings.Repeat("b", 40)); err != nil {
		t.Fatalf("retained PRD ancestor blocked: %v", err)
	}
}
