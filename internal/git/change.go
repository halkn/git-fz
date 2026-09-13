package git

import (
	"bytes"
	"context"
	"fmt"
)

type Change struct {
	Path           string
	OriginalPath   string
	IndexStatus    byte
	WorktreeStatus byte
}

func (c Change) Status() string { return string([]byte{c.IndexStatus, c.WorktreeStatus}) }

func (c Change) IsRename() bool {
	return c.OriginalPath != "" && (c.IndexStatus == 'R' || c.WorktreeStatus == 'R')
}

func (c Change) IsCopy() bool {
	return c.OriginalPath != "" && (c.IndexStatus == 'C' || c.WorktreeStatus == 'C')
}

func (c Change) StagePaths() []string { return []string{c.Path} }

func (c Change) UnstagePaths() []string {
	if c.IsRename() {
		return []string{c.OriginalPath, c.Path}
	}
	return []string{c.Path}
}

func (c Change) CanStage() bool {
	return c.WorktreeStatus != ' ' && c.WorktreeStatus != '!'
}

func (c Change) CanUnstage() bool {
	return c.IndexStatus != ' ' && c.IndexStatus != '?' && c.IndexStatus != '!'
}

func (c *Client) ListChanges(ctx context.Context) ([]Change, error) {
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
				return nil, fmt.Errorf("parse git status output: rename record has no source")
			}
			i++
			change.OriginalPath = string(records[i])
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func (c *Client) Add(ctx context.Context, paths ...string) (Result, error) {
	args := append([]string{"--literal-pathspecs", "add", "--"}, paths...)
	return c.Execute(ctx, args...)
}

func (c *Client) Unstage(ctx context.Context, paths ...string) (Result, error) {
	state, err := c.headState(ctx)
	if err != nil {
		return Result{}, err
	}
	if state == headStateUnborn {
		args := append([]string{"--literal-pathspecs", "rm", "--cached", "--"}, paths...)
		return c.Execute(ctx, args...)
	}
	args := append([]string{"--literal-pathspecs", "restore", "--staged", "--"}, paths...)
	return c.Execute(ctx, args...)
}

func (c *Client) StatusShort(ctx context.Context) (Result, error) {
	return c.Execute(ctx, "status", "--short", "--untracked-files=all")
}

func (c *Client) Diff(ctx context.Context, path string, staged bool) (Result, error) {
	return c.DiffPaths(ctx, []string{path}, staged)
}

func (c *Client) DiffPaths(ctx context.Context, paths []string, staged bool) (Result, error) {
	args := []string{"--literal-pathspecs", "diff", "--color=always", "--no-ext-diff"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--")
	args = append(args, paths...)
	return c.Execute(ctx, args...)
}

func (c *Client) UntrackedDiff(ctx context.Context, path string) (Result, error) {
	args := []string{"diff", "--no-index", "--color=always", "--no-ext-diff", "--", "/dev/null", path}
	return c.Execute(ctx, args...)
}
