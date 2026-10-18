package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func newLandedCompletionFixture(t *testing.T) (*Store, domain.Task, domain.Run, []string, domain.LandingObservation) {
	t.Helper()
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "landing.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	bindRepository(t, s, "prj_default", "example/repo", 1)
	budget := domain.Budget{TimeMinutes: 30, MaxAttempts: 3, Authority: []string{"source.read", "source.write", "source.fast_forward"}}
	task, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Land the authorized scope", AcceptanceCriteria: "The original criteria pass", Status: domain.TaskReady, Budget: &budget})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RegisterAgent(ctx, domain.Agent{ID: "landing", Name: "Landing agent"}); err != nil {
		t.Fatal(err)
	}
	run, err := s.ClaimTask(ctx, testMutation("landing-claim"), task.ID, "landing", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitPlan(ctx, testMutation("landing-plan"), run.ID, "Implement and land", false); err != nil {
		t.Fatal(err)
	}
	if err = s.BeginVerification(ctx, testMutation("landing-verify"), run.ID); err != nil {
		t.Fatal(err)
	}
	run, err = s.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	landing := domain.LandingObservation{Repository: "example/repo", BaseBranch: "main", Commit: strings.Repeat("a", 40), ObservedAt: nowMillis()}
	ids := []string{}
	for _, kind := range []string{"test", "semantic_review", "landing"} {
		item, err := s.RecordEvidence(ctx, domain.EvidenceInput{ID: "landing-" + kind, RunID: run.ID, PlanRevisionID: *run.ActivePlanRevisionID, Type: kind, Producer: "builder", Verifier: "independent-" + kind, SubjectDigest: landing.Commit, SourceRevision: landing.Commit, Command: kind, Outcome: "passed", ProvenanceURI: "artifact://" + kind, ProvenanceDigest: "sha256:" + kind, ArtifactURI: "artifact://" + kind, ArtifactDigest: "sha256:" + kind, ObservedAt: nowMillis()})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, item.ID)
	}
	return s, task, run, ids, landing
}

func TestLandedCompletionCannotCompleteEditedCriteriaUsingFrozenRunEvidence(t *testing.T) {
	s, task, run, ids, landing := newLandedCompletionFixture(t)
	ctx := context.Background()
	changed := "Different criteria require another authorized run"
	if _, err := s.UpdateTask(ctx, task.ID, domain.UpdateTaskInput{AcceptanceCriteria: &changed}); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteLandedRun(ctx, testMutation("landed-complete"), run.ID, "Original criteria landed", ids, landing); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("old-scope evidence completed the edited task: %v", err)
	}
	current, err := s.GetTask(ctx, task.ID)
	if err != nil || current.Status == domain.TaskDone || current.Landing != nil {
		t.Fatalf("rejected landing changed task completion: %+v %v", current, err)
	}
	active, err := s.GetRun(ctx, run.ID)
	if err != nil || active.Status != domain.RunVerifying {
		t.Fatalf("rejected landing changed active run: %+v %v", active, err)
	}
}

func TestImportedLandingCannotUseChangedScopeOrEvidenceFromAnotherGate(t *testing.T) {
	ctx := context.Background()
	s, task, run, ids, landing := newLandedCompletionFixture(t)
	if err := s.CompleteLandedRun(ctx, testMutation("landed-complete"), run.ID, "Landed", ids, landing); err != nil {
		t.Fatal(err)
	}
	exported, err := s.ExportProject(ctx, task.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name   string
		change func(*portableBundle)
	}{
		{"changed criteria", func(bundle *portableBundle) { bundle.Tasks[0].Task.AcceptanceCriteria = "Different unverified scope" }},
		{"another plan", func(bundle *portableBundle) {
			other := bundle.PlanRevisions[0]
			other.ID, other.Revision = "different-plan", other.Revision+1
			bundle.PlanRevisions = append(bundle.PlanRevisions, other)
			bundle.Evidence[0].PlanRevisionID = other.ID
		}},
		{"another policy", func(bundle *portableBundle) { bundle.PlanRevisions[0].PolicyVersion = "changed-policy" }},
		{"another source", func(bundle *portableBundle) { bundle.PlanRevisions[0].SourceRevision = strings.Repeat("b", 40) }},
		{"expired before landing", func(bundle *portableBundle) {
			expired := landing.ObservedAt - 1
			bundle.Evidence[0].ExpiresAt = &expired
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var bundle portableBundle
			if err := json.Unmarshal(exported, &bundle); err != nil {
				t.Fatal(err)
			}
			scenario.change(&bundle)
			data, err := json.Marshal(bundle)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := Open(ctx, filepath.Join(t.TempDir(), "restored.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			if err := restored.ImportProject(ctx, data); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("inconsistent landing import could grant prerequisite availability: %v", err)
			}
			if _, err := restored.GetTask(ctx, task.ID); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("failed import partially retained the landed task: %v", err)
			}
		})
	}
}

