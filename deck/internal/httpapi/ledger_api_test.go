package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/store"
)

func TestStructuredPlanEvidenceAndCompletionAPI(t *testing.T) {
	appStore, handler := newTestStoreAndHandler(t)
	ctx := context.Background()
	task, err := appStore.CreateTask(ctx, domain.CreateTaskInput{
		Title: "Exercise structured completion", Status: domain.TaskReady, Priority: "high", BudgetMinutes: 30,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := appStore.RegisterAgent(ctx, domain.Agent{ID: "agt_api_ledger", Name: "API ledger agent"}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	run, err := appStore.ClaimTask(ctx, store.Mutation{ActorID: "service:test", IdempotencyKey: "claim-api-ledger"}, task.ID, "agt_api_ledger", time.Hour)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}

	planResponse := requestJSON(t, handler, http.MethodPost, "/api/v1/runs/"+run.ID+"/plan", "plan-api-ledger", map[string]any{
		"agent_id": run.AgentID, "expected_version": run.Version, "fence_token": run.FenceToken,
		"plan": "Implement, verify independently, and report.", "context_digest": "sha256:api-context",
		"risk_class": "high", "verification_strategy": "Run focused tests.",
		"expected_side_effects": []string{"write source"}, "rollback_approach": "revert",
		"source_revision": "git:api-head", "policy_version": "policy:api", "require_approval": false,
	})
	if planResponse.Code != http.StatusOK {
		t.Fatalf("plan status = %d, body = %s", planResponse.Code, planResponse.Body.String())
	}
	var planned struct {
		PlanRevision domain.PlanRevision `json:"plan_revision"`
	}
	if err := json.NewDecoder(planResponse.Body).Decode(&planned); err != nil {
		t.Fatalf("decode plan response: %v", err)
	}
	if planned.PlanRevision.Revision != 1 || planned.PlanRevision.SourceRevision != "git:api-head" {
		t.Fatalf("plan revision = %#v", planned.PlanRevision)
	}

	run, err = appStore.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get running run: %v", err)
	}
	verifyResponse := requestJSON(t, handler, http.MethodPost, "/api/v1/runs/"+run.ID+"/verify", "verify-api-ledger", map[string]any{
		"agent_id": run.AgentID, "expected_version": run.Version, "fence_token": run.FenceToken,
	})
	if verifyResponse.Code != http.StatusNoContent {
		t.Fatalf("verify status = %d, body = %s", verifyResponse.Code, verifyResponse.Body.String())
	}
	run, err = appStore.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get verifying run: %v", err)
	}
	missingEvidenceResponse := requestJSON(t, handler, http.MethodPost, "/api/v1/runs/"+run.ID+"/complete", "complete-without-evidence", map[string]any{
		"agent_id": run.AgentID, "expected_version": run.Version, "fence_token": run.FenceToken,
		"summary": "A Markdown assertion is not evidence.",
	})
	if missingEvidenceResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("completion without evidence status = %d, body = %s", missingEvidenceResponse.Code, missingEvidenceResponse.Body.String())
	}
	expiresAt := time.Now().UTC().Add(time.Hour).UnixMilli()
	evidenceResponse := requestJSON(t, handler, http.MethodPost, "/api/v1/runs/"+run.ID+"/evidence", "evidence-api-ledger", map[string]any{
		"agent_id": run.AgentID, "expected_version": run.Version, "fence_token": run.FenceToken,
		"id": "evd_api_ledger", "plan_revision_id": planned.PlanRevision.ID, "type": "test",
		"producer": "agent:builder", "verifier": "agent:reviewer", "subject_digest": "sha256:tree",
		"source_revision": "git:api-head", "command": "go test ./...", "outcome": "passed",
		"provenance_uri": "artifact://test-output", "provenance_digest": "sha256:test-output",
		"observed_at": time.Now().UTC().UnixMilli(), "expires_at": expiresAt,
		"artifact_uri": "artifact://test-report", "artifact_digest": "sha256:test-report",
		"policy_version": "policy:api",
	})
	if evidenceResponse.Code != http.StatusCreated {
		t.Fatalf("evidence status = %d, body = %s", evidenceResponse.Code, evidenceResponse.Body.String())
	}
	var evidence domain.EvidenceRecord
	if err := json.NewDecoder(evidenceResponse.Body).Decode(&evidence); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	if evidence.AdmissionStatus != "current" {
		t.Fatalf("evidence = %#v", evidence)
	}

	completeResponse := requestJSON(t, handler, http.MethodPost, "/api/v1/runs/"+run.ID+"/complete", "complete-api-ledger", map[string]any{
		"agent_id": run.AgentID, "expected_version": run.Version, "fence_token": run.FenceToken,
		"summary": "Structured completion passed.", "evidence_ids": []string{evidence.ID},
	})
	if completeResponse.Code != http.StatusNoContent {
		t.Fatalf("complete status = %d, body = %s", completeResponse.Code, completeResponse.Body.String())
	}
	detail, err := appStore.GetTaskDetail(ctx, task.ID)
	if err != nil {
		t.Fatalf("get completed detail: %v", err)
	}
	if detail.Task.Status != domain.TaskDone || detail.DisplayState != "completed" || len(detail.Artifacts) != 1 {
		t.Fatalf("completed detail = %#v", detail)
	}
}

