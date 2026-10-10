package workerapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	rt "github.com/jmlero/the-platform/marina/internal/runtime"
)

func TestRepairPlannerKeepsItsStageAndUsesTheOriginalModelBudget(t *testing.T) {
	s, first, _, restart := engineFixture(t)
	ctx := context.Background()
	e, err := s.journal.GetExecution(ctx, first.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	spec := first.Specification
	selected := *spec.Engine
	selected.Template = "repair-planning"
	spec.Engine = &selected
	spec.CredentialReferences = []rt.CredentialReference{{Kind: "engine-gateway", Reference: "secret://trial/repair-planning/gateway", ExpiresAt: spec.DeadlineAt}}
	spec.Stage, spec.IdempotencyKey = "repair_plan", "repair:1:repair_plan"
	_, attempt, _, err := s.journal.EnsureRuntimeAttempt(ctx, e.ID, e.Version, spec)
	if err != nil {
		t.Fatal(err)
	}
	bindEngineForCalls(t, s.journal, attempt.Specification)
	restart()
	input, err := s.Input(ctx, attempt)
	if err != nil || input.Stage != "repair_plan" {
		t.Fatalf("planner stage was lost: %+v %v", input, err)
	}
	token, err := s.EngineCredential(ctx, attempt.Specification)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"completed","usage":{"input_tokens":2,"output_tokens":1}}`)
	}))
	defer upstream.Close()
	s.client, s.config.Model = upstream.Client(), Model{Endpoint: upstream.URL, Name: "pinned", MaxOutputTokens: 100}
	if w := engineRequest(s, "POST", "/engine/model/v1/responses", token, `{"input":"diagnose"}`); w.Code != 200 {
		t.Fatalf("planner model denied: %d %s", w.Code, w.Body.String())
	}
	totals, err := s.journal.ExecutionModelTotals(ctx, e.ID)
	if err != nil || totals.Tokens != 3 || totals.PendingCalls != 0 {
		t.Fatalf("planner usage not charged to original execution: %+v %v", totals, err)
	}
	changed := attempt.Specification
	changed.Stage = ""
	if _, err := s.EngineCredential(ctx, changed); err == nil {
		t.Fatal("planner credential accepted a changed stage")
	}
	if w := engineRequest(s, "POST", "/engine/git/git-receive-pack", token, "push"); w.Code != 404 {
		t.Fatal("planner acquired Git write access")
	}
}
