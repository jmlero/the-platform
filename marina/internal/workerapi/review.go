package workerapi

import (
	"context"
	"errors"
	"strings"

	"github.com/jmlero/the-platform/marina/internal/reviewbyo"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

func (server *Server) reviewRequest(attempt rt.Attempt, request map[string]any) (Model, map[string]any, error) {
	m := server.config.ReviewerModel
	if m == nil || m.Name == "" || strings.EqualFold(strings.TrimSpace(m.Name), strings.TrimSpace(server.config.Model.Name)) ||
		attempt.Specification.Engine == nil || attempt.Specification.Engine.Harness != workresult.ReviewerHarness || server.artifacts == nil {
		return Model{}, nil, errors.New("independent reviewer is unavailable")
	}
	data, err := server.artifacts.Get(strings.TrimPrefix(attempt.Specification.ContextURI, "artifact://"))
	if err != nil {
		return Model{}, nil, err
	}
	input, err := reviewbyo.ValidateInput(data)
	if err != nil || input.ProfileDigest != attempt.Specification.ProfileDigest || input.Context.Digest != attempt.Specification.ContextDigest ||
		input.SourceRevision != attempt.Specification.SourceRevision || len(request) != 1 || request["input"] != string(data) {
		return Model{}, nil, errors.New("review request differs from the immutable input or includes tools/session fields")
	}
	phase := "initial"
	if input.Repair != nil {
		phase = "repair"
	}
	return *m, map[string]any{"input": string(data), "instructions": "Candidate phase: " + phase + "\n" + workresult.ReviewInstructions, "stream": false}, nil
}

func (server *Server) reviewResponse(ctx context.Context, attempt rt.Attempt) ([]byte, error) {
	items, err := server.journal.ListRuntimeArtifacts(ctx, attempt.ExecutionID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.RuntimeAttemptID == attempt.ID && item.Kind == "review_model_response" {
			data, err := server.artifacts.Get(item.Digest)
			if err != nil || len(data) > 128<<10 || int64(len(data)) != item.SizeBytes {
				return nil, errors.New("saved review response is unavailable")
			}
			return data, nil
		}
	}
	return nil, nil
}
