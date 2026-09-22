package runtime

import (
	"encoding/hex"
	"fmt"
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
func (e EngineSelection) Validate() error {
	if e.Backend != "kagent" || !validEngineDigest(e.Revision) {
		return fmt.Errorf("invalid engine selection: %w", ErrInvalidInput)
	}
	for _, s := range []string{e.Version, e.Endpoint, e.Namespace, e.Harness, e.Template} {
		if strings.TrimSpace(s) == "" {
			return ErrInvalidInput
		}
	}
	return nil
}

// EngineAttempt stores correlation and side-effect intent, not engine history.
// The runtime attempt retains the selected backend/version and input digests.
type EngineAttempt struct {
	AttemptID         string `json:"attempt_id"`
	SpecificationHash string `json:"specification_digest"`
	MessageID         string `json:"message_id"`
	InstanceID        string `json:"instance_id"`
	Revision          string `json:"revision"`
	Route             string `json:"route"`
	ContextID         string `json:"context_id"`
	TaskID            string `json:"task_id"`
	Phase             string `json:"phase"`
	CancelRequested   bool   `json:"cancel_requested"`
	DeleteRequested   bool   `json:"delete_requested"`
	Deleted           bool   `json:"deleted"`
	Collected         bool   `json:"collected"`
	State             State  `json:"state"`
	Version           int64  `json:"version"`
}

func (r EngineAttempt) CanUpdate(n EngineAttempt) bool {
	if r.AttemptID != n.AttemptID || r.SpecificationHash != n.SpecificationHash || r.MessageID != n.MessageID || r.Version != n.Version {
		return false
	}
	for _, pair := range [][2]string{{r.InstanceID, n.InstanceID}, {r.Revision, n.Revision}, {r.Route, n.Route}, {r.ContextID, n.ContextID}, {r.TaskID, n.TaskID}} {
		if pair[0] != "" && pair[0] != pair[1] {
			return false
		}
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
