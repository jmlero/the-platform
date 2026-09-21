package reconcile_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/reconcile"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

type outboxConflict struct{ reason string }

func (err outboxConflict) Error() string          { return err.reason }
func (err outboxConflict) MutationConflict() bool { return true }

type outboxProvider struct {
	controlProviderStub
	lost, commitBeforeLoss bool
	accepted               map[string]string
	requests               []string
	occurredAt             time.Time
}

func (p *outboxProvider) publish(key string, version, fence int64, request any) error {
	body, _ := json.Marshal(request)
	p.requests = append(p.requests, string(body))
	// Like Deck, compare the complete idempotency payload before the live guard.
	if prior, ok := p.accepted[key]; ok {
		if prior != string(body) {
			return outboxConflict{"idempotency payload changed"}
		}
		return nil
	}
	if version != p.run.Version || fence != p.run.FenceToken {
		return outboxConflict{"stale mutation guard"}
	}
	if !p.lost && !p.commitBeforeLoss {
		p.lost = true
		return context.DeadlineExceeded
	}
	p.accepted[key] = string(body)
	p.run.Version++
	if !p.lost {
		p.lost = true
		return context.DeadlineExceeded
	}
	return nil
}

func (p *outboxProvider) AppendRunEvent(_ context.Context, _ string, event workcontrol.ProjectedEvent) error {
	p.occurredAt = event.OccurredAt
	return p.publish(event.IdempotencyKey, event.ExpectedVersion, event.FenceToken, event)
}

func (p *outboxProvider) AcknowledgeRunCommand(_ context.Context, _, _ string, ack workcontrol.CommandAcknowledgement) error {
	return p.publish(ack.IdempotencyKey, ack.ExpectedVersion, ack.FenceToken, ack)
}

func TestOutboxRecoversLostRequestsAfterAuthoritySyncAndRestart(t *testing.T) {
	for _, kind := range []string{"event", "command_ack"} {
		for _, tc := range []struct{ committed, revoked bool }{{false, false}, {true, false}, {false, true}} {
			t.Run(fmt.Sprintf("%s/committed=%t/revoked=%t", kind, tc.committed, tc.revoked), func(t *testing.T) {
				ctx := context.Background()
				now := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Minute)
				createdAt := now
				lease := now.Add(time.Hour)
				path := filepath.Join(t.TempDir(), "outbox.db")
				journal, err := sqlite.Open(ctx, path, sqlite.WithClock(func() time.Time { return now }))
				if err != nil {
					t.Fatal(err)
				}
				defer func() { journal.Close() }()
				identity, err := journal.EnsureInstallationIdentity(ctx)
				if err != nil {
					t.Fatal(err)
				}
				binding := execution.ProjectBinding{ProjectID: "project", Version: 1}
				record, _, err := journal.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ProjectID: binding.ProjectID, ExternalWorkID: "task", ExternalRunID: "run", AgentID: identity.LogicalAgentID, ExternalRunVersion: 1, FenceToken: 1, LeaseExpiresAt: lease, Budget: execution.Budget{DeadlineAt: lease, MaxAttempts: 2, MaxTokens: 10000}})
				if err != nil {
					t.Fatal(err)
				}
				p := &outboxProvider{controlProviderStub: controlProviderStub{run: workcontrol.Run{ID: "run", WorkItemID: "task", AgentID: record.AgentID, Version: 1, FenceToken: 1, Status: workcontrol.RunPlanning, LeaseExpiresAt: &lease}}, commitBeforeLoss: tc.committed, accepted: map[string]string{}}
				_, _, err = journal.EnqueueOutbox(ctx, record.ID, execution.OutboxInput{Kind: kind, IdempotencyKey: "first", Payload: map[string]any{"type": "execution.progress", "message": "first", "command_id": "command", "status": "rejected", "result": "unsupported"}})
				if err != nil {
					t.Fatal(err)
				}
				newController := func() *reconcile.ControlController {
					controller, err := reconcile.NewControlController(p, journal, binding, identity, time.Minute)
					if err != nil {
						t.Fatal(err)
					}
					return controller.WithIntakeEnabled(false)
				}
				first := newController().Cycle(ctx)
				if len(first.Errors) != 1 || !p.lost {
					t.Fatalf("lost response not injected: %#v", first)
				}
				// A heartbeat advances the provider and the local authority before retry.
				p.run.Version++
				if tc.revoked {
					p.run.FenceToken++
				}
				_, err = journal.UpdateProviderAuthority(ctx, record.ID, record.Version, p.run.Version, p.run.FenceToken, lease, "heartbeat")
				if err != nil {
					t.Fatal(err)
				}
				if err := journal.Close(); err != nil {
					t.Fatal(err)
				}
				now = now.Add(2 * time.Second)
				journal, err = sqlite.Open(ctx, path, sqlite.WithClock(func() time.Time { return now }))
				if err != nil {
					t.Fatal(err)
				}
				published := 0
				for range 3 {
					cycle := newController().Cycle(ctx)
					published += cycle.Published
					now = now.Add(4 * time.Second)
				}
				if tc.revoked {
					if published != 0 || len(p.accepted) != 0 || len(p.requests) != 2 || p.requests[0] != p.requests[1] {
						t.Fatalf("revoked mutation crossed its original fence: published=%d requests=%v", published, p.requests)
					}
					return
				}
				if published != 1 || len(p.accepted) != 1 {
					t.Fatalf("publication did not recover: published=%d accepted=%d requests=%v", published, len(p.accepted), p.requests)
				}
				if p.requests[0] != p.requests[1] {
					t.Fatal("first retry did not replay the persisted request exactly")
				}
				if kind == "event" && !p.occurredAt.Equal(createdAt) {
					t.Fatalf("event without timestamp used retry time: got %s want %s", p.occurredAt, createdAt)
				}
				_, _, err = journal.EnqueueOutbox(ctx, record.ID, execution.OutboxInput{Kind: "event", IdempotencyKey: "second", Payload: map[string]any{"type": "execution.progress", "message": "second"}})
				if err != nil {
					t.Fatal(err)
				}
				cycle := newController().Cycle(ctx)
				if len(cycle.Errors) != 0 || cycle.Published != 1 || len(p.accepted) != 2 {
					t.Fatalf("following event remained blocked: %#v", cycle)
				}
			})
		}
	}
}
