package httpapi

import "testing"

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
