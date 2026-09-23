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
	"net/http"
	"os"
	"strings"
)

const (
	serviceAuthPolicyVersion = 1
	maxServiceAuthPolicySize = 1 << 20
)

var (
	ErrServiceUnauthenticated = errors.New("service authentication failed")
	ErrServiceForbidden       = errors.New("service operation is not authorized")
	ErrServiceAuthUnavailable = errors.New("service authorization policy is unavailable")
)

// ServiceIdentity is the stable Deck principal represented by an opaque
// credential. Projects and capabilities are deliberately explicit; wildcard
// grants would make it too easy to expand Marina's authority accidentally.
type ServiceIdentity struct {
	ID           string
	projects     map[string]struct{}
	capabilities map[string]struct{}
}

func (identity ServiceIdentity) AllowsProject(projectID string) bool {
	_, ok := identity.projects[projectID]
	return ok
}

// Projects returns the granted project IDs as a non-nil slice.
func (identity ServiceIdentity) Projects() []string {
	projects := make([]string, 0, len(identity.projects))
	for projectID := range identity.projects {
		projects = append(projects, projectID)
	}
	return projects
}

func (identity ServiceIdentity) AllowsCapability(capability string) bool {
	_, ok := identity.capabilities[capability]
	return ok
}

type serviceAuthPolicy struct {
	Version    int                         `json:"version"`
	Identities []serviceAuthPolicyIdentity `json:"identities"`
}

type serviceAuthPolicyIdentity struct {
	ID                  string   `json:"id"`
	TokenSHA256         []string `json:"token_sha256"`
	Projects            []string `json:"projects"`
	Capabilities        []string `json:"capabilities"`
	decodedFingerprints [][sha256.Size]byte
}

// FileServiceAuthorizer reloads its policy for every request. Kubernetes
// projected secrets and atomic local file replacement can therefore rotate a
// credential without changing the stable service identity or restarting either
// product. A malformed replacement fails closed.
type FileServiceAuthorizer struct {
	path string
}

func NewFileServiceAuthorizer(path string) (*FileServiceAuthorizer, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("service authorization policy path is required: %w", ErrServiceAuthUnavailable)
	}
	authorizer := &FileServiceAuthorizer{path: path}
	if _, err := authorizer.load(); err != nil {
		return nil, err
	}
	return authorizer, nil
}

func (authorizer *FileServiceAuthorizer) Authorize(
	request *http.Request,
	capability string,
) (ServiceIdentity, error) {
	token, err := bearerToken(request.Header.Values("Authorization"))
	if err != nil {
		return ServiceIdentity{}, err
	}
	policy, err := authorizer.load()
	if err != nil {
		return ServiceIdentity{}, err
	}
	digest := sha256.Sum256([]byte(token))
	for _, configured := range policy.Identities {
		for _, fingerprint := range configured.decodedFingerprints {
			if subtle.ConstantTimeCompare(digest[:], fingerprint[:]) != 1 {
				continue
			}
			identity := ServiceIdentity{
				ID:           configured.ID,
				projects:     stringSet(configured.Projects),
				capabilities: stringSet(configured.Capabilities),
			}
			if !identity.AllowsCapability(capability) {
				return ServiceIdentity{}, ErrServiceForbidden
			}
			return identity, nil
		}
	}
	return ServiceIdentity{}, ErrServiceUnauthenticated
}

func (authorizer *FileServiceAuthorizer) load() (serviceAuthPolicy, error) {
	file, err := os.Open(authorizer.path)
	if err != nil {
		return serviceAuthPolicy{}, fmt.Errorf("open service authorization policy: %w: %w", err, ErrServiceAuthUnavailable)
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxServiceAuthPolicySize+1))
	if err != nil {
		return serviceAuthPolicy{}, fmt.Errorf("read service authorization policy: %w: %w", err, ErrServiceAuthUnavailable)
	}
	if len(payload) > maxServiceAuthPolicySize {
		return serviceAuthPolicy{}, fmt.Errorf("service authorization policy exceeds size limit: %w", ErrServiceAuthUnavailable)
	}

	var policy serviceAuthPolicy
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return serviceAuthPolicy{}, fmt.Errorf("decode service authorization policy: %w: %w", err, ErrServiceAuthUnavailable)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return serviceAuthPolicy{}, fmt.Errorf("service authorization policy has trailing JSON: %w", ErrServiceAuthUnavailable)
	}
	if err := validateServiceAuthPolicy(&policy); err != nil {
		return serviceAuthPolicy{}, err
	}
	return policy, nil
}

