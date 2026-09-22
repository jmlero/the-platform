package workerapi

import (
	"strings"

	"github.com/jmlero/the-platform/marina/internal/analysis"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/worker"
)

// Keep the same authority/budget/usage assertions for both real input formats.
func gatewayWorkloadSpec(workload string, spec runtimeapi.Specification) runtimeapi.Specification {
	if workload == "analysis" {
		spec.RepositoryURI, spec.SourceRevision = "", ""
		spec.Workload, spec.HarnessVersion = analysis.Kind, analysis.HarnessWithContext
		spec.WorkerImage = "analysis@sha256:" + strings.Repeat("a", 64)
		spec.InputDigest = analysis.DiagnosticFixture().InputDigest()
		spec.ProfileDigest = strings.Repeat("b", 64)
	}
	return spec
}

func gatewayWorkloadInput(workload string, spec runtimeapi.Specification) worker.Input {
	input := worker.Input{Stage: "implement", SourceRevision: spec.SourceRevision}
	if workload == "analysis" {
		input.Stage = "analyze"
		input.Analysis = &analysis.Input{Version: 1, ProfileDigest: spec.ProfileDigest, InputDigest: spec.InputDigest, Sources: analysis.DiagnosticFixture().Sources}
	}
	return input
}
