package appdeck

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func TestCompletionPublishesStructuredEvidenceBeforeGuardedTerminalMutation(t *testing.T) {
	paths := []string{}
	client, err := New("https://deck.example", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["agent_id"] != "agent" || body["expected_version"] != float64(7) || body["fence_token"] != float64(2) || r.Header.Get("Idempotency-Key") == "" {
			t.Fatalf("unguarded mutation: %#v", body)
		}
		if r.URL.Path == "/api/v1/runs/run/evidence" {
			if body["source_revision"] != "head" || body["verifier"] != "verifier" {
				t.Fatalf("evidence lost provenance: %#v", body)
			}
			return jsonResponse(201, map[string]string{"id": "evidence"}), nil
		}
		return jsonResponse(204, nil), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	guard := workcontrol.Mutation{IdempotencyKey: "stable", AgentID: "agent", ExpectedVersion: 7, FenceToken: 2}
	ctx := context.Background()
	if err := client.StartRun(ctx, "run", guard); err != nil {
		t.Fatal(err)
	}
	if err := client.BeginVerification(ctx, "run", guard); err != nil {
		t.Fatal(err)
	}
	id, err := client.RecordEvidence(ctx, "run", guard, workcontrol.EvidenceSubmission{ID: "evidence", SourceRevision: "head", Verifier: "verifier", ObservedAt: time.Now()})
	if err != nil || id != "evidence" {
		t.Fatalf("record evidence: %q %v", id, err)
	}
	if err := client.CompleteRun(ctx, "run", guard, "Ready for review", []string{id}); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 4 || paths[2] != "/api/v1/runs/run/evidence" || paths[3] != "/api/v1/runs/run/complete" {
		t.Fatalf("completion flow: %v", paths)
	}
}

func TestWorkerReadinessRequiresCompletionCapabilities(t *testing.T) {
	client, err := New("https://deck.example", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, map[string]any{"provider": "appdeck", "protocol_version": "1.0", "capabilities": workcontrol.MilestoneOneCapabilities(), "server_time": time.Now().UnixMilli(), "ready": true, "readiness": map[string]string{"store": "ready"}}), nil
	})}, WithRequiredCapabilities(workcontrol.WorkerCapabilities()))
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Health(context.Background()); err == nil {
		t.Fatal("worker mode reported ready without completion support")
	}
}
