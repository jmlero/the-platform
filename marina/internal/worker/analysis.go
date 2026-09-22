package worker

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/analysis"
)

func runAnalysis(ctx context.Context, input Input, root string) (Result, error) {
	result := Result{Stage: "analyze"}
	a := input.Analysis
	if a == nil || a.Version != 1 || a.InputDigest != analysis.Digest(a.Sources) || input.SourceURL != "" || input.SourceRevision != "" || len(input.AllowedPaths) > 0 || len(input.Checks) > 0 || len(input.Harness) == 0 {
		return result, errors.New("analysis requires frozen inputs and no repository capabilities")
	}
	if b, err := hex.DecodeString(a.ProfileDigest); err != nil || len(b) != 32 {
		return result, errors.New("analysis profile digest required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return result, err
	}
	if input.Context != nil {
		if err := input.Context.ValidateAt(time.Now()); err != nil {
			return result, errors.New("analysis context is invalid or expired")
		}
		aCopy := *a
		aCopy.Context = input.Context
		a = &aCopy
	}
	payload, err := json.Marshal(a)
	if err != nil {
		return result, err
	}
	env := append([]string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "PYTHONDONTWRITEBYTECODE=1", "LANGSMITH_TRACING=false", "LANGCHAIN_TRACING_V2=false"}, input.Environment...)
	output, _, err := command(ctx, root, env, strings.NewReader(string(payload)), input.Harness)
	if err != nil {
		for _, field := range strings.Fields(output) {
			category := strings.TrimPrefix(field, "MARINA_ANALYSIS_FAILURE=")
			if category == field {
				continue
			}
			switch category {
			case "model_timeout", "model_unavailable", "model_rejected", "tool_denied", "invalid_report", "multiple_reports", "harness_error":
				return result, fmt.Errorf("analysis harness failed (%s); inspect the governed model diagnostics", category)
			}
		}
		return result, errors.New("analysis harness failed; inspect the governed model diagnostics")
	}
	var report analysis.Report
	decoder := json.NewDecoder(strings.NewReader(output))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&report); err != nil {
		return result, errors.New("analysis harness returned an invalid report")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return result, errors.New("analysis harness must return one report")
	}
	result.Analysis = &report
	result.Summary = report.Summary
	result.InputDigest = a.InputDigest
	result.ProfileDigest = a.ProfileDigest
	result.ObservedAt = time.Now().UTC()
	return result, nil
}
