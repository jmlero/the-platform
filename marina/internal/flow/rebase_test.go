package flow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/worker"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

func TestChangedAcceptanceCriteriaBlocksAutomaticCompletionWithClearReason(t *testing.T) {
	f, p, runtime := newRepairFlow(t, 6)
	runtime.exitCode = 0
	source := &repairSource{main: f.s.Base, head: f.s.Head, writes: map[string]sourcecontrol.Revision{}}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 40; cycle++ {
		e, err := f.journal.GetExecution(f.ctx, f.e.ID)
		if err != nil {
			t.Fatal(err)
		}
		s, err := f.c.load(f.ctx, e)
		if err != nil {
			t.Fatal(err)
		}
		if s.Step == "complete" {
			p.work.AcceptanceCriteria = "Edited criteria require another run"
			if err := f.c.advance(f.ctx, e); err != nil {
				t.Fatalf("criteria edit should produce a blocker rather than an indefinite retry: %v", err)
			}
			e, err = f.journal.GetExecution(f.ctx, e.ID)
			if err != nil || e.State != execution.StateBlocked || !strings.Contains(p.blocked, "Acceptance criteria changed") || !strings.Contains(p.blocked, "new task") || p.run.Status == workcontrol.RunCompleted {
				t.Fatalf("edited criteria completed or lacked a focused blocker: %+v err=%v reason=%q", e, err, p.blocked)
			}
			return
		}
		if err := f.c.advance(f.ctx, e); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}
	t.Fatal("task did not reach automatic completion")
}

type rebaseDependencyProvider struct {
	*repairProvider
	task         workcontrol.WorkItem
	prerequisite workcontrol.WorkItem
}

func (p rebaseDependencyProvider) GetWork(_ context.Context, id string) (workcontrol.WorkDetail, error) {
	if id == p.task.ID {
		return workcontrol.WorkDetail{Work: p.task, Runs: []workcontrol.Run{p.run}}, nil
	}
	return workcontrol.WorkDetail{Work: p.prerequisite}, nil
}

func TestRebaseReobservesLandedPrerequisitesOnItsExactNewBase(t *testing.T) {
	for _, contained := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing after rewrite", true: "still present"}[contained], func(t *testing.T) {
			f, p, runtime := newRepairFlow(t, 6)
			f.s.Step, f.s.Head = "ci", strings.Repeat("c", 40)
			task := workcontrol.WorkItem{ID: f.e.ExternalWorkID, ProjectID: f.e.ProjectID, DependsOn: []string{"prerequisite"}}
			f.c.Provider = rebaseDependencyProvider{repairProvider: p, task: task, prerequisite: workcontrol.WorkItem{ID: "prerequisite", ProjectID: task.ProjectID,
				Landing: &workcontrol.TaskLanding{LandingObservation: workcontrol.LandingObservation{Repository: f.c.Config.Git.Repository, BaseBranch: f.c.Config.Git.BaseBranch, Commit: strings.Repeat("a", 40)}}}}
			source := &rebaseAncestrySource{repairSource: &repairSource{main: strings.Repeat("d", 40), head: f.s.Head}, contained: contained}
			f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
			if err := f.c.beginRebase(f.ctx, f.e, p.run, f.s, source.main); err != nil {
				t.Fatal(err)
			}
			if contained {
				s, err := f.c.load(f.ctx, f.e)
				if err != nil || s.Step != "implement" || s.Head != source.main || len(s.Rebases) != 1 || p.blocked != "" {
					t.Fatalf("available prerequisite did not admit rebase: %+v err=%v blocked=%q", s, err, p.blocked)
				}
			} else if !strings.Contains(p.blocked, "prerequisite") || !strings.Contains(p.blocked, "no longer on main") {
				t.Fatalf("rewritten main admitted work without its prerequisite: blocked=%q", p.blocked)
			}
			if len(runtime.starts) != 0 || source.lands != 0 {
				t.Fatal("prerequisite observation started compute or updated a ref")
			}
		})
	}
}

type racingLandingSource struct {
	*repairSource
}

func (s *racingLandingSource) FastForward(context.Context, sourcecontrol.FastForwardRequest) (sourcecontrol.Revision, error) {
	s.main = strings.Repeat("d", 40)
	return sourcecontrol.Revision{}, context.DeadlineExceeded
}

