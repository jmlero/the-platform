package flow

import (
	"errors"
	"reflect"
	"strings"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

func validatePRDCandidate(task workcontrol.WorkItem, candidate workresult.Candidate) error {
	p := task.PRD
	if p == nil {
		return nil
	}
	if p.Validate() != nil || p.Digest != p.ContentDigest() || p.MessageID == "" || p.ApprovalDigest == "" || !strings.HasPrefix(p.ApprovedBy, "operator:") || p.ApprovedAt <= 0 || task.Context == nil || task.Context.Repository == nil || !reflect.DeepEqual(*task.Context.Repository, p.Repository) {
		return errors.New("PRD approval or repository binding is invalid")
	}
	if len(candidate.Changes) != 1 {
		return errors.New("PRD publication must change only the approved document")
	}
	change := candidate.Changes[0]
	content := string(change.Content)
	if change.Text != nil {
		content = *change.Text
	}
	if change.Path != p.Path || change.Delete || (change.Mode != "" && change.Mode != "100644") || content != p.Content {
		return errors.New("PRD candidate differs from the exact approved path or text")
	}
	return nil
}
