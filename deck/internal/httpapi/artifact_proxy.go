package httpapi

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Deck stores artifact metadata. Bytes remain in the separately owned Marina
// store and are fetched only after operator authentication and ledger lookup.
func WithArtifactStore(baseURL, tokenFile, caFile string) Option {
	return func(s *Server) {
		s.artifactURL = strings.TrimRight(baseURL, "/")
		s.artifactTokenFile = tokenFile
		s.artifactCAFile = caFile
	}
}

// artifactClientCache reuses one client, and its connections, until the
// mounted trust file changes; Kubernetes updates ConfigMap mounts in place.
type artifactClientCache struct {
	mu     sync.Mutex
	ca     []byte
	client *http.Client
}

func (s *Server) artifactHTTPClient() (*http.Client, error) {
	var ca []byte
	if s.artifactCAFile != "" {
		var err error
		if ca, err = os.ReadFile(s.artifactCAFile); err != nil {
			return nil, fmt.Errorf("read artifact trust: %w", err)
		}
	}
	cache := &s.artifactClients
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.client != nil && bytes.Equal(cache.ca, ca) {
		return cache.client, nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if ca != nil {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(ca) {
			return nil, errors.New("artifact trust file contains no certificates")
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	}
	if cache.client != nil {
		cache.client.CloseIdleConnections()
	}
	cache.ca = ca
	cache.client = &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return cache.client, nil
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
		http.Error(w, "No downloadable artifact is registered for this task", http.StatusNotFound)
		return
	}
	token, err := os.ReadFile(s.artifactTokenFile)
	if err != nil || len(strings.TrimSpace(string(token))) < 32 {
		http.Error(w, "Artifact store is not configured", http.StatusServiceUnavailable)
		return
	}
	client, err := s.artifactHTTPClient()
	if err != nil {
		s.logger.Error("artifact trust configuration unavailable", "error", err)
		http.Error(w, "Artifact trust configuration unavailable", http.StatusServiceUnavailable)
		return
	}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, uri, nil)
	if err != nil {
		http.Error(w, "Artifact location unavailable", http.StatusServiceUnavailable)
		return
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	response, err := client.Do(request)
	if err != nil {
		http.Error(w, "Artifact store unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		http.Error(w, "Artifact store rejected the download", http.StatusBadGateway)
		return
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	sum := sha256.Sum256(b)
	if err != nil || len(b) > 4<<20 || "sha256:"+hex.EncodeToString(sum[:]) != digest {
		http.Error(w, "Artifact integrity check failed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="verification.json"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b)
}
