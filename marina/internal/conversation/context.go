package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jmlero/the-platform/marina/internal/conversationbyo"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func (c *Controller) addContext(ctx context.Context, in *conversationbyo.Input) error {
	provider, ok := c.Source.(workcontrol.ConversationContextInspection)
	if !ok {
		if c.Repository != nil {
			return errors.New("conversation context inspection unavailable")
		}
		return nil
	}
	selected, err := provider.GetConversationContext(ctx, in.MessageID)
	if err != nil {
		return err
	}
	if err := workcontrol.ValidateResolvedContext(selected.Selection, selected); err != nil {
		return err
	}
	if err := validateContext(time.Now(), selected); err != nil {
		return err
	}
	for _, source := range selected.Sources {
		if source.ProjectID != in.ProjectID || len(source.Content) > 16<<10 {
			return errors.New("conversation context escaped project or size bound")
		}
	}
	in.SelectedContext = &selected
	if selected.Selection == nil || selected.Selection.Repository == nil {
		for _, source := range selected.Sources {
			if source.Kind == "git" {
				return errors.New("selected Git context requires a repository binding")
			}
		}
		return nil
	}
	if c.Repository == nil {
		return errors.New("conversation repository reader unavailable")
	}
	binding := *selected.Selection.Repository
	reader, err := c.Repository(ctx, binding)
	if err != nil {
		return err
	}
	in.Repository, err = c.prepareRepository(ctx, in.MessageID, binding, reader)
	if err != nil {
		return err
	}
	for _, source := range selected.Sources {
		if source.Kind != "git" {
			continue
		}
		data, exists, err := reader.ReadReviewFile(ctx, binding.Repository, source.SourceRevision, source.Path)
		if err != nil {
			return err
		}
		if !exists || len(data) > 16<<10 || !utf8.Valid(data) {
			return errors.New("selected Git context is unavailable or oversized")
		}
		if source.Trust == "guidance" && instructionOverride(string(data)) {
			return errors.New("selected Git guidance contains an instruction override")
		}
		in.ContextFiles = append(in.ContextFiles, conversationbyo.ContextFile{SourceID: source.ID, Revision: source.SourceRevision, Path: source.Path, Content: string(data)})
	}
	return nil
}

type repositoryIndex struct {
	Binding  workcontrol.RepositoryBinding  `json:"binding"`
	Revision string                         `json:"revision"`
	Files    []sourcecontrol.RepositoryFile `json:"files"`
}

func (c *Controller) prepareRepository(ctx context.Context, messageID string, binding workcontrol.RepositoryBinding, reader sourcecontrol.RepositoryReader) (*conversationbyo.Repository, error) {
	if c.Journal != nil {
		items, err := c.Journal.ConversationEngineJournal().ListRuntimeArtifacts(ctx, messageID)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if item.ID != "conversation-tree-"+messageID {
				continue
			}
			var index repositoryIndex
			raw, err := c.Blobs.Get(item.Digest)
			if err != nil || json.Unmarshal(raw, &index) != nil || index.Binding != binding {
				return nil, errors.New("saved conversation repository is invalid")
			}
			return &conversationbyo.Repository{Binding: index.Binding, Revision: index.Revision, TreeDigest: item.Digest}, nil
		}
	}
	head, err := reader.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: binding.Repository, Ref: binding.BaseBranch})
	if err != nil {
		return nil, err
	}
	files, err := reader.ListRepositoryFiles(ctx, binding.Repository, head.Commit)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(repositoryIndex{Binding: binding, Revision: head.Commit, Files: files})
	if err != nil {
		return nil, err
	}
	digest, err := c.Blobs.Put(encoded)
	if err != nil {
		return nil, err
	}
	if c.Journal != nil {
		_, _, err = c.Journal.ConversationEngineJournal().RecordRuntimeArtifact(ctx, rt.Artifact{ID: "conversation-tree-" + messageID, ExecutionID: messageID, RuntimeAttemptID: messageID, Name: "repository-tree.json", Kind: "conversation-repository-tree", URI: "artifact://" + digest, Digest: digest, SizeBytes: int64(len(encoded)), CreatedAt: time.Now().UTC()})
		if err != nil {
			return nil, err
		}
	}
	return &conversationbyo.Repository{Binding: binding, Revision: head.Commit, TreeDigest: digest}, nil
}

func (c *Controller) frozenInput(turn sqlite.ConversationTurn) (conversationbyo.Input, error) {
	return frozenInput(c.Blobs.Get, turn.Specification.ContextDigest, turn.Specification.InputDigest)
}

func frozenInput(read func(string) ([]byte, error), digest, inputDigest string) (conversationbyo.Input, error) {
	var in conversationbyo.Input
	raw, err := read(digest)
	if err != nil {
		return in, err
	}
	if json.Unmarshal(raw, &in) != nil || in.InputDigest != inputDigest || conversationbyo.ValidateInput(in) != nil {
		return in, errors.New("frozen conversation context is invalid")
	}
	return in, nil
}

func checkSelection(ctx context.Context, source workcontrol.ConversationSource, in conversationbyo.Input) error {
	if in.SelectedContext == nil {
		return nil
	}
	provider, ok := source.(workcontrol.ConversationContextInspection)
	if !ok {
		return errors.New("conversation context inspection unavailable")
	}
	current, err := provider.GetConversationContext(ctx, in.MessageID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current.Selection, in.SelectedContext.Selection) {
		return errors.New("conversation context selection changed")
	}
	if err := workcontrol.ValidateResolvedContext(in.SelectedContext.Selection, current); err != nil {
		return err
	}
	return validateContext(time.Now(), current)
}

func validateContext(now time.Time, resolved workcontrol.ResolvedContext) error {
	if err := workcontrol.CheckContextValidity(now, resolved); err != nil {
		return err
	}
	for _, source := range resolved.Sources {
		for _, other := range resolved.Sources {
			if slices.Contains(source.ConflictsWith, other.ID) {
				return errors.New("selected conversation context sources conflict")
			}
		}
		if source.Trust != "guidance" {
			continue
		}
		if instructionOverride(source.Content) {
			return errors.New("selected conversation guidance contains an instruction override")
		}
	}
	return nil
}

func instructionOverride(content string) bool {
	for _, marker := range []string{"ignore previous instructions", "ignore all previous", "ignore task permissions", "override system", "reveal credentials", "publish every secret"} {
		if strings.Contains(strings.ToLower(content), marker) {
			return true
		}
	}
	return false
}
