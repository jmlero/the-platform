package httpapi

import (
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactProxyRestrictsLocationToConfiguredStore(t *testing.T) {
	base := "https://marina.internal"
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, uri := range []string{"http://marina.internal/artifacts/" + digest, "https://evil.test/artifacts/" + digest, base + "/artifacts/" + digest + "?redirect=1", base + "/artifacts/other"} {
		if artifactLocation(base, uri, digest) {
			t.Fatalf("unsafe location accepted: %s", uri)
		}
	}
	if !artifactLocation(base, base+"/artifacts/"+digest, digest) {
		t.Fatal("registered immutable artifact rejected")
	}
}

func TestArtifactClientIsReusedUntilTrustFileChanges(t *testing.T) {
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	store := httptest.NewTLSServer(nil)
	defer store.Close()
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: store.Certificate().Raw})
	writeCA := func(bundle []byte) {
		t.Helper()
		if err := os.WriteFile(caFile, bundle, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeCA(block)
	s := &Server{artifactCAFile: caFile}
	first, err := s.artifactHTTPClient()
	if err != nil {
		t.Fatal(err)
	}
	if again, err := s.artifactHTTPClient(); err != nil || again != first {
		t.Fatalf("unchanged trust rebuilt client: %v", err)
	}
	writeCA(append(block, block...))
	if rotated, err := s.artifactHTTPClient(); err != nil || rotated == first {
		t.Fatalf("rotated trust reused stale client: %v", err)
	}
	if err := os.WriteFile(caFile, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.artifactHTTPClient(); err == nil {
		t.Fatal("invalid trust file accepted")
	}
}
