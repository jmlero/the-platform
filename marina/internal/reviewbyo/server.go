// Package reviewbyo runs a text-only reviewer inside the existing kagent BYO
// actor. It has no source, filesystem, shell, conversation, or publication tool.
package reviewbyo

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"net/http"
	"os"
	"strings"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/worker"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

const ModelURL = "https://marina.marina-system.svc/engine/model/v1/responses"

func ValidateInput(raw []byte) (worker.Input, error) {
	var input worker.Input
	if len(raw) == 0 || len(raw) > workresult.MaxReviewInputBytes {
		return input, errors.New("review requires complete bounded input")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Stage != "review" || input.SourceURL != "" ||
		input.Candidate == nil || input.Candidate.BaseRevision != input.SourceRevision || input.CandidateDigest != input.Candidate.Digest() ||
		input.Context == nil || input.Context.Validate() != nil || len(input.Before) != len(input.Candidate.Changes) || strings.TrimSpace(input.AcceptanceCriteria) == "" {
		return input, errors.New("review input does not match the frozen candidate and context")
	}
	profile, err := hex.DecodeString(input.ProfileDigest)
	if err != nil || len(profile) != 32 {
		return input, errors.New("review profile is missing")
	}
	candidate, _ := json.Marshal(workresult.Result{Stage: "implement", SourceRevision: input.SourceRevision, Changes: input.Candidate.Changes, ObservedAt: time.Now().UTC()})
	if _, err := workresult.Parse(candidate, workresult.Expected{Stage: "implement", SourceRevision: input.SourceRevision, AllowedPaths: input.AllowedPaths}); err != nil {
		return input, err
	}
	for i, before := range input.Before {
		change := input.Candidate.Changes[i]
		if before.Path != change.Path || (!before.Exists && (len(before.Content) != 0 || before.Text != nil && *before.Text != "")) ||
			(before.Text != nil && len(before.Content) != 0) || (change.Delete && !before.Exists) {
			return input, errors.New("review original files do not match the candidate paths")
		}
	}
	return input, nil
}

type modelCall func(context.Context, []byte, string) (string, error)

func run(ctx context.Context, raw []byte, token string, call modelCall) (workresult.Result, error) {
	input, err := ValidateInput(raw)
	if err != nil || token == "" {
		return workresult.Result{}, errors.New("review input or attempt credential is unavailable")
	}
	answer, err := call(ctx, raw, token)
	if err != nil || len(answer) > 64<<10 {
		return workresult.Result{}, errors.New("independent review model unavailable")
	}
	var verdict workresult.Review
	decoder := json.NewDecoder(strings.NewReader(answer))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&verdict) != nil || decoder.Decode(new(any)) != io.EOF {
		return workresult.Result{}, errors.New("independent review verdict is malformed")
	}
	if err := verdict.Validate(input.CandidateDigest); err != nil {
		return workresult.Result{}, err
	}
	return workresult.Result{Stage: "review", SourceRevision: input.SourceRevision, InputDigest: strings.TrimPrefix(artifacts.Digest(raw), "sha256:"),
		ProfileDigest: input.ProfileDigest, Review: &verdict, ObservedAt: time.Now().UTC(), Summary: "Independent candidate review"}, nil
}

func callModel(ctx context.Context, raw []byte, token string) (string, error) {
	body, _ := json.Marshal(map[string]string{"input": string(raw)})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, ModelURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("governed review call rejected")
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, (128<<10)+1))
	if err != nil || len(encoded) > 128<<10 {
		return "", errors.New("review response exceeds its bound")
	}
	var output struct {
		Status string `json:"status"`
		Output []struct {
			Content []struct{ Type, Text string } `json:"content"`
		} `json:"output"`
	}
	if json.Unmarshal(encoded, &output) != nil || output.Status != "completed" {
		return "", errors.New("review model did not complete")
	}
	var text []string
	for _, item := range output.Output {
		for _, part := range item.Content {
			if part.Type == "output_text" {
				text = append(text, part.Text)
			}
		}
	}
	return strings.Join(text, "\n"), nil
}

func Execute(ctx context.Context, request *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		fail := func() { yield(a2a.NewStatusUpdateEvent(request, a2a.TaskStateFailed, nil), nil) }
		if request.Message == nil || len(request.Message.Parts) != 1 || request.Message.Parts[0] == nil {
			fail()
			return
		}
		body, ok := request.Message.Parts[0].Content.(a2a.Text)
		if !ok {
			fail()
			return
		}
		if !yield(a2a.NewSubmittedTask(request, request.Message), nil) {
			return
		}
		result, err := run(ctx, []byte(body), os.Getenv("OPENAI_API_KEY"), callModel)
		if err != nil {
			fail()
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
