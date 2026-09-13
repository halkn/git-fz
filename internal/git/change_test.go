package git

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseChangesHandlesWhitespaceAndRename(t *testing.T) {
	data := strings.Join([]string{
		" M path with spaces.txt",
		"?? 日本語\tfile.txt",
		"R  old name.txt",
		"new name.txt",
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
	if changes[2].Path != "new name.txt" || changes[2].Status() != "R " || !changes[2].CanUnstage() {
		t.Fatalf("rename change = %#v", changes[2])
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
