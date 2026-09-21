package deck

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
	ErrInvalidConfiguration = errors.New("invalid Deck configuration")
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
	return fmt.Sprintf("Deck returned HTTP %d: %s", e.StatusCode, e.Message)
}

func (e *APIError) RetryableFailure() bool { return e.Retryable }

// A conflict rejects the mutation without committing it. Consumers must replay
// their original request before considering a newer version of the same fence.
func (e *APIError) MutationConflict() bool { return e.StatusCode == http.StatusConflict }

type Client struct {
	baseURL              *url.URL
	httpClient           *http.Client
	bearerTokenSource    bearerTokenSource
	requiredCapabilities []string
}

type Option func(*Client) error

func WithRequiredCapabilities(required []string) Option {
	return func(client *Client) error {
		if len(required) == 0 {
			return errors.New("enabled capabilities are required")
		}
		client.requiredCapabilities = append([]string(nil), required...)
		return nil
	}
}

func WithBearerTokenFile(path string) Option {
	return func(client *Client) error {
		source, err := newFileBearerTokenSource(path)
		if err != nil {
			return err
		}
		client.bearerTokenSource = source
		return nil
	}
}

var (
	_ workcontrol.HealthChecker       = (*Client)(nil)
	_ workcontrol.ProviderInformation = (*Client)(nil)
	_ workcontrol.AgentRegistry       = (*Client)(nil)
	_ workcontrol.WorkDiscovery       = (*Client)(nil)
	_ workcontrol.WorkInspection      = (*Client)(nil)
	_ workcontrol.Acquisition         = (*Client)(nil)
	_ workcontrol.Planning            = (*Client)(nil)
	_ workcontrol.VersionedPlanning   = (*Client)(nil)
	_ workcontrol.DecisionInspection  = (*Client)(nil)
	_ workcontrol.RunControl          = (*Client)(nil)
	_ workcontrol.EventPublisher      = (*Client)(nil)
)

