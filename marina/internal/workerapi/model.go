package workerapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
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
	if err != nil || input.Stage != "implement" {
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
	if json.Unmarshal(body, &requestBody) != nil || requestBody == nil || forbiddenModelInput(requestBody) || !localTools(requestBody["tools"]) {
		http.Error(w, "only self-contained text and local tools are supported", 400)
		return
	}
	requestBody["model"] = model.Name
	requestBody["store"] = false
	requestBody["max_output_tokens"] = model.MaxOutputTokens
	encoded, _ := json.Marshal(requestBody)
	// Byte-count reservation is conservative for the supported text tokenizers.
	// Unknown responses keep their reservation; final upstream usage is recorded.
	inputReservation := int64(len(encoded)) + 1024
	reservation := inputReservation + model.MaxOutputTokens
	cost := price(inputReservation, model.InputMicrosPerMillion) + price(model.MaxOutputTokens, model.OutputMicrosPerMillion)
	callID, err := server.journal.ReserveModelCall(r.Context(), record.ID, attempt.ID, record.FenceToken, reservation, cost)
	if err != nil {
		http.Error(w, "model request exceeds remaining authority or budget", 429)
		return
	}
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
		token, err := os.ReadFile(model.TokenFile)
		if err != nil {
			http.Error(w, "model credential unavailable", 503)
			return
		}
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
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
	}()
	observe := func(data []byte) {
		var event struct {
			Usage *struct {
				Input  int64 `json:"input_tokens"`
				Output int64 `json:"output_tokens"`
			} `json:"usage"`
			Response struct {
				Usage *struct {
					Input  int64 `json:"input_tokens"`
					Output int64 `json:"output_tokens"`
				} `json:"usage"`
			} `json:"response"`
		}
		if json.Unmarshal(data, &event) != nil {
			return
		}
		candidate := event.Usage
		if candidate == nil {
			candidate = event.Response.Usage
		}
		if candidate != nil && candidate.Input >= 0 && candidate.Output >= 0 && candidate.Input < 1<<32 && candidate.Output < 1<<32 {
			usage.Input = candidate.Input
			usage.Output = candidate.Output
			known = true
		}
	}
	if strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
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
