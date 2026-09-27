package conversation

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kubernetes"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
)

// Gateway grants one text-only model call to a single immutable conversation
// turn, and one follow-up after the turn's clarification question was answered.
// It has no source, shell, tool, task, or result-publication route.
type Gateway struct {
	journal       *sqlite.Store
	signingKey    []byte
	model         workerapi.Model
	profileDigest string
	policyVersion string
	client        *http.Client
}

// The gateway, rather than the actor, fixes the upstream output contract.
// A missing delegation is represented as null so strict-schema providers can
// require every top-level property without granting the model extra fields.
var conversationResponseFormat = json.RawMessage(`{
  "format": {
    "type": "json_schema",
    "name": "conversation_reply",
    "strict": true,
    "schema": {
      "type": "object",
      "properties": {
        "answer": {"type": "string", "description": "Plain answer for the operator, without claiming an action already happened"},
        "delegation": {
          "anyOf": [
            {"type": "null"},
            {
              "type": "object",
              "properties": {
                "title": {"type": "string"},
                "description": {"type": "string"},
                "acceptance_criteria": {"type": "string"},
                "mode": {"type": "string", "enum": ["ready", "draft"]},
                "followup_task_id": {"type": "string"}
              },
              "required": ["title", "description", "acceptance_criteria", "mode", "followup_task_id"],
              "additionalProperties": false
            }
          ]
        },
        "question": {
          "anyOf": [
            {"type": "null"},
            {
              "type": "object",
              "properties": {
                "text": {"type": "string"},
                "choices": {"type": "array", "items": {"type": "string"}}
              },
              "required": ["text", "choices"],
              "additionalProperties": false
            }
          ]
        }
      },
      "required": ["answer", "delegation", "question"],
      "additionalProperties": false
    }
  }
}`)

func NewGateway(journal *sqlite.Store, signingKey []byte, model workerapi.Model, profileDigest, policyVersion string, client *http.Client) *Gateway {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Gateway{journal: journal, signingKey: append([]byte(nil), signingKey...), model: model,
		profileDigest: profileDigest, policyVersion: policyVersion, client: &copyClient}
}

func (g *Gateway) AdmitEngine(ctx context.Context, spec rt.Specification) error {
	if len(g.signingKey) < 32 || len(g.profileDigest) != 64 || g.policyVersion == "" ||
		spec.ProfileDigest != g.profileDigest || spec.PolicyVersion != g.policyVersion ||
		spec.Workload != "conversation/v1" || spec.Validate(time.Now()) != nil {
		return rt.ErrInvalidInput
	}
	turn, err := g.journal.GetConversationTurn(ctx, spec.AttemptID)
	if err != nil {
		return err
	}
	digest, err := spec.Digest()
	if err != nil || digest != turn.SpecificationDigest || turn.Status != "running" || turn.MessageID != spec.ExecutionID ||
		!time.Now().Before(turn.DeadlineAt) || turn.MaxTokens < 1 {
		return rt.ErrConflict
	}
	engine, err := g.journal.ConversationEngineJournal().GetEngineAttempt(ctx, spec.AttemptID)
	if err == nil && (engine.CancelRequested || engine.DeleteRequested || engine.Deleted || engine.State.Terminal()) {
		return rt.ErrConflict
	}
	return nil
}

