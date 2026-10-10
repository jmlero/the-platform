package sourcecontrol

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

var ErrInvalidInput = errors.New("invalid source-control input")

type ChangeOperation string

const (
	ChangeCreate ChangeOperation = "create"
	ChangeUpdate ChangeOperation = "update"
	ChangeDelete ChangeOperation = "delete"
)

type Change struct {
	Path          string
	ContentDigest string
	Operation     ChangeOperation
	Mode          string
}

type BranchRequest struct {
	Repository     string
	Name           string
	BaseRevision   string
	IdempotencyKey string
	AuthorityFence int64
}

type WriteRequest struct {
	Repository     string
	Branch         string
	ExpectedHead   string
	IdempotencyKey string
	AuthorityFence int64
	Changes        []Change
}

func (request WriteRequest) Validate() error {
	if strings.TrimSpace(request.Repository) == "" || strings.TrimSpace(request.Branch) == "" ||
		strings.TrimSpace(request.ExpectedHead) == "" || strings.TrimSpace(request.IdempotencyKey) == "" ||
		request.AuthorityFence < 1 || len(request.Changes) == 0 {
		return fmt.Errorf("guarded write identity, expected head, fence, and changes are required: %w", ErrInvalidInput)
	}
	for _, change := range request.Changes {
		changePath := strings.TrimSpace(change.Path)
		if changePath == "" || path.IsAbs(changePath) || path.Clean(changePath) != changePath ||
			changePath == ".." || strings.HasPrefix(changePath, "../") || strings.ContainsRune(changePath, '\x00') ||
			(change.Operation != ChangeDelete && strings.TrimSpace(change.ContentDigest) == "") ||
			(change.Operation != ChangeCreate && change.Operation != ChangeUpdate && change.Operation != ChangeDelete) {
			return fmt.Errorf("source change is incomplete: %w", ErrInvalidInput)
		}
	}
	return nil
}

type PullRequestRequest struct {
	Repository     string
	BaseRef        string
	HeadRef        string
	ExpectedHead   string
	Title          string
	Body           string
	IdempotencyKey string
	AuthorityFence int64
}

type HeadRequest struct {
	Repository string
	Ref        string
}

// FastForwardRequest pins both refs so a retry cannot land a different commit.
type FastForwardRequest struct {
	Repository     string
	BaseRef        string
	ExpectedBase   string
	HeadRef        string
	ExpectedHead   string
	AuthorityFence int64
}

type ObservationRequest struct {
	Repository    string
	PullRequestID string
	HeadRevision  string
}

type Revision struct {
	Commit string
	Ref    string
}

type PullRequest struct {
	ID           string
	URL          string
	HeadRevision string
	State        string
}

type ReviewObservation struct {
	ID           string
	HeadRevision string
	State        string
	ObservedAt   time.Time
}

type CheckObservation struct {
	ID           string
	HeadRevision string
	State        string
	ObservedAt   time.Time
}

type Port interface {
	CreateBranch(context.Context, BranchRequest) (Revision, error)
	WriteChanges(context.Context, WriteRequest) (Revision, error)
	CreatePullRequest(context.Context, PullRequestRequest) (PullRequest, error)
	GetPullRequest(context.Context, ObservationRequest) (PullRequest, error)
	CurrentHead(context.Context, HeadRequest) (Revision, error)
	ObserveReviews(context.Context, ObservationRequest) ([]ReviewObservation, error)
	ObserveChecks(context.Context, ObservationRequest) ([]CheckObservation, error)
}

// ReviewSource supplies complete original files at the candidate's exact base.
// Missing files are explicit; transport failures must never look like additions.
type ReviewSource interface {
	ReadReviewFile(ctx context.Context, repository, revision, filename string) (content []byte, exists bool, err error)
}

// RepositoryReader exposes only pinned repository reads to conversation turns.
type RepositoryReader interface {
	ReviewSource
	CurrentHead(context.Context, HeadRequest) (Revision, error)
	ListRepositoryFiles(context.Context, string, string) ([]RepositoryFile, error)
}

type RepositoryFile struct {
	Path string `json:"path"`
	Size int    `json:"size"`
}

// LandingPort is used only by the main-landing workload. Legacy PR sources
// continue to implement Port without acquiring main-update authority.
type LandingPort interface {
	Port
	FastForward(context.Context, FastForwardRequest) (Revision, error)
}

// AncestryPort proves a recorded landed commit remains on a subsequently advanced base.
type AncestryPort interface {
	ContainsCommit(context.Context, string, string, string) (bool, error)
}
