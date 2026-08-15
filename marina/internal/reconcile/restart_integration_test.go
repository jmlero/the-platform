package reconcile_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/appdeck"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/reconcile"
)

func TestRestartCycleRenewsExactPersistedRunWithoutClaiming(t *testing.T) {
	ctx := context.Background()
	journalTime := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	databasePath := filepath.Join(t.TempDir(), "marina.db")
	store, err := sqlite.Open(ctx, databasePath, sqlite.WithClock(func() time.Time { return journalTime }))
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	record, _, err := store.EnsureExecution(ctx, execution.NewExecution{
		WorkProvider:   "appdeck",
		ExternalWorkID: "task-1",
		ExternalRunID:  "run-1",
		AgentID:        "marina-1",
		LeaseExpiresAt: journalTime.Add(5 * time.Minute),
		Budget: execution.Budget{
			DeadlineAt:  journalTime.Add(time.Hour),
			MaxAttempts: 3,
			MaxTokens:   10_000,
		},
	})
	if err != nil {
		t.Fatalf("ensure execution: %v", err)
	}
	record, err = store.Transition(ctx, record.ID, execution.Transition{
		ExpectedVersion: record.Version,
		Target:          execution.StateRunning,
		Message:         "runtime started before restart",
	})
	if err != nil {
		t.Fatalf("transition execution: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close journal before restart: %v", err)
	}

	journalTime = journalTime.Add(6 * time.Minute)
	store, err = sqlite.Open(ctx, databasePath, sqlite.WithClock(func() time.Time { return journalTime }))
	if err != nil {
		t.Fatalf("reopen journal: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	providerLease := journalTime.Add(-time.Minute)
	providerVersion := int64(1)
	heartbeatSeen := false
	calls := make([]string, 0, 4)
	client, err := appdeck.New("http://appdeck.test", &http.Client{Transport: integrationRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls = append(calls, request.Method+" "+request.URL.Path)
		switch request.Method + " " + request.URL.Path {
		case "GET /api/v1/provider":
			return integrationJSONResponse(http.StatusOK, map[string]any{
				"provider":         "appdeck",
				"protocol_version": "1.0",
				"capabilities": []string{
					"provider.info", "agent.register", "work.ready.list", "work.inspect",
					"work.claim.atomic", "work.claim.lookup", "run.assigned.list",
					"run.lease.renew", "run.block", "command.list", "command.ack", "event.append",
				},
				"server_time": journalTime.UnixMilli(),
				"ready":       true,
				"readiness":   map[string]string{"store": "available"},
			}), nil
		case "GET /api/v1/tasks/task-1":
			return integrationJSONResponse(http.StatusOK, map[string]any{
				"task": map[string]any{
					"id":         "task-1",
					"project_id": "project-1",
					"title":      "Persisted work",
					"status":     "running",
				},
				"runs": []map[string]any{{
					"id":                         "run-1",
					"task_id":                    "task-1",
					"agent_id":                   "marina-1",
					"status":                     "running",
					"version":                    providerVersion,
					"fence_token":                1,
					"task_version":               1,
					"acceptance_criteria_digest": "legacy:task-1",
					"lease_expires_at":           providerLease.UnixMilli(),
				}},
			}), nil
		case "POST /api/v1/runs/run-1/heartbeat":
			var body struct {
				AgentID      string `json:"agent_id"`
				LeaseSeconds int    `json:"lease_seconds"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				return nil, err
			}
			if body.AgentID != "marina-1" || body.LeaseSeconds != 30 {
				t.Errorf("unexpected heartbeat: %#v", body)
			}
			heartbeatSeen = true
			providerVersion++
			providerLease = journalTime.Add(30 * time.Second)
			return integrationJSONResponse(http.StatusNoContent, nil), nil
		default:
			t.Fatalf("unexpected provider call %s %s", request.Method, request.URL.Path)
			return nil, nil
		}
	})})
	if err != nil {
		t.Fatalf("create AppDeck adapter: %v", err)
	}

	planner, err := reconcile.NewPlanner(store, 10*time.Second)
	if err != nil {
		t.Fatalf("new planner: %v", err)
	}
	planner.WithClock(func() time.Time { return journalTime })
	providerReconciler, err := reconcile.NewLeaseReconciler(client, store, 30*time.Second)
	if err != nil {
		t.Fatalf("new lease reconciler: %v", err)
	}
	providerReconciler.WithClock(func() time.Time { return journalTime })
	manager, err := reconcile.NewLeaseManager(planner, providerReconciler, time.Minute, 10*time.Second, 100)
	if err != nil {
		t.Fatalf("new lease manager: %v", err)
	}
	manager.WithBudgetStopper(providerReconciler)

	cycle := manager.Cycle(ctx)
	if cycle.Err != nil || len(cycle.Attempts) != 1 || cycle.Attempts[0].Err != nil {
		t.Fatalf("restart cycle = %#v", cycle)
	}
	if cycle.Attempts[0].Intent.Action != execution.RecoveryReconcileLease || cycle.Attempts[0].Result.Status != reconcile.LeaseRenewed {
		t.Fatalf("restart attempt = %#v", cycle.Attempts[0])
	}
	if !heartbeatSeen {
		t.Fatal("persisted provider run was not heartbeated")
	}
	wantCalls := []string{
		"GET /api/v1/provider",
		"GET /api/v1/tasks/task-1",
		"POST /api/v1/runs/run-1/heartbeat",
		"GET /api/v1/tasks/task-1",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("provider calls = %#v, want %#v", calls, wantCalls)
	}

	persisted, err := store.GetExecution(ctx, record.ID)
	if err != nil {
		t.Fatalf("load reconciled execution: %v", err)
	}
	if persisted.State != execution.StateRunning || !persisted.LeaseExpiresAt.Equal(providerLease) || persisted.Version != record.Version+1 {
		t.Fatalf("persisted execution = %#v", persisted)
	}
}

type integrationRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip integrationRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func integrationJSONResponse(status int, payload any) *http.Response {
	var body bytes.Buffer
	if payload != nil {
		_ = json.NewEncoder(&body).Encode(payload)
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body.Bytes())),
	}
}
