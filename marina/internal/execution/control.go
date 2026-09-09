package execution

import "time"

type InstallationIdentity struct {
	InstallationID string
	LogicalAgentID string
	CreatedAt      time.Time
}

type ProjectBinding struct {
	ProjectID      string
	Version        int64
	Provider       string
	Repository     string
	Runtime        string
	PolicyVersion  string
	ContextConfig  string
	ArtifactConfig string
	DeliveryConfig string
	BudgetMinutes  int
	MaxConcurrency int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type NewAcquisitionIntent struct {
	TaskSnapshot             string
	ProjectID                string
	WorkID                   string
	TaskVersion              int
	TaskDigest               string
	AcceptanceCriteriaDigest string
	ClaimKey                 string
	AgentID                  string
	LeaseSeconds             int
	BudgetMinutes            int
}

type AcquisitionIntent struct {
	TaskSnapshot             string
	ID                       string
	ProjectID                string
	WorkID                   string
	TaskVersion              int
	TaskDigest               string
	AcceptanceCriteriaDigest string
	ClaimKey                 string
	AgentID                  string
	LeaseSeconds             int
	BudgetMinutes            int
	Status                   string
	ExternalRunID            string
	ExternalVersion          int64
	FenceToken               int64
	LeaseExpiresAt           *time.Time
	RetryAttempt             int
	NextEligibleAt           *time.Time
	LastErrorClass           FailureClass
	ExhaustionReason         string
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type OutboxInput struct {
	Kind           string
	IdempotencyKey string
	Payload        map[string]any
}

type OutboxItem struct {
	ID               string
	ExecutionID      string
	Sequence         int64
	Kind             string
	IdempotencyKey   string
	Payload          map[string]any
	Status           string
	Attempts         int
	NextEligibleAt   *time.Time
	LastErrorClass   FailureClass
	ExhaustionReason string
	CreatedAt        time.Time
	PublishedAt      *time.Time
}
