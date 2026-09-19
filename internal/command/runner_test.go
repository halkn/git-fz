package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halkn/git-fz/internal/fzf"
	"github.com/halkn/git-fz/internal/git"
)

type fakePicker struct {
	selectFn func([]fzf.Item, fzf.Options) ([]string, error)
	items    []fzf.Item
	options  fzf.Options
}

func (p *fakePicker) Select(_ context.Context, items []fzf.Item, options fzf.Options) ([]string, error) {
	p.items = items
	p.options = options
	return p.selectFn(items, options)
}

func TestRunLogPrintsSelectedSHA(t *testing.T) {
	dir := newCommandRepository(t)
	client := git.NewInDir(dir, "")
	picker := &fakePicker{selectFn: func(items []fzf.Item, _ fzf.Options) ([]string, error) {
		if len(items) != 1 {
			t.Fatalf("picker received %d items, want 1", len(items))
		}
		return []string{items[0].Payload}, nil
	}}
	runner := NewRunner(client, picker, "/tmp/git-fz")
	var stdout, stderr bytes.Buffer
	if err := runner.Run(context.Background(), []string{"log"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(stdout.String())
	if len(sha) != 40 {
		t.Fatalf("stdout = %q, want a full SHA", stdout.String())
	}
	if !strings.Contains(picker.options.Preview, "__preview commit {1}") {
		t.Fatalf("preview = %q", picker.options.Preview)
	}
}

func TestRunVersionPrintsInjectedVersion(t *testing.T) {
	previous := Version
	t.Cleanup(func() { Version = previous })
	Version = "1.2.3"
	runner := NewRunner(git.New(""), &fakePicker{}, "/tmp/git-fz")
	var stdout, stderr bytes.Buffer
	if err := runner.Run(context.Background(), []string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); got != "git-fz 1.2.3\n" {
		t.Fatalf("stdout = %q, want %q", got, "git-fz 1.2.3\n")
	}
}

func TestRunHelpListsVersionOption(t *testing.T) {
	runner := NewRunner(git.New(""), &fakePicker{}, "/tmp/git-fz")
	var stdout, stderr bytes.Buffer
	if err := runner.Run(context.Background(), []string{"--help"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "--version") {
		t.Fatalf("usage = %q, want it to mention --version", stdout.String())
	}
}

func TestRunSwitchUsesRemoteTrackingBranch(t *testing.T) {
	dir := newCommandRepository(t)
	runCommandGit(t, dir, "remote", "add", "origin", "https://example.invalid/origin.git")
	runCommandGit(t, dir, "update-ref", "refs/remotes/origin/feature/remote", "HEAD")
	client := git.NewInDir(dir, "")
	picker := &fakePicker{selectFn: func(items []fzf.Item, _ fzf.Options) ([]string, error) {
		for _, item := range items {
			if strings.Contains(item.Display, "[remote origin] feature/remote") {
				return []string{item.Payload}, nil
			}
		}
		t.Fatal("remote branch was not offered to picker")
		return nil, nil
	}}
	runner := NewRunner(client, picker, "/tmp/git-fz")
	var stdout, stderr bytes.Buffer
	if err := runner.Run(context.Background(), []string{"switch"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(runCommandGit(t, dir, "branch", "--show-current"))); got != "feature/remote" {
		t.Fatalf("current branch = %q, want feature/remote", got)
	}
}

func TestRunStageConfiguresMultiSelectActions(t *testing.T) {
	dir := newCommandRepository(t)
	writeCommandFile(t, dir, "space name 日本.txt", "changed\n")
	client := git.NewInDir(dir, "")
	picker := &fakePicker{selectFn: func(items []fzf.Item, options fzf.Options) ([]string, error) {
		if len(items) != 1 || !options.Multi {
			t.Fatalf("stage picker = items %#v options %#v", items, options)
		}
		if len(options.Bind) != 2 || !strings.Contains(options.Bind[0], "ctrl-s") || !strings.Contains(options.Bind[1], "ctrl-u") {
			t.Fatalf("stage bindings = %#v", options.Bind)
		}
		if !strings.Contains(options.Bind[0], "{+1}") || !strings.Contains(options.Bind[0], "__stage-source") || !strings.Contains(options.Bind[0], "transform") || !strings.Contains(options.Bind[0], "reload-sync") {
			t.Fatalf("stage binding = %#v", options.Bind)
		}
		return []string{items[0].Payload}, nil
	}}
	runner := NewRunner(client, picker, "/tmp/git-fz")
	var stdout, stderr bytes.Buffer
	if err := runner.Run(context.Background(), []string{"stage"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "??") || !strings.Contains(stdout.String(), "space name") {
		t.Fatalf("status output = %q", stdout.String())
	}
	if !strings.Contains(picker.options.Preview, "__preview change {1}") {
		t.Fatalf("preview = %q", picker.options.Preview)
	}
}

func TestRunStageTreatsNoMatchAsFinish(t *testing.T) {
	dir := newCommandRepository(t)
	path := "untracked.txt"
	writeCommandFile(t, dir, path, "untracked\n")
	picker := &fakePicker{selectFn: func([]fzf.Item, fzf.Options) ([]string, error) {
		return nil, fzf.ErrNoMatch
	}}
	runner := NewRunner(git.NewInDir(dir, ""), picker, "/tmp/git-fz")
	var stdout, stderr bytes.Buffer
	if err := runner.Run(context.Background(), []string{"stage"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "?? "+path) {
		t.Fatalf("status output = %q", stdout.String())
	}
}

func TestRunNormalizesPickerCancellationAtCallerBoundary(t *testing.T) {
	dir := newCommandRepository(t)
	picker := &fakePicker{selectFn: func([]fzf.Item, fzf.Options) ([]string, error) {
		return nil, fzf.ErrCancelled
	}}
	runner := NewRunner(git.NewInDir(dir, ""), picker, "/tmp/git-fz")
	var stdout, stderr bytes.Buffer
	err := runner.Run(context.Background(), []string{"log"}, &stdout, &stderr)
	if !errors.Is(err, fzf.ErrCancelled) {
		t.Fatalf("Run() error = %v, want ErrCancelled", err)
	}
}

func TestRunStageActionHandlesEncodedPath(t *testing.T) {
	dir := newCommandRepository(t)
	path := "space name 日本.txt"
	writeCommandFile(t, dir, path, "untracked\n")
	change := git.Change{Path: path, IndexStatus: '?', WorktreeStatus: '?'}
	payload, err := marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	token := fzf.EncodePayload(payload)
	runner := NewRunner(git.NewInDir(dir, ""), &fakePicker{}, "/tmp/git-fz")
	var stdout, stderr bytes.Buffer
	if err := runner.Run(context.Background(), []string{"__stage-action", "stage", token}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	status := string(runCommandGit(t, dir, "status", "--porcelain=v1", "--", path))
	if !strings.Contains(status, "A  ") {
		t.Fatalf("status = %q, want staged path", status)
	}
}

func TestRunPreviewShowsCommitDiff(t *testing.T) {
	dir := newCommandRepository(t)
	client := git.NewInDir(dir, "")
	commits, err := client.ListCommits(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := marshal(commits[0])
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(client, &fakePicker{}, "/tmp/git-fz")
	var stdout bytes.Buffer
	if err := runner.Run(context.Background(), []string{"__preview", "commit", fzf.EncodePayload(payload)}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "first commit") {
		t.Fatalf("preview = %q", stdout.String())
	}
}

func TestRunChangePreviewShowsDiffErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		change git.Change
	}{
		{name: "tracked", change: git.Change{Path: "tracked.txt", WorktreeStatus: 'M'}},
		{name: "untracked", change: git.Change{Path: "untracked", IndexStatus: '?', WorktreeStatus: '?'}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			fakeGit := filepath.Join(dir, "git")
			script := "#!/bin/sh\nif [ \"$1\" = rev-parse ]; then printf '%s\\n' \"$PWD\"; exit 0; fi\nprintf 'preview unavailable\\n' >&2\nexit 2\n"
			if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			payload, err := marshal(test.change)
			if err != nil {
				t.Fatal(err)
			}
			runner := NewRunner(git.NewInDir(dir, fakeGit), &fakePicker{}, "/tmp/git-fz")
			var stdout bytes.Buffer
			if err := runner.Run(context.Background(), []string{"__preview", "change", fzf.EncodePayload(payload)}, &stdout, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), "preview unavailable") {
				t.Fatalf("preview error = %q", stdout.String())
			}
		})
	}
}

func TestRunStageActionUnstagesRenameWithoutChangingWorktree(t *testing.T) {
	dir := newCommandRepository(t)
	oldPath := "old 日本.txt"
	newPath := "new 日本.txt"
	writeCommandFile(t, dir, oldPath, "original\n")
	runCommandGit(t, dir, "add", "--", oldPath)
	runCommandGit(t, dir, "commit", "-m", "rename source")
	runCommandGit(t, dir, "mv", oldPath, newPath)
	client := git.NewInDir(dir, "")
	changes, err := client.ListChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !changes[0].IsRename() {
		t.Fatalf("rename changes = %#v", changes)
	}
	payload, err := marshal(changes[0])
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(client, &fakePicker{}, "/tmp/git-fz")
	var stdout, stderr bytes.Buffer
	if err := runner.Run(context.Background(), []string{"__stage-action", "unstage", fzf.EncodePayload(payload)}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if cached := strings.TrimSpace(string(runCommandGit(t, dir, "diff", "--cached", "--name-status"))); cached != "" {
		t.Fatalf("cached rename after unstage = %q", cached)
	}
	if _, err := os.Stat(filepath.Join(dir, newPath)); err != nil {
		t.Fatalf("destination missing after unstage: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, oldPath)); !os.IsNotExist(err) {
		t.Fatalf("source exists after unstage: %v", err)
	}
}

func TestRunRenamePreviewAndStageUseDestination(t *testing.T) {
	dir := newCommandRepository(t)
	oldPath := "old 日本.txt"
	newPath := "new 日本.txt"
	writeCommandFile(t, dir, oldPath, "original\n")
	runCommandGit(t, dir, "add", "--", oldPath)
	runCommandGit(t, dir, "commit", "-m", "rename source")
	runCommandGit(t, dir, "mv", oldPath, newPath)
	writeCommandFile(t, dir, newPath, "original\nupdated\n")
	unrelatedPath := "unrelated.txt"
	writeCommandFile(t, dir, unrelatedPath, "unrelated\n")
	runCommandGit(t, dir, "add", "--", unrelatedPath)

	client := git.NewInDir(dir, "")
	changes, err := client.ListChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var rename git.Change
	for _, change := range changes {
		if change.IsRename() {
			rename = change
			break
		}
	}
	if !rename.IsRename() || rename.Path != newPath || rename.OriginalPath != oldPath {
		t.Fatalf("rename change = %#v", rename)
	}

	payload, err := marshal(rename)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(client, &fakePicker{}, "/tmp/git-fz")
	var preview bytes.Buffer
	if err := runner.Run(context.Background(), []string{"__preview", "change", fzf.EncodePayload(payload)}, &preview, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.String(), "updated") {
		t.Fatalf("rename preview = %q", preview.String())
	}

	var stdout, stderr bytes.Buffer
	if err := runner.Run(context.Background(), []string{"__stage-action", "stage", fzf.EncodePayload(payload)}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	cached := string(runCommandGit(t, dir, "diff", "--cached", "--name-status", "-z"))
	if !strings.Contains(cached, newPath) || !strings.Contains(cached, unrelatedPath) {
		t.Fatalf("cached paths after rename stage = %q", cached)
	}
	stdout.Reset()
	if err := runner.Run(context.Background(), []string{"__stage-action", "unstage", fzf.EncodePayload(payload)}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	cached = string(runCommandGit(t, dir, "diff", "--cached", "--name-only", "-z"))
	if strings.Contains(cached, oldPath) || strings.Contains(cached, newPath) || !strings.Contains(cached, unrelatedPath) {
		t.Fatalf("cached paths after rename unstage = %q", cached)
	}
}

func TestRunDeletedFilePreviewStageAndUnstagePreserveOtherStagedChanges(t *testing.T) {
	dir := newCommandRepository(t)
	deletedPath := "deleted 日本.txt"
	stagedPath := "keep staged.txt"
	writeCommandFile(t, dir, deletedPath, "content that will be deleted\n")
	writeCommandFile(t, dir, stagedPath, "before\n")
	runCommandGit(t, dir, "add", "--", deletedPath, stagedPath)
	runCommandGit(t, dir, "commit", "-qm", "add files")
	if err := os.Remove(filepath.Join(dir, deletedPath)); err != nil {
		t.Fatal(err)
	}
	writeCommandFile(t, dir, stagedPath, "before\na staged change\n")
	runCommandGit(t, dir, "add", "--", stagedPath)

	client := git.NewInDir(dir, "")
	changes, err := client.ListChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var deleted git.Change
	for _, change := range changes {
		if change.Path == deletedPath {
			deleted = change
			break
		}
	}
	if deleted.Path == "" || deleted.Status() != " D" {
		t.Fatalf("deleted change = %#v, want unstaged deletion", deleted)
	}

	payload, err := marshal(deleted)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(client, &fakePicker{}, "/tmp/git-fz")
	var preview bytes.Buffer
	if err := runner.Run(context.Background(), []string{"__preview", "change", fzf.EncodePayload(payload)}, &preview, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.String(), "content that will be deleted") {
		t.Fatalf("deleted preview = %q", preview.String())
	}

	var stdout, stderr bytes.Buffer
	if err := runner.Run(context.Background(), []string{"__stage-action", "stage", fzf.EncodePayload(payload)}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if status := string(runCommandGit(t, dir, "status", "--porcelain=v1", "-z")); !strings.Contains(status, "D  "+deletedPath) || !strings.Contains(status, "M  "+stagedPath) {
		t.Fatalf("status after staging deletion = %q", status)
	}
	stagedDeleted := deleted
	stagedDeleted.IndexStatus = 'D'
	stagedDeleted.WorktreeStatus = ' '
	preview.Reset()
	if err := runner.Run(context.Background(), []string{"__preview", "change", fzf.EncodePayload(payloadForChange(t, stagedDeleted))}, &preview, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.String(), "content that will be deleted") {
		t.Fatalf("staged deleted preview = %q", preview.String())
	}

	stdout.Reset()
	if err := runner.Run(context.Background(), []string{"__stage-action", "unstage", fzf.EncodePayload(payloadForChange(t, stagedDeleted))}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	status := string(runCommandGit(t, dir, "status", "--porcelain=v1", "-z"))
	if !strings.Contains(status, " D "+deletedPath) || !strings.Contains(status, "M  "+stagedPath) {
		t.Fatalf("status after unstaging deletion = %q", status)
	}
	if _, err := os.Stat(filepath.Join(dir, deletedPath)); !os.IsNotExist(err) {
		t.Fatalf("deleted file was restored after unstage: %v", err)
	}
}

func payloadForChange(t *testing.T, change git.Change) string {
	t.Helper()
	payload, err := marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestRunStageActionTransformReportsFailureAndAllowsRetry(t *testing.T) {
	dir := newCommandRepository(t)
	path := "locked file.txt"
	writeCommandFile(t, dir, path, "untracked\n")
	change := git.Change{Path: path, IndexStatus: '?', WorktreeStatus: '?'}
	payload, err := marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	token := fzf.EncodePayload(payload)
	lockPath := filepath.Join(dir, ".git", "index.lock")
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(git.NewInDir(dir, ""), &fakePicker{}, "/tmp/git-fz")
	var stdout, stderr bytes.Buffer
	if err := runner.Run(context.Background(), []string{"__stage-action", "--transform", "stage", token}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "change-header:Stage failed:") {
		t.Fatalf("failure feedback = %q", stdout.String())
	}
	if status := string(runCommandGit(t, dir, "status", "--porcelain=v1", "--", path)); !strings.HasPrefix(status, "?? ") {
		t.Fatalf("status after failed stage = %q", status)
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := runner.Run(context.Background(), []string{"__stage-action", "--transform", "stage", token}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "change-header:"+stageHeader+"\n" {
		t.Fatalf("success feedback = %q", stdout.String())
	}
	if status := string(runCommandGit(t, dir, "status", "--porcelain=v1", "--", path)); !strings.HasPrefix(status, "A  ") {
		t.Fatalf("status after retry = %q", status)
	}
}

func TestRunStageActionTransformHandlesNoSelectionAndNoOp(t *testing.T) {
	runner := NewRunner(git.NewInDir(t.TempDir(), ""), &fakePicker{}, "/tmp/git-fz")
	var stdout, stderr bytes.Buffer
	if err := runner.Run(context.Background(), []string{"__stage-action", "--transform", "stage"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "usage:") || !strings.Contains(stdout.String(), "No changes selected") {
		t.Fatalf("no-selection feedback = %q", stdout.String())
	}

	stdout.Reset()
	change := git.Change{Path: "untracked.txt", IndexStatus: '?', WorktreeStatus: '?'}
	payload, err := marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(context.Background(), []string{"__stage-action", "--transform", "unstage", fzf.EncodePayload(payload)}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "No applicable changes for unstage") || stdout.String() == "change-header:"+stageHeader+"\n" {
		t.Fatalf("no-op feedback = %q", stdout.String())
	}
}

func newCommandRepository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runCommandGit(t, dir, "init", "-q", "-b", "main")
	runCommandGit(t, dir, "config", "user.name", "git-fz test")
	runCommandGit(t, dir, "config", "user.email", "git-fz@example.invalid")
	writeCommandFile(t, dir, "README.md", "first\n")
	runCommandGit(t, dir, "add", "README.md")
	runCommandGit(t, dir, "commit", "-m", "first commit")
	return dir
}

func writeCommandFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runCommandGit(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output.String())
	}
	return output.Bytes()
}
