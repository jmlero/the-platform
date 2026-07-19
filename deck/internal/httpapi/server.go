package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/store"
)

type Server struct {
	store  *store.Store
	logger *slog.Logger
}

type bootstrapResponse struct {
	Tasks     []domain.Task     `json:"tasks"`
	Agents    []domain.Agent    `json:"agents"`
	Decisions []domain.Decision `json:"decisions"`
	Events    []domain.Event    `json:"events"`
	Now       int64             `json:"now"`
}

func New(store *store.Store, logger *slog.Logger) *Server {
	return &Server{store: store, logger: logger}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/v1/provider", s.providerInfo)
	mux.HandleFunc("GET /api/v1/bootstrap", s.bootstrap)
	mux.HandleFunc("GET /api/v1/tasks", s.listTasks)
	mux.HandleFunc("POST /api/v1/tasks", s.createTask)
	mux.HandleFunc("GET /api/v1/tasks/{id}", s.getTask)
	mux.HandleFunc("PATCH /api/v1/tasks/{id}", s.updateTask)
	mux.HandleFunc("POST /api/v1/tasks/{id}/move", s.moveTask)
	mux.HandleFunc("POST /api/v1/tasks/{id}/claim", s.claimTask)
	mux.HandleFunc("GET /api/v1/agents", s.listAgents)
	mux.HandleFunc("POST /api/v1/agents", s.registerAgent)
	mux.HandleFunc("POST /api/v1/runs/{id}/heartbeat", s.heartbeatRun)
	mux.HandleFunc("POST /api/v1/runs/{id}/plan", s.submitPlan)
	mux.HandleFunc("POST /api/v1/runs/{id}/start", s.startRun)
	mux.HandleFunc("POST /api/v1/runs/{id}/progress", s.reportProgress)
	mux.HandleFunc("POST /api/v1/runs/{id}/verify", s.beginVerification)
	mux.HandleFunc("POST /api/v1/runs/{id}/complete", s.completeRun)
	mux.HandleFunc("POST /api/v1/runs/{id}/block", s.blockRun)
	mux.HandleFunc("GET /api/v1/decisions", s.listDecisions)
	mux.HandleFunc("GET /api/v1/decisions/{id}", s.getDecision)
	mux.HandleFunc("POST /api/v1/decisions/{id}/answer", s.answerDecision)
	mux.HandleFunc("GET /api/v1/events", s.listEvents)
	mux.HandleFunc("GET /api/v1/events/stream", s.streamEvents)
	return s.middleware(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "service": "appdeck", "time": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) providerInfo(w http.ResponseWriter, r *http.Request) {
	readiness := map[string]string{"store": "available"}
	ready := true
	if err := s.store.Health(r.Context()); err != nil {
		ready = false
		readiness["store"] = "unavailable"
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, providerInfoResponse{
		Provider:        providerName,
		ProtocolVersion: protocolVersion,
		Capabilities:    append([]string(nil), protocolCapabilities...),
		ServerTime:      time.Now().UTC().UnixMilli(),
		Ready:           ready,
		Readiness:       readiness,
	})
}

func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.store.ListTasks(r.Context())
	if err != nil {
		s.writeError(w, err)
		return
	}
	agents, err := s.store.ListAgents(r.Context())
	if err != nil {
		s.writeError(w, err)
		return
	}
	decisions, err := s.store.ListDecisions(r.Context(), "pending")
	if err != nil {
		s.writeError(w, err)
		return
	}
	events, err := s.store.ListEvents(r.Context(), 80, 0)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bootstrapResponse{
		Tasks: tasks, Agents: agents, Decisions: decisions, Events: events, Now: time.Now().UTC().UnixMilli(),
	})
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.store.ListTasks(r.Context())
	if err != nil {
		s.writeError(w, err)
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		writeJSON(w, http.StatusOK, tasks)
		return
	}
	validStatus := false
	for _, candidate := range domain.TaskStatuses {
		if status == candidate {
			validStatus = true
			break
		}
	}
	if !validStatus {
		s.writeError(w, fmt.Errorf("unknown task status %q: %w", status, domain.ErrInvalidInput))
		return
	}
	filtered := make([]domain.Task, 0, len(tasks))
	for _, task := range tasks {
		if task.Status == status {
			filtered = append(filtered, task)
		}
	}
	writeJSON(w, http.StatusOK, filtered)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var input domain.CreateTaskInput
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	task, err := s.store.CreateTask(r.Context(), input)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	detail, err := s.store.GetTaskDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	var input domain.UpdateTaskInput
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	task, err := s.store.UpdateTask(r.Context(), r.PathValue("id"), input)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) moveTask(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Status string `json:"status"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	task, err := s.store.MoveTask(r.Context(), r.PathValue("id"), input.Status)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) claimTask(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AgentID      string `json:"agent_id"`
		LeaseSeconds int    `json:"lease_seconds"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	run, err := s.store.ClaimTask(
		r.Context(), r.PathValue("id"), input.AgentID, time.Duration(input.LeaseSeconds)*time.Second,
	)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.store.ListAgents(r.Context())
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agents)
}

func (s *Server) registerAgent(w http.ResponseWriter, r *http.Request) {
	var agent domain.Agent
	if err := readJSON(r, &agent); err != nil {
		s.writeError(w, err)
		return
	}
	registered, err := s.store.RegisterAgent(r.Context(), agent)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, registered)
}

