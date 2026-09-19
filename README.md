# git-fz

`git-fz` adds small fuzzy selectors to common Git operations. It is used as
the external Git subcommand `git fz` and keeps Git responsible for the actual
operation.

## Requirements

- Git 2.23 or newer
- [fzf](https://github.com/junegunn/fzf)

## Installation

Each tagged release ships prebuilt archives for `aarch64-apple-darwin` and
`x86_64-unknown-linux-gnu`, together with `SHA256SUMS`. Download one from the
[releases page](https://github.com/halkn/git-fz/releases), extract it, and put
the `git-fz` binary on `PATH`.

With [mise](https://mise.jdx.dev):

```console
mise use -g github:halkn/git-fz@latest
```

With Go:

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

`git fz --version` prints the release version for a downloaded archive, and the
module or VCS-derived version that Go stamps into any other build.

`switch` lists local and remote branches, previews recent commits, and runs
`git switch` for the selected branch. Remote branches are passed to
`git switch --track`, so the local branch name and tracking configuration use
Git's normal rules.

`log` searches up to the latest 1,000 commits across local and remote refs,
previews the selected commit with `git show`, and prints its full SHA to
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
gofmt -l .
go vet ./...
go test ./...
go build ./cmd/git-fz
```

CI runs the same checks on every push and pull request. Pushing a `v*` tag
builds the release archives and publishes them.
