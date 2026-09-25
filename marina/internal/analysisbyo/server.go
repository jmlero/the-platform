// Package analysisbyo hosts the pinned LangChain diagnostic workload behind
// kagent's private A2A contract. Marina remains the independent result verifier.
package analysisbyo

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"os"
	"os/exec"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/jmlero/the-platform/marina/internal/workresult"
	"github.com/kagent-dev/kagent/go/adk/pkg/app"
)

const (
	gatewayModelURL = "https://marina.marina-system.svc/engine/model/v1"
	gatewayToolURL  = "https://marina.marina-system.svc/engine/tools/read-context-source"
)

type diagnoseFunc func(context.Context, analysis.Input, string) (analysis.Report, error)

func run(ctx context.Context, raw []byte, token string, diagnose diagnoseFunc) (workresult.Result, error) {
	if len(raw) == 0 || len(raw) > 128<<10 || token == "" {
		return workresult.Result{}, errors.New("analysis input or gateway credential is missing or oversized")
	}
	var input analysis.Input
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return workresult.Result{}, errors.New("invalid analysis input")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return workresult.Result{}, errors.New("one analysis input is required")
	}
	profile, err := hex.DecodeString(input.ProfileDigest)
	if err != nil || len(profile) != 32 || input.Version != 1 || input.InputDigest != analysis.Digest(input.Sources) ||
		input.Model == "" || input.ModelURL != gatewayModelURL || input.ToolURL != gatewayToolURL ||
		len(input.Tools) > 1 || len(input.Tools) == 1 && input.Tools[0] != "read_context_source" {
		return workresult.Result{}, errors.New("analysis input differs from its governed profile")
	}
	if input.Context != nil {
		if err := input.Context.ValidateAt(time.Now()); err != nil {
			return workresult.Result{}, errors.New("analysis context is invalid or expired")
		}
	}
	report, err := diagnose(ctx, input, token)
	if err != nil {
		return workresult.Result{}, err
	}
	result := workresult.Result{Stage: "analyze", Analysis: &report, InputDigest: input.InputDigest,
		ProfileDigest: input.ProfileDigest, Summary: report.Summary, ObservedAt: time.Now().UTC()}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > workresult.MaxResultBytes {
		return workresult.Result{}, errors.New("analysis result exceeds the bounded contract")
	}
	return result, nil
}

func langchain(ctx context.Context, input analysis.Input, token string) (analysis.Report, error) {
	var report analysis.Report
	payload, err := json.Marshal(input)
	if err != nil {
		return report, err
	}
	command := exec.CommandContext(ctx, "python3", "/opt/marina-harness/run.py")
	command.Env = append(os.Environ(), "MARINA_WORKER_TOKEN="+token)
	command.Stdin = bytes.NewReader(payload)
	var output bytes.Buffer
	command.Stdout = &limitedWriter{buffer: &output, remaining: 64 << 10}
	command.Stderr = io.Discard
	if command.Run() != nil {
		return report, errors.New("LangChain diagnostic harness failed; inspect governed model diagnostics")
	}
	decoder := json.NewDecoder(&output)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&report) != nil {
		return report, errors.New("LangChain returned an invalid report")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return report, errors.New("LangChain must return one report")
	}
	return report, nil
}

type limitedWriter struct {
	buffer    *bytes.Buffer
	remaining int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, errors.New("LangChain output exceeds limit")
	}
	w.remaining -= len(p)
	return w.buffer.Write(p)
}

type executor struct{ diagnose diagnoseFunc }

func (e executor) Execute(ctx context.Context, request *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if !yield(a2a.NewSubmittedTask(request, request.Message), nil) {
			return
		}
		if request.Message == nil || len(request.Message.Parts) != 1 || request.Message.Parts[0] == nil {
			yield(a2a.NewStatusUpdateEvent(request, a2a.TaskStateFailed, nil), nil)
			return
		}
		body, ok := request.Message.Parts[0].Content.(a2a.Text)
		if !ok {
			yield(a2a.NewStatusUpdateEvent(request, a2a.TaskStateFailed, nil), nil)
			return
		}
		result, err := run(ctx, []byte(body), os.Getenv("OPENAI_API_KEY"), e.diagnose)
		if err != nil {
			yield(a2a.NewStatusUpdateEvent(request, a2a.TaskStateFailed, nil), nil)
			return
		}
		encoded, _ := json.Marshal(result)
		artifact := a2a.NewArtifactEvent(request, a2a.NewRawPart(encoded))
		artifact.Artifact.Name = "marina-result.json"
		artifact.Artifact.Parts[0].MediaType = "application/json"
		if !yield(artifact, nil) {
			return
		}
		yield(a2a.NewStatusUpdateEvent(request, a2a.TaskStateCompleted, nil), nil)
	}
}

func (executor) Cancel(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(func(a2a.Event, error) bool) {}
}

// Serve runs one pinned BYO A2A actor. Substrate owns its lifecycle and network.
func Serve() error {
	server, err := app.New(app.AppConfig{Port: "80", AppName: "marina-analysis-byo",
		AgentCard: a2a.AgentCard{Name: "marina-analysis-byo", Version: "v1", Capabilities: a2a.AgentCapabilities{Streaming: true}}},
		executor{diagnose: langchain})
	if err != nil {
		return err
	}
	return server.Run()
}
