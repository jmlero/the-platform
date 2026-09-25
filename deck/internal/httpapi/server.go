package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/store"
)

type Server struct {
	store                 *store.Store
	logger                *slog.Logger
	serviceAuthorizer     *FileServiceAuthorizer
	operatorAuthenticator *FileOperatorAuthenticator
	secureOperatorCookies bool
	operatorSessionTTL    time.Duration
	artifactURL           string
	artifactTokenFile     string
	artifactCAFile        string
	artifactClients       artifactClientCache
}

type Option func(*Server)

func WithServiceAuthorizer(authorizer *FileServiceAuthorizer) Option {
	return func(server *Server) {
		server.serviceAuthorizer = authorizer
	}
}

func WithOperatorAuthenticator(authenticator *FileOperatorAuthenticator) Option {
	return func(server *Server) {
		server.operatorAuthenticator = authenticator
	}
}

func WithSecureOperatorCookies(secure bool) Option {
	return func(server *Server) {
		server.secureOperatorCookies = secure
	}
}

type serviceIdentityContextKey struct{}

type operatorSessionContextKey struct{}

const (
	operatorSessionCookieName = "deck_operator_session"
	operatorCSRFCookieName    = "deck_operator_csrf"
	operatorCSRFHeader        = "X-CSRF-Token"
	defaultOperatorSessionTTL = 12 * time.Hour
)

type authenticatedOperatorSession struct {
	OperatorID string
	TokenHash  string
	ExpiresAt  int64
}

type operatorSessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	OperatorID    string `json:"operator_id"`
	ExpiresAt     int64  `json:"expires_at,omitempty"`
}

type bootstrapResponse struct {
	Tasks          []domain.Task         `json:"tasks"`
	Agents         []domain.Agent        `json:"agents"`
	Decisions      []domain.Decision     `json:"decisions"`
	Events         []domain.Event        `json:"events"`
	Channels       []domain.Channel      `json:"channels"`
	ProviderStatus domain.ProviderStatus `json:"provider_status"`
	Now            int64                 `json:"now"`
}

