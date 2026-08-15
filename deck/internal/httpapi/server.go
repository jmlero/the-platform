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
	store             *store.Store
	logger            *slog.Logger
	serviceAuthorizer *FileServiceAuthorizer
}

type Option func(*Server)

func WithServiceAuthorizer(authorizer *FileServiceAuthorizer) Option {
	return func(server *Server) {
		server.serviceAuthorizer = authorizer
	}
}

type serviceIdentityContextKey struct{}

type bootstrapResponse struct {
	Tasks     []domain.Task     `json:"tasks"`
	Agents    []domain.Agent    `json:"agents"`
	Decisions []domain.Decision `json:"decisions"`
	Events    []domain.Event    `json:"events"`
	Now       int64             `json:"now"`
}

func New(store *store.Store, logger *slog.Logger, options ...Option) *Server {
	server := &Server{store: store, logger: logger}
	for _, option := range options {
		option(server)
	}
	return server
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.readiness)
	mux.HandleFunc("GET /api/v1/provider", s.requireService(capabilityProviderInfo, s.providerInfo))
	mux.HandleFunc("GET /api/v1/bootstrap", s.bootstrap)
	mux.HandleFunc("GET /api/v1/tasks", s.requireService(capabilityWorkReadyList, s.listTasks))
	mux.HandleFunc("POST /api/v1/tasks", s.createTask)
	mux.HandleFunc("GET /api/v1/tasks/{id}", s.requireService(capabilityWorkInspect, s.getTask))
	mux.HandleFunc("PATCH /api/v1/tasks/{id}", s.updateTask)
	mux.HandleFunc("POST /api/v1/tasks/{id}/move", s.moveTask)
	mux.HandleFunc("POST /api/v1/tasks/{id}/claim", s.requireService(capabilityWorkClaim, s.claimTask))
	mux.HandleFunc("GET /api/v1/claims/{key}", s.requireService(capabilityClaimLookup, s.lookupClaim))
	mux.HandleFunc("GET /api/v1/agents", s.listAgents)
	mux.HandleFunc("POST /api/v1/agents", s.requireService(capabilityAgentRegister, s.registerAgent))
	mux.HandleFunc("POST /api/v1/runs/{id}/heartbeat", s.requireService(capabilityLeaseRenew, s.heartbeatRun))
	mux.HandleFunc("POST /api/v1/runs/{id}/plan", s.requireService(capabilityPlanSubmit, s.submitPlan))
	mux.HandleFunc("POST /api/v1/runs/{id}/start", s.requireService(capabilityRunStart, s.startRun))
	mux.HandleFunc("POST /api/v1/runs/{id}/progress", s.requireService(capabilityProgressAppend, s.reportProgress))
	mux.HandleFunc("POST /api/v1/runs/{id}/verify", s.requireService(capabilityRunVerify, s.beginVerification))
	mux.HandleFunc("POST /api/v1/runs/{id}/complete", s.requireService(capabilityRunComplete, s.completeRun))
	mux.HandleFunc("POST /api/v1/runs/{id}/block", s.requireService(capabilityRunBlock, s.blockRun))
	mux.HandleFunc("GET /api/v1/runs", s.requireService(capabilityAssignedRuns, s.listAssignedRuns))
	mux.HandleFunc("GET /api/v1/runs/{id}/commands", s.requireService(capabilityCommandList, s.listCommands))
	mux.HandleFunc("POST /api/v1/runs/{id}/commands/{commandId}/ack", s.requireService(capabilityCommandAck, s.acknowledgeCommand))
	mux.HandleFunc("POST /api/v1/runs/{id}/events", s.requireService(capabilityEventAppend, s.appendProjectedEvent))
	mux.HandleFunc("GET /api/v1/decisions", s.requireService(capabilityDecisionList, s.listDecisions))
	mux.HandleFunc("GET /api/v1/decisions/{id}", s.requireService(capabilityDecisionInspect, s.getDecision))
	mux.HandleFunc("POST /api/v1/decisions/{id}/answer", s.requireService(capabilityDecisionAnswer, s.answerDecision))
	mux.HandleFunc("GET /api/v1/events", s.requireService(capabilityEventList, s.listEvents))
	mux.HandleFunc("GET /api/v1/events/stream", s.requireService(capabilityEventStream, s.streamEvents))

	// Local operator routes remain separate from the authenticated service
	// surface until human sessions are implemented. They must not be exposed to
	// an untrusted network.
	mux.HandleFunc("GET /api/v1/operator/tasks/{id}", s.getTask)
	mux.HandleFunc("POST /api/v1/operator/decisions/{id}/answer", s.answerDecision)
	mux.HandleFunc("POST /api/v1/operator/runs/{id}/commands", s.createOperatorCommand)
	mux.HandleFunc("GET /api/v1/operator/events/stream", s.streamEvents)
	return s.middleware(mux)
}

