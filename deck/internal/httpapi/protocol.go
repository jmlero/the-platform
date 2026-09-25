package httpapi

const (
	protocolVersion = "1.0"
	providerName    = "deck"

	capabilityAgentRegister        = "agent.register"
	capabilityDecisionAnswer       = "decision.answer"
	capabilityDecisionInspect      = "decision.inspect"
	capabilityDecisionList         = "decision.list"
	capabilityDecisionRequest      = "decision.request"
	capabilityCommandAck           = "command.ack"
	capabilityCommandList          = "command.list"
	capabilityRepositoryRead       = "repository.read"
	capabilityRepositoryWrite      = "repository.write"
	capabilityContextRead          = "context.read"
	capabilityWorkloadPublish      = "workload.publish"
	capabilityWorkloadInvoke       = "workload.invoke"
	capabilityWorkloadDiscover     = "workload.discover"
	capabilityEventList            = "event.list"
	capabilityEventStream          = "event.stream"
	capabilityEventAppend          = "event.append"
	capabilityChannelList          = "channel.list"
	capabilityChannelMessage       = "channel.message.append"
	capabilityChannelRead          = "channel.message.list"
	capabilityConversationList     = "conversation.invocation.list"
	capabilityConversationDelegate = "conversation.task.delegate"
	capabilityConversationOutcome  = "conversation.outcome.publish"
	capabilityConversationQuestion = "conversation.question.write"
	capabilityEvidenceRecord       = "evidence.record"
	capabilityOperationReport      = "operation.report"
	capabilityProviderInfo         = "provider.info"
	capabilityRunBlock             = "run.block"
	capabilityRunComplete          = "run.complete"
	capabilityLeaseRenew           = "run.lease.renew"
	capabilityPlanSubmit           = "run.plan.submit"
	capabilityProgressAppend       = "run.progress.append"
	capabilityRunStart             = "run.start"
	capabilityRunVerify            = "run.verify"
	capabilityWorkClaim            = "work.claim.atomic"
	capabilityClaimLookup          = "work.claim.lookup"
	capabilityAssignedRuns         = "run.assigned.list"
	capabilityWorkInspect          = "work.inspect"
	capabilityWorkReadyList        = "work.ready.list"

	errorCodeConflict           = "conflict"
	errorCodeForbidden          = "forbidden"
	errorCodeInternal           = "internal_error"
	errorCodeInvalidInput       = "invalid_input"
	errorCodeInvalidTransition  = "invalid_transition"
	errorCodeNotFound           = "not_found"
	errorCodeServiceUnavailable = "service_unavailable"
	errorCodeUnauthenticated    = "unauthenticated"
)

// Capability identifiers describe protocol behavior, not implementation
// packages. Keep this list sorted so provider responses and contract fixtures
// remain deterministic.
var protocolCapabilities = []string{
	capabilityAgentRegister,
	capabilityChannelList,
	capabilityChannelMessage,
	capabilityChannelRead,
	capabilityCommandAck,
	capabilityCommandList,
	capabilityContextRead,
	capabilityConversationList,
	capabilityConversationOutcome,
	capabilityConversationQuestion,
	capabilityConversationDelegate,
	capabilityDecisionAnswer,
	capabilityDecisionInspect,
	capabilityDecisionList,
	capabilityDecisionRequest,
	capabilityEventAppend,
	capabilityEventList,
	capabilityEventStream,
	capabilityEvidenceRecord,
	capabilityOperationReport,
	capabilityProviderInfo,
	capabilityRepositoryRead,
	capabilityRepositoryWrite,
	capabilityAssignedRuns,
	capabilityRunBlock,
	capabilityRunComplete,
	capabilityLeaseRenew,
	capabilityPlanSubmit,
	capabilityProgressAppend,
	capabilityRunStart,
	capabilityRunVerify,
	capabilityWorkClaim,
	capabilityClaimLookup,
	capabilityWorkInspect,
	capabilityWorkReadyList,
	capabilityWorkloadDiscover,
	capabilityWorkloadInvoke,
	capabilityWorkloadPublish,
}

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
