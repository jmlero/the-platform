package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConversationRepositoryIndexIsPinnedCompleteAndExcludesLinks(t *testing.T) {
	commit, tree := strings.Repeat("a", 40), strings.Repeat("b", 40)
	truncated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Fatalf("mutation: %s", r.Method)
		}
		switch r.URL.RequestURI() {
		case "/repos/owner/repo/git/commits/" + commit:
			fmt.Fprintf(w, `{"tree":{"sha":%q}}`, tree)
		case "/repos/owner/repo/git/trees/" + tree + "?recursive=1":
			fmt.Fprintf(w, `{"sha":%q,"truncated":%t,"tree":[{"path":"z.go","type":"blob","mode":"100644","sha":%q,"size":10},{"path":"link","type":"blob","mode":"120000","sha":%q,"size":5},{"path":"a.go","type":"blob","mode":"100755","sha":%q,"size":12}]}`, tree, truncated, commit, commit, commit)
		default:
			t.Fatalf("unpinned route: %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	client, err := New(Config{APIURL: server.URL, Repository: "owner/repo"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	files, err := client.ListRepositoryFiles(context.Background(), "owner/repo", commit)
	if err != nil || len(files) != 2 || files[0].Path != "a.go" || files[1].Path != "z.go" {
		t.Fatalf("index: %+v %v", files, err)
	}
	truncated = true
	if _, err := client.ListRepositoryFiles(context.Background(), "owner/repo", commit); err == nil {
		t.Fatal("partial tree accepted")
	}
	if _, err := client.ListRepositoryFiles(context.Background(), "other/repo", commit); err == nil {
		t.Fatal("other repository accepted")
	}
	if _, err := client.ListRepositoryFiles(context.Background(), "owner/repo", "main"); err == nil {
		t.Fatal("mutable revision accepted")
	}
}
