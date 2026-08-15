package appdeck

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const maxBearerTokenFileSize = 4096

var ErrCredentialUnavailable = errors.New("AppDeck credential is unavailable")

type bearerTokenSource interface {
	Token() (string, error)
}

type fileBearerTokenSource struct {
	path string
}

func newFileBearerTokenSource(path string) (*fileBearerTokenSource, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("AppDeck token file path is required: %w", ErrInvalidConfiguration)
	}
	source := &fileBearerTokenSource{path: path}
	if _, err := source.Token(); err != nil {
		return nil, err
	}
	return source, nil
}

// Token reads the reference for each HTTP request so a projected or atomically
// replaced secret takes effect without replacing Marina's stable identity or
// restarting an active reconciliation controller.
func (source *fileBearerTokenSource) Token() (string, error) {
	file, err := os.Open(source.path)
	if err != nil {
		return "", fmt.Errorf("open AppDeck token file: %w: %w", err, ErrCredentialUnavailable)
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxBearerTokenFileSize+1))
	if err != nil {
		return "", fmt.Errorf("read AppDeck token file: %w: %w", err, ErrCredentialUnavailable)
	}
	if len(payload) > maxBearerTokenFileSize {
		return "", fmt.Errorf("AppDeck token file exceeds size limit: %w", ErrCredentialUnavailable)
	}
	payload = bytes.TrimSpace(payload)
	if len(payload) < 32 {
		return "", fmt.Errorf("AppDeck token must contain at least 32 characters: %w", ErrCredentialUnavailable)
	}
	for _, character := range payload {
		if character < 0x21 || character > 0x7e {
			return "", fmt.Errorf("AppDeck token contains an invalid character: %w", ErrCredentialUnavailable)
		}
	}
	return string(payload), nil
}
