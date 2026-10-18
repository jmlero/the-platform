package workcontrol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"time"
)

func contextDigest(value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func ValidateResolvedContext(selection *TaskContext, resolved ResolvedContext) error {
	if !reflect.DeepEqual(selection, resolved.Selection) {
		return errors.New("context selection differs from authorized task")
	}
	if selection == nil {
		if len(resolved.Sources)+len(resolved.Messages) != 0 {
			return errors.New("unexpected context sources")
		}
		return nil
	}
	if len(selection.Sources) > 32 || len(selection.Messages) > 20 || len(resolved.Sources) != len(selection.Sources) || len(resolved.Messages) != len(selection.Messages) {
		return errors.New("context selection is missing or oversized")
	}
	for i, source := range resolved.Sources {
		ref := selection.Sources[i]
		if source.ID != ref.ID || source.Revision != ref.Revision || source.Digest != ref.Digest {
			return errors.New("context source revision differs from authorized selection")
		}
		if source.Trust != "data" && source.Trust != "guidance" {
			return errors.New("unknown context trust")
		}
		canonical := source
		canonical.Digest, canonical.RevokedAt, canonical.Superseded = "", 0, false
		if canonical.Kind == "decision" {
			canonical.Content = ""
		}
		if contextDigest(canonical) != ref.Digest {
			return errors.New("context source digest mismatch")
		}
	}
	for i, message := range resolved.Messages {
		ref := selection.Messages[i]
		canonical := message
		canonical.Digest = ""
		if message.ID != ref.ID || ref.Revision != 1 || message.Trust != "data" || message.Digest != ref.Digest || contextDigest(canonical) != ref.Digest {
			return errors.New("selected conversation message changed or crossed trust boundary")
		}
	}
	return nil
}

// Revocation changes use of saved context, never the saved selection itself.
func CheckContextValidity(now time.Time, resolved ResolvedContext) error {
	for _, source := range resolved.Sources {
		if source.RevokedAt != 0 {
			return errors.New("selected context source " + source.ID + " was revoked")
		}
		if source.Trust == "guidance" && source.ExpiresAt != 0 && source.ExpiresAt <= now.UnixMilli() {
			return errors.New("selected guidance " + source.ID + " is stale")
		}
	}
	return nil
}

// ContextSourceDigest is the digest ValidateResolvedContext expects in a
// selection reference for this source.
func ContextSourceDigest(source ContextSource) string {
	canonical := source
	canonical.Digest, canonical.RevokedAt, canonical.Superseded = "", 0, false
	if canonical.Kind == "decision" {
		canonical.Content = ""
	}
	return contextDigest(canonical)
}

// ContextMessageDigest is the digest ValidateResolvedContext expects for a
// selected message.
func ContextMessageDigest(message ContextMessage) string {
	canonical := message
	canonical.Digest = ""
	return contextDigest(canonical)
}
