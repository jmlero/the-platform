package analysisbyo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"iter"
	"os"
	"os/exec"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/kagent-dev/kagent/go/adk/pkg/app"
	hitl "github.com/kagent-dev/kagent/go/api/a2a"
)

// Inject a non-selected URI into the real Python context tool, independently of
// whether a model elects to request it. A refusal must not produce a report.
func TestLiveDeniedActor(t *testing.T) {
	if os.Getenv("KINT07_DENIED_FIXTURE") != "1" {
		t.Skip("isolated local Substrate acceptance only")
	}
	server, err := app.New(app.AppConfig{Port: "80", AppName: "kint07-denied",
		AgentCard: a2a.AgentCard{Name: "kint07-denied", Version: "v1", Capabilities: a2a.AgentCapabilities{Streaming: true}}},
		executor{diagnose: func(ctx context.Context, input analysis.Input, token string) (analysis.Report, error) {
			payload, _ := json.Marshal(input)
			command := exec.CommandContext(ctx, "python3", "-c", `import json, os, sys, urllib.error
sys.path.insert(0, "/opt/marina-harness")
from run import context_tools
try:
    context_tools(json.load(sys.stdin), os.environ["MARINA_WORKER_TOKEN"])[0]("deck://prj_default/source/not-selected/1")
except urllib.error.HTTPError as error:
    if error.code == 403 and error.read().strip() == b"context tool denied: source_not_selected":
        print("KINT07_SOURCE_NOT_SELECTED_403")
        sys.exit(0)
sys.exit(1)
`)
			command.Env = append(os.Environ(), "MARINA_WORKER_TOKEN="+token)
			command.Stdin = bytes.NewReader(payload)
			if output, err := command.Output(); err == nil && string(output) == "KINT07_SOURCE_NOT_SELECTED_403\n" {
				// The durable gateway audit is the authority for this observation.
				return analysis.Report{}, errors.New("context tool denied the non-selected source")
			}
			// An unexpected allow must fail independent validation, never count as
			// a successful denial in the acceptance runner.
			return analysis.Report{Summary: "denial probe did not observe the required refusal"}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Run(); err != nil {
		t.Fatal(err)
	}
}

// This opt-in actor exercises the real engine's question continuation. It is
// compiled only into a test binary, never the installed diagnostic executable.
// The question is synthetic; after its answer the ordinary LangChain harness,
// governed gateway and independent Marina validator process the frozen input.
func TestLiveQuestionActor(t *testing.T) {
	if os.Getenv("KINT08_QUESTION_FIXTURE") != "1" {
		t.Skip("isolated local Substrate acceptance only")
	}
	server, err := app.New(app.AppConfig{Port: "80", AppName: "kint08-question",
		AgentCard: a2a.AgentCard{Name: "kint08-question", Version: "v1",
			Capabilities: a2a.AgentCapabilities{Streaming: true, Extensions: []a2a.AgentExtension{hitl.HITLExtension()}}}}, questionExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Run(); err != nil {
		t.Fatal(err)
	}
}

type questionExecutor struct{}

func (questionExecutor) Execute(ctx context.Context, request *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		question := hitl.AskUserRequest{Type: hitl.HITLTypeAskUserRequest, ID: "kint08-diagnostic-scope",
			Questions: []hitl.HITLQuestion{{Question: "Which records should this diagnostic analyze?", Choices: []string{"Only the frozen synthetic fixture"}}}}
		answer, err := hitl.ParseAskUserResponse(request.Message)
		if err != nil {
			if !yield(a2a.NewSubmittedTask(request, request.Message), nil) {
				return
			}
			message := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(question.Questions[0].Question))
			if err := hitl.AttachHITL(message, question); err != nil {
				yield(nil, err)
				return
			}
			yield(a2a.NewStatusUpdateEvent(request, a2a.TaskStateInputRequired, message), nil)
			return
		}
		if hitl.ValidateAskUserResponse(&question, answer) != nil || len(answer.Answers[0].Answer) != 1 ||
			answer.Answers[0].Answer[0] != question.Questions[0].Choices[0] || request.StoredTask == nil {
			yield(a2a.NewStatusUpdateEvent(request, a2a.TaskStateFailed, nil), nil)
			return
		}
		var raw []byte
		for _, message := range request.StoredTask.History {
			if message.Role == a2a.MessageRoleUser && len(message.Parts) == 1 {
				if body, ok := message.Parts[0].Content.(a2a.Text); ok && json.Valid([]byte(body)) {
					raw = []byte(body)
					break
				}
			}
		}
		if !yield(a2a.NewStatusUpdateEvent(request, a2a.TaskStateWorking, nil), nil) {
			return
		}
		result, err := run(ctx, raw, os.Getenv("OPENAI_API_KEY"), langchain)
		if err != nil {
			yield(a2a.NewStatusUpdateEvent(request, a2a.TaskStateFailed, nil), nil)
			return
		}
		encoded, _ := json.Marshal(result)
		artifact := a2a.NewArtifactEvent(request, a2a.NewRawPart(encoded))
		artifact.Artifact.Name = "marina-result.json"
		artifact.Artifact.Parts[0].MediaType = "application/json"
		if yield(artifact, nil) {
			yield(a2a.NewStatusUpdateEvent(request, a2a.TaskStateCompleted, nil), nil)
		}
	}
}

func (questionExecutor) Cancel(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(func(a2a.Event, error) bool) {}
}
