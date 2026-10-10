package workcontrol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

type PRDDraft struct {
	Title   string `json:"title"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (p PRDDraft) Validate() error {
	if strings.TrimSpace(p.Title) == "" || len(p.Title) > 160 || strings.TrimSpace(p.Content) == "" || len(p.Content) > 16<<10 || !utf8.ValidString(p.Content) || strings.ContainsRune(p.Content, 0) || len(p.Path) > 240 || path.Clean(p.Path) != p.Path || !strings.HasPrefix(p.Path, "docs/prd/") || !strings.HasSuffix(p.Path, ".md") || strings.ContainsAny(p.Path, "\\\r\n\x00") {
		return fmt.Errorf("PRD requires a title, bounded Markdown text and a path under docs/prd/: %w", errors.New("invalid PRD"))
	}
	return nil
}

type PRDDocument struct {
	PRDDraft
	MessageID      string            `json:"message_id"`
	Repository     RepositoryBinding `json:"repository"`
	Digest         string            `json:"digest"`
	ApprovalDigest string            `json:"approval_digest,omitempty"`
	ApprovedBy     string            `json:"approved_by,omitempty"`
	ApprovedAt     int64             `json:"approved_at,omitempty"`
}

func (p PRDDocument) ContentDigest() string {
	data, _ := json.Marshal(struct {
		Draft      PRDDraft
		Repository RepositoryBinding
	}{p.PRDDraft, p.Repository})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
