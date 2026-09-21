package worker

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// RunRemote is the container entry point. Its only credential is scoped to one
// attempt; the authority watchdog kills the entire child process group on loss.
func RunRemote(ctx context.Context) error {
	base := strings.TrimRight(os.Getenv("MARINA_WORKER_URL"), "/")
	id := os.Getenv("MARINA_ATTEMPT_ID")
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) || strings.ContainsAny(id, "/\\. ") || id == "" {
		return errors.New("valid worker gateway and attempt ID required")
	}
	tokenBytes, err := os.ReadFile(os.Getenv("MARINA_WORKER_TOKEN_FILE"))
	if err != nil {
		return err
	}
	token := strings.TrimSpace(string(tokenBytes))
	if len(token) < 32 {
		return errors.New("invalid attempt credential")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	ca := os.Getenv("MARINA_WORKER_CA_FILE")
	if ca != "" {
		b, err := os.ReadFile(ca)
		if err != nil {
			return err
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(b) {
			return errors.New("invalid gateway CA")
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	}
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(ctx context.Context, method, path string, body []byte) ([]byte, error) {
		r, err := http.NewRequestWithContext(ctx, method, base+"/workers/"+id+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		response, err := client.Do(r)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return nil, gatewayStatusError{response.StatusCode}
		}
		return io.ReadAll(io.LimitReader(response.Body, MaxResultBytes+1))
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Linux enforces this per real UID; workers run as a dedicated unprivileged UID.
	limit := unix.Rlimit{Cur: 256, Max: 256}
	if err = unix.Setrlimit(unix.RLIMIT_NPROC, &limit); err != nil {
		return fmt.Errorf("enforce process limit: %w", err)
	}
	checkAuthority := func(ctx context.Context) (time.Time, error) {
		b, err := request(ctx, "GET", "/authority", nil)
		if err != nil {
			return time.Time{}, err
		}
		var authority struct {
			ExpiresAt time.Time `json:"expires_at"`
		}
		if err := json.Unmarshal(b, &authority); err != nil {
			return time.Time{}, err
		}
		return authority.ExpiresAt, nil
	}
	expires, err := checkAuthority(ctx)
	if err != nil {
		return err
	}
	if !time.Now().Add(8 * time.Second).Before(expires) {
		return errors.New("worker authority expired")
	}
	b, err := request(ctx, "GET", "/input", nil)
	if err != nil {
		return err
	}
	var input Input
	if err = json.Unmarshal(b, &input); err != nil {
		return err
	}
	input.GitAuthorization = token
	if input.Stage == "implement" {
		input.Environment = append(input.Environment, "MARINA_WORKER_TOKEN="+token)
	}
	if ca != "" {
		input.Environment = append(input.Environment, "SSL_CERT_FILE="+ca, "NODE_EXTRA_CA_CERTS="+ca, "GIT_SSL_CAINFO="+ca)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = watchAuthority(ctx, expires, 8*time.Second, 3*time.Second, checkAuthority)
		cancel()
	}()
	defer func() { cancel(); <-done }()
	result, err := Run(ctx, input, "/work/attempt")
	if err != nil {
		return err
	}
	result.Summary = strings.ReplaceAll(result.Summary, token, "[redacted]")
	b, err = json.Marshal(result)
	if err != nil {
		return err
	}
	if len(b) > MaxResultBytes {
		return errors.New("worker result exceeds transfer limit")
	}
	for retry := 0; retry < 3; retry++ {
		if _, err = request(ctx, "POST", "/result", b); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return err
}
