package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseChangesHandlesWhitespaceAndRename(t *testing.T) {
	data := strings.Join([]string{
		" M path with spaces.txt",
		"?? 日本語\tfile.txt",
		"R  new name.txt",
		"old name.txt",
		"D  deleted.txt",
		"",
	}, "\x00")
	changes, err := parseChanges([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 4 {
		t.Fatalf("got %d changes, want 4: %#v", len(changes), changes)
	}
	if changes[0].Path != "path with spaces.txt" || changes[0].Status() != " M" || !changes[0].CanStage() {
		t.Fatalf("modified change = %#v", changes[0])
	}
	if changes[1].Path != "日本語\tfile.txt" || changes[1].Status() != "??" || !changes[1].CanStage() || changes[1].CanUnstage() {
		t.Fatalf("untracked change = %#v", changes[1])
	}
	if changes[2].Path != "new name.txt" || changes[2].OriginalPath != "old name.txt" || !changes[2].IsRename() || changes[2].Status() != "R " || !changes[2].CanUnstage() {
		t.Fatalf("rename change = %#v", changes[2])
	}
}

func TestParseChangesKeepsCopySourceSeparate(t *testing.T) {
	changes, err := parseChanges([]byte("C  copied.txt\x00original.txt\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1: %#v", len(changes), changes)
	}
	change := changes[0]
	if !change.IsCopy() || change.IsRename() || change.Path != "copied.txt" || change.OriginalPath != "original.txt" {
		t.Fatalf("copy change = %#v", change)
	}
	if got := strings.Join(change.UnstagePaths(), "\x00"); got != "copied.txt" {
		t.Fatalf("copy unstage paths = %q, want copied.txt", got)
	}
}

func TestLiteralPathspecDoesNotExpandOrMixFiles(t *testing.T) {
	dir := newRepository(t)
	writeFile(t, dir, "*.txt", "wildcard\n")
	writeFile(t, dir, "unselected.txt", "other\n")
	client := NewInDir(dir, "")

	if _, err := client.Add(context.Background(), "*.txt"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(runGit(t, dir, "diff", "--cached", "--name-only"))); got != "*.txt" {
		t.Fatalf("staged paths = %q, want *.txt only", got)
	}
	if _, err := client.Unstage(context.Background(), "*.txt"); err != nil {
		t.Fatal(err)
	}

	runGit(t, dir, "add", "--all")
	runGit(t, dir, "commit", "-qm", "initial")
	writeFile(t, dir, "*.txt", "wildcard changed\n")
	writeFile(t, dir, "unselected.txt", "other changed\n")
	runGit(t, dir, "add", "--all")
	if _, err := client.Unstage(context.Background(), "*.txt"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(runGit(t, dir, "diff", "--cached", "--name-only"))); got != "unselected.txt" {
		t.Fatalf("cached paths after unstage = %q, want unselected.txt only", got)
	}
	result, err := client.Diff(context.Background(), "*.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result.Stdout), "unselected.txt") {
		t.Fatalf("literal diff included unselected.txt: %q", result.Stdout)
	}
}

func TestLiteralPathspecSupportsGitSpecialNames(t *testing.T) {
	dir := newRepository(t)
	paths := []string{"*", "?", "[]", ":literal", "line\nbreak"}
	for _, path := range paths {
		writeFile(t, dir, path, "content\n")
	}
	client := NewInDir(dir, "")
	if _, err := client.Add(context.Background(), paths...); err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(runGit(t, dir, "diff", "--cached", "--name-only", "-z")), "\x00"), "\x00")
	if len(got) != len(paths) {
		t.Fatalf("staged paths = %#v, want %#v", got, paths)
	}
	for _, path := range paths {
		if !contains(got, path) {
			t.Fatalf("staged paths = %#v, missing %q", got, path)
		}
	}
}