func New(store *store.Store, logger *slog.Logger, options ...Option) *Server {
	server := &Server{
		store: store, logger: logger, secureOperatorCookies: true,
		operatorSessionTTL: defaultOperatorSessionTTL,
	}
	for _, option := range options {
		option(server)
	}
	return server
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.contextRoutes(mux)
	s.workloadRoutes(mux)
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.readiness)
	mux.HandleFunc("GET /api/v1/provider", s.requireService(capabilityProviderInfo, s.providerInfo))
	mux.HandleFunc("GET /api/v1/operator/provider", s.requireOperator(s.providerInfo))
	mux.HandleFunc("POST /api/v1/operator/session", s.createOperatorSession)
	mux.HandleFunc("GET /api/v1/operator/session", s.requireOperator(s.getOperatorSession))
	mux.HandleFunc("DELETE /api/v1/operator/session", s.requireOperator(s.deleteOperatorSession))
	mux.HandleFunc("POST /api/v1/operator/tasks/{id}/accept", s.requireOperator(s.acceptTask))
	mux.HandleFunc("PUT /api/v1/operator/tasks/{id}/dependencies", s.requireOperator(s.setTaskDependencies))
	mux.HandleFunc("GET /api/v1/operator/projects", s.requireOperator(s.projectChannels))
	mux.HandleFunc("POST /api/v1/operator/projects", s.requireOperator(s.createProjectChannel))
	mux.HandleFunc("GET /api/v1/bootstrap", s.requireOperator(s.bootstrap))
	mux.HandleFunc("GET /api/v1/tasks", s.requireService(capabilityWorkReadyList, s.listTasks))
	mux.HandleFunc("POST /api/v1/tasks", s.requireOperator(s.createTask))
	mux.HandleFunc("GET /api/v1/tasks/{id}", s.requireService(capabilityWorkInspect, s.getTask))
	mux.HandleFunc("PATCH /api/v1/tasks/{id}", s.requireOperator(s.updateTask))
	mux.HandleFunc("POST /api/v1/tasks/{id}/move", s.requireOperator(s.moveTask))
	mux.HandleFunc("POST /api/v1/tasks/{id}/claim", s.requireService(capabilityWorkClaim, s.claimTask))
	mux.HandleFunc("GET /api/v1/claims/{key}", s.requireService(capabilityClaimLookup, s.lookupClaim))
	mux.HandleFunc("GET /api/v1/agents", s.requireOperator(s.listAgents))
	mux.HandleFunc("POST /api/v1/agents", s.requireService(capabilityAgentRegister, s.registerAgent))
	mux.HandleFunc("POST /api/v1/runs/{id}/heartbeat", s.requireService(capabilityLeaseRenew, s.heartbeatRun))
	mux.HandleFunc("POST /api/v1/runs/{id}/plan", s.requireService(capabilityPlanSubmit, s.submitPlan))
	mux.HandleFunc("POST /api/v1/runs/{id}/questions", s.requireService(capabilityDecisionRequest, s.requestEngineQuestion))
	mux.HandleFunc("POST /api/v1/runs/{id}/start", s.requireService(capabilityRunStart, s.runTransition(s.store.StartRun)))
	mux.HandleFunc("POST /api/v1/runs/{id}/progress", s.requireService(capabilityProgressAppend, s.reportProgress))
	mux.HandleFunc("POST /api/v1/runs/{id}/verify", s.requireService(capabilityRunVerify, s.runTransition(s.store.BeginVerification)))
	mux.HandleFunc("POST /api/v1/runs/{id}/complete", s.requireService(capabilityRunComplete, s.completeRun))
	mux.HandleFunc("POST /api/v1/runs/{id}/block", s.requireService(capabilityRunBlock, s.blockRun))
	mux.HandleFunc("GET /api/v1/runs", s.requireService(capabilityAssignedRuns, s.listAssignedRuns))
	mux.HandleFunc("GET /api/v1/runs/{id}/commands", s.requireService(capabilityCommandList, s.listCommands))
	mux.HandleFunc("POST /api/v1/runs/{id}/commands/{commandId}/ack", s.requireService(capabilityCommandAck, s.acknowledgeCommand))
	mux.HandleFunc("POST /api/v1/runs/{id}/events", s.requireService(capabilityEventAppend, s.appendProjectedEvent))
	mux.HandleFunc("POST /api/v1/runs/{id}/evidence", s.requireService(capabilityEvidenceRecord, s.recordEvidence))
	mux.HandleFunc("POST /api/v1/runs/{id}/operations", s.requireService(capabilityOperationReport, s.reportOperationalState))
	mux.HandleFunc("GET /api/v1/channels", s.requireService(capabilityChannelList, s.listChannels))
	mux.HandleFunc("GET /api/v1/channels/{id}/messages", s.requireService(capabilityChannelRead, s.listChannelMessages))
	mux.HandleFunc("GET /api/v1/conversation/invocations", s.requireService(capabilityConversationList, s.listConversationInvocations))
	mux.HandleFunc("POST /api/v1/conversation/outcomes", s.requireService(capabilityConversationOutcome, s.publishConversationOutcome))
	mux.HandleFunc("POST /api/v1/conversation/delegations", s.requireService(capabilityConversationDelegate, s.publishConversationOutcome))
	mux.HandleFunc("POST /api/v1/conversation/questions", s.requireService(capabilityConversationQuestion, s.ensureConversationQuestion))
	mux.HandleFunc("GET /api/v1/operator/threads/{id}/questions", s.requireOperator(s.listConversationQuestions))
	mux.HandleFunc("POST /api/v1/operator/conversation/questions/{id}/answer", s.requireOperator(s.answerConversationQuestion))
	mux.HandleFunc("POST /api/v1/channels/{id}/messages", s.requireService(capabilityChannelMessage, s.postChannelMessage))
	mux.HandleFunc("GET /api/v1/decisions", s.requireService(capabilityDecisionList, s.listDecisions))
	mux.HandleFunc("GET /api/v1/decisions/{id}", s.requireService(capabilityDecisionInspect, s.getDecision))
	mux.HandleFunc("POST /api/v1/decisions/{id}/answer", s.requireService(capabilityDecisionAnswer, s.answerDecision))
	mux.HandleFunc("GET /api/v1/events", s.requireService(capabilityEventList, s.listEvents))
	mux.HandleFunc("GET /api/v1/events/stream", s.requireService(capabilityEventStream, s.streamEvents))

	mux.HandleFunc("DELETE /api/v1/operator/tasks/{id}", s.requireOperator(s.deleteDraft))
	mux.HandleFunc("PUT /api/v1/operator/tasks/{id}/archive", s.requireOperator(s.setTaskArchived))
	mux.HandleFunc("GET /api/v1/operator/tasks/{id}", s.requireOperator(s.getTask))
	mux.HandleFunc("GET /api/v1/operator/tasks/{id}/artifacts/{digest}", s.requireOperator(s.downloadArtifact))
	mux.HandleFunc("POST /api/v1/operator/decisions/{id}/answer", s.requireOperator(s.answerDecision))
	mux.HandleFunc("POST /api/v1/operator/runs/{id}/commands", s.requireOperator(s.createOperatorCommand))
	mux.HandleFunc("GET /api/v1/operator/events/stream", s.requireOperator(s.streamEvents))
	mux.HandleFunc("GET /api/v1/operator/channels/{id}/threads", s.requireOperator(s.listThreads))
	mux.HandleFunc("POST /api/v1/operator/channels/{id}/threads", s.requireOperator(s.createThread))
	mux.HandleFunc("POST /api/v1/operator/channels/{id}/invocations", s.requireOperator(s.invokeConversation))
	mux.HandleFunc("GET /api/v1/operator/threads/{id}", s.requireOperator(s.getThread))
	mux.HandleFunc("GET /api/v1/operator/threads/{id}/messages", s.requireOperator(s.listThreadMessages))
	mux.HandleFunc("POST /api/v1/operator/threads/{id}/tasks", s.requireOperator(s.linkThreadTask))
	mux.HandleFunc("GET /api/v1/operator/tasks/{id}/thread", s.requireOperator(s.getTaskThread))
	mux.HandleFunc("GET /api/v1/operator/channels", s.requireOperator(s.listChannels))
	mux.HandleFunc("POST /api/v1/operator/channels", s.requireOperator(s.createChannel))
	mux.HandleFunc("GET /api/v1/operator/channels/{id}/messages", s.requireOperator(s.listChannelMessages))
	mux.HandleFunc("POST /api/v1/operator/channels/{id}/messages", s.requireOperator(s.postChannelMessage))
	mux.HandleFunc("POST /api/v1/operator/messages/{id}/promote", s.requireOperator(s.promoteChannelMessage))
	mux.HandleFunc("GET /api/v1/operator/export", s.requireOperator(s.exportProject))
	mux.HandleFunc("POST /api/v1/operator/import", s.requireOperator(s.importProject))
	return s.middleware(mux)
}

