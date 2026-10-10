package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const diagnosticSchemaVersion = "marina-diagnostics/v1"

type diagnosticExport struct {
	ConversationTurns []diagnosticConversationTurn `json:"conversation_turns"`
	ModelCalls        []diagnosticModelCall        `json:"model_calls"`
	ToolDecisions     []diagnosticToolDecision     `json:"tool_decisions"`
	RuntimeArtifacts  []diagnosticArtifact         `json:"runtime_artifacts"`
	SchemaVersion     string                       `json:"schema_version"`
	GeneratedAt       time.Time                    `json:"generated_at"`
	Counts            map[string]int64             `json:"counts"`
	Executions        []diagnosticExecution        `json:"executions"`
	Checkpoints       []diagnosticCheckpoint       `json:"checkpoints"`
	Evidence          []diagnosticEvidence         `json:"evidence"`
	Events            []diagnosticEvent            `json:"events"`
	RuntimeAttempts   []diagnosticRuntimeAttempt   `json:"runtime_attempts"`
	ContextPackages   []diagnosticContextPackage   `json:"context_packages"`
	PolicySnapshots   []diagnosticPolicySnapshot   `json:"policy_snapshots"`
	Signals           []diagnosticSignal           `json:"signals"`
}

// Conversation diagnostics expose durable accounting and publication identity,
// never conversation text, prompts, or the engine's private configuration.
type diagnosticConversationTurn struct {
	MessageID           string `json:"message_id"`
	ProjectID           string `json:"project_id"`
	ChannelID           string `json:"channel_id"`
	ThreadID            string `json:"thread_id"`
	AgentID             string `json:"agent_id"`
	Status              string `json:"status"`
	SpecificationDigest string `json:"specification_digest"`
	ResultDigest        string `json:"result_digest"`
	DeadlineAt          int64  `json:"deadline_at_unix_milli"`
	MaxTokens           int64  `json:"max_tokens"`
	ReservedTokens      int64  `json:"reserved_tokens"`
	UsedTokens          int64  `json:"used_tokens"`
	ModelCallStatus     string `json:"model_call_status"`
	PublishedMessageID  string `json:"published_message_id,omitempty"`
	PublishedTaskID     string `json:"published_task_id,omitempty"`
}

type diagnosticExecution struct {
	ID                 string `json:"id"`
	ProjectID          string `json:"project_id"`
	WorkProvider       string `json:"work_provider"`
	ExternalWorkID     string `json:"external_work_id"`
	ExternalRunID      string `json:"external_run_id"`
	State              string `json:"state"`
	Attempt            int    `json:"attempt"`
	UsedTokens         int64  `json:"used_tokens"`
	UsedCostMicros     int64  `json:"used_cost_micros"`
	PendingModelCalls  int64  `json:"pending_model_calls"`
	ReservedTokens     int64  `json:"reserved_tokens"`
	ReservedCostMicros int64  `json:"reserved_cost_micros"`
	FailureClass       string `json:"failure_class,omitempty"`
	FailureMessage     string `json:"failure_message,omitempty"`
	LastErrorClass     string `json:"last_error_class,omitempty"`
	ExhaustionReason   string `json:"exhaustion_reason,omitempty"`
	Version            int64  `json:"version"`
	UpdatedAtUnixMilli int64  `json:"updated_at_unix_milli"`
}

type diagnosticCheckpoint struct {
	ID             int64          `json:"id"`
	ExecutionID    string         `json:"execution_id"`
	Key            string         `json:"key"`
	Phase          string         `json:"phase"`
	SourceRevision string         `json:"source_revision,omitempty"`
	Payload        map[string]any `json:"payload"`
	CreatedAt      int64          `json:"created_at_unix_milli"`
}

type diagnosticEvidence struct {
	ID             int64  `json:"id"`
	ExecutionID    string `json:"execution_id"`
	Key            string `json:"key"`
	Kind           string `json:"kind"`
	SourceRevision string `json:"source_revision"`
	Environment    string `json:"environment,omitempty"`
	Command        string `json:"command,omitempty"`
	Summary        string `json:"summary"`
	URI            string `json:"uri,omitempty"`
	ObservedAt     int64  `json:"observed_at_unix_milli"`
}

type diagnosticEvent struct {
	ID          int64          `json:"id"`
	ExecutionID string         `json:"execution_id"`
	Type        string         `json:"type"`
	Message     string         `json:"message"`
	Payload     map[string]any `json:"payload"`
	CreatedAt   int64          `json:"created_at_unix_milli"`
}

