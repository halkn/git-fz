package git

import (
	"bytes"
	"context"
	"fmt"
)

type Commit struct {
	SHA     string
	Subject string
	Date    string
	Author  string
}

func (c *Client) ListCommits(ctx context.Context) ([]Commit, error) {
	if err := c.EnsureRepository(ctx); err != nil {
		return nil, err
	}
	if _, err := c.Execute(ctx, "rev-parse", "--verify", "HEAD^{commit}"); err != nil {
		return nil, ErrNoCommits
	}

	result, err := c.Execute(ctx,
		"log",
		"--all",
		"--date=relative",
		"--format=%H%x00%s%x00%ad%x00%an%x00",
		"HEAD",
	)
	if err != nil {
		return nil, err
	}

	data := bytes.TrimSuffix(result.Stdout, []byte{'\n'})
	fields := bytes.Split(data, []byte{0})
	if len(fields) > 0 && len(fields[len(fields)-1]) == 0 {
		fields = fields[:len(fields)-1]
	}
	if len(fields)%4 != 0 {
		return nil, fmt.Errorf("parse git log output: expected groups of 4 fields, got %d", len(fields))
	}

	commits := make([]Commit, 0, len(fields)/4)
	for i := 0; i < len(fields); i += 4 {
		commits = append(commits, Commit{
			SHA:     string(bytes.TrimPrefix(fields[i], []byte{'\n'})),
			Subject: string(fields[i+1]),
			Date:    string(fields[i+2]),
			Author:  string(fields[i+3]),
		})
	}
	if len(commits) == 0 {
		return nil, ErrNoCommits
	}
	return commits, nil
}

func (c *Client) ShowCommit(ctx context.Context, sha string) (Result, error) {
	return c.Execute(ctx,
		"show",
		"--color=always",
		"--no-ext-diff",
		"--format=fuller",
		"--stat",
		"--patch",
		sha,
	)
}
