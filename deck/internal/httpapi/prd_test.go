package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPRDApprovalCannotUseServiceBearerOrSkipOperatorSession(t *testing.T) {
	_, local := newTestStoreAndHandler(t)
	_, authenticated, _ := newOperatorAuthTestHandler(t)
	for _, tc := range []struct {
		name    string
		handler http.Handler
		token   string
		status  int
	}{
		{"local service bearer", local, "service-token", http.StatusForbidden},
		{"missing operator session", authenticated, "", http.StatusUnauthorized},
		{"service token is not an operator session", authenticated, "service-token", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{"/api/v1/operator/messages/msg/prd/approve", "/api/v1/operator/messages/msg/prd/tasks/approve", "/api/v1/operator/tasks/tsk/prd/accept"} {
				request := httptest.NewRequest("POST", path, strings.NewReader(`{"digest":"draft","profile_digest":"profile","budget":{}}`))
				request.Header.Set("Idempotency-Key", "approval")
				if tc.token != "" {
					request.Header.Set("Authorization", "Bearer "+tc.token)
				}
				response := httptest.NewRecorder()
				tc.handler.ServeHTTP(response, request)
				if response.Code != tc.status {
					t.Fatalf("%d: %s", response.Code, response.Body.String())
				}
			}
		})
	}
}
