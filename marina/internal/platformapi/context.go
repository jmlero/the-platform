package platformapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

// frozenContext turns a client's context items into the selection and
// resolved sources the workflow validates. Each item's digest must be the
// SHA-256 of its content, so a client cannot misstate what it froze.
func frozenContext(projectID string, items []ContextItem, binding *workcontrol.RepositoryBinding, now time.Time) (*workcontrol.TaskContext, workcontrol.ResolvedContext, error) {
	resolved := workcontrol.ResolvedContext{Sources: []workcontrol.ContextSource{}, Messages: []workcontrol.ContextMessage{}}
	if len(items) == 0 && binding == nil {
		return nil, resolved, nil
	}
	selection := &workcontrol.TaskContext{Repository: binding, Sources: []workcontrol.ContextReference{}, Messages: []workcontrol.ContextReference{}, AuthorizedAt: now.UnixMilli()}
	for i, item := range items {
		if item.Trust != "guidance" && item.Trust != "data" {
			return nil, resolved, fmt.Errorf("context %q has unknown trust", item.Name)
		}
		switch item.Kind {
		case "document", "repository-file":
			if item.Kind == "document" && sha256Digest(item.Content) != item.Digest {
				return nil, resolved, fmt.Errorf("context %q digest does not match its content", item.Name)
			}
			if item.Kind == "repository-file" && (item.Path == "" || item.Revision == "" || binding == nil) {
				return nil, resolved, fmt.Errorf("context %q needs a path, a revision and a repository", item.Name)
			}
			kind := "document"
			if item.Kind == "repository-file" {
				kind = "git"
			}
			id := fmt.Sprintf("ctx-%d", i+1)
			if item.Source != nil && item.Source.ID != "" {
				id = item.Source.ID
			}
			source := workcontrol.ContextSource{ContextSourceInput: workcontrol.ContextSourceInput{ID: id, Kind: kind, Title: item.Name, Content: item.Content,
				Path: item.Path, SourceRevision: item.Revision, Trust: item.Trust, State: "active"}, ProjectID: projectID, Revision: 1, CreatedAt: now.UnixMilli()}
			if item.Source != nil && item.Source.Revision > 0 {
				source.Revision = item.Source.Revision
			}
			source.Digest = workcontrol.ContextSourceDigest(source)
			resolved.Sources = append(resolved.Sources, source)
			selection.Sources = append(selection.Sources, workcontrol.ContextReference{ID: source.ID, Revision: source.Revision, Digest: source.Digest})
		case "message":
			if item.Trust != "data" || sha256Digest(item.Content) != item.Digest {
				return nil, resolved, fmt.Errorf("message %q must be data with a matching digest", item.Name)
			}
			message := workcontrol.ContextMessage{ID: fmt.Sprintf("msg-%d", i+1), AuthorType: "human", Content: item.Content, Trust: "data", CreatedAt: now.UnixMilli()}
			message.Digest = workcontrol.ContextMessageDigest(message)
			resolved.Messages = append(resolved.Messages, message)
			selection.Messages = append(selection.Messages, workcontrol.ContextReference{ID: message.ID, Revision: 1, Digest: message.Digest})
		default:
			return nil, resolved, errors.New("unknown context kind " + item.Kind)
		}
	}
	resolved.Selection = selection
	return selection, resolved, workcontrol.ValidateResolvedContext(selection, resolved)
}

func sha256Digest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}
