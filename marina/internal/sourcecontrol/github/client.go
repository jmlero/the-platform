// Package github implements publication on GitHub and configured GitHub
// Enterprise Server installations. Workers never receive its write credential.
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
)

type Config struct {
	APIURL      string                             `json:"api_url"`
	Repository  string                             `json:"repository"`
	CloneURL    string                             `json:"clone_url"`
	TokenFile   string                             `json:"token_file"`
	BaseBranch  string                             `json:"base_branch"`
	AuthorName  string                             `json:"author_name"`
	AuthorEmail string                             `json:"author_email"`
	CommitTime  time.Time                          `json:"-"`
	Authorize   func(context.Context, int64) error `json:"-"`
	ReadBlob    func(string) ([]byte, error)       `json:"-"`
}

type Client struct {
	config Config
	http   *http.Client
}

var _ sourcecontrol.Port = (*Client)(nil)
var _ sourcecontrol.LandingPort = (*Client)(nil)

func New(config Config, client *http.Client) (*Client, error) {
	u, err := url.Parse(config.APIURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		return nil, errors.New("Git API requires HTTPS, or loopback HTTP for tests")
	}
	parts := strings.Split(config.Repository, "/")
	if len(parts) != 2 || !segment(parts[0]) || !segment(parts[1]) {
		return nil, errors.New("Git repository must be owner/name")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{config: config, http: &copyClient}, nil
}

func segment(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/\\?%#\x00 \r\n")
}
func (client *Client) repository(repository string) error {
	if repository != client.config.Repository {
		return errors.New("repository is outside the configured binding")
	}
	return nil
}
func (client *Client) root() string {
	return strings.TrimRight(client.config.APIURL, "/") + "/repos/" + client.config.Repository
}

func (client *Client) CurrentHead(ctx context.Context, request sourcecontrol.HeadRequest) (sourcecontrol.Revision, error) {
	if err := client.repository(request.Repository); err != nil {
		return sourcecontrol.Revision{}, err
	}
	if !validBranch(request.Ref) {
		return sourcecontrol.Revision{}, sourcecontrol.ErrInvalidInput
	}
	var result struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	status, err := client.request(ctx, http.MethodGet, "/git/ref/heads/"+url.PathEscape(request.Ref), nil, &result, 0)
	if err != nil {
		return sourcecontrol.Revision{}, err
	}
	if status != 200 {
		return sourcecontrol.Revision{}, httpError("inspect branch", status)
	}
	if result.Object.SHA == "" {
		return sourcecontrol.Revision{}, errors.New("Git provider omitted branch head")
	}
	return sourcecontrol.Revision{Commit: result.Object.SHA, Ref: request.Ref}, nil
}

func (client *Client) FastForward(ctx context.Context, request sourcecontrol.FastForwardRequest) (sourcecontrol.Revision, error) {
	if err := client.repository(request.Repository); err != nil {
		return sourcecontrol.Revision{}, err
	}
	if !validBranch(request.BaseRef) || !strings.HasPrefix(request.HeadRef, "marina/") || !validBranch(request.HeadRef) ||
		request.BaseRef == request.HeadRef || request.ExpectedBase == "" || request.ExpectedHead == "" || request.ExpectedBase == request.ExpectedHead ||
		request.AuthorityFence < 1 || client.config.BaseBranch == "" || request.BaseRef != client.config.BaseBranch {
		return sourcecontrol.Revision{}, sourcecontrol.ErrInvalidInput
	}
	head, err := client.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: request.Repository, Ref: request.HeadRef})
	if err != nil {
		return sourcecontrol.Revision{}, err
	}
	if head.Commit != request.ExpectedHead {
		return sourcecontrol.Revision{}, errors.New("task branch changed after verification")
	}
	base, err := client.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: request.Repository, Ref: request.BaseRef})
	if err != nil {
		return sourcecontrol.Revision{}, err
	}
	if base.Commit == request.ExpectedHead {
		return base, nil
	}
	if base.Commit != request.ExpectedBase {
		return sourcecontrol.Revision{}, errors.New("base branch moved before landing")
	}
	status, updateErr := client.request(ctx, http.MethodPatch, "/git/refs/heads/"+url.PathEscape(request.BaseRef), map[string]any{"sha": request.ExpectedHead, "force": false}, nil, request.AuthorityFence)
	if updateErr == nil && status == 200 {
		return sourcecontrol.Revision{Commit: request.ExpectedHead, Ref: request.BaseRef}, nil
	}
	// GitHub may have applied the update and lost its response. Read the ref
	// before any retry; an exact match is the same successful operation.
	current, readErr := client.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: request.Repository, Ref: request.BaseRef})
	if readErr == nil && current.Commit == request.ExpectedHead {
		return current, nil
	}
	if updateErr != nil {
		return sourcecontrol.Revision{}, updateErr
	}
	return sourcecontrol.Revision{}, httpError("fast-forward base", status)
}

