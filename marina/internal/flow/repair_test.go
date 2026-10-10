package flow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

type repairProvider struct {
	Provider
	run           workcontrol.Run
	work          workcontrol.WorkItem
	evidence      map[string]workcontrol.EvidenceSubmission
	blocked       string
	loseResponses bool
}

func (p *repairProvider) GetWork(context.Context, string) (workcontrol.WorkDetail, error) {
	return workcontrol.WorkDetail{Work: p.work, Runs: []workcontrol.Run{p.run}}, nil
}

func (p *repairProvider) RecordEvidence(_ context.Context, _ string, _ workcontrol.Mutation, in workcontrol.EvidenceSubmission) (string, error) {
	if earlier, ok := p.evidence[in.ID]; ok {
		before, _ := json.Marshal(earlier)
		after, _ := json.Marshal(in)
		if string(before) != string(after) {
			return "", errors.New("evidence replay changed its content")
		}
		return in.ID, nil
	}
	p.evidence[in.ID] = in
	if p.loseResponses {
		return "", context.DeadlineExceeded
	}
	return in.ID, nil
}

func (p *repairProvider) BlockRun(_ context.Context, _ string, in workcontrol.RunBlock) error {
	p.blocked = in.Reason
	p.run.Status = workcontrol.RunBlocked
	return nil
}

func (p *repairProvider) BeginVerification(context.Context, string, workcontrol.Mutation) error {
	p.run.Status = workcontrol.RunVerifying
	return nil
}

func (p *repairProvider) CompleteRun(_ context.Context, _ string, _ workcontrol.Mutation, _ string, evidence []string) error {
	for _, id := range evidence {
		if p.evidence[id].Outcome != "passed" {
			return errors.New("completion used failed or missing evidence")
		}
	}
	p.run.Status = workcontrol.RunCompleted
	return nil
}

func (p *repairProvider) CompleteLandedRun(ctx context.Context, id string, guard workcontrol.Mutation, summary string, evidence []string, landing workcontrol.LandingObservation) error {
	for _, selected := range evidence {
		if p.evidence[selected].SourceRevision != landing.Commit {
			return errors.New("completion selected evidence from another commit")
		}
	}
	return p.CompleteRun(ctx, id, guard, summary, evidence)
}

type repairRuntime struct {
	rt.Adapter
	f                    engineFlow
	starts               []rt.Specification
	exitCode             int
	alwaysFail           bool
	approach             string
	beforeImplementation func(rt.Specification) error
	changePlan           func(*workresult.RepairPlan)
	failReviews          int
	changeReview         func(*workresult.Result)
	loseStarts           bool
	observations         map[string]rt.Observation
}

func (r *repairRuntime) InspectByKey(_ context.Context, key string) (rt.Observation, error) {
	return r.observations[key], nil
}

