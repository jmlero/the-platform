package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol/github"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

type RepositoryProvider interface {
	GetRepository(context.Context, string) (workcontrol.RepositoryRequest, error)
	RecordRepositoryResult(context.Context, string, workcontrol.RepositoryResult) error
}

func withRepository(config Config, b workcontrol.RepositoryBinding) Config {
	config.Git.Repository = b.Repository
	config.Git.CloneURL = b.CloneURL
	config.Git.BaseBranch = b.BaseBranch
	return config
}

// InitializeRepository preserves a UI-selected repository across restart only
// while the installation's private execution profile remains unchanged.
func InitializeRepository(ctx context.Context, journal *sqlite.Store, config Config, input execution.ProjectBinding) (execution.ProjectBinding, error) {
	if !config.RepositoryEnabled() {
		input.Repository, input.DeliveryConfig = "", ""
		input.ContextConfig = config.Digest()
		return journal.EnsureProjectBinding(ctx, input)
	}
	effective := config
	current, err := journal.CurrentProjectBinding(ctx, config.ProjectID)
	if err == nil {
		selected, profile, readErr := journal.RepositorySnapshot(ctx, config.ProjectID, current.Version)
		if readErr == nil && profile == config.Digest() {
			effective = withRepository(config, selected)
		} else if readErr != nil && !errors.Is(readErr, execution.ErrNotFound) {
			return current, readErr
		}
	} else if !errors.Is(err, execution.ErrNotFound) {
		return current, err
	}
	input.Repository = effective.Git.CloneURL
	input.ContextConfig = effective.Digest()
	binding, err := journal.EnsureProjectBinding(ctx, input)
	if err != nil {
		return binding, err
	}
	err = journal.RecordRepositorySnapshot(ctx, config.ProjectID, config.Digest(), workcontrol.RepositoryBinding{Version: binding.Version, Repository: effective.Git.Repository, CloneURL: effective.Git.CloneURL, BaseBranch: effective.Git.BaseBranch})
	return binding, err
}

func (c *Controller) ConfigurationForExecution(ctx context.Context, e execution.Execution) (Config, error) {
	selected, profile, err := c.Journal.RepositorySnapshot(ctx, e.ProjectID, e.BindingVersion)
	if err != nil {
		return Config{}, err
	}
	if profile != c.Config.Digest() {
		return Config{}, contextProblem("The installation execution profile changed")
	}
	snapshot, err := c.Journal.AcquiredTaskSnapshot(ctx, e.ProjectID, e.ExternalWorkID, e.TaskVersion)
	if err != nil {
		return Config{}, err
	}
	var task workcontrol.WorkItem
	if err = json.Unmarshal([]byte(snapshot), &task); err != nil {
		return Config{}, err
	}
	if task.Context != nil && task.Context.Repository != nil && *task.Context.Repository != selected {
		return Config{}, contextProblem("The authorized repository differs from its recorded binding")
	}
	return withRepository(c.Config, selected), nil
}

var repositoryName = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,99}/[A-Za-z0-9_-][A-Za-z0-9_.-]{0,99}$`)

func requestedRepository(config Config, request workcontrol.RepositoryRequest) (Config, error) {
	if !repositoryName.MatchString(request.Repository) || request.BaseBranch == "" || len(request.BaseBranch) > 200 {
		return Config{}, errors.New("Use an owner/repository name and a base branch")
	}
	u, err := url.Parse(config.Git.CloneURL)
	if err != nil {
		return Config{}, err
	}
	// The installed credential, API and clone origin remain Marina-owned. A
	// request cannot redirect credentials to a new host or supply a URL.
	u.Path = "/" + request.Repository + ".git"
	u.RawPath = ""
	selected := withRepository(config, workcontrol.RepositoryBinding{Repository: request.Repository, CloneURL: u.String(), BaseBranch: request.BaseBranch})
	if request.Repository == config.Git.Repository {
		selected.Git.CloneURL = config.Git.CloneURL
	}
	if !safeURL(selected.Git.CloneURL) || strings.ContainsAny(request.BaseBranch, " ~^:?*[\\\x00\r\n") || strings.Contains(request.BaseBranch, "..") {
		return Config{}, errors.New("Invalid repository or base branch")
	}
	return selected, nil
}

func (c *Controller) ReconcileRepository(ctx context.Context) error {
	if !c.Config.RepositoryEnabled() {
		return nil
	}
	provider, ok := c.Provider.(RepositoryProvider)
	if !ok {
		return errors.New("provider lacks repository association support")
	}
	request, err := provider.GetRepository(ctx, c.Config.ProjectID)
	if err != nil {
		return err
	}
	if request.Status != "pending" {
		return nil
	}
	result, err := c.Journal.RepositoryReceipt(ctx, c.Config.ProjectID, request.Revision)
	if err == nil {
		return provider.RecordRepositoryResult(ctx, c.Config.ProjectID, result)
	}
	if !errors.Is(err, execution.ErrNotFound) {
		return err
	}
	current, err := c.Journal.CurrentProjectBinding(ctx, c.Config.ProjectID)
	if err != nil {
		return err
	}
	reject := func(reason string) error {
		currentSummary, _, readErr := c.Journal.RepositorySnapshot(ctx, c.Config.ProjectID, current.Version)
		if readErr != nil {
			return readErr
		}
		result, err := c.Journal.RejectRepositoryRequest(ctx, c.Config.ProjectID, request.Revision, reason, &currentSummary)
		if err != nil {
			return err
		}
		return provider.RecordRepositoryResult(ctx, c.Config.ProjectID, result)
	}
	if request.ExpectedBindingVersion != 0 && request.ExpectedBindingVersion != current.Version {
		// Private installation revisions can advance Marina's binding before
		// Deck receives a new association. Accept the operator's older Deck
		// version only when its exact repository binding remains in history.
		observed, _, readErr := c.Journal.RepositorySnapshot(ctx, c.Config.ProjectID, request.ExpectedBindingVersion)
		if readErr != nil || request.Binding == nil || observed != *request.Binding || request.ExpectedBindingVersion > current.Version {
			return reject("The repository binding changed. Refresh and submit the association again.")
		}
	}
	config, err := requestedRepository(c.Config, request)
	if err != nil {
		return reject(err.Error())
	}
	client, err := github.New(config.Git, nil)
	if err != nil {
		return reject("Repository connection settings are invalid")
	}
	if _, err = client.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: config.Git.Repository, Ref: config.Git.BaseBranch}); err != nil {
		var apiError *github.APIError
		if errors.As(err, &apiError) && !apiError.RetryableFailure() {
			return reject("Marina could not read that repository and base branch. Check access and the branch name, then submit again.")
		}
		if errors.Is(err, sourcecontrol.ErrInvalidInput) {
			return reject("The base branch is invalid")
		}
		return fmt.Errorf("repository validation pending: check repository access and base branch: %w", err)
	}
	result, err = c.Journal.ApplyRepositoryRequest(ctx, c.Config.ProjectID, request.Revision, current.Version, c.Config.Digest(), config.Digest(), workcontrol.RepositoryBinding{Repository: config.Git.Repository, CloneURL: config.Git.CloneURL, BaseBranch: config.Git.BaseBranch})
	if err != nil {
		return err
	}
	return provider.RecordRepositoryResult(ctx, c.Config.ProjectID, result)
}
