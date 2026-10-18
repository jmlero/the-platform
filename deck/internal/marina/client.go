// Package marina is Deck's client of Marina's public marina.dev/v1 platform
// API, and the loop that submits Deck's ready work as Runs and Turns and
// mirrors their status back into Deck's ledger. Marina never calls Deck.
package marina

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	apiPrefix        = "/apis/marina.dev/v1"
	apiVersion       = "marina.dev/v1"
	maxResponseBytes = 4 << 20
	// MaxArtifactBytes bounds an evidence download proxied to the browser.
	MaxArtifactBytes = 4 << 20
	maxTokenBytes    = 64 << 10
	maxCABytes       = 1 << 20
)

var (
	namePattern   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// ValidName reports whether value is a Kubernetes-style object name.
func ValidName(value string) bool {
	return len(value) <= 63 && namePattern.MatchString(value)
}

// Digest returns the sha256:<hex> digest Marina expects for content.
func Digest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Config locates Marina. The token file is read for every request because
// projected service account tokens rotate.
type Config struct {
	URL       string
	TokenFile string
	CAFile    string
}

type Client struct {
	base      *url.URL
	tokenFile string
	http      *http.Client
}

// APIError is a Kubernetes Status returned by Marina. Its message is safe to
// log; handlers decide what reaches the browser.
type APIError struct {
	Code    int    `json:"code"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = http.StatusText(e.Code)
	}
	if e.Reason != "" {
		return fmt.Sprintf("Marina: %s (HTTP %d, %s)", message, e.Code, e.Reason)
	}
	return fmt.Sprintf("Marina: %s (HTTP %d)", message, e.Code)
}

func statusCode(err error) int {
	var status *APIError
	if errors.As(err, &status) {
		return status.Code
	}
	return 0
}

func IsNotFound(err error) bool { return statusCode(err) == http.StatusNotFound }

// IsAlreadyExists reports a create whose name exists with a different spec.
func IsAlreadyExists(err error) bool {
	var status *APIError
	return errors.As(err, &status) && status.Code == http.StatusConflict && status.Reason == "AlreadyExists"
}

// IsConflict reports a 409 answer or cancel Marina will not accept.
func IsConflict(err error) bool { return statusCode(err) == http.StatusConflict }

// IsRejected reports a request Marina refused for good: retrying the same
// request cannot succeed.
func IsRejected(err error) bool {
	code := statusCode(err)
	return code == http.StatusBadRequest || code == http.StatusUnprocessableEntity || code == http.StatusForbidden || IsAlreadyExists(err)
}

// New requires https outside loopback, never follows redirects and trusts the
// optional CA bundle in addition to the system roots.
func New(config Config) (*Client, error) {
	raw := strings.TrimSpace(config.URL)
	if raw == "" {
		return nil, errors.New("Marina URL is required")
	}
	base, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse Marina URL: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" || base.Host == "" {
		return nil, errors.New("Marina URL must be an http or https URL with a host")
	}
	if base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("Marina URL must not contain credentials, a query or a fragment")
	}
	if base.Scheme != "https" && !loopback(base.Hostname()) {
		return nil, errors.New("Marina requires https outside loopback")
	}
	if strings.TrimSpace(config.TokenFile) == "" {
		return nil, errors.New("a Marina token file is required")
	}
	base.Path = strings.TrimRight(base.Path, "/")
	base.RawPath = ""
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if strings.TrimSpace(config.CAFile) != "" {
		pool, err := certPool(config.CAFile)
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	return &Client{base: base, tokenFile: config.TokenFile, http: &http.Client{
		Transport: transport, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func loopback(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func certPool(path string) (*x509.CertPool, error) {
	contents, err := readBounded(path, maxCABytes)
	if err != nil {
		return nil, fmt.Errorf("read Marina CA file: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(contents) {
		return nil, errors.New("Marina CA file contains no PEM certificates")
	}
	return pool, nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	return contents, nil
}

func namespaced(namespace, resource string, parts ...string) string {
	path := apiPrefix + "/namespaces/" + url.PathEscape(namespace) + "/" + resource
	for _, part := range parts {
		path += "/" + url.PathEscape(part)
	}
	return path
}

func (c *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	var value Capabilities
	return value, c.do(ctx, http.MethodGet, apiPrefix+"/capabilities", "", nil, &value)
}

func (c *Client) Project(ctx context.Context, namespace, name string) (Project, error) {
	var value Project
	return value, c.do(ctx, http.MethodGet, namespaced(namespace, "projects", name), "", nil, &value)
}

func (c *Client) Workload(ctx context.Context, namespace, name string) (Workload, error) {
	var value Workload
	return value, c.do(ctx, http.MethodGet, namespaced(namespace, "workloads", name), "", nil, &value)
}

// CreateRun sends exactly the recorded spec, so a retry after a lost response
// is an exact replay that Marina answers with the original Run.
func (c *Client) CreateRun(ctx context.Context, namespace, name string, spec json.RawMessage) (Run, error) {
	var value Run
	return value, c.do(ctx, http.MethodPost, namespaced(namespace, "runs"), "", object("Run", name, spec), &value)
}

func (c *Client) Run(ctx context.Context, namespace, name string) (Run, error) {
	var value Run
	return value, c.do(ctx, http.MethodGet, namespaced(namespace, "runs", name), "", nil, &value)
}

func (c *Client) CancelRun(ctx context.Context, namespace, name, idempotencyKey string) error {
	var value Run
	return c.do(ctx, http.MethodPost, namespaced(namespace, "runs", name, "cancel"), idempotencyKey, nil, &value)
}

func (c *Client) RunEvents(ctx context.Context, namespace, name string, after int64) (EventPage, error) {
	var value EventPage
	path := namespaced(namespace, "runs", name, "events") + "?after=" + strconv.FormatInt(after, 10)
	return value, c.do(ctx, http.MethodGet, path, "", nil, &value)
}

// RunArtifact downloads one retained evidence file and checks its digest.
func (c *Client) RunArtifact(ctx context.Context, namespace, name, digest string) ([]byte, error) {
	if !digestPattern.MatchString(digest) {
		return nil, errors.New("invalid artifact digest")
	}
	request, err := c.request(ctx, http.MethodGet, namespaced(namespace, "runs", name, "artifacts", digest), "", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call Marina: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxArtifactBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Marina artifact: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, statusError(response.StatusCode, body)
	}
	if len(body) > MaxArtifactBytes || Digest(string(body)) != digest {
		return nil, errors.New("Marina artifact failed its size or digest check")
	}
	return body, nil
}

func (c *Client) Decision(ctx context.Context, namespace, name string) (Decision, error) {
	var value Decision
	return value, c.do(ctx, http.MethodGet, namespaced(namespace, "decisions", name), "", nil, &value)
}

func (c *Client) AnswerDecision(ctx context.Context, namespace, name, answer, idempotencyKey string) (Decision, error) {
	var value Decision
	return value, c.do(ctx, http.MethodPost, namespaced(namespace, "decisions", name, "answer"), idempotencyKey, map[string]string{"answer": answer}, &value)
}

func (c *Client) CreateTurn(ctx context.Context, namespace, name string, spec json.RawMessage) (Turn, error) {
	var value Turn
	return value, c.do(ctx, http.MethodPost, namespaced(namespace, "turns"), "", object("Turn", name, spec), &value)
}

func (c *Client) Turn(ctx context.Context, namespace, name string) (Turn, error) {
	var value Turn
	return value, c.do(ctx, http.MethodGet, namespaced(namespace, "turns", name), "", nil, &value)
}

func object(kind, name string, spec json.RawMessage) any {
	return struct {
		APIVersion string          `json:"apiVersion"`
		Kind       string          `json:"kind"`
		Metadata   ObjectMeta      `json:"metadata"`
		Spec       json.RawMessage `json:"spec"`
	}{apiVersion, kind, ObjectMeta{Name: name}, spec}
}

func (c *Client) request(ctx context.Context, method, path, idempotencyKey string, input any) (*http.Request, error) {
	token, err := readBounded(c.tokenFile, maxTokenBytes)
	if err != nil || strings.TrimSpace(string(token)) == "" {
		return nil, errors.New("Marina token file is unreadable or empty")
	}
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("encode Marina request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	resource, query, _ := strings.Cut(path, "?")
	endpoint := *c.base
	endpoint.Path += resource
	endpoint.RawQuery = query
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create Marina request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "deck")
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return request, nil
}

func (c *Client) do(ctx context.Context, method, path, idempotencyKey string, input, output any) error {
	request, err := c.request(ctx, method, path, idempotencyKey, input)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call Marina: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read Marina response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("Marina response exceeded %d bytes", maxResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return statusError(response.StatusCode, body)
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("decode Marina response: %w", err)
	}
	return nil
}

func statusError(code int, body []byte) error {
	status := &APIError{}
	_ = json.Unmarshal(body, status)
	status.Code = code
	if len(status.Message) > 2048 {
		status.Message = status.Message[:2048]
	}
	return status
}
