package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/flow"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
)

func initializeProjectBinding(ctx context.Context, journal *sqlite.Store, config flow.Config) (execution.ProjectBinding, error) {
	binding, err := flow.InitializeRepository(ctx, journal, config, execution.ProjectBinding{ProjectID: config.ProjectID, Provider: "deck", Repository: config.Git.CloneURL, Runtime: "kubernetes:" + config.Kubernetes.Namespace, PolicyVersion: config.PolicyVersion, ContextConfig: config.Digest(), ArtifactConfig: config.ArtifactDirectory, DeliveryConfig: config.Git.APIURL, BudgetMinutes: config.BudgetMinutes, MaxConcurrency: 1})
	if err != nil {
		return binding, err
	}
	if config.RepositoryEnabled() {
		binding.Version, err = journal.FirstRepositoryVersion(ctx, binding.ProjectID)
	}
	return binding, err
}

// The attempt/turn identity is only a route selector. The selected gateway
// still validates the HMAC, live authority, frozen profile, scope and budget.
func projectGateway(journal *sqlite.Store, workers, conversations map[string]http.Handler, artifacts http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/artifacts/") {
			artifacts.ServeHTTP(w, r)
			return
		}
		var project string
		var err error
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if strings.HasPrefix(r.URL.Path, "/conversation/") {
			id, _, found := strings.Cut(token, ".")
			if !ok || !found || len(id) > 128 {
				http.Error(w, "turn credential required", 401)
				return
			}
			turn, lookup := journal.GetConversationTurn(r.Context(), id)
			project, err = turn.ProjectID, lookup
		} else {
			var id string
			if strings.HasPrefix(r.URL.Path, "/engine/") {
				id, _, ok = strings.Cut(token, ".")
			} else if strings.HasPrefix(r.URL.Path, "/workers/") {
				parts := strings.Split(r.URL.Path, "/")
				if len(parts) > 2 {
					id = parts[2]
				}
			} else {
				http.NotFound(w, r)
				return
			}
			if !ok || id == "" || len(id) > 128 {
				http.Error(w, "attempt credential required", 401)
				return
			}
			attempt, lookup := journal.GetRuntimeAttempt(r.Context(), id)
			err = lookup
			if err == nil {
				record, lookup := journal.GetExecution(r.Context(), attempt.ExecutionID)
				project, err = record.ProjectID, lookup
			}
		}
		if err != nil || project == "" {
			http.Error(w, "attempt unavailable", 403)
			return
		}
		handlers := workers
		if strings.HasPrefix(r.URL.Path, "/conversation/") {
			handlers = conversations
		}
		handler := handlers[project]
		if handler == nil {
			http.Error(w, "project gateway unavailable", 403)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

var errProjectNotConfigured = errors.New("execution project is not configured")
