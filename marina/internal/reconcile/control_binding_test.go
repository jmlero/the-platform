package reconcile

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func TestFreshProfileUsesCurrentBindingForSameRepository(t *testing.T) {
	ctx := context.Background()
	journal, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "binding.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	input := execution.ProjectBinding{ProjectID: "project", Provider: "deck", Repository: "https://github.com/acme/repo.git", Runtime: "test", PolicyVersion: "test", ContextConfig: "old", ArtifactConfig: "test", DeliveryConfig: "test", BudgetMinutes: 30, MaxConcurrency: 1}
	old, err := journal.EnsureProjectBinding(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	repository := workcontrol.RepositoryBinding{Version: old.Version, Repository: "acme/repo", CloneURL: input.Repository, BaseBranch: "main"}
	if err := journal.RecordRepositorySnapshot(ctx, input.ProjectID, "old", repository); err != nil {
		t.Fatal(err)
	}
	input.ContextConfig = "new"
	current, err := journal.EnsureProjectBinding(ctx, input)
	if err != nil || current.Version <= old.Version {
		t.Fatalf("profile rotation did not version the binding: %+v %v", current, err)
	}
	newRepository := repository
	newRepository.Version = current.Version
	if err := journal.RecordRepositorySnapshot(ctx, input.ProjectID, "new", newRepository); err != nil {
		t.Fatal(err)
	}
	work := workcontrol.WorkItem{Context: &workcontrol.TaskContext{Repository: &repository}, Workload: &workcontrol.WorkloadProfile{ID: "repository/v2", Digest: "new"}}
	version, err := bindingVersionForWork(ctx, journal, input.ProjectID, old.Version, work)
	if err != nil || version != current.Version {
		t.Fatalf("fresh profile bound to stale version %d, want %d: %v", version, current.Version, err)
	}
	work.Workload.Digest = "old"
	version, err = bindingVersionForWork(ctx, journal, input.ProjectID, old.Version, work)
	if err != nil || version != old.Version {
		t.Fatalf("old profile silently moved to new binding %d: %v", version, err)
	}
	work.Workload.Digest = "new"
	input.Repository = "https://github.com/acme/other.git"
	input.ContextConfig = "changed-repository"
	changed, err := journal.EnsureProjectBinding(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	changedRepository := workcontrol.RepositoryBinding{Version: changed.Version, Repository: "acme/other", CloneURL: input.Repository, BaseBranch: "main"}
	if err := journal.RecordRepositorySnapshot(ctx, input.ProjectID, "new", changedRepository); err != nil {
		t.Fatal(err)
	}
	version, err = bindingVersionForWork(ctx, journal, input.ProjectID, old.Version, work)
	if err != nil || version != old.Version {
		t.Fatalf("repository change silently moved work to version %d: %v", version, err)
	}
	work.Context.Repository.Repository = "other/repo"
	if _, err = bindingVersionForWork(ctx, journal, input.ProjectID, old.Version, work); err == nil {
		t.Fatal("forged repository selection accepted")
	}
}
