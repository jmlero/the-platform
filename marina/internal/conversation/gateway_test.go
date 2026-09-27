package conversation

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kubernetes"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGatewayDeniesToolsAndSecondModelCall(t *testing.T) {
	ctx := context.Background()
	journal, err := sqlite.Open(ctx, t.TempDir()+"/journal.db")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	deadline := time.Now().Add(time.Minute).UTC().Truncate(time.Millisecond)
	message := workcontrol.ConversationMessage{ID: "msg_1", ProjectID: "prj_1", ChannelID: "chn_1", ThreadID: "thr_1", AuthorType: "human", Body: "Explain", InvokedAgentID: "agt_1", InvocationRequestID: "req_1"}
	if _, err := journal.EnsureConversationTurn(ctx, message, deadline, 5000); err != nil {
		t.Fatal(err)
	}
	spec := rt.Specification{Workload: "conversation/v1", ExecutionID: message.ID, AttemptID: message.ID, IdempotencyKey: "conversation-" + message.ID,
		InputDigest: strings.Repeat("a", 64), ProfileDigest: strings.Repeat("b", 64), ContextURI: "artifact://sha256:" + strings.Repeat("c", 64), ContextDigest: "sha256:" + strings.Repeat("c", 64),
		HarnessVersion: "conversation-byo/v1", NetworkPolicy: "gateway-only", FilesystemPolicy: "ephemeral", Limits: rt.Limits{CPUMillis: 100, MemoryBytes: 1, EphemeralStorageBytes: 1, WallTimeSeconds: 60, MaxProcesses: 1},
		AuthorityFence: 1, DeadlineAt: deadline, PolicyVersion: "test", Engine: &rt.EngineSelection{Backend: "kagent", Version: "v", Endpoint: "engine", Namespace: "test", Harness: "conversation", Template: "turn-1", Revision: strings.Repeat("d", 64)},
		CredentialReferences: []rt.CredentialReference{{Kind: "engine-gateway", Reference: "secret://test/turn-1/gateway", ExpiresAt: deadline}}}
	if err := journal.SaveConversationSpecification(ctx, message.ID, spec); err != nil {
		t.Fatal(err)
	}
	upstreamCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		upstreamCalls++
		var request struct {
			Text struct {
				Format struct {
					Type   string `json:"type"`
					Name   string `json:"name"`
					Strict bool   `json:"strict"`
					Schema struct {
						Required []string `json:"required"`
					} `json:"schema"`
				} `json:"format"`
			} `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Text.Format.Type != "json_schema" || request.Text.Format.Name != "conversation_reply" || !request.Text.Format.Strict || len(request.Text.Format.Schema.Required) != 3 {
			t.Fatalf("conversation output format not constrained: %+v", request.Text.Format)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"output":[{"type":"message","content":[{"type":"output_text","text":"Hello"}]}],"usage":{"input_tokens":10,"output_tokens":5}}`))}, nil
	})}
	key := []byte(strings.Repeat("k", 32))
	gateway := NewGateway(journal, key, workerapi.Model{Endpoint: "https://model.example/v1", Name: "fixture", MaxOutputTokens: 100, Currency: "USD"}, spec.ProfileDigest, spec.PolicyVersion, client)
	if err := NewGateway(journal, key, workerapi.Model{Name: "changed", MaxOutputTokens: 100}, strings.Repeat("e", 64), spec.PolicyVersion, client).AdmitEngine(ctx, spec); err == nil {
		t.Fatal("changed model profile admitted an existing turn")
	}
	if err := NewGateway(journal, key, workerapi.Model{Name: "fixture", MaxOutputTokens: 100}, spec.ProfileDigest, "changed-policy", client).AdmitEngine(ctx, spec); err == nil {
		t.Fatal("changed policy admitted an existing turn")
	}
	call := func(body string) int {
		r := httptest.NewRequest(http.MethodPost, "https://marina.example/conversation/model/v1/responses", bytes.NewBufferString(body))
		r.TLS = &tls.ConnectionState{}
		r.Header.Set("Authorization", "Bearer "+message.ID+"."+kubernetes.Token(key, spec))
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, r)
		return w.Code
	}
	if status := call(`{"input":"hello","tools":[{"type":"function","name":"shell"}]}`); status != 400 || upstreamCalls != 0 {
		t.Fatalf("tool request: status=%d calls=%d", status, upstreamCalls)
	}
	if status := call(`{"input":"hello"}`); status != 200 || upstreamCalls != 1 {
		t.Fatalf("first call: status=%d calls=%d", status, upstreamCalls)
	}
	if status := call(`{"input":"hello"}`); status != 429 || upstreamCalls != 1 {
		t.Fatalf("duplicate call: status=%d calls=%d", status, upstreamCalls)
	}
	turn, err := journal.GetConversationTurn(ctx, message.ID)
	if err != nil {
		t.Fatal(err)
	}
	if turn.UsedTokens != 15 || turn.ReservedTokens != 0 {
		data, _ := json.Marshal(turn)
		t.Fatalf("usage: %s", data)
	}

	// Only an answer delivered to this turn's clarification admits one more call.
	engine := journal.ConversationEngineJournal()
	record, err := engine.EnsureEngineAttempt(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []func(*rt.EngineAttempt){
		func(r *rt.EngineAttempt) {
			r.Phase, r.State, r.InstanceID, r.Revision, r.Route, r.ContextID = "ready", rt.StateRunning, "instance", spec.Engine.Revision, "route", "context"
		},
		func(r *rt.EngineAttempt) { r.Phase = "sending" },
		func(r *rt.EngineAttempt) { r.Phase, r.TaskID, r.QuestionID = "active", "task", "clarification" },
	} {
		step(&record)
		if record, err = engine.UpdateEngineAttempt(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	if status := call(`{"input":"hello"}`); status != 429 || upstreamCalls != 1 {
		t.Fatalf("follow-up before the answer: status=%d calls=%d", status, upstreamCalls)
	}
	record.AnswerMessageID, record.AnswerDigest, record.AnswerPhase = "marina-answer-"+message.ID, strings.Repeat("f", 64), "sending"
	if record, err = engine.UpdateEngineAttempt(ctx, record); err != nil {
		t.Fatal(err)
	}
	if status := call(`{"input":"hello and the answer"}`); status != 200 || upstreamCalls != 2 {
		t.Fatalf("follow-up after the answer: status=%d calls=%d", status, upstreamCalls)
	}
	if status := call(`{"input":"hello again"}`); status != 429 || upstreamCalls != 2 {
		t.Fatalf("third call: status=%d calls=%d", status, upstreamCalls)
	}
	turn, err = journal.GetConversationTurn(ctx, message.ID)
	if err != nil || turn.UsedTokens != 30 || turn.ReservedTokens != 0 {
		t.Fatalf("usage after follow-up: %+v %v", turn, err)
	}
}
