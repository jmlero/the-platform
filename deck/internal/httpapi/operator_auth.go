package httpapi

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	operatorAuthPolicyVersion = 1
	maxOperatorAuthPolicySize = 64 * 1024
)

var (
	ErrOperatorUnauthenticated = errors.New("operator unauthenticated")
	ErrOperatorAuthUnavailable = errors.New("operator authentication unavailable")
)

type OperatorIdentity struct {
	ID string
}

type operatorAuthPolicy struct {
	Version   int                      `json:"version"`
	Operators []configuredOperatorAuth `json:"operators"`
}

type configuredOperatorAuth struct {
	ID                  string   `json:"id"`
	TokenSHA256         []string `json:"token_sha256"`
	decodedFingerprints [][sha256.Size]byte
}

type FileOperatorAuthenticator struct {
	path string
}

func NewFileOperatorAuthenticator(path string) (*FileOperatorAuthenticator, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("operator authentication policy path is required: %w", ErrOperatorAuthUnavailable)
	}
	authenticator := &FileOperatorAuthenticator{path: path}
	if _, err := authenticator.load(); err != nil {
		return nil, err
	}
	return authenticator, nil
}

func (authenticator *FileOperatorAuthenticator) Authenticate(token string) (OperatorIdentity, error) {
	token = strings.TrimSpace(token)
	if len(token) < 32 {
		return OperatorIdentity{}, ErrOperatorUnauthenticated
	}
	for _, character := range token {
		if character < 0x21 || character > 0x7e {
			return OperatorIdentity{}, ErrOperatorUnauthenticated
		}
	}
	policy, err := authenticator.load()
	if err != nil {
		return OperatorIdentity{}, err
	}
	digest := sha256.Sum256([]byte(token))
	for _, operator := range policy.Operators {
		for _, fingerprint := range operator.decodedFingerprints {
			if subtle.ConstantTimeCompare(digest[:], fingerprint[:]) == 1 {
				return OperatorIdentity{ID: operator.ID}, nil
			}
		}
	}
	return OperatorIdentity{}, ErrOperatorUnauthenticated
}

func (authenticator *FileOperatorAuthenticator) load() (operatorAuthPolicy, error) {
	file, err := os.Open(authenticator.path)
	if err != nil {
		return operatorAuthPolicy{}, fmt.Errorf("open operator authentication policy: %w: %w", err, ErrOperatorAuthUnavailable)
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxOperatorAuthPolicySize+1))
	if err != nil {
		return operatorAuthPolicy{}, fmt.Errorf("read operator authentication policy: %w: %w", err, ErrOperatorAuthUnavailable)
	}
	if len(payload) > maxOperatorAuthPolicySize {
		return operatorAuthPolicy{}, fmt.Errorf("operator authentication policy exceeds size limit: %w", ErrOperatorAuthUnavailable)
	}
	var policy operatorAuthPolicy
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return operatorAuthPolicy{}, fmt.Errorf("decode operator authentication policy: %w: %w", err, ErrOperatorAuthUnavailable)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return operatorAuthPolicy{}, fmt.Errorf("operator authentication policy has trailing JSON: %w", ErrOperatorAuthUnavailable)
	}
	if err := validateOperatorAuthPolicy(&policy); err != nil {
		return operatorAuthPolicy{}, err
	}
	return policy, nil
}

func validateOperatorAuthPolicy(policy *operatorAuthPolicy) error {
	if policy.Version != operatorAuthPolicyVersion {
		return fmt.Errorf("operator authentication policy version must be %d: %w", operatorAuthPolicyVersion, ErrOperatorAuthUnavailable)
	}
	if len(policy.Operators) == 0 {
		return fmt.Errorf("operator authentication policy must contain an operator: %w", ErrOperatorAuthUnavailable)
	}
	operatorIDs := make(map[string]struct{}, len(policy.Operators))
	fingerprints := make(map[string]struct{})
	for index := range policy.Operators {
		operator := &policy.Operators[index]
		operator.ID = strings.TrimSpace(operator.ID)
		if operator.ID == "" || len(operator.ID) > 128 {
			return fmt.Errorf("operator identity id must contain at most 128 characters: %w", ErrOperatorAuthUnavailable)
		}
		if _, duplicate := operatorIDs[operator.ID]; duplicate {
			return fmt.Errorf("operator identity %q is duplicated: %w", operator.ID, ErrOperatorAuthUnavailable)
		}
		operatorIDs[operator.ID] = struct{}{}
		if len(operator.TokenSHA256) == 0 {
			return fmt.Errorf("operator identity %q has no token fingerprint: %w", operator.ID, ErrOperatorAuthUnavailable)
		}
		operator.decodedFingerprints = make([][sha256.Size]byte, 0, len(operator.TokenSHA256))
		for _, encoded := range operator.TokenSHA256 {
			encoded = strings.ToLower(strings.TrimSpace(encoded))
			decoded, err := hex.DecodeString(encoded)
			if err != nil || len(decoded) != sha256.Size {
				return fmt.Errorf("operator identity %q has an invalid SHA-256 fingerprint: %w", operator.ID, ErrOperatorAuthUnavailable)
			}
			if _, duplicate := fingerprints[encoded]; duplicate {
				return fmt.Errorf("operator token fingerprint is assigned more than once: %w", ErrOperatorAuthUnavailable)
			}
			fingerprints[encoded] = struct{}{}
			var fingerprint [sha256.Size]byte
			copy(fingerprint[:], decoded)
			operator.decodedFingerprints = append(operator.decodedFingerprints, fingerprint)
		}
	}
	return nil
}

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func constantTimeStringEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