type diagnosticRuntimeAttempt struct {
	Workload            string `json:"workload"`
	ProfileDigest       string `json:"profile_digest,omitempty"`
	ContextDigest       string `json:"context_digest"`
	ID                  string `json:"id"`
	ExecutionID         string `json:"execution_id"`
	AttemptNumber       int    `json:"attempt_number"`
	SpecificationDigest string `json:"specification_digest"`
	ProviderHandle      string `json:"provider_handle,omitempty"`
	State               string `json:"state"`
	DeadlineAt          int64  `json:"deadline_at_unix_milli"`
	CleanupState        string `json:"cleanup_state"`
	OutputsCollected    bool   `json:"outputs_collected"`
	FailureClass        string `json:"failure_class,omitempty"`
	FailureMessage      string `json:"failure_message,omitempty"`
	Version             int64  `json:"version"`
}

type diagnosticContextPackage struct {
	Digest         string `json:"digest"`
	ExecutionID    string `json:"execution_id"`
	PlanRevisionID string `json:"plan_revision_id"`
	PolicyVersion  string `json:"policy_version"`
	SourceRevision string `json:"source_revision"`
	CreatedAt      int64  `json:"created_at_unix_milli"`
}

type diagnosticPolicySnapshot struct {
	ProjectID   string `json:"project_id"`
	Version     string `json:"version"`
	ChangeClass string `json:"change_class"`
	Environment string `json:"environment"`
	RiskClass   string `json:"risk_class"`
	Digest      string `json:"digest"`
	CreatedAt   int64  `json:"created_at_unix_milli"`
}

type diagnosticSignal struct {
	ID             string  `json:"id"`
	ProjectID      string  `json:"project_id"`
	Source         string  `json:"source"`
	Type           string  `json:"type"`
	Classification string  `json:"classification"`
	Severity       string  `json:"severity"`
	CorrelationID  string  `json:"correlation_id"`
	Confidence     float64 `json:"confidence"`
	ObservedAt     int64   `json:"observed_at_unix_milli"`
}

func (store *Store) ExportDiagnostics(ctx context.Context) ([]byte, error) {
	export := diagnosticExport{
		SchemaVersion: diagnosticSchemaVersion, GeneratedAt: store.now().UTC(), Counts: make(map[string]int64),
		Executions: []diagnosticExecution{}, Checkpoints: []diagnosticCheckpoint{}, Evidence: []diagnosticEvidence{},
		Events: []diagnosticEvent{}, RuntimeAttempts: []diagnosticRuntimeAttempt{}, ContextPackages: []diagnosticContextPackage{},
		PolicySnapshots: []diagnosticPolicySnapshot{}, Signals: []diagnosticSignal{},
		ConversationTurns: []diagnosticConversationTurn{},
	}
	for _, table := range []string{
		"executions", "checkpoints", "evidence", "events", "acquisition_intents", "provider_outbox",
		"runtime_attempts", "runtime_artifacts", "context_packages", "policy_snapshots", "validated_signals",
		"conversation_turns", "conversation_engine_attempts", "conversation_artifacts",
	} {
		var count int64
		if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			return nil, fmt.Errorf("count diagnostic table %s: %w", table, err)
		}
		export.Counts[table] = count
	}
	loaders := []func(context.Context, *diagnosticExport) error{
		store.exportDiagnosticExecutions, store.exportDiagnosticCheckpoints, store.exportDiagnosticEvidence,
		store.exportDiagnosticEvents, store.exportDiagnosticRuntimeAttempts, store.exportDiagnosticContextPackages,
		store.exportDiagnosticPolicies, store.exportDiagnosticSignals, store.exportDiagnosticModelCalls, store.exportDiagnosticToolDecisions, store.exportDiagnosticArtifacts,
		store.exportDiagnosticConversationTurns,
	}
	for _, load := range loaders {
		if err := load(ctx, &export); err != nil {
			return nil, err
		}
	}
	encoded, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode diagnostic export: %w", err)
	}
	return append(encoded, '\n'), nil
}

