package sqlite_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func TestRetentionKeepsHistoricalCheckpointReferencesAndRejectsActiveOrWrongStore(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	store := openStore(t, ctx, filepath.Join(t.TempDir(), "marina.db"), &now)
	root := t.TempDir()
	_, err := store.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "p", Provider: "deck", Repository: "test://repo", Runtime: "test", PolicyVersion: "test", ContextConfig: "test", ArtifactConfig: root, DeliveryConfig: "test", BudgetMinutes: 10, MaxConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := store.EnsureExecution(ctx, validExecution(now, "retention-run"))
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	record, _, _, err = store.SaveCheckpoint(ctx, record.ID, record.Version, execution.CheckpointInput{Key: "old-output", Phase: "repository_flow", Payload: map[string]any{"state": `{"implementation_digest":"` + digest + `"}`}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArtifactRetentionReferences(ctx, root); err == nil {
		t.Fatal("active execution allowed deletion")
	}
	record = transition(t, ctx, store, record, execution.StateBlocked, "fixture stopped")
	refs, err := store.ArtifactRetentionReferences(ctx, root)
	if err != nil || !refs[digest] {
		t.Fatalf("historical references=%v err=%v", refs, err)
	}
	if _, err := store.ArtifactRetentionReferences(ctx, t.TempDir()); err == nil {
		t.Fatal("unrelated artifact directory accepted")
	}
}

func TestRetentionProtectsConversationSnapshotsAndRejectsActiveTurns(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	store := openStore(t, ctx, filepath.Join(t.TempDir(), "marina.db"), &now)
	root := t.TempDir()
	_, err := store.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "p", Provider: "deck", Repository: "test://repo", Runtime: "test", PolicyVersion: "test", ContextConfig: "test", ArtifactConfig: root, DeliveryConfig: "test", BudgetMinutes: 10, MaxConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	message := workcontrol.ConversationMessage{ID: "msg_retention", ProjectID: "p", ChannelID: "chn", ThreadID: "thr", AuthorType: "human", Body: "Explain", InvokedAgentID: "agt", InvocationRequestID: "req"}
	_, err = store.EnsureConversationTurn(ctx, message, now.Add(time.Minute), 5000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArtifactRetentionReferences(ctx, root); err == nil {
		t.Fatal("active conversation allowed deletion")
	}
	tree := "sha256:" + strings.Repeat("a", 64)
	_, _, err = store.ConversationEngineJournal().RecordRuntimeArtifact(ctx, rt.Artifact{ID: "tree", ExecutionID: message.ID, RuntimeAttemptID: message.ID, Name: "repository-tree.json", Kind: "conversation-repository-tree", URI: "artifact://" + tree, Digest: tree, SizeBytes: 10, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishConversationTurn(ctx, message.ID, "blocked", "", "test finished"); err != nil {
		t.Fatal(err)
	}
	refs, err := store.ArtifactRetentionReferences(ctx, root)
	if err != nil || !refs[tree] {
		t.Fatalf("conversation tree not retained: %v %v", refs, err)
	}
}
