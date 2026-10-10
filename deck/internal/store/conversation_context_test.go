package store

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestConversationContextKeepsInvokedRevisionsAcrossReplayRestartAndExport(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "conversation-context.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := s.CreateChannel(ctx, "prj_default", "chat-context")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterAgent(ctx, domain.Agent{ID: "agt_chat", InstallationID: "chat", Name: "Chat", Capabilities: []string{"conversation.invoke.prj_default"}}); err != nil {
		t.Fatal(err)
	}
	request, err := s.RequestRepository(ctx, "prj_default", domain.RepositoryRequestInput{Repository: "owner/one", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordRepositoryResult(ctx, "prj_default", domain.RepositoryResult{RequestRevision: request.Revision, Status: "connected", Binding: &domain.RepositoryBinding{Version: 1, Repository: "owner/one", CloneURL: "https://github.com/owner/one.git", BaseBranch: "main"}}); err != nil {
		t.Fatal(err)
	}
	input := domain.ContextSourceInput{ID: "rules", Kind: "instruction", Title: "Rules", Content: "Preserve the API", Trust: "guidance", State: "active"}
	first, err := s.PutContextSource(ctx, "prj_default", input)
	if err != nil {
		t.Fatal(err)
	}
	invocationInput := domain.ConversationInvocationInput{ChannelID: channel.ID, AgentID: "agt_chat", Body: "Explain the API", RequestID: "context-1", AuthorID: "operator"}
	invocation, err := s.InvokeConversation(ctx, invocationInput)
	if err != nil {
		t.Fatal(err)
	}
	input.ExpectedRevision, input.Content = first.Revision, "Use the new API"
	second, err := s.PutContextSource(ctx, "prj_default", input)
	if err != nil {
		t.Fatal(err)
	}
	request, err = s.RequestRepository(ctx, "prj_default", domain.RepositoryRequestInput{ExpectedRevision: request.Revision, ExpectedBindingVersion: 1, Repository: "owner/two", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordRepositoryResult(ctx, "prj_default", domain.RepositoryResult{RequestRevision: request.Revision, Status: "connected", Binding: &domain.RepositoryBinding{Version: 2, Repository: "owner/two", CloneURL: "https://github.com/owner/two.git", BaseBranch: "main"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InvokeConversation(ctx, invocationInput); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	resolved, err := s.ResolveConversationContext(ctx, invocation.Message.ID)
	if err != nil || resolved.Selection == nil || len(resolved.Sources) != 1 || resolved.Sources[0].Revision != first.Revision {
		t.Fatalf("frozen context: %#v %v", resolved, err)
	}
	if resolved.Selection.Repository == nil || resolved.Selection.Repository.Version != 1 || resolved.Selection.Repository.Repository != "owner/one" {
		t.Fatalf("repository changed: %#v", resolved.Selection.Repository)
	}
	invocationInput.RequestID = "context-2"
	next, err := s.InvokeConversation(ctx, invocationInput)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = s.ResolveConversationContext(ctx, next.Message.ID)
	if err != nil || len(resolved.Sources) != 1 || resolved.Sources[0].Revision != second.Revision {
		t.Fatalf("new invocation: %#v %v", resolved, err)
	}
	if resolved.Selection.Repository == nil || resolved.Selection.Repository.Version != 2 {
		t.Fatalf("new repository binding missing: %#v", resolved.Selection)
	}
	if err := s.RevokeContextSource(ctx, "prj_default", first.ID, first.Revision); err != nil {
		t.Fatal(err)
	}
	resolved, err = s.ResolveConversationContext(ctx, invocation.Message.ID)
	if err != nil || resolved.Sources[0].RevokedAt == 0 || resolved.Sources[0].Digest != first.Digest {
		t.Fatalf("revocation: %#v %v", resolved, err)
	}
	exported, err := s.ExportProject(ctx, "prj_default")
	if err != nil {
		t.Fatal(err)
	}
	target, err := Open(ctx, filepath.Join(t.TempDir(), "import.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if err := target.ImportProject(ctx, exported); err != nil {
		t.Fatal(err)
	}
	restored, err := target.ResolveConversationContext(ctx, invocation.Message.ID)
	if err != nil || marshalJSON(restored) != marshalJSON(resolved) {
		t.Fatalf("import context: %#v %v", restored, err)
	}
	reexported, err := target.ExportProject(ctx, "prj_default")
	if err != nil || !bytes.Equal(exported, reexported) {
		t.Fatalf("round trip: %v", err)
	}
	message, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: channel.ID, AuthorType: "human", AuthorID: "operator", Body: "ordinary message"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveConversationContext(ctx, message.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("non invocation context: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM conversation_context WHERE invocation_message_id = ?", next.Message.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveConversationContext(ctx, next.Message.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("legacy invocation selected latest context: %v", err)
	}
}
