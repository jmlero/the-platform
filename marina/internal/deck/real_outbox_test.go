package deck

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/reconcile"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

type lostOutboxTransport struct {
	suffix             string
	committed, dropped bool
	requests           []string
}

func (transport *lostOutboxTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	matched := r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, transport.suffix)
	if matched {
		body, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			return nil, err
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		transport.requests = append(transport.requests, r.Header.Get("Idempotency-Key")+" "+string(body))
		if !transport.dropped && !transport.committed {
			transport.dropped = true
			r.Body.Close()
			return nil, context.DeadlineExceeded
		}
	}
	response, err := http.DefaultTransport.RoundTrip(r)
	if matched && !transport.dropped && err == nil && response.StatusCode < 300 {
		transport.dropped = true
		response.Body.Close()
		return nil, context.DeadlineExceeded
	}
	return response, err
}

func TestRealOutboxRecovery(t *testing.T) {
	if os.Getenv("DECK_CONFORMANCE_URL") == "" {
		t.Skip("run make conformance for real-process outbox recovery")
	}
	for _, kind := range []string{"event", "command_ack"} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/committed=%t", kind, committed), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				now := time.Now().UTC()
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
				binding, err := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "prj_default", Provider: "deck", Repository: "disabled://conformance", Runtime: "test", PolicyVersion: "test", ContextConfig: "test", ArtifactConfig: "test", DeliveryConfig: "test", BudgetMinutes: 30, MaxConcurrency: 1})
				if err != nil {
					t.Fatal(err)
				}
				transport := &lostOutboxTransport{suffix: "/events", committed: committed}
				if kind == "command_ack" {
					transport.suffix = "/ack"
				}
				client, err := New(os.Getenv("DECK_CONFORMANCE_URL"), &http.Client{Transport: transport, Timeout: 3 * time.Second}, WithBearerTokenFile(os.Getenv("DECK_CONFORMANCE_TOKEN_FILE")))
				if err != nil {
					t.Fatal(err)
				}
				if err := waitForProvider(ctx, client); err != nil {
					t.Fatal(err)
				}
				_, err = client.RegisterAgent(ctx, workcontrol.AgentRegistration{ID: identity.LogicalAgentID, InstallationID: identity.InstallationID, Name: "Outbox recovery proof", Kind: "controller", Capabilities: workcontrol.WorkerCapabilities()})
				if err != nil {
					t.Fatal(err)
				}
				var task taskDTO
				err = client.do(ctx, "POST", "/api/v1/tasks", map[string]any{"project_id": "prj_default", "title": "Outbox recovery", "acceptance_criteria": "Retry publishes once and preserves order", "status": "ready", "priority": "medium", "budget_minutes": 30}, &task)
				if err != nil {
					t.Fatal(err)
				}
				run, err := client.ClaimWork(ctx, task.ID, workcontrol.Claim{IdempotencyKey: "outbox:" + task.ID, AgentID: identity.LogicalAgentID, ExpectedTaskVersion: task.Version, Lease: 2 * time.Minute})
				if err != nil {
					t.Fatal(err)
				}
				payload := map[string]any{"type": "execution.progress", "message": "first"}
				var command commandDTO
				if kind == "command_ack" {
					err = client.do(ctx, "POST", "/api/v1/operator/runs/"+run.ID+"/commands", map[string]string{"type": "pause", "reason": "outbox recovery proof"}, &command)
					if err != nil {
						t.Fatal(err)
					}
					run.Version, run.FenceToken = command.RunVersion, command.FenceToken
					payload = map[string]any{"command_id": command.ID, "status": "rejected", "result": "unsupported by configured workflow"}
				}
				e, _, err := journal.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ProjectID: binding.ProjectID, ExternalWorkID: task.ID, ExternalRunID: run.ID, AgentID: run.AgentID, ExternalRunVersion: run.Version, FenceToken: run.FenceToken, TaskVersion: task.Version, BindingVersion: binding.Version, LeaseExpiresAt: *run.LeaseExpiresAt, Budget: execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 2, MaxTokens: 10000}})
				if err != nil {
					t.Fatal(err)
				}
				_, _, err = journal.EnqueueOutbox(ctx, e.ID, execution.OutboxInput{Kind: kind, IdempotencyKey: e.ID + ":first", Payload: payload})
				if err != nil {
					t.Fatal(err)
				}
				if kind == "command_ack" {
					e, err = journal.SetCommandCursor(ctx, e.ID, e.Version, command.Sequence)
					if err != nil {
						t.Fatal(err)
					}
				}
				_, _, err = journal.EnqueueOutbox(ctx, e.ID, execution.OutboxInput{Kind: "event", IdempotencyKey: e.ID + ":second", Payload: map[string]any{"type": "execution.progress", "message": "second"}})
				if err != nil {
					t.Fatal(err)
				}
				newController := func() *reconcile.ControlController {
					controller, err := reconcile.NewControlController(client, journal, binding, identity, 2*time.Minute)
					if err != nil {
						t.Fatal(err)
					}
					return controller.WithIntakeEnabled(false)
				}
				first := newController().Cycle(ctx)
				if !transport.dropped || len(first.Errors) == 0 {
					t.Fatalf("loss was not injected: %#v", first)
				}
				lease, err := reconcile.NewLeaseReconciler(client, journal, 2*time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = lease.Reconcile(ctx, e.ID, 3*time.Minute); err != nil {
					t.Fatal(err)
				}
				if err := journal.Close(); err != nil {
					t.Fatal(err)
				}
				now = now.Add(4 * time.Second)
				journal, err = sqlite.Open(ctx, path, sqlite.WithClock(func() time.Time { return now }))
				if err != nil {
					t.Fatal(err)
				}
				published := first.Published
				for range 4 {
					published += newController().Cycle(ctx).Published
					now = now.Add(8 * time.Second)
				}
				if published != 2 {
					t.Fatalf("outbox remained stuck: published=%d requests=%v", published, transport.requests)
				}
				if len(transport.requests) < 2 || transport.requests[0] != transport.requests[1] {
					t.Fatal("restart changed the first retry's wire request")
				}
				detail, err := client.GetWork(ctx, task.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(detail.Runs) != 1 || detail.Runs[0].Version != run.Version+3 {
					t.Fatalf("expected one heartbeat and exactly two publications, got runs=%+v", detail.Runs)
				}
				if kind == "command_ack" {
					commands, err := client.ListRunCommands(ctx, run.ID, 0, 10)
					if err != nil || len(commands) != 1 || commands[0].Status != "rejected" {
						t.Fatalf("command did not converge: %+v %v", commands, err)
					}
				}
			})
		}
	}
}
