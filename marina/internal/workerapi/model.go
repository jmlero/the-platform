package workerapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/execution"
)

func forbiddenModelInput(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		if kind, _ := value["type"].(string); kind == "input_image" || kind == "input_file" || kind == "item_reference" || kind == "web_search" || kind == "web_search_preview" || kind == "file_search" || kind == "mcp" || kind == "code_interpreter" || kind == "image_generation" {
			return true
		}
		for key, child := range value {
			if (key == "previous_response_id" || key == "conversation") && child != nil {
				return true
			}
			if forbiddenModelInput(child) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if forbiddenModelInput(child) {
				return true
			}
		}
	}
	return false
}

func price(tokens, rate int64) int64 { return (tokens*rate + 999999) / 1000000 }

func localTools(value any) bool {
	if value == nil {
		return true
	}
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		tool, ok := item.(map[string]any)
		if !ok {
			return false
		}
		switch tool["type"] {
		case "function", "custom":
		case "namespace":
			if !localTools(tool["tools"]) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (server *Server) model(w http.ResponseWriter, r *http.Request) {
	attempt, record, ok := server.admit(w, r)
	if !ok {
		return
	}
	input, err := server.Input(r.Context(), attempt)
	if err != nil || (input.Stage != "implement" && input.Stage != "analyze") {
		http.Error(w, "this worker has no model authority", 403)
		return
	}
	model := server.config.Model
	if model.Endpoint == "" || model.Name == "" || model.MaxOutputTokens < 1 {
		http.Error(w, "model endpoint is not configured", 503)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if err != nil {
		http.Error(w, "model input exceeds 256 KiB", 413)
		return
	}
	var requestBody map[string]any
	if json.Unmarshal(body, &requestBody) != nil || requestBody == nil {
		http.Error(w, "invalid model input", 400)
		return
	}
	if attempt.Specification.Engine != nil && input.Stage == "implement" {
		normalizeNativeModel(requestBody)
	}
	if forbiddenModelInput(requestBody) || !localTools(requestBody["tools"]) {
		http.Error(w, "only self-contained text and local tools are supported", 400)
		return
	}
	if attempt.Specification.Engine != nil && !engineModelFields(requestBody) {
		http.Error(w, "unsupported engine Responses field", 400)
		return
	}
	if input.Stage == "analyze" && !analysisTools(requestBody["tools"], attempt.Specification.Tools) {
		http.Error(w, "analysis tool is not in the frozen profile allowlist", 403)
		return
	}
	requestBody["model"] = model.Name
	requestBody["store"] = false
	requestBody["max_output_tokens"] = model.MaxOutputTokens
	encoded, _ := json.Marshal(requestBody)
	var credential string
	if model.TokenFile != "" {
		token, err := os.ReadFile(model.TokenFile)
		credential = strings.TrimSpace(string(token))
		if err != nil || credential == "" {
			http.Error(w, "model credential unavailable", 503)
			return
		}
	}
	// Byte-count reservation is conservative for the supported text tokenizers.
	// Unknown responses keep their reservation; final upstream usage is recorded.
	inputReservation := int64(len(encoded)) + 1024
	reservation := inputReservation + model.MaxOutputTokens
	cost := price(inputReservation, model.InputMicrosPerMillion) + price(model.MaxOutputTokens, model.OutputMicrosPerMillion)
	callID, err := server.journal.ReserveModelCall(r.Context(), record.ID, attempt.ID, record.FenceToken, reservation, cost)
	if err != nil {
		if errors.Is(err, execution.ErrBudgetExhausted) {
			// Preserve the fixed budget reason after ephemeral worker cleanup. The
			// stable key prevents harness retries from flooding the operator ledger.
			_, _, saveErr := server.journal.EnqueueOutbox(r.Context(), record.ID, execution.OutboxInput{
				Kind: "event", IdempotencyKey: "model-budget:" + attempt.ID + ":" + artifacts.Digest([]byte(err.Error())),
				Payload: map[string]any{"type": "model.budget_exhausted", "message": err.Error()},
			})
			if saveErr != nil {
				http.Error(w, "model budget denial could not be recorded", 503)
				return
			}
		}
		http.Error(w, "model request exceeds remaining authority or budget", 429)
		return
	}
	var usage struct {
		Input  int64 `json:"input_tokens"`
		Output int64 `json:"output_tokens"`
	}
	known := false
	defer func() {
		if known {
			accountCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
			defer cancel()
			_ = server.journal.FinishModelCall(accountCtx, callID, usage.Input+usage.Output, price(usage.Input, model.InputMicrosPerMillion)+price(usage.Output, model.OutputMicrosPerMillion))
		}
		reportCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer cancel()
		if totals, err := server.journal.ExecutionModelTotals(reportCtx, record.ID); err == nil {
			_, _, _ = server.journal.EnqueueOutbox(reportCtx, record.ID, execution.OutboxInput{Kind: "event", IdempotencyKey: "model-usage:" + callID, Payload: map[string]any{"type": "model.usage", "message": "Model usage recorded; unresolved calls retain their reservations", "execution_id": record.ID, "attempt_id": attempt.ID, "model_call_id": callID, "reported_tokens": totals.Tokens, "reported_cost_micros": totals.CostMicros, "pending_model_calls": totals.PendingCalls, "reserved_tokens": totals.ReservedTokens, "reserved_cost_micros": totals.ReservedCostMicros, "currency": model.Currency}})
		}
	}()
	ctx, cancel := server.liveContext(r)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(model.Endpoint, "/")+"/responses", bytes.NewReader(encoded))
	if err != nil {
		http.Error(w, "invalid model endpoint", 503)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream, application/json")
	if model.TokenFile != "" {
		request.Header.Set("Authorization", "Bearer "+credential)
	}
	response, err := server.client.Do(request)
	if err != nil {
		http.Error(w, "model endpoint unavailable", 502)
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		http.Error(w, "model endpoint rejected the request", 502)
		return
	}
	w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "no-store")
	streaming := strings.Contains(response.Header.Get("Content-Type"), "text/event-stream")
	observe := func(data []byte) {
		type tokenUsage struct {
			Input  *int64 `json:"input_tokens"`
			Output *int64 `json:"output_tokens"`
		}
		var event struct {
			Type     string      `json:"type"`
			Status   string      `json:"status"`
			Usage    *tokenUsage `json:"usage"`
			Response struct {
				Status string      `json:"status"`
				Usage  *tokenUsage `json:"usage"`
			} `json:"response"`
		}
		if json.Unmarshal(data, &event) != nil {
			return
		}
		candidate, status := event.Usage, event.Status
		if streaming {
			candidate, status = event.Response.Usage, event.Response.Status
			if event.Type != "response."+status {
				return
			}
		}
		// Intermediate usage is only a partial count. Missing counters are
		// unknown, not zero; neither may release a durable reservation.
		if status != "completed" && status != "incomplete" && status != "failed" {
			return
		}
		if candidate != nil && candidate.Input != nil && candidate.Output != nil &&
			*candidate.Input >= 0 && *candidate.Output >= 0 && *candidate.Input < 1<<32 && *candidate.Output < 1<<32 {
			usage.Input = *candidate.Input
			usage.Output = *candidate.Output
			known = true
		}
	}
	if streaming {
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 4096), 2<<20)
		for scanner.Scan() {
			line := scanner.Bytes()
			if bytes.HasPrefix(line, []byte("data:")) {
				observe(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))))
			}
			if _, err := w.Write(append(append([]byte{}, line...), '\n')); err != nil {
				return
			}
			if flush, ok := w.(http.Flusher); ok {
				flush.Flush()
			}
		}
		if scanner.Err() != nil {
			return
		}
	} else {
		payload, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
		if err != nil {
			return
		}
		observe(payload)
		_, _ = w.Write(payload)
	}
}

