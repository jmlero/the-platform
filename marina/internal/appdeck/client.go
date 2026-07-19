package appdeck

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

const maxResponseBytes = 4 << 20

var (
	ErrInvalidConfiguration = errors.New("invalid AppDeck configuration")
	ErrIncompatibleProvider = errors.New("incompatible work-control provider")
	ErrProviderNotReady     = errors.New("work-control provider is not ready")
)

type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Retryable  bool
	RequestID  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("AppDeck returned HTTP %d: %s", e.StatusCode, e.Message)
}

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
}

var (
	_ workcontrol.HealthChecker       = (*Client)(nil)
	_ workcontrol.ProviderInformation = (*Client)(nil)
	_ workcontrol.AgentRegistry       = (*Client)(nil)
	_ workcontrol.WorkDiscovery       = (*Client)(nil)
	_ workcontrol.WorkInspection      = (*Client)(nil)
	_ workcontrol.Acquisition         = (*Client)(nil)
	_ workcontrol.Planning            = (*Client)(nil)
	_ workcontrol.RunControl          = (*Client)(nil)
	_ workcontrol.EventPublisher      = (*Client)(nil)
)

func New(serverURL string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil {
		return nil, fmt.Errorf("parse AppDeck URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("AppDeck URL must use http or https: %w", ErrInvalidConfiguration)
	}
	if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("AppDeck URL must be an origin with an optional path prefix: %w", ErrInvalidConfiguration)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	return &Client{baseURL: parsed, httpClient: httpClient}, nil
}

func (c *Client) Health(ctx context.Context) error {
	_, err := c.CheckCompatibility(ctx, workcontrol.ReconciliationCapabilities())
	return err
}

func (c *Client) GetProviderInfo(ctx context.Context) (workcontrol.ProviderInfo, error) {
	var response providerInfoDTO
	if err := c.do(ctx, http.MethodGet, "/api/v1/provider", nil, &response); err != nil {
		return workcontrol.ProviderInfo{}, err
	}
	return response.toDomain(), nil
}

func (c *Client) CheckCompatibility(
	ctx context.Context,
	requiredCapabilities []string,
) (workcontrol.ProviderInfo, error) {
	info, err := c.GetProviderInfo(ctx)
	if err != nil {
		var apiError *APIError
		if errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound {
			return workcontrol.ProviderInfo{}, fmt.Errorf("provider information endpoint is missing: %w", ErrIncompatibleProvider)
		}
		return workcontrol.ProviderInfo{}, err
	}
	if err := validateProviderInfo(info, requiredCapabilities); err != nil {
		return info, err
	}
	return info, nil
}

func (c *Client) RegisterAgent(
	ctx context.Context,
	registration workcontrol.AgentRegistration,
) (workcontrol.Agent, error) {
	if strings.TrimSpace(registration.Name) == "" {
		return workcontrol.Agent{}, errors.New("agent name is required")
	}
	if registration.Kind == "" {
		registration.Kind = "marina"
	}

	request := registerAgentRequest{
		ID:           registration.ID,
		Name:         registration.Name,
		Kind:         registration.Kind,
		Capabilities: append([]string(nil), registration.Capabilities...),
	}
	var response agentDTO
	if err := c.do(ctx, http.MethodPost, "/api/v1/agents", request, &response); err != nil {
		return workcontrol.Agent{}, err
	}

	return response.toDomain(), nil
}

func (c *Client) ListReadyWork(ctx context.Context) ([]workcontrol.WorkItem, error) {
	var response []taskDTO
	if err := c.do(ctx, http.MethodGet, "/api/v1/tasks?status=ready", nil, &response); err != nil {
		return nil, err
	}

	ready := make([]workcontrol.WorkItem, 0, len(response))
	for _, task := range response {
		if task.Status != string(workcontrol.WorkReady) {
			continue
		}
		ready = append(ready, task.toDomain())
	}
	return ready, nil
}

func (c *Client) GetWork(ctx context.Context, workItemID string) (workcontrol.WorkDetail, error) {
	if err := validatePathSegment("work item ID", workItemID); err != nil {
		return workcontrol.WorkDetail{}, err
	}
	var response taskDetailDTO
	if err := c.do(ctx, http.MethodGet, "/api/v1/tasks/"+workItemID, nil, &response); err != nil {
		return workcontrol.WorkDetail{}, err
	}
	return response.toDomain(), nil
}

func (c *Client) ClaimWork(
	ctx context.Context,
	workItemID string,
	claim workcontrol.Claim,
) (workcontrol.Run, error) {
	if err := validatePathSegment("work item ID", workItemID); err != nil {
		return workcontrol.Run{}, err
	}
	if strings.TrimSpace(claim.AgentID) == "" {
		return workcontrol.Run{}, errors.New("agent ID is required")
	}
	if claim.Lease < time.Second {
		return workcontrol.Run{}, errors.New("lease must be at least one second")
	}

	leaseSeconds := roundedLeaseSeconds(claim.Lease)
	request := claimRequest{AgentID: claim.AgentID, LeaseSeconds: leaseSeconds}
	var response runDTO
	endpoint := "/api/v1/tasks/" + workItemID + "/claim"
	if err := c.do(ctx, http.MethodPost, endpoint, request, &response); err != nil {
		return workcontrol.Run{}, err
	}

	return response.toDomain(), nil
}

func (c *Client) RenewRunLease(
	ctx context.Context,
	runID string,
	renewal workcontrol.LeaseRenewal,
) error {
	if err := validatePathSegment("run ID", runID); err != nil {
		return err
	}
	if strings.TrimSpace(renewal.AgentID) == "" {
		return errors.New("agent ID is required")
	}
	if renewal.Lease < time.Second {
		return errors.New("lease must be at least one second")
	}
	request := claimRequest{
		AgentID:      renewal.AgentID,
		LeaseSeconds: roundedLeaseSeconds(renewal.Lease),
	}
	return c.do(ctx, http.MethodPost, "/api/v1/runs/"+runID+"/heartbeat", request, nil)
}

func (c *Client) SubmitRunPlan(
	ctx context.Context,
	runID string,
	submission workcontrol.PlanSubmission,
) error {
	if err := validatePathSegment("run ID", runID); err != nil {
		return err
	}
	submission.Plan = strings.TrimSpace(submission.Plan)
	if submission.Plan == "" {
		return errors.New("plan is required")
	}
	request := planRequest{
		Plan:            submission.Plan,
		RequireApproval: submission.RequireApproval,
	}
	return c.do(ctx, http.MethodPost, "/api/v1/runs/"+runID+"/plan", request, nil)
}

func (c *Client) ReportRunProgress(
	ctx context.Context,
	runID string,
	progress workcontrol.Progress,
) error {
	if err := validatePathSegment("run ID", runID); err != nil {
		return err
	}
	progress.Message = strings.TrimSpace(progress.Message)
	if progress.Message == "" {
		return errors.New("progress message is required")
	}
	request := progressRequest{Message: progress.Message, Payload: progress.Payload}
	return c.do(ctx, http.MethodPost, "/api/v1/runs/"+runID+"/progress", request, nil)
}

func (c *Client) BlockRun(ctx context.Context, runID string, reason string) error {
	if err := validatePathSegment("run ID", runID); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errors.New("block reason is required")
	}
	return c.do(ctx, http.MethodPost, "/api/v1/runs/"+runID+"/block", blockRequest{Reason: reason}, nil)
}