func New(serverURL string, httpClient *http.Client, options ...Option) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil {
		return nil, fmt.Errorf("parse Deck URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("Deck URL must use http or https: %w", ErrInvalidConfiguration)
	}
	if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("Deck URL must be an origin with an optional path prefix: %w", ErrInvalidConfiguration)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	client := &Client{baseURL: parsed, httpClient: httpClient}
	for _, option := range options {
		if err := option(client); err != nil {
			return nil, fmt.Errorf("configure Deck client: %w", err)
		}
	}
	if client.bearerTokenSource != nil {
		if parsed.Scheme != "https" && !loopbackHost(parsed.Hostname()) {
			return nil, fmt.Errorf("Deck Bearer credentials require HTTPS outside loopback: %w", ErrInvalidConfiguration)
		}
		// API endpoints are canonical and never require redirects. Refusing them
		// prevents net/http from forwarding Authorization on a same-host HTTPS
		// to HTTP redirect while preserving the caller's transport and timeout.
		credentialClient := *client.httpClient
		credentialClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
		client.httpClient = &credentialClient
	}
	return client, nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func (c *Client) Health(ctx context.Context) error {
	required := c.requiredCapabilities
	if len(required) == 0 {
		required = workcontrol.MilestoneOneCapabilities()
	}
	_, err := c.CheckCompatibility(ctx, required)
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
	if strings.TrimSpace(registration.ID) == "" || strings.TrimSpace(registration.InstallationID) == "" || strings.TrimSpace(registration.Name) == "" {
		return workcontrol.Agent{}, errors.New("stable agent ID, installation ID, and agent name are required")
	}
	if registration.Kind == "" {
		registration.Kind = "marina"
	}

	request := registerAgentRequest{
		ID:             registration.ID,
		InstallationID: registration.InstallationID,
		Name:           registration.Name,
		Kind:           registration.Kind,
		Capabilities:   append([]string(nil), registration.Capabilities...),
	}
	var response agentDTO
	if err := c.do(ctx, http.MethodPost, "/api/v1/agents", request, &response); err != nil {
		return workcontrol.Agent{}, err
	}
	if response.ID != registration.ID || response.InstallationID != registration.InstallationID {
		return workcontrol.Agent{}, errors.New("Deck registration response changed the stable agent or installation identity")
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
	if claim.ExpectedTaskVersion < 1 {
		return workcontrol.Run{}, errors.New("expected task version is required")
	}
	request := claimRequest{AgentID: claim.AgentID, ExpectedTaskVersion: claim.ExpectedTaskVersion, LeaseSeconds: leaseSeconds}
	var response runDTO
	endpoint := "/api/v1/tasks/" + workItemID + "/claim"
	if err := c.doIdempotent(ctx, http.MethodPost, endpoint, claim.IdempotencyKey, request, &response); err != nil {
		return workcontrol.Run{}, err
	}

	return response.toDomain(), nil
}

func (c *Client) LookupClaim(ctx context.Context, projectID, idempotencyKey string) (workcontrol.Run, error) {
	if err := validatePathSegment("project ID", projectID); err != nil {
		return workcontrol.Run{}, err
	}
	if err := validatePathSegment("claim key", idempotencyKey); err != nil {
		return workcontrol.Run{}, err
	}
	var response runDTO
	endpoint := "/api/v1/claims/" + url.PathEscape(idempotencyKey) + "?project_id=" + url.QueryEscape(projectID)
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return workcontrol.Run{}, err
	}
	return response.toDomain(), nil
}

func (c *Client) ListAssignedRuns(ctx context.Context, projectID, agentID, cursor string, limit int) (workcontrol.ActiveRunPage, error) {
	query := url.Values{"project_id": {projectID}, "agent_id": {agentID}, "limit": {strconv.Itoa(limit)}}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	var response activeRunPageDTO
	if err := c.do(ctx, http.MethodGet, "/api/v1/runs?"+query.Encode(), nil, &response); err != nil {
		return workcontrol.ActiveRunPage{}, err
	}
	items := make([]workcontrol.ActiveRun, 0, len(response.Items))
	for _, item := range response.Items {
		items = append(items, workcontrol.ActiveRun{Run: item.Run.toDomain(), Work: item.Task.toDomain()})
	}
	return workcontrol.ActiveRunPage{Items: items, NextCursor: response.NextCursor}, nil
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
		AgentID: renewal.AgentID, ExpectedVersion: renewal.ExpectedVersion,
		FenceToken: renewal.FenceToken, LeaseSeconds: roundedLeaseSeconds(renewal.Lease),
	}
	return c.doIdempotent(
		ctx, http.MethodPost, "/api/v1/runs/"+runID+"/heartbeat", renewal.IdempotencyKey, request, nil,
	)
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
		runGuardRequest: runGuardRequest{AgentID: submission.AgentID, ExpectedVersion: submission.ExpectedVersion, FenceToken: submission.FenceToken},
		Plan:            submission.Plan,
		RequireApproval: submission.RequireApproval,
	}
	return c.doIdempotent(
		ctx, http.MethodPost, "/api/v1/runs/"+runID+"/plan", submission.IdempotencyKey, request, nil,
	)
}

func (c *Client) SubmitPlanRevision(
	ctx context.Context,
	runID string,
	submission workcontrol.PlanRevisionSubmission,
) (workcontrol.PlanRevisionResult, error) {
	if err := validatePathSegment("run ID", runID); err != nil {
		return workcontrol.PlanRevisionResult{}, err
	}
	request, err := newPlanRevisionRequest(submission)
	if err != nil {
		return workcontrol.PlanRevisionResult{}, err
	}
	var response planRevisionResponseDTO
	if err := c.doIdempotent(
		ctx, http.MethodPost, "/api/v1/runs/"+runID+"/plan", submission.IdempotencyKey, request, &response,
	); err != nil {
		return workcontrol.PlanRevisionResult{}, err
	}
	result := workcontrol.PlanRevisionResult{PlanRevision: response.PlanRevision.toDomain()}
	if response.Decision != nil {
		decision := response.Decision.toDomain()
		result.Decision = &decision
	}
	return result, nil
}

