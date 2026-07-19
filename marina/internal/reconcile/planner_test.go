package reconcile

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
)

type executionSourceStub struct {
	records []execution.Execution
	err     error
	limit   int
}

func (source *executionSourceStub) ListActiveExecutions(_ context.Context, limit int) ([]execution.Execution, error) {
	source.limit = limit
	return source.records, source.err
}

func TestPlannerBuildsSideEffectFreeRecoveryIntents(t *testing.T) {
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	source := &executionSourceStub{records: []execution.Execution{
		{
			ID:             "exec-running",
			State:          execution.StateRunning,
			Attempt:        1,
			LeaseExpiresAt: now.Add(time.Hour),
			DeadlineAt:     now.Add(2 * time.Hour),
			MaxAttempts:    3,
			MaxTokens:      1000,
		},
		{
			ID:             "exec-expired",
			State:          execution.StatePlanning,
			Attempt:        1,
			LeaseExpiresAt: now,
			DeadlineAt:     now.Add(2 * time.Hour),
			MaxAttempts:    3,
			MaxTokens:      1000,
		},
	}}
	planner, err := NewPlanner(source, 10*time.Minute)
	if err != nil {
		t.Fatalf("new planner: %v", err)
	}
	planner.WithClock(func() time.Time { return now })

	intents, err := planner.Plan(context.Background(), 27)
	if err != nil {
		t.Fatalf("plan recovery: %v", err)
	}
	if source.limit != 27 {
		t.Fatalf("source limit = %d, want 27", source.limit)
	}
	if len(intents) != 2 {
		t.Fatalf("intent count = %d, want 2", len(intents))
	}
	if intents[0].Action != execution.RecoveryInspectRuntime {
		t.Fatalf("first action = %q, want %q", intents[0].Action, execution.RecoveryInspectRuntime)
	}
	if intents[1].Action != execution.RecoveryReconcileLease {
		t.Fatalf("second action = %q, want %q", intents[1].Action, execution.RecoveryReconcileLease)
	}
}

func TestPlannerReturnsSourceFailure(t *testing.T) {
	want := errors.New("journal unavailable")
	planner, err := NewPlanner(&executionSourceStub{err: want}, 0)
	if err != nil {
		t.Fatalf("new planner: %v", err)
	}
	if _, err := planner.Plan(context.Background(), 1); !errors.Is(err, want) {
		t.Fatalf("plan error = %v, want %v", err, want)
	}
}

func TestPlannerRejectsInvalidConfiguration(t *testing.T) {
	if _, err := NewPlanner(nil, 0); !errors.Is(err, ErrMissingExecutionSource) {
		t.Fatalf("nil source error = %v", err)
	}
	if _, err := NewPlanner(&executionSourceStub{}, -time.Second); err == nil {
		t.Fatal("negative renewal window was accepted")
	}
}
