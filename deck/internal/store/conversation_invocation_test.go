package store

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestConversationInvocationSelectsAgentAndReplaysWithoutDuplicateMessage(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "conversation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	channel, err := s.CreateChannel(ctx, "prj_default", "conversation")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RegisterAgent(ctx, domain.Agent{ID: "agt_chat", InstallationID: "chat-installation", Name: "Chat", Capabilities: []string{"conversation.invoke.prj_default"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RegisterAgent(ctx, domain.Agent{ID: "agt_worker", InstallationID: "worker-installation", Name: "Worker", Capabilities: []string{"work-control"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RegisterAgent(ctx, domain.Agent{ID: "agt_other", InstallationID: "other-installation", Name: "Other chat", Capabilities: []string{"conversation.invoke.prj_default"}})
	if err != nil {
		t.Fatal(err)
	}
	input := domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_chat", Body: "Investigate login", RequestID: "request-1", AuthorID: "operator:one"}
	first, err := s.InvokeConversation(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Thread.SelectedAgentID != "agt_chat" || first.Message.ThreadID != first.Thread.ID || first.Message.InvokedAgentID != "agt_chat" {
		t.Fatalf("first invocation: %#v", first)
	}
	replay, err := s.InvokeConversation(ctx, input)
	if err != nil || replay.Message.ID != first.Message.ID || replay.Thread.ID != first.Thread.ID {
		t.Fatalf("replay: %#v %v", replay, err)
	}
	input.Body = "Changed request"
	if _, err := s.InvokeConversation(ctx, input); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed request: %v", err)
	}
	input = domain.ConversationInvocationInput{ChannelID: channel.ID, ThreadID: first.Thread.ID, AgentID: "agt_chat", Body: "Use GitHub", RequestID: "request-2", AuthorID: "operator:one"}
	second, err := s.InvokeConversation(ctx, input)
	if err != nil || second.Message.ID == first.Message.ID {
		t.Fatalf("second invocation: %#v %v", second, err)
	}
	input.RequestID = "request-3"
	input.AgentID = "agt_worker"
	if _, err := s.InvokeConversation(ctx, input); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("non-conversation agent: %v", err)
	}
	_, err = s.RegisterAgent(ctx, domain.Agent{ID: "agt_foreign", InstallationID: "foreign-installation", Name: "Foreign chat", Capabilities: []string{"conversation.invoke.other"}})
	if err != nil {
		t.Fatal(err)
	}
	input.AgentID = "agt_foreign"
	if _, err := s.InvokeConversation(ctx, input); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("foreign project agent: %v", err)
	}
	input.AgentID = "agt_other"
	if _, err := s.InvokeConversation(ctx, input); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed thread agent: %v", err)
	}
	page, err := s.PageThreadMessages(ctx, first.Thread.ID, 100, "")
	if err != nil || len(page.Items) != 2 || page.Items[1].InvokedAgentID != "agt_chat" {
		t.Fatalf("history: %#v %v", page, err)
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
	for _, id := range []string{first.Message.ID, second.Message.ID} {
		restoredMessage, err := restored.GetChannelMessage(ctx, id)
		if err != nil || restoredMessage.InvokedAgentID != "agt_chat" || restoredMessage.InvocationRequestID == "" {
			t.Fatalf("restored invocation %s: %+v %v", id, restoredMessage, err)
		}
	}
	replayedAfterImport, err := restored.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_chat", Body: "Investigate login", RequestID: "request-1", AuthorID: "operator:one"})
	if err != nil || replayedAfterImport.Message.ID != first.Message.ID {
		t.Fatalf("replay after import: %#v %v", replayedAfterImport, err)
	}
	loaded, err := restored.GetThread(ctx, first.Thread.ID)
	if err != nil || loaded.SelectedAgentID != "agt_chat" {
		t.Fatalf("restored thread: %#v %v", loaded, err)
	}
	reexported, err := restored.ExportProject(ctx, "prj_default")
	if err != nil || !bytes.Equal(exported, reexported) {
		t.Fatalf("export round trip: %v", err)
	}
}

func TestConversationInvocationCanAddressAnExistingHumanMessageOnce(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "existing.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	channel, err := s.CreateChannel(ctx, "prj_default", "existing")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateChannel(ctx, "prj_default", "other")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RegisterAgent(ctx, domain.Agent{ID: "agt_chat", InstallationID: "chat-installation", Name: "Chat", Capabilities: []string{"conversation.invoke.prj_default"}})
	if err != nil {
		t.Fatal(err)
	}
	message, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: channel.ID, AuthorType: "human", AuthorID: "operator:one", Body: "Explain this design"})
	if err != nil {
		t.Fatal(err)
	}
	input := domain.ConversationInvocationInput{ChannelID: channel.ID, MessageID: message.ID, AgentID: "agt_chat", RequestID: "existing-1", AuthorID: "operator:one"}
	if _, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: other.ID, MessageID: message.ID, AgentID: "agt_chat", RequestID: "wrong-channel", AuthorID: "operator:one"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("foreign message: %v", err)
	}
	invoked, err := s.InvokeConversation(ctx, input)
	if err != nil || invoked.Message.ID != message.ID || invoked.Thread.RootMessageID != message.ID || invoked.Message.InvokedAgentID != "agt_chat" {
		t.Fatalf("existing invocation: %#v %v", invoked, err)
	}
	replay, err := s.InvokeConversation(ctx, input)
	if err != nil || replay.Message.ID != message.ID || replay.Thread.ID != invoked.Thread.ID {
		t.Fatalf("existing replay: %#v %v", replay, err)
	}
	input.RequestID = "existing-2"
	if _, err := s.InvokeConversation(ctx, input); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate invocation: %v", err)
	}
	page, err := s.PageThreadMessages(ctx, invoked.Thread.ID, 100, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("one canonical message: %#v %v", page, err)
	}
}
