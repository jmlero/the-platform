package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestDeleteDraftRequiresCurrentVersionAndNoExecution(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Draft"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDraft(ctx, task.ID, task.Version+1); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	if err := s.DeleteDraft(ctx, task.ID, task.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTask(ctx, task.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted draft: %v", err)
	}
	task, _ = s.CreateTask(ctx, domain.CreateTaskInput{Title: "Authorized", Status: domain.TaskReady})
	if err := s.DeleteDraft(ctx, task.ID, task.Version); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("ready delete: %v", err)
	}
	_, err = s.RegisterAgent(ctx, domain.Agent{ID: "agt_retention", Name: "Retention"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.ClaimTask(ctx, testMutation("retention-claim"), task.ID, "agt_retention", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	task, _ = s.GetTask(ctx, task.ID)
	if _, err := s.SetTaskArchived(ctx, task.ID, task.Version, true); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("active archive: %v", err)
	}
	if err := s.BlockRun(ctx, testMutation("retention-block"), run.ID, "Stopped"); err != nil {
		t.Fatal(err)
	}
	task, err = s.MoveTask(ctx, task.ID, domain.TaskBacklog)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDraft(ctx, task.ID, task.Version); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("history delete: %v", err)
	}
}

func TestArchivePreservesHistoryAndSurvivesExportAndRestore(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "deck.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	task, _ := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Finished", Status: domain.TaskReady})
	if _, err := s.SetTaskArchived(ctx, task.ID, task.Version, true); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("ready archive: %v", err)
	}
	_, err = s.RegisterAgent(ctx, domain.Agent{ID: "agt_archive", Name: "Archive"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.ClaimTask(ctx, testMutation("archive-claim"), task.ID, "agt_archive", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitPlan(ctx, testMutation("archive-plan"), run.ID, "Implement and verify", false); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginVerification(ctx, testMutation("archive-verify"), run.ID); err != nil {
		t.Fatal(err)
	}
	evidenceID := recordPassingEvidence(t, ctx, s, run.ID, "archive")
	if err := s.CompleteRunWithEvidence(ctx, testMutation("archive-complete"), run.ID, "Verified PR https://github.com/example/repo/pull/1", []string{evidenceID}); err != nil {
		t.Fatal(err)
	}
	before, _ := s.GetTaskDetail(ctx, task.ID)
	if _, err := s.SetTaskArchived(ctx, task.ID, before.Task.Version-1, true); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale archive: %v", err)
	}
	archived, err := s.SetTaskArchived(ctx, task.ID, before.Task.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if !archived.Archived || archived.Status != domain.TaskDone {
		t.Fatalf("archive changed outcome: %#v", archived)
	}
	if _, err := s.MoveTask(ctx, task.ID, domain.TaskReady); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("archived requeue: %v", err)
	}
	if _, err := s.ClaimTask(ctx, testMutation("archived-claim"), task.ID, "agt_archive", time.Minute); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("archived claim: %v", err)
	}
	title := "Unexpected edit"
	if _, err := s.UpdateTask(ctx, task.ID, domain.UpdateTaskInput{Title: &title}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("archived edit: %v", err)
	}
	after, err := s.GetTaskDetail(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Runs, before.Runs) || !reflect.DeepEqual(after.Artifacts, before.Artifacts) || !reflect.DeepEqual(after.Evidence, before.Evidence) {
		t.Fatal("archive modified execution history")
	}
	// Portable exports intentionally contain artifact metadata, not artifact bytes.
	for i := range before.Artifacts {
		before.Artifacts[i].Content = ""
	}
	encoded, err := s.ExportProject(ctx, task.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := Open(ctx, filepath.Join(t.TempDir(), "import.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer imported.Close()
	if err := imported.ImportProject(ctx, encoded); err != nil {
		t.Fatal(err)
	}
	detail, err := imported.GetTaskDetail(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Task.Archived || len(detail.Runs) != len(before.Runs) || !reflect.DeepEqual(detail.Runs, before.Runs) || !reflect.DeepEqual(detail.Artifacts, before.Artifacts) || !reflect.DeepEqual(detail.Evidence, before.Evidence) {
		t.Fatal("archive or execution metadata lost in export/import")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	task, err = s.GetTask(ctx, task.ID)
	if err != nil || !task.Archived {
		t.Fatalf("archive lost on restart: %#v %v", task, err)
	}
	task, err = s.SetTaskArchived(ctx, task.ID, task.Version, false)
	if err != nil || task.Archived {
		t.Fatalf("restore: %#v %v", task, err)
	}
	if _, err := s.MoveTask(ctx, task.ID, domain.TaskReady); err != nil {
		t.Fatal(err)
	}
}

func TestLinkedDraftMustBeArchivedAndPromotionReplaysOriginalTask(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	channel, err := s.CreateChannel(ctx, "prj_default", "test")
	if err != nil {
		t.Fatal(err)
	}
	message, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: channel.ID, AuthorType: "human", AuthorID: "operator", Body: "Feature idea"})
	if err != nil {
		t.Fatal(err)
	}
	input := domain.CreateTaskInput{Title: "Linked draft", AcceptanceCriteria: "Test passes", Budget: &domain.Budget{TimeMinutes: 10}}
	task, err := s.PromoteChannelMessage(ctx, message.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDraft(ctx, task.ID, task.Version); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("linked delete: %v", err)
	}
	if _, err := s.SetTaskArchived(ctx, task.ID, task.Version, true); err != nil {
		t.Fatal(err)
	}
	replay, err := s.PromoteChannelMessage(ctx, message.ID, input)
	if err != nil || replay.ID != task.ID || !replay.Archived {
		t.Fatalf("promotion replay: %#v %v", replay, err)
	}
}
