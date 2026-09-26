package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDiagnosticsCommandExportsRedactedJournalView(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "marina.db")
	if code := run([]string{"diagnostics", "--journal-path", path}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	var exported struct {
		SchemaVersion string         `json:"schema_version"`
		Counts        map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &exported); err != nil {
		t.Fatalf("decode diagnostics: %v\n%s", err, stdout.String())
	}
	if exported.SchemaVersion != "marina-diagnostics/v1" || exported.Counts["executions"] != 0 {
		t.Fatalf("unexpected diagnostics: %#v", exported)
	}
}

func TestVersionCommand(t *testing.T) {
	previousVersion := version
	version = "test-version"
	t.Cleanup(func() { version = previousVersion })

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "test-version\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestPreviousProviderEnvironmentFailsBeforeStarting(t *testing.T) {
	t.Setenv("MARINA_APPDECK_TOKEN_FILE", "private-value")
	var stdout, stderr bytes.Buffer
	code := run([]string{"serve", "--help"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "MARINA_DECK_TOKEN_FILE") || strings.Contains(stderr.String(), "private-value") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
}

func TestDoctorCommand(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "deck-token")
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0o600); err != nil {
		t.Fatalf("write Deck token: %v", err)
	}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/provider" {
			return textResponse(http.StatusNotFound, `{"error":"unexpected route"}`), nil
		}
		if request.Header.Get("X-Request-ID") == "" {
			t.Error("doctor request correlation ID is empty")
		}
		if request.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("doctor Authorization = %q", request.Header.Get("Authorization"))
		}
		return textResponse(http.StatusOK, `{
			"provider":"deck",
			"protocol_version":"1.0",
			"capabilities":["provider.info","agent.register","work.ready.list","work.inspect","work.claim.atomic","work.claim.lookup","run.assigned.list","run.lease.renew","run.block","command.list","command.ack","event.append"],
			"server_time":1784368800000,
			"ready":true,
			"readiness":{"store":"available"}
		}`), nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"doctor",
		"--deck-url", "https://deck.test",
		"--deck-token-file", tokenPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "Deck: available\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func textResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"unknown"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestEnvironmentDurationIsValidated(t *testing.T) {
	t.Setenv("MARINA_TEST_DURATION", "not-a-duration")
	if _, err := envDurationOrDefault("MARINA_TEST_DURATION", time.Minute); err == nil {
		t.Fatal("invalid environment duration was accepted")
	}

	t.Setenv("MARINA_TEST_DURATION", "45s")
	value, err := envDurationOrDefault("MARINA_TEST_DURATION", time.Minute)
	if err != nil {
		t.Fatalf("parse duration: %v", err)
	}
	if value != 45*time.Second {
		t.Fatalf("duration = %s, want 45s", value)
	}
}

func TestServeRejectsIncompleteOrUnreadableTLSBeforeContactingDeck(t *testing.T) {
	for _, arguments := range [][]string{
		{"--tls-cert-file", "missing.crt"},
		{"--tls-key-file", "missing.key"},
		{"--tls-cert-file", "missing.crt", "--tls-key-file", "missing.key"},
	} {
		t.Run(strings.Join(arguments, " "), func(t *testing.T) {
			var stderr bytes.Buffer
			if code := runServe(arguments, &stderr); code != 2 || !strings.Contains(stderr.String(), "configure server TLS") {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
			}
		})
	}
}

func TestShutdownDrainsAnActiveModelResponse(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	server.Start()
	defer server.Close()
	response := make(chan error, 1)
	go func() {
		result, err := server.Client().Get(server.URL)
		if err == nil {
			defer result.Body.Close()
			if result.StatusCode != http.StatusOK {
				err = fmt.Errorf("status = %d", result.StatusCode)
			}
		}
		response <- err
	}()
	<-started
	shutdown := make(chan error, 1)
	go func() { shutdown <- shutdownHTTPServer(server.Config) }()
	// A model stream can outlive the old 10-second grace. The release must
	// complete without losing its response after shutdown starts.
	time.Sleep(11 * time.Second)
	close(release)
	if err := <-shutdown; err != nil {
		t.Fatalf("shutdown interrupted active response: %v", err)
	}
	if err := <-response; err != nil {
		t.Fatalf("active response failed: %v", err)
	}
}

func TestShutdownKeepsGatewayOpenUntilActiveControllerFinishes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	var controllers sync.WaitGroup
	controllers.Add(1)
	shutdown := make(chan error, 1)
	go func() { shutdown <- drainControllersAndShutdown(server.Config, &controllers) }()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatalf("gateway closed while active controller still needed it: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("gateway status = %d", response.StatusCode)
	}
	controllers.Done()
	if err := <-shutdown; err != nil {
		t.Fatalf("shutdown after controller completed: %v", err)
	}
}

func TestConversationCycleSurvivesTerminationSignal(t *testing.T) {
	signalContext, signalDone := context.WithCancel(t.Context())
	cycleContext, cycleDone := conversationCycleContext(signalContext)
	defer cycleDone()
	signalDone()
	if cycleContext.Err() != nil {
		t.Fatal("termination signal interrupted the active conversation cycle")
	}
	cycleDone()
	if cycleContext.Err() != context.Canceled {
		t.Fatalf("cycle was not bounded by its own cancellation: %v", cycleContext.Err())
	}
}
