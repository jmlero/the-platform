package flow

import (
	"context"
	"fmt"

	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func (c *Controller) confirmDependencies(ctx context.Context, source sourcecontrol.Port, task workcontrol.WorkItem, base string) error {
	if ref := task.PRDReference; ref != nil {
		detail, err := c.Provider.GetWork(ctx, ref.PublicationTaskID)
		if err != nil {
			return err
		}
		p := detail.Work
		if p.ProjectID != task.ProjectID || p.Status != "done" || p.PRD == nil || p.Landing == nil || p.PRD.Digest != ref.Digest || p.PRD.Path != ref.Path || p.Landing.Commit != ref.Revision || ref.Repository.Repository != c.Config.Git.Repository || ref.Repository.BaseBranch != c.Config.Git.BaseBranch {
			return fmt.Errorf("PRD source no longer matches its approved publication: %w", errLandingUnavailable)
		}
		if ref.Revision != base {
			ancestry, ok := source.(sourcecontrol.AncestryPort)
			if !ok {
				return errLandingUnavailable
			}
			contained, err := ancestry.ContainsCommit(ctx, c.Config.Git.Repository, ref.Revision, base)
			if err != nil {
				return err
			}
			if !contained {
				return fmt.Errorf("PRD source is no longer on main: %w", errLandingUnavailable)
			}
		}
	}
	for _, id := range task.DependsOn {
		detail, err := c.Provider.GetWork(ctx, id)
		if err != nil {
			return err
		}
		landing := detail.Work.Landing
		if landing == nil {
			continue
		} // v1 dependencies retain Deck's human-acceptance gate.
		if detail.Work.ProjectID != task.ProjectID || landing.Repository != c.Config.Git.Repository || landing.BaseBranch != c.Config.Git.BaseBranch {
			return fmt.Errorf("prerequisite %s belongs to another repository or base: %w", id, errLandingUnavailable)
		}
		if landing.Commit == base {
			continue
		}
		ancestry, ok := source.(sourcecontrol.AncestryPort)
		if !ok {
			return fmt.Errorf("cannot observe prerequisite %s on main: %w", id, errLandingUnavailable)
		}
		contained, err := ancestry.ContainsCommit(ctx, c.Config.Git.Repository, landing.Commit, base)
		if err != nil {
			return err
		}
		if !contained {
			return fmt.Errorf("prerequisite %s is no longer on main: %w", id, errLandingUnavailable)
		}
	}
	return nil
}
