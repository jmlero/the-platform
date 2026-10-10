package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommitAncestryRequiresExactProviderProof(t *testing.T) {
	ancestor, descendant := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, scenario := range []string{"ahead", "diverged", "wrong merge base", "wrong base", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo/compare/"+ancestor+"..."+descendant || r.URL.Query().Get("per_page") != "1" {
					t.Error("comparison was not bound to the two exact commits")
				}
				if scenario == "unavailable" {
					w.WriteHeader(503)
					return
				}
				base, merge, status := ancestor, ancestor, "ahead"
				if scenario == "diverged" {
					status = "diverged"
				}
				if scenario == "wrong merge base" {
					merge = descendant
				}
				if scenario == "wrong base" {
					base = descendant
				}
				fmt.Fprintf(w, `{"status":%q,"base_commit":{"sha":%q},"merge_base_commit":{"sha":%q}}`, status, base, merge)
			}))
			defer server.Close()
			client, err := New(Config{APIURL: server.URL, Repository: "owner/repo"}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			contains, err := client.ContainsCommit(context.Background(), "owner/repo", ancestor, descendant)
			if contains != (scenario == "ahead") {
				t.Fatalf("contains=%t error=%v", contains, err)
			}
			if (scenario == "unavailable" || scenario == "wrong base") && err == nil {
				t.Fatal("missing provider proof was accepted")
			}
		})
	}
}
