package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type Client struct {
	Path string
	Dir  string

	rootMu sync.Mutex
	root   string
}

type Result struct {
	Stdout []byte
	Stderr []byte
}

type CommandError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *CommandError) Error() string {
	command := strings.Join(e.Args, " ")
	if e.Stderr != "" {
		return fmt.Sprintf("git %s: %s", command, strings.TrimSpace(e.Stderr))
	}
	return fmt.Sprintf("git %s: %v", command, e.Err)
}

func (e *CommandError) Unwrap() error { return e.Err }

func (e *CommandError) ExitCode() int {
	var exitError *exec.ExitError
	if errors.As(e.Err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}

func New(path string) *Client { return &Client{Path: path} }

func NewInDir(dir, path string) *Client { return &Client{Path: path, Dir: dir} }

func (c *Client) executable() (string, error) {
	if c.Path != "" {
		path, err := exec.LookPath(c.Path)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrGitUnavailable, err)
		}
		return path, nil
	}
	path, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrGitUnavailable, err)
	}
	return path, nil
}

func (c *Client) Execute(ctx context.Context, args ...string) (Result, error) {
	root, err := c.repositoryRoot(ctx)
	if err != nil {
		if errors.Is(err, ErrGitUnavailable) {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("%w: %v", ErrNotRepository, err)
	}
	return c.executeAt(ctx, root, args...)
}

func (c *Client) executeAt(ctx context.Context, dir string, args ...string) (Result, error) {
	path, err := c.executable()
	if err != nil {
		return Result{}, err
	}

	env, err := c.environment()
	if err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, &CommandError{
			Args:   args,
			Stderr: stderr.String(),
			Err:    err,
		}
	}
	return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, nil
}

func (c *Client) EnsureRepository(ctx context.Context) error {
	if _, err := c.repositoryRoot(ctx); err != nil {
		if errors.Is(err, ErrGitUnavailable) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrNotRepository, err)
	}
	return nil
}

func (c *Client) repositoryRoot(ctx context.Context) (string, error) {
	c.rootMu.Lock()
	defer c.rootMu.Unlock()
	if c.root != "" {
		return c.root, nil
	}

	result, err := c.executeAt(ctx, c.Dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	root := strings.TrimSuffix(string(result.Stdout), "\n")
	if root == "" {
		return "", errors.New("git rev-parse returned an empty repository root")
	}
	c.root = root
	return root, nil
}

func (c *Client) environment() ([]string, error) {
	baseDir, err := filepath.Abs(c.Dir)
	if err != nil {
		return nil, fmt.Errorf("resolve git working directory: %w", err)
	}
	env := os.Environ()
	for i, variable := range env {
		key, value, ok := strings.Cut(variable, "=")
		if !ok || (key != "GIT_DIR" && key != "GIT_WORK_TREE") || value == "" || filepath.IsAbs(value) {
			continue
		}
		env[i] = key + "=" + filepath.Join(baseDir, value)
	}
	return env, nil
}

type headState uint8

const (
	headStateUnknown headState = iota
	headStateWithCommit
	headStateUnborn
)

func (c *Client) headState(ctx context.Context) (headState, error) {
	_, headErr := c.Execute(ctx, "rev-parse", "--verify", "HEAD^{commit}")
	if headErr == nil {
		return headStateWithCommit, nil
	}

	if _, err := c.Execute(ctx, "symbolic-ref", "-q", "HEAD"); err != nil {
		return headStateUnknown, fmt.Errorf("determine HEAD state: %w", headErr)
	}
	result, err := c.Execute(ctx, "rev-list", "--all", "--max-count=1")
	if err != nil {
		return headStateUnknown, fmt.Errorf("determine repository history: %w", err)
	}
	if len(bytes.TrimSpace(result.Stdout)) != 0 {
		return headStateUnknown, headErr
	}
	return headStateUnborn, nil
}