func (s *Server) createOperatorSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.operatorAuthenticator == nil {
		s.writeAPIError(w, http.StatusNotFound, errorCodeNotFound, "operator authentication is disabled", false)
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	identity, err := s.operatorAuthenticator.Authenticate(input.Token)
	if err != nil {
		s.writeOperatorAuthError(w, err)
		return
	}
	sessionToken, err := newOperatorSecret()
	if err != nil {
		s.logger.Error("generate operator session token", "error", err)
		s.writeAPIError(w, http.StatusInternalServerError, errorCodeInternal, "internal server error", true)
		return
	}
	csrfToken, err := newOperatorSecret()
	if err != nil {
		s.logger.Error("generate operator CSRF token", "error", err)
		s.writeAPIError(w, http.StatusInternalServerError, errorCodeInternal, "internal server error", true)
		return
	}
	expiresAt := time.Now().UTC().Add(s.operatorSessionTTL)
	if err := s.store.CreateOperatorSession(r.Context(), store.OperatorSession{
		TokenHash: sha256Hex(sessionToken), CSRFHash: sha256Hex(csrfToken),
		OperatorID: identity.ID, ExpiresAt: expiresAt.UnixMilli(),
	}); err != nil {
		s.logger.Error("create operator session", "error", err)
		s.writeAPIError(w, http.StatusInternalServerError, errorCodeInternal, "internal server error", true)
		return
	}
	s.setOperatorCookies(w, sessionToken, csrfToken, expiresAt)
	writeJSON(w, http.StatusCreated, operatorSessionResponse{
		Authenticated: true, OperatorID: identity.ID, ExpiresAt: expiresAt.UnixMilli(),
	})
}