func (r *repairRuntime) Start(ctx context.Context, spec rt.Specification) (rt.Observation, error) {
	r.starts = append(r.starts, spec)
	stage := "implement"
	if strings.HasSuffix(spec.IdempotencyKey, ":verify") {
		stage = "verify"
	} else if strings.HasSuffix(spec.IdempotencyKey, ":repair_plan") {
		stage = "repair_plan"
	} else if spec.Stage == "review" {
		stage = "review"
	}
	result := workresult.Result{Stage: stage, SourceRevision: spec.SourceRevision, ObservedAt: time.Now().UTC(), Summary: "Use the initial algorithm"}
	s, err := r.f.c.load(ctx, r.f.e)
	if err != nil {
		return rt.Observation{}, err
	}
	switch stage {
	case "implement":
		if r.beforeImplementation != nil {
			if err := r.beforeImplementation(spec); err != nil {
				return rt.Observation{}, err
			}
		}
		result.Changes = []workresult.Change{{Path: "src/fix.go", Mode: "100644", Content: []byte(fmt.Sprintf("revision %d", len(r.starts)))}}
	case "repair_plan":
		approach := r.approach
		if approach == "" {
			approach = fmt.Sprintf("Use approach %d", len(s.Failures))
		}
		result.Repair = &workresult.RepairPlan{Diagnosis: "The regression exposes a boundary error", Approach: approach,
			FailureDigest: s.Failures[len(s.Failures)-1].ArtifactDigest, CanRepair: true}
		if r.changePlan != nil {
			r.changePlan(result.Repair)
		}
	case "review":
		candidate, err := r.f.c.candidate(s)
		if err != nil {
			return rt.Observation{}, err
		}
		result.InputDigest, result.ProfileDigest = spec.InputDigest, spec.ProfileDigest
		result.Review = &workresult.Review{PatchDigest: candidate.Digest()}
		for _, id := range workresult.ReviewCheckIDs {
			outcome := "passed"
			if id == "tests" && r.failReviews > 0 {
				outcome = "failed"
			}
			result.Review.Checks = append(result.Review.Checks, workresult.ReviewCheck{ID: id, Outcome: outcome,
				Reason: "The candidate preserves regression coverage", Citations: []workresult.Citation{{Path: "src/fix.go", Line: 1}}})
		}
		if r.failReviews > 0 {
			r.failReviews--
		}
		if r.changeReview != nil {
			r.changeReview(&result)
		}
	case "verify":
		check := r.f.c.Config.Checks[0]
		exit := r.exitCode
		if len(s.Failures) > 0 && !r.alwaysFail {
			exit = 0
		}
		result.Checks = []workresult.CheckResult{{ID: check.ID, Directory: check.Directory, Command: check.Command,
			ExitCode: exit, Output: "regression output", OutputDigest: artifacts.Digest([]byte("regression output"))}}
	}
	b, _ := json.Marshal(result)
	digest, err := r.f.c.Artifacts.Put(b)
	if err != nil {
		return rt.Observation{}, err
	}
	_, _, err = r.f.c.Journal.RecordRuntimeArtifact(ctx, rt.Artifact{ID: "result:" + spec.AttemptID, ExecutionID: spec.ExecutionID,
		RuntimeAttemptID: spec.AttemptID, Name: stage + ".json", Kind: "worker_result", URI: r.f.c.artifactURL(digest), Digest: digest, SizeBytes: int64(len(b))})
	if err != nil {
		return rt.Observation{}, err
	}
	observation := rt.Observation{Found: true, ProviderHandle: "job:" + spec.AttemptID, State: rt.StateSucceeded}
	if r.observations == nil {
		r.observations = map[string]rt.Observation{}
	}
	r.observations[spec.IdempotencyKey] = observation
	if r.loseStarts {
		return rt.Observation{}, context.DeadlineExceeded
	}
	return observation, nil
}

func (r *repairRuntime) Cleanup(context.Context, string) error { return nil }
func (r *repairRuntime) CollectArtifacts(context.Context, string) ([]rt.Artifact, error) {
	return nil, nil
}

type repairSource struct {
	sourcecontrol.Port
	head    string
	main    string
	writes  map[string]sourcecontrol.Revision
	ciFails bool
	lands   int
}

func (s *repairSource) ReadReviewFile(context.Context, string, string, string) ([]byte, bool, error) {
	return []byte("package fixture\n"), true, nil
}

func (s *repairSource) CurrentHead(_ context.Context, request sourcecontrol.HeadRequest) (sourcecontrol.Revision, error) {
	if request.Ref == "main" {
		return sourcecontrol.Revision{Commit: s.main}, nil
	}
	return sourcecontrol.Revision{Commit: s.head}, nil
}

func (s *repairSource) CreateBranch(_ context.Context, request sourcecontrol.BranchRequest) (sourcecontrol.Revision, error) {
	s.head = request.BaseRevision
	return sourcecontrol.Revision{Commit: request.BaseRevision}, nil
}

func (s *repairSource) WriteChanges(_ context.Context, request sourcecontrol.WriteRequest) (sourcecontrol.Revision, error) {
	if revision, ok := s.writes[request.IdempotencyKey]; ok {
		return revision, nil
	}
	if request.ExpectedHead != s.head {
		return sourcecontrol.Revision{}, errors.New("task branch changed")
	}
	s.head = fmt.Sprintf("%040d", len(s.writes)+1)
	revision := sourcecontrol.Revision{Commit: s.head}
	s.writes[request.IdempotencyKey] = revision
	return sourcecontrol.Revision{}, context.DeadlineExceeded // The commit exists; only its response was lost.
}

