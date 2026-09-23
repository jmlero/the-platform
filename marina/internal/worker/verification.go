package worker

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
)

// RunVerification is the deterministic verifier entry point. It consumes only
// the pinned repository and installed checks; Harness and Analysis are ignored.
func RunVerification(ctx context.Context, input Input, root string) (Result, error) {
	result := Result{Stage: "verify", SourceRevision: input.SourceRevision}
	if input.Stage != "verify" || !revision(input.SourceRevision) || strings.TrimSpace(input.SourceURL) == "" {
		return result, errors.New("verification requires a pinned source revision")
	}
	if input.Context != nil {
		if err := input.Context.ValidateAt(time.Now()); err != nil {
			return result, err
		}
	}
	repo, environment, git, err := checkout(ctx, input, root, false)
	if err != nil {
		return result, err
	}
	if len(input.Checks) == 0 {
		return result, errors.New("verification has no trusted checks")
	}
	for _, check := range input.Checks {
		if check.ID == "" || len(check.Command) == 0 || !safeDirectory(check.Directory) {
			return result, errors.New("invalid trusted verification check")
		}
		output, exitCode, runErr := command(ctx, filepath.Join(repo, check.Directory), environment, nil, check.Command)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		var exitErr *exec.ExitError
		if runErr != nil && !errors.As(runErr, &exitErr) {
			return result, fmt.Errorf("start verification check %s: %w", check.ID, runErr)
		}
		output = redact(output, input.GitAuthorization)
		result.Checks = append(result.Checks, CheckResult{ID: check.ID, Command: check.Command, Directory: check.Directory, ExitCode: exitCode,
			Output: output, OutputDigest: artifacts.Digest([]byte(output))})
	}
	if status, err := git(repo, "status", "--porcelain", "--untracked-files=no"); err != nil || status != "" {
		return result, errors.New("verification modified tracked source files")
	}
	if head, err := git(repo, "rev-parse", "HEAD"); err != nil || head != input.SourceRevision {
		return result, errors.New("verification changed source revision")
	}
	result.Summary = "Independent repository checks completed"
	result.ObservedAt = time.Now().UTC()
	return result, nil
}

func checkout(ctx context.Context, input Input, root string, agent bool) (string, []string, func(string, ...string) (string, error), error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", nil, nil, err
	}
	for _, dir := range []string{"home", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			return "", nil, nil, err
		}
	}
	repo := filepath.Join(root, "repo")
	environment := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Join(root, "home"), "TMPDIR=" + filepath.Join(root, "tmp"),
		"LANG=C.UTF-8", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
		"GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOMODCACHE=/opt/marina-cache/go", "GOCACHE=" + filepath.Join(root, "go-cache")}
	if agent {
		if err := os.MkdirAll(filepath.Join(root, "codex"), 0700); err != nil {
			return "", nil, nil, err
		}
		environment = append(environment, "CODEX_HOME="+filepath.Join(root, "codex"))
	}
	environment = append(environment, input.Environment...)
	gitEnvironment := append([]string{}, environment...)
	if input.GitAuthorization != "" {
		gitEnvironment = append(gitEnvironment, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: Bearer "+input.GitAuthorization)
	}
	git := func(directory string, args ...string) (string, error) {
		all := append([]string{"git", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "http.followRedirects=false"}, args...)
		output, _, err := command(ctx, directory, gitEnvironment, nil, all)
		if err != nil {
			return "", fmt.Errorf("repository operation failed: %w", err)
		}
		return strings.TrimSpace(output), nil
	}
	if _, err := git(root, "init", "--quiet", repo); err != nil {
		return "", nil, nil, err
	}
	if _, err := git(repo, "fetch", "--quiet", "--depth=1", "--", input.SourceURL, input.SourceRevision); err != nil {
		return "", nil, nil, err
	}
	if _, err := git(repo, "checkout", "--quiet", "--detach", input.SourceRevision); err != nil {
		return "", nil, nil, err
	}
	head, err := git(repo, "rev-parse", "HEAD")
	if err != nil || head != input.SourceRevision {
		return "", nil, nil, errors.New("worker source revision mismatch")
	}
	return repo, environment, git, nil
}

func safeDirectory(value string) bool {
	return value != "" && !path.IsAbs(value) && path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsAny(value, "\\\x00")
}
func revision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

type cappedOutput struct{ bytes.Buffer }

func (buffer *cappedOutput) Write(value []byte) (int, error) {
	n := len(value)
	if remaining := 1<<20 - buffer.Len(); remaining > 0 {
		_, _ = buffer.Buffer.Write(value[:min(len(value), remaining)])
	}
	return n, nil
}

func command(ctx context.Context, directory string, environment []string, stdin io.Reader, args []string) (string, int, error) {
	if len(args) == 0 || args[0] == "" {
		return "", -1, errors.New("empty worker command")
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = directory
	cmd.Env = environment
	cmd.Stdin = stdin
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second
	var output cappedOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	return output.String(), code, err
}

func redact(value, credential string) string {
	if credential != "" {
		value = strings.ReplaceAll(value, credential, "[redacted]")
	}
	return value
}
