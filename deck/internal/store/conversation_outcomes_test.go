package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestConversationOutcomeCreatesOneReadyTaskAndReply(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	profileDigest := strings.Repeat("a", 64)
	if err := s.PublishWorkloads(ctx, "prj_default", "test", domain.WorkloadCatalog{ExpiresAt: time.Now().Add(time.Minute).UnixMilli(), Profiles: []domain.WorkloadProfile{{ID: "repository/v1", Digest: profileDigest, Authority: []string{"source.read", "source.write", "pull_request.create"}, MaxTokens: 30000, BudgetMinutes: 40, MinAttempts: 2}}}); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	channel, err := s.CreateChannel(ctx, "prj_default", "delegation")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RegisterAgent(ctx, domain.Agent{ID: "agt_chat", InstallationID: "chat", Name: "Chat", Capabilities: []string{"conversation.invoke.prj_default"}})
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_chat", AuthorID: "operator:one", RequestID: "delegate-1", Body: "Fix the login regression and verify it"})
	if err != nil {
		t.Fatal(err)
	}
	input := domain.ConversationOutcomeInput{InvocationMessageID: invocation.Message.ID, AgentID: "agt_chat", Status: "completed", Body: "I created the task.", ResultDigest: "sha256:result-1", ProfileID: "repository/v1", ProfileDigest: profileDigest, Task: &domain.CreateTaskInput{Title: "Fix login regression", AcceptanceCriteria: "Login regression test passes", Status: domain.TaskReady, Budget: &domain.Budget{TimeMinutes: 30, MaxAttempts: 2, MaxTokens: 20000, MaxConcurrency: 1, Authority: []string{"source.read", "source.write", "pull_request.create"}}}}
	first, err := s.PublishConversationOutcome(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.PublishConversationOutcome(ctx, input)
	if err != nil || second.MessageID != first.MessageID || second.TaskID != first.TaskID || first.TaskID == "" {
		t.Fatalf("replay: %+v %+v %v", first, second, err)
	}
	task, err := s.GetTask(ctx, first.TaskID)
	if err != nil || task.Status != domain.TaskReady {
		t.Fatalf("task: %+v %v", task, err)
	}
	thread, err := s.GetTaskThread(ctx, task.ID)
	if err != nil || thread.ID != invocation.Thread.ID {
		t.Fatalf("origin: %+v %v", thread, err)
	}
	page, err := s.PageThreadMessages(ctx, thread.ID, 100, "")
	if err != nil || len(page.Items) != 2 || page.Items[1].References.TaskIDs[0] != task.ID {
		t.Fatalf("messages: %+v %v", page, err)
	}
	input.Body = "changed reply"
	if _, err := s.PublishConversationOutcome(ctx, input); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed replay: %v", err)
	}
	input.Body = "I created the task."
	encoded, err := s.ExportProject(ctx, "prj_default")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Open(ctx, filepath.Join(t.TempDir(), "restored.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.ImportProject(ctx, encoded); err != nil {
		t.Fatal(err)
	}
	replayed, err := restored.PublishConversationOutcome(ctx, input)
	if err != nil || replayed.MessageID != first.MessageID || replayed.TaskID != first.TaskID {
		t.Fatalf("restored replay: %+v %v", replayed, err)
	}
}

func TestConversationOutcomeAdmitsFastForwardProfile(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	digest := strings.Repeat("b", 64)
	authority := []string{"source.read", "source.write", "source.fast_forward"}
	if err := s.PublishWorkloads(ctx, "prj_default", "test", domain.WorkloadCatalog{ExpiresAt: time.Now().Add(time.Minute).UnixMilli(), Profiles: []domain.WorkloadProfile{{ID: "repository/v2", Digest: digest, Authority: authority, MaxTokens: 30000, BudgetMinutes: 40, MinAttempts: 2}}}); err != nil {
		t.Fatal(err)
	}
	channel, err := s.CreateChannel(ctx, "prj_default", "landing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterAgent(ctx, domain.Agent{ID: "agt_chat", InstallationID: "chat", Name: "Chat", Capabilities: []string{"conversation.invoke.prj_default"}}); err != nil {
		t.Fatal(err)
	}
	invocation, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_chat", AuthorID: "operator:one", RequestID: "landing-delegation", Body: "Fix and land the regression"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.PublishConversationOutcome(ctx, domain.ConversationOutcomeInput{InvocationMessageID: invocation.Message.ID, AgentID: "agt_chat", Status: "completed", Body: "Task created", ResultDigest: "sha256:landing", ProfileID: "repository/v2", ProfileDigest: digest, Task: &domain.CreateTaskInput{Title: "Fix regression", AcceptanceCriteria: "Regression passes", Status: domain.TaskReady, Budget: &domain.Budget{TimeMinutes: 30, MaxAttempts: 2, MaxTokens: 20000, MaxConcurrency: 1, Authority: authority}}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.GetTask(ctx, result.TaskID)
	if err != nil || task.Status != domain.TaskReady || task.Workload == nil || task.Workload.ID != "repository/v2" {
		t.Fatalf("landing delegation: task=%+v err=%v", task, err)
	}
}

func TestConversationOutcomeAdmitsIndependentReviewBudget(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	digest := strings.Repeat("b", 64)
	authority := []string{"source.read", "source.write", "source.fast_forward"}
	if err := s.PublishWorkloads(ctx, "prj_default", "test", domain.WorkloadCatalog{ExpiresAt: time.Now().Add(time.Minute).UnixMilli(), Profiles: []domain.WorkloadProfile{{ID: "repository/v2", Digest: digest, Authority: authority, MaxTokens: 30000, BudgetMinutes: 40, MinAttempts: 3}}}); err != nil {
		t.Fatal(err)
	}
	channel, err := s.CreateChannel(ctx, "prj_default", "landing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterAgent(ctx, domain.Agent{ID: "agt_chat", InstallationID: "chat", Name: "Chat", Capabilities: []string{"conversation.invoke.prj_default"}}); err != nil {
		t.Fatal(err)
	}
	invocation, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_chat", AuthorID: "operator:one", RequestID: "landing-delegation", Body: "Fix and land the regression"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.PublishConversationOutcome(ctx, domain.ConversationOutcomeInput{InvocationMessageID: invocation.Message.ID, AgentID: "agt_chat", Status: "completed", Body: "Task created", ResultDigest: "sha256:landing", ProfileID: "repository/v2", ProfileDigest: digest, Task: &domain.CreateTaskInput{Title: "Fix regression", AcceptanceCriteria: "Regression passes", Status: domain.TaskReady, Budget: &domain.Budget{TimeMinutes: 30, MaxAttempts: 3, MaxTokens: 20000, MaxConcurrency: 1, Authority: authority}}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.GetTask(ctx, result.TaskID)
	if err != nil || task.Status != domain.TaskReady || task.Workload == nil || task.Workload.ID != "repository/v2" {
		t.Fatalf("landing delegation: task=%+v err=%v", task, err)
	}
}

func TestConversationFollowupPreservesOriginalTaskAndHistory(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	channel, _ := s.CreateChannel(ctx, "prj_default", "feedback")
	_, _ = s.RegisterAgent(ctx, domain.Agent{ID: "agt_chat", InstallationID: "chat", Name: "Chat", Capabilities: []string{"conversation.invoke.prj_default"}})
	first, _ := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_chat", AuthorID: "operator:one", RequestID: "original", Body: "Build the login fix"})
	original, err := s.PublishConversationOutcome(ctx, domain.ConversationOutcomeInput{InvocationMessageID: first.Message.ID, AgentID: "agt_chat", Status: "completed", Body: "Task created", ResultDigest: "sha256:original", Task: &domain.CreateTaskInput{Title: "Original task"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE tasks SET status='done' WHERE id=?", original.TaskID); err != nil {
		t.Fatal(err)
	}
	feedback, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, ThreadID: first.Thread.ID, AgentID: "agt_chat", AuthorID: "operator:one", RequestID: "feedback", Body: "Use PKCE for this instead"})
	if err != nil {
		t.Fatal(err)
	}
	followup, err := s.PublishConversationOutcome(ctx, domain.ConversationOutcomeInput{InvocationMessageID: feedback.Message.ID, AgentID: "agt_chat", Status: "completed", Body: "Follow-up created", ResultDigest: "sha256:followup", FollowupTaskID: original.TaskID, Task: &domain.CreateTaskInput{Title: "Use PKCE"}})
	if err != nil {
		t.Fatal(err)
	}
	if followup.TaskID == original.TaskID || followup.FollowupTaskID != original.TaskID {
		t.Fatalf("follow-up: %+v", followup)
	}
	prior, err := s.GetTask(ctx, original.TaskID)
	if err != nil || prior.Title != "Original task" {
		t.Fatalf("original changed: %+v %v", prior, err)
	}
	if _, err := s.PublishConversationOutcome(ctx, domain.ConversationOutcomeInput{InvocationMessageID: feedback.Message.ID, AgentID: "agt_chat", Status: "completed", Body: "Follow-up created", ResultDigest: "sha256:followup", FollowupTaskID: "other", Task: &domain.CreateTaskInput{Title: "Use PKCE"}}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed follow-up: %v", err)
	}
}

func TestOriginatingThreadReceivesRunMilestonesAndBlockerOnce(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	channel, _ := s.CreateChannel(ctx, "prj_default", "progress")
	_, _ = s.RegisterAgent(ctx, domain.Agent{ID: "agt_chat", InstallationID: "chat", Name: "Chat", Capabilities: []string{"conversation.invoke.prj_default"}})
	invocation, _ := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_chat", AuthorID: "operator:one", RequestID: "progress", Body: "Investigate regression"})
	_, err = s.PublishConversationOutcome(ctx, domain.ConversationOutcomeInput{InvocationMessageID: invocation.Message.ID, AgentID: "agt_chat", Status: "completed", Body: "I can investigate.", ResultDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.PromoteChannelMessage(ctx, invocation.Message.ID, domain.CreateTaskInput{Title: "Investigate", Status: domain.TaskReady, AcceptanceCriteria: "Report findings", Budget: &domain.Budget{TimeMinutes: 10, MaxAttempts: 1}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.ClaimTask(ctx, testMutation("thread-claim"), task.ID, "agt_chat", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BlockRun(ctx, testMutation("thread-block"), run.ID, "Required fixture unavailable"); err != nil {
		t.Fatal(err)
	}
	if err := s.BlockRun(ctx, testMutation("thread-block"), run.ID, "Required fixture unavailable"); err != nil {
		t.Fatal(err)
	}
	page, err := s.PageThreadMessages(ctx, invocation.Thread.ID, 100, "")
	if err != nil || len(page.Items) != 4 {
		t.Fatalf("thread messages: %+v %v", page, err)
	}
	last := page.Items[len(page.Items)-1]
	if last.Body != "Run blocked: Required fixture unavailable" || len(last.References.TaskIDs) != 1 || last.References.TaskIDs[0] != task.ID || len(last.References.RunIDs) != 1 || last.References.RunIDs[0] != run.ID {
		t.Fatalf("blocker reference: %+v", last)
	}
}

func TestOriginatingThreadCompletionLinksCurrentEvidence(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	channel, _ := s.CreateChannel(ctx, "prj_default", "completion")
	message, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: channel.ID, AuthorType: "human", AuthorID: "operator", Body: "Verify the fix"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.PromoteChannelMessage(ctx, message.ID, domain.CreateTaskInput{Title: "Verify fix", Status: domain.TaskReady, AcceptanceCriteria: "Checks pass", Budget: &domain.Budget{TimeMinutes: 10, MaxAttempts: 1}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RegisterAgent(ctx, domain.Agent{ID: "agt_verify", InstallationID: "verify", Name: "Verify"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.ClaimTask(ctx, testMutation("completion-claim"), task.ID, "agt_verify", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitPlan(ctx, testMutation("completion-plan"), run.ID, "Run checks", false); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginVerification(ctx, testMutation("completion-verify"), run.ID); err != nil {
		t.Fatal(err)
	}
	evidenceID := recordPassingEvidence(t, ctx, s, run.ID, "conversation-complete")
	if err := s.CompleteRunWithEvidence(ctx, testMutation("completion-finish"), run.ID, "Checks passed", []string{evidenceID}); err != nil {
		t.Fatal(err)
	}
	thread, err := s.GetTaskThread(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.PageThreadMessages(ctx, thread.ID, 100, "")
	if err != nil || len(page.Items) < 2 {
		t.Fatalf("completion messages: %+v %v", page, err)
	}
	last := page.Items[len(page.Items)-1]
	if last.Body != "Run completed: Checks passed" || len(last.References.TaskIDs) != 1 || last.References.TaskIDs[0] != task.ID || len(last.References.RunIDs) != 1 || last.References.RunIDs[0] != run.ID || len(last.References.EvidenceIDs) != 1 || last.References.EvidenceIDs[0] != evidenceID || len(last.References.ArtifactIDs) != 1 {
		t.Fatalf("completion links: %+v", last)
	}
}