func roundedLeaseSeconds(lease time.Duration) int {
	return int((lease + time.Second - 1) / time.Second)
}

func (c *Client) do(
	ctx context.Context,
	method string,
	endpoint string,
	body any,
	result any,
) error {
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode AppDeck request: %w", err)
		}
		requestBody = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.endpoint(endpoint), requestBody)
	if err != nil {
		return fmt.Errorf("create AppDeck request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Request-ID", newRequestID())
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call AppDeck: %w", err)
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read AppDeck response: %w", err)
	}
	if len(payload) > maxResponseBytes {
		return errors.New("AppDeck response exceeds size limit")
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message := strings.TrimSpace(string(payload))
		var apiResponse struct {
			Error     string `json:"error"`
			Code      string `json:"code"`
			Message   string `json:"message"`
			Retryable bool   `json:"retryable"`
			RequestID string `json:"request_id"`
		}
		if json.Unmarshal(payload, &apiResponse) == nil {
			if strings.TrimSpace(apiResponse.Message) != "" {
				message = apiResponse.Message
			} else if strings.TrimSpace(apiResponse.Error) != "" {
				message = apiResponse.Error
			}
		}
		if message == "" {
			message = http.StatusText(response.StatusCode)
		}
		requestID := strings.TrimSpace(apiResponse.RequestID)
		if requestID == "" {
			requestID = response.Header.Get("X-Request-ID")
		}
		return &APIError{
			StatusCode: response.StatusCode,
			Code:       apiResponse.Code,
			Message:    message,
			Retryable:  apiResponse.Retryable,
			RequestID:  requestID,
		}
	}

	if result == nil || len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, result); err != nil {
		return fmt.Errorf("decode AppDeck response: %w", err)
	}
	return nil
}