func (s *Server) getOperatorSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	session, _ := operatorSessionFromRequest(r)
	writeJSON(w, http.StatusOK, operatorSessionResponse{
		Authenticated: true, OperatorID: session.OperatorID, ExpiresAt: session.ExpiresAt,
	})
}

func (s *Server) deleteOperatorSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	session, _ := operatorSessionFromRequest(r)
	if err := s.store.DeleteOperatorSession(r.Context(), session.TokenHash); err != nil {
		s.logger.Error("delete operator session", "error", err)
		s.writeAPIError(w, http.StatusInternalServerError, errorCodeInternal, "internal server error", true)
		return
	}
	s.clearOperatorCookies(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setOperatorCookies(
	w http.ResponseWriter,
	sessionToken string,
	csrfToken string,
	expiresAt time.Time,
) {
	maxAge := int(time.Until(expiresAt).Seconds())
	http.SetCookie(w, &http.Cookie{
		Name: operatorSessionCookieName, Value: sessionToken, Path: "/",
		Expires: expiresAt, MaxAge: maxAge, HttpOnly: true,
		Secure: s.secureOperatorCookies, SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: operatorCSRFCookieName, Value: csrfToken, Path: "/",
		Expires: expiresAt, MaxAge: maxAge, HttpOnly: false,
		Secure: s.secureOperatorCookies, SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) clearOperatorCookies(w http.ResponseWriter) {
	for _, name := range []string{operatorSessionCookieName, operatorCSRFCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name: name, Path: "/", MaxAge: -1, Expires: time.Unix(1, 0),
			HttpOnly: name == operatorSessionCookieName,
			Secure:   s.secureOperatorCookies, SameSite: http.SameSiteStrictMode,
		})
	}
}

func (s *Server) lookupClaim(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if projectID == "" || !s.requireProject(w, r, projectID) {
		return
	}
	actorID := serviceActorID(r)
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
	command, err := s.store.CreateCommand(
		r.Context(), r.PathValue("id"), input.Type, input.Reason,
		"operator:"+operatorIDFromRequest(r),
	)
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
		store.RunGuard
		Status string `json:"status"`
		Result string `json:"result"`
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
	mutation.Guard = &input.RunGuard
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
		store.RunGuard
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
	mutation.Guard = &input.RunGuard
	projected := domain.ProjectedEvent{Producer: input.Producer, ProducerEventID: input.ProducerEventID,
		ExecutionID: input.ExecutionID, Sequence: input.Sequence, Type: input.Type, Message: input.Message,
		Payload: input.Payload, OccurredAt: input.OccurredAt}
	if err := s.store.AppendProjectedEvent(r.Context(), mutation, r.PathValue("id"), projected); err != nil {
		if projectID, projectErr := s.store.ProjectIDForRun(r.Context(), r.PathValue("id")); projectErr == nil && projected.Producer != "" {
			dependencies := stringSliceFromAny(projected.Payload["degraded_dependencies"])
			_ = s.store.RecordProjectionRejected(r.Context(), projectID, projected.Producer, err.Error(), dependencies)
		}
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) recordEvidence(w http.ResponseWriter, r *http.Request) {
	if !s.requireRunProject(w, r, r.PathValue("id")) {
		return
	}
	var input struct {
		store.RunGuard
		domain.EvidenceInput
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
	mutation.Guard = &input.RunGuard
	input.RunID = r.PathValue("id")
	evidence, err := s.store.RecordEvidenceGuarded(r.Context(), mutation, input.EvidenceInput)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, evidence)
}

func (s *Server) reportOperationalState(w http.ResponseWriter, r *http.Request) {
	if !s.requireRunProject(w, r, r.PathValue("id")) {
		return
	}
	var input struct {
		store.RunGuard
		Environment          string `json:"environment"`
		RolloutState         string `json:"rollout_state"`
		ObservationStartedAt *int64 `json:"observation_started_at"`
		ObservationEndsAt    *int64 `json:"observation_ends_at"`
		RollbackState        string `json:"rollback_state"`
		PolicyVersion        string `json:"policy_version"`
		SourceRevision       string `json:"source_revision"`
		Outcome              string `json:"outcome"`
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
	mutation.Guard = &input.RunGuard
	state, err := s.store.RecordOperationalStateGuarded(r.Context(), mutation, domain.OperationalStateInput{
		RunID: r.PathValue("id"), Environment: input.Environment, RolloutState: input.RolloutState,
		ObservationStartedAt: input.ObservationStartedAt, ObservationEndsAt: input.ObservationEndsAt,
		RollbackState: input.RollbackState, PolicyVersion: input.PolicyVersion,
		SourceRevision: input.SourceRevision, Outcome: input.Outcome,
	})
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if projectID == "" {
		projectID = "prj_default"
	}
	if !s.requireProject(w, r, projectID) {
		return
	}
	channels, err := s.store.ListChannels(r.Context(), projectID)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": channels})
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID string `json:"project_id"`
		Name      string `json:"name"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	channel, err := s.store.CreateChannel(r.Context(), input.ProjectID, input.Name)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, channel)
}

func (s *Server) listChannelMessages(w http.ResponseWriter, r *http.Request) {
	channel, err := s.store.GetChannel(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	if !s.requireProject(w, r, channel.ProjectID) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	messages, err := s.store.PageChannelMessages(r.Context(), channel.ID, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, messages)
}

func (s *Server) postChannelMessage(w http.ResponseWriter, r *http.Request) {
	channel, err := s.store.GetChannel(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	if !s.requireProject(w, r, channel.ProjectID) {
		return
	}
	var input struct {
		ThreadID   string                     `json:"thread_id"`
		Body       string                     `json:"body"`
		References domain.CanonicalReferences `json:"references"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	authorType, authorID := "agent", serviceActorID(r)
	if operatorID, ok := operatorSessionFromRequest(r); ok {
		authorType, authorID = "human", "operator:"+operatorID.OperatorID
	}
	message, err := s.store.PostChannelMessage(r.Context(), domain.ChannelMessageInput{
		ChannelID: channel.ID, ThreadID: input.ThreadID, AuthorType: authorType, AuthorID: authorID,
		Body: input.Body, References: input.References,
	})
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, message)
}

func (s *Server) promoteChannelMessage(w http.ResponseWriter, r *http.Request) {
	var input domain.CreateTaskInput
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	task, err := s.store.PromoteChannelMessage(r.Context(), r.PathValue("id"), input)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) exportProject(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if projectID == "" {
		projectID = "prj_default"
	}
	encoded, err := s.store.ExportProject(r.Context(), projectID)
	if err != nil {
		s.writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="deck-export.json"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encoded)
}

func (s *Server) importProject(w http.ResponseWriter, r *http.Request) {
	encoded, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		s.writeError(w, fmt.Errorf("read project import: %w", domain.ErrInvalidInput))
		return
	}
	if err := s.store.ImportProject(r.Context(), encoded); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func stringSliceFromAny(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if stringsValue, ok := value.([]string); ok {
			return stringsValue
		}
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			result = append(result, strings.TrimSpace(text))
		}
	}
	return result
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "service": "deck", "time": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	ready := true
	checks := map[string]string{
		"store":         "available",
		"service_auth":  "disabled",
		"operator_auth": "disabled",
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
	if s.operatorAuthenticator != nil {
		checks["operator_auth"] = "available"
		if _, err := s.operatorAuthenticator.load(); err != nil {
			ready = false
			checks["operator_auth"] = "unavailable"
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
		"status": state, "service": "deck", "readiness": checks,
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
	events, err := s.store.ListEvents(r.Context(), 80, 0, nil)
	if err != nil {
		s.writeError(w, err)
		return
	}
	channels, err := s.store.ListChannels(r.Context(), "prj_default")
	if err != nil {
		s.writeError(w, err)
		return
	}
	providerStatus, err := s.store.GetProviderStatus(r.Context(), "prj_default")
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bootstrapResponse{
		Tasks: tasks, Agents: agents, Decisions: decisions, Events: events,
		Channels: channels, ProviderStatus: providerStatus, Now: time.Now().UTC().UnixMilli(),
	})
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && !slices.Contains(domain.TaskStatuses, status) {
		s.writeError(w, fmt.Errorf("unknown task status %q: %w", status, domain.ErrInvalidInput))
		return
	}
	tasks, err := s.store.ListTasks(r.Context())
	if err != nil {
		s.writeError(w, err)
		return
	}
	tasks = filterTasksForServiceIdentity(r, tasks)
	if status != "" {
		tasks = slices.DeleteFunc(tasks, func(task domain.Task) bool { return task.Status != status })
		if status == domain.TaskReady {
			eligible := make([]domain.Task, 0, len(tasks))
			for _, task := range tasks {
				ready, err := s.store.TaskDependenciesSatisfied(r.Context(), task.ID)
				if err != nil {
					s.writeError(w, err)
					return
				}
				if ready && !task.Archived {
					eligible = append(eligible, task)
				}
			}
			tasks = eligible
		}
	}
	writeJSON(w, http.StatusOK, tasks)
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
		store.RunGuard
		LeaseSeconds int `json:"lease_seconds"`
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
	mutation.Guard = &input.RunGuard
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
		store.RunGuard
		Plan                 string   `json:"plan"`
		ContextDigest        string   `json:"context_digest"`
		RiskClass            string   `json:"risk_class"`
		VerificationStrategy string   `json:"verification_strategy"`
		ExpectedSideEffects  []string `json:"expected_side_effects"`
		RollbackApproach     string   `json:"rollback_approach"`
		SourceRevision       string   `json:"source_revision"`
		PolicyVersion        string   `json:"policy_version"`
		RequireApproval      bool     `json:"require_approval"`
		DecisionExpiresAt    *int64   `json:"decision_expires_at"`
		DecisionEvidence     string   `json:"decision_evidence"`
		EstimatedCostMinor   int64    `json:"estimated_cost_minor"`
		Currency             string   `json:"currency"`
		WaitingConsequence   string   `json:"waiting_consequence"`
		RejectingConsequence string   `json:"rejecting_consequence"`
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
	mutation.Guard = &input.RunGuard
	var revision domain.PlanRevision
	var decision *domain.Decision
	if strings.TrimSpace(input.ContextDigest) == "" && strings.TrimSpace(input.RiskClass) == "" &&
		strings.TrimSpace(input.VerificationStrategy) == "" && strings.TrimSpace(input.RollbackApproach) == "" {
		decision, err = s.store.SubmitPlan(r.Context(), mutation, r.PathValue("id"), input.Plan, input.RequireApproval)
		if err == nil {
			revisions, listErr := s.store.ListPlanRevisions(r.Context(), r.PathValue("id"))
			if listErr != nil {
				err = listErr
			} else if len(revisions) > 0 {
				revision = revisions[len(revisions)-1]
			}
		}
	} else {
		revision, decision, err = s.store.SubmitPlanRevision(
			r.Context(), mutation, r.PathValue("id"), domain.PlanRevisionInput{
				Plan: input.Plan, ContextDigest: input.ContextDigest, RiskClass: input.RiskClass,
				VerificationStrategy: input.VerificationStrategy, ExpectedSideEffects: input.ExpectedSideEffects,
				RollbackApproach: input.RollbackApproach, SourceRevision: input.SourceRevision,
				PolicyVersion: input.PolicyVersion,
			}, domain.DecisionRequest{
				Required: input.RequireApproval, ExpiresAt: input.DecisionExpiresAt, Risk: input.RiskClass,
				EvidenceSummary: input.DecisionEvidence, EstimatedCostMinor: input.EstimatedCostMinor,
				Currency: input.Currency, WaitingConsequence: input.WaitingConsequence,
				RejectingConsequence: input.RejectingConsequence,
			},
		)
	}
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan_revision": revision, "decision": decision})
}

func (s *Server) requestEngineQuestion(w http.ResponseWriter, r *http.Request) {
	if !s.requireRunProject(w, r, r.PathValue("id")) {
		return
	}
	var input struct {
		store.RunGuard
		domain.EngineQuestion
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
	mutation.Guard = &input.RunGuard
	decision, err := s.store.RequestEngineQuestion(r.Context(), mutation, r.PathValue("id"), input.EngineQuestion)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, decision)
}

func (s *Server) reportProgress(w http.ResponseWriter, r *http.Request) {
	if !s.requireRunProject(w, r, r.PathValue("id")) {
		return
	}
	var input struct {
		store.RunGuard
		Message string         `json:"message"`
		Payload map[string]any `json:"payload"`
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
	mutation.Guard = &input.RunGuard
	if err := s.store.ReportProgress(
		r.Context(), mutation, r.PathValue("id"), input.Message, input.Payload,
	); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) runTransition(transition func(context.Context, store.Mutation, string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireRunProject(w, r, r.PathValue("id")) {
			return
		}
		guard, err := readRunGuard(r)
		if err != nil {
			s.writeError(w, err)
			return
		}
		mutation, err := mutationFromRequest(r)
		if err != nil {
			s.writeError(w, err)
			return
		}
		mutation.Guard = guard
		if err := transition(r.Context(), mutation, r.PathValue("id")); err != nil {
			s.writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) completeRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireRunProject(w, r, r.PathValue("id")) {
		return
	}
	var input struct {
		store.RunGuard
		Summary     string   `json:"summary"`
		Evidence    string   `json:"evidence"`
		EvidenceIDs []string `json:"evidence_ids"`
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
	mutation.Guard = &input.RunGuard
	if len(input.EvidenceIDs) == 0 {
		s.writeError(w, fmt.Errorf("evidence_ids must reference current structured evidence; Markdown evidence is no longer a completion gate: %w", domain.ErrInvalidInput))
		return
	}
	if err := s.store.CompleteRunWithEvidence(
		r.Context(), mutation, r.PathValue("id"), input.Summary, input.EvidenceIDs,
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
		store.RunGuard
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
	mutation, err := mutationFromRequest(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	mutation.Guard = &input.RunGuard
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
	events, err := s.store.ListEvents(r.Context(), limit, afterID, serviceProjectScope(r))
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
		missed, err := s.store.ListEvents(r.Context(), 500, lastEventID, serviceProjectScope(r))
		if err == nil {
			for _, event := range missed {
				if err := writeSSE(w, event); err != nil {
					return
				}
			}
		}
	}
	fmt.Fprint(w, ": deck event stream\n\n")
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

func (s *Server) requireOperator(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if s.operatorAuthenticator == nil {
			ctx := context.WithValue(r.Context(), operatorSessionContextKey{}, authenticatedOperatorSession{
				OperatorID: "local",
			})
			next(w, r.WithContext(ctx))
			return
		}
		if !operatorOriginAllowed(r) {
			s.writeAPIError(w, http.StatusForbidden, errorCodeForbidden, "same-origin operator request required", false)
			return
		}
		cookie, err := r.Cookie(operatorSessionCookieName)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			s.writeOperatorAuthError(w, ErrOperatorUnauthenticated)
			return
		}
		tokenHash := sha256Hex(cookie.Value)
		stored, err := s.store.GetOperatorSession(r.Context(), tokenHash, time.Now().UTC().UnixMilli())
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				s.clearOperatorCookies(w)
				s.writeOperatorAuthError(w, ErrOperatorUnauthenticated)
				return
			}
			s.logger.Error("read operator session", "error", err)
			s.writeAPIError(w, http.StatusInternalServerError, errorCodeInternal, "internal server error", true)
			return
		}
		if !isSafeMethod(r.Method) {
			csrfCookie, cookieErr := r.Cookie(operatorCSRFCookieName)
			headers := r.Header.Values(operatorCSRFHeader)
			if cookieErr != nil || len(headers) != 1 || csrfCookie.Value == "" ||
				!constantTimeStringEqual(csrfCookie.Value, strings.TrimSpace(headers[0])) ||
				!constantTimeStringEqual(sha256Hex(csrfCookie.Value), stored.CSRFHash) {
				s.writeAPIError(w, http.StatusForbidden, errorCodeForbidden, "valid CSRF token required", false)
				return
			}
		}
		ctx := context.WithValue(r.Context(), operatorSessionContextKey{}, authenticatedOperatorSession{
			OperatorID: stored.OperatorID, TokenHash: stored.TokenHash, ExpiresAt: stored.ExpiresAt,
		})
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) writeOperatorAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrOperatorUnauthenticated):
		s.writeAPIError(w, http.StatusUnauthorized, errorCodeUnauthenticated, "operator authentication required", false)
	case errors.Is(err, ErrOperatorAuthUnavailable):
		s.logger.Error("operator authentication policy unavailable", "error", err)
		s.writeAPIError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "operator authentication is unavailable", true)
	default:
		s.logger.Error("operator authentication failed", "error", err)
		s.writeAPIError(w, http.StatusInternalServerError, errorCodeInternal, "internal server error", true)
	}
}

