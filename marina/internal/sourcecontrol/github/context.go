package github

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"
)

// ReadContextFile fetches bytes at an immutable Git revision through the same
// configured provider and credential boundary as repository execution. It never
// follows download URLs, symlinks, submodules, or arbitrary external sources.
func (client *Client) ReadContextFile(ctx context.Context, repository, revision, filename string) (string, error) {
	if err := client.repository(repository); err != nil {
		return "", err
	}
	_, revisionErr := hex.DecodeString(revision)
	if (len(revision) != 40 && len(revision) != 64) || revisionErr != nil || strings.ToLower(revision) != revision || filename == "" || filename == "." || filename == ".." || path.IsAbs(filename) || path.Clean(filename) != filename || strings.HasPrefix(filename, "../") || strings.ContainsAny(filename, "\\\x00\r\n") {
		return "", errors.New("context file requires an exact commit and safe relative path")
	}
	segments := strings.Split(filename, "/")
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	var result struct {
		Type, Encoding, Content string
		Size                    int
	}
	status, err := client.request(ctx, http.MethodGet, "/contents/"+strings.Join(segments, "/")+"?ref="+revision, nil, &result, 0)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", httpError("read pinned context file", status)
	}
	if result.Type != "file" || result.Encoding != "base64" || result.Size < 0 || result.Size > 16<<10 {
		return "", errors.New("context source must be a regular UTF-8 file no larger than 16 KiB")
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(result.Content, "\n", ""))
	if err != nil || len(content) != result.Size || len(content) > 16<<10 || !utf8.Valid(content) || strings.ContainsRune(string(content), '\x00') {
		return "", errors.New("invalid or oversized context file")
	}
	return string(content), nil
}
