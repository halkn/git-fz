package git

import (
	"bytes"
	"context"
	"fmt"
)

type Change struct {
	Path           string
	IndexStatus    byte
	WorktreeStatus byte
}

func (c Change) Status() string { return string([]byte{c.IndexStatus, c.WorktreeStatus}) }

func (c Change) CanStage() bool {
	return c.WorktreeStatus != ' ' && c.WorktreeStatus != '!'
}

func (c Change) CanUnstage() bool {
	return c.IndexStatus != ' ' && c.IndexStatus != '?' && c.IndexStatus != '!'
}

func (c *Client) ListChanges(ctx context.Context) ([]Change, error) {
	if err := c.EnsureRepository(ctx); err != nil {
		return nil, err
	}
	result, err := c.Execute(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	changes, err := parseChanges(result.Stdout)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return nil, ErrNoChanges
	}
	return changes, nil
}

func parseChanges(data []byte) ([]Change, error) {
	records := bytes.Split(data, []byte{0})
	if len(records) > 0 && len(records[len(records)-1]) == 0 {
		records = records[:len(records)-1]
	}
	changes := make([]Change, 0, len(records))
	for i := 0; i < len(records); i++ {
		record := records[i]
		if len(record) < 4 || record[2] != ' ' {
			return nil, fmt.Errorf("parse git status output: malformed record %q", record)
		}
		change := Change{
			Path:           string(record[3:]),
			IndexStatus:    record[0],
			WorktreeStatus: record[1],
		}
		if record[0] == 'R' || record[0] == 'C' || record[1] == 'R' || record[1] == 'C' {
			if i+1 >= len(records) {
				return nil, fmt.Errorf("parse git status output: rename record has no destination")
			}
			i++
			change.Path = string(records[i])
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func (c *Client) Add(ctx context.Context, paths ...string) (Result, error) {
	args := append([]string{"add", "--"}, paths...)
	return c.Execute(ctx, args...)
}

func (c *Client) Unstage(ctx context.Context, paths ...string) (Result, error) {
	if _, err := c.Execute(ctx, "rev-parse", "--verify", "HEAD^{commit}"); err != nil {
		args := append([]string{"rm", "--cached", "--"}, paths...)
		return c.Execute(ctx, args...)
	}
	args := append([]string{"restore", "--staged", "--"}, paths...)
	return c.Execute(ctx, args...)
}

func (c *Client) StatusShort(ctx context.Context) (Result, error) {
	return c.Execute(ctx, "status", "--short", "--untracked-files=all")
}

func (c *Client) Diff(ctx context.Context, path string, staged bool) (Result, error) {
	args := []string{"diff", "--color=always", "--no-ext-diff"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	return c.Execute(ctx, args...)
}

func (c *Client) UntrackedDiff(ctx context.Context, path string) (Result, error) {
	args := []string{"diff", "--no-index", "--color=always", "--no-ext-diff", "--", "/dev/null", path}
	return c.Execute(ctx, args...)
}