func (s *repairSource) ObserveChecks(_ context.Context, request sourcecontrol.ObservationRequest) ([]sourcecontrol.CheckObservation, error) {
	status := "success"
	if s.ciFails && len(s.writes) == 1 {
		status = "failure"
	}
	return []sourcecontrol.CheckObservation{{ID: "required", HeadRevision: request.HeadRevision, State: status, ObservedAt: time.Now().UTC()}}, nil
}

func (s *repairSource) FastForward(_ context.Context, request sourcecontrol.FastForwardRequest) (sourcecontrol.Revision, error) {
	if s.main == request.ExpectedHead {
		return sourcecontrol.Revision{Commit: s.main, Ref: "main"}, nil
	}
	if s.main != request.ExpectedBase || s.head != request.ExpectedHead {
		return sourcecontrol.Revision{}, errors.New("ref changed before landing")
	}
	s.main = s.head
	s.lands++
	return sourcecontrol.Revision{Commit: s.main, Ref: "main"}, nil
}

func newRepairFlow(t *testing.T, limit int) (engineFlow, *repairProvider, *repairRuntime) {
	t.Helper()
	f := newEngineFlow(t)
	e, _, err := f.journal.EnsureExecution(f.ctx, execution.NewExecution{ProjectID: f.e.ProjectID, WorkProvider: "deck",
		ExternalWorkID: f.e.ExternalWorkID, ExternalRunID: "repair-run", AgentID: f.e.AgentID, TaskVersion: f.e.TaskVersion,
		LeaseExpiresAt: f.deadline, Budget: execution.Budget{DeadlineAt: f.deadline, MaxAttempts: limit, MaxTokens: 20000}})
	if err != nil {
		t.Fatal(err)
	}
	f.e = e
	e, err = f.journal.Transition(f.ctx, e.ID, execution.Transition{ExpectedVersion: e.Version, Target: execution.StateRunning, Message: "Approved test task"})
	if err != nil {
		t.Fatal(err)
	}
	f.e = e
	criteria := sha256.Sum256([]byte(f.task.AcceptanceCriteria))
	p := &repairProvider{work: f.task, run: workcontrol.Run{ID: e.ExternalRunID, AgentID: e.AgentID, TaskVersion: e.TaskVersion, AcceptanceCriteriaDigest: hex.EncodeToString(criteria[:]),
		FenceToken: e.FenceToken, Version: e.ExternalRunVersion, Status: workcontrol.RunRunning, LeaseExpiresAt: &f.deadline},
		evidence: map[string]workcontrol.EvidenceSubmission{}}
	f.c.Provider = p
	manifest, err := f.c.assembleContext(time.Now().UTC(), e, f.task, f.s, workcontrol.ResolvedContext{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.journal.RecordContextPackage(f.ctx, manifest); err != nil {
		t.Fatal(err)
	}
	f.s.ContextDigest = manifest.Digest
	r := &repairRuntime{f: f, exitCode: 1}
	f.c.Runtime = r
	f.c.EngineRuntime, f.c.EngineTemplates = r, &flowTemplates{}
	f.c.Config.Delivery = "fast_forward"
	f.c.Config.RequiredCI = []string{"required"}
	f.s.ConfigDigest = f.c.Config.Digest()
	f.s.Branch = "marina/" + e.ID
	f.s.PlanID = "plan"
	return f, p, r
}

func TestFailedVerificationRecordsFailureBeforeRepairWork(t *testing.T) {
	f, p, r := newRepairFlow(t, 8)
	f.s.Step = "verify"
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
	if s.Step != "repair" || s.VerificationDigest == "" || p.blocked != "" || len(r.starts) != 1 {
		t.Fatalf("failed verification must persist its evidence and schedule repair: state=%+v blocked=%q starts=%d", s, p.blocked, len(r.starts))
	}
}

func TestFailedCIIsRetainedAsSourceBoundEvidenceBeforeRepair(t *testing.T) {
	f, p, r := newRepairFlow(t, 8)
	r.exitCode = 0
	source := &repairSource{head: f.s.Head, main: f.s.Base, writes: map[string]sourcecontrol.Revision{}, ciFails: true}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 30; cycle++ {
		e, err := f.journal.GetExecution(f.ctx, f.e.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.c.advance(f.ctx, e); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		s, err := f.c.load(f.ctx, e)
		if err != nil {
			t.Fatal(err)
		}
		if s.Step == "repair_plan" {
			for _, evidence := range p.evidence {
				if evidence.Type == "ci" && evidence.Outcome == "failed" && evidence.SourceRevision == source.head && evidence.ArtifactDigest == s.Failures[0].ArtifactDigest {
					return
				}
			}
			t.Fatal("repair started without retaining the failed CI observation in Deck")
		}
	}
	t.Fatal("CI repair was not scheduled")
}

func TestRepairPersistsDiagnosisBeforeImplementationAndUsesFreshEvidence(t *testing.T) {
	f, p, r := newRepairFlow(t, 8)
	source := &repairSource{head: f.s.Head, main: f.s.Base, writes: map[string]sourcecontrol.Revision{}}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	r.beforeImplementation = func(spec rt.Specification) error {
		s, err := f.c.load(f.ctx, f.e)
		if err != nil {
			return err
		}
		if len(s.Failures) > 0 && s.RepairPlanDigest == "" {
			return errors.New("implementation started without a saved diagnosis")
		}
		if spec.ContextDigest != f.s.ContextDigest || spec.DeadlineAt != f.e.DeadlineAt {
			return errors.New("repair changed frozen context or deadline")
		}
		return nil
	}
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 50; cycle++ {
		e, err := f.journal.GetExecution(f.ctx, f.e.ID)
		if err != nil {
			t.Fatal(err)
		}
		if e.State.Terminal() {
			break
		}
		if err := f.c.advance(f.ctx, e); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cycle %d: %v", cycle, err)
		}
	}
	e, err := f.journal.GetExecution(f.ctx, f.e.ID)
	if err != nil || e.State != execution.StateCompleted {
		t.Fatalf("repair did not complete: %+v %v blocker=%q", e, err, p.blocked)
	}
	if len(r.starts) != 7 || len(source.writes) != 2 || source.lands != 1 {
		t.Fatalf("duplicate or missing effects: starts=%d commits=%d lands=%d", len(r.starts), len(source.writes), source.lands)
	}
	evidence, err := f.journal.ListEvidence(f.ctx, e.ID)
	if err != nil || len(evidence) != 5 || evidence[1].Summary != "failed" || evidence[4].Summary != "passed" || evidence[4].SourceRevision != source.main || evidence[3].Kind != "landing" {
		t.Fatalf("evidence history/latest head mismatch: %+v %v", evidence, err)
	}
	for _, in := range p.evidence {
		if in.SourceRevision == source.main && in.Outcome != "passed" {
			t.Fatal("landed commit has stale evidence")
		}
	}
}

func TestCIRepairAndFailureLimitsSurviveRestartAtEveryBoundary(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		limit       int
		verifyFails bool
		ciFails     bool
		approach    string
		want        execution.State
		starts      int
		commits     int
		failures    int
	}{
		{"verification repair", 8, false, false, "", execution.StateCompleted, 7, 2, 1},
		{"CI repair", 8, false, true, "", execution.StateCompleted, 7, 2, 1},
		{"three failures", 20, true, false, "", execution.StateBlocked, 11, 3, 3},
		{"smaller task budget", 3, false, false, "", execution.StateBlocked, 3, 1, 1},
		{"repeated approach", 11, true, false, "Repeat the same repair", execution.StateBlocked, 8, 2, 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f, p, r := newRepairFlow(t, scenario.limit)
			p.loseResponses = true
			r.alwaysFail, r.approach = scenario.verifyFails, scenario.approach
			if scenario.ciFails {
				r.exitCode = 0
			}
			source := &repairSource{head: f.s.Head, main: f.s.Base, writes: map[string]sourcecontrol.Revision{}, ciFails: scenario.ciFails}
			f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
			if err := f.c.save(f.ctx, f.e, f.s); err != nil {
				t.Fatal(err)
			}
			// Reopen the same persisted journal between every state transition.
			path := f.path
			f.journal.Close()
			for cycle := 0; cycle < 80; cycle++ {
				journal, err := sqlite.Open(f.ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				f.journal, f.c.Journal = journal, journal
				e, err := journal.GetExecution(f.ctx, f.e.ID)
				if err != nil {
					t.Fatal(err)
				}
				if e.State.Terminal() {
					break
				}
				if err := f.c.advance(f.ctx, e); err != nil && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("cycle %d: %v", cycle, err)
				}
				journal.Close()
			}
			defer f.journal.Close()
			e, err := f.journal.GetExecution(f.ctx, f.e.ID)
			if err != nil || e.State != scenario.want {
				t.Fatalf("state=%+v want=%s err=%v blocker=%q", e, scenario.want, err, p.blocked)
			}
			s, err := f.c.load(f.ctx, e)
			if err != nil || len(s.Failures) != scenario.failures {
				t.Fatalf("failure history=%+v err=%v", s.Failures, err)
			}
			if len(r.starts) != scenario.starts || len(source.writes) != scenario.commits {
				t.Fatalf("effects: starts=%d commits=%d; want %d/%d", len(r.starts), len(source.writes), scenario.starts, scenario.commits)
			}
			if e.MaxAttempts != scenario.limit || !e.DeadlineAt.Equal(f.e.DeadlineAt) || e.MaxTokens != f.e.MaxTokens {
				t.Fatal("restart or repair expanded the original budget")
			}
			if scenario.want == execution.StateBlocked && source.lands != 0 {
				t.Fatal("failed attempt landed")
			}
		})
	}
}

