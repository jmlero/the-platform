package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type HealthChecker interface {
	Health(context.Context) error
}

type Dependencies struct {
	WorkControl HealthChecker
	Journal     HealthChecker
}

type Handler struct {
	dependencies     Dependencies
	readinessTimeout time.Duration
	clock            func() time.Time
}

func New(dependencies Dependencies) http.Handler {
	handler := &Handler{
		dependencies:     dependencies,
		readinessTimeout: 2 * time.Second,
		clock:            time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.health)
	mux.HandleFunc("GET /readyz", handler.ready)
	return mux
}

func (h *Handler) health(response http.ResponseWriter, _ *http.Request) {
	h.write(response, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "marina",
		"time":    h.clock().UTC().Format(time.RFC3339),
	})
}

func (h *Handler) ready(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), h.readinessTimeout)
	defer cancel()

	statuses := map[string]string{
		"journal":      dependencyStatus(ctx, h.dependencies.Journal),
		"work_control": dependencyStatus(ctx, h.dependencies.WorkControl),
	}
	status := http.StatusOK
	readyStatus := "ready"
	for _, dependency := range statuses {
		if dependency != "available" {
			status = http.StatusServiceUnavailable
			readyStatus = "not_ready"
			break
		}
	}

	h.write(response, status, map[string]any{
		"status":       readyStatus,
		"dependencies": statuses,
	})
}

func dependencyStatus(ctx context.Context, dependency HealthChecker) string {
	if dependency == nil {
		return "not_configured"
	}
	if err := dependency.Health(ctx); err != nil {
		return "unavailable"
	}
	return "available"
}

func (h *Handler) write(response http.ResponseWriter, status int, payload any) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(payload)
}
