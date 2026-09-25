package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestInvocationFeedIncludesExistingMessageInvokedAfterPosting(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "dispatch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	channel, err := s.CreateChannel(ctx, "prj_default", "dispatch")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RegisterAgent(ctx, domain.Agent{ID: "agt_chat", InstallationID: "install", Name: "Chat", Capabilities: []string{"conversation.invoke.prj_default"}})
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: channel.ID, AuthorType: "human", AuthorID: "operator:one", Body: "Explain this"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: channel.ID, AuthorType: "human", AuthorID: "operator:one", Body: "Later unrelated message"})
	if err != nil {
		t.Fatal(err)
	}
	invoked, err := s.InvokeConversation(ctx, domain.ConversationInvocationInput{ChannelID: channel.ID, MessageID: old.ID, AgentID: "agt_chat", RequestID: "request-1", AuthorID: "operator:one"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.ListConversationInvocations(ctx, "prj_default", 0, 100)
	if err != nil || len(page) != 1 || page[0].Message.ID != old.ID || page[0].Message.ThreadID != invoked.Thread.ID || page[0].EventID < 1 {
		t.Fatalf("dispatch feed: %+v %v", page, err)
	}
	again, err := s.ListConversationInvocations(ctx, "prj_default", page[0].EventID, 100)
	if err != nil || len(again) != 0 {
		t.Fatalf("cursor replay: %+v %v", again, err)
	}
}