func TestRepairProgressShowsTheSavedDiagnosisAndRemainingOriginalBudget(t *testing.T) {
	f, _, r := newRepairFlow(t, 8)
	// A diagnosis is visible before the implementation has any side effects.
	f.s.Failures = []repairFailure{{Number: 1, Stage: "verify", SourceRevision: f.s.Head,
		ArtifactDigest: artifacts.Digest([]byte("failed check")), Diagnosis: "An equality comparison misses the upper boundary",
		NextApproach: "Use an inclusive upper bound", PlanDigest: artifacts.Digest([]byte("plan"))}}
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	for {
		items, err := f.journal.ListReadyOutbox(f.ctx, 100)
		if err != nil || len(items) == 0 {
			t.Fatalf("progress not queued: %+v %v", items, err)
		}
		if items[0].Payload["type"] == "execution.progress" {
			payload = items[0].Payload
			break
		}
		if err := f.journal.MarkOutboxPublished(f.ctx, items[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	if payload["failure_count"] != float64(1) || payload["remaining_runtime_attempts"] != float64(8) ||
		payload["remaining_tokens"] != float64(f.e.MaxTokens) || payload["repair_plan_digest"] != f.s.Failures[0].PlanDigest ||
		!strings.Contains(fmt.Sprint(payload["message"]), f.s.Failures[0].Diagnosis) ||
		!strings.Contains(fmt.Sprint(payload["message"]), f.s.Failures[0].NextApproach) || len(r.starts) != 0 {
		t.Fatalf("operator cannot see the durable repair plan and original budget: %+v", payload)
	}
}

func TestRepairRejectsAnUnboundOrUnavailableApproach(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*workresult.RepairPlan)
	}{
		{"different failure", func(p *workresult.RepairPlan) { p.FailureDigest = artifacts.Digest([]byte("other failure")) }},
		{"no new approach", func(p *workresult.RepairPlan) { p.CanRepair = false }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f, p, r := newRepairFlow(t, 8)
			digest, err := f.c.Artifacts.Put([]byte("failure output"))
			if err != nil {
				t.Fatal(err)
			}
			f.s.Step = "repair_plan"
			f.s.Failures = []repairFailure{{Number: 1, Stage: "verify", SourceRevision: f.s.Head, ArtifactDigest: digest}}
			r.changePlan = scenario.change
			source := &repairSource{head: f.s.Head, main: f.s.Base, writes: map[string]sourcecontrol.Revision{}}
			f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
			if err := f.c.save(f.ctx, f.e, f.s); err != nil {
				t.Fatal(err)
			}
			if err := f.c.advance(f.ctx, f.e); err != nil {
				t.Fatal(err)
			}
			e, err := f.journal.GetExecution(f.ctx, f.e.ID)
			if err != nil || e.State != execution.StateBlocked || p.blocked == "" || len(r.starts) != 1 || len(source.writes) != 0 {
				t.Fatalf("invalid proposal admitted work: %+v starts=%d writes=%d blocker=%q err=%v", e, len(r.starts), len(source.writes), p.blocked, err)
			}
		})
	}
}