func (c *Client) GetDecision(ctx context.Context, decisionID string) (workcontrol.Decision, error) {
	if err := validatePathSegment("decision ID", decisionID); err != nil {
		return workcontrol.Decision{}, err
	}
	var response decisionDTO
	if err := c.do(ctx, http.MethodGet, "/api/v1/decisions/"+decisionID, nil, &response); err != nil {
		return workcontrol.Decision{}, err
	}
	return response.toDomain(), nil
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
	request := progressRequest{runGuardRequest: runGuardRequest{AgentID: progress.AgentID, ExpectedVersion: progress.ExpectedVersion, FenceToken: progress.FenceToken}, Message: progress.Message, Payload: progress.Payload}
	return c.doIdempotent(
		ctx, http.MethodPost, "/api/v1/runs/"+runID+"/progress", progress.IdempotencyKey, request, nil,
	)
}

func (c *Client) BlockRun(ctx context.Context, runID string, block workcontrol.RunBlock) error {
	if err := validatePathSegment("run ID", runID); err != nil {
		return err
	}
	reason := strings.TrimSpace(block.Reason)
	if reason == "" {
		return errors.New("block reason is required")
	}
	return c.doIdempotent(
		ctx,
		http.MethodPost,
		"/api/v1/runs/"+runID+"/block",
		block.IdempotencyKey,
		blockRequest{runGuardRequest: runGuardRequest{AgentID: block.AgentID, ExpectedVersion: block.ExpectedVersion, FenceToken: block.FenceToken}, Reason: reason},
		nil,
	)
}

func (c *Client) ListRunCommands(ctx context.Context, runID string, afterSequence int64, limit int) ([]workcontrol.Command, error) {
	query := url.Values{"after_sequence": {strconv.FormatInt(afterSequence, 10)}, "limit": {strconv.Itoa(limit)}}
	var response commandPageDTO
	if err := c.do(ctx, http.MethodGet, "/api/v1/runs/"+runID+"/commands?"+query.Encode(), nil, &response); err != nil {
		return nil, err
	}
	commands := make([]workcontrol.Command, 0, len(response.Items))
	for _, command := range response.Items {
		commands = append(commands, command.toDomain())
	}
	return commands, nil
}

func (c *Client) AcknowledgeRunCommand(ctx context.Context, runID, commandID string, acknowledgement workcontrol.CommandAcknowledgement) error {
	request := commandAckRequest{runGuardRequest: runGuardRequest{AgentID: acknowledgement.AgentID,
		ExpectedVersion: acknowledgement.ExpectedVersion, FenceToken: acknowledgement.FenceToken},
		Status: acknowledgement.Status, Result: acknowledgement.Result}
	return c.doIdempotent(ctx, http.MethodPost, "/api/v1/runs/"+runID+"/commands/"+commandID+"/ack",
		acknowledgement.IdempotencyKey, request, nil)
}

func (c *Client) AppendRunEvent(ctx context.Context, runID string, event workcontrol.ProjectedEvent) error {
	request := projectedEventRequest{runGuardRequest: runGuardRequest{AgentID: event.AgentID,
		ExpectedVersion: event.ExpectedVersion, FenceToken: event.FenceToken}, Producer: event.Producer,
		ProducerEventID: event.ProducerEventID, ExecutionID: event.ExecutionID, Sequence: event.Sequence,
		Type: event.Type, Message: event.Message, Payload: event.Payload, OccurredAt: event.OccurredAt.UnixMilli()}
	return c.doIdempotent(ctx, http.MethodPost, "/api/v1/runs/"+runID+"/events", event.IdempotencyKey, request, nil)
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
	return c.doRequest(ctx, method, endpoint, "", body, result)
}

func (c *Client) doIdempotent(
	ctx context.Context,
	method string,
	endpoint string,
	idempotencyKey string,
	body any,
	result any,
) error {
	key, err := clientIdempotencyKey(idempotencyKey)
	if err != nil {
		return err
	}
	return c.doRequest(ctx, method, endpoint, key, body, result)
}

func (c *Client) doRequest(
	ctx context.Context,
	method string,
	endpoint string,
	idempotencyKey string,
	body any,
	result any,
) error {
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Deck request: %w", err)
		}
		requestBody = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.endpoint(endpoint), requestBody)
	if err != nil {
		return fmt.Errorf("create Deck request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Request-ID", newRequestID())
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	if c.bearerTokenSource != nil {
		token, err := c.bearerTokenSource.Token()
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call Deck: %w", err)
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read Deck response: %w", err)
	}
	if len(payload) > maxResponseBytes {
		return errors.New("Deck response exceeds size limit")
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
		return fmt.Errorf("decode Deck response: %w", err)
	}
	return nil
}