// The pinned native Codex advertises web search even with no template tools.
// Remove only its known declaration before forwarding: the model cannot select
// an undeclared remote tool. Explicit search choices, other search shapes and
// provider tools still fail validation. Transport-only client metadata is not
// model context and is never forwarded to the upstream provider.
func normalizeNativeModel(body map[string]any) {
	delete(body, "client_metadata")
	if tools, ok := body["tools"].([]any); ok {
		local := make([]any, 0, len(tools))
		for _, value := range tools {
			tool, ok := value.(map[string]any)
			if _, flag := tool["external_web_access"].(bool); ok && flag && len(tool) == 2 && tool["type"] == "web_search" {
				continue
			}
			local = append(local, value)
		}
		body["tools"] = local
	}
}

// Native runtimes use the existing self-contained Responses contract. Do not
// pass new provider options through by default: background jobs, stored prompts,
// server conversations and remote tools would escape the governed call/context.
func engineModelFields(body map[string]any) bool {
	for key, value := range body {
		switch key {
		case "model", "input", "instructions", "tools", "parallel_tool_calls", "reasoning", "stream", "store", "include", "prompt_cache_key", "text", "temperature", "top_p", "max_output_tokens", "metadata", "service_tier", "truncation":
		case "tool_choice":
			if choice, ok := value.(string); ok && choice != "auto" && choice != "none" && choice != "required" {
				return false
			}
		case "previous_response_id", "conversation":
			if value != nil {
				return false
			}
		case "background":
			if value != nil && value != false {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// DiagnosticReport is an output schema, not a tool with external side effects.
// HTTP context reads require the immutable runtime allowlist as well.
func analysisTools(value any, allowed []string) bool {
	if value == nil {
		return true
	}
	items, ok := value.([]any)
	if !ok || len(items) > 1+len(allowed) {
		return false
	}
	for _, item := range items {
		tool, ok := item.(map[string]any)
		if !ok || tool["type"] != "function" || (tool["name"] != "DiagnosticReport" && !(tool["name"] == "read_context_source" && slices.Contains(allowed, "read_context_source"))) {
			return false
		}
	}
	return true
}
