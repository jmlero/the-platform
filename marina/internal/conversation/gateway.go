package conversation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/conversationbyo"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
)

// Gateway grants one bounded model exchange to an immutable conversation turn,
// and one follow-up after its clarification answer. Each exchange permits at most
// four model requests and fixed read-only tools; both share the turn's budget.
// It has no direct source, shell, task, or result-publication route.
type Gateway struct {
	ProjectID     string
	Blobs         *artifacts.Store
	Source        workcontrol.ConversationSource
	Repository    func(context.Context, workcontrol.RepositoryBinding) (sourcecontrol.RepositoryReader, error)
	MCP           *MCPTools
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
        "answer": {
          "type": "string",
          "description": "Plain answer for the operator, without claiming an action already happened"
        },
        "delegation": {
          "anyOf": [
            {
              "type": "null"
            },
            {
              "type": "object",
              "properties": {
                "title": {
                  "type": "string"
                },
                "description": {
                  "type": "string"
                },
                "acceptance_criteria": {
                  "type": "string"
                },
                "mode": {
                  "type": "string",
                  "enum": [
                    "ready",
                    "draft"
                  ]
                },
                "followup_task_id": {
                  "type": "string"
                }
              },
              "required": [
                "title",
                "description",
                "acceptance_criteria",
                "mode",
                "followup_task_id"
              ],
              "additionalProperties": false
            }
          ]
        },
        "question": {
          "anyOf": [
            {
              "type": "null"
            },
            {
              "type": "object",
              "properties": {
                "text": {
                  "type": "string"
                },
                "choices": {
                  "type": "array",
                  "items": {
                    "type": "string"
                  }
                }
              },
              "required": [
                "text",
                "choices"
              ],
              "additionalProperties": false
            }
          ]
        },
        "prd": {
          "anyOf": [
            {
              "type": "null"
            },
            {
              "type": "object",
              "properties": {
                "title": {
                  "type": "string"
                },
                "path": {
                  "type": "string"
                },
                "content": {
                  "type": "string"
                }
              },
              "required": [
                "title",
                "path",
                "content"
              ],
              "additionalProperties": false
            }
          ]
        },
        "prd_tasks": {
          "anyOf": [
            {
              "type": "null"
            },
            {
              "type": "object",
              "properties": {
                "publication_task_id": {
                  "type": "string"
                },
                "revision": {
                  "type": "string"
                },
                "tasks": {
                  "type": "array",
                  "items": {
                    "type": "object",
                    "properties": {
                      "title": {
                        "type": "string"
                      },
                      "description": {
                        "type": "string"
                      },
                      "acceptance_criteria": {
                        "type": "string"
                      }
                    },
                    "required": [
                      "title",
                      "description",
                      "acceptance_criteria"
                    ],
                    "additionalProperties": false
                  }
                }
              },
              "required": [
                "publication_task_id",
                "revision",
                "tasks"
              ],
              "additionalProperties": false
            }
          ]
        }
      },
      "required": [
        "answer",
        "delegation",
        "question",
        "prd",
        "prd_tasks"
      ],
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
	return spec.AttemptID + "." + rt.CredentialToken(g.signingKey, spec), nil
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
	// Platform Turn IDs (<namespace>.<name>) contain dots; the token never does.
	separator := strings.LastIndex(bearer, ".")
	found := separator > 0
	id, token := bearer[:max(separator, 0)], bearer[separator+1:]
	if !ok || !found || len(id) > 128 || len(token) != 64 || strings.ContainsAny(id, "/\\ \t\r\n") {
		http.Error(w, "turn credential required", 401)
		return
	}
	turn, err := g.journal.GetConversationTurn(r.Context(), id)
	if err != nil || (g.ProjectID != "" && turn.ProjectID != g.ProjectID) || turn.SpecificationDigest == "" || g.AdmitEngine(r.Context(), turn.Specification) != nil {
		http.Error(w, "turn unavailable", 403)
		return
	}
	want := rt.CredentialToken(g.signingKey, turn.Specification)
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
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
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
	var frozen conversationbyo.Input
	var transcript []conversationbyo.Message
	clarified := false
	if g.Blobs != nil {
		frozen, err = frozenInput(g.Blobs.Get, turn.Specification.ContextDigest, turn.Specification.InputDigest)
		if err != nil || checkSelection(r.Context(), g.Source, frozen) != nil {
			http.Error(w, "conversation context unavailable", 403)
			return
		}
		if json.Unmarshal([]byte(input.Input), &transcript) != nil || len(transcript) < len(frozen.Messages) || !reflect.DeepEqual(transcript[:len(frozen.Messages)], frozen.Messages) {
			http.Error(w, "frozen conversation transcript required", 400)
			return
		}
		if len(transcript) != len(frozen.Messages) {
			if len(transcript) != len(frozen.Messages)+2 {
				http.Error(w, "frozen conversation transcript required", 400)
				return
			}
			question, answer := transcript[len(frozen.Messages)], transcript[len(frozen.Messages)+1]
			engine, err := g.journal.ConversationEngineJournal().GetEngineAttempt(r.Context(), id)
			if err != nil || engine.AnswerPhase != "sending" && engine.AnswerPhase != "sent" || question.ID != "clarification-question" || question.Role != "agent" || len(question.Body) > 1000 ||
				answer.ID != "clarification-answer" || answer.Role != "human" || len(answer.Body) > 16<<10 || fmt.Sprintf("%x", sha256.Sum256([]byte(answer.Body))) != engine.AnswerDigest {
				http.Error(w, "recorded conversation answer required", 403)
				return
			}
			clarified = true
		}
		input.Instructions = conversationbyo.ModelInstructions(clarified)
	}
	payload := map[string]any{"model": g.model.Name, "input": input.Input, "instructions": input.Instructions,
		"store": false, "stream": false, "max_output_tokens": g.model.MaxOutputTokens, "text": conversationResponseFormat}
	if frozen.Repository != nil {
		payload["tools"] = repositoryTools
		payload["instructions"] = input.Instructions + repositoryInstructions
	}
	if frozen.SelectedContext != nil {
		prompt, err := json.Marshal(map[string]any{"thread": transcript, "repository": frozen.Repository, "selected_context": frozen.SelectedContext, "selected_git_files": frozen.ContextFiles})
		if err != nil {
			http.Error(w, "conversation context unavailable", 503)
			return
		}
		payload["input"] = string(prompt)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "model input invalid", 400)
		return
	}
	reservation := int64(len(encoded)) + 1024 + g.model.MaxOutputTokens
	if frozen.Repository != nil {
		reservation = turn.MaxTokens - turn.UsedTokens
		if int64(len(encoded))+1024+g.model.MaxOutputTokens > reservation {
			http.Error(w, "model budget or turn unavailable", 429)
			return
		}
	}
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
	output, used, err := g.modelResponse(ctx, turn, frozen, payload, credential, reservation)
	if err != nil {
		http.Error(w, "model or repository response unavailable within turn limits", 502)
		return
	}
	accountCtx, finish := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer finish()
	if err := complete(accountCtx, id, used); err != nil {
		http.Error(w, "model usage could not be recorded", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(output)
}