func clientIdempotencyKey(value string) (string, error) {
	if value == "" {
		return "idem_" + strings.TrimPrefix(newRequestID(), "req_"), nil
	}
	if value != strings.TrimSpace(value) || len(value) > 128 {
		return "", errors.New("idempotency key must contain 1 to 128 visible ASCII characters")
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return "", errors.New("idempotency key must contain only visible ASCII characters")
		}
	}
	return value, nil
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
	if info.Provider != "deck" {
		return fmt.Errorf("provider is %q, want deck: %w", info.Provider, ErrIncompatibleProvider)
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
	ID             string   `json:"id,omitempty"`
	InstallationID string   `json:"installation_id"`
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Capabilities   []string `json:"capabilities"`
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
	AgentID             string `json:"agent_id"`
	ExpectedTaskVersion int    `json:"expected_task_version,omitempty"`
	ExpectedVersion     int64  `json:"expected_version,omitempty"`
	FenceToken          int64  `json:"fence_token,omitempty"`
	LeaseSeconds        int    `json:"lease_seconds"`
}

type runGuardRequest struct {
	AgentID         string `json:"agent_id"`
	ExpectedVersion int64  `json:"expected_version"`
	FenceToken      int64  `json:"fence_token"`
}

type progressRequest struct {
	runGuardRequest
	Message string         `json:"message"`
	Payload map[string]any `json:"payload,omitempty"`
}

type planRequest struct {
	runGuardRequest
	Plan            string `json:"plan"`
	RequireApproval bool   `json:"require_approval"`
}

type planRevisionRequest struct {
	runGuardRequest
	Plan                 string   `json:"plan"`
	ContextDigest        string   `json:"context_digest"`
	RiskClass            string   `json:"risk_class"`
	VerificationStrategy string   `json:"verification_strategy"`
	ExpectedSideEffects  []string `json:"expected_side_effects"`
	RollbackApproach     string   `json:"rollback_approach"`
	SourceRevision       string   `json:"source_revision"`
	PolicyVersion        string   `json:"policy_version"`
	RequireApproval      bool     `json:"require_approval"`
	DecisionExpiresAt    int64    `json:"decision_expires_at,omitempty"`
	DecisionEvidence     string   `json:"decision_evidence,omitempty"`
	EstimatedCostMinor   int64    `json:"estimated_cost_minor,omitempty"`
	Currency             string   `json:"currency,omitempty"`
	WaitingConsequence   string   `json:"waiting_consequence,omitempty"`
	RejectingConsequence string   `json:"rejecting_consequence,omitempty"`
}

