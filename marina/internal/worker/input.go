package worker

import (
	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/jmlero/the-platform/marina/internal/contextpack"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

const MaxResultBytes = workresult.MaxResultBytes

type Check = workresult.Check
type Change = workresult.Change
type CheckResult = workresult.CheckResult
type Result = workresult.Result

type Input struct {
	Analysis           *analysis.Input      `json:"analysis,omitempty"`
	Context            *contextpack.Package `json:"context,omitempty"`
	Stage              string               `json:"stage"`
	SourceURL          string               `json:"source_url"`
	SourceRevision     string               `json:"source_revision"`
	Title              string               `json:"title"`
	Description        string               `json:"description"`
	AcceptanceCriteria string               `json:"acceptance_criteria"`
	Feedback           string               `json:"feedback,omitempty"`
	AllowedPaths       []string             `json:"allowed_paths"`
	Checks             []Check              `json:"checks"`
	Harness            []string             `json:"harness,omitempty"`
	Environment        []string             `json:"-"`
	GitAuthorization   string               `json:"-"`
}

func AllowedPath(scopes []string, name string) bool { return workresult.AllowedPath(scopes, name) }
