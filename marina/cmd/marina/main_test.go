package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

func TestDoctorCommand(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "appdeck-token")
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0o600); err != nil {
		t.Fatalf("write AppDeck token: %v", err)
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
			"provider":"appdeck",
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
		"--appdeck-url", "https://appdeck.test",
		"--appdeck-token-file", tokenPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "AppDeck: available\n" {
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

func TestServeRejectsIncompleteOrUnreadableTLSBeforeContactingAppDeck(t *testing.T) {
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
