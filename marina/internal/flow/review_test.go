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
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

func TestCandidateIsHeldForReviewBeforeBranchOrCommit(t *testing.T) {
	f, p, runtime := newRepairFlow(t, 11)
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	if err := f.c.advance(f.ctx, f.e); err != nil {
		t.Fatal(err)
	}
	s, err := f.c.load(f.ctx, f.e)
	if err != nil || s.Step != "review" || s.ImplementationDigest == "" || len(runtime.starts) != 1 || p.blocked != "" {
		t.Fatalf("candidate must wait for independent review: state=%+v starts=%d blocker=%q err=%v", s, len(runtime.starts), p.blocked, err)
	}
}

func advanceReviewFlow(t *testing.T, f *engineFlow, cycles int, stop func(state) bool) state {
	t.Helper()
	for range cycles {
		e, err := f.journal.GetExecution(f.ctx, f.e.ID)
		if err != nil {
			t.Fatal(err)
		}
		s, err := f.c.load(f.ctx, e)
		if err != nil {
			t.Fatal(err)
		}
		if e.State.Terminal() || stop != nil && stop(s) {
			return s
		}
		if err := f.c.advance(f.ctx, e); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}
	t.Fatal("review flow did not reach its expected boundary")
	return state{}
}

func TestRejectedReviewIsEvidenceBeforeAnyCommitAndFeedsDiagnosis(t *testing.T) {
	f, p, runtime := newRepairFlow(t, 11)
	runtime.failReviews, runtime.exitCode = 1, 0
	source := &repairSource{head: f.s.Head, main: f.s.Base, writes: map[string]sourcecontrol.Revision{}}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	s := advanceReviewFlow(t, &f, 30, func(s state) bool { return s.Step == "repair_plan" })
	if len(source.writes) != 0 || len(s.Failures) != 1 || s.Failures[0].Stage != "review" || len(s.Failures[0].EvidenceIDs) != 1 || p.blocked != "" {
		t.Fatalf("rejected patch reached publication or lost its failure: %+v commits=%d blocker=%q", s, len(source.writes), p.blocked)
	}
	failed := p.evidence[s.Failures[0].EvidenceIDs[0]]
	if failed.Type != "semantic_review" || failed.Outcome != "failed" || failed.SourceRevision != f.s.Base {
		t.Fatalf("review failure was not retained: %+v", failed)
	}
	advanceReviewFlow(t, &f, 50, nil)
	e, _ := f.journal.GetExecution(f.ctx, f.e.ID)
	if e.State != execution.StateCompleted || len(source.writes) != 1 || len(runtime.starts) != 6 || source.lands != 1 {
		t.Fatalf("review repair failed: %s starts=%d commits=%d blocker=%q", e.State, len(runtime.starts), len(source.writes), p.blocked)
	}
}

func TestReviewCannotAuthorizeAChangedPatchBaseOrProducerIdentity(t *testing.T) {
	for _, scenario := range []string{"patch", "base", "producer"} {
		t.Run(scenario, func(t *testing.T) {
			f, p, _ := newRepairFlow(t, 11)
			source := &repairSource{head: f.s.Head, main: f.s.Base, writes: map[string]sourcecontrol.Revision{}}
			f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
			if err := f.c.save(f.ctx, f.e, f.s); err != nil {
				t.Fatal(err)
			}
			s := advanceReviewFlow(t, &f, 20, func(s state) bool { return s.Step == "branch" })
			s.Step = "publish"
			switch scenario {
			case "patch":
				result, err := f.c.checkedResult(s.ImplementationDigest, workresult.Expected{Stage: "implement", SourceRevision: s.Head, AllowedPaths: f.c.Config.AllowedPaths})
				if err != nil {
					t.Fatal(err)
				}
				result.Changes[0].Content = []byte("unreviewed change")
				data, _ := json.Marshal(result)
				s.ImplementationDigest, err = f.c.Artifacts.Put(data)
				if err != nil {
					t.Fatal(err)
				}
			case "base":
				s.Head = strings.Repeat("c", 40)
			case "producer":
				s.Reviewer = s.Implementer
			}
			if err := f.c.save(f.ctx, f.e, s); err != nil {
				t.Fatal(err)
			}
			e, _ := f.journal.GetExecution(f.ctx, f.e.ID)
			if err := f.c.advance(f.ctx, e); err != nil {
				t.Fatal(err)
			}
			if len(source.writes) != 0 || p.blocked == "" {
				t.Fatalf("changed candidate committed: %+v", s)
			}
		})
	}
}

func TestMalformedReviewBlocksWithoutCountingAnImplementationFailure(t *testing.T) {
	f, p, runtime := newRepairFlow(t, 11)
	runtime.changeReview = func(r *workresult.Result) { r.Review.Checks = r.Review.Checks[:2] }
	source := &repairSource{head: f.s.Head, main: f.s.Base, writes: map[string]sourcecontrol.Revision{}}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	s := advanceReviewFlow(t, &f, 20, nil)
	if len(s.Failures) != 0 || len(source.writes) != 0 || p.blocked == "" {
		t.Fatalf("malformed verdict treated as a pass or repair: %+v", s)
	}
}