func TestHistoricalLandingImportRetainsEvidenceThatExpiredAfterCompletion(t *testing.T) {
	ctx := context.Background()
	s, task, run, ids, landing := newLandedCompletionFixture(t)
	if err := s.CompleteLandedRun(ctx, testMutation("landed-complete"), run.ID, "Landed", ids, landing); err != nil {
		t.Fatal(err)
	}
	exported, err := s.ExportProject(ctx, task.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	var bundle portableBundle
	if err := json.Unmarshal(exported, &bundle); err != nil {
		t.Fatal(err)
	}
	// Move the retained history back one day while keeping evidence valid at
	// original completion. It is readable history, not current authority.
	completedAt := *bundle.Runs[0].FinishedAt - 24*60*60*1000
	landingAt := landing.ObservedAt - 24*60*60*1000
	bundle.Runs[0].FinishedAt = &completedAt
	bundle.Tasks[0].Task.Landing.ObservedAt = landingAt
	for i := range bundle.Evidence {
		expiredAt := completedAt + 60_000
		bundle.Evidence[i].ExpiresAt = &expiredAt
	}
	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Open(ctx, filepath.Join(t.TempDir(), "restored.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.ImportProject(ctx, data); err != nil {
		t.Fatalf("valid retained history could not be imported: %v", err)
	}
	copy, err := restored.GetTask(ctx, task.ID)
	if err != nil || copy.Landing == nil || copy.Landing.Commit != landing.Commit {
		t.Fatalf("historical landing was lost: %+v %v", copy, err)
	}
}

func TestLandedDependencyRequiresUnexpiredSelectedEvidence(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	bindRepository(t, s, "prj_default", "example/repo", 1)
	budget := domain.Budget{TimeMinutes: 30, MaxAttempts: 3, Authority: []string{"source.read", "source.write", "source.fast_forward"}}
	first, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Land prerequisite", AcceptanceCriteria: "Verified", Status: domain.TaskReady, Budget: &budget})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Uses prerequisite"})
	if err != nil {
		t.Fatal(err)
	}
	second, err = s.SetTaskDependencies(ctx, second.ID, second.Version, []string{first.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.MoveTask(ctx, second.ID, domain.TaskReady); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RegisterAgent(ctx, domain.Agent{ID: "landing", Name: "Landing agent"}); err != nil {
		t.Fatal(err)
	}
	run, err := s.ClaimTask(ctx, testMutation("landing-claim"), first.ID, "landing", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitPlan(ctx, testMutation("landing-plan"), run.ID, "Implement and land", false); err != nil {
		t.Fatal(err)
	}
	if err = s.BeginVerification(ctx, testMutation("landing-verify"), run.ID); err != nil {
		t.Fatal(err)
	}
	run, err = s.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("a", 40)
	ids := []string{}
	for _, kind := range []string{"test", "semantic_review", "landing"} {
		item, err := s.RecordEvidence(ctx, domain.EvidenceInput{ID: "landing-" + kind, RunID: run.ID, PlanRevisionID: *run.ActivePlanRevisionID, Type: kind, Producer: "builder", Verifier: "independent-" + kind, SubjectDigest: commit, SourceRevision: commit, Command: kind, Outcome: "passed", ProvenanceURI: "artifact://" + kind, ProvenanceDigest: "sha256:" + kind, ArtifactURI: "artifact://" + kind, ArtifactDigest: "sha256:" + kind, ObservedAt: nowMillis()})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, item.ID)
	}
	if err = s.CompleteLandedRun(ctx, testMutation("landed-complete"), run.ID, "Landed", ids, domain.LandingObservation{Repository: "example/repo", BaseBranch: "main", Commit: commit, ObservedAt: nowMillis()}); err != nil {
		t.Fatal(err)
	}
	if satisfied, err := s.TaskDependenciesSatisfied(ctx, second.ID); err != nil || !satisfied {
		t.Fatalf("current landing must release prerequisite: %v %v", satisfied, err)
	}
	// Expiry can occur after completion without an explicit admission-status edit.
	if _, err = s.db.ExecContext(ctx, "UPDATE evidence_records SET expires_at = ? WHERE id = ?", nowMillis()-1, ids[0]); err != nil {
		t.Fatal(err)
	}
	if satisfied, err := s.TaskDependenciesSatisfied(ctx, second.ID); err != nil || satisfied {
		t.Fatalf("expired landing evidence released prerequisite: %v %v", satisfied, err)
	}
	if _, err = s.NextReadyTask(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("discovered dependent with expired evidence: %v", err)
	}
	if _, err = s.ClaimTask(ctx, testMutation("expired-claim"), second.ID, "landing", time.Minute); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("claimed dependent with expired evidence: %v", err)
	}
}

func TestLandingRequiresTheAuthorizedRepositoryAndIndependentEvidenceOnTheCommit(t *testing.T) {
	s, task, run, ids, landing := newLandedCompletionFixture(t)
	ctx := context.Background()
	guard := func(key string) Mutation {
		current, err := s.GetRun(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		m := testMutation(key)
		m.Guard = &RunGuard{AgentID: current.AgentID, ExpectedVersion: current.Version, FenceToken: current.FenceToken}
		return m
	}
	wrongCommit, wrongRepository := landing, landing
	wrongCommit.Commit = strings.Repeat("b", 40)
	wrongRepository.Repository = "other/repo"
	for name, attempt := range map[string]func() error{
		"wrong commit": func() error { return s.CompleteLandedRun(ctx, guard("commit"), run.ID, "landed", ids, wrongCommit) },
		"wrong repository": func() error {
			return s.CompleteLandedRun(ctx, guard("repository"), run.ID, "landed", ids, wrongRepository)
		},
		"missing landing": func() error { return s.CompleteLandedRun(ctx, guard("missing"), run.ID, "landed", ids[:2], landing) },
	} {
		if err := attempt(); err == nil {
			t.Fatalf("%s completed the task", name)
		}
	}
	if err := s.CompleteLandedRun(ctx, guard("landed"), run.ID, "landed", ids, landing); err != nil {
		t.Fatal(err)
	}
	done, err := s.GetTask(ctx, task.ID)
	if err != nil || done.Status != domain.TaskDone || done.Landing == nil || done.Review != nil {
		t.Fatalf("landed task = %+v, %v", done, err)
	}
}
