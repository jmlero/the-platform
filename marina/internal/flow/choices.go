package flow

import (
	"errors"
	"path"
	"strings"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

// WithChoices applies a Run's frozen Workload choices to the installation
// profile (decision 0033). Choices can narrow limits and pick gates; they
// cannot raise the installation's ceilings or drop CI from landing on main.
func (c Config) WithChoices(choices *workcontrol.FactoryChoices) (Config, error) {
	if choices == nil {
		return c, nil
	}
	out := c
	if len(choices.Checks) == 0 || len(choices.Checks) > 16 {
		return c, errors.New("a workload needs one to sixteen checks")
	}
	out.Checks = make([]workresult.Check, 0, len(choices.Checks))
	for _, check := range choices.Checks {
		if check.ID == "" || len(check.Command) == 0 || !relativePath(check.Directory, true) {
			return c, errors.New("each check needs an id, a command and a directory inside the repository")
		}
		out.Checks = append(out.Checks, workresult.Check{ID: check.ID, Directory: check.Directory, Command: append([]string(nil), check.Command...)})
	}
	if len(choices.AllowedPaths) == 0 {
		return c, errors.New("a workload needs allowed paths")
	}
	for _, allowed := range choices.AllowedPaths {
		if !relativePath(allowed, false) {
			return c, errors.New("allowed paths must stay inside the repository")
		}
	}
	out.AllowedPaths = append([]string(nil), choices.AllowedPaths...)
	switch choices.Delivery {
	case "fast_forward":
		if len(choices.RequiredCI) == 0 {
			return c, errors.New("landing on main needs required CI checks")
		}
	case "pull_request":
	default:
		return c, errors.New("delivery must be pull_request or fast_forward")
	}
	out.Delivery, out.RequiredCI = choices.Delivery, append([]string(nil), choices.RequiredCI...)
	out.RequireApproval = choices.RequireApproval
	if choices.Review && c.ReviewerModel == nil {
		return c, errors.New("independent review needs an installed reviewer model")
	}
	out.SkipReview = !choices.Review
	if choices.RepairFailures < 0 || choices.RepairFailures > 5 {
		return c, errors.New("repair limits allow zero to five failures")
	}
	limit := choices.RepairFailures
	out.RepairFailures = &limit
	if choices.MaxTokens > c.MaxTokens || choices.BudgetMinutes > c.BudgetMinutes {
		return c, errors.New("workload limits exceed the installation's ceilings")
	}
	if choices.MaxTokens > 0 {
		out.MaxTokens = choices.MaxTokens
	}
	if choices.BudgetMinutes > 0 {
		out.BudgetMinutes = choices.BudgetMinutes
	}
	return out, nil
}

func relativePath(value string, allowRoot bool) bool {
	if value == "" || value == "." {
		return allowRoot
	}
	clean := path.Clean(value)
	return clean == value && !path.IsAbs(clean) && clean != ".." && !strings.HasPrefix(clean, "../")
}

// failureLimit is the number of failures before a repository run blocks.
func (c Config) failureLimit() int {
	if c.RepairFailures != nil {
		return *c.RepairFailures
	}
	return repositoryFailureLimit
}