func TestMainMovementBetweenObservationAndFastForwardReconcilesIntoRebase(t *testing.T) {
	f, p, runtime := newRepairFlow(t, 6)
	f.s.Step, f.s.Head = "land", strings.Repeat("c", 40)
	source := &racingLandingSource{repairSource: &repairSource{main: f.s.Base, head: f.s.Head}}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	if err := f.c.advance(f.ctx, f.e); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("landing race did not retain ambiguous result for reconciliation: %v", err)
	}
	s, err := f.c.load(f.ctx, f.e)
	if err != nil || s.Step != "land" || s.Landing != nil || len(s.Rebases) != 0 {
		t.Fatalf("failed landing fabricated completion: %+v %v", s, err)
	}
	if err := f.c.advance(f.ctx, f.e); err != nil {
		t.Fatal(err)
	}
	s, err = f.c.load(f.ctx, f.e)
	if err != nil || s.Step != "implement" || s.Head != source.main || len(s.Rebases) != 1 || p.blocked != "" || len(runtime.starts) != 0 || source.lands != 0 {
		t.Fatalf("landing race did not schedule fresh gates on observed main: %+v err=%v blocked=%q", s, err, p.blocked)
	}
}

type conflictingRebaseRuntime struct {
	*repairRuntime
}

func (r *conflictingRebaseRuntime) Start(ctx context.Context, spec rt.Specification) (rt.Observation, error) {
	if !strings.HasSuffix(spec.IdempotencyKey, ":implement") || !strings.Contains(spec.IdempotencyKey, ":rebase:") {
		return r.repairRuntime.Start(ctx, spec)
	}
	r.starts = append(r.starts, spec)
	result := workresult.Result{Stage: "implement", SourceRevision: spec.SourceRevision, Summary: "The task and main both changed the same lines", ObservedAt: time.Now().UTC(), BlockingReason: "conflict in src/fix.go"}
	data, err := json.Marshal(result)
	if err != nil {
		return rt.Observation{}, err
	}
	digest, err := r.f.c.Artifacts.Put(data)
	if err != nil {
		return rt.Observation{}, err
	}
	_, _, err = r.f.c.Journal.RecordRuntimeArtifact(ctx, rt.Artifact{ID: "result:" + spec.AttemptID, ExecutionID: spec.ExecutionID, RuntimeAttemptID: spec.AttemptID,
		Name: "conflict.json", Kind: "worker_result", URI: r.f.c.artifactURL(digest), Digest: digest, SizeBytes: int64(len(data))})
	if err != nil {
		return rt.Observation{}, err
	}
	return rt.Observation{Found: true, ProviderHandle: "kagent:" + spec.AttemptID, State: rt.StateSucceeded}, nil
}

func TestNativeRebaseConflictBlocksWithoutPublishingOrDiscardingHistory(t *testing.T) {
	f, p, runtime := newRepairFlow(t, 6)
	f.s.Step, f.s.Head = "ci", strings.Repeat("c", 40)
	source := &repairSource{main: strings.Repeat("d", 40), head: f.s.Head, writes: map[string]sourcecontrol.Revision{}}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	f.c.EngineRuntime = &conflictingRebaseRuntime{runtime}
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 2; cycle++ {
		if err := f.c.advance(f.ctx, f.e); err != nil {
			t.Fatal(err)
		}
	}
	e, err := f.journal.GetExecution(f.ctx, f.e.ID)
	if err != nil || e.State != execution.StateBlocked || !strings.Contains(p.blocked, "src/fix.go") || len(runtime.starts) != 1 || len(source.writes) != 0 || source.lands != 0 {
		t.Fatalf("conflict did not become a bounded blocker: %+v err=%v blocked=%q starts=%d commits=%d", e, err, p.blocked, len(runtime.starts), len(source.writes))
	}
	s, err := f.c.load(f.ctx, e)
	if err != nil || len(s.Rebases) != 1 || s.Rebases[0].HeadRevision != f.s.Head || s.Rebases[0].BaseRevision != f.s.Base || len(p.evidence) != 0 {
		t.Fatalf("conflict lost pinned history or published passing evidence: %+v err=%v", s, err)
	}
	artifacts, err := f.journal.ListRuntimeArtifacts(f.ctx, e.ID)
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("conflict evidence was not retained: %+v err=%v", artifacts, err)
	}
}