func (c *Client) endpoint(suffix string) string {
	endpoint := *c.baseURL
	path, rawQuery, _ := strings.Cut(suffix, "?")
	endpoint.Path = c.baseURL.Path + path
	endpoint.RawPath = ""
	endpoint.RawQuery = rawQuery
	return endpoint.String()
}

func validateProviderInfo(info workcontrol.ProviderInfo, requiredCapabilities []string) error {
	if info.Provider != "appdeck" {
		return fmt.Errorf("provider is %q, want appdeck: %w", info.Provider, ErrIncompatibleProvider)
	}
	if !compatibleProtocolVersion(info.ProtocolVersion) {
		return fmt.Errorf(
			"provider protocol is %q, Marina supports >=%d.%d and <%d.0: %w",
			info.ProtocolVersion,
			workcontrol.ProtocolMajor,
			workcontrol.ProtocolMinimumMinor,
			workcontrol.ProtocolMajor+1,
			ErrIncompatibleProvider,
		)
	}
	available := make(map[string]struct{}, len(info.Capabilities))
	for _, capability := range info.Capabilities {
		capability = strings.TrimSpace(capability)
		if capability == "" {
			return fmt.Errorf("provider advertised an empty capability: %w", ErrIncompatibleProvider)
		}
		if _, exists := available[capability]; exists {
			return fmt.Errorf("provider advertised duplicate capability %q: %w", capability, ErrIncompatibleProvider)
		}
		available[capability] = struct{}{}
	}
	missing := make([]string, 0)
	for _, capability := range requiredCapabilities {
		if _, ok := available[capability]; !ok {
			missing = append(missing, capability)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("provider is missing capabilities %s: %w", strings.Join(missing, ", "), ErrIncompatibleProvider)
	}
	if info.ServerTime.IsZero() {
		return fmt.Errorf("provider server_time is missing: %w", ErrIncompatibleProvider)
	}
	if !info.Ready {
		reasons := make([]string, 0, len(info.Readiness))
		for dependency, status := range info.Readiness {
			if status != "available" {
				reasons = append(reasons, dependency+"="+status)
			}
		}
		sort.Strings(reasons)
		detail := strings.Join(reasons, ", ")
		if detail == "" {
			detail = "no dependency detail"
		}
		return fmt.Errorf("provider reported not ready (%s): %w", detail, ErrProviderNotReady)
	}
	return nil
}

func compatibleProtocolVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	return majorErr == nil && minorErr == nil &&
		major == workcontrol.ProtocolMajor && minor >= workcontrol.ProtocolMinimumMinor
}

func newRequestID() string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err == nil {
		return "marina_" + hex.EncodeToString(value)
	}
	return fmt.Sprintf("marina_%d", time.Now().UTC().UnixNano())
}

func validatePathSegment(name string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if strings.ContainsAny(value, "/?#") {
		return fmt.Errorf("%s contains path delimiters", name)
	}
	return nil
}

func fromUnixMilli(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}

func optionalUnixMilli(value *int64) *time.Time {
	if value == nil {
		return nil
	}
	converted := fromUnixMilli(*value)
	return &converted
}