func newPlanRevisionRequest(submission workcontrol.PlanRevisionSubmission) (planRevisionRequest, error) {
	submission.Plan = strings.TrimSpace(submission.Plan)
	submission.ContextDigest = strings.TrimSpace(submission.ContextDigest)
	submission.RiskClass = strings.TrimSpace(submission.RiskClass)
	submission.VerificationStrategy = strings.TrimSpace(submission.VerificationStrategy)
	submission.RollbackApproach = strings.TrimSpace(submission.RollbackApproach)
	submission.SourceRevision = strings.TrimSpace(submission.SourceRevision)
	submission.PolicyVersion = strings.TrimSpace(submission.PolicyVersion)
	submission.AgentID = strings.TrimSpace(submission.AgentID)
	if submission.AgentID == "" || submission.ExpectedVersion < 1 || submission.FenceToken < 1 {
		return planRevisionRequest{}, errors.New("agent ID, expected version, and fence token are required")
	}
	if submission.Plan == "" || !sha256Digest(submission.ContextDigest) || submission.VerificationStrategy == "" ||
		submission.RollbackApproach == "" || (submission.SourceRevision != "" && !fullCommitRevision(submission.SourceRevision)) || submission.PolicyVersion == "" {
		return planRevisionRequest{}, errors.New("plan, context digest, verification strategy, rollback approach and policy version are required; an optional source revision must be a full commit")
	}
	switch submission.RiskClass {
	case "low", "medium", "high", "critical":
	default:
		return planRevisionRequest{}, errors.New("risk class must be low, medium, high, or critical")
	}
	if submission.EstimatedCostMinor < 0 {
		return planRevisionRequest{}, errors.New("estimated cost cannot be negative")
	}
	if submission.Currency != "" && len(strings.TrimSpace(submission.Currency)) != 3 {
		return planRevisionRequest{}, errors.New("currency must contain three characters")
	}
	expectedSideEffects := make([]string, 0, len(submission.ExpectedSideEffects))
	for _, effect := range submission.ExpectedSideEffects {
		effect = strings.TrimSpace(effect)
		if effect == "" {
			return planRevisionRequest{}, errors.New("expected side effects cannot contain empty values")
		}
		expectedSideEffects = append(expectedSideEffects, effect)
	}
	request := planRevisionRequest{
		runGuardRequest: runGuardRequest{AgentID: submission.AgentID, ExpectedVersion: submission.ExpectedVersion, FenceToken: submission.FenceToken},
		Plan:            submission.Plan, ContextDigest: submission.ContextDigest, RiskClass: submission.RiskClass,
		VerificationStrategy: submission.VerificationStrategy, ExpectedSideEffects: expectedSideEffects,
		RollbackApproach: submission.RollbackApproach, SourceRevision: submission.SourceRevision,
		PolicyVersion: submission.PolicyVersion, RequireApproval: submission.RequireApproval,
		DecisionEvidence: strings.TrimSpace(submission.DecisionEvidence), EstimatedCostMinor: submission.EstimatedCostMinor,
		Currency: strings.ToUpper(strings.TrimSpace(submission.Currency)), WaitingConsequence: strings.TrimSpace(submission.WaitingConsequence),
		RejectingConsequence: strings.TrimSpace(submission.RejectingConsequence),
	}
	if submission.DecisionExpiresAt != nil {
		if submission.DecisionExpiresAt.IsZero() {
			return planRevisionRequest{}, errors.New("decision expiry cannot be zero")
		}
		request.DecisionExpiresAt = submission.DecisionExpiresAt.UnixMilli()
	}
	return request, nil
}

func sha256Digest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func fullCommitRevision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

type planRevisionResponseDTO struct {
	PlanRevision planRevisionDTO `json:"plan_revision"`
	Decision     *decisionDTO    `json:"decision"`
}

type planRevisionDTO struct {
	ID                   string   `json:"id"`
	RunID                string   `json:"run_id"`
	Revision             int      `json:"revision"`
	Plan                 string   `json:"plan"`
	ContextDigest        string   `json:"context_digest"`
	RiskClass            string   `json:"risk_class"`
	VerificationStrategy string   `json:"verification_strategy"`
	ExpectedSideEffects  []string `json:"expected_side_effects"`
	RollbackApproach     string   `json:"rollback_approach"`
	SourceRevision       string   `json:"source_revision"`
	PolicyVersion        string   `json:"policy_version"`
	CreatedAt            int64    `json:"created_at"`
}

func (plan planRevisionDTO) toDomain() workcontrol.PlanRevision {
	return workcontrol.PlanRevision{
		ID: plan.ID, RunID: plan.RunID, Revision: plan.Revision, Plan: plan.Plan,
		ContextDigest: plan.ContextDigest, RiskClass: plan.RiskClass,
		VerificationStrategy: plan.VerificationStrategy, ExpectedSideEffects: append([]string(nil), plan.ExpectedSideEffects...),
		RollbackApproach: plan.RollbackApproach, SourceRevision: plan.SourceRevision,
		PolicyVersion: plan.PolicyVersion, CreatedAt: fromUnixMilli(plan.CreatedAt),
	}
}

type decisionDTO struct {
	ID                   string   `json:"id"`
	TaskID               string   `json:"task_id"`
	RunID                *string  `json:"run_id"`
	PlanRevisionID       string   `json:"plan_revision_id"`
	Question             string   `json:"question"`
	Options              []string `json:"options"`
	Recommendation       string   `json:"recommendation"`
	RiskClass            string   `json:"risk_class"`
	EvidenceSummary      string   `json:"evidence_summary"`
	EstimatedCostMinor   int64    `json:"estimated_cost_minor"`
	Currency             string   `json:"currency"`
	WaitingConsequence   string   `json:"waiting_consequence"`
	RejectingConsequence string   `json:"rejecting_consequence"`
	Status               string   `json:"status"`
	Answer               string   `json:"answer"`
	ExpiresAt            *int64   `json:"expires_at"`
	CreatedAt            int64    `json:"created_at"`
	AnsweredAt           *int64   `json:"answered_at"`
	AnsweredBy           string   `json:"answered_by"`
}