func TestRenameKeepsBothPathsAndUnstagesAsAUnit(t *testing.T) {
	dir := newRepository(t)
	oldPath := "old 日本.txt"
	newPath := "new 日本.txt"
	writeFile(t, dir, oldPath, "original\n")
	runGit(t, dir, "add", "--", oldPath)
	runGit(t, dir, "commit", "-qm", "initial")
	runGit(t, dir, "mv", oldPath, newPath)

	client := NewInDir(dir, "")
	changes, err := client.ListChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !changes[0].IsRename() || changes[0].Path != newPath || changes[0].OriginalPath != oldPath {
		t.Fatalf("rename changes = %#v", changes)
	}
	if _, err := client.Unstage(context.Background(), changes[0].UnstagePaths()...); err != nil {
		t.Fatal(err)
	}
	if cached := strings.TrimSpace(string(runGit(t, dir, "diff", "--cached", "--name-status"))); cached != "" {
		t.Fatalf("cached rename after unstage = %q", cached)
	}
	if _, err := os.Stat(filepath.Join(dir, newPath)); err != nil {
		t.Fatalf("destination missing after unstage: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, oldPath)); !os.IsNotExist(err) {
		t.Fatalf("source exists after unstage: %v", err)
	}
	changes, err = client.ListChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(changes, oldPath, " D") || !hasChange(changes, newPath, "??") {
		t.Fatalf("status after unstage = %#v", changes)
	}
}

func TestRepositoryOperationsUseRootFromChildDirectory(t *testing.T) {
	dir := newRepository(t)
	subdir := filepath.Join(dir, "sub")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := "sub/file.txt"
	writeFile(t, dir, path, "content\n")
	client := NewInDir(subdir, "")

	changes, err := client.ListChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != path {
		t.Fatalf("child directory changes = %#v", changes)
	}
	if _, err := client.Add(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if status := string(runGit(t, dir, "status", "--porcelain=v1", "--untracked-files=all")); !strings.HasPrefix(status, "A  sub/file.txt") {
		t.Fatalf("status after child add = %q", status)
	}
	if _, err := client.Unstage(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if status := string(runGit(t, dir, "status", "--porcelain=v1", "--untracked-files=all")); !strings.HasPrefix(status, "?? sub/file.txt") {
		t.Fatalf("status after child unstage = %q", status)
	}
}

func TestRepositoryRootPreservesTrailingWhitespace(t *testing.T) {
	for _, suffix := range []string{" ", "\n"} {
		t.Run(fmt.Sprintf("suffix-%q", suffix), func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "repo"+suffix)
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			runGit(t, dir, "init", "-q", "-b", "main")
			writeFile(t, dir, "file.txt", "content\n")

			client := NewInDir(dir, "")
			changes, err := client.ListChanges(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(changes) != 1 || changes[0].Path != "file.txt" {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestRepositoryOperationsResolveRelativeGitEnvironment(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "repo")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "file.txt", "content\n")

	t.Setenv("GIT_DIR", filepath.Join("repo", ".git"))
	t.Setenv("GIT_WORK_TREE", "repo")
	client := NewInDir(parent, "")
	changes, err := client.ListChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != "file.txt" {
		t.Fatalf("changes = %#v", changes)
	}
	if _, err := client.Add(context.Background(), "file.txt"); err != nil {
		t.Fatal(err)
	}
	changes, err = client.ListChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Status() != "A " {
		t.Fatalf("staged changes = %#v", changes)
	}
}

func TestUnstageDoesNotRemoveIndexForBrokenHead(t *testing.T) {
	dir := newRepository(t)
	writeFile(t, dir, "README.md", "first\n")
	runGit(t, dir, "add", "--", "README.md")
	runGit(t, dir, "commit", "-qm", "initial")
	writeFile(t, dir, "README.md", "changed\n")
	runGit(t, dir, "add", "--", "README.md")
	runGit(t, dir, "symbolic-ref", "HEAD", "refs/heads/gone")

	client := NewInDir(dir, "")
	if _, err := client.Unstage(context.Background(), "README.md"); err == nil {
		t.Fatal("Unstage() succeeded with a broken HEAD")
	}
	cached := string(runGit(t, dir, "diff", "--cached", "--name-only"))
	if !strings.Contains(cached, "README.md") {
		t.Fatalf("cached paths after broken HEAD unstage = %q", cached)
	}
}

func TestChangedPathCanBeStagedAndUnstaged(t *testing.T) {
	dir := newRepository(t)
	writeFile(t, dir, "README.md", "first\nchanged\n")
	writeFile(t, dir, "space name 日本.txt", "untracked\n")
	client := NewInDir(dir, "")

	changes, err := client.ListChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(changes, "space name 日本.txt", "??") {
		t.Fatalf("changes = %#v", changes)
	}
	if _, err := client.Add(context.Background(), "space name 日本.txt"); err != nil {
		t.Fatal(err)
	}
	changes, err = client.ListChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(changes, "space name 日本.txt", "A ") {
		t.Fatalf("staged changes = %#v", changes)
	}
	if _, err := client.Unstage(context.Background(), "space name 日本.txt"); err != nil {
		t.Fatal(err)
	}
	changes, err = client.ListChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasChange(changes, "space name 日本.txt", "??") {
		t.Fatalf("unstaged changes = %#v", changes)
	}

	if _, err := client.Add(context.Background(), "README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Unstage(context.Background(), "README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.StatusShort(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNoChanges(t *testing.T) {
	dir := newRepository(t)
	writeFile(t, dir, "README.md", "first\n")
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "first commit")
	client := NewInDir(dir, "")
	if _, err := client.ListChanges(context.Background()); !errors.Is(err, ErrNoChanges) {
		t.Fatalf("ListChanges() error = %v, want ErrNoChanges", err)
	}
}

func hasChange(changes []Change, path, status string) bool {
	for _, change := range changes {
		if change.Path == path && change.Status() == status {
			return true
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
