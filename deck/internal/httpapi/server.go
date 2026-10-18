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
	"strconv"
	"strings"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/marina"
	"github.com/jmlero/the-platform/deck/internal/store"
)

type Server struct {
	store                 *store.Store
	logger                *slog.Logger
	operatorAuthenticator *FileOperatorAuthenticator
	secureOperatorCookies bool
	operatorSessionTTL    time.Duration
	marina                *marina.Client
}

type Option func(*Server)

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

// WithMarina forwards operator answers, cancellations and evidence downloads
// to Marina, which executes Deck's work.
func WithMarina(client *marina.Client) Option {
	return func(server *Server) {
		server.marina = client
	}
}

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

// Handler serves the operator API. Deck exposes no service API: it calls
// Marina, and Marina never calls Deck.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.contextRoutes(mux)
	s.workloadRoutes(mux)
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.readiness)
	mux.HandleFunc("POST /api/v1/operator/session", s.createOperatorSession)
	mux.HandleFunc("GET /api/v1/operator/session", s.requireOperator(s.getOperatorSession))
	mux.HandleFunc("DELETE /api/v1/operator/session", s.requireOperator(s.deleteOperatorSession))
	mux.HandleFunc("POST /api/v1/operator/tasks/{id}/accept", s.requireOperator(s.acceptTask))
	mux.HandleFunc("PUT /api/v1/operator/tasks/{id}/dependencies", s.requireOperator(s.setTaskDependencies))
	mux.HandleFunc("GET /api/v1/operator/projects", s.requireOperator(s.projectChannels))
	mux.HandleFunc("POST /api/v1/operator/projects", s.requireOperator(s.createProjectChannel))
	mux.HandleFunc("GET /api/v1/bootstrap", s.requireOperator(s.bootstrap))
	mux.HandleFunc("POST /api/v1/tasks", s.requireOperator(s.createTask))
	mux.HandleFunc("PATCH /api/v1/tasks/{id}", s.requireOperator(s.updateTask))
	mux.HandleFunc("POST /api/v1/tasks/{id}/move", s.requireOperator(s.moveTask))
	mux.HandleFunc("GET /api/v1/agents", s.requireOperator(s.listAgents))
	mux.HandleFunc("GET /api/v1/operator/threads/{id}/questions", s.requireOperator(s.listConversationQuestions))
	mux.HandleFunc("POST /api/v1/operator/conversation/questions/{id}/answer", s.requireOperator(s.answerConversationQuestion))
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

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if projectID == "" {
		projectID = "prj_default"
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
	var input struct {
		ThreadID   string                     `json:"thread_id"`
		Body       string                     `json:"body"`
		References domain.CanonicalReferences `json:"references"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	message, err := s.store.PostChannelMessage(r.Context(), domain.ChannelMessageInput{
		ChannelID: channel.ID, ThreadID: input.ThreadID, AuthorType: "human", AuthorID: "operator:" + operatorIDFromRequest(r),
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

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "service": "deck", "time": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	ready := true
	checks := map[string]string{
		"store":         "available",
		"operator_auth": "disabled",
		"marina":        "disabled",
	}
	if err := s.store.Health(r.Context()); err != nil {
		ready = false
		checks["store"] = "unavailable"
	}
	// Deck serves its UI without Marina; reachability is in the provider status.
	if s.marina != nil {
		checks["marina"] = "configured"
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
		Channels: channels, ProviderStatus: s.executorStatus(providerStatus), Now: time.Now().UTC().UnixMilli(),
	})
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
	detail.ProviderStatus = s.executorStatus(detail.ProviderStatus)
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

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.store.ListAgents(r.Context())
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agents)
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
		missed, err := s.store.ListEvents(r.Context(), 500, lastEventID, nil)
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
