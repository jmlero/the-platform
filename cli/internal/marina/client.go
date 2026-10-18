// Package marina is a small client for Marina's public marina.dev/v1 platform
// API. It mirrors only the wire shapes platformctl consumes.
package marina

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	APIVersion       = "marina.dev/v1"
	apiPrefix        = "/apis/marina.dev/v1"
	maxResponseBytes = 4 << 20
	maxEventBytes    = 1 << 20
)

var namePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// ValidName reports whether value is a valid namespace or object name.
func ValidName(value string) bool {
	return len(value) <= 63 && namePattern.MatchString(value)
}

type Client struct {
	baseURL    *url.URL
	token      string
	httpClient *http.Client
}

// APIError is a Kubernetes Status returned by Marina.
type APIError struct {
	StatusCode int
	Reason     string `json:"reason"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = http.StatusText(e.StatusCode)
	}
	details := fmt.Sprintf("Marina request failed: %s (HTTP %d", message, e.StatusCode)
	if e.Reason != "" {
		details += ", reason " + e.Reason
	}
	return details + ")"
}

// New requires https outside loopback and never follows redirects, so a
// bearer token is never sent in clear text or to another origin.
func New(rawBaseURL, token string, httpClient *http.Client) (*Client, error) {
	rawBaseURL = strings.TrimSpace(rawBaseURL)
	if rawBaseURL == "" {
		return nil, fmt.Errorf("Marina URL is required")
	}
	baseURL, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse Marina URL: %w", err)
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, fmt.Errorf("Marina URL must use http or https")
	}
	if baseURL.Host == "" {
		return nil, fmt.Errorf("Marina URL must include a host")
	}
	if baseURL.User != nil {
		return nil, fmt.Errorf("Marina URL must not contain credentials")
	}
	if baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("Marina URL must not contain a query or fragment")
	}
	if baseURL.Scheme != "https" && !isLoopbackHost(baseURL.Hostname()) {
		return nil, fmt.Errorf("Marina requires https outside loopback")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	baseURL.RawPath = ""
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	noRedirects := *httpClient
	noRedirects.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{baseURL: baseURL, token: strings.TrimSpace(token), httpClient: &noRedirects}, nil
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func namespaced(namespace, resource string, parts ...string) string {
	path := apiPrefix + "/namespaces/" + url.PathEscape(namespace) + "/" + resource
	for _, part := range parts {
		path += "/" + url.PathEscape(part)
	}
	return path
}

func object(kind, name string, spec any) map[string]any {
	return map[string]any{"apiVersion": APIVersion, "kind": kind, "metadata": ObjectMeta{Name: name}, "spec": spec}
}

func (c *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	var value Capabilities
	_, raw, err := c.do(ctx, http.MethodGet, apiPrefix+"/capabilities", "", nil, &value)
	value.Raw = raw
	return value, err
}

// CreateRun reports created=false when Marina returned an existing Run with
// the same spec.
func (c *Client) CreateRun(ctx context.Context, namespace, name string, spec RunSpec) (Run, bool, error) {
	var value Run
	status, raw, err := c.do(ctx, http.MethodPost, namespaced(namespace, "runs"), "", object("Run", name, spec), &value)
	value.Raw = raw
	return value, status == http.StatusCreated, err
}

func (c *Client) ListRuns(ctx context.Context, namespace string) (RunList, error) {
	var value RunList
	_, raw, err := c.do(ctx, http.MethodGet, namespaced(namespace, "runs"), "", nil, &value)
	value.Raw = raw
	return value, err
}

func (c *Client) GetRun(ctx context.Context, namespace, name string) (Run, error) {
	var value Run
	_, raw, err := c.do(ctx, http.MethodGet, namespaced(namespace, "runs", name), "", nil, &value)
	value.Raw = raw
	return value, err
}

func (c *Client) CancelRun(ctx context.Context, namespace, name, idempotencyKey string) (Run, error) {
	var value Run
	_, raw, err := c.do(ctx, http.MethodPost, namespaced(namespace, "runs", name, "cancel"), idempotencyKey, nil, &value)
	value.Raw = raw
	return value, err
}

func (c *Client) RunEvents(ctx context.Context, namespace, name string, after int64) (EventPage, error) {
	var value EventPage
	_, raw, err := c.do(ctx, http.MethodGet, eventsPath(namespace, name, after), "", nil, &value)
	value.Raw = raw
	return value, err
}

// StreamRunEvents reads server-sent events until the stream ends, ctx is
// cancelled or handle returns an error.
func (c *Client) StreamRunEvents(ctx context.Context, namespace, name string, after int64, handle func(Event) error) error {
	request, err := c.newRequest(ctx, http.MethodGet, eventsPath(namespace, name, after), "", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call Marina: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
		return statusError(response.StatusCode, body)
	}
	return readEventStream(response.Body, handle)
}

func readEventStream(body io.Reader, handle func(Event) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), maxEventBytes)
	var data []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(data) > 0 {
				var event Event
				if err := json.Unmarshal([]byte(strings.Join(data, "\n")), &event); err != nil {
					return fmt.Errorf("decode Marina event: %w", err)
				}
				if err := handle(event); err != nil {
					return err
				}
			}
			data = data[:0]
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		name, value, _ := strings.Cut(line, ":")
		if name == "data" {
			data = append(data, strings.TrimPrefix(value, " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read Marina event stream: %w", err)
	}
	return nil
}

func eventsPath(namespace, name string, after int64) string {
	path := namespaced(namespace, "runs", name, "events")
	if after > 0 {
		path += "?after=" + strconv.FormatInt(after, 10)
	}
	return path
}

func (c *Client) CreateTurn(ctx context.Context, namespace, name string, spec TurnSpec) (Turn, bool, error) {
	var value Turn
	status, raw, err := c.do(ctx, http.MethodPost, namespaced(namespace, "turns"), "", object("Turn", name, spec), &value)
	value.Raw = raw
	return value, status == http.StatusCreated, err
}

func (c *Client) GetTurn(ctx context.Context, namespace, name string) (Turn, error) {
	var value Turn
	_, raw, err := c.do(ctx, http.MethodGet, namespaced(namespace, "turns", name), "", nil, &value)
	value.Raw = raw
	return value, err
}

func (c *Client) ListDecisions(ctx context.Context, namespace string) (DecisionList, error) {
	var value DecisionList
	_, raw, err := c.do(ctx, http.MethodGet, namespaced(namespace, "decisions"), "", nil, &value)
	value.Raw = raw
	return value, err
}

func (c *Client) AnswerDecision(ctx context.Context, namespace, name, answer, idempotencyKey string) (Decision, error) {
	var value Decision
	_, raw, err := c.do(ctx, http.MethodPost, namespaced(namespace, "decisions", name, "answer"), idempotencyKey, map[string]string{"answer": answer}, &value)
	value.Raw = raw
	return value, err
}

func (c *Client) newRequest(ctx context.Context, method, requestPath, idempotencyKey string, input any) (*http.Request, error) {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("encode Marina request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	path, query, _ := strings.Cut(requestPath, "?")
	endpoint := *c.baseURL
	endpoint.Path += path
	endpoint.RawQuery = query
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create Marina request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "platformctl")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return request, nil
}

func (c *Client) do(ctx context.Context, method, requestPath, idempotencyKey string, input, output any) (int, json.RawMessage, error) {
	request, err := c.newRequest(ctx, method, requestPath, idempotencyKey, input)
	if err != nil {
		return 0, nil, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("call Marina: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return response.StatusCode, nil, fmt.Errorf("read Marina response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return response.StatusCode, nil, fmt.Errorf("Marina response exceeded %d bytes", maxResponseBytes)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return response.StatusCode, nil, statusError(response.StatusCode, body)
	}
	if err := json.Unmarshal(body, output); err != nil {
		return response.StatusCode, nil, fmt.Errorf("decode Marina response: %w", err)
	}
	return response.StatusCode, body, nil
}

func statusError(code int, body []byte) error {
	apiError := &APIError{StatusCode: code}
	_ = json.Unmarshal(body, apiError)
	return apiError
}
