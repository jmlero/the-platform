package worker

import (
	"github.com/jmlero/the-platform/marina/internal/contextpack"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

const MaxResultBytes = workresult.MaxResultBytes

type Check = workresult.Check
type Change = workresult.Change
type CheckResult = workresult.CheckResult
type Result = workresult.Result

type Input struct {
	PRD                *workcontrol.PRDDocument `json:"prd,omitempty"`
	Rebase             *Rebase                  `json:"rebase,omitempty"`
	Context            *contextpack.Package     `json:"context,omitempty"`
	Stage              string                   `json:"stage"`
	SourceURL          string                   `json:"source_url"`
	SourceRevision     string                   `json:"source_revision"`
	Title              string                   `json:"title"`
	Description        string                   `json:"description"`
	AcceptanceCriteria string                   `json:"acceptance_criteria"`
	Feedback           string                   `json:"feedback,omitempty"`
	Repair             *workresult.RepairPlan   `json:"repair,omitempty"`
	Candidate          *workresult.Candidate    `json:"candidate,omitempty"`
	CandidateDigest    string                   `json:"candidate_digest,omitempty"`
	ProfileDigest      string                   `json:"review_profile_digest,omitempty"`
	Before             []workresult.BaseFile    `json:"before,omitempty"`
	AllowedPaths       []string                 `json:"allowed_paths"`
	Checks             []Check                  `json:"checks"`
	Environment        []string                 `json:"-"`
	GitAuthorization   string                   `json:"-"`
}

// Rebase pins the original task history and the new base for a native Git rebase.
type Rebase struct {
	BaseRevision string `json:"base_revision"`
	HeadRevision string `json:"head_revision"`
	Branch       string `json:"branch"`
}

func AllowedPath(scopes []string, name string) bool { return workresult.AllowedPath(scopes, name) }
