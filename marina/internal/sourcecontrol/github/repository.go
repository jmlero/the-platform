package github

import (
	"context"
	"errors"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
)

// ListRepositoryFiles freezes a complete regular-file index at one exact commit.
// Links and submodules are never exposed as readable files.
func (client *Client) ListRepositoryFiles(ctx context.Context, repository, revision string) ([]sourcecontrol.RepositoryFile, error) {
	if err := client.repository(repository); err != nil {
		return nil, err
	}
	if !validObjectID(revision) {
		return nil, errors.New("repository index requires an exact commit")
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	status, err := client.request(ctx, http.MethodGet, "/git/commits/"+revision, nil, &commit, 0)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, httpError("read repository commit", status)
	}
	if !validObjectID(commit.Tree.SHA) {
		return nil, errors.New("repository commit omitted tree")
	}
	var tree struct {
		SHA       string `json:"sha"`
		Truncated bool   `json:"truncated"`
		Tree      []struct {
			Path, Mode, Type, SHA string
			Size                  int
		} `json:"tree"`
	}
	status, err = client.request(ctx, http.MethodGet, "/git/trees/"+commit.Tree.SHA+"?recursive=1", nil, &tree, 0)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, httpError("read repository index", status)
	}
	if tree.Truncated || tree.SHA != commit.Tree.SHA || len(tree.Tree) > 10000 {
		return nil, errors.New("repository requires a complete bounded tree")
	}
	files := []sourcecontrol.RepositoryFile{}
	seen, bytes := map[string]bool{}, 0
	for _, entry := range tree.Tree {
		if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755") {
			continue
		}
		if entry.Path == "" || entry.Path == "." || entry.Path == ".." || path.IsAbs(entry.Path) || path.Clean(entry.Path) != entry.Path || strings.HasPrefix(entry.Path, "../") || strings.ContainsAny(entry.Path, "\\\x00\r\n") || !validObjectID(entry.SHA) || entry.Size < 0 || seen[entry.Path] {
			return nil, errors.New("repository tree contains invalid file metadata")
		}
		seen[entry.Path] = true
		bytes += len(entry.Path) + 64
		if bytes > 1<<20 {
			return nil, errors.New("repository index exceeds size bound")
		}
		files = append(files, sourcecontrol.RepositoryFile{Path: entry.Path, Size: entry.Size})
	}
	slices.SortFunc(files, func(a, b sourcecontrol.RepositoryFile) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}
