package flow

import (
	"context"
	"errors"
	"fmt"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func (c *Controller) advanceLanding(ctx context.Context, e execution.Execution, r workcontrol.Run, s state) error {
	source, err := c.Source(e)
	if err != nil {
		return err
	}
	if s.Step == "ci" || s.Step == "land" {
		head, err := source.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: c.Config.Git.Repository, Ref: s.Branch})
		if err != nil {
			return err
		}
		if head.Commit != s.Head {
			return c.block(ctx, e, r, "Task branch changed after independent verification")
		}
		base, err := source.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: c.Config.Git.Repository, Ref: c.Config.Git.BaseBranch})
		if err != nil {
			return err
		}
		if base.Commit != s.Base && base.Commit != s.Head {
			return c.block(ctx, e, r, "Base branch moved before landing; create a revised task")
		}
		checks, err := source.ObserveChecks(ctx, sourcecontrol.ObservationRequest{Repository: c.Config.Git.Repository, HeadRevision: s.Head})
		if err != nil {
			return err
		}
		for _, required := range c.Config.RequiredCI {
			found := false
			for _, check := range checks {
				if check.ID != required || check.HeadRevision != s.Head {
					continue
				}
				found = true
				if check.State == "failure" || check.State == "failed" || check.State == "cancelled" || check.State == "timed_out" || check.State == "action_required" {
					return c.block(ctx, e, r, "Required CI failed on the task branch: "+required)
				}
				if check.State != "success" {
					return nil
				}
			}
			if !found {
				return nil
			}
		}
		if s.Step == "ci" {
			s.Step = "land"
			return c.save(ctx, e, s)
		}
		landing, ok := source.(sourcecontrol.LandingPort)
		if !ok {
			return errors.New("configured source does not support guarded fast-forward")
		}
		revision, err := landing.FastForward(ctx, sourcecontrol.FastForwardRequest{Repository: c.Config.Git.Repository, BaseRef: c.Config.Git.BaseBranch, ExpectedBase: s.Base, HeadRef: s.Branch, ExpectedHead: s.Head, AuthorityFence: e.FenceToken})
		if err != nil {
			return err
		}
		if revision.Commit != s.Head || revision.Ref != c.Config.Git.BaseBranch {
			return errors.New("landing response did not confirm the exact commit on the base branch")
		}
		if r.Status != workcontrol.RunVerifying {
			if err := c.Provider.BeginVerification(ctx, r.ID, guard(e, r, "verify")); err != nil {
				return err
			}
		}
		if e.State != execution.StateVerifying {
			if err := c.transition(ctx, e, execution.StateVerifying, "Required CI passed; exact commit landed on the base branch"); err != nil {
				return err
			}
		}
		s.Step = "evidence"
		return c.save(ctx, e, s)
	}
	if s.Step == "evidence" {
		return c.evidence(ctx, e, r, s)
	}
	if s.Step == "complete" {
		if err := c.Provider.CompleteRun(ctx, r.ID, guard(e, r, "complete"), fmt.Sprintf("Verified commit landed on %s: %s", c.Config.Git.BaseBranch, s.Head), s.EvidenceIDs); err != nil {
			return err
		}
		return c.transition(ctx, e, execution.StateCompleted, "Verified commit landed on the base branch")
	}
	return fmt.Errorf("unknown landing step %q", s.Step)
}
