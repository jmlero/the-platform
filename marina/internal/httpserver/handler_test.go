package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type checkerStub struct {
	err   error
	calls int
}

func (checker *checkerStub) Health(context.Context) error {
	checker.calls++
	return checker.err
}

func TestHealthIsLiveWithoutDependency(t *testing.T) {
	checker := &checkerStub{err: errors.New("unavailable")}
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	New(Dependencies{WorkControl: checker, Journal: &checkerStub{}}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if checker.calls != 0 {
		t.Fatalf("liveness called dependency %d times", checker.calls)
	}
}

func TestReadyReflectsDependency(t *testing.T) {
	tests := []struct {
		name         string
		dependencies Dependencies
		wantStatus   int
		wantBodies   []string
	}{
		{
			name: "available",
			dependencies: Dependencies{
				WorkControl: &checkerStub{},
				Journal:     &checkerStub{},
			},
			wantStatus: http.StatusOK,
			wantBodies: []string{`"work_control":"available"`, `"journal":"available"`},
		},
		{
			name: "work control unavailable",
			dependencies: Dependencies{
				WorkControl: &checkerStub{err: errors.New("connection failed")},
				Journal:     &checkerStub{},
			},
			wantStatus: http.StatusServiceUnavailable,
			wantBodies: []string{`"work_control":"unavailable"`, `"journal":"available"`},
		},
		{
			name: "journal unavailable",
			dependencies: Dependencies{
				WorkControl: &checkerStub{},
				Journal:     &checkerStub{err: errors.New("database failed")},
			},
			wantStatus: http.StatusServiceUnavailable,
			wantBodies: []string{`"work_control":"available"`, `"journal":"unavailable"`},
		},
		{
			name:         "not configured",
			dependencies: Dependencies{},
			wantStatus:   http.StatusServiceUnavailable,
			wantBodies:   []string{`"work_control":"not_configured"`, `"journal":"not_configured"`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			response := httptest.NewRecorder()

			New(test.dependencies).ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			body := response.Body.String()
			for _, wantBody := range test.wantBodies {
				if !contains(body, wantBody) {
					t.Fatalf("body = %q, want substring %q", body, wantBody)
				}
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
		})
	}
}

func contains(value string, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}