func operatorSessionFromRequest(r *http.Request) (authenticatedOperatorSession, bool) {
	session, ok := r.Context().Value(operatorSessionContextKey{}).(authenticatedOperatorSession)
	return session, ok
}

func operatorIDFromRequest(r *http.Request) string {
	if session, ok := operatorSessionFromRequest(r); ok && session.OperatorID != "" {
		return session.OperatorID
	}
	return "local"
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func operatorOriginAllowed(r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func newOperatorSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

// fallbackServiceActorID attributes service-route writes when service
// authentication is not configured. Claims and idempotency records are keyed
// on it, so it must stay stable.
const fallbackServiceActorID = "local-prototype"

func serviceActorID(r *http.Request) string {
	if identity, ok := serviceIdentityFromRequest(r); ok {
		return "service:" + identity.ID
	}
	return fallbackServiceActorID
}

func mutationFromRequest(r *http.Request) (store.Mutation, error) {
	actorID := serviceActorID(r)
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
	var guard store.RunGuard
	if err := readJSON(r, &guard); err != nil {
		return nil, err
	}
	if strings.TrimSpace(guard.AgentID) == "" || guard.ExpectedVersion < 1 || guard.FenceToken < 1 {
		return nil, fmt.Errorf("agent_id, expected_version, and fence_token are required: %w", domain.ErrInvalidInput)
	}
	return &guard, nil
}

func (s *Server) writeServiceAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrServiceUnauthenticated):
		w.Header().Set("WWW-Authenticate", `Bearer realm="deck"`)
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

// serviceProjectScope returns nil when the request has no service identity.
func serviceProjectScope(r *http.Request) []string {
	if identity, ok := serviceIdentityFromRequest(r); ok {
		return identity.Projects()
	}
	return nil
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
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, Last-Event-ID, X-CSRF-Token, X-Request-ID")
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
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid request body: %w: %v", domain.ErrInvalidInput, err)
	}
	return nil
}

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
