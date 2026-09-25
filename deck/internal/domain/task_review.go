package domain

// TaskReview is human acceptance of one result, separate from v1 execution status.
// SourceAvailable is an explicit operator attestation, not a Git-provider observation.
type TaskReview struct {
	RunID           string `json:"run_id"`
	AcceptedBy      string `json:"accepted_by"`
	AcceptedAt      int64  `json:"accepted_at"`
	TaskVersion     int    `json:"task_version"`
	SourceAvailable bool   `json:"source_available"`
}