func validateServiceAuthPolicy(policy *serviceAuthPolicy) error {
	if policy.Version != serviceAuthPolicyVersion {
		return fmt.Errorf("service authorization policy version must be %d: %w", serviceAuthPolicyVersion, ErrServiceAuthUnavailable)
	}
	if len(policy.Identities) == 0 {
		return fmt.Errorf("service authorization policy must contain an identity: %w", ErrServiceAuthUnavailable)
	}
	identityIDs := make(map[string]struct{}, len(policy.Identities))
	fingerprints := make(map[string]struct{})
	for index := range policy.Identities {
		identity := &policy.Identities[index]
		identity.ID = strings.TrimSpace(identity.ID)
		if identity.ID == "" {
			return fmt.Errorf("service identity id is required: %w", ErrServiceAuthUnavailable)
		}
		if len(identity.ID) > 128 {
			return fmt.Errorf("service identity id exceeds 128 characters: %w", ErrServiceAuthUnavailable)
		}
		if _, duplicate := identityIDs[identity.ID]; duplicate {
			return fmt.Errorf("service identity %q is duplicated: %w", identity.ID, ErrServiceAuthUnavailable)
		}
		identityIDs[identity.ID] = struct{}{}
		if err := validateExplicitValues("project", identity.Projects); err != nil {
			return err
		}
		if err := validateExplicitValues("capability", identity.Capabilities); err != nil {
			return err
		}
		if len(identity.TokenSHA256) == 0 {
			return fmt.Errorf("service identity %q has no token fingerprint: %w", identity.ID, ErrServiceAuthUnavailable)
		}
		identity.decodedFingerprints = make([][sha256.Size]byte, 0, len(identity.TokenSHA256))
		for _, encoded := range identity.TokenSHA256 {
			encoded = strings.ToLower(strings.TrimSpace(encoded))
			decoded, err := hex.DecodeString(encoded)
			if err != nil || len(decoded) != sha256.Size {
				return fmt.Errorf("service identity %q has an invalid SHA-256 fingerprint: %w", identity.ID, ErrServiceAuthUnavailable)
			}
			if _, duplicate := fingerprints[encoded]; duplicate {
				return fmt.Errorf("service token fingerprint is assigned more than once: %w", ErrServiceAuthUnavailable)
			}
			fingerprints[encoded] = struct{}{}
			var fingerprint [sha256.Size]byte
			copy(fingerprint[:], decoded)
			identity.decodedFingerprints = append(identity.decodedFingerprints, fingerprint)
		}
	}
	return nil
}

func validateExplicitValues(kind string, values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("service identity must contain a %s grant: %w", kind, ErrServiceAuthUnavailable)
	}
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == "*" {
			return fmt.Errorf("service identity %s grants must be explicit: %w", kind, ErrServiceAuthUnavailable)
		}
		if _, duplicate := seen[value]; duplicate {
			return fmt.Errorf("service identity %s grant %q is duplicated: %w", kind, value, ErrServiceAuthUnavailable)
		}
		seen[value] = struct{}{}
		values[index] = value
	}
	return nil
}

func bearerToken(values []string) (string, error) {
	if len(values) != 1 {
		return "", ErrServiceUnauthenticated
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) < 32 {
		return "", ErrServiceUnauthenticated
	}
	for _, character := range parts[1] {
		if character < 0x21 || character > 0x7e {
			return "", ErrServiceUnauthenticated
		}
	}
	return parts[1], nil
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
