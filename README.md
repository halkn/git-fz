# git-fz

`git-fz` adds small fuzzy selectors to common Git operations. It is used as
the external Git subcommand `git fz` and keeps Git responsible for the actual
operation.

## Requirements

- Git 2.23 or newer
- [fzf](https://github.com/junegunn/fzf)

## Installation

```console
go install github.com/halkn/git-fz/cmd/git-fz@latest
```

Make sure the directory containing Go-installed binaries is on `PATH`.

To build from a checkout:

```console
go build -o "$(go env GOPATH)/bin/git-fz" ./cmd/git-fz
```

## Usage

Run the commands from inside a Git repository:

```console
git fz switch
git fz log
git fz stage
```

`switch` lists local and remote branches, previews recent commits, and runs
`git switch` for the selected branch. Remote branches are passed to
`git switch --track`, so the local branch name and tracking configuration use
Git's normal rules.

`log` previews the selected commit with `git show` and prints its full SHA to
standard output when accepted. This makes it useful in shell pipelines.

`stage` lists tracked and untracked changes. Select rows with `TAB`, use
`Ctrl-S` to stage selected rows, and `Ctrl-U` to unstage selected rows. The
picker reloads the current Git status after each operation; `ENTER` finishes
and prints the short status. If an operation fails, its Git error stays in the
picker header so it can be corrected and retried. Discard, commit, and hunk or
line staging are not part of the MVP.

The selector passes opaque payloads to fzf and decodes them inside the
binary, so paths and branch names containing spaces, tabs, newlines, or
non-ASCII characters do not need shell quoting or display-string parsing.

## Development

```console
go test ./...
go build ./cmd/git-fz
```
