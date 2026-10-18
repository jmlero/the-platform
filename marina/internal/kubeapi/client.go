// Package kubeapi is a small Kubernetes REST client for the platform API:
// identity reviews, Marina's custom resources and Secrets. It uses the
// in-cluster service account and no client library.
package kubeapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	ServerURL string `json:"server_url"`
	TokenFile string `json:"token_file"`
	CAFile    string `json:"ca_file"`
}

// InCluster is the standard service account configuration.
func InCluster() Config {
	return Config{ServerURL: "https://kubernetes.default.svc", TokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
		CAFile: "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"}
}

type Client struct {
	config Config
	http   *http.Client
}

// StatusError is a non-2xx response; Reason carries the Kubernetes Status reason.
type StatusError struct {
	Code    int
	Reason  string
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("Kubernetes API returned %d %s", e.Code, e.Reason)
}

// IsStatus reports whether err is a Kubernetes response with this HTTP code.
func IsStatus(err error, code int) bool {
	var status *StatusError
	return errors.As(err, &status) && status.Code == code
}

func New(config Config, client *http.Client) (*Client, error) {
	u, err := url.Parse(config.ServerURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" ||
		(u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		return nil, errors.New("the Kubernetes API needs an HTTPS endpoint (loopback HTTP is allowed for tests)")
	}
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if config.CAFile != "" {
			pem, err := os.ReadFile(config.CAFile)
			if err != nil {
				return nil, err
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, errors.New("invalid Kubernetes CA")
			}
			transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
		}
		client = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	}
	copied := *client
	copied.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{config: config, http: &copied}, nil
}

// Do sends one request and decodes a 2xx JSON response into result.
func (c *Client) Do(ctx context.Context, method, path, contentType string, body, result any) error {
	response, err := c.open(ctx, method, path, contentType, body)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if result != nil {
		if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(result); err != nil {
			return fmt.Errorf("decode Kubernetes response: %w", err)
		}
	}
	return nil
}

// Stream opens a long-lived request such as a watch; the caller closes the body.
func (c *Client) Stream(ctx context.Context, path string) (io.ReadCloser, error) {
	response, err := c.open(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

func (c *Client) open(ctx context.Context, method, path, contentType string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.config.ServerURL, "/")+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		if contentType == "" {
			contentType = "application/json"
		}
		request.Header.Set("Content-Type", contentType)
	}
	request.Header.Set("Accept", "application/json")
	if c.config.TokenFile != "" {
		token, err := os.ReadFile(c.config.TokenFile)
		if err != nil {
			return nil, errors.New("read Kubernetes credential")
		}
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		defer response.Body.Close()
		var status struct{ Reason, Message string }
		_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&status)
		return nil, &StatusError{Code: response.StatusCode, Reason: status.Reason, Message: status.Message}
	}
	return response, nil
}
