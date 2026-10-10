package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// EngineSelection is part of the immutable runtime specification. Never resolve
// a moving template during recovery to replace this authorized revision.
type EngineSelection struct {
	Backend   string `json:"backend"`
	Version   string `json:"version"`
	Endpoint  string `json:"endpoint"`
	Namespace string `json:"namespace"`
	Harness   string `json:"harness"`
	Template  string `json:"template"`
	Revision  string `json:"revision"`
}

func validEngineDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && strings.ToLower(s) == s
}

var engineResourceName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func (e EngineSelection) Validate() error {
	if !validEngineDigest(e.Revision) {
		return fmt.Errorf("invalid engine selection: %w", ErrInvalidInput)
	}
	if err := e.profile(); err != nil {
		return err
	}
	if len(e.Template) > 63 || !engineResourceName.MatchString(e.Template) {
		return ErrInvalidInput
	}
	return nil
}

// ValidateProfile checks an installed selection whose template and revision
// Marina provisions for each attempt. A fixed template would serve two attempts.
func (e EngineSelection) ValidateProfile() error {
	if e.Template != "" || e.Revision != "" {
		return fmt.Errorf("installed engine profile cannot name a prepared template: %w", ErrInvalidInput)
	}
	return e.profile()
}

func (e EngineSelection) profile() error {
	if e.Backend != "kagent" {
		return fmt.Errorf("invalid engine selection: %w", ErrInvalidInput)
	}
	for _, s := range []string{e.Version, e.Endpoint, e.Namespace, e.Harness} {
		if strings.TrimSpace(s) == "" {
			return ErrInvalidInput
		}
	}
	for _, name := range []string{e.Namespace, e.Harness} {
		if len(name) > 63 || !engineResourceName.MatchString(name) {
			return ErrInvalidInput
		}
	}
	return nil
}

// ErrEngineTemplatePending means the engine has not prepared an attempt's
// template yet; the caller retries on a later cycle.
var ErrEngineTemplatePending = errors.New("engine template is not prepared yet")

// EngineTemplate is the attempt-exclusive engine configuration Marina creates.
// Its name also names the Secret that carries the attempt's gateway credential.
type EngineTemplate struct {
	Namespace    string
	Name         string
	Harness      string
	Description  string
	SystemPrompt string
	// Model only labels the request; Marina's gateway pins the upstream model.
	Model   string
	BaseURL string
}

// EngineTemplates prepares and removes attempt-exclusive engine templates.
type EngineTemplates interface {
	EnsureEngineTemplate(context.Context, EngineTemplate) (revision string, err error)
	DeleteEngineTemplate(ctx context.Context, namespace, name string) error
}

// EngineTemplateName derives an attempt-exclusive name from a durable attempt
// key, so a restarted cycle finds the resources an earlier cycle created.
func EngineTemplateName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return EngineTemplatePrefix + hex.EncodeToString(sum[:8])
}

// EngineTemplatePrefix marks the only engine resources Marina creates or deletes.
const EngineTemplatePrefix = "marina-"

// Prepared returns the selection one attempt uses with its prepared template.
func (e EngineSelection) Prepared(template, revision string) EngineSelection {
	e.Template, e.Revision = template, revision
	return e
}

// EngineGatewayReference binds the only credential installed for governed
// execution. The Secret is named after the attempt-exclusive AgentTemplate;
// neither a shared credential nor an upstream credential belongs in an agent.
func (s Specification) EngineGatewayReference() (CredentialReference, error) {
	if s.Engine == nil || len(s.CredentialReferences) != 1 {
		return CredentialReference{}, ErrInvalidInput
	}
	r := s.CredentialReferences[0]
	if r.Kind != "engine-gateway" || r.Reference != "secret://"+s.Engine.Namespace+"/"+s.Engine.Template+"/gateway" || r.ExpiresAt.IsZero() || r.ExpiresAt.After(s.DeadlineAt) {
		return CredentialReference{}, ErrInvalidInput
	}
	return r, nil
}

// EngineAttempt stores correlation and side-effect intent, not engine history.
// The runtime attempt retains the selected backend/version and input digests.
type EngineAttempt struct {
	AttemptID           string `json:"attempt_id"`
	SpecificationHash   string `json:"specification_digest"`
	MessageID           string `json:"message_id"`
	InstanceID          string `json:"instance_id"`
	Revision            string `json:"revision"`
	Route               string `json:"route"`
	ContextID           string `json:"context_id"`
	TaskID              string `json:"task_id"`
	QuestionID          string `json:"question_id,omitempty"`
	AnswerMessageID     string `json:"answer_message_id,omitempty"`
	AnswerDigest        string `json:"answer_digest,omitempty"`
	AnswerPhase         string `json:"answer_phase,omitempty"`
	Phase               string `json:"phase"`
	CancelRequested     bool   `json:"cancel_requested"`
	DeleteRequested     bool   `json:"delete_requested"`
	Deleted             bool   `json:"deleted"`
	Collected           bool   `json:"collected"`
	State               State  `json:"state"`
	ObservationFailures int    `json:"observation_failures,omitempty"`
	FailureClass        string `json:"failure_class,omitempty"`
	FailureMessage      string `json:"failure_message,omitempty"`
	Version             int64  `json:"version"`
}

// AllowsCalls fails closed before message delivery and as soon as durable
// cancellation or cleanup intent exists, independently of engine termination.
func (r EngineAttempt) AllowsCalls() bool {
	return !r.CancelRequested && !r.DeleteRequested && !r.Deleted && !r.State.Terminal() && (r.Phase == "sending" || r.Phase == "active")
}

func (r EngineAttempt) CanUpdate(n EngineAttempt) bool {
	if n.ObservationFailures < 0 || n.ObservationFailures > 3 || r.FailureClass != "" && (r.FailureClass != n.FailureClass || r.FailureMessage != n.FailureMessage) {
		return false
	}
	if r.AttemptID != n.AttemptID || r.SpecificationHash != n.SpecificationHash || r.MessageID != n.MessageID || r.Version != n.Version {
		return false
	}
	for _, pair := range [][2]string{{r.InstanceID, n.InstanceID}, {r.Revision, n.Revision}, {r.Route, n.Route}, {r.ContextID, n.ContextID}, {r.TaskID, n.TaskID}} {
		if pair[0] != "" && pair[0] != pair[1] {
			return false
		}
	}
	for _, pair := range [][2]string{{r.QuestionID, n.QuestionID}, {r.AnswerMessageID, n.AnswerMessageID}, {r.AnswerDigest, n.AnswerDigest}} {
		if pair[0] != "" && pair[0] != pair[1] {
			return false
		}
	}
	if r.AnswerPhase == "sent" && n.AnswerPhase != "sent" || r.AnswerPhase == "sending" && n.AnswerPhase != "sending" && n.AnswerPhase != "sent" {
		return false
	}
	if r.CancelRequested && !n.CancelRequested || r.DeleteRequested && !n.DeleteRequested || r.Deleted && !n.Deleted || r.Collected && !n.Collected {
		return false
	}
	if n.Deleted && !n.DeleteRequested || n.CancelRequested && n.State == StateSucceeded && !r.State.Terminal() {
		return false
	}
	if !n.State.Valid() || !r.State.CanObserve(n.State) {
		return false
	}
	if n.Phase == r.Phase {
		return true
	}
	switch r.Phase {
	case "creating":
		return n.Phase == "ready" || n.Phase == "revision_mismatch"
	case "ready":
		return n.Phase == "sending"
	case "sending":
		return n.Phase == "active"
	}
	return false
}
