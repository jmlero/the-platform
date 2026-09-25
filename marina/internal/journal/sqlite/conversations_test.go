package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func TestConversationTurnReplayAndUnknownModelCallStayBounded(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/journal.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	message := workcontrol.ConversationMessage{ID: "msg_1", ProjectID: "prj_1", ChannelID: "chn_1", ThreadID: "thr_1", AuthorType: "human", Body: "Explain the migration", InvokedAgentID: "agt_1", InvocationRequestID: "request-1", CreatedAt: time.Now().UnixMilli()}
	deadline := time.Now().Add(time.Minute)
	first, err := store.EnsureConversationTurn(ctx, message, deadline, 1000)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.EnsureConversationTurn(ctx, message, deadline, 1000)
	if err != nil || second.MessageID != first.MessageID {
		t.Fatalf("replay: %+v %v", second, err)
	}
	message.Body = "changed request"
	if _, err := store.EnsureConversationTurn(ctx, message, deadline, 1000); err == nil {
		t.Fatal("changed invocation replay accepted")
	}
	if err := store.SaveConversationSpecification(ctx, first.MessageID, rt.Specification{DeadlineAt: first.DeadlineAt}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveConversationCall(ctx, first.MessageID, 800); err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveConversationCall(ctx, first.MessageID, 1); err == nil {
		t.Fatal("second model call accepted")
	}
	turn, err := store.GetConversationTurn(ctx, first.MessageID)
	if err != nil || turn.ReservedTokens != 800 || turn.UsedTokens != 0 {
		t.Fatalf("reservation: %+v %v", turn, err)
	}
	if err := store.CompleteConversationCall(ctx, first.MessageID, 300); err != nil {
		t.Fatal(err)
	}
	turn, err = store.GetConversationTurn(ctx, first.MessageID)
	if err != nil || turn.ReservedTokens != 0 || turn.UsedTokens != 300 {
		t.Fatalf("accounting: %+v %v", turn, err)
	}
}
