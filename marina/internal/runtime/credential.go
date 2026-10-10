package runtime

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// CredentialToken binds an ephemeral gateway credential to one immutable attempt.
func CredentialToken(key []byte, specification Specification) string {
	digest, _ := specification.Digest()
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(specification.AttemptID + "\x00" + digest))
	return hex.EncodeToString(mac.Sum(nil))
}