func validBranch(value string) bool {
	return value != "" && !strings.ContainsAny(value, " ~^:?*[\\\x00\r\n") && !strings.Contains(value, "..") && !strings.HasPrefix(value, "/") && !strings.HasSuffix(value, "/")
}

func (client *Client) CreateBranch(ctx context.Context, request sourcecontrol.BranchRequest) (sourcecontrol.Revision, error) {
	if err := client.repository(request.Repository); err != nil {
		return sourcecontrol.Revision{}, err
	}
	if !strings.HasPrefix(request.Name, "marina/") || !validBranch(request.Name) || request.BaseRevision == "" || request.IdempotencyKey == "" {
		return sourcecontrol.Revision{}, sourcecontrol.ErrInvalidInput
	}
	existing, err := client.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: request.Repository, Ref: request.Name})
	if err == nil {
		if existing.Commit != request.BaseRevision {
			return sourcecontrol.Revision{}, errors.New("existing branch has a different base")
		}
		return existing, nil
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 404 {
		return sourcecontrol.Revision{}, err
	}
	status, err := client.request(ctx, http.MethodPost, "/git/refs", map[string]string{"ref": "refs/heads/" + request.Name, "sha": request.BaseRevision}, nil, request.AuthorityFence)
	if err != nil {
		return sourcecontrol.Revision{}, err
	}
	if status != 201 {
		return sourcecontrol.Revision{}, httpError("create branch", status)
	}
	return sourcecontrol.Revision{Commit: request.BaseRevision, Ref: request.Name}, nil
}

func (client *Client) WriteChanges(ctx context.Context, request sourcecontrol.WriteRequest) (sourcecontrol.Revision, error) {
	if err := request.Validate(); err != nil {
		return sourcecontrol.Revision{}, err
	}
	if err := client.repository(request.Repository); err != nil {
		return sourcecontrol.Revision{}, err
	}
	if !strings.HasPrefix(request.Branch, "marina/") || !validBranch(request.Branch) || client.config.ReadBlob == nil || client.config.CommitTime.IsZero() || client.config.AuthorEmail == "" || client.config.AuthorName == "" {
		return sourcecontrol.Revision{}, sourcecontrol.ErrInvalidInput
	}
	var base struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	status, err := client.request(ctx, http.MethodGet, "/git/commits/"+url.PathEscape(request.ExpectedHead), nil, &base, 0)
	if err != nil {
		return sourcecontrol.Revision{}, err
	}
	if status != 200 || base.Tree.SHA == "" {
		return sourcecontrol.Revision{}, httpError("inspect base tree", status)
	}
	tree := make([]map[string]any, 0, len(request.Changes))
	for _, change := range request.Changes {
		entry := map[string]any{"path": change.Path, "mode": "100644", "type": "blob", "sha": nil}
		if change.Operation != sourcecontrol.ChangeDelete {
			content, err := client.config.ReadBlob(change.ContentDigest)
			if err != nil {
				return sourcecontrol.Revision{}, err
			}
			var blob struct {
				SHA string `json:"sha"`
			}
			status, err := client.request(ctx, http.MethodPost, "/git/blobs", map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString(content)}, &blob, request.AuthorityFence)
			if err != nil {
				return sourcecontrol.Revision{}, err
			}
			if status != 201 || blob.SHA == "" {
				return sourcecontrol.Revision{}, httpError("create blob", status)
			}
			entry["sha"] = blob.SHA
			if change.Mode == "100755" {
				entry["mode"] = "100755"
			}
		}
		tree = append(tree, entry)
	}
	var createdTree struct {
		SHA string `json:"sha"`
	}
	status, err = client.request(ctx, http.MethodPost, "/git/trees", map[string]any{"base_tree": base.Tree.SHA, "tree": tree}, &createdTree, request.AuthorityFence)
	if err != nil {
		return sourcecontrol.Revision{}, err
	}
	if status != 201 || createdTree.SHA == "" {
		return sourcecontrol.Revision{}, httpError("create tree", status)
	}
	identity := map[string]string{"name": client.config.AuthorName, "email": client.config.AuthorEmail, "date": client.config.CommitTime.UTC().Format(time.RFC3339)}
	var commit struct {
		SHA string `json:"sha"`
	}
	status, err = client.request(ctx, http.MethodPost, "/git/commits", map[string]any{"message": "Marina bounded repository change\n\nMarina-Operation: " + request.IdempotencyKey,
		"tree": createdTree.SHA, "parents": []string{request.ExpectedHead}, "author": identity, "committer": identity}, &commit, request.AuthorityFence)
	if err != nil {
		return sourcecontrol.Revision{}, err
	}
	if status != 201 || commit.SHA == "" {
		return sourcecontrol.Revision{}, httpError("create commit", status)
	}
	current, err := client.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: request.Repository, Ref: request.Branch})
	if err != nil {
		return sourcecontrol.Revision{}, err
	}
	if current.Commit == commit.SHA {
		return current, nil
	}
	if current.Commit != request.ExpectedHead {
		return sourcecontrol.Revision{}, errors.New("branch head changed outside this publication")
	}
	status, err = client.request(ctx, http.MethodPatch, "/git/refs/heads/"+url.PathEscape(request.Branch), map[string]any{"sha": commit.SHA, "force": false}, nil, request.AuthorityFence)
	if err != nil {
		return sourcecontrol.Revision{}, err
	}
	if status != 200 {
		return sourcecontrol.Revision{}, httpError("advance branch", status)
	}
	return sourcecontrol.Revision{Commit: commit.SHA, Ref: request.Branch}, nil
}

