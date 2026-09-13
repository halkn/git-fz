package git

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBranches(t *testing.T) {
	data := strings.Join([]string{
		"refs/heads/main", "main", "*", "",
		"refs/heads/feature/login", "feature/login", " ", "",
		"refs/remotes/origin/feature/login", "origin/feature/login", " ", "",
		"refs/remotes/origin/HEAD", "origin/HEAD", " ", "refs/remotes/origin/main",
		"refs/remotes/upstream/日本語", "upstream/日本語", " ", "",
		"",
	}, "\x00")
	branches, err := parseBranches([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 4 {
		t.Fatalf("got %d branches, want 4: %#v", len(branches), branches)
	}
	if !branches[0].IsCurrent || branches[0].Name != "main" || branches[0].IsRemote() {
		t.Fatalf("local branch = %#v", branches[0])
	}
	if branches[3].Remote != "upstream" || branches[3].Name != "日本語" {
		t.Fatalf("non-ASCII remote branch = %#v", branches[3])
	}
}

func TestParseBranchesRejectsIncompleteRecord(t *testing.T) {
	if _, err := parseBranches([]byte("refs/heads/main\x00main\x00")); err == nil {
		t.Fatal("parseBranches() returned nil error for incomplete record")
	}
}

func TestRepositoryBranchAndCommitOperations(t *testing.T) {
	dir := newRepository(t)
	writeFile(t, dir, "README.md", "first\n")
	runGit(t, dir, "add", "--", "README.md")
	runGit(t, dir, "commit", "-m", "first commit")
	runGit(t, dir, "branch", "feature/login")
	runGit(t, dir, "remote", "add", "origin", "https://example.invalid/origin.git")
	runGit(t, dir, "update-ref", "refs/remotes/origin/remote-only", "HEAD")
	runGit(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	client := NewInDir(dir, "")
	branches, err := client.ListBranches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var foundLocal, foundRemote bool
	for _, branch := range branches {
		if branch.Name == "feature/login" && !branch.IsRemote() {
			foundLocal = true
		}
		if branch.Name == "main" && !branch.IsRemote() && !branch.IsCurrent {
			t.Fatalf("local main branch is not marked current: %#v", branch)
		}
		if branch.Name == "remote-only" && branch.Remote == "origin" {
			foundRemote = true
			if branch.Ref != "origin/remote-only" {
				t.Fatalf("remote ref = %q", branch.Ref)
			}
		}
		if branch.Name == "HEAD" {
			t.Fatalf("remote HEAD was included: %#v", branch)
		}
	}
	if !foundLocal || !foundRemote {
		t.Fatalf("branches = %#v", branches)
	}

	commits, err := client.ListCommits(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || commits[0].Subject != "first commit" {
		t.Fatalf("commits = %#v", commits)
	}
	runGit(t, dir, "switch", "--detach", "--quiet", "HEAD")
	if commits, err := client.ListCommits(context.Background()); err != nil || len(commits) != 1 {
		t.Fatalf("detached ListCommits() = %#v, %v", commits, err)
	}

	var remoteBranch Branch
	for _, branch := range branches {
		if branch.Name == "remote-only" {
			remoteBranch = branch
		}
	}
	if _, err := client.Switch(context.Background(), remoteBranch); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(runGit(t, dir, "branch", "--show-current"))); got != "remote-only" {
		t.Fatalf("current branch = %q, want remote-only", got)
	}
	upstream := strings.TrimSpace(string(runGit(t, dir, "for-each-ref", "--format=%(upstream:short)", "refs/heads/remote-only")))
	if upstream != "origin/remote-only" {
		t.Fatalf("upstream = %q, want origin/remote-only", upstream)
	}
}

func TestSwitchesLocalJapaneseSlashBranchWhenPathCollides(t *testing.T) {
	dir := newRepository(t)
	writeFile(t, dir, "README.md", "first\n")
	runGit(t, dir, "add", "--", "README.md")
	runGit(t, dir, "commit", "-qm", "first commit")
	branchName := "feature/日本語"
	runGit(t, dir, "branch", branchName)
	if err := os.Mkdir(filepath.Join(dir, "feature"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, filepath.Join("feature", "日本語"), "path with the same name\n")

	client := NewInDir(dir, "")
	branches, err := client.ListBranches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var branch Branch
	for _, candidate := range branches {
		if candidate.Name == branchName && !candidate.IsRemote() {
			branch = candidate
			break
		}
	}
	if branch.Ref == "" {
		t.Fatalf("local branch %q was not listed: %#v", branchName, branches)
	}
	preview, err := client.BranchLog(context.Background(), branch.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(preview.Stdout), "first commit") {
		t.Fatalf("branch preview = %q", preview.Stdout)
	}
	if _, err := client.Switch(context.Background(), branch); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(runGit(t, dir, "branch", "--show-current"))); got != branchName {
		t.Fatalf("current branch = %q, want %q", got, branchName)
	}
}

func TestSwitchRemoteBranchUsesSelectedRemoteWhenNamesCollide(t *testing.T) {
	dir := newRepository(t)
	writeFile(t, dir, "README.md", "first\n")
	runGit(t, dir, "add", "--", "README.md")
	runGit(t, dir, "commit", "-qm", "first commit")
	origin := newBareRepository(t)
	upstream := newBareRepository(t)
	runGit(t, dir, "remote", "add", "origin", origin)
	runGit(t, dir, "remote", "add", "upstream", upstream)
	for _, remote := range []string{"origin", "upstream"} {
		runGit(t, dir, "push", "-q", remote, "HEAD:refs/heads/feature/shared")
		runGit(t, dir, "fetch", "-q", remote, "feature/shared")
	}

	client := NewInDir(dir, "")
	branches, err := client.ListBranches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var selected Branch
	var matching int
	for _, branch := range branches {
		if branch.Name != "feature/shared" || !branch.IsRemote() {
			continue
		}
		matching++
		if branch.Remote == "upstream" {
			selected = branch
		}
	}
	if matching != 2 || selected.Ref != "upstream/feature/shared" {
		t.Fatalf("same-name remote branches = %#v, selected = %#v", branches, selected)
	}
	if _, err := client.Switch(context.Background(), selected); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(runGit(t, dir, "branch", "--show-current"))); got != "feature/shared" {
		t.Fatalf("current branch = %q, want feature/shared", got)
	}
	upstreamRef := strings.TrimSpace(string(runGit(t, dir, "for-each-ref", "--format=%(upstream:short)", "refs/heads/feature/shared")))
	if upstreamRef != "upstream/feature/shared" {
		t.Fatalf("upstream = %q, want upstream/feature/shared", upstreamRef)
	}
}

func TestEmptyRepositoryHasNoBranchesOrCommits(t *testing.T) {
	dir := newRepository(t)
	client := NewInDir(dir, "")
	if _, err := client.ListBranches(context.Background()); !errors.Is(err, ErrNoBranches) {
		t.Fatalf("ListBranches() error = %v, want ErrNoBranches", err)
	}
	if _, err := client.ListCommits(context.Background()); !errors.Is(err, ErrNoCommits) {
		t.Fatalf("ListCommits() error = %v, want ErrNoCommits", err)
	}
}

func TestListCommitsUsesAllRefsWhenHeadIsUnborn(t *testing.T) {
	dir := newRepository(t)
	writeFile(t, dir, "README.md", "first\n")
	runGit(t, dir, "add", "--", "README.md")
	runGit(t, dir, "commit", "-qm", "initial")
	runGit(t, dir, "symbolic-ref", "HEAD", "refs/heads/gone")

	commits, err := NewInDir(dir, "").ListCommits(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || commits[0].Subject != "initial" {
		t.Fatalf("commits = %#v", commits)
	}
}

func newRepository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "config", "user.name", "git-fz test")
	runGit(t, dir, "config", "user.email", "git-fz@example.invalid")
	return dir
}

func newBareRepository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--bare", "-q")
	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var output bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		t.Fatalf("git %v: %v\nstdout=%q\nstderr=%q", args, err, output.String(), stderr.String())
	}
	return output.Bytes()
}
