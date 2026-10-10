package domain

// LandingObservation is Marina's source-provider observation, not human acceptance.
type LandingObservation struct {
	Repository string `json:"repository"`
	BaseBranch string `json:"base_branch"`
	Commit     string `json:"commit"`
	ObservedAt int64  `json:"observed_at"`
}

// TaskLanding retains the completed run's exact landing and selected evidence.
type TaskLanding struct {
	LandingObservation
	RunID       string   `json:"run_id"`
	EvidenceIDs []string `json:"evidence_ids"`
}
