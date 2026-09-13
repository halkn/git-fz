package git

import "errors"

var (
	ErrGitUnavailable = errors.New("git executable not found")
	ErrNotRepository  = errors.New("not a git repository")
	ErrNoBranches     = errors.New("no branches found")
	ErrNoCommits      = errors.New("no commits found")
	ErrNoChanges      = errors.New("no changed files found")
)
