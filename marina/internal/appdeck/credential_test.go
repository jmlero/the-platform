package appdeck

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	clientTokenOne = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	clientTokenTwo = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
)

func TestClientReloadsBearerTokenWithoutPersistingIt(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "appdeck-token")
	if err := os.WriteFile(tokenPath, []byte(clientTokenOne+"\n"), 0o600); err != nil {
		t.Fatalf("write initial token: %v", err)
	}
	seen := make([]string, 0, 2)
	providerTime := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		seen = append(seen, request.Header.Get("Authorization"))
		return jsonResponse(http.StatusOK, compatibleProviderInfo(providerTime)), nil
	})}
	client, err := New("https://appdeck.test", httpClient, WithBearerTokenFile(tokenPath))
	if err != nil {
		t.Fatalf("create authenticated client: %v", err)
	}
	if _, err := client.GetProviderInfo(context.Background()); err != nil {
		t.Fatalf("request with initial token: %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte(clientTokenTwo+"\n"), 0o600); err != nil {
		t.Fatalf("rotate token: %v", err)
	}
	if _, err := client.GetProviderInfo(context.Background()); err != nil {
		t.Fatalf("request with rotated token: %v", err)
	}
	want := []string{"Bearer " + clientTokenOne, "Bearer " + clientTokenTwo}
	if len(seen) != len(want) || seen[0] != want[0] || seen[1] != want[1] {
		t.Fatalf("authorization headers = %#v, want rotated values", seen)
	}
}

func TestClientRejectsBearerTokenOverNonLoopbackHTTP(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "appdeck-token")
	if err := os.WriteFile(tokenPath, []byte(clientTokenOne+"\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	_, err := New("http://appdeck.internal", nil, WithBearerTokenFile(tokenPath))
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("configuration error = %v, want ErrInvalidConfiguration", err)
	}
	if _, err := New("http://127.0.0.1:8080", nil, WithBearerTokenFile(tokenPath)); err != nil {
		t.Fatalf("loopback development URL rejected: %v", err)
	}
}

func TestCredentialedClientDoesNotFollowRedirects(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "appdeck-token")
	if err := os.WriteFile(tokenPath, []byte(clientTokenOne+"\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls > 1 {
			t.Fatalf("credentialed client followed redirect to %s", request.URL)
		}
		return &http.Response{
			StatusCode: http.StatusTemporaryRedirect,
			Header:     http.Header{"Location": []string{"http://appdeck.test/api/v1/provider"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})}
	client, err := New("https://appdeck.test", httpClient, WithBearerTokenFile(tokenPath))
	if err != nil {
		t.Fatalf("create authenticated client: %v", err)
	}
	_, err = client.GetProviderInfo(context.Background())
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("redirect error = %v, want HTTP %d APIError", err, http.StatusTemporaryRedirect)
	}
	if calls != 1 {
		t.Fatalf("request calls = %d, want 1", calls)
	}
}