type registerAgentRequest struct {
	ID           string   `json:"id,omitempty"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Capabilities []string `json:"capabilities"`
}

type providerInfoDTO struct {
	Provider        string            `json:"provider"`
	ProtocolVersion string            `json:"protocol_version"`
	Capabilities    []string          `json:"capabilities"`
	ServerTime      int64             `json:"server_time"`
	Ready           bool              `json:"ready"`
	Readiness       map[string]string `json:"readiness"`
}

func (info providerInfoDTO) toDomain() workcontrol.ProviderInfo {
	readiness := make(map[string]string, len(info.Readiness))
	for dependency, status := range info.Readiness {
		readiness[dependency] = status
	}
	return workcontrol.ProviderInfo{
		Provider:        info.Provider,
		ProtocolVersion: info.ProtocolVersion,
		Capabilities:    append([]string(nil), info.Capabilities...),
		ServerTime:      fromUnixMilli(info.ServerTime),
		Ready:           info.Ready,
		Readiness:       readiness,
	}
}

type claimRequest struct {
	AgentID      string `json:"agent_id"`
	LeaseSeconds int    `json:"lease_seconds"`
}

type progressRequest struct {
	Message string         `json:"message"`
	Payload map[string]any `json:"payload,omitempty"`
}

type planRequest struct {
	Plan            string `json:"plan"`
	RequireApproval bool   `json:"require_approval"`
}

type blockRequest struct {
	Reason string `json:"reason"`
}

type agentDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities"`
	LastSeenAt   int64    `json:"last_seen_at"`
	CreatedAt    int64    `json:"created_at"`
}

func (agent agentDTO) toDomain() workcontrol.Agent {
	return workcontrol.Agent{
		ID:           agent.ID,
		Name:         agent.Name,
		Kind:         agent.Kind,
		Status:       agent.Status,
		Capabilities: append([]string(nil), agent.Capabilities...),
		LastSeenAt:   fromUnixMilli(agent.LastSeenAt),
		CreatedAt:    fromUnixMilli(agent.CreatedAt),
	}
}

type taskDTO struct {
	ID                 string `json:"id"`
	ProjectID          string `json:"project_id"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
	Status             string `json:"status"`
	Priority           string `json:"priority"`
	BudgetMinutes      int    `json:"budget_minutes"`
	Version            int    `json:"version"`
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `json:"updated_at"`
}

type taskDetailDTO struct {
	Task taskDTO  `json:"task"`
	Runs []runDTO `json:"runs"`
}

func (detail taskDetailDTO) toDomain() workcontrol.WorkDetail {
	runs := make([]workcontrol.Run, 0, len(detail.Runs))
	for _, run := range detail.Runs {
		runs = append(runs, run.toDomain())
	}
	return workcontrol.WorkDetail{
		Work: detail.Task.toDomain(),
		Runs: runs,
	}
}

func (task taskDTO) toDomain() workcontrol.WorkItem {
	return workcontrol.WorkItem{
		ID:                 task.ID,
		ProjectID:          task.ProjectID,
		Title:              task.Title,
		Description:        task.Description,
		AcceptanceCriteria: task.AcceptanceCriteria,
		Status:             workcontrol.WorkStatus(task.Status),
		Priority:           task.Priority,
		BudgetMinutes:      task.BudgetMinutes,
		Version:            task.Version,
		CreatedAt:          fromUnixMilli(task.CreatedAt),
		UpdatedAt:          fromUnixMilli(task.UpdatedAt),
	}
}

type runDTO struct {
	ID             string `json:"id"`
	TaskID         string `json:"task_id"`
	AgentID        string `json:"agent_id"`
	Status         string `json:"status"`
	Plan           string `json:"plan"`
	ResultSummary  string `json:"result_summary"`
	LeaseExpiresAt *int64 `json:"lease_expires_at"`
	StartedAt      int64  `json:"started_at"`
	FinishedAt     *int64 `json:"finished_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

func (run runDTO) toDomain() workcontrol.Run {
	return workcontrol.Run{
		ID:             run.ID,
		WorkItemID:     run.TaskID,
		AgentID:        run.AgentID,
		Status:         workcontrol.RunStatus(run.Status),
		Plan:           run.Plan,
		ResultSummary:  run.ResultSummary,
		LeaseExpiresAt: optionalUnixMilli(run.LeaseExpiresAt),
		StartedAt:      fromUnixMilli(run.StartedAt),
		FinishedAt:     optionalUnixMilli(run.FinishedAt),
		UpdatedAt:      fromUnixMilli(run.UpdatedAt),
	}
}