func TestRepairRetainsUncertainModelUsageWithoutStartingAnotherAttempt(t *testing.T) {
	f, p, r := newRepairFlow(t, 8)
	r.beforeImplementation = func(spec rt.Specification) error {
		engine, err := f.journal.EnsureEngineAttempt(f.ctx, spec)
		if err != nil {
			return err
		}
		engine.InstanceID, engine.ContextID, engine.Route, engine.Revision, engine.Phase = "instance", "context", "route", spec.Engine.Revision, "ready"
		engine, err = f.journal.UpdateEngineAttempt(f.ctx, engine)
		if err != nil {
			return err
		}
		engine.Phase = "sending"
		if _, err = f.journal.UpdateEngineAttempt(f.ctx, engine); err != nil {
			return err
		}
		_, err = f.journal.ReserveModelCall(f.ctx, f.e.ID, spec.AttemptID, f.e.FenceToken, 1000, 100)
		return err
	}
	source := &repairSource{head: f.s.Head, main: f.s.Base, writes: map[string]sourcecontrol.Revision{}}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 20; cycle++ {
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
	}
	totals, err := f.journal.ExecutionModelTotals(f.ctx, f.e.ID)
	if err != nil || totals.PendingCalls != 1 || totals.ReservedTokens != 1000 || totals.ReservedCostMicros != 100 ||
		len(r.starts) != 1 || len(source.writes) != 0 || source.lands != 0 || !strings.Contains(p.blocked, "uncertain") {
		t.Fatalf("uncertain usage admitted a repair or lost its reservation: %+v starts=%d commits=%d blocker=%q err=%v", totals, len(r.starts), len(source.writes), p.blocked, err)
	}
}

