package flow

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

type cancellingRuntime struct {
	runtimeapi.Adapter
	calls int
}

func (r *cancellingRuntime) InspectByKey(context.Context, string) (runtimeapi.Observation, error) {
	return runtimeapi.Observation{Found: true, ProviderHandle: "job/uid", State: runtimeapi.StateRunning}, nil
}
func (r *cancellingRuntime) Cancel(context.Context, string, bool) (runtimeapi.Observation, error) {
	r.calls++
	state := runtimeapi.StateRunning
	if r.calls > 1 {
		state = runtimeapi.StateCancelled
	}
	return runtimeapi.Observation{Found: true, ProviderHandle: "job/uid", State: state}, nil
}

func TestCancelIsNotAcknowledgedUntilJobTerminationIsObserved(t *testing.T) {
	ctx := context.Background()
	journal, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	now := time.Now().UTC()
	e, _, err := journal.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", LeaseExpiresAt: now.Add(time.Minute), Budget: execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 2, MaxTokens: 10000}})
	if err != nil {
		t.Fatal(err)
	}
	_, attempt, _, err := journal.EnsureRuntimeAttempt(ctx, e.ID, e.Version, runtimeapi.Specification{IdempotencyKey: "job", RepositoryURI: "https://git.example/repo.git", SourceRevision: strings.Repeat("a", 40), ContextURI: "journal://context", ContextDigest: "sha256:context", HarnessVersion: "test", Limits: runtimeapi.Limits{CPUMillis: 1000, MemoryBytes: 1024, EphemeralStorageBytes: 1024, WallTimeSeconds: 60, MaxProcesses: 256}, NetworkPolicy: "gateway-only", FilesystemPolicy: "ephemeral", AuthorityFence: 1, DeadlineAt: now.Add(time.Minute), PolicyVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &cancellingRuntime{}
	controller := &Controller{Journal: journal, Runtime: runtime}
	status, _, err := controller.Command(ctx, e, workcontrol.Command{Type: "cancel"})
	if err == nil || status != "" {
		t.Fatalf("acknowledged live Job: %q %v", status, err)
	}
	status, _, err = controller.Command(ctx, e, workcontrol.Command{Type: "cancel"})
	if err != nil || status != "completed" {
		t.Fatalf("termination not acknowledged: %q %v", status, err)
	}
	attempt, err = journal.GetRuntimeAttempt(ctx, attempt.ID)
	if err != nil || attempt.State != runtimeapi.StateCancelled {
		t.Fatalf("termination not durable: %#v %v", attempt, err)
	}
}
