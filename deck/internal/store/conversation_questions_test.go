package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestConversationQuestionAnswerBindsOneThreadAndReplays(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "questions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	channel, err := s.CreateChannel(ctx, "prj_default", "questions")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RegisterAgent(ctx, domain.Agent{ID: "agt_chat", InstallationID: "chat", Name: "Chat", Capabilities: []string{"conversation.invoke.prj_default"}})
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_chat", Body: "Help me choose", RequestID: "request-1", AuthorID: "operator:one"})
	if err != nil {
		t.Fatal(err)
	}
	in := domain.ConversationQuestionInput{InvocationMessageID: invocation.Message.ID, AgentID: "agt_chat", QuestionID: "scope", Text: "Which provider?", Choices: []string{"GitHub", "Google"}, ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}
	question, err := s.EnsureConversationQuestion(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if question.ThreadID != invocation.Thread.ID || question.Status != "pending" {
		t.Fatalf("wrong question: %+v", question)
	}
	replayed, err := s.EnsureConversationQuestion(ctx, in)
	if err != nil || replayed.ID != question.ID || replayed.MessageID != question.MessageID {
		t.Fatalf("question replay: %+v %v", replayed, err)
	}
	changed := in
	changed.Text = "Different question"
	if _, err := s.EnsureConversationQuestion(ctx, changed); err == nil {
		t.Fatal("changed question replay accepted")
	}
	freeText := in
	freeText.QuestionID, freeText.Text, freeText.Choices = "details", "What details?", nil
	if _, err := s.EnsureConversationQuestion(ctx, freeText); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureConversationQuestion(ctx, freeText); err != nil {
		t.Fatalf("free-text question replay: %v", err)
	}
	answered, err := s.AnswerConversationQuestion(ctx, question.ID, "GitHub", "operator:one", "answer-1")
	if err != nil || answered.Status != "answered" || answered.AnswerMessageID == "" {
		t.Fatalf("answer: %+v %v", answered, err)
	}
	replayed, err = s.AnswerConversationQuestion(ctx, question.ID, "GitHub", "operator:one", "answer-1")
	if err != nil || replayed.AnswerMessageID != answered.AnswerMessageID {
		t.Fatalf("answer replay: %+v %v", replayed, err)
	}
	if _, err := s.AnswerConversationQuestion(ctx, question.ID, "Google", "operator:one", "answer-1"); err == nil {
		t.Fatal("changed answer accepted")
	}
	if _, err := s.AnswerConversationQuestion(ctx, question.ID, "GitHub", "operator:two", "answer-2"); err == nil {
		t.Fatal("second answer accepted")
	}
	exported, err := s.ExportProject(ctx, "prj_default")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Open(ctx, filepath.Join(t.TempDir(), "restored.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.ImportProject(ctx, exported); err != nil {
		t.Fatal(err)
	}
	recovered, err := restored.GetConversationQuestion(ctx, question.ID)
	if err != nil || recovered.AnswerMessageID != answered.AnswerMessageID || recovered.ThreadID != invocation.Thread.ID || recovered.ExpiresAt != in.ExpiresAt {
		t.Fatalf("question import: %+v %v", recovered, err)
	}
}

func TestExpiredConversationQuestionPreservesPendingHistoryAndRejectsAnswer(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "questions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	channel, err := s.CreateChannel(ctx, "prj_default", "questions")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RegisterAgent(ctx, domain.Agent{ID: "agt_chat", InstallationID: "chat", Name: "Chat", Capabilities: []string{"conversation.invoke.prj_default"}})
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_chat", Body: "Help", RequestID: "request", AuthorID: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.EnsureConversationQuestion(ctx, domain.ConversationQuestionInput{InvocationMessageID: invocation.Message.ID, AgentID: "agt_chat", QuestionID: "scope", Text: "Which provider?", ExpiresAt: time.Now().Add(time.Minute).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE conversation_questions SET expires_at=? WHERE id=?", time.Now().Add(-time.Second).UnixMilli(), q.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AnswerConversationQuestion(ctx, q.ID, "GitHub", "operator", "late"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expired answer accepted: %v", err)
	}
	q, err = s.GetConversationQuestion(ctx, q.ID)
	if err != nil || q.Status != "pending" || q.Answer != "" {
		t.Fatalf("expired history changed: %+v %v", q, err)
	}
}

func TestEndedConversationCannotAcceptOrCreateAnotherQuestion(t *testing.T) {
	for _, status := range []string{"completed", "blocked"} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			s, err := Open(ctx, filepath.Join(t.TempDir(), "questions.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			channel, err := s.CreateChannel(ctx, "prj_default", "questions")
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.RegisterAgent(ctx, domain.Agent{ID: "agt_chat", InstallationID: "chat", Name: "Chat", Capabilities: []string{"conversation.invoke.prj_default"}})
			if err != nil {
				t.Fatal(err)
			}
			invocation, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_chat", Body: "Help me choose", RequestID: "request", AuthorID: "operator"})
			if err != nil {
				t.Fatal(err)
			}
			in := domain.ConversationQuestionInput{InvocationMessageID: invocation.Message.ID, AgentID: "agt_chat", QuestionID: "scope", Text: "Which provider?"}
			q, err := s.EnsureConversationQuestion(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.PublishConversationOutcome(ctx, domain.ConversationOutcomeInput{InvocationMessageID: invocation.Message.ID, AgentID: "agt_chat", Status: status, Body: "Turn ended", ResultDigest: "digest"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.AnswerConversationQuestion(ctx, q.ID, "GitHub", "operator", "late-answer"); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("late answer accepted: %v", err)
			}
			// Older portable bundles have outcomes but no closed_at field.
			if _, err := s.db.ExecContext(ctx, "UPDATE conversation_questions SET closed_at=NULL WHERE id=?", q.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.AnswerConversationQuestion(ctx, q.ID, "GitHub", "operator", "legacy-late-answer"); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("late answer from legacy history accepted: %v", err)
			}
			in.QuestionID = "new-operation"
			if _, err := s.EnsureConversationQuestion(ctx, in); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("new question after terminal outcome: %v", err)
			}
		})
	}
}