func (g *Gateway) EngineCredential(ctx context.Context, spec rt.Specification) (string, error) {
	if err := g.AdmitEngine(ctx, spec); err != nil {
		return "", err
	}
	return spec.AttemptID + "." + kubernetes.Token(g.signingKey, spec), nil
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/conversation/model/v1/responses" {
		http.NotFound(w, r)
		return
	}
	if r.TLS == nil {
		http.Error(w, "TLS required", 403)
		return
	}
	bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	id, token, found := strings.Cut(bearer, ".")
	if !ok || !found || len(id) > 128 || !strings.HasPrefix(id, "msg_") || len(token) != 64 || strings.ContainsAny(id, "/\\ \t\r\n") {
		http.Error(w, "turn credential required", 401)
		return
	}
	turn, err := g.journal.GetConversationTurn(r.Context(), id)
	if err != nil || turn.SpecificationDigest == "" || g.AdmitEngine(r.Context(), turn.Specification) != nil {
		http.Error(w, "turn unavailable", 403)
		return
	}
	want := kubernetes.Token(g.signingKey, turn.Specification)
	if subtle.ConstantTimeCompare([]byte(token), []byte(want)) != 1 {
		http.Error(w, "turn unavailable", 403)
		return
	}
	if g.model.Endpoint == "" || g.model.Name == "" || g.model.MaxOutputTokens < 1 {
		http.Error(w, "model unavailable", 503)
		return
	}
	var input struct {
		Input        string          `json:"input"`
		Instructions string          `json:"instructions"`
		Model        string          `json:"model"`
		Stream       bool            `json:"stream"`
		Tools        json.RawMessage `json:"tools"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	if err != nil {
		http.Error(w, "model input too large", 413)
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || strings.TrimSpace(input.Input) == "" ||
		len(input.Instructions) > 8<<10 || input.Stream || len(input.Tools) != 0 {
		http.Error(w, "text-only model input required", 400)
		return
	}
	encoded, err := json.Marshal(map[string]any{"model": g.model.Name, "input": input.Input, "instructions": input.Instructions,
		"store": false, "stream": false, "max_output_tokens": g.model.MaxOutputTokens, "text": conversationResponseFormat})
	if err != nil {
		http.Error(w, "model input invalid", 400)
		return
	}
	reservation := int64(len(encoded)) + 1024 + g.model.MaxOutputTokens
	reserve, complete := g.journal.ReserveConversationCall, g.journal.CompleteConversationCall
	if turn.ModelCallStatus == "completed" {
		engine, err := g.journal.ConversationEngineJournal().GetEngineAttempt(r.Context(), id)
		// The actor may call while the answer delivery is still in flight;
		// either phase means Marina recorded this turn's answer.
		if err != nil || (engine.AnswerPhase != "sending" && engine.AnswerPhase != "sent") {
			http.Error(w, "model budget or turn unavailable", 429)
			return
		}
		reserve, complete = g.journal.ReserveConversationFollowUpCall, g.journal.CompleteConversationFollowUpCall
	}
	if err := reserve(r.Context(), id, reservation); err != nil {
		http.Error(w, "model budget or turn unavailable", 429)
		return
	}
	credential := ""
	if g.model.TokenFile != "" {
		secret, err := os.ReadFile(g.model.TokenFile)
		credential = strings.TrimSpace(string(secret))
		if err != nil || credential == "" {
			http.Error(w, "model credential unavailable", 503)
			return
		}
	}
	ctx, cancel := context.WithDeadline(r.Context(), turn.DeadlineAt)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(g.model.Endpoint, "/")+"/responses", bytes.NewReader(encoded))
	if err != nil {
		http.Error(w, "model unavailable", 503)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	if credential != "" {
		request.Header.Set("Authorization", "Bearer "+credential)
	}
	response, err := g.client.Do(request)
	if err != nil {
		http.Error(w, "model unavailable", 502)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		http.Error(w, "model rejected request", 502)
		return
	}
	output, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(output) > 1<<20 {
		http.Error(w, "model response too large", 502)
		return
	}
	var decoded struct {
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(output, &decoded) != nil || decoded.Usage.InputTokens < 1 || decoded.Usage.OutputTokens < 0 {
		http.Error(w, "model usage missing", 502)
		return
	}
	accountCtx, finish := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer finish()
	if err := complete(accountCtx, id, decoded.Usage.InputTokens+decoded.Usage.OutputTokens); err != nil {
		http.Error(w, "model usage could not be recorded", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(output)
}
