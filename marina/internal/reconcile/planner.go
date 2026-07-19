package reconcile

import (
	"context"
	"errors"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
)

var ErrMissingExecutionSource = errors.New("active execution source is required")

type ActiveExecutionSource interface {
	ListActiveExecutions(context.Context, int) ([]execution.Execution, error)
}

// Planner reconstructs restart work from durable records without performing
// external side effects. Controllers must reconcile the provider lease and
// runtime before acting on an intent.
type Planner struct {
	source      ActiveExecutionSource
	clock       func() time.Time
	renewBefore time.Duration
}

func NewPlanner(source ActiveExecutionSource, renewBefore time.Duration) (*Planner, error) {
	if source == nil {
		return nil, ErrMissingExecutionSource
	}
	if renewBefore < 0 {
		return nil, errors.New("lease renewal window cannot be negative")
	}
	return &Planner{
		source:      source,
		clock:       time.Now,
		renewBefore: renewBefore,
	}, nil
}

func (planner *Planner) WithClock(clock func() time.Time) *Planner {
	if clock != nil {
		planner.clock = clock
	}
	return planner
}

func (planner *Planner) Plan(ctx context.Context, limit int) ([]execution.RecoveryIntent, error) {
	records, err := planner.source.ListActiveExecutions(ctx, limit)
	if err != nil {
		return nil, err
	}
	now := planner.clock().UTC()
	intents := make([]execution.RecoveryIntent, 0, len(records))
	for _, record := range records {
		intents = append(intents, execution.PlanRecovery(record, now, planner.renewBefore))
	}
	return intents, nil
}