func TestRepeatedMainMovementStopsAtTheOriginalRuntimeBudget(t *testing.T) {
	f, p, runtime := newRepairFlow(t, 6)
	runtime.exitCode = 0
	source := &repairSource{main: f.s.Base, head: f.s.Head, writes: map[string]sourcecontrol.Revision{}}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	movements := 0
	for cycle := 0; cycle < 70; cycle++ {
		e, err := f.journal.GetExecution(f.ctx, f.e.ID)
		if err != nil {
			t.Fatal(err)
		}
		if e.State.Terminal() {
			break
		}
		s, err := f.c.load(f.ctx, e)
		if err != nil {
			t.Fatal(err)
		}
		if s.Step == "ci" && movements == len(s.Rebases) {
			if movements == 0 {
				source.main = strings.Repeat("d", 40)
			} else {
				source.main = strings.Repeat("e", 40)
			}
			movements++
		}
		if err := f.c.advance(f.ctx, e); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}
	e, err := f.journal.GetExecution(f.ctx, f.e.ID)
	if err != nil || e.State != execution.StateBlocked || !strings.Contains(p.blocked, "original budget") || movements != 2 || len(runtime.starts) != 6 || len(source.writes) != 2 || source.lands != 0 {
		t.Fatalf("main movement expanded the task budget: %+v err=%v blocked=%q movements=%d starts=%d commits=%d", e, err, p.blocked, movements, len(runtime.starts), len(source.writes))
	}
}

func TestRebaseAfterRepairRetainsFailureAndDiagnosisUnderOriginalBudget(t *testing.T) {
	f, p, runtime := newRepairFlow(t, 11)
	source := &repairSource{main: f.s.Base, head: f.s.Head, writes: map[string]sourcecontrol.Revision{}}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	var diagnosed repairFailure
	for cycle := 0; cycle < 80; cycle++ {
		e, err := f.journal.GetExecution(f.ctx, f.e.ID)
		if err != nil {
			t.Fatal(err)
		}
		if e.State.Terminal() {
			break
		}
		s, err := f.c.load(f.ctx, e)
		if err != nil {
			t.Fatal(err)
		}
		if s.Step == "ci" && len(s.Rebases) == 0 {
			diagnosed = s.Failures[0]
			source.main = strings.Repeat("d", 40)
		}
		if err := f.c.advance(f.ctx, e); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}
	e, err := f.journal.GetExecution(f.ctx, f.e.ID)
	if err != nil || e.State != execution.StateCompleted {
		t.Fatalf("repaired rebase did not complete: %+v err=%v blocked=%q", e, err, p.blocked)
	}
	s, err := f.c.load(f.ctx, e)
	before, _ := json.Marshal(diagnosed)
	if err != nil || len(s.Failures) != 1 || len(s.Rebases) != 1 || s.RepairPlanDigest != diagnosed.PlanDigest || len(runtime.starts) != 10 || len(source.writes) != 3 || source.lands != 1 {
		t.Fatalf("repair history or gates changed across rebase: %+v starts=%d commits=%d err=%v", s, len(runtime.starts), len(source.writes), err)
	}
	after, _ := json.Marshal(s.Failures[0])
	if string(before) != string(after) || s.Failures[0].SourceRevision == s.Head {
		t.Fatal("rebase rewrote earlier failure evidence or diagnosis")
	}
}

func TestImplementationAndRebasePromptsRequireFrozenOfflineChecks(t *testing.T) {
	for _, rebase := range []*worker.Rebase{nil, {BaseRevision: strings.Repeat("a", 40), HeadRevision: strings.Repeat("b", 40), Branch: "marina/task"}} {
		prompt, err := enginePrompt(worker.Input{Stage: "implement", Rebase: rebase})
		if err != nil {
			t.Fatal(err)
		}
		for _, required := range []string{"Run every configured check", "prepackaged toolchain", "do not download", "summary"} {
			if !strings.Contains(string(prompt), required) {
				t.Fatalf("producer prompt omitted %q; rebase=%v", required, rebase != nil)
			}
		}
	}
}

type rebaseAncestrySource struct {
	*repairSource
	contained bool
}

func (s *rebaseAncestrySource) ContainsCommit(context.Context, string, string, string) (bool, error) {
	return s.contained, nil
}