func (store *Store) exportDiagnosticConversationTurns(ctx context.Context, export *diagnosticExport) error {
	rows, err := store.db.QueryContext(ctx, `SELECT message_id, project_id, channel_id, thread_id, agent_id, status,
		specification_digest, result_digest, deadline_at, max_tokens, reserved_tokens, used_tokens, model_call_status,
		published_message_id, published_task_id FROM conversation_turns ORDER BY message_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item diagnosticConversationTurn
		if err := rows.Scan(&item.MessageID, &item.ProjectID, &item.ChannelID, &item.ThreadID, &item.AgentID, &item.Status,
			&item.SpecificationDigest, &item.ResultDigest, &item.DeadlineAt, &item.MaxTokens, &item.ReservedTokens,
			&item.UsedTokens, &item.ModelCallStatus, &item.PublishedMessageID, &item.PublishedTaskID); err != nil {
			return err
		}
		export.ConversationTurns = append(export.ConversationTurns, item)
	}
	return rows.Err()
}

func (store *Store) exportDiagnosticExecutions(ctx context.Context, export *diagnosticExport) error {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, project_id, work_provider, external_work_id, external_run_id, state, attempt,
			used_tokens, used_cost_micros, failure_class, failure_message, last_error_class,
			exhaustion_reason, version, updated_at,
			(SELECT COUNT(*) FROM model_calls WHERE execution_id = executions.id AND finished_at IS NULL),
			(SELECT COALESCE(SUM(reserved_tokens), 0) FROM model_calls WHERE execution_id = executions.id AND finished_at IS NULL),
			(SELECT COALESCE(SUM(reserved_cost_micros), 0) FROM model_calls WHERE execution_id = executions.id AND finished_at IS NULL)
		FROM executions ORDER BY id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item diagnosticExecution
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.WorkProvider, &item.ExternalWorkID, &item.ExternalRunID,
			&item.State, &item.Attempt, &item.UsedTokens, &item.UsedCostMicros, &item.FailureClass,
			&item.FailureMessage, &item.LastErrorClass, &item.ExhaustionReason, &item.Version, &item.UpdatedAtUnixMilli,
			&item.PendingModelCalls, &item.ReservedTokens, &item.ReservedCostMicros); err != nil {
			return err
		}
		item.FailureMessage = omitFreeText(item.FailureMessage)
		item.ExhaustionReason = omitFreeText(item.ExhaustionReason)
		export.Executions = append(export.Executions, item)
	}
	return rows.Err()
}

func (store *Store) exportDiagnosticCheckpoints(ctx context.Context, export *diagnosticExport) error {
	rows, err := store.db.QueryContext(ctx, `SELECT id, execution_id, checkpoint_key, phase, source_revision, payload_json, created_at FROM checkpoints ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item diagnosticCheckpoint
		var payloadJSON string
		if err := rows.Scan(&item.ID, &item.ExecutionID, &item.Key, &item.Phase, &item.SourceRevision, &payloadJSON, &item.CreatedAt); err != nil {
			return err
		}
		item.Payload = sanitizedObject(payloadJSON)
		export.Checkpoints = append(export.Checkpoints, item)
	}
	return rows.Err()
}

func (store *Store) exportDiagnosticEvidence(ctx context.Context, export *diagnosticExport) error {
	rows, err := store.db.QueryContext(ctx, `SELECT id, execution_id, evidence_key, kind, source_revision, environment, command, summary, uri, observed_at FROM evidence ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item diagnosticEvidence
		if err := rows.Scan(&item.ID, &item.ExecutionID, &item.Key, &item.Kind, &item.SourceRevision, &item.Environment, &item.Command, &item.Summary, &item.URI, &item.ObservedAt); err != nil {
			return err
		}
		item.Command, item.Summary, item.URI = omitFreeText(item.Command), omitFreeText(item.Summary), sanitizeText(item.URI)
		export.Evidence = append(export.Evidence, item)
	}
	return rows.Err()
}

func (store *Store) exportDiagnosticEvents(ctx context.Context, export *diagnosticExport) error {
	rows, err := store.db.QueryContext(ctx, `SELECT id, execution_id, type, message, payload_json, created_at FROM events ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item diagnosticEvent
		var payloadJSON string
		if err := rows.Scan(&item.ID, &item.ExecutionID, &item.Type, &item.Message, &payloadJSON, &item.CreatedAt); err != nil {
			return err
		}
		item.Message, item.Payload = omitFreeText(item.Message), sanitizedObject(payloadJSON)
		export.Events = append(export.Events, item)
	}
	return rows.Err()
}

func (store *Store) exportDiagnosticRuntimeAttempts(ctx context.Context, export *diagnosticExport) error {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, execution_id, attempt_number, specification_digest, provider_handle, state,
			deadline_at, cleanup_state, outputs_collected, failure_class, failure_message, version, specification_json
		FROM runtime_attempts ORDER BY execution_id, attempt_number
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item diagnosticRuntimeAttempt
		var outputsCollected int
		var specification string
		if err := rows.Scan(&item.ID, &item.ExecutionID, &item.AttemptNumber, &item.SpecificationDigest,
			&item.ProviderHandle, &item.State, &item.DeadlineAt, &item.CleanupState, &outputsCollected,
			&item.FailureClass, &item.FailureMessage, &item.Version, &specification); err != nil {
			return err
		}
		var spec struct {
			Workload      string `json:"workload"`
			ProfileDigest string `json:"profile_digest"`
			ContextDigest string `json:"context_digest"`
		}
		if err = json.Unmarshal([]byte(specification), &spec); err != nil {
			return err
		}
		item.Workload = spec.Workload
		if item.Workload == "" {
			item.Workload = "repository/v1"
		}
		item.ProfileDigest = spec.ProfileDigest
		item.ContextDigest = spec.ContextDigest
		item.OutputsCollected = outputsCollected == 1
		item.ProviderHandle, item.FailureMessage = diagnosticFingerprint(item.ProviderHandle), omitFreeText(item.FailureMessage)
		export.RuntimeAttempts = append(export.RuntimeAttempts, item)
	}
	return rows.Err()
}

