package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

// bindRepository records the repository Marina bound, as the Marina sync does.
func bindRepository(t *testing.T, s *Store, project, repository string, version int64) {
	t.Helper()
	if err := s.SetRepositoryBinding(context.Background(), project, domain.RepositoryBinding{Version: version, Repository: repository, CloneURL: "https://github.com/" + repository + ".git", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
}

func TestMarinaRepositoryBindingFreezesAtAuthorization(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	bindRepository(t, s, "prj_default", "owner/one", 1)
	bindRepository(t, s, "prj_default", "owner/one", 1)
	repository, err := s.GetRepository(ctx, "prj_default")
	if err != nil || repository.Status != "connected" || repository.Revision != 1 {
		t.Fatalf("repository = %+v, %v", repository, err)
	}
	first, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "First", Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	bindRepository(t, s, "prj_default", "owner/two", 2)
	next, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Next", Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	first, err = s.GetTask(ctx, first.ID)
	if err != nil || first.Context.Repository.Version != 1 || next.Context.Repository.Version != 2 {
		t.Fatal("binding changed under authorized task", err)
	}
	if err := s.SetRepositoryBinding(ctx, "prj_default", domain.RepositoryBinding{Version: 0, Repository: "bad name", BaseBranch: "main"}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("invalid binding = %v", err)
	}
}
