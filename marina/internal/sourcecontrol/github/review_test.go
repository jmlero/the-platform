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

func TestReviewReadsPinnedBlobAndRejectsIncompleteTreesLinksAndUnavailableSource(t *testing.T) {
	base, treeID, blobID := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	for _, scenario := range []string{"file", "missing", "symlink", "submodule", "truncated", "unavailable", "wrong blob"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/owner/repo/git/commits/" + base:
					if scenario == "unavailable" {
						w.WriteHeader(404)
						return
					}
					fmt.Fprintf(w, `{"tree":{"sha":%q}}`, treeID)
				case "/repos/owner/repo/git/trees/" + treeID:
					if r.URL.Query().Get("recursive") != "1" {
						t.Error("tree was not fully read")
					}
					mode, kind, name := "100644", "blob", "tests/regression.go"
					if scenario == "symlink" {
						mode = "120000"
					}
					if scenario == "submodule" {
						mode, kind = "160000", "commit"
					}
					if scenario == "missing" {
						name = "different"
					}
					fmt.Fprintf(w, `{"sha":%q,"truncated":%t,"tree":[{"path":%q,"mode":%q,"type":%q,"sha":%q,"size":4}]}`, treeID, scenario == "truncated", name, mode, kind, blobID)
				case "/repos/owner/repo/git/blobs/" + blobID:
					identity := blobID
					if scenario == "wrong blob" {
						identity = base
					}
					fmt.Fprintf(w, `{"sha":%q,"encoding":"base64","size":4,"content":%q}`, identity, base64.StdEncoding.EncodeToString([]byte("test")))
				default:
					t.Errorf("unpinned source read: %s", r.URL)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			client, err := New(Config{APIURL: server.URL, Repository: "owner/repo"}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			data, exists, err := client.ReadReviewFile(context.Background(), "owner/repo", base, "tests/regression.go")
			if scenario == "file" {
				if err != nil || !exists || string(data) != "test" {
					t.Fatalf("pinned source missing: %q %t %v", data, exists, err)
				}
			} else if scenario == "missing" {
				if err != nil || exists {
					t.Fatalf("new file was not established from the complete tree: %t %v", exists, err)
				}
			} else if err == nil {
				t.Fatal("unsafe source passed review admission")
			}
		})
	}
}
