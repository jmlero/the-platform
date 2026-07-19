package execution

import (
	"context"
	"time"
)

type Journal interface {
	EnsureExecution(context.Context, NewExecution) (Execution, bool, error)
	GetExecution(context.Context, string) (Execution, error)
	ListActiveExecutions(context.Context, int) ([]Execution, error)
	Transition(context.Context, string, Transition) (Execution, error)
	RenewLease(context.Context, string, int64, time.Time) (Execution, error)
	ReconcileLease(context.Context, string, int64, time.Time, string) (Execution, error)
	SaveCheckpoint(context.Context, string, int64, CheckpointInput) (Execution, Checkpoint, bool, error)
	LatestCheckpoint(context.Context, string) (Checkpoint, error)
	AddEvidence(context.Context, string, int64, EvidenceInput) (Execution, Evidence, bool, error)
	ListEvidence(context.Context, string) ([]Evidence, error)
	ListEvents(context.Context, string, int) ([]Event, error)
	Close() error
}