func (s *Server) heartbeatRun(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AgentID      string `json:"agent_id"`
		LeaseSeconds int    `json:"lease_seconds"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	if err := s.store.HeartbeatRun(
		r.Context(), r.PathValue("id"), input.AgentID, time.Duration(input.LeaseSeconds)*time.Second,
	); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) submitPlan(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Plan            string `json:"plan"`
		RequireApproval bool   `json:"require_approval"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	decision, err := s.store.SubmitPlan(r.Context(), r.PathValue("id"), input.Plan, input.RequireApproval)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"decision": decision})
}

func (s *Server) reportProgress(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Message string         `json:"message"`
		Payload map[string]any `json:"payload"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	if err := s.store.ReportProgress(r.Context(), r.PathValue("id"), input.Message, input.Payload); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) startRun(w http.ResponseWriter, r *http.Request) {
	if err := s.store.StartRun(r.Context(), r.PathValue("id")); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) beginVerification(w http.ResponseWriter, r *http.Request) {
	if err := s.store.BeginVerification(r.Context(), r.PathValue("id")); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) completeRun(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Summary  string `json:"summary"`
		Evidence string `json:"evidence"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	if err := s.store.CompleteRun(r.Context(), r.PathValue("id"), input.Summary, input.Evidence); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) blockRun(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	if strings.TrimSpace(input.Reason) == "" {
		s.writeError(w, fmt.Errorf("reason is required: %w", domain.ErrInvalidInput))
		return
	}
	if err := s.store.BlockRun(r.Context(), r.PathValue("id"), input.Reason); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listDecisions(w http.ResponseWriter, r *http.Request) {
	decisions, err := s.store.ListDecisions(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, decisions)
}

func (s *Server) getDecision(w http.ResponseWriter, r *http.Request) {
	decision, err := s.store.GetDecision(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, decision)
}

func (s *Server) answerDecision(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Answer string `json:"answer"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	decision, err := s.store.AnswerDecision(r.Context(), r.PathValue("id"), input.Answer)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, decision)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	afterID, _ := strconv.ParseInt(r.URL.Query().Get("after_id"), 10, 64)
	events, err := s.store.ListEvents(r.Context(), limit, afterID)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	events, cancel := s.store.Subscribe(32)
	defer cancel()
	lastEventID, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	if lastEventID > 0 {
		missed, err := s.store.ListEvents(r.Context(), 500, lastEventID)
		if err == nil {
			for _, event := range missed {
				if err := writeSSE(w, event); err != nil {
					return
				}
			}
		}
	}
	fmt.Fprint(w, ": appdeck event stream\n\n")
	flusher.Flush()

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeSSE(w, event); err != nil {
				return
			}
			flusher.Flush()
		case <-keepAlive.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, event domain.Event) error {
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if event.ID > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", event.ID); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", encoded)
	return err
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := acceptedRequestID(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if origin := r.Header.Get("Origin"); origin != "" && isDevelopmentOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Last-Event-ID, X-Request-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("request panic", "panic", recovered, "path", r.URL.Path, "request_id", requestID)
				s.writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal server error", true)
			}
			s.logger.Debug(
				"request",
				"method", r.Method,
				"path", r.URL.Path,
				"request_id", requestID,
				"duration", time.Since(started),
			)
		}()
		next.ServeHTTP(w, r)
	})
}

func isDevelopmentOrigin(origin string) bool {
	return strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:")
}

func acceptedRequestID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			strings.ContainsRune("-_.:", character) {
			continue
		}
		return ""
	}
	return value
}

func newRequestID() string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err == nil {
		return "req_" + hex.EncodeToString(value)
	}
	return fmt.Sprintf("req_%d", time.Now().UTC().UnixNano())
}

func readJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nilResponseWriter{}, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid request body: %w: %v", domain.ErrInvalidInput, err)
	}
	return nil
}

type nilResponseWriter struct{}

func (nilResponseWriter) Header() http.Header       { return make(http.Header) }
func (nilResponseWriter) Write([]byte) (int, error) { return 0, nil }
func (nilResponseWriter) WriteHeader(int)           {}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "internal server error"
	code := errorCodeInternal
	retryable := true
	switch {
	case errors.Is(err, domain.ErrNotFound):
		status, code, message, retryable = http.StatusNotFound, errorCodeNotFound, "not found", false
	case errors.Is(err, domain.ErrConflict):
		status, code, message, retryable = http.StatusConflict, errorCodeConflict, err.Error(), false
	case errors.Is(err, domain.ErrInvalidTransition):
		status, code, message, retryable = http.StatusUnprocessableEntity, errorCodeInvalidTransition, err.Error(), false
	case errors.Is(err, domain.ErrInvalidInput):
		status, code, message, retryable = http.StatusUnprocessableEntity, errorCodeInvalidInput, err.Error(), false
	case errors.Is(err, context.Canceled):
		return
	default:
		s.logger.Error("api error", "error", err, "request_id", w.Header().Get("X-Request-ID"))
	}
	s.writeAPIError(w, status, code, message, retryable)
}

func (s *Server) writeAPIError(
	w http.ResponseWriter,
	status int,
	code string,
	message string,
	retryable bool,
) {
	writeJSON(w, status, apiErrorResponse{
		Error:     message,
		Code:      code,
		Message:   message,
		Retryable: retryable,
		RequestID: w.Header().Get("X-Request-ID"),
	})
}