func TestRepairedTaskCannotCompleteIfItsBranchMovesAfterLanding(t *testing.T) {
	f, p, _ := newRepairFlow(t, 8)
	source := &repairSource{head: f.s.Head, main: f.s.Base, writes: map[string]sourcecontrol.Revision{}}
	f.c.Source = func(execution.Execution) (sourcecontrol.Port, error) { return source, nil }
	if err := f.c.save(f.ctx, f.e, f.s); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 45; cycle++ {
		e, err := f.journal.GetExecution(f.ctx, f.e.ID)
		if err != nil {
			t.Fatal(err)
		}
		s, err := f.c.load(f.ctx, e)
		if err != nil {
			t.Fatal(err)
		}
		if s.Step == "evidence" {
			source.head = strings.Repeat("f", 40) // An external writer moves the task branch after landing.
			if err := f.c.advance(f.ctx, e); err != nil {
				t.Fatal(err)
			}
			e, err = f.journal.GetExecution(f.ctx, e.ID)
			if err != nil || e.State != execution.StateBlocked || p.blocked == "" || source.lands != 1 {
				t.Fatalf("stale repaired head admitted completion evidence: %+v blocker=%q lands=%d err=%v", e, p.blocked, source.lands, err)
			}
			return
		}
		if err := f.c.advance(f.ctx, e); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}
	t.Fatal("repair never reached the landing evidence boundary")
}
