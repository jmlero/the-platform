package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
)

func TestCredentialRotationReloadsAndEmptyCredentialStopsBeforeProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	want, calls := "first-private-token", 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer "+want {
			t.Error("unexpected credential")
		}
		_, _ = w.Write([]byte(`{"object":{"sha":"head"}}`))
	}))
	defer server.Close()
	client, err := New(Config{APIURL: server.URL, Repository: "owner/repo", TokenFile: path}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"first-private-token", "replacement-private-token", ""} {
		want = token
		if err := os.WriteFile(path, []byte(token), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := client.CurrentHead(context.Background(), sourcecontrol.HeadRequest{Repository: "owner/repo", Ref: "main"})
		if token != "" && err != nil {
			t.Fatal(err)
		}
		if token == "" && err == nil {
			t.Fatal("empty credential reached provider")
		}
		if err != nil && strings.Contains(err.Error(), "private-token") {
			t.Fatal("credential leaked in error")
		}
	}
	if calls != 2 {
		t.Fatalf("provider calls=%d, want 2", calls)
	}
}

func TestPullRequestRecoveryFindsExistingRunWithoutCreatingDuplicate(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
			t.Errorf("unexpected write %s", r.URL.Path)
		}
		if r.URL.Path == "/repos/owner/repo/git/ref/heads/marina/run" {
			_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": "head"}})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"number": 12, "html_url": "https://git.example/pull/12", "state": "open", "body": "Review result\n\n<!-- marina:run -->", "head": map[string]string{"sha": "head"}}})
	}))
	defer server.Close()
	client, err := New(Config{APIURL: server.URL, Repository: "owner/repo"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	pr, err := client.CreatePullRequest(context.Background(), sourcecontrol.PullRequestRequest{Repository: "owner/repo", HeadRef: "marina/run", BaseRef: "main", ExpectedHead: "head", Title: "Fix", Body: "Review result", IdempotencyKey: "run", AuthorityFence: 1})
	if err != nil || pr.ID != "12" || writes != 0 {
		t.Fatalf("recovered PR = %#v, %v, writes=%d", pr, err, writes)
	}
}

func TestBranchCreationRequiresLiveAuthority(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("write reached the Git provider without authority")
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	client, _ := New(Config{APIURL: server.URL, Repository: "owner/repo"}, server.Client())
	_, err := client.CreateBranch(context.Background(), sourcecontrol.BranchRequest{Repository: "owner/repo", Name: "marina/run", BaseRevision: "head", IdempotencyKey: "run", AuthorityFence: 1})
	if err == nil {
		t.Fatal("write was authorized without an authority checker")
	}
}

func TestPullRequestObservationReportsClosureWithoutMutation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/repos/owner/repo/pulls/12" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 12, "state": "closed", "html_url": "https://git.example/pull/12", "head": map[string]string{"sha": "head"}})
	}))
	defer server.Close()
	client, _ := New(Config{APIURL: server.URL, Repository: "owner/repo"}, server.Client())
	pr, err := client.GetPullRequest(context.Background(), sourcecontrol.ObservationRequest{Repository: "owner/repo", PullRequestID: "12"})
	if err != nil || pr.State != "closed" || pr.HeadRevision != "head" {
		t.Fatalf("PR observation: %#v %v", pr, err)
	}
}

func TestFastForwardMainRecoversLostResponseWithoutSecondUpdate(t *testing.T) {
	base, branch, updates := "base", "verified", 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/git/ref/heads/main":
			_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": base}})
		case "/repos/owner/repo/git/ref/heads/marina/run":
			_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": branch}})
		case "/repos/owner/repo/git/refs/heads/main":
			if r.Method != http.MethodPatch {
				t.Errorf("unexpected method %s", r.Method)
			}
			updates++
			var body struct {
				SHA   string `json:"sha"`
				Force bool   `json:"force"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SHA != branch || body.Force {
				t.Errorf("unguarded update: %+v %v", body, err)
			}
			base = branch
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close() // GitHub applied the update but its response was lost.
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := New(Config{APIURL: server.URL, Repository: "owner/repo", BaseBranch: "main", Authorize: func(context.Context, int64) error { return nil }}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	request := sourcecontrol.FastForwardRequest{Repository: "owner/repo", BaseRef: "main", ExpectedBase: "base", HeadRef: "marina/run", ExpectedHead: "verified", AuthorityFence: 1}
	for range 2 {
		revision, err := client.FastForward(context.Background(), request)
		if err != nil || revision.Commit != "verified" || revision.Ref != "main" {
			t.Fatalf("revision=%+v err=%v", revision, err)
		}
	}
	if updates != 1 {
		t.Fatalf("updates=%d, want one", updates)
	}
}

func TestFastForwardMainRejectsMovedBaseAndChangedTaskHead(t *testing.T) {
	for _, state := range []struct{ name, base, branch string }{{"moved base", "other", "verified"}, {"changed task head", "base", "other"}} {
		t.Run(state.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("mutation reached provider")
					return
				}
				sha := state.base
				if strings.HasSuffix(r.URL.Path, "/marina/run") {
					sha = state.branch
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": sha}})
			}))
			defer server.Close()
			client, _ := New(Config{APIURL: server.URL, Repository: "owner/repo", BaseBranch: "main", Authorize: func(context.Context, int64) error { return nil }}, server.Client())
			_, err := client.FastForward(context.Background(), sourcecontrol.FastForwardRequest{Repository: "owner/repo", BaseRef: "main", ExpectedBase: "base", HeadRef: "marina/run", ExpectedHead: "verified", AuthorityFence: 1})
			if err == nil {
				t.Fatal("changed head accepted")
			}
		})
	}
}
