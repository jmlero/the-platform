package store

import (
	"context"
	"errors"
	"github.com/jmlero/the-platform/deck/internal/domain"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPRDApprovalFreezesDraftAndCreatesOneLandingTaskAcrossRestartAndExport(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "deck.db")
	s, err := Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	channel, err := s.CreateChannel(ctx, "prj_default", "prd")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RegisterAgent(ctx, domain.Agent{ID: "agt_chat", InstallationID: "chat", Name: "Chat", Capabilities: []string{"conversation.invoke.prj_default"}})
	if err != nil {
		t.Fatal(err)
	}
	bindRepository(t, s, "prj_default", "owner/repo", 1)
	inv, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_chat", AuthorID: "operator", RequestID: "draft", Body: "Draft a PRD for saved filters"})
	if err != nil {
		t.Fatal(err)
	}
	input := domain.ConversationOutcomeInput{InvocationMessageID: inv.Message.ID, AgentID: "agt_chat", Status: "completed", Body: "Review this PRD.", ResultDigest: "draft-result", PRD: &domain.PRDDraft{Title: "Saved filters", Path: "docs/prd/saved-filters.md", Content: "# Saved filters\n\nUsers can save and restore filters.\n"}}
	out, err := s.PublishConversationOutcome(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if out.TaskID != "" {
		t.Fatal("draft authorized execution")
	}
	msg, err := s.GetChannelMessage(ctx, out.MessageID)
	if err != nil || msg.PRD == nil || msg.PRD.Digest == "" {
		t.Fatalf("draft missing: %+v %v", msg, err)
	}
	profile := domain.WorkloadProfile{ID: "repository/v2", Digest: strings.Repeat("a", 64), Authority: []string{"source.read", "source.write", "source.fast_forward"}, MaxTokens: 30000, BudgetMinutes: 30, MinAttempts: 3}
	if err = s.PublishWorkloads(ctx, "prj_default", "test", domain.WorkloadCatalog{Profiles: []domain.WorkloadProfile{profile}, ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	approval := domain.PRDApproval{Digest: msg.PRD.Digest, ProfileDigest: profile.Digest, Budget: domain.Budget{TimeMinutes: 30, MaxAttempts: 3, MaxTokens: 20000, MaxConcurrency: 1, Authority: profile.Authority}}
	bad := approval
	bad.Digest = strings.Repeat("b", 64)
	if _, err = s.ApprovePRD(ctx, out.MessageID, prdMutation("bad-digest"), bad); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed draft approved: %v", err)
	}

	for _, test := range []struct {
		name  string
		input domain.PRDApproval
		want  error
	}{
		{"changed-profile", domain.PRDApproval{Digest: approval.Digest, ProfileDigest: strings.Repeat("c", 64), Budget: approval.Budget}, domain.ErrConflict},
		{"missing-budget", domain.PRDApproval{Digest: approval.Digest, ProfileDigest: approval.ProfileDigest}, domain.ErrInvalidInput},
	} {
		if _, err := s.ApprovePRD(ctx, msg.ID, prdMutation(test.name), test.input); !errors.Is(err, test.want) {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
	if err := s.PublishWorkloads(ctx, "prj_default", "test", domain.WorkloadCatalog{Profiles: []domain.WorkloadProfile{}, ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApprovePRD(ctx, msg.ID, prdMutation("unavailable"), approval); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("unavailable profile approved: %v", err)
	}
	if err := s.PublishWorkloads(ctx, "prj_default", "test", domain.WorkloadCatalog{Profiles: []domain.WorkloadProfile{profile}, ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	task, err := s.ApprovePRD(ctx, out.MessageID, prdMutation("approve"), approval)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != domain.TaskReady || task.PRD == nil || task.PRD.Content != input.PRD.Content || task.PRD.MessageID != msg.ID || task.Workload.ID != "repository/v2" {
		t.Fatalf("wrong approved task: %+v", task)
	}
	thread, err := s.GetTaskThread(ctx, task.ID)
	if err != nil || thread.ID != inv.Thread.ID {
		t.Fatalf("lost thread: %+v %v", thread, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.ApprovePRD(ctx, msg.ID, prdMutation("approve"), approval)
	if err != nil || replay.ID != task.ID {
		t.Fatalf("restart replay: %+v %v", replay, err)
	}
	replay, err = s.ApprovePRD(ctx, msg.ID, prdMutation("another-key"), approval)
	if err != nil || replay.ID != task.ID {
		t.Fatalf("duplicate approval: %+v %v", replay, err)
	}
	bad = approval
	bad.Budget.MaxTokens++
	if _, err = s.ApprovePRD(ctx, msg.ID, prdMutation("changed-budget"), bad); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed approval: %v", err)
	}
	export, err := s.ExportProject(ctx, "prj_default")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Open(ctx, filepath.Join(t.TempDir(), "restored.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.ImportProject(ctx, export); err != nil {
		t.Fatal(err)
	}
	replay, err = restored.ApprovePRD(ctx, msg.ID, prdMutation("restored"), approval)
	if err != nil || replay.ID != task.ID || replay.PRD.Content != input.PRD.Content {
		t.Fatalf("export replay: %+v %v", replay, err)
	}
	input.PRD.Content = "changed"
	if _, err = s.PublishConversationOutcome(ctx, input); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed publication: %v", err)
	}
}

func prdMutation(key string) Mutation { return Mutation{ActorID: "operator:test", IdempotencyKey: key} }
