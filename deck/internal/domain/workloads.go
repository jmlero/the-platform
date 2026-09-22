package domain

// WorkloadProfile is Marina's safe projection of installed configuration.
// It contains no endpoints or credential references and grants no authority.
type WorkloadProfile struct {
	ID            string   `json:"id"`
	Digest        string   `json:"digest"`
	Harness       string   `json:"harness"`
	Authority     []string `json:"authority"`
	MaxTokens     int64    `json:"max_tokens"`
	BudgetMinutes int      `json:"budget_minutes"`
	MinAttempts   int      `json:"min_attempts"`
	Tools         []string `json:"tools"`
	ContextKinds  []string `json:"context_kinds"`
}
type WorkloadCatalog struct {
	Profiles  []WorkloadProfile `json:"profiles"`
	ExpiresAt int64             `json:"expires_at"`
}
type WorkloadInvocation struct {
	ProfileID     string          `json:"profile_id"`
	ProfileDigest string          `json:"profile_digest"`
	Task          CreateTaskInput `json:"task"`
}