func TestAdvancedMainContainingVerifiedCommitDoesNotSpendAnotherRebase(t *testing.T) {
	for _, step := range []string{"ci", "land"} {
		t.Run(step, func(t *testing.T) {
			f, p, runtime := newRepairFlow(t, 3)
			f.s.Step, f.s.Head = step, strings.Repeat("c", 40)
			source := &rebaseAncestrySource{repairSource: &repairSource{main: strings.Repeat("d", 40), head: f.s.Head}, contained: true}
			f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
			if err := f.c.save(f.ctx, f.e, f.s); err != nil {
				t.Fatal(err)
			}
			for cycle := 0; cycle < 2; cycle++ {
				if err := f.c.advance(f.ctx, f.e); err != nil {
					t.Fatal(err)
				}
				s, err := f.c.load(f.ctx, f.e)
				if err != nil {
					t.Fatal(err)
				}
				if s.Step == "evidence" {
					if s.Landing == nil || s.Landing.Commit != f.s.Head || len(s.Rebases) != 0 || len(runtime.starts) != 0 || source.lands != 0 {
						t.Fatalf("known landed commit spent budget or changed refs: %+v starts=%d lands=%d", s, len(runtime.starts), source.lands)
					}
					return
				}
			}
			f.s, _ = f.c.load(f.ctx, f.e)
			t.Fatalf("verified ancestor should recover the landing instead of rebasing: state=%+v blocked=%q", f.s, p.blocked)
		})
	}
}

func TestVerifiedAncestorStillRequiresPassingCIOnItsExactCommit(t *testing.T) {
	f, p, runtime := newRepairFlow(t, 3)
	f.s.Step, f.s.Head = "land", strings.Repeat("c", 40)
	source := &rebaseAncestrySource{repairSource: &repairSource{main: strings.Repeat("d", 40), head: f.s.Head, ciFails: true,
		writes: map[string]sourcecontrol.Revision{"original": {Commit: f.s.Head}}}, contained: true}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	if err := f.c.advance(f.ctx, f.e); err != nil {
		t.Fatal(err)
	}
	s, err := f.c.load(f.ctx, f.e)
	if err != nil || s.Step != "repair" || len(s.Failures) != 1 || s.Failures[0].SourceRevision != f.s.Head || s.Landing != nil || len(s.Rebases) != 0 || len(runtime.starts) != 0 || source.lands != 0 || p.blocked != "" {
		t.Fatalf("ancestry bypassed failing exact-head CI: %+v err=%v", s, err)
	}
}

func TestRebasedCommitUsesFreshGatesAcrossRestartAndLostResponses(t *testing.T) {
	f, p, runtime := newRepairFlow(t, 6)
	p.loseResponses = true
	runtime.exitCode, runtime.loseStarts = 0, true
	source := &repairSource{main: f.s.Base, head: f.s.Head, writes: map[string]sourcecontrol.Revision{}}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	originalBase := f.s.Base
	var originalHead string
	for cycle := 0; cycle < 80; cycle++ {
		if cycle > 0 {
			if err := f.journal.Close(); err != nil {
				t.Fatal(err)
			}
			journal, err := sqlite.Open(f.ctx, f.path)
			if err != nil {
				t.Fatal(err)
			}
			f.journal, f.c.Journal = journal, journal
		}
		e, err := f.journal.GetExecution(f.ctx, f.e.ID)
		if err != nil {
			t.Fatal(err)
		}
		if e.State.Terminal() {
			break
		}
		s, err := f.c.load(f.ctx, e)
		if err != nil {
			t.Fatal(err)
		}
		if s.Step == "ci" && originalHead == "" {
			originalHead = s.Head
			source.main = strings.Repeat("d", 40)
		}
		if err := f.c.advance(f.ctx, e); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cycle %d: %v", cycle, err)
		}
	}
	defer f.journal.Close()
	e, err := f.journal.GetExecution(f.ctx, f.e.ID)
	if err != nil || e.State != execution.StateCompleted {
		t.Fatalf("rebased task did not complete: %+v err=%v blocked=%q", e, err, p.blocked)
	}
	s, err := f.c.load(f.ctx, e)
	if err != nil || len(s.Rebases) != 1 || s.Base != originalBase || s.Rebases[0].HeadRevision != originalHead || s.Head == originalHead {
		t.Fatalf("rebase history or original inputs changed: %+v err=%v", s, err)
	}
	if len(runtime.starts) != 6 || len(source.writes) != 2 || source.lands != 1 {
		t.Fatalf("duplicate work or skipped rebase gates: starts=%d commits=%d lands=%d", len(runtime.starts), len(source.writes), source.lands)
	}
	for i, stage := range []string{"implement", "review", "verify"} {
		attempt := runtime.starts[i+3]
		if !strings.HasSuffix(attempt.IdempotencyKey, ":"+stage) || !strings.Contains(attempt.IdempotencyKey, ":rebase:1:") || attempt.ContextDigest != f.s.ContextDigest || !attempt.DeadlineAt.Equal(f.e.DeadlineAt) {
			t.Fatalf("rebase gate reused an earlier attempt or expanded inputs: %+v", attempt)
		}
	}
	for _, id := range s.completionEvidence() {
		if evidence := p.evidence[id]; evidence.SourceRevision != s.Head || evidence.Outcome != "passed" {
			t.Fatalf("completion reused an old review/check: %+v", evidence)
		}
	}
	if e.MaxAttempts != 6 || e.MaxTokens != f.e.MaxTokens || !e.DeadlineAt.Equal(f.e.DeadlineAt) {
		t.Fatal("rebase expanded the original budget")
	}
}

