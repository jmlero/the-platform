package flow

import (
	"context"
	"errors"
	"testing"

	rt "github.com/jmlero/the-platform/marina/internal/runtime"
)

type failedCollection struct {
	terminalEngine
	fail bool
}

func (e *failedCollection) CollectArtifacts(context.Context, string) ([]rt.Artifact, error) {
	if e.fail {
		return nil, errors.New("artifact store unavailable")
	}
	return nil, nil
}

func TestCleanupFailureIsDurableAndRetryable(t *testing.T) {
	f := newEngineFlow(t)
	e := &failedCollection{fail: true}
	f.c.EngineRuntime = e
	f.c.EngineTemplates = &flowTemplates{}
	attempt, done, _, _, err := f.c.worker(f.ctx, f.e, f.task, f.s, "implement")
	if err != nil || !done {
		t.Fatalf("terminal setup: %v", err)
	}
	if err := f.c.cleanup(f.ctx); err == nil {
		t.Fatal("failed collection was hidden")
	}
	attempt, err = f.journal.GetRuntimeAttempt(f.ctx, attempt.ID)
	if err != nil || attempt.CleanupState != rt.CleanupFailed {
		t.Fatalf("cleanup failure not retained: %+v %v", attempt, err)
	}
	e.fail = false
	if err := f.c.cleanup(f.ctx); err != nil {
		t.Fatal(err)
	}
	attempt, err = f.journal.GetRuntimeAttempt(f.ctx, attempt.ID)
	if err != nil || attempt.CleanupState != rt.CleanupCompleted || len(e.cleaned) != 1 {
		t.Fatalf("cleanup did not recover: %+v %v", attempt, err)
	}
}

func TestCleanupLeavesOtherProjectsAttemptsToTheirOwnProfile(t *testing.T) {
	f := newEngineFlow(t)
	e := &failedCollection{}
	f.c.EngineRuntime = e
	f.c.EngineTemplates = &flowTemplates{}
	attempt, done, _, _, err := f.c.worker(f.ctx, f.e, f.task, f.s, "implement")
	if err != nil || !done {
		t.Fatalf("terminal setup: %v", err)
	}
	other := *f.c
	other.Config.ProjectID = "other.project"
	if err := other.cleanup(f.ctx); err != nil {
		t.Fatal(err)
	}
	if attempt, err = f.journal.GetRuntimeAttempt(f.ctx, attempt.ID); err != nil || attempt.CleanupState == rt.CleanupCompleted || len(e.cleaned) != 0 {
		t.Fatalf("another project's profile cleaned the attempt: %+v %v", attempt, err)
	}
	if err := f.c.cleanup(f.ctx); err != nil {
		t.Fatal(err)
	}
	if attempt, err = f.journal.GetRuntimeAttempt(f.ctx, attempt.ID); err != nil || attempt.CleanupState != rt.CleanupCompleted {
		t.Fatalf("own project did not clean up: %+v %v", attempt, err)
	}
}
