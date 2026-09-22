package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/jmlero/the-platform/marina/internal/flow"
)

// Local installation inspection only. This command grants no authority and
// never reads credentials, changes intake, or submits work to a service.
func runWorkloadProfile(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("workload-profile", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "installation workflow configuration")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *path == "" {
		fmt.Fprintln(stderr, "workload-profile requires --config and no positional arguments")
		return 2
	}
	c, err := flow.Load(*path)
	if err != nil || c.Analysis == nil {
		fmt.Fprintln(stderr, "a valid installed analysis profile is required")
		return 1
	}
	p := c.Analysis
	out := map[string]any{"workload": analysis.Kind, "profile_revision": p.Revision, "profile_digest": c.AnalysisDigest(), "authority": c.AnalysisAuthority(), "input_digest": p.Fixture.InputDigest(), "worker_image": p.Image, "harness": p.HarnessVersion, "model": c.Model.Name, "model_destination": c.Model.Endpoint, "max_tokens": p.MaxTokens, "budget_minutes": p.BudgetMinutes}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(out); err != nil {
		return 1
	}
	return 0
}
