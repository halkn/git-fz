package git

import (
	"context"
	"fmt"
	"strings"
)

type Branch struct {
	Name      string
	Ref       string
	Remote    string
	IsCurrent bool
}

func (b Branch) IsRemote() bool { return b.Remote != "" }

func (c *Client) ListBranches(ctx context.Context) ([]Branch, error) {
	result, err := c.Execute(ctx,
		"for-each-ref",
		"--sort=-committerdate",
		"--format=%(refname)%00%(refname:short)%00%(HEAD)%00%(symref)%00",
		"refs/heads",
		"refs/remotes",
	)
	if err != nil {
		return nil, err
	}

	return parseBranches(result.Stdout)
}

func parseBranches(data []byte) ([]Branch, error) {
	groups, err := splitNULGroups(data, 4)
	if err != nil {
		return nil, fmt.Errorf("parse git branch output: %w", err)
	}

	branches := make([]Branch, 0, len(groups))
	for _, fields := range groups {
		rawRef := fields[0]
		shortRef := fields[1]
		isCurrent := fields[2] == "*"
		symref := fields[3]
		if symref != "" {
			continue
		}

		switch {
		case strings.HasPrefix(rawRef, "refs/heads/"):
			branches = append(branches, Branch{
				Name:      strings.TrimPrefix(rawRef, "refs/heads/"),
				Ref:       rawRef,
				IsCurrent: isCurrent,
			})
		case strings.HasPrefix(rawRef, "refs/remotes/"):
			remoteRef := strings.TrimPrefix(rawRef, "refs/remotes/")
			parts := strings.SplitN(remoteRef, "/", 2)
			if len(parts) != 2 || parts[1] == "" {
				continue
			}
			branches = append(branches, Branch{
				Name:   parts[1],
				Ref:    shortRef,
				Remote: parts[0],
			})
		}
	}
	if len(branches) == 0 {
		return nil, ErrNoBranches
	}
	return branches, nil
}

func (c *Client) Switch(ctx context.Context, branch Branch) (Result, error) {
	if branch.IsRemote() {
		return c.Execute(ctx, "switch", "--track", branch.Ref)
	}
	return c.Execute(ctx, "switch", "--", branch.Name)
}

func (c *Client) BranchLog(ctx context.Context, ref string) (Result, error) {
	return c.Execute(ctx,
		"log",
		"--color=always",
		"--decorate",
		"--oneline",
		"-n",
		"20",
		ref,
	)
}
