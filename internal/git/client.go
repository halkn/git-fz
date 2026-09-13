package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type Client struct {
	Path string
	Dir  string
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

	cmd := exec.CommandContext(ctx, path, args...)
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
	result, err := c.executeAt(ctx, c.Dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(result.Stdout))
	if root == "" {
		return "", errors.New("git rev-parse returned an empty repository root")
	}
	return root, nil
}
