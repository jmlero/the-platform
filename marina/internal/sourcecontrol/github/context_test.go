package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadContextFileUsesExactRevisionAndRejectsLinks(t *testing.T) {
	revision := strings.Repeat("a", 40)
	kind := "file"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/contents/docs/decision.md" || r.URL.Query().Get("ref") != revision {
			t.Errorf("unexpected context request: %s", r.URL)
		}
		fmt.Fprintf(w, `{"type":%q,"encoding":"base64","size":12,"content":%q}`, kind, base64.StdEncoding.EncodeToString([]byte("Keep the API")))
	}))
	defer server.Close()
	client, err := New(Config{APIURL: server.URL, Repository: "owner/repo"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	content, err := client.ReadContextFile(context.Background(), "owner/repo", revision, "docs/decision.md")
	if err != nil || content != "Keep the API" {
		t.Fatalf("read context: %q %v", content, err)
	}
	kind = "symlink"
	if _, err = client.ReadContextFile(context.Background(), "owner/repo", revision, "docs/decision.md"); err == nil {
		t.Fatal("symlink context accepted")
	}
	if _, err = client.ReadContextFile(context.Background(), "owner/repo", "main", "docs/decision.md"); err == nil {
		t.Fatal("mutable ref accepted")
	}
	if _, err = client.ReadContextFile(context.Background(), "owner/repo", revision, "../secret"); err == nil {
		t.Fatal("path escape accepted")
	}
}
