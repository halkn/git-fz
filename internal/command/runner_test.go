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
		if !strings.Contains(options.Bind[0], "{+1}") || !strings.Contains(options.Bind[0], "__stage-source") {
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