func TestMovedMainSchedulesRebaseOnFreshBranchUnderOriginalBudget(t *testing.T) {
	f, p, runtime := newRepairFlow(t, 11)
	originalBase, originalBranch := f.s.Base, f.s.Branch
	f.s.Step, f.s.Head = "ci", strings.Repeat("c", 40)
	f.s.ImplementationDigest, f.s.VerificationDigest = "old-implementation", "old-verification"
	f.s.ReviewDigest, f.s.ReviewEvidenceID = "old-review", "old-review-evidence"
	f.s.EvidenceIDs = []string{"old-test"}
	source := &repairSource{main: strings.Repeat("d", 40), head: f.s.Head, writes: map[string]sourcecontrol.Revision{}}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	if err := f.c.advance(f.ctx, f.e); err != nil {
		t.Fatal(err)
	}
	s, err := f.c.load(f.ctx, f.e)
	if err != nil {
		t.Fatal(err)
	}
	if p.blocked != "" || s.Step != "implement" || s.Head != source.main || s.Base != originalBase || s.Branch == originalBranch || !strings.HasPrefix(s.Branch, originalBranch+"/") {
		t.Fatalf("main movement should schedule bounded rebase without rewriting the existing branch: %+v blocked=%q", s, p.blocked)
	}
	if s.ReviewDigest != "" || s.ReviewEvidenceID != "" || s.VerificationDigest != "" || len(s.EvidenceIDs) != 0 || len(runtime.starts) != 0 || source.lands != 0 {
		t.Fatalf("rebase reused stale review/check evidence or published before fresh review: %+v", s)
	}
	input, err := f.c.engineInput(f.ctx, f.task, s)
	if err != nil || input.Rebase == nil || input.Rebase.BaseRevision != originalBase || input.Rebase.HeadRevision != f.s.Head || input.SourceRevision != source.main || input.Context.Digest != f.s.ContextDigest {
		t.Fatalf("rebase did not retain its pinned history and context: %+v %v", input, err)
	}
	prompt, err := enginePrompt(input)
	if err != nil || !strings.Contains(string(prompt), "rebase --onto") || !strings.Contains(string(prompt), "abort the rebase") {
		t.Fatalf("unsafe rebase instruction: %v", err)
	}
	before, _ := json.Marshal(s)
	if err := f.c.save(f.ctx, f.e, s); err != nil {
		t.Fatal(err)
	}
	recovered, err := f.c.load(f.ctx, f.e)
	after, _ := json.Marshal(recovered)
	if err != nil || string(before) != string(after) || operationKey(f.e, s, "review") == operationKey(f.e, f.s, "review") {
		t.Fatal("rebase recovery changed durable state or reused an old gate")
	}
	e, err := f.journal.GetExecution(f.ctx, f.e.ID)
	if err != nil || e.MaxAttempts != f.e.MaxAttempts || !e.DeadlineAt.Equal(f.e.DeadlineAt) {
		t.Fatalf("rebase changed original authority: %+v %v", e, err)
	}
}

func TestRebaseCannotExtendOriginalRuntimeBudget(t *testing.T) {
	f, p, r := newRepairFlow(t, 2)
	source := &repairSource{main: strings.Repeat("d", 40), head: f.s.Head}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	if err := f.c.beginRebase(f.ctx, f.e, p.run, f.s, source.main); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.blocked, "original budget") || len(r.starts) != 0 || source.lands != 0 {
		t.Fatal("insufficient runtime budget admitted a rebase")
	}
}
