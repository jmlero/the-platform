package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const (
	serviceAuthTokenOne = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	serviceAuthTokenTwo = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
)

func TestFileServiceAuthorizerAuthenticatesScopesAndReloadsRotation(t *testing.T) {
	policyPath := filepath.Join(t.TempDir(), "service-auth.json")
	writeServiceAuthPolicy(t, policyPath, serviceAuthTokenOne, []string{"provider.info"}, []string{"prj_default"})
	authorizer, err := NewFileServiceAuthorizer(policyPath)
	if err != nil {
		t.Fatalf("create service authorizer: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/provider", nil)
	request.Header.Set("Authorization", "Bearer "+serviceAuthTokenOne)
	identity, err := authorizer.Authorize(request, "provider.info")
	if err != nil {
		t.Fatalf("authorize initial token: %v", err)
	}
	if identity.ID != "marina-installation" || !identity.AllowsProject("prj_default") {
		t.Fatalf("identity = %#v", identity)
	}
	if _, err := authorizer.Authorize(request, "work.claim.atomic"); !errors.Is(err, ErrServiceForbidden) {
		t.Fatalf("missing capability error = %v", err)
	}

	writeServiceAuthPolicy(t, policyPath, serviceAuthTokenTwo, []string{"provider.info"}, []string{"prj_default"})
	if _, err := authorizer.Authorize(request, "provider.info"); !errors.Is(err, ErrServiceUnauthenticated) {
		t.Fatalf("retired token error = %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+serviceAuthTokenTwo)
	rotated, err := authorizer.Authorize(request, "provider.info")
	if err != nil || rotated.ID != identity.ID {
		t.Fatalf("rotated identity = %#v, error = %v", rotated, err)
	}
}

func TestFileServiceAuthorizerFailsClosed(t *testing.T) {
	policyPath := filepath.Join(t.TempDir(), "service-auth.json")
	writeServiceAuthPolicy(t, policyPath, serviceAuthTokenOne, []string{"provider.info"}, []string{"prj_default"})
	authorizer, err := NewFileServiceAuthorizer(policyPath)
	if err != nil {
		t.Fatalf("create service authorizer: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/provider", nil)
	request.Header.Set("Authorization", "Bearer "+serviceAuthTokenOne)

	if err := os.WriteFile(policyPath, []byte(`{"version":1,"identities":[]}`), 0o600); err != nil {
		t.Fatalf("replace policy: %v", err)
	}
	if _, err := authorizer.Authorize(request, "provider.info"); !errors.Is(err, ErrServiceAuthUnavailable) {
		t.Fatalf("malformed replacement error = %v", err)
	}

	request.Header.Del("Authorization")
	if _, err := authorizer.Authorize(request, "provider.info"); !errors.Is(err, ErrServiceUnauthenticated) {
		t.Fatalf("missing credential error = %v", err)
	}
}

func writeServiceAuthPolicy(
	t *testing.T,
	path string,
	token string,
	capabilities []string,
	projects []string,
) {
	t.Helper()
	digest := sha256.Sum256([]byte(token))
	policy := `{"version":1,"identities":[{"id":"marina-installation","token_sha256":["` +
		hex.EncodeToString(digest[:]) + `"],"projects":["` + projects[0] + `"],"capabilities":["` +
		capabilities[0] + `"]}]}`
	if err := os.WriteFile(path, []byte(policy), 0o600); err != nil {
		t.Fatalf("write service auth policy: %v", err)
	}
}
