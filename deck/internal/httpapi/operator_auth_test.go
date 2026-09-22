package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/store"
)

const (
	operatorAuthToken    = "operator-auth-token-with-at-least-32-bytes"
	operatorAuthTokenTwo = "rotated-operator-token-with-at-least-32-bytes"
)

func TestFileOperatorAuthenticatorReloadsRotationAndFailsClosed(t *testing.T) {
	policyPath := filepath.Join(t.TempDir(), "operator-auth.json")
	writeOperatorAuthPolicy(t, policyPath, operatorAuthToken)
	authenticator, err := NewFileOperatorAuthenticator(policyPath)
	if err != nil {
		t.Fatalf("create operator authenticator: %v", err)
	}
	identity, err := authenticator.Authenticate(operatorAuthToken)
	if err != nil || identity.ID != "operator-primary" {
		t.Fatalf("initial identity = %#v, error = %v", identity, err)
	}

	writeOperatorAuthPolicy(t, policyPath, operatorAuthTokenTwo)
	if _, err := authenticator.Authenticate(operatorAuthToken); err != ErrOperatorUnauthenticated {
		t.Fatalf("retired token error = %v", err)
	}
	identity, err = authenticator.Authenticate(operatorAuthTokenTwo)
	if err != nil || identity.ID != "operator-primary" {
		t.Fatalf("rotated identity = %#v, error = %v", identity, err)
	}

	if err := os.WriteFile(policyPath, []byte(`{"version":1,"operators":[]}`), 0o600); err != nil {
		t.Fatalf("replace operator policy: %v", err)
	}
	if _, err := authenticator.Authenticate(operatorAuthTokenTwo); err == nil ||
		!strings.Contains(err.Error(), ErrOperatorAuthUnavailable.Error()) {
		t.Fatalf("malformed policy error = %v", err)
	}
}

func TestOperatorAuthenticationProtectsEveryHumanRoute(t *testing.T) {
	_, handler, _ := newOperatorAuthTestHandler(t)
	routes := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/bootstrap", ""},
		{http.MethodGet, "/api/v1/operator/provider", ""},
		{http.MethodGet, "/api/v1/operator/projects/prj_default/workloads", ""},
		{http.MethodPost, "/api/v1/operator/projects/prj_default/workloads/invoke", `{}`},
		{http.MethodPost, "/api/v1/tasks", `{}`},
		{http.MethodPatch, "/api/v1/tasks/tsk_missing", `{}`},
		{http.MethodPost, "/api/v1/tasks/tsk_missing/move", `{}`},
		{http.MethodGet, "/api/v1/agents", ""},
		{http.MethodGet, "/api/v1/operator/tasks/tsk_missing", ""},
		{http.MethodPost, "/api/v1/operator/decisions/dec_missing/answer", `{}`},
		{http.MethodPost, "/api/v1/operator/runs/run_missing/commands", `{}`},
		{http.MethodGet, "/api/v1/operator/events/stream", ""},
		{http.MethodGet, "/api/v1/operator/channels", ""},
		{http.MethodPost, "/api/v1/operator/channels", `{}`},
		{http.MethodGet, "/api/v1/operator/channels/chn_missing/messages", ""},
		{http.MethodPost, "/api/v1/operator/channels/chn_missing/messages", `{}`},
		{http.MethodPost, "/api/v1/operator/messages/msg_missing/promote", `{}`},
		{http.MethodGet, "/api/v1/operator/export", ""},
		{http.MethodPost, "/api/v1/operator/import", `{}`},
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			request := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertAPIError(t, response, http.StatusUnauthorized, errorCodeUnauthenticated)
		})
	}
}

