package appdeck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const maxResponseBytes = 4 << 20

type Client struct {
	baseURL    *url.URL
	token      string
	httpClient *http.Client
}

type APIError struct {
	StatusCode int
	Code       string `json:"code"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable"`
	RequestID  string `json:"request_id"`
	ErrorAlias string `json:"error"`
}

func (e *APIError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = strings.TrimSpace(e.ErrorAlias)
	}
	if message == "" {
		message = http.StatusText(e.StatusCode)
	}
	details := fmt.Sprintf("AppDeck request failed: %s (HTTP %d", message, e.StatusCode)
	if e.Code != "" {
		details += ", code " + e.Code
	}
	if e.RequestID != "" {
		details += ", request " + e.RequestID
	}
	return details + ")"
}

func New(rawBaseURL, token string, httpClient *http.Client) (*Client, error) {
	rawBaseURL = strings.TrimSpace(rawBaseURL)
	if rawBaseURL == "" {
		return nil, fmt.Errorf("AppDeck URL is required")
	}
	baseURL, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse AppDeck URL: %w", err)
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, fmt.Errorf("AppDeck URL must use http or https")
	}
	if baseURL.Host == "" {
		return nil, fmt.Errorf("AppDeck URL must include a host")
	}
	if baseURL.User != nil {
		return nil, fmt.Errorf("AppDeck URL must not contain credentials")
	}
	if baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("AppDeck URL must not contain a query or fragment")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	baseURL.RawPath = ""
	token = strings.TrimSpace(token)
	if token != "" && baseURL.Scheme != "https" && !isLoopbackHost(baseURL.Hostname()) {
		return nil, fmt.Errorf("AppDeck bearer tokens require https outside loopback")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if token != "" {
		// Refuse redirects so net/http cannot forward Authorization on a
		// same-host HTTPS to HTTP hop.
		credentialClient := *httpClient
		credentialClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
		httpClient = &credentialClient
	}
	return &Client{
		baseURL:    baseURL,
		token:      token,
		httpClient: httpClient,
	}, nil
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	var health Health
	err := c.do(ctx, http.MethodGet, "/healthz", nil, &health)
	return health, err
}

func (c *Client) Bootstrap(ctx context.Context) (Bootstrap, error) {
	var bootstrap Bootstrap
	err := c.do(ctx, http.MethodGet, "/api/v1/bootstrap", nil, &bootstrap)
	return bootstrap, err
}

func (c *Client) Task(ctx context.Context, id string) (TaskDetail, error) {
	id, err := resourceID(id)
	if err != nil {
		return TaskDetail{}, err
	}
	var detail TaskDetail
	err = c.do(ctx, http.MethodGet, "/api/v1/operator/tasks/"+id, nil, &detail)
	return detail, err
}

func (c *Client) CreateTask(ctx context.Context, input CreateTaskInput) (Task, error) {
	var task Task
	err := c.do(ctx, http.MethodPost, "/api/v1/tasks", input, &task)
	return task, err
}

func (c *Client) CreateCommand(ctx context.Context, runID string, input CreateCommandInput) (Command, error) {
	runID, err := resourceID(runID)
	if err != nil {
		return Command{}, err
	}
	var command Command
	err = c.do(ctx, http.MethodPost, "/api/v1/operator/runs/"+runID+"/commands", input, &command)
	return command, err
}

func resourceID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("resource ID is required")
	}
	if strings.ContainsAny(value, "/?#") {
		return "", fmt.Errorf("resource ID contains an invalid path character")
	}
	return value, nil
}

func (c *Client) do(ctx context.Context, method, requestPath string, input, output any) error {
	var requestBody io.Reader
	if input != nil {
		var encoded bytes.Buffer
		if err := json.NewEncoder(&encoded).Encode(input); err != nil {
			return fmt.Errorf("encode AppDeck request: %w", err)
		}
		requestBody = &encoded
	}

	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + requestPath
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return fmt.Errorf("create AppDeck request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "platformctl")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call AppDeck: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read AppDeck response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("AppDeck response exceeded %d bytes", maxResponseBytes)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		apiError := &APIError{StatusCode: response.StatusCode}
		_ = json.Unmarshal(body, apiError)
		if apiError.RequestID == "" {
			apiError.RequestID = response.Header.Get("X-Request-ID")
		}
		return apiError
	}
	if output == nil || len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("decode AppDeck response: %w", err)
	}
	return nil
}
