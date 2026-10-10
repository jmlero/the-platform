package flow

import (
	"context"
	"errors"
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

type revokedRuntime struct {
	runtimeapi.Adapter
	bound     bool
	cancelled int
}

type unavailableRun struct{ Provider }

func (unavailableRun) GetWork(context.Context, string) (workcontrol.WorkDetail, error) {
	return workcontrol.WorkDetail{}, errors.New("Deck unavailable")
}

func TestExpiredLeaseStopsTheOriginalAttemptWhileDeckIsUnavailable(t *testing.T) {
	f := newEngineFlow(t)
	r := &revokedRuntime{}
	f.c.EngineRuntime, f.c.EngineTemplates = r, &flowTemplates{}
	attempt, _, _, _, err := f.c.worker(f.ctx, f.e, f.task, f.s, "implement")
	if err != nil {
		t.Fatal(err)
	}
	f.e, err = f.journal.GetExecution(f.ctx, f.e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.journal.ReconcileLease(f.ctx, f.e.ID, f.e.Version, time.Now().Add(-time.Second), "test expired lease"); err != nil {
		t.Fatal(err)
	}
	f.c.Provider = unavailableRun{}
	if err = f.c.StopExpired(f.ctx); err != nil {
		t.Fatal(err)
	}
	attempt, err = f.journal.GetRuntimeAttempt(f.ctx, attempt.ID)
	if err != nil || r.cancelled != 1 || attempt.State != runtimeapi.StateCancelled {
		t.Fatalf("expired attempt remained live without Deck: %+v cancels=%d error=%v", attempt, r.cancelled, err)
	}
	attempts, err := f.journal.ListRuntimeAttempts(f.ctx, f.e.ID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("lease loss created replacement work: %+v %v", attempts, err)
	}
}

func (r *revokedRuntime) InspectByKey(context.Context, string) (runtimeapi.Observation, error) {
	if r.bound {
		return runtimeapi.Observation{}, errors.New("admission revoked")
	}
	return runtimeapi.Observation{}, nil
}
func (r *revokedRuntime) Start(_ context.Context, spec runtimeapi.Specification) (runtimeapi.Observation, error) {
	r.bound = true
	return runtimeapi.Observation{Found: true, ProviderHandle: "kagent:" + spec.AttemptID, State: runtimeapi.StateRunning}, nil
}
func (r *revokedRuntime) Cancel(_ context.Context, handle string, _ bool) (runtimeapi.Observation, error) {
	r.cancelled++
	return runtimeapi.Observation{Found: true, ProviderHandle: handle, State: runtimeapi.StateCancelled}, nil
}
func (r *revokedRuntime) CollectArtifacts(context.Context, string) ([]runtimeapi.Artifact, error) {
	return nil, nil
}
func (r *revokedRuntime) Cleanup(context.Context, string) error { return nil }

func TestRevokedAdmissionCannotPreventStoppingTheRecordedAttempt(t *testing.T) {
	f := newEngineFlow(t)
	r := &revokedRuntime{}
	f.c.EngineRuntime = r
	f.c.EngineTemplates = &flowTemplates{}
	if _, _, _, _, err := f.c.worker(f.ctx, f.e, f.task, f.s, "implement"); err != nil {
		t.Fatal(err)
	}
	if err := f.c.stop(f.ctx, f.e); err != nil || r.cancelled != 1 {
		t.Fatalf("revoked attempt was not stopped: %v cancels=%d", err, r.cancelled)
	}
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
	_, attempt, _, err := journal.EnsureRuntimeAttempt(ctx, e.ID, e.Version, runtimeapi.Specification{IdempotencyKey: "execution:verify", RepositoryURI: "https://git.example/repo.git", SourceRevision: strings.Repeat("a", 40), ContextURI: "journal://context", ContextDigest: "sha256:context", HarnessVersion: "test", Limits: runtimeapi.Limits{CPUMillis: 1000, MemoryBytes: 1024, EphemeralStorageBytes: 1024, WallTimeSeconds: 60, MaxProcesses: 256}, NetworkPolicy: "gateway-only", FilesystemPolicy: "ephemeral", AuthorityFence: 1, DeadlineAt: now.Add(time.Minute), PolicyVersion: "test"})
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
