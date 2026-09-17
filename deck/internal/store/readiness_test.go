package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestRepositoryReadinessBeforeCreateMoveAndEdit(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "ready.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	valid := domain.CreateTaskInput{Title: "Fix repository", AcceptanceCriteria: "Regression passes", Status: domain.TaskReady, Budget: &domain.Budget{MaxAttempts: 2, Authority: []string{"source.read", "source.write", "pull_request.create"}}}
	for _, test := range []struct {
		name, criteria string
		attempts       int
		authority      []string
		want           string
	}{
		{"criteria", " ", 2, valid.Budget.Authority, "acceptance criteria"},
		{"attempts", valid.AcceptanceCriteria, 1, valid.Budget.Authority, "two attempts"},
		{"authority", valid.AcceptanceCriteria, 2, []string{"source.read"}, "source.write"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			budget := *valid.Budget
			input.Budget = &budget
			input.AcceptanceCriteria = test.criteria
			budget.MaxAttempts = test.attempts
			budget.Authority = test.authority
			if _, err := s.CreateTask(ctx, input); !errors.Is(err, domain.ErrInvalidInput) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ready create: %v", err)
			}
			input.Status = domain.TaskBacklog
			draft, err := s.CreateTask(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.MoveTask(ctx, draft.ID, domain.TaskReady); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("ready move: %v", err)
			}
			loaded, _ := s.GetTask(ctx, draft.ID)
			if loaded.Status != domain.TaskBacklog {
				t.Fatal("invalid draft dispatched")
			}
			ready, err := s.CreateTask(ctx, valid)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.UpdateTask(ctx, ready.ID, domain.UpdateTaskInput{AcceptanceCriteria: &input.AcceptanceCriteria, Budget: input.Budget}); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("ready edit: %v", err)
			}
		})
	}
	if _, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Generic control task", Status: domain.TaskReady}); err != nil {
		t.Fatal(err)
	}
}
