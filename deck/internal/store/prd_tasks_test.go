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

func prdTasksFixture(t *testing.T) (*Store, domain.Task, domain.ConversationInvocation, domain.WorkloadProfile) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	channel, err := s.CreateChannel(ctx, "prj_default", "prd-tasks")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RegisterAgent(ctx, domain.Agent{ID: "chat", Name: "Chat", InstallationID: "chat", Capabilities: []string{"conversation.invoke.prj_default"}})
	if err != nil {
		t.Fatal(err)
	}
	req, err := s.RequestRepository(ctx, "prj_default", domain.RepositoryRequestInput{Repository: "example/repo", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	err = s.RecordRepositoryResult(ctx, "prj_default", domain.RepositoryResult{RequestRevision: req.Revision, Status: "connected", Binding: &domain.RepositoryBinding{Version: 1, Repository: "example/repo", CloneURL: "https://github.com/example/repo.git", BaseBranch: "main"}})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "chat", AuthorID: "operator", RequestID: "draft", Body: "Draft a saved filters PRD"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.PublishConversationOutcome(ctx, domain.ConversationOutcomeInput{InvocationMessageID: inv.Message.ID, AgentID: "chat", Status: "completed", Body: "Review this PRD", ResultDigest: "draft", PRD: &domain.PRDDraft{Title: "Saved filters", Path: "docs/prd/filters.md", Content: "# Saved filters\n\nSave and restore filters.\n"}})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := s.GetChannelMessage(ctx, out.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	profile := domain.WorkloadProfile{ID: "repository/v2", Digest: strings.Repeat("a", 64), Harness: "codex/v1", Authority: []string{"source.read", "source.write", "source.fast_forward"}, MaxTokens: 30000, BudgetMinutes: 30, MinAttempts: 3}
	err = s.PublishWorkloads(ctx, "prj_default", "test", domain.WorkloadCatalog{Profiles: []domain.WorkloadProfile{profile}, ExpiresAt: time.Now().Add(time.Minute).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.ApprovePRD(ctx, msg.ID, prdMutation("publish"), domain.PRDApproval{Digest: msg.PRD.Digest, ProfileDigest: profile.Digest, Budget: domain.Budget{TimeMinutes: 30, MaxTokens: 20000, MaxAttempts: 3, MaxConcurrency: 1, Authority: profile.Authority}})
	if err != nil {
		t.Fatal(err)
	}
	return s, task, inv, profile
}

func landPRDTask(t *testing.T, s *Store, task domain.Task, commit string) {
	t.Helper()
	ctx := context.Background()
	run, err := s.ClaimTask(ctx, testMutation("claim-"+task.ID), task.ID, "chat", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SubmitPlan(ctx, testMutation("plan-"+task.ID), run.ID, "Implement scope", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BeginVerification(ctx, testMutation("verify-"+task.ID), run.ID); err != nil {
		t.Fatal(err)
	}
	run, err = s.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, kind := range []string{"test", "semantic_review", "landing"} {
		e, err := s.RecordEvidence(ctx, domain.EvidenceInput{ID: task.ID + kind, RunID: run.ID, PlanRevisionID: *run.ActivePlanRevisionID, Type: kind, Producer: "builder", Verifier: "independent-" + kind, SubjectDigest: commit, SourceRevision: commit, Command: kind, Outcome: "passed", ProvenanceURI: "artifact://" + kind, ProvenanceDigest: "sha256:" + kind, ArtifactURI: "artifact://" + kind, ArtifactDigest: "sha256:" + kind, ObservedAt: nowMillis()})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.ID)
	}
	if err = s.CompleteLandedRun(ctx, testMutation("complete-"+task.ID), run.ID, "Landed", ids, domain.LandingObservation{Repository: "example/repo", BaseBranch: "main", Commit: commit, ObservedAt: nowMillis()}); err != nil {
		t.Fatal(err)
	}
}

func TestPRDTasksNeedLandedSourceAndOneExplicitOrderedApproval(t *testing.T) {
	ctx := context.Background()
	s, publication, inv, profile := prdTasksFixture(t)
	turn, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: inv.Message.ChannelID, ThreadID: inv.Thread.ID, AgentID: "chat", AuthorID: "operator", RequestID: "tasks", Body: "Propose tasks from the published PRD"})
	if err != nil {
		t.Fatal(err)
	}
	proposal := &domain.PRDTaskProposal{PublicationTaskID: publication.ID, Revision: strings.Repeat("a", 40), Tasks: []domain.PRDTaskDraft{{Title: "Save filters", Description: "Persist filters", AcceptanceCriteria: "Saving survives reload; unit and browser tests pass"}, {Title: "Restore filters", Description: "Restore a saved filter", AcceptanceCriteria: "Restoring reproduces filter values; unit and browser tests pass"}}}
	input := domain.ConversationOutcomeInput{InvocationMessageID: turn.Message.ID, AgentID: "chat", Status: "completed", Body: "Review the ordered tasks", ResultDigest: "tasks", PRDTasks: proposal}
	if _, err = s.PublishConversationOutcome(ctx, input); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("unlanded PRD authorized: %v", err)
	}
	landPRDTask(t, s, publication, proposal.Revision)
	out, err := s.PublishConversationOutcome(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := s.GetChannelMessage(ctx, out.MessageID)
	if err != nil || msg.PRDTasks == nil || out.TaskID != "" {
		t.Fatalf("proposal: %+v %v", msg, err)
	}
	approval := domain.PRDApproval{Digest: msg.PRDTasks.Digest, ProfileDigest: profile.Digest, Budget: publication.Budget}
	tasks, err := s.ApprovePRDTasks(ctx, msg.ID, prdMutation("tasks"), approval)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 || tasks[0].Status != domain.TaskReady || tasks[1].Status != domain.TaskReady || tasks[0].PRDReference == nil || tasks[0].PRDReference.Revision != proposal.Revision || tasks[0].PRD != nil || len(tasks[0].DependsOn) != 1 || tasks[0].DependsOn[0] != publication.ID || tasks[1].DependsOn[0] != tasks[0].ID {
		t.Fatalf("wrong ordered tasks: %+v", tasks)
	}
	if eligible, err := s.TaskDependenciesSatisfied(ctx, tasks[1].ID); err != nil || eligible {
		t.Fatalf("second task prematurely eligible: %v %v", eligible, err)
	}
	again, err := s.ApprovePRDTasks(ctx, msg.ID, prdMutation("tasks-new-key"), approval)
	if err != nil || again[0].ID != tasks[0].ID {
		t.Fatalf("duplicate approval: %+v %v", again, err)
	}
	approval.Budget.MaxTokens++
	if _, err = s.ApprovePRDTasks(ctx, msg.ID, prdMutation("tasks-changed"), approval); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed replay: %v", err)
	}
	progress, err := s.GetPRDProgress(ctx, publication.ID)
	if err != nil || len(progress.Tasks) != 2 || progress.ReadyToAccept {
		t.Fatalf("progress: %+v %v", progress, err)
	}
	if _, err = s.AcceptPRD(ctx, publication.ID, prdMutation("early"), progress.ResultsDigest); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("accepted unfinished PRD: %v", err)
	}
	t.Run("import cannot manufacture acceptance of unfinished tasks", func(t *testing.T) {
		raw, err := s.ExportProject(ctx, "prj_default")
		if err != nil {
			t.Fatal(err)
		}
		var bundle portableBundle
		if err = json.Unmarshal(raw, &bundle); err != nil {
			t.Fatal(err)
		}
		for i := range bundle.Tasks {
			if bundle.Tasks[i].Task.ID == publication.ID {
				bundle.Tasks[i].Task.PRDAcceptance = &domain.PRDAcceptance{ResultsDigest: progress.ResultsDigest, AcceptedBy: "operator:test", AcceptedAt: nowMillis()}
			}
		}
		raw, _ = json.Marshal(bundle)
		copy, err := Open(ctx, filepath.Join(t.TempDir(), "invalid-acceptance.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer copy.Close()
		if err = copy.ImportProject(ctx, raw); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("unfinished PRD acceptance imported: %v", err)
		}
	})
	for i, task := range tasks {
		landPRDTask(t, s, task, strings.Repeat(string(rune('b'+i)), 40))
	}
	progress, err = s.GetPRDProgress(ctx, publication.ID)
	if err != nil || !progress.ReadyToAccept {
		t.Fatalf("landed progress: %+v %v", progress, err)
	}
	accepted, err := s.AcceptPRD(ctx, publication.ID, prdMutation("accept"), progress.ResultsDigest)
	if err != nil || accepted.Acceptance == nil {
		t.Fatalf("acceptance: %+v %v", accepted, err)
	}
	exported, err := s.ExportProject(ctx, "prj_default")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"changed proposal", "detached task", "different source"} {
		t.Run(scenario, func(t *testing.T) {
			var bundle portableBundle
			if err := json.Unmarshal(exported, &bundle); err != nil {
				t.Fatal(err)
			}
			for i := range bundle.ChannelMessages {
				if p := bundle.ChannelMessages[i].PRDTasks; p != nil && scenario == "changed proposal" {
					p.Tasks[0].AcceptanceCriteria = "Different scope"
				}
			}
			for i := range bundle.Tasks {
				if ref := bundle.Tasks[i].Task.PRDReference; ref != nil {
					if scenario == "detached task" {
						ref.PlanMessageID = "missing"
					}
					if scenario == "different source" {
						ref.Revision = strings.Repeat("f", 40)
					}
				}
			}
			invalid, _ := json.Marshal(bundle)
			copy, err := Open(ctx, filepath.Join(t.TempDir(), "invalid.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer copy.Close()
			if err = copy.ImportProject(ctx, invalid); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("inconsistent PRD import accepted: %v", err)
			}
			if _, err = copy.GetTask(ctx, publication.ID); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("rejected import partially persisted: %v", err)
			}
		})
	}
	restored, err := Open(ctx, filepath.Join(t.TempDir(), "restored.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.ImportProject(ctx, exported); err != nil {
		t.Fatal(err)
	}
	copy, err := restored.GetPRDProgress(ctx, publication.ID)
	if err != nil || copy.Acceptance == nil || copy.ResultsDigest != progress.ResultsDigest || copy.Tasks[1].PRDReference.Revision != proposal.Revision {
		t.Fatalf("export lost PRD identity: %+v %v", copy, err)
	}
	if _, err = restored.AcceptPRD(ctx, publication.ID, prdMutation("accept-restored"), copy.ResultsDigest); err != nil {
		t.Fatalf("acceptance replay after import: %v", err)
	}
}
