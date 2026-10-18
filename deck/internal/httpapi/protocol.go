package httpapi

const (
	protocolVersion = "1.0"

	errorCodeConflict           = "conflict"
	errorCodeForbidden          = "forbidden"
	errorCodeInternal           = "internal_error"
	errorCodeInvalidInput       = "invalid_input"
	errorCodeInvalidTransition  = "invalid_transition"
	errorCodeNotFound           = "not_found"
	errorCodeServiceUnavailable = "service_unavailable"
	errorCodeUnauthenticated    = "unauthenticated"
)

// Error classifications are part of the versioned wire contract. Every code
// must have a canonical JSON fixture so consumers can test decoding and retry
// behavior without depending on a particular failure message.
var protocolErrorCodes = []string{
	errorCodeConflict,
	errorCodeForbidden,
	errorCodeInternal,
	errorCodeInvalidInput,
	errorCodeInvalidTransition,
	errorCodeNotFound,
	errorCodeServiceUnavailable,
	errorCodeUnauthenticated,
}

type apiErrorResponse struct {
	// Error remains as a compatibility alias for the prototype UI and clients.
	Error     string `json:"error"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	RequestID string `json:"request_id"`
}
