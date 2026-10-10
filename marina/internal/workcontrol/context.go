package workcontrol

import (
	"context"
	"errors"
)

var ErrContextUnavailable = errors.New("context provider unavailable")

// Context references freeze the operator's selection, not authority. Source
// records and messages remain canonical; execution packages are derived copies.
type ContextReference struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
	Digest   string `json:"digest"`
}

type ContextSourceInput struct {
	ID               string   `json:"id"`
	ExpectedRevision int64    `json:"expected_revision"`
	Kind             string   `json:"kind"`
	Title            string   `json:"title"`
	Content          string   `json:"content,omitempty"`
	Reference        string   `json:"reference,omitempty"`
	SourceRevision   string   `json:"source_revision,omitempty"`
	Path             string   `json:"path,omitempty"`
	Trust            string   `json:"trust"`
	State            string   `json:"state"`
	ExpiresAt        int64    `json:"expires_at,omitempty"`
	ConflictsWith    []string `json:"conflicts_with,omitempty"`
}

type ContextSource struct {
	ContextSourceInput
	ProjectID  string `json:"project_id"`
	Revision   int64  `json:"revision"`
	Digest     string `json:"digest"`
	CreatedAt  int64  `json:"created_at"`
	RevokedAt  int64  `json:"revoked_at,omitempty"`
	Superseded bool   `json:"superseded"`
}

type TaskContext struct {
	Repository   *RepositoryBinding `json:"repository,omitempty"`
	Sources      []ContextReference `json:"sources"`
	Messages     []ContextReference `json:"messages"`
	AuthorizedAt int64              `json:"authorized_at"`
}

type ContextMessage struct {
	ID         string `json:"id"`
	ChannelID  string `json:"channel_id"`
	AuthorType string `json:"author_type"`
	AuthorID   string `json:"author_id"`
	Content    string `json:"content"`
	Digest     string `json:"digest"`
	Trust      string `json:"trust"`
	CreatedAt  int64  `json:"created_at"`
}

type ResolvedContext struct {
	Selection *TaskContext     `json:"selection"`
	Sources   []ContextSource  `json:"sources"`
	Messages  []ContextMessage `json:"messages"`
}

// ContextInspection resolves only the exact source revisions captured for a run.
type ContextInspection interface {
	GetRunContext(context.Context, string) (ResolvedContext, error)
}

// ConversationContextInspection resolves the exact project sources captured at
// invocation. A later edit cannot change them; revocation remains observable.
type ConversationContextInspection interface {
	GetConversationContext(context.Context, string) (ResolvedContext, error)
}