func (store *Store) exportDiagnosticContextPackages(ctx context.Context, export *diagnosticExport) error {
	rows, err := store.db.QueryContext(ctx, `SELECT digest, execution_id, plan_revision_id, policy_version, source_revision, created_at FROM context_packages ORDER BY digest`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item diagnosticContextPackage
		if err := rows.Scan(&item.Digest, &item.ExecutionID, &item.PlanRevisionID, &item.PolicyVersion, &item.SourceRevision, &item.CreatedAt); err != nil {
			return err
		}
		export.ContextPackages = append(export.ContextPackages, item)
	}
	return rows.Err()
}

func (store *Store) exportDiagnosticPolicies(ctx context.Context, export *diagnosticExport) error {
	rows, err := store.db.QueryContext(ctx, `SELECT project_id, version, change_class, environment, risk_class, digest, created_at FROM policy_snapshots ORDER BY project_id, version, change_class, environment, risk_class`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item diagnosticPolicySnapshot
		if err := rows.Scan(&item.ProjectID, &item.Version, &item.ChangeClass, &item.Environment, &item.RiskClass, &item.Digest, &item.CreatedAt); err != nil {
			return err
		}
		export.PolicySnapshots = append(export.PolicySnapshots, item)
	}
	return rows.Err()
}

func (store *Store) exportDiagnosticSignals(ctx context.Context, export *diagnosticExport) error {
	rows, err := store.db.QueryContext(ctx, `SELECT id, project_id, source, signal_type, classification, severity, correlation_id, validated_json, observed_at FROM validated_signals ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item diagnosticSignal
		var validatedJSON string
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.Source, &item.Type, &item.Classification, &item.Severity, &item.CorrelationID, &validatedJSON, &item.ObservedAt); err != nil {
			return err
		}
		var envelope struct {
			Confidence float64 `json:"confidence"`
		}
		_ = json.Unmarshal([]byte(validatedJSON), &envelope)
		item.Confidence = envelope.Confidence
		export.Signals = append(export.Signals, item)
	}
	return rows.Err()
}

var (
	secretAssignmentPattern = regexp.MustCompile(`(?i)(authorization|api[_-]?key|access[_-]?token|refresh[_-]?token|token|secret|password|cookie)(\s*[:=]\s*)([^\s,;]+)`)
	bearerPattern           = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)
)

func sanitizedObject(encoded string) map[string]any {
	var object map[string]any
	if json.Unmarshal([]byte(encoded), &object) != nil || object == nil {
		return map[string]any{"decode_error": true}
	}
	return sanitizeMap(object)
}

func sanitizeMap(object map[string]any) map[string]any {
	result := make(map[string]any, len(object))
	for key, value := range object {
		if sensitiveKey(key) {
			result[key] = "[REDACTED]"
			continue
		}
		result[key] = sanitizeValue(value, safeDiagnosticStringKey(key))
	}
	return result
}

func sanitizeValue(value any, allowStrings bool) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizeMap(typed)
	case []any:
		result := make([]any, len(typed))
		for index := range typed {
			result[index] = sanitizeValue(typed[index], allowStrings)
		}
		return result
	case string:
		if !allowStrings {
			return "[REDACTED]"
		}
		return sanitizeText(typed)
	default:
		return value
	}
}

func safeDiagnosticStringKey(key string) bool {
	lower := strings.ToLower(key)
	if strings.HasSuffix(lower, "_id") || strings.HasSuffix(lower, "_ids") ||
		strings.HasSuffix(lower, "_version") || strings.HasSuffix(lower, "_status") ||
		strings.HasSuffix(lower, "_digest") || strings.HasSuffix(lower, "_revision") {
		return true
	}
	switch lower {
	case "id", "status", "state", "phase", "kind", "type", "action", "classification", "revision":
		return true
	default:
		return false
	}
}

func sensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, fragment := range []string{"token", "secret", "password", "credential", "authorization", "cookie", "api_key", "apikey"} {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

func sanitizeText(value string) string {
	trimmed := strings.TrimSpace(value)
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		parsed.User, parsed.RawQuery, parsed.Fragment = nil, "", ""
		trimmed = parsed.String()
	}
	trimmed = secretAssignmentPattern.ReplaceAllString(trimmed, `$1$2[REDACTED]`)
	return bearerPattern.ReplaceAllString(trimmed, "Bearer [REDACTED]")
}

func omitFreeText(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "[OMITTED]"
}

func diagnosticFingerprint(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}