func TestOperatorSessionLoginCSRFAndLogout(t *testing.T) {
	_, handler, _ := newOperatorAuthTestHandler(t)

	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(
		http.MethodPost, "/api/v1/operator/session", strings.NewReader(`{"token":"wrong-token-that-is-long-enough-to-submit"}`),
	))
	assertAPIError(t, invalid, http.StatusUnauthorized, errorCodeUnauthenticated)
	if len(invalid.Result().Cookies()) != 0 {
		t.Fatal("invalid login set cookies")
	}

	loginRequest := httptest.NewRequest(
		http.MethodPost, "/api/v1/operator/session", strings.NewReader(`{"token":"`+operatorAuthToken+`"}`),
	)
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusCreated {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}
	var session operatorSessionResponse
	if err := json.NewDecoder(login.Body).Decode(&session); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if session.OperatorID != "operator-primary" || !session.Authenticated || session.ExpiresAt <= 0 {
		t.Fatalf("login response = %#v", session)
	}
	if login.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("login Cache-Control = %q", login.Header().Get("Cache-Control"))
	}
	cookies := cookieMap(login.Result().Cookies())
	sessionCookie := cookies[operatorSessionCookieName]
	csrfCookie := cookies[operatorCSRFCookieName]
	if sessionCookie == nil || sessionCookie.Value == "" || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie = %#v", sessionCookie)
	}
	if csrfCookie == nil || csrfCookie.Value == "" || csrfCookie.HttpOnly || csrfCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("CSRF cookie = %#v", csrfCookie)
	}

	bootstrapRequest := httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil)
	bootstrapRequest.AddCookie(sessionCookie)
	bootstrap := httptest.NewRecorder()
	handler.ServeHTTP(bootstrap, bootstrapRequest)
	if bootstrap.Code != http.StatusOK {
		t.Fatalf("authenticated bootstrap status = %d, body = %s", bootstrap.Code, bootstrap.Body.String())
	}

	crossOriginRequest := httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil)
	crossOriginRequest.AddCookie(sessionCookie)
	crossOriginRequest.Header.Set("Origin", "http://localhost:5173")
	crossOrigin := httptest.NewRecorder()
	handler.ServeHTTP(crossOrigin, crossOriginRequest)
	assertAPIError(t, crossOrigin, http.StatusForbidden, errorCodeForbidden)

	missingCSRFRequest := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(`{"title":"Protected task"}`))
	missingCSRFRequest.AddCookie(sessionCookie)
	missingCSRFRequest.AddCookie(csrfCookie)
	missingCSRF := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRF, missingCSRFRequest)
	assertAPIError(t, missingCSRF, http.StatusForbidden, errorCodeForbidden)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(`{"title":"Protected task"}`))
	createRequest.AddCookie(sessionCookie)
	createRequest.AddCookie(csrfCookie)
	createRequest.Header.Set(operatorCSRFHeader, csrfCookie.Value)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, createRequest)
	if created.Code != http.StatusCreated {
		t.Fatalf("CSRF-authenticated create status = %d, body = %s", created.Code, created.Body.String())
	}

	logoutRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/operator/session", nil)
	logoutRequest.AddCookie(sessionCookie)
	logoutRequest.AddCookie(csrfCookie)
	logoutRequest.Header.Set(operatorCSRFHeader, csrfCookie.Value)
	logout := httptest.NewRecorder()
	handler.ServeHTTP(logout, logoutRequest)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body = %s", logout.Code, logout.Body.String())
	}
	cleared := cookieMap(logout.Result().Cookies())
	if cleared[operatorSessionCookieName] == nil || cleared[operatorSessionCookieName].MaxAge >= 0 ||
		cleared[operatorCSRFCookieName] == nil || cleared[operatorCSRFCookieName].MaxAge >= 0 {
		t.Fatalf("logout cookies = %#v", cleared)
	}

	reusedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil)
	reusedRequest.AddCookie(sessionCookie)
	reused := httptest.NewRecorder()
	handler.ServeHTTP(reused, reusedRequest)
	assertAPIError(t, reused, http.StatusUnauthorized, errorCodeUnauthenticated)
}

func TestOperatorAuthenticationDoesNotChangeServiceAuthentication(t *testing.T) {
	_, handler, _ := newOperatorAuthTestHandler(t)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/provider", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("provider status = %d, want service behavior unchanged", response.Code)
	}
}

func TestReadinessReflectsOperatorAuthenticationPolicyHealth(t *testing.T) {
	_, handler, policyPath := newOperatorAuthTestHandler(t)

	healthy := httptest.NewRecorder()
	handler.ServeHTTP(healthy, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if healthy.Code != http.StatusOK {
		t.Fatalf("healthy readiness status = %d, body = %s", healthy.Code, healthy.Body.String())
	}
	var payload struct {
		Readiness map[string]string `json:"readiness"`
	}
	if err := json.NewDecoder(healthy.Body).Decode(&payload); err != nil {
		t.Fatalf("decode healthy readiness: %v", err)
	}
	if payload.Readiness["operator_auth"] != "available" {
		t.Fatalf("healthy operator readiness = %#v", payload.Readiness)
	}

	// The handler reloads its policy, so replacing the only matching file in its
	// temporary parent makes readiness fail closed without restarting Deck.
	if err := os.WriteFile(policyPath, []byte(`{"version":1,"operators":[]}`), 0o600); err != nil {
		t.Fatalf("replace operator policy: %v", err)
	}
	degraded := httptest.NewRecorder()
	handler.ServeHTTP(degraded, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if degraded.Code != http.StatusServiceUnavailable {
		t.Fatalf("degraded readiness status = %d, body = %s", degraded.Code, degraded.Body.String())
	}
	payload.Readiness = nil
	if err := json.NewDecoder(degraded.Body).Decode(&payload); err != nil {
		t.Fatalf("decode degraded readiness: %v", err)
	}
	if payload.Readiness["operator_auth"] != "unavailable" {
		t.Fatalf("degraded operator readiness = %#v", payload.Readiness)
	}
}

func newOperatorAuthTestHandler(t *testing.T) (*store.Store, http.Handler, string) {
	t.Helper()
	appStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "deck-operator-auth-test.db"))
	if err != nil {
		t.Fatalf("open Deck store: %v", err)
	}
	t.Cleanup(func() {
		if err := appStore.Close(); err != nil {
			t.Errorf("close Deck store: %v", err)
		}
	})
	policyPath := filepath.Join(t.TempDir(), "operator-auth.json")
	writeOperatorAuthPolicy(t, policyPath, operatorAuthToken)
	authenticator, err := NewFileOperatorAuthenticator(policyPath)
	if err != nil {
		t.Fatalf("create operator authenticator: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return appStore, New(
		appStore,
		logger,
		WithOperatorAuthenticator(authenticator),
		WithSecureOperatorCookies(false),
	).Handler(), policyPath
}

func writeOperatorAuthPolicy(t *testing.T, path string, token string) {
	t.Helper()
	digest := sha256.Sum256([]byte(token))
	policy := map[string]any{
		"version": 1,
		"operators": []map[string]any{{
			"id": "operator-primary", "token_sha256": []string{hex.EncodeToString(digest[:])},
		}},
	}
	payload, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("marshal operator auth policy: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write operator auth policy: %v", err)
	}
}

func cookieMap(cookies []*http.Cookie) map[string]*http.Cookie {
	result := make(map[string]*http.Cookie, len(cookies))
	for _, cookie := range cookies {
		result[cookie.Name] = cookie
	}
	return result
}
