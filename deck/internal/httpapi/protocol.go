package httpapi

const (
	protocolVersion = "1.0"
	providerName    = "appdeck"

	errorCodeConflict          = "conflict"
	errorCodeInternal          = "internal_error"
	errorCodeInvalidInput      = "invalid_input"
	errorCodeInvalidTransition = "invalid_transition"
	errorCodeNotFound          = "not_found"
)

// Capability identifiers describe protocol behavior, not implementation
// packages. Keep this list sorted so provider responses and contract fixtures
// remain deterministic.
var protocolCapabilities = []string{
	"agent.register",
	"decision.answer",
	"decision.inspect",
	"decision.list",
	"event.list",
	"event.stream",
	"provider.info",
	"run.block",
	"run.complete",
	"run.lease.renew",
	"run.plan.submit",
	"run.progress.append",
	"run.start",
	"run.verify",
	"work.claim.atomic",
	"work.inspect",
	"work.ready.list",
}

// Error classifications are part of the versioned wire contract. Every code
// must have a canonical JSON fixture so consumers can test decoding and retry
// behavior without depending on a particular failure message.
var protocolErrorCodes = []string{
	errorCodeConflict,
	errorCodeInternal,
	errorCodeInvalidInput,
	errorCodeInvalidTransition,
	errorCodeNotFound,
}

type providerInfoResponse struct {
	Provider        string            `json:"provider"`
	ProtocolVersion string            `json:"protocol_version"`
	Capabilities    []string          `json:"capabilities"`
	ServerTime      int64             `json:"server_time"`
	Ready           bool              `json:"ready"`
	Readiness       map[string]string `json:"readiness"`
}

type apiErrorResponse struct {
	// Error remains as a compatibility alias for the prototype UI and clients.
	Error     string `json:"error"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	RequestID string `json:"request_id"`
}
