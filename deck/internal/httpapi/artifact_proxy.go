package httpapi

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// AppDeck stores artifact metadata. Bytes remain in the separately owned Marina
// store and are fetched only after operator authentication and ledger lookup.
func WithArtifactStore(baseURL, tokenFile, caFile string) Option {
	return func(s *Server) {
		s.artifactURL = strings.TrimRight(baseURL, "/")
		s.artifactTokenFile = tokenFile
		s.artifactCAFile = caFile
	}
}

func artifactLocation(base, uri, digest string) bool {
	h := strings.TrimPrefix(digest, "sha256:")
	_, err := hex.DecodeString(h)
	return strings.HasPrefix(base, "https://") && len(h) == 64 && digest == "sha256:"+h && strings.ToLower(h) == h && err == nil && uri == strings.TrimRight(base, "/")+"/artifacts/"+digest
}

func (s *Server) downloadArtifact(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	detail, err := s.store.GetTaskDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	digest := r.PathValue("digest")
	uri := ""
	for _, evidence := range detail.Evidence {
		if evidence.ArtifactDigest == digest && artifactLocation(s.artifactURL, evidence.ArtifactURI, digest) {
			uri = evidence.ArtifactURI
			break
		}
	}
	if uri == "" {
		http.Error(w, "No downloadable artifact is registered for this task", 404)
		return
	}
	token, err := os.ReadFile(s.artifactTokenFile)
	if err != nil || len(strings.TrimSpace(string(token))) < 32 {
		http.Error(w, "Artifact store is not configured", 503)
		return
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if s.artifactCAFile != "" {
		b, err := os.ReadFile(s.artifactCAFile)
		if err != nil {
			http.Error(w, "Artifact trust configuration unavailable", 503)
			return
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(b) {
			http.Error(w, "Invalid artifact trust configuration", 503)
			return
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(r.Context(), "GET", uri, nil)
	if err != nil {
		http.Error(w, "Artifact location unavailable", 503)
		return
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	response, err := client.Do(request)
	if err != nil {
		http.Error(w, "Artifact store unavailable", 502)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		http.Error(w, "Artifact store rejected the download", 502)
		return
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	sum := sha256.Sum256(b)
	if err != nil || len(b) > 4<<20 || "sha256:"+hex.EncodeToString(sum[:]) != digest {
		http.Error(w, "Artifact integrity check failed", 502)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="verification.json"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b)
}