func TestReviewFailureLimitAndExactCommitEvidenceSurviveLostResponsesAndRestart(t *testing.T) {
	for _, rejects := range []int{1, 3} {
		t.Run(string(rune('0'+rejects)), func(t *testing.T) {
			f, p, runtime := newRepairFlow(t, 20)
			runtime.failReviews, runtime.exitCode, runtime.loseStarts, p.loseResponses = rejects, 0, true, true
			source := &repairSource{head: f.s.Head, main: f.s.Base, writes: map[string]sourcecontrol.Revision{}}
			f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
			if err := f.c.save(f.ctx, f.e, f.s); err != nil {
				t.Fatal(err)
			}
			for range 100 {
				e, err := f.journal.GetExecution(f.ctx, f.e.ID)
				if err != nil {
					t.Fatal(err)
				}
				if e.State.Terminal() {
					break
				}
				if err := f.c.advance(f.ctx, e); err != nil && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
				f.journal.Close()
				f.journal, err = sqlite.Open(f.ctx, f.path)
				if err != nil {
					t.Fatal(err)
				}
				f.c.Journal = f.journal
			}
			defer f.journal.Close()
			e, _ := f.journal.GetExecution(f.ctx, f.e.ID)
			s, _ := f.c.load(f.ctx, e)
			if len(s.Failures) != rejects || e.MaxAttempts != 20 || !e.DeadlineAt.Equal(f.e.DeadlineAt) {
				t.Fatalf("review changed history or budget: %+v %+v", e, s)
			}
			if rejects == 3 {
				if e.State != execution.StateBlocked || len(source.writes) != 0 || len(runtime.starts) != 8 || source.lands != 0 {
					t.Fatalf("three failures did not stop: %+v blocker=%q", s, p.blocked)
				}
			} else {
				if e.State != execution.StateCompleted || len(source.writes) != 1 || len(runtime.starts) != 6 || source.lands != 1 {
					t.Fatalf("repair duplicated work: %+v blocker=%q", s, p.blocked)
				}
				passed := p.evidence[s.ReviewEvidenceID]
				data, err := f.c.Artifacts.Get(passed.ArtifactDigest)
				if err != nil {
					t.Fatal(err)
				}
				var record struct{ Commit, BaseRevision, Model, ProfileDigest, CandidateDigest string }
				if json.Unmarshal(data, &record) != nil || record.Commit != source.main || record.BaseRevision != f.s.Base || record.Model != f.c.Config.ReviewerModel.Name || record.ProfileDigest != f.c.Config.Digest() || passed.SubjectDigest != record.CandidateDigest || passed.Verifier == passed.Producer {
					t.Fatalf("review evidence lost source/model identity: %s %+v", data, passed)
				}
			}
		})
	}
}

func TestReviewFailureDisputedWithEvidenceEscalates(t *testing.T) {
	f, p, runtime := newRepairFlow(t, 11)
	runtime.failReviews = 1
	runtime.changePlan = func(plan *workresult.RepairPlan) {
		plan.CanRepair, plan.Disputed = false, true
		plan.Diagnosis = "The requested review change conflicts with the frozen acceptance criterion"
	}
	source := &repairSource{head: f.s.Head, main: f.s.Base, writes: map[string]sourcecontrol.Revision{}}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	advanceReviewFlow(t, &f, 30, nil)
	if len(runtime.starts) != 3 || len(source.writes) != 0 || !strings.Contains(p.blocked, "conflicts with the frozen") {
		t.Fatalf("disputed finding looped: starts=%d commits=%d blocker=%q", len(runtime.starts), len(source.writes), p.blocked)
	}
}

func TestReviewRequiresCompleteInputWithinItsBound(t *testing.T) {
	f, p, runtime := newRepairFlow(t, 11)
	source := &repairSource{head: f.s.Head, main: f.s.Base, writes: map[string]sourcecontrol.Revision{}}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	result := workresult.Result{Stage: "implement", SourceRevision: f.s.Head, ObservedAt: time.Now().UTC(), Changes: []workresult.Change{{Path: "src/large.go", Mode: "100644", Content: []byte(strings.Repeat("x", workresult.MaxReviewInputBytes))}}}
	data, _ := json.Marshal(result)
	f.s.ImplementationDigest, _ = f.c.Artifacts.Put(data)
	f.s.Step = "review"
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	advanceReviewFlow(t, &f, 10, nil)
	if len(runtime.starts) != 0 || len(source.writes) != 0 || !strings.Contains(p.blocked, "256 KiB") {
		t.Fatalf("oversized review was truncated or dispatched: %q", p.blocked)
	}
}

func TestUnreviewedPatchCannotPublish(t *testing.T) {
	f, p, _ := newRepairFlow(t, 11)
	source := &repairSource{head: f.s.Head, main: f.s.Base, writes: map[string]sourcecontrol.Revision{}}
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
	s.Step = "publish"
	if err := f.c.save(f.ctx, f.e, s); err != nil {
		t.Fatal(err)
	}
	_ = f.c.advance(f.ctx, f.e)
	if len(source.writes) != 0 || p.blocked == "" {
		t.Fatalf("publication bypassed review: commits=%d blocker=%q", len(source.writes), p.blocked)
	}
}

// A workload may turn review off; the clean verification Job remains the
// independent evidence, so the run still lands only after checks and CI.
func TestWorkloadWithoutReviewCommitsTheCheckedImplementationAndLands(t *testing.T) {
	f, p, runtime := newRepairFlow(t, 11)
	f.c.Config.SkipReview = true
	runtime.exitCode = 0
	source := &repairSource{head: f.s.Head, main: f.s.Base, writes: map[string]sourcecontrol.Revision{}}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	f.s.ConfigDigest = f.c.Config.Digest()
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	advanceReviewFlow(t, &f, 50, nil)
	e, _ := f.journal.GetExecution(f.ctx, f.e.ID)
	if e.State != execution.StateCompleted || len(runtime.starts) != 2 || len(source.writes) != 1 || source.lands != 1 || p.blocked != "" {
		t.Fatalf("reviewless run: %s starts=%d commits=%d lands=%d blocker=%q", e.State, len(runtime.starts), len(source.writes), source.lands, p.blocked)
	}
	for _, item := range p.evidence {
		if item.Type == "semantic_review" {
			t.Fatalf("review evidence recorded without a review: %+v", item)
		}
	}
}
