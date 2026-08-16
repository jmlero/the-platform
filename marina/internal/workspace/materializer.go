package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrInvalidInput = errors.New("invalid workspace input")
	ErrConflict     = errors.New("workspace conflict")
	revisionPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}([0-9a-fA-F]{24})?$`)
)

type Request struct {
	ExecutionID string
	AttemptID   string
	Repository  string
	Revision    string
}

type Result struct {
	Path       string
	Revision   string
	Repository string
}

type Materializer struct {
	root      string
	gitBinary string
}

func NewMaterializer(root, gitBinary string) (*Materializer, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil || strings.TrimSpace(root) == "" || strings.TrimSpace(gitBinary) == "" {
		return nil, fmt.Errorf("workspace root and Git binary are required: %w", ErrInvalidInput)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create workspace root: %w", err)
	}
	return &Materializer{root: absolute, gitBinary: strings.TrimSpace(gitBinary)}, nil
}

func (materializer *Materializer) Materialize(ctx context.Context, request Request) (Result, error) {
	request.ExecutionID = strings.TrimSpace(request.ExecutionID)
	request.AttemptID = strings.TrimSpace(request.AttemptID)
	request.Repository = strings.TrimSpace(request.Repository)
	request.Revision = strings.ToLower(strings.TrimSpace(request.Revision))
	if request.ExecutionID == "" || request.AttemptID == "" || request.Repository == "" || !revisionPattern.MatchString(request.Revision) {
		return Result{}, fmt.Errorf("execution, attempt, repository, and full commit revision are required: %w", ErrInvalidInput)
	}
	key := sha256.Sum256([]byte(request.ExecutionID + "\x00" + request.AttemptID))
	destination := filepath.Join(materializer.root, hex.EncodeToString(key[:12]))
	if result, err := materializer.inspect(ctx, destination, request); err == nil {
		return result, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}

	temporary, err := os.MkdirTemp(materializer.root, ".materialize-")
	if err != nil {
		return Result{}, fmt.Errorf("create temporary workspace: %w", err)
	}
	defer os.RemoveAll(temporary)
	checkout := filepath.Join(temporary, "repository")
	if output, err := materializer.git(ctx, "", "clone", "--no-checkout", "--", request.Repository, checkout); err != nil {
		return Result{}, fmt.Errorf("clone repository: %w: %s", err, output)
	}
	if output, err := materializer.git(ctx, checkout, "checkout", "--detach", request.Revision); err != nil {
		return Result{}, fmt.Errorf("checkout pinned revision: %w: %s", err, output)
	}
	result, err := materializer.inspect(ctx, checkout, request)
	if err != nil {
		return Result{}, err
	}
	if err := os.Rename(checkout, destination); err != nil {
		if existing, inspectErr := materializer.inspect(ctx, destination, request); inspectErr == nil {
			return existing, nil
		}
		return Result{}, fmt.Errorf("publish materialized workspace: %w", err)
	}
	result.Path = destination
	return result, nil
}

func (materializer *Materializer) inspect(ctx context.Context, path string, request Request) (Result, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Result{}, err
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("workspace path is not a directory: %w", ErrConflict)
	}
	output, err := materializer.git(ctx, path, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, fmt.Errorf("inspect workspace revision: %w: %s", err, output)
	}
	observed := strings.ToLower(strings.TrimSpace(output))
	if observed != request.Revision {
		return Result{}, fmt.Errorf("workspace revision %s does not match requested %s: %w", observed, request.Revision, ErrConflict)
	}
	return Result{Path: path, Revision: observed, Repository: request.Repository}, nil
}

func (materializer *Materializer) git(ctx context.Context, directory string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, materializer.gitBinary, arguments...)
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"LANG=C",
	}
	if directory != "" {
		command.Dir = directory
	}
	output, err := command.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}
