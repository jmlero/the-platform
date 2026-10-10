package workerapi

import (
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/worker"
	"strings"
)

func gatewayWorkloadSpec(workload string, spec rt.Specification) rt.Specification {
	if workload == "repository" {
		spec.Engine = &rt.EngineSelection{Backend: "kagent", Version: "pinned", Endpoint: "engine", Namespace: "trial", Harness: "codex", Template: "attempt", Revision: strings.Repeat("d", 64)}
		spec.ContextURI = "artifact://sha256:" + strings.Repeat("b", 64)
		spec.ContextDigest = "sha256:" + strings.Repeat("b", 64)
		spec.ProfileDigest = strings.Repeat("c", 64)
		spec.CredentialReferences = []rt.CredentialReference{{Kind: "engine-gateway", Reference: "secret://trial/attempt/gateway", ExpiresAt: spec.DeadlineAt}}
	}
	return spec
}
func gatewayWorkloadInput(workload string, spec rt.Specification) worker.Input {
	stage := "verify"
	if spec.Engine != nil {
		stage = "implement"
	}
	return worker.Input{Stage: stage, SourceRevision: spec.SourceRevision}
}
