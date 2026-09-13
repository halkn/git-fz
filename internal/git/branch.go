package git

import (
	"bytes"
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
	if err := c.EnsureRepository(ctx); err != nil {
		return nil, err
	}

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
	data = bytes.TrimSuffix(data, []byte{'\n'})
	fields := bytes.Split(data, []byte{0})
	if len(fields) > 0 && len(fields[len(fields)-1]) == 0 {
		fields = fields[:len(fields)-1]
	}
	if len(fields)%4 != 0 {
		return nil, fmt.Errorf("parse git branch output: expected groups of 4 fields, got %d", len(fields))
	}

	branches := make([]Branch, 0, len(fields)/4)
	for i := 0; i < len(fields); i += 4 {
		rawRef := strings.TrimPrefix(string(fields[i]), "\n")
		shortRef := string(fields[i+1])
		isCurrent := string(fields[i+2]) == "*"
		symref := string(fields[i+3])
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
