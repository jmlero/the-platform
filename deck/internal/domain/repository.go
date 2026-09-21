package domain

// RepositoryBinding is a safe projection of Marina-owned connection settings.
// Credential references and execution policy never enter Deck.
type RepositoryBinding struct {
	Version    int64  `json:"version"`
	Repository string `json:"repository"`
	CloneURL   string `json:"clone_url"`
	BaseBranch string `json:"base_branch"`
}
type RepositoryRequestInput struct {
	ExpectedRevision       int64  `json:"expected_revision"`
	ExpectedBindingVersion int64  `json:"expected_binding_version"`
	Repository             string `json:"repository"`
	BaseBranch             string `json:"base_branch"`
}
type RepositoryRequest struct {
	RepositoryRequestInput
	Revision int64              `json:"revision"`
	Status   string             `json:"status"`
	Reason   string             `json:"reason,omitempty"`
	Binding  *RepositoryBinding `json:"binding,omitempty"`
}
type RepositoryResult struct {
	RequestRevision int64              `json:"request_revision"`
	Status          string             `json:"status"`
	Reason          string             `json:"reason,omitempty"`
	Binding         *RepositoryBinding `json:"binding,omitempty"`
}