type pull struct {
	Number int    `json:"number"`
	URL    string `json:"html_url"`
	State  string `json:"state"`
	Body   string `json:"body"`
	Head   struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

func (value pull) domain() sourcecontrol.PullRequest {
	return sourcecontrol.PullRequest{ID: strconv.Itoa(value.Number), URL: value.URL, HeadRevision: value.Head.SHA, State: value.State}
}

func (client *Client) CreatePullRequest(ctx context.Context, request sourcecontrol.PullRequestRequest) (sourcecontrol.PullRequest, error) {
	if err := client.repository(request.Repository); err != nil {
		return sourcecontrol.PullRequest{}, err
	}
	if !strings.HasPrefix(request.HeadRef, "marina/") || !validBranch(request.HeadRef) || !validBranch(request.BaseRef) || request.IdempotencyKey == "" || request.Title == "" {
		return sourcecontrol.PullRequest{}, sourcecontrol.ErrInvalidInput
	}
	head, err := client.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: request.Repository, Ref: request.HeadRef})
	if err != nil {
		return sourcecontrol.PullRequest{}, err
	}
	if head.Commit != request.ExpectedHead {
		return sourcecontrol.PullRequest{}, errors.New("pull-request head changed")
	}
	marker := "<!-- marina:" + request.IdempotencyKey + " -->"
	query := url.Values{"head": {strings.Split(request.Repository, "/")[0] + ":" + request.HeadRef}, "base": {request.BaseRef}, "state": {"all"}, "per_page": {"100"}}
	var existing []pull
	status, err := client.request(ctx, http.MethodGet, "/pulls?"+query.Encode(), nil, &existing, 0)
	if err != nil {
		return sourcecontrol.PullRequest{}, err
	}
	if status != 200 {
		return sourcecontrol.PullRequest{}, httpError("recover pull request", status)
	}
	for _, candidate := range existing {
		if strings.Contains(candidate.Body, marker) {
			return candidate.domain(), nil
		}
	}
	if len(existing) > 0 {
		return sourcecontrol.PullRequest{}, errors.New("branch already has an unrelated pull request")
	}
	var created pull
	status, err = client.request(ctx, http.MethodPost, "/pulls", map[string]any{"title": request.Title, "body": request.Body + "\n\n" + marker, "base": request.BaseRef, "head": request.HeadRef, "maintainer_can_modify": false}, &created, request.AuthorityFence)
	if err != nil {
		return sourcecontrol.PullRequest{}, err
	}
	if status != 201 {
		return sourcecontrol.PullRequest{}, httpError("create pull request", status)
	}
	return created.domain(), nil
}

func (client *Client) GetPullRequest(ctx context.Context, request sourcecontrol.ObservationRequest) (sourcecontrol.PullRequest, error) {
	if err := client.repository(request.Repository); err != nil {
		return sourcecontrol.PullRequest{}, err
	}
	number, err := strconv.Atoi(request.PullRequestID)
	if err != nil || number < 1 {
		return sourcecontrol.PullRequest{}, sourcecontrol.ErrInvalidInput
	}
	var result pull
	status, err := client.request(ctx, http.MethodGet, "/pulls/"+strconv.Itoa(number), nil, &result, 0)
	if err != nil {
		return sourcecontrol.PullRequest{}, err
	}
	if status != 200 {
		return sourcecontrol.PullRequest{}, httpError("inspect pull request", status)
	}
	return result.domain(), nil
}

