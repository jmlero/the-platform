package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
)

func TestConcurrentToolDecisionsRetainDistinctOrderedEvents(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	record, _, err := store.EnsureExecution(ctx, execution.NewExecution{ProjectID: "project", WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", LeaseExpiresAt: time.Now().Add(time.Hour), Budget: execution.Budget{DeadlineAt: time.Now().Add(time.Hour), MaxAttempts: 1, MaxTokens: 20000}})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			input := execution.OutboxInput{Kind: "event", IdempotencyKey: fmt.Sprintf("tool:%d", i), Payload: map[string]any{"type": "tool.decision", "uri_digest": fmt.Sprint(i)}}
			first, created, err := store.EnqueueOutbox(ctx, record.ID, input)
			if err != nil || !created {
				t.Errorf("decision %d: created=%v error=%v", i, created, err)
				return
			}
			replay, created, err := store.EnqueueOutbox(ctx, record.ID, input)
			if err != nil || created || first.ID != replay.ID {
				t.Errorf("replay %d: created=%v error=%v", i, created, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	for sequence := 1; sequence <= 32; sequence++ {
		items, err := store.ListReadyOutbox(ctx, 100)
		if err != nil || len(items) != 1 {
			t.Fatalf("sequence %d: %d items, %v", sequence, len(items), err)
		}
		if items[0].Sequence != int64(sequence) {
			t.Fatalf("sequence %d: got %d", sequence, items[0].Sequence)
		}
		if err := store.MarkOutboxPublished(ctx, items[0].ID); err != nil {
			t.Fatal(err)
		}
	}
}
