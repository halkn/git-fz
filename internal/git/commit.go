package git

import (
	"context"
	"fmt"
	"strconv"
)

type Commit struct {
	SHA     string
	Subject string
	Date    string
	Author  string
}

func (c *Client) ListCommits(ctx context.Context) ([]Commit, error) {
	result, err := c.Execute(ctx,
		"log",
		"--all",
		"-n",
		strconv.Itoa(maxCommits),
		"--date=relative",
		"--format=%H%x00%s%x00%ad%x00%an%x00",
	)
	if err != nil {
		return nil, err
	}

	groups, err := splitNULGroups(result.Stdout, 4)
	if err != nil {
		return nil, fmt.Errorf("parse git log output: %w", err)
	}

	commits := make([]Commit, 0, len(groups))
	for _, fields := range groups {
		commits = append(commits, Commit{
			SHA:     fields[0],
			Subject: fields[1],
			Date:    fields[2],
			Author:  fields[3],
		})
	}
	if len(commits) == 0 {
		state, err := c.headState(ctx)
		if err != nil {
			return nil, err
		}
		if state != headStateUnborn {
			return nil, fmt.Errorf("git log returned no commits while HEAD is not unborn")
		}
		return nil, ErrNoCommits
	}
	return commits, nil
}

const maxCommits = 1000

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
