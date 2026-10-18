package main

import (
	"context"
	"net/http"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/flow"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kubeapi"
	"github.com/jmlero/the-platform/marina/internal/platformapi"
)

// configurePlatformAPI builds the marina.dev/v1 controller and HTTP API on the
// installation's Kubernetes service account (decision 0033).
func configurePlatformAPI(config flow.Config, projects []flow.Config, journal *sqlite.Store, agentID string) (*platformapi.Controller, http.Handler, error) {
	kube, err := kubeapi.New(kubeapi.Config{ServerURL: config.Kubernetes.ServerURL, TokenFile: config.Kubernetes.TokenFile, CAFile: config.Kubernetes.CAFile}, nil)
	if err != nil {
		return nil, nil, err
	}
	store, err := artifacts.New(config.ArtifactDirectory)
	if err != nil {
		return nil, nil, err
	}
	installed := map[string]flow.Config{}
	for _, project := range projects {
		installed[project.ProjectID] = project
	}
	controller := &platformapi.Controller{Kube: kube, Journal: journal, Platform: journal.Platform(), Projects: installed, AgentID: agentID}
	server := &platformapi.Server{Kube: kube, Platform: journal.Platform(), Artifacts: store,
		Capabilities: func(context.Context) (platformapi.Capabilities, error) { return capabilities(projects), nil }}
	return controller, server.Handler(platformapi.NewAuthorizer(kube)), nil
}

// capabilities describes what the installed profiles offer, without naming
// the engine, sandbox, endpoints or credentials behind them.
func capabilities(projects []flow.Config) platformapi.Capabilities {
	out := platformapi.Capabilities{WorkloadTypes: []string{}, Models: []platformapi.ModelCapability{}, Tools: []platformapi.ToolCapability{},
		Limits: platformapi.Limits{MaxAttempts: flow.RepositoryRuntimeLimit}}
	types, models, tools := map[string]bool{}, map[string]map[string]bool{}, map[string]bool{}
	role := func(name, role string) {
		if name == "" {
			return
		}
		if models[name] == nil {
			models[name] = map[string]bool{}
		}
		models[name][role] = true
	}
	for _, project := range projects {
		if project.RepositoryEnabled() {
			types["repository-change"] = true
			role(project.Model.Name, "implement")
			if project.ReviewerModel != nil {
				role(project.ReviewerModel.Name, "review")
			}
		}
		if project.Conversation != nil {
			types["conversation"] = true
			role(project.Model.Name, "conversation")
			if project.Conversation.MCP != nil {
				for _, tool := range project.Conversation.MCP.Tools {
					tools[tool] = true
				}
			}
		}
		out.Limits.MaxTokens = max(out.Limits.MaxTokens, project.MaxTokens)
		out.Limits.MaxTimeMinutes = max(out.Limits.MaxTimeMinutes, project.BudgetMinutes)
	}
	for _, kind := range []string{"repository-change", "conversation"} {
		if types[kind] {
			out.WorkloadTypes = append(out.WorkloadTypes, kind)
		}
	}
	for name, roles := range models {
		model := platformapi.ModelCapability{Name: name}
		for _, r := range []string{"implement", "review", "conversation"} {
			if roles[r] {
				model.Roles = append(model.Roles, r)
			}
		}
		out.Models = append(out.Models, model)
	}
	for name := range tools {
		out.Tools = append(out.Tools, platformapi.ToolCapability{Name: name})
	}
	return out
}
