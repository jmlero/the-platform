package deck

import (
	"context"
	"github.com/jmlero/the-platform/marina/internal/kagent"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"strings"
)

func workflowEngineProfile() *rt.EngineSelection {
	return &rt.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: "kint-engine-controller.kint-engine.svc:8083", Namespace: "kint-engine", Harness: "kint-codex"}
}

type workflowTemplates struct{}

func (workflowTemplates) EnsureEngineTemplate(context.Context, rt.EngineTemplate) (string, error) {
	return strings.Repeat("d", 64), nil
}
func (workflowTemplates) DeleteEngineTemplate(context.Context, string, string) error { return nil }
func (r *workflowRuntime) CollectArtifacts(context.Context, string) ([]rt.Artifact, error) {
	return nil, nil
}