func (decision decisionDTO) toDomain() workcontrol.Decision {
	runID := ""
	if decision.RunID != nil {
		runID = *decision.RunID
	}
	return workcontrol.Decision{
		ID: decision.ID, TaskID: decision.TaskID, RunID: runID, PlanRevisionID: decision.PlanRevisionID,
		Question: decision.Question, Options: append([]string(nil), decision.Options...), Recommendation: decision.Recommendation,
		RiskClass: decision.RiskClass, EvidenceSummary: decision.EvidenceSummary, EstimatedCostMinor: decision.EstimatedCostMinor,
		Currency: decision.Currency, WaitingConsequence: decision.WaitingConsequence, RejectingConsequence: decision.RejectingConsequence,
		Status: workcontrol.DecisionStatus(decision.Status), Answer: decision.Answer, ExpiresAt: optionalUnixMilli(decision.ExpiresAt),
		CreatedAt: fromUnixMilli(decision.CreatedAt), AnsweredAt: optionalUnixMilli(decision.AnsweredAt), AnsweredBy: decision.AnsweredBy,
	}
}

type blockRequest struct {
	runGuardRequest
	Reason string `json:"reason"`
}

type agentDTO struct {
	ID             string   `json:"id"`
	InstallationID string   `json:"installation_id"`
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Status         string   `json:"status"`
	Capabilities   []string `json:"capabilities"`
	LastSeenAt     int64    `json:"last_seen_at"`
	CreatedAt      int64    `json:"created_at"`
}

func (agent agentDTO) toDomain() workcontrol.Agent {
	return workcontrol.Agent{
		ID:             agent.ID,
		InstallationID: agent.InstallationID,
		Name:           agent.Name,
		Kind:           agent.Kind,
		Status:         agent.Status,
		Capabilities:   append([]string(nil), agent.Capabilities...),
		LastSeenAt:     fromUnixMilli(agent.LastSeenAt),
		CreatedAt:      fromUnixMilli(agent.CreatedAt),
	}
}

type taskDTO struct {
	ID                 string    `json:"id"`
	ProjectID          string    `json:"project_id"`
	Title              string    `json:"title"`
	Description        string    `json:"description"`
	AcceptanceCriteria string    `json:"acceptance_criteria"`
	Status             string    `json:"status"`
	Priority           string    `json:"priority"`
	BudgetMinutes      int       `json:"budget_minutes"`
	Budget             budgetDTO `json:"budget"`
	Version            int       `json:"version"`
	CreatedAt          int64     `json:"created_at"`
	UpdatedAt          int64     `json:"updated_at"`
}

type budgetDTO struct {
	TimeMinutes    int      `json:"time_minutes"`
	DeadlineAt     *int64   `json:"deadline_at"`
	MaxAttempts    int      `json:"max_attempts"`
	MaxTokens      int64    `json:"max_tokens"`
	MaxCostMinor   int64    `json:"max_cost_minor"`
	Currency       string   `json:"currency"`
	MaxConcurrency int      `json:"max_concurrency"`
	Authority      []string `json:"authority"`
}

func (budget budgetDTO) toDomain() workcontrol.Budget {
	return workcontrol.Budget{TimeMinutes: budget.TimeMinutes, DeadlineAt: optionalUnixMilli(budget.DeadlineAt),
		MaxAttempts: budget.MaxAttempts, MaxTokens: budget.MaxTokens, MaxCostMinor: budget.MaxCostMinor,
		Currency: budget.Currency, MaxConcurrency: budget.MaxConcurrency, Authority: budget.Authority}
}

type taskDetailDTO struct {
	Task          taskDTO           `json:"task"`
	Runs          []runDTO          `json:"runs"`
	PlanRevisions []planRevisionDTO `json:"plan_revisions"`
	Decisions     []decisionDTO     `json:"decisions"`
}

