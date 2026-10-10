package workcontrol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// A proposal has no execution budget or authority. The operator grants those
// separately, once for the complete ordered list.
type PRDTaskDraft struct {
	Title              string `json:"title"`
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
}
type PRDTaskProposal struct {
	PublicationTaskID string         `json:"publication_task_id"`
	Revision          string         `json:"revision"`
	Tasks             []PRDTaskDraft `json:"tasks"`
}

func (p PRDTaskProposal) Validate() error {
	raw, _ := json.Marshal(p)
	if len(raw) > 8<<10 {
		return errors.New("PRD task proposal exceeds output bound")
	}
	revision, err := hex.DecodeString(p.Revision)
	if p.PublicationTaskID == "" || len(p.PublicationTaskID) > 128 || err != nil || (len(revision) != 20 && len(revision) != 32) || strings.ToLower(p.Revision) != p.Revision || len(p.Tasks) < 1 || len(p.Tasks) > 8 {
		return errors.New("invalid PRD task proposal")
	}
	for _, t := range p.Tasks {
		if strings.TrimSpace(t.Title) == "" || len(t.Title) > 200 || strings.ContainsRune(t.Title, 0) || strings.TrimSpace(t.Description) == "" || len(t.Description) > 4000 || strings.ContainsRune(t.Description, 0) || strings.TrimSpace(t.AcceptanceCriteria) == "" || len(t.AcceptanceCriteria) > 2000 || strings.ContainsRune(t.AcceptanceCriteria, 0) {
			return fmt.Errorf("each PRD task needs bounded text and observable criteria: %w", errors.New("invalid PRD task proposal"))
		}
	}
	return nil
}

type PRDReference struct {
	PublicationTaskID string            `json:"publication_task_id"`
	PlanMessageID     string            `json:"plan_message_id"`
	Path              string            `json:"path"`
	Revision          string            `json:"revision"`
	Digest            string            `json:"digest"`
	Repository        RepositoryBinding `json:"repository"`
}
type PRDTaskPlan struct {
	PRDTaskProposal
	Source         PRDReference `json:"source"`
	Digest         string       `json:"digest"`
	ApprovalDigest string       `json:"approval_digest,omitempty"`
	ApprovedBy     string       `json:"approved_by,omitempty"`
	ApprovedAt     int64        `json:"approved_at,omitempty"`
	TaskIDs        []string     `json:"task_ids,omitempty"`
}

func (p PRDTaskPlan) ContentDigest() string {
	raw, _ := json.Marshal(struct {
		Proposal PRDTaskProposal
		Source   PRDReference
	}{p.PRDTaskProposal, p.Source})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
