package workerapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

// Opt-in fixture for the isolated local Substrate acceptance recipe. The control
// listener is loopback-only and reached through authenticated kubectl forwarding.
// It never runs in the installed Marina service. Deck and the model are synthetic;
// the gateway, SQLite accounting, native harness and injection path are real.
func TestLiveSubstrateGateway(t *testing.T) {
	if os.Getenv("KINT04_SUBSTRATE_FIXTURE") != "1" {
		t.Skip("isolated Substrate fixture only")
	}
	ctx := context.Background()
	dir := t.TempDir()
	j, err := sqlite.Open(ctx, filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	var mu sync.Mutex
	var s *Server
	var attempt rt.Attempt
	var command string
	var outputs []string
	var statuses []int
	var calls int
	var credential string
	var upstreamSecret = "synthetic-first"
	tokenFile := filepath.Join(dir, "provider-token")
	if err := os.WriteFile(tokenFile, []byte(upstreamSecret), 0600); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The outer gateway handler holds mu, making controls and inspection race-free.
		if r.Header.Get("Authorization") != "Bearer "+upstreamSecret {
			http.Error(w, "wrong upstream credential", 403)
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || !localTools(body["tools"]) {
			http.Error(w, "invalid native request", 400)
			return
		}
		if _, ok := body["client_metadata"]; ok {
			http.Error(w, "metadata escaped", 400)
			return
		}
		calls++
		if input, ok := body["input"].([]any); ok {
			for _, v := range input {
				m, _ := v.(map[string]any)
				if m["type"] == "function_call_output" {
					b, _ := json.Marshal(m["output"])
					outputs = append(outputs, string(b))
				}
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		seq := 0
		event := func(kind string, fields map[string]any) {
			fields["type"] = kind
			fields["sequence_number"] = seq
			seq++
			b, _ := json.Marshal(fields)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, b)
		}
		response := map[string]any{"id": fmt.Sprintf("resp_fixture_%d", calls), "object": "response", "status": "in_progress", "output": []any{}}
		event("response.created", map[string]any{"response": response})
		var item map[string]any
		if command != "" {
			args, _ := json.Marshal(map[string]any{"cmd": command, "yield_time_ms": 1000, "max_output_tokens": 2500})
			command = ""
			item = map[string]any{"id": fmt.Sprintf("fc_%d", calls), "call_id": fmt.Sprintf("call_%d", calls), "type": "function_call", "name": "exec_command", "arguments": string(args), "status": "completed"}
			event("response.output_item.added", map[string]any{"output_index": 0, "item": item})
			event("response.function_call_arguments.done", map[string]any{"output_index": 0, "item_id": item["id"], "arguments": string(args)})
		} else {
			part := map[string]any{"type": "output_text", "text": "fixture complete", "annotations": []any{}}
			item = map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "status": "completed", "content": []any{part}}
			event("response.output_item.added", map[string]any{"output_index": 0, "item": item})
			event("response.content_part.added", map[string]any{"item_id": "msg_fixture", "output_index": 0, "content_index": 0, "part": part})
			event("response.output_text.delta", map[string]any{"item_id": "msg_fixture", "output_index": 0, "content_index": 0, "delta": "fixture complete"})
			event("response.output_text.done", map[string]any{"item_id": "msg_fixture", "output_index": 0, "content_index": 0, "text": "fixture complete"})
		}
		event("response.output_item.done", map[string]any{"output_index": 0, "item": item})
		response["status"] = "completed"
		response["output"] = []any{item}
		response["usage"] = map[string]any{"input_tokens": 2, "output_tokens": 1, "total_tokens": 3}
		event("response.completed", map[string]any{"response": response})
	}))
	defer upstream.Close()
	gateway := &http.Server{Addr: ":8443", ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if s == nil {
			http.Error(w, "fixture not initialized", 503)
			return
		}
		recorder := httptest.NewRecorder()
		s.ServeHTTP(recorder, r)
		statuses = append(statuses, recorder.Code)
		for k, v := range recorder.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(recorder.Code)
		io.Copy(w, recorder.Body)
	})}
	defer gateway.Close()
	go func() {
		if err := gateway.ListenAndServeTLS("/fixture/tls.crt", "/fixture/tls.key"); err != nil && err != http.ErrServerClosed {
			t.Error(err)
		}
	}()
	done := make(chan struct{})
	controls := http.NewServeMux()
	controls.HandleFunc("POST /init", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Template, Revision string
			ExpirySeconds      int
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.Template == "" || len(in.Revision) != 64 || in.ExpirySeconds < 1 {
			http.Error(w, "invalid init", 400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if s != nil {
			http.Error(w, "already initialized", 409)
			return
		}
		lease := time.Now().Add(time.Hour).Truncate(time.Millisecond)
		expiry := time.Now().Add(time.Duration(in.ExpirySeconds) * time.Second)
		e, _, err := j.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", LeaseExpiresAt: lease, Budget: execution.Budget{DeadlineAt: lease, MaxAttempts: 2, MaxTokens: 1000000}})
		if err != nil {
			t.Error(err)
			http.Error(w, "execution", 500)
			return
		}
		spec := rt.Specification{IdempotencyKey: in.Template, RepositoryURI: "https://git.example/repo.git", SourceRevision: strings.Repeat("a", 40), ContextURI: "artifact://sha256:" + strings.Repeat("b", 64), ContextDigest: "sha256:" + strings.Repeat("b", 64), ProfileDigest: strings.Repeat("c", 64), HarnessVersion: "kint04-native", Limits: rt.Limits{CPUMillis: 1000, MemoryBytes: 536870912, EphemeralStorageBytes: 1073741824, WallTimeSeconds: 3600, MaxProcesses: 64}, NetworkPolicy: "restricted", FilesystemPolicy: "ephemeral", AuthorityFence: 1, DeadlineAt: lease, PolicyVersion: "fixture", Engine: &rt.EngineSelection{Backend: "kagent", Version: "1.0.0-alpha1", Endpoint: "kint-engine-controller.kint-engine.svc:8083", Namespace: "kint-engine", Harness: "kint-codex", Template: in.Template, Revision: in.Revision}}
		spec.CredentialReferences = []rt.CredentialReference{{Kind: "engine-gateway", Reference: "secret://kint-engine/" + in.Template + "/gateway", ExpiresAt: expiry}}
		e, attempt, _, err = j.EnsureRuntimeAttempt(ctx, e.ID, e.Version, spec)
		if err != nil {
			t.Error(err)
			http.Error(w, "attempt", 500)
			return
		}
		input, _ := json.Marshal(gatewayWorkloadInput("repository", attempt.Specification))
		_, _, _, err = j.SaveCheckpoint(ctx, e.ID, e.Version, execution.CheckpointInput{Key: "worker-input:" + attempt.ID, Phase: "worker_input", Payload: map[string]any{"input": string(input)}})
		if err != nil {
			t.Error(err)
			http.Error(w, "input", 500)
			return
		}
		if _, err = j.EnsureEngineAttempt(ctx, attempt.Specification); err != nil {
			t.Error(err)
			http.Error(w, "engine", 500)
			return
		}
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			t.Error(err)
			return
		}
		p := &provider{run: workcontrol.Run{ID: "run", WorkItemID: "task", AgentID: "agent", TaskVersion: 1, Version: 1, FenceToken: 1, Status: workcontrol.RunRunning, LeaseExpiresAt: &lease}}
		s = New(Config{SigningKey: key, ContextCheck: func(context.Context, execution.Execution) error { return nil }, Model: Model{Endpoint: upstream.URL, Name: "gpt-5", TokenFile: tokenFile, MaxOutputTokens: 1024}}, j, nil, p, nil)
		s.client = upstream.Client()
		credential, err = s.EngineCredential(ctx, attempt.Specification)
		if err != nil {
			t.Error(err)
			http.Error(w, "credential", 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"credential": credential, "attempt": attempt.ID, "expires_at": expiry})
	})
	controls.HandleFunc("POST /bind", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Instance, Context, Route, Revision string }
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.Instance == "" || in.Context == "" || in.Route == "" {
			http.Error(w, "invalid binding", 400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		engine, err := j.GetEngineAttempt(ctx, attempt.ID)
		if err == nil && in.Revision == attempt.Specification.Engine.Revision {
			engine.InstanceID, engine.ContextID, engine.Route = in.Instance, in.Context, in.Route
			engine.Revision, engine.Phase = in.Revision, "ready"
			engine, err = j.UpdateEngineAttempt(ctx, engine)
			if err == nil {
				engine.Phase = "sending"
				_, err = j.UpdateEngineAttempt(ctx, engine)
			}
		} else {
			err = rt.ErrConflict
		}
		if err != nil {
			t.Error(err)
			http.Error(w, "binding", 409)
			return
		}
		w.WriteHeader(204)
	})
	controls.HandleFunc("POST /command", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		b, _ := io.ReadAll(io.LimitReader(r.Body, 16000))
		command = string(b)
		w.WriteHeader(204)
	})
	controls.HandleFunc("POST /cancel", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		e, err := j.GetEngineAttempt(ctx, attempt.ID)
		if err == nil {
			e.CancelRequested = true
			_, err = j.UpdateEngineAttempt(ctx, e)
		}
		if err != nil {
			t.Error(err)
			http.Error(w, "cancel", 500)
			return
		}
		w.WriteHeader(204)
	})
	controls.HandleFunc("POST /rotate", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		upstreamSecret = "synthetic-rotated"
		if err := os.WriteFile(tokenFile, []byte(upstreamSecret), 0600); err != nil {
			t.Error(err)
			http.Error(w, "rotate", 500)
			return
		}
		w.WriteHeader(204)
	})
	controls.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if s == nil {
			http.Error(w, "uninitialized", 503)
			return
		}
		totals, err := j.ExecutionModelTotals(ctx, attempt.ExecutionID)
		if err != nil {
			t.Error(err)
			return
		}
		safe := strings.ReplaceAll(strings.Join(outputs, "\n"), credential, "[redacted]")
		json.NewEncoder(w).Encode(map[string]any{"calls": calls, "statuses": statuses, "outputs": safe, "totals": totals})
	})
	controls.HandleFunc("POST /finish", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204); close(done) })
	control := &http.Server{Addr: "127.0.0.1:8080", Handler: controls, ReadHeaderTimeout: 5 * time.Second}
	defer control.Close()
	go func() {
		if err := control.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			t.Error(err)
		}
	}()
	select {
	case <-done:
	case <-time.After(55 * time.Minute):
		t.Fatal("fixture timed out")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls == 0 {
		t.Error("no native model call reached gateway")
	}
	totals, err := j.ExecutionModelTotals(ctx, attempt.ExecutionID)
	if err != nil || totals.Tokens != int64(3*calls) || totals.PendingCalls != 0 {
		t.Errorf("live model accounting: %+v %v", totals, err)
	}
	t.Logf("real gateway fixture: %d synthetic model calls, %d routed requests", calls, len(statuses))
}