func (detail taskDetailDTO) toDomain() workcontrol.WorkDetail {
	runs := make([]workcontrol.Run, 0, len(detail.Runs))
	for _, run := range detail.Runs {
		runs = append(runs, run.toDomain())
	}
	plans := make([]workcontrol.PlanRevision, 0, len(detail.PlanRevisions))
	for _, plan := range detail.PlanRevisions {
		plans = append(plans, plan.toDomain())
	}
	decisions := make([]workcontrol.Decision, 0, len(detail.Decisions))
	for _, decision := range detail.Decisions {
		decisions = append(decisions, decision.toDomain())
	}
	return workcontrol.WorkDetail{
		Work:          detail.Task.toDomain(),
		Runs:          runs,
		PlanRevisions: plans, Decisions: decisions,
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
		Budget:             task.Budget.toDomain(),
		Version:            task.Version,
		CreatedAt:          fromUnixMilli(task.CreatedAt),
		UpdatedAt:          fromUnixMilli(task.UpdatedAt),
	}
}

type runDTO struct {
	ID                       string `json:"id"`
	TaskID                   string `json:"task_id"`
	AgentID                  string `json:"agent_id"`
	Version                  int64  `json:"version"`
	FenceToken               int64  `json:"fence_token"`
	TaskVersion              int    `json:"task_version"`
	AcceptanceCriteriaDigest string `json:"acceptance_criteria_digest"`
	Status                   string `json:"status"`
	Plan                     string `json:"plan"`
	ActivePlanRevisionID     string `json:"active_plan_revision_id"`
	ResultSummary            string `json:"result_summary"`
	LeaseExpiresAt           *int64 `json:"lease_expires_at"`
	StartedAt                int64  `json:"started_at"`
	FinishedAt               *int64 `json:"finished_at"`
	UpdatedAt                int64  `json:"updated_at"`
}

func (run runDTO) toDomain() workcontrol.Run {
	return workcontrol.Run{
		ID:         run.ID,
		WorkItemID: run.TaskID,
		AgentID:    run.AgentID,
		Version:    run.Version, FenceToken: run.FenceToken, TaskVersion: run.TaskVersion,
		AcceptanceCriteriaDigest: run.AcceptanceCriteriaDigest,
		Status:                   workcontrol.RunStatus(run.Status),
		Plan:                     run.Plan,
		ActivePlanRevisionID:     run.ActivePlanRevisionID,
		ResultSummary:            run.ResultSummary,
		LeaseExpiresAt:           optionalUnixMilli(run.LeaseExpiresAt),
		StartedAt:                fromUnixMilli(run.StartedAt),
		FinishedAt:               optionalUnixMilli(run.FinishedAt),
		UpdatedAt:                fromUnixMilli(run.UpdatedAt),
	}
}

type activeRunDTO struct {
	Run  runDTO  `json:"run"`
	Task taskDTO `json:"task"`
}
type activeRunPageDTO struct {
	Items      []activeRunDTO `json:"items"`
	NextCursor string         `json:"next_cursor"`
}

type commandDTO struct {
	ID          string `json:"id"`
	RunID       string `json:"run_id"`
	Sequence    int64  `json:"sequence"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	RequestedBy string `json:"requested_by"`
	Result      string `json:"result"`
	RunVersion  int64  `json:"run_version"`
	FenceToken  int64  `json:"fence_token"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

func (command commandDTO) toDomain() workcontrol.Command {
	return workcontrol.Command{ID: command.ID, RunID: command.RunID,
		Sequence: command.Sequence, Type: command.Type, Status: command.Status, Reason: command.Reason,
		RequestedBy: command.RequestedBy, Result: command.Result, RunVersion: command.RunVersion,
		FenceToken: command.FenceToken, CreatedAt: fromUnixMilli(command.CreatedAt), UpdatedAt: fromUnixMilli(command.UpdatedAt)}
}

type commandPageDTO struct {
	Items []commandDTO `json:"items"`
}
type commandAckRequest struct {
	runGuardRequest
	Status string `json:"status"`
	Result string `json:"result"`
}
type projectedEventRequest struct {
	runGuardRequest
	Producer        string         `json:"producer"`
	ProducerEventID string         `json:"producer_event_id"`
	ExecutionID     string         `json:"execution_id"`
	Sequence        int64          `json:"sequence"`
	Type            string         `json:"type"`
	Message         string         `json:"message"`
	Payload         map[string]any `json:"payload"`
	OccurredAt      int64          `json:"occurred_at"`
}
