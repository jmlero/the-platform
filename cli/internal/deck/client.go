package deck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
)

const maxResponseBytes = 4 << 20

type Client struct {
	baseURL       *url.URL
	token         string
	httpClient    *http.Client
	authenticated bool
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
	details := fmt.Sprintf("Deck request failed: %s (HTTP %d", message, e.StatusCode)
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
		return nil, fmt.Errorf("Deck URL is required")
	}
	baseURL, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse Deck URL: %w", err)
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, fmt.Errorf("Deck URL must use http or https")
	}
	if baseURL.Host == "" {
		return nil, fmt.Errorf("Deck URL must include a host")
	}
	if baseURL.User != nil {
		return nil, fmt.Errorf("Deck URL must not contain credentials")
	}
	if baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("Deck URL must not contain a query or fragment")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	baseURL.RawPath = ""
	token = strings.TrimSpace(token)
	if token != "" && baseURL.Scheme != "https" && !isLoopbackHost(baseURL.Hostname()) {
		return nil, fmt.Errorf("Deck operator tokens require https outside loopback")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	// Each invocation owns an in-memory cookie jar. Never reuse a caller's jar.
	credentialClient := *httpClient
	credentialClient.Jar, _ = cookiejar.New(nil)
	credentialClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	httpClient = &credentialClient

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

// Close revokes this invocation's session. Callers use a short independent
// context so an expired command deadline does not prevent best-effort logout.
func (c *Client) Close(ctx context.Context) error {
	if !c.authenticated {
		return nil
	}
	err := c.request(ctx, http.MethodDelete, "/api/v1/operator/session", nil, nil)
	c.authenticated = false
	c.token = ""
	c.httpClient.Jar, _ = cookiejar.New(nil)
	return err
}

func (c *Client) do(ctx context.Context, method, requestPath string, input, output any) error {
	if requestPath != "/healthz" && c.token != "" && !c.authenticated {
		if err := c.request(ctx, http.MethodPost, "/api/v1/operator/session", map[string]string{"token": c.token}, nil); err != nil {
			// Authentication responses must never echo submitted credentials in diagnostics.
			if apiErr, ok := err.(*APIError); ok {
				return &APIError{StatusCode: apiErr.StatusCode, Code: apiErr.Code, Message: "operator login failed", RequestID: apiErr.RequestID}
			}
			return fmt.Errorf("operator login failed: %w", err)
		}
		session, csrf := false, false
		for _, cookie := range c.httpClient.Jar.Cookies(c.baseURL) {
			session = session || cookie.Name == "deck_operator_session" && cookie.Value != ""
			csrf = csrf || cookie.Name == "deck_operator_csrf" && cookie.Value != ""
		}
		if !session || !csrf {
			return fmt.Errorf("operator login did not provide usable session and CSRF cookies; check HTTPS and cookie configuration")
		}
		c.authenticated = true
		c.token = ""
	}
	return c.request(ctx, method, requestPath, input, output)
}

func (c *Client) request(ctx context.Context, method, requestPath string, input, output any) error {
	var requestBody io.Reader
	if input != nil {
		var encoded bytes.Buffer
		if err := json.NewEncoder(&encoded).Encode(input); err != nil {
			return fmt.Errorf("encode Deck request: %w", err)
		}
		requestBody = &encoded
	}

	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + requestPath
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return fmt.Errorf("create Deck request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "platformctl")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		for _, cookie := range c.httpClient.Jar.Cookies(&endpoint) {
			if cookie.Name == "deck_operator_csrf" {
				request.Header.Set("X-CSRF-Token", cookie.Value)
			}
		}
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call Deck: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read Deck response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("Deck response exceeded %d bytes", maxResponseBytes)
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
		return fmt.Errorf("decode Deck response: %w", err)
	}
	return nil
}
