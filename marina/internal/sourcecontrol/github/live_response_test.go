package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
)

type loseCreatedPullResponse struct {
	base http.RoundTripper
	lost bool
}

func (transport *loseCreatedPullResponse) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return response, err
	}
	if !transport.lost && request.Method == http.MethodPost &&
		strings.HasSuffix(request.URL.Path, "/repos/jmlero/marina-acceptance/pulls") &&
		response.StatusCode == http.StatusCreated {
		transport.lost = true
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		return nil, errors.New("injected lost successful pull-request response")
	}
	return response, nil
}

// This opt-in acceptance test creates one review-only PR in the disposable
// repository. Supply an already pushed, reviewed head and a private token-file
// path. It is skipped by ordinary module checks.
func TestLivePullRequestLostResponseRecoversOriginal(t *testing.T) {
	if os.Getenv("KINT09_LIVE_PR_LOSS") != "1" {
		t.Skip("set KINT09_LIVE_PR_LOSS for the authorized disposable PR exercise")
	}
	branch := os.Getenv("KINT09_PR_BRANCH")
	head := os.Getenv("KINT09_PR_HEAD")
	tokenFile := os.Getenv("KINT09_GIT_TOKEN_FILE")
	if !strings.HasPrefix(branch, "marina/kint09-") || head == "" || tokenFile == "" {
		t.Fatal("exact disposable branch, head, and private token-file path are required")
	}
	transport := &loseCreatedPullResponse{base: http.DefaultTransport}
	client, err := New(Config{
		APIURL: "https://api.github.com", Repository: "jmlero/marina-acceptance",
		TokenFile: tokenFile,
		Authorize: func(_ context.Context, fence int64) error {
			if fence != 1 {
				return errors.New("unexpected authority fence")
			}
			return nil
		},
	}, &http.Client{Transport: transport, Timeout: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	request := sourcecontrol.PullRequestRequest{
		Repository: "jmlero/marina-acceptance", BaseRef: "main", HeadRef: branch,
		ExpectedHead: head, Title: "KINT-09: repair clamp bounds and add strict helper",
		Body:           "Disposable publication-response recovery check. Only src/clamp.cjs changed. Review only; do not merge.",
		IdempotencyKey: "kint09-pr-response-20260926", AuthorityFence: 1,
	}
	ctx := context.Background()
	if _, err := client.CreatePullRequest(ctx, request); err == nil || !transport.lost {
		t.Fatalf("successful PR response was not lost: %v", err)
	}
	first, err := client.CreatePullRequest(ctx, request)
	if err != nil || first.ID == "" || first.HeadRevision != head || first.State != "open" {
		t.Fatalf("original PR was not recovered: %+v %v", first, err)
	}
	second, err := client.CreatePullRequest(ctx, request)
	if err != nil || second.ID != first.ID || second.URL != first.URL {
		t.Fatalf("recovery created or selected another PR: %+v %+v %v", first, second, err)
	}
	t.Logf("original review PR recovered twice after one lost successful response: %s", first.URL)
}
