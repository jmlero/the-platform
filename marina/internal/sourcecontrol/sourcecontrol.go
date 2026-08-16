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
	CurrentHead(context.Context, HeadRequest) (Revision, error)
	ObserveReviews(context.Context, ObservationRequest) ([]ReviewObservation, error)
	ObserveChecks(context.Context, ObservationRequest) ([]CheckObservation, error)
}
