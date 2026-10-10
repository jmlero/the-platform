package github

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"path"
	"strings"
)

// ReadReviewFile reads the actual Git tree and blob, rather than a contents
// endpoint that can follow a symlink. A missing path is established only by a
// successful, complete tree read; authentication and transport failures block.
func (client *Client) ReadReviewFile(ctx context.Context, repository, revision, filename string) ([]byte, bool, error) {
	if err := client.repository(repository); err != nil {
		return nil, false, err
	}
	_, err := hex.DecodeString(revision)
	if err != nil || (len(revision) != 40 && len(revision) != 64) || strings.ToLower(revision) != revision ||
		filename == "" || filename == "." || filename == ".." || path.IsAbs(filename) || path.Clean(filename) != filename ||
		strings.HasPrefix(filename, "../") || strings.ContainsAny(filename, "\\\x00\r\n") {
		return nil, false, errors.New("review file requires an exact base and safe path")
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	status, err := client.request(ctx, http.MethodGet, "/git/commits/"+revision, nil, &commit, 0)
	if err != nil {
		return nil, false, err
	}
	if status != http.StatusOK {
		return nil, false, httpError("read review base", status)
	}
	if !validObjectID(commit.Tree.SHA) {
		return nil, false, errors.New("review base omitted its tree")
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
		return nil, false, err
	}
	if status != http.StatusOK {
		return nil, false, httpError("read review tree", status)
	}
	if tree.Truncated || tree.SHA != commit.Tree.SHA {
		return nil, false, errors.New("review requires a complete pinned tree")
	}
	for _, entry := range tree.Tree {
		if entry.Path != filename {
			continue
		}
		if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755") || entry.Size < 0 || entry.Size > 256<<10 || !validObjectID(entry.SHA) {
			return nil, false, errors.New("review source must be a bounded regular file")
		}
		var blob struct {
			SHA, Encoding, Content string
			Size                   int
		}
		status, err = client.request(ctx, http.MethodGet, "/git/blobs/"+entry.SHA, nil, &blob, 0)
		if err != nil {
			return nil, false, err
		}
		if status != http.StatusOK {
			return nil, false, httpError("read review blob", status)
		}
		if blob.SHA != entry.SHA || blob.Encoding != "base64" || blob.Size != entry.Size {
			return nil, false, errors.New("review blob differs from its tree")
		}
		data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(blob.Content, "\n", ""))
		if err != nil || len(data) != blob.Size {
			return nil, false, errors.New("review blob bytes or size are invalid")
		}
		return data, true, nil
	}
	return nil, false, nil
}

func validObjectID(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil && (len(value) == 40 || len(value) == 64) && strings.ToLower(value) == value
}