func (client *Client) ObserveChecks(ctx context.Context, request sourcecontrol.ObservationRequest) ([]sourcecontrol.CheckObservation, error) {
	if err := client.repository(request.Repository); err != nil {
		return nil, err
	}
	var result struct {
		TotalCount int `json:"total_count"`
		Runs       []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HeadSHA    string `json:"head_sha"`
		} `json:"check_runs"`
	}
	status, err := client.request(ctx, http.MethodGet, "/commits/"+url.PathEscape(request.HeadRevision)+"/check-runs?per_page=100", nil, &result, 0)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, httpError("observe CI checks", status)
	}
	if result.TotalCount > 100 {
		return nil, errors.New("CI check result exceeds the supported page; refusing incomplete evidence")
	}
	items := []sourcecontrol.CheckObservation{}
	for _, check := range result.Runs {
		state := check.Status
		if state == "completed" {
			state = check.Conclusion
		}
		items = append(items, sourcecontrol.CheckObservation{ID: check.Name, HeadRevision: check.HeadSHA, State: state, ObservedAt: time.Now().UTC()})
	}
	var statuses struct {
		TotalCount int    `json:"total_count"`
		SHA        string `json:"sha"`
		Statuses   []struct {
			Context string `json:"context"`
			State   string `json:"state"`
		} `json:"statuses"`
	}
	status, err = client.request(ctx, http.MethodGet, "/commits/"+url.PathEscape(request.HeadRevision)+"/status?per_page=100", nil, &statuses, 0)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, httpError("observe commit statuses", status)
	}
	if statuses.TotalCount > 100 {
		return nil, errors.New("commit statuses exceed the supported page")
	}
	for _, check := range statuses.Statuses {
		items = append(items, sourcecontrol.CheckObservation{ID: check.Context, HeadRevision: statuses.SHA, State: check.State, ObservedAt: time.Now().UTC()})
	}
	return items, nil
}

func (client *Client) ObserveReviews(ctx context.Context, request sourcecontrol.ObservationRequest) ([]sourcecontrol.ReviewObservation, error) {
	if err := client.repository(request.Repository); err != nil {
		return nil, err
	}
	if _, err := strconv.Atoi(request.PullRequestID); err != nil {
		return nil, sourcecontrol.ErrInvalidInput
	}
	var reviews []struct {
		ID       int64  `json:"id"`
		CommitID string `json:"commit_id"`
		State    string `json:"state"`
		User     struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	status, err := client.request(ctx, http.MethodGet, "/pulls/"+request.PullRequestID+"/reviews?per_page=100", nil, &reviews, 0)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, httpError("observe reviews", status)
	}
	if len(reviews) == 100 {
		return nil, errors.New("review history may be incomplete")
	}
	latest := map[string]sourcecontrol.ReviewObservation{}
	for _, review := range reviews {
		if review.State == "APPROVED" || review.State == "CHANGES_REQUESTED" || review.State == "DISMISSED" {
			latest[review.User.Login] = sourcecontrol.ReviewObservation{ID: strconv.FormatInt(review.ID, 10), HeadRevision: review.CommitID, State: review.State, ObservedAt: time.Now().UTC()}
		}
	}
	items := []sourcecontrol.ReviewObservation{}
	for _, review := range latest {
		items = append(items, review)
	}
	return items, nil
}

type APIError struct {
	Action string
	Status int
}

func (err *APIError) Error() string {
	return fmt.Sprintf("Git provider %s returned HTTP %d", err.Action, err.Status)
}
func (err *APIError) RetryableFailure() bool    { return err.Status == 429 || err.Status >= 500 }
func httpError(action string, status int) error { return &APIError{Action: action, Status: status} }

func (client *Client) request(ctx context.Context, method, path string, body, result any, fence int64) (int, error) {
	if method != http.MethodGet {
		if client.config.Authorize == nil || fence < 1 {
			return 0, errors.New("Git mutation requires a live authority check")
		}
		if err := client.config.Authorize(ctx, fence); err != nil {
			return 0, err
		}
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.root()+path, reader)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Content-Type", "application/json")
	if client.config.TokenFile != "" {
		token, err := os.ReadFile(client.config.TokenFile)
		if err != nil || strings.TrimSpace(string(token)) == "" {
			return 0, errors.New("read Git provider credential")
		}
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	}
	response, err := client.http.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if result != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(result); err != nil {
			return response.StatusCode, err
		}
	}
	return response.StatusCode, nil
}