func (s *Server) lookupClaim(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if projectID == "" || !s.requireProject(w, r, projectID) {
		return
	}
	actorID := "local-prototype"
	if identity, ok := serviceIdentityFromRequest(r); ok {
		actorID = "service:" + identity.ID
	}
	run, err := s.store.LookupClaim(r.Context(), projectID, actorID, r.PathValue("key"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) listAssignedRuns(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	if projectID == "" || agentID == "" {
		s.writeError(w, fmt.Errorf("project_id and agent_id are required: %w", domain.ErrInvalidInput))
		return
	}
	if !s.requireProject(w, r, projectID) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, next, err := s.store.ListAssignedRuns(r.Context(), projectID, agentID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}

func (s *Server) createOperatorCommand(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Type   string `json:"type"`
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	command, err := s.store.CreateCommand(r.Context(), r.PathValue("id"), input.Type, input.Reason, "operator:local")
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, command)
}

func (s *Server) listCommands(w http.ResponseWriter, r *http.Request) {
	if !s.requireRunProject(w, r, r.PathValue("id")) {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after_sequence"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	commands, err := s.store.ListCommands(r.Context(), r.PathValue("id"), after, limit)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": commands})
}

func (s *Server) acknowledgeCommand(w http.ResponseWriter, r *http.Request) {
	if !s.requireRunProject(w, r, r.PathValue("id")) {
		return
	}
	var input struct {
		AgentID         string `json:"agent_id"`
		ExpectedVersion int64  `json:"expected_version"`
		FenceToken      int64  `json:"fence_token"`
		Status          string `json:"status"`
		Result          string `json:"result"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	mutation, err := mutationFromRequest(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	mutation.Guard = &store.RunGuard{AgentID: input.AgentID, ExpectedVersion: input.ExpectedVersion, FenceToken: input.FenceToken}
	if err := s.store.AcknowledgeCommand(r.Context(), mutation, r.PathValue("id"), r.PathValue("commandId"), input.Status, input.Result); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) appendProjectedEvent(w http.ResponseWriter, r *http.Request) {
	if !s.requireRunProject(w, r, r.PathValue("id")) {
		return
	}
	var input struct {
		AgentID         string         `json:"agent_id"`
		ExpectedVersion int64          `json:"expected_version"`
		FenceToken      int64          `json:"fence_token"`
		Producer        string         `json:"producer"`
		ProducerEventID string         `json:"producer_event_id"`
		ExecutionID     string         `json:"execution_id"`
		Sequence        int64          `json:"sequence"`
		Type            string         `json:"type"`
		Message         string         `json:"message"`
		Payload         map[string]any `json:"payload"`
		OccurredAt      int64          `json:"occurred_at"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	mutation, err := mutationFromRequest(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	mutation.Guard = &store.RunGuard{AgentID: input.AgentID, ExpectedVersion: input.ExpectedVersion, FenceToken: input.FenceToken}
	projected := domain.ProjectedEvent{Producer: input.Producer, ProducerEventID: input.ProducerEventID,
		ExecutionID: input.ExecutionID, Sequence: input.Sequence, Type: input.Type, Message: input.Message,
		Payload: input.Payload, OccurredAt: input.OccurredAt}
	if err := s.store.AppendProjectedEvent(r.Context(), mutation, r.PathValue("id"), projected); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "service": "appdeck", "time": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	ready := true
	checks := map[string]string{
		"store":        "available",
		"service_auth": "disabled",
	}
	if err := s.store.Health(r.Context()); err != nil {
		ready = false
		checks["store"] = "unavailable"
	}
	if s.serviceAuthorizer != nil {
		checks["service_auth"] = "available"
		if _, err := s.serviceAuthorizer.load(); err != nil {
			ready = false
			checks["service_auth"] = "unavailable"
		}
	}

	status := http.StatusOK
	state := "ready"
	if !ready {
		status = http.StatusServiceUnavailable
		state = "degraded"
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]any{
		"status": state, "service": "appdeck", "readiness": checks,
		"time": time.Now().UTC().Format(time.RFC3339),
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
		writeJSON(w, http.StatusOK, filterTasksForServiceIdentity(r, tasks))
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
		if task.Status == status && serviceIdentityAllowsProject(r, task.ProjectID) {
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
	if !s.requireProject(w, r, detail.Task.ProjectID) {
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
		AgentID             string `json:"agent_id"`
		ExpectedTaskVersion int    `json:"expected_task_version"`
		LeaseSeconds        int    `json:"lease_seconds"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	task, err := s.store.GetTask(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	if !s.requireProject(w, r, task.ProjectID) {
		return
	}
	mutation, err := mutationFromRequest(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	run, err := s.store.ClaimTaskVersioned(
		r.Context(), mutation, r.PathValue("id"), input.ExpectedTaskVersion,
		input.AgentID, time.Duration(input.LeaseSeconds)*time.Second,
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
	if strings.TrimSpace(agent.ID) == "" || strings.TrimSpace(agent.InstallationID) == "" {
		s.writeError(w, fmt.Errorf("stable id and installation_id are required: %w", domain.ErrInvalidInput))
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
	if !s.requireRunProject(w, r, r.PathValue("id")) {
		return
	}
	var input struct {
		AgentID         string `json:"agent_id"`
		ExpectedVersion int64  `json:"expected_version"`
		FenceToken      int64  `json:"fence_token"`
		LeaseSeconds    int    `json:"lease_seconds"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	mutation, err := mutationFromRequest(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	mutation.Guard = &store.RunGuard{AgentID: input.AgentID, ExpectedVersion: input.ExpectedVersion, FenceToken: input.FenceToken}
	if err := s.store.HeartbeatRun(
		r.Context(), mutation, r.PathValue("id"), input.AgentID, time.Duration(input.LeaseSeconds)*time.Second,
	); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) submitPlan(w http.ResponseWriter, r *http.Request) {
	if !s.requireRunProject(w, r, r.PathValue("id")) {
		return
	}
	var input struct {
		AgentID         string `json:"agent_id"`
		ExpectedVersion int64  `json:"expected_version"`
		FenceToken      int64  `json:"fence_token"`
		Plan            string `json:"plan"`
		RequireApproval bool   `json:"require_approval"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	mutation, err := mutationFromRequest(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	mutation.Guard = &store.RunGuard{AgentID: input.AgentID, ExpectedVersion: input.ExpectedVersion, FenceToken: input.FenceToken}
	decision, err := s.store.SubmitPlan(
		r.Context(), mutation, r.PathValue("id"), input.Plan, input.RequireApproval,
	)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"decision": decision})
}

func (s *Server) reportProgress(w http.ResponseWriter, r *http.Request) {
	if !s.requireRunProject(w, r, r.PathValue("id")) {
		return
	}
	var input struct {
		AgentID         string         `json:"agent_id"`
		ExpectedVersion int64          `json:"expected_version"`
		FenceToken      int64          `json:"fence_token"`
		Message         string         `json:"message"`
		Payload         map[string]any `json:"payload"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	mutation, err := mutationFromRequest(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	mutation.Guard = &store.RunGuard{AgentID: input.AgentID, ExpectedVersion: input.ExpectedVersion, FenceToken: input.FenceToken}
	if err := s.store.ReportProgress(
		r.Context(), mutation, r.PathValue("id"), input.Message, input.Payload,
	); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) startRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireRunProject(w, r, r.PathValue("id")) {
		return
	}
	input, err := readRunGuard(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	mutation, err := mutationFromRequest(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	mutation.Guard = input
	if err := s.store.StartRun(r.Context(), mutation, r.PathValue("id")); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) beginVerification(w http.ResponseWriter, r *http.Request) {
	if !s.requireRunProject(w, r, r.PathValue("id")) {
		return
	}
	input, err := readRunGuard(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	mutation, err := mutationFromRequest(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	mutation.Guard = input
	if err := s.store.BeginVerification(r.Context(), mutation, r.PathValue("id")); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) completeRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireRunProject(w, r, r.PathValue("id")) {
		return
	}
	var input struct {
		AgentID         string `json:"agent_id"`
		ExpectedVersion int64  `json:"expected_version"`
		FenceToken      int64  `json:"fence_token"`
		Summary         string `json:"summary"`
		Evidence        string `json:"evidence"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	mutation, err := mutationFromRequest(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	mutation.Guard = &store.RunGuard{AgentID: input.AgentID, ExpectedVersion: input.ExpectedVersion, FenceToken: input.FenceToken}
	if err := s.store.CompleteRun(
		r.Context(), mutation, r.PathValue("id"), input.Summary, input.Evidence,
	); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) blockRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireRunProject(w, r, r.PathValue("id")) {
		return
	}
	var input struct {
		AgentID         string `json:"agent_id"`
		ExpectedVersion int64  `json:"expected_version"`
		FenceToken      int64  `json:"fence_token"`
		Reason          string `json:"reason"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	if strings.TrimSpace(input.Reason) == "" {
		s.writeError(w, fmt.Errorf("reason is required: %w", domain.ErrInvalidInput))
		return
	}
	mutation, err := mutationFromRequest(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	mutation.Guard = &store.RunGuard{AgentID: input.AgentID, ExpectedVersion: input.ExpectedVersion, FenceToken: input.FenceToken}
	if err := s.store.BlockRun(r.Context(), mutation, r.PathValue("id"), input.Reason); err != nil {
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
	filtered, err := s.filterDecisionsForServiceIdentity(r, decisions)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, filtered)
}

func (s *Server) getDecision(w http.ResponseWriter, r *http.Request) {
	decision, err := s.store.GetDecision(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	if !s.requireTaskProject(w, r, decision.TaskID) {
		return
	}
	writeJSON(w, http.StatusOK, decision)
}

func (s *Server) answerDecision(w http.ResponseWriter, r *http.Request) {
	decision, err := s.store.GetDecision(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	if !s.requireTaskProject(w, r, decision.TaskID) {
		return
	}
	var input struct {
		Answer string `json:"answer"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	decision, err = s.store.AnswerDecision(r.Context(), r.PathValue("id"), input.Answer)
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
	filtered, err := s.filterEventsForServiceIdentity(r, events)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, filtered)
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
			missed, err = s.filterEventsForServiceIdentity(r, missed)
			if err != nil {
				return
			}
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
			allowed, err := s.serviceIdentityAllowsEvent(r, event)
			if err != nil {
				return
			}
			if !allowed {
				continue
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

func (s *Server) requireService(capability string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.serviceAuthorizer == nil {
			next(w, r)
			return
		}
		identity, err := s.serviceAuthorizer.Authorize(r, capability)
		if err != nil {
			s.writeServiceAuthError(w, err)
			return
		}
		ctx := context.WithValue(r.Context(), serviceIdentityContextKey{}, identity)
		next(w, r.WithContext(ctx))
	}
}

func mutationFromRequest(r *http.Request) (store.Mutation, error) {
	actorID := "local-prototype"
	if identity, ok := serviceIdentityFromRequest(r); ok {
		actorID = "service:" + identity.ID
	}
	values := r.Header.Values("Idempotency-Key")
	if len(values) > 1 {
		return store.Mutation{}, fmt.Errorf("Idempotency-Key must be sent at most once: %w", domain.ErrInvalidInput)
	}
	if len(values) == 0 {
		return store.Mutation{}, fmt.Errorf("Idempotency-Key is required: %w", domain.ErrInvalidInput)
	}
	return store.NewMutation(actorID, values[0])
}

func readRunGuard(r *http.Request) (*store.RunGuard, error) {
	var input struct {
		AgentID         string `json:"agent_id"`
		ExpectedVersion int64  `json:"expected_version"`
		FenceToken      int64  `json:"fence_token"`
	}
	if err := readJSON(r, &input); err != nil {
		return nil, err
	}
	guard := &store.RunGuard{AgentID: input.AgentID, ExpectedVersion: input.ExpectedVersion, FenceToken: input.FenceToken}
	if strings.TrimSpace(guard.AgentID) == "" || guard.ExpectedVersion < 1 || guard.FenceToken < 1 {
		return nil, fmt.Errorf("agent_id, expected_version, and fence_token are required: %w", domain.ErrInvalidInput)
	}
	return guard, nil
}

func (s *Server) writeServiceAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrServiceUnauthenticated):
		w.Header().Set("WWW-Authenticate", `Bearer realm="appdeck"`)
		s.writeAPIError(w, http.StatusUnauthorized, errorCodeUnauthenticated, "service authentication required", false)
	case errors.Is(err, ErrServiceForbidden):
		s.writeAPIError(w, http.StatusForbidden, errorCodeForbidden, "service operation is not authorized", false)
	case errors.Is(err, ErrServiceAuthUnavailable):
		s.logger.Error("service authorization policy unavailable", "error", err)
		s.writeAPIError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "service authorization is unavailable", true)
	default:
		s.logger.Error("service authorization failed", "error", err)
		s.writeAPIError(w, http.StatusInternalServerError, errorCodeInternal, "internal server error", true)
	}
}

func serviceIdentityFromRequest(r *http.Request) (ServiceIdentity, bool) {
	identity, ok := r.Context().Value(serviceIdentityContextKey{}).(ServiceIdentity)
	return identity, ok
}

func serviceIdentityAllowsProject(r *http.Request, projectID string) bool {
	identity, ok := serviceIdentityFromRequest(r)
	return !ok || identity.AllowsProject(projectID)
}

func (s *Server) requireProject(w http.ResponseWriter, r *http.Request, projectID string) bool {
	if serviceIdentityAllowsProject(r, projectID) {
		return true
	}
	s.writeServiceAuthError(w, ErrServiceForbidden)
	return false
}

func (s *Server) requireTaskProject(w http.ResponseWriter, r *http.Request, taskID string) bool {
	task, err := s.store.GetTask(r.Context(), taskID)
	if err != nil {
		s.writeError(w, err)
		return false
	}
	return s.requireProject(w, r, task.ProjectID)
}

func (s *Server) requireRunProject(w http.ResponseWriter, r *http.Request, runID string) bool {
	projectID, err := s.store.ProjectIDForRun(r.Context(), runID)
	if err != nil {
		s.writeError(w, err)
		return false
	}
	return s.requireProject(w, r, projectID)
}

func filterTasksForServiceIdentity(r *http.Request, tasks []domain.Task) []domain.Task {
	if _, ok := serviceIdentityFromRequest(r); !ok {
		return tasks
	}
	filtered := make([]domain.Task, 0, len(tasks))
	for _, task := range tasks {
		if serviceIdentityAllowsProject(r, task.ProjectID) {
			filtered = append(filtered, task)
		}
	}
	return filtered
}

func (s *Server) filterDecisionsForServiceIdentity(
	r *http.Request,
	decisions []domain.Decision,
) ([]domain.Decision, error) {
	if _, ok := serviceIdentityFromRequest(r); !ok {
		return decisions, nil
	}
	filtered := make([]domain.Decision, 0, len(decisions))
	projects := make(map[string]string)
	for _, decision := range decisions {
		projectID, ok := projects[decision.TaskID]
		if !ok {
			task, err := s.store.GetTask(r.Context(), decision.TaskID)
			if err != nil {
				return nil, err
			}
			projectID = task.ProjectID
			projects[decision.TaskID] = projectID
		}
		if serviceIdentityAllowsProject(r, projectID) {
			filtered = append(filtered, decision)
		}
	}
	return filtered, nil
}

func (s *Server) filterEventsForServiceIdentity(
	r *http.Request,
	events []domain.Event,
) ([]domain.Event, error) {
	if _, ok := serviceIdentityFromRequest(r); !ok {
		return events, nil
	}
	filtered := make([]domain.Event, 0, len(events))
	for _, event := range events {
		allowed, err := s.serviceIdentityAllowsEvent(r, event)
		if err != nil {
			return nil, err
		}
		if allowed {
			filtered = append(filtered, event)
		}
	}
	return filtered, nil
}

func (s *Server) serviceIdentityAllowsEvent(r *http.Request, event domain.Event) (bool, error) {
	if _, ok := serviceIdentityFromRequest(r); !ok {
		return true, nil
	}
	if event.TaskID == nil {
		return false, nil
	}
	task, err := s.store.GetTask(r.Context(), *event.TaskID)
	if err != nil {
		return false, err
	}
	return serviceIdentityAllowsProject(r, task.ProjectID), nil
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
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, Last-Event-ID, X-Request-ID")
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