func TestOperatorChannelPromotionAndExportAPI(t *testing.T) {
	_, handler := newTestStoreAndHandler(t)
	channelResponse := requestJSON(t, handler, http.MethodPost, "/api/v1/operator/channels", "", map[string]any{
		"project_id": "prj_default", "name": "delivery",
	})
	if channelResponse.Code != http.StatusCreated {
		t.Fatalf("channel status = %d, body = %s", channelResponse.Code, channelResponse.Body.String())
	}
	var channel domain.Channel
	if err := json.NewDecoder(channelResponse.Body).Decode(&channel); err != nil {
		t.Fatalf("decode channel: %v", err)
	}
	messageResponse := requestJSON(t, handler, http.MethodPost, "/api/v1/operator/channels/"+channel.ID+"/messages", "", map[string]any{
		"body": "Turn the report into bounded work.", "references": map[string]any{},
	})
	if messageResponse.Code != http.StatusCreated {
		t.Fatalf("message status = %d, body = %s", messageResponse.Code, messageResponse.Body.String())
	}
	var message domain.ChannelMessage
	if err := json.NewDecoder(messageResponse.Body).Decode(&message); err != nil {
		t.Fatalf("decode message: %v", err)
	}
	promoteResponse := requestJSON(t, handler, http.MethodPost, "/api/v1/operator/messages/"+message.ID+"/promote", "", map[string]any{
		"title": "Investigate the report", "acceptance_criteria": "A source-bound diagnosis is recorded.",
		"status": "backlog", "priority": "high",
		"budget": map[string]any{"time_minutes": 30, "max_attempts": 2, "max_tokens": 50000, "max_cost_minor": 1000, "currency": "EUR", "max_concurrency": 1, "authority": []string{"source.read"}},
	})
	if promoteResponse.Code != http.StatusCreated {
		t.Fatalf("promote status = %d, body = %s", promoteResponse.Code, promoteResponse.Body.String())
	}

	exportRequest := httptest.NewRequest(http.MethodGet, "/api/v1/operator/export?project_id=prj_default", nil)
	exportResponse := httptest.NewRecorder()
	handler.ServeHTTP(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusOK || !strings.Contains(exportResponse.Body.String(), `"schema_version": "deck-export/v1"`) {
		t.Fatalf("export status = %d, body = %s", exportResponse.Code, exportResponse.Body.String())
	}
	if strings.Contains(exportResponse.Body.String(), `"content"`) {
		t.Fatalf("export leaked artifact content: %s", exportResponse.Body.String())
	}
}

func requestJSON(t *testing.T, handler http.Handler, method, target, idempotencyKey string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal %s %s: %v", method, target, err)
	}
	request := httptest.NewRequest(method, target, strings.NewReader(string(encoded)))
	request.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
