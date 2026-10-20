// Command hello-agent is the smallest "bring your own" kagent agent: an A2A
// server on port 80 that answers each message with one model call.
//
// kagent passes the AgentTemplate's model and system prompt in
// KAGENT_CONFIG_JSON. The key in OPENAI_API_KEY is a placeholder; the engine's
// egress proxy swaps in the real credential, so this container never sees it.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/app"
)

type config struct {
	Instruction string `json:"instruction"`
	Model       struct {
		Model   string `json:"model"`
		BaseURL string `json:"base_url"`
	} `json:"model"`
}

func ask(ctx context.Context, c config, question string) (string, error) {
	body, _ := json.Marshal(map[string]any{"model": c.Model.Model, "instructions": c.Instruction, "input": question})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(c.Model.BaseURL, "/")+"/responses", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+os.Getenv("OPENAI_API_KEY"))
	response, err := (&http.Client{Timeout: 2 * time.Minute}).Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("model returned %s", response.Status)
	}
	var output struct {
		Output []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&output); err != nil {
		return "", err
	}
	var text []string
	for _, item := range output.Output {
		for _, part := range item.Content {
			if part.Type == "output_text" {
				text = append(text, part.Text)
			}
		}
	}
	if len(text) == 0 {
		return "", errors.New("model returned no text")
	}
	return strings.Join(text, "\n"), nil
}

type executor struct{ config config }

func (e executor) Execute(ctx context.Context, request *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		var question []string
		if request.Message != nil {
			for _, part := range request.Message.Parts {
				if text, ok := part.Content.(a2a.Text); ok {
					question = append(question, string(text))
				}
			}
		}
		if !yield(a2a.NewSubmittedTask(request, request.Message), nil) {
			return
		}
		answer, err := ask(ctx, e.config, strings.Join(question, "\n"))
		if err != nil {
			log.Printf("hello-agent: %v", err)
			yield(a2a.NewStatusUpdateEvent(request, a2a.TaskStateFailed, nil), nil)
			return
		}
		reply := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(answer))
		yield(a2a.NewStatusUpdateEvent(request, a2a.TaskStateCompleted, reply), nil)
	}
}

func (executor) Cancel(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(func(a2a.Event, error) bool) {}
}

func main() {
	var c config
	if err := json.Unmarshal([]byte(os.Getenv("KAGENT_CONFIG_JSON")), &c); err != nil || c.Model.BaseURL == "" {
		log.Fatal("hello-agent: KAGENT_CONFIG_JSON with a model base_url is required")
	}
	server, err := app.New(app.AppConfig{Port: "80", AppName: "hello-agent",
		AgentCard: a2a.AgentCard{Name: "hello-agent", Version: "v1", Description: "Answers each message with one model call"}}, executor{c})
	if err != nil {
		log.Fatal(err)
	}
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
