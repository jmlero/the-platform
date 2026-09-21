package store

import (
	"context"
	"errors"
	"github.com/jmlero/the-platform/deck/internal/domain"
	"path/filepath"
	"testing"
)

func TestRepositoryAssociationFreezesAtAuthorizationAndRejectsStaleResults(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	input := domain.RepositoryRequestInput{Repository: "owner/one", BaseBranch: "main"}
	request, err := s.RequestRepository(ctx, "prj_default", input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.RequestRepository(ctx, "prj_default", input)
	if err != nil || replay.Revision != request.Revision {
		t.Fatal("request replay", err)
	}
	result := domain.RepositoryResult{RequestRevision: request.Revision, Status: "connected", Binding: &domain.RepositoryBinding{Version: 1, Repository: "owner/one", CloneURL: "https://github.com/owner/one.git", BaseBranch: "main"}}
	if err = s.RecordRepositoryResult(ctx, "prj_default", result); err != nil {
		t.Fatal(err)
	}
	first, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "First", Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	input.ExpectedRevision = request.Revision
	input.ExpectedBindingVersion = 1
	input.Repository = "owner/two"
	request, err = s.RequestRepository(ctx, "prj_default", input)
	if err != nil {
		t.Fatal(err)
	}
	result.RequestRevision = request.Revision
	result.Binding = &domain.RepositoryBinding{Version: 2, Repository: "owner/two", CloneURL: "https://github.com/owner/two.git", BaseBranch: "main"}
	if err = s.RecordRepositoryResult(ctx, "prj_default", result); err != nil {
		t.Fatal(err)
	}
	next, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Next", Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	first, err = s.GetTask(ctx, first.ID)
	if err != nil || first.Context.Repository.Version != 1 || next.Context.Repository.Version != 2 {
		t.Fatal("binding changed under authorized task", err)
	}
	result.RequestRevision--
	if err = s.RecordRepositoryResult(ctx, "prj_default", result); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("stale result accepted", err)
	}
}
