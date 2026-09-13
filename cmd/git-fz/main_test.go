package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("GIT_FZ_FAKE_FZF") == "1" {
		os.Exit(runFakeFZF())
	}
	os.Exit(m.Run())
}

func TestGitExternalSubcommandPreservesRepositoryContext(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo space 日本語")
	if err := os.MkdirAll(filepath.Join(repo, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	runExternalGit(t, repo, nil, "init", "-q", "-b", "main")
	runExternalGit(t, repo, nil, "config", "user.name", "git-fz test")
	runExternalGit(t, repo, nil, "config", "user.email", "git-fz@example.invalid")
	writeExternalFile(t, repo, "README.md", "first\n")
	runExternalGit(t, repo, nil, "add", "--", "README.md")
	runExternalGit(t, repo, nil, "commit", "-qm", "first commit")

	path := "space name 日本.txt"
	writeExternalFile(t, repo, path, "untracked content\n")
	link := filepath.Join(parent, "repo-link")
	if err := os.Symlink(filepath.Join(repo, "subdir"), link); err != nil {
		t.Fatal(err)
	}

	gitFZ := buildGitFZBinary(t)
	fakeDir := t.TempDir()
	if err := os.Symlink(gitFZ, filepath.Join(fakeDir, "git-fz")); err != nil {
		t.Fatal(err)
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(testBinary, filepath.Join(fakeDir, "fzf")); err != nil {
		t.Fatal(err)
	}

	firstLogs := newFakeFZFLogs(t)
	stdout := string(runExternalGit(t, parent, fakeFZFEnv(fakeDir, path, "stage", firstLogs), "-C", repo, "-C", "subdir", "fz", "stage"))
	if !strings.HasPrefix(stdout, "A  ") {
		t.Fatalf("stage stdout = %q, want staged path", stdout)
	}
	assertFakeFZFRun(t, firstLogs, "ctrl-s", "A  "+path, "untracked content")

	secondLogs := newFakeFZFLogs(t)
	stdout = string(runExternalGit(t, parent, fakeFZFEnv(fakeDir, path, "unstage", secondLogs), "-C", link, "-C", "..", "fz", "stage"))
	if !strings.HasPrefix(stdout, "?? ") {
		t.Fatalf("unstage stdout = %q, want untracked path", stdout)
	}
	assertFakeFZFRun(t, secondLogs, "ctrl-u", "?? "+path, "untracked content")
	if got := string(readExternalFile(t, repo, path)); got != "untracked content\n" {
		t.Fatalf("worktree content after unstage = %q", got)
	}
	if status := string(runExternalGit(t, repo, nil, "status", "--porcelain=v1", "-z")); status != "?? "+path+"\x00" {
		t.Fatalf("final status = %q, want untracked file", status)
	}
}

type fakeFZFLogs struct {
	args    string
	preview string
	action  string
	reload  string
}

func newFakeFZFLogs(t *testing.T) fakeFZFLogs {
	t.Helper()
	dir := t.TempDir()
	return fakeFZFLogs{
		args:    filepath.Join(dir, "args"),
		preview: filepath.Join(dir, "preview"),
		action:  filepath.Join(dir, "action"),
		reload:  filepath.Join(dir, "reload"),
	}
}

func fakeFZFEnv(fakeDir, match, action string, logs fakeFZFLogs) map[string]string {
	return map[string]string{
		"PATH":                        fakeDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GIT_FZ_FAKE_FZF":             "1",
		"GIT_FZ_FAKE_FZF_MATCH":       match,
		"GIT_FZ_FAKE_FZF_ACTION":      action,
		"GIT_FZ_FAKE_FZF_ARGS_LOG":    logs.args,
		"GIT_FZ_FAKE_FZF_PREVIEW_LOG": logs.preview,
		"GIT_FZ_FAKE_FZF_ACTION_LOG":  logs.action,
		"GIT_FZ_FAKE_FZF_RELOAD_LOG":  logs.reload,
	}
}

func assertFakeFZFRun(t *testing.T, logs fakeFZFLogs, bind, reloadStatus, previewText string) {
	t.Helper()
	args := string(readExternalFile(t, "", logs.args))
	if !strings.Contains(args, "--preview=") || !strings.Contains(args, "--bind="+bind) {
		t.Fatalf("fzf args = %q, missing preview or %s binding", args, bind)
	}
	preview := string(readExternalFile(t, "", logs.preview))
	if !strings.Contains(preview, previewText) {
		t.Fatalf("preview = %q, want %q", preview, previewText)
	}
	action := string(readExternalFile(t, "", logs.action))
	if !strings.Contains(action, "change-header:") || strings.Contains(action, "Stage failed:") {
		t.Fatalf("action output = %q", action)
	}
	reload := string(readExternalFile(t, "", logs.reload))
	if !strings.Contains(reload, reloadStatus) {
		t.Fatalf("reload output = %q, want %q", reload, reloadStatus)
	}
}

func runFakeFZF() int {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 2
	}
	match := os.Getenv("GIT_FZ_FAKE_FZF_MATCH")
	var token string
	for _, row := range bytes.Split(input, []byte{0}) {
		fields := bytes.SplitN(row, []byte{'\t'}, 2)
		if len(fields) != 2 || (match != "" && !strings.Contains(string(fields[1]), match)) {
			continue
		}
		token = string(fields[0])
		break
	}
	if token == "" {
		return 1
	}

	args := os.Args[1:]
	writeFakeFZFLog(os.Getenv("GIT_FZ_FAKE_FZF_ARGS_LOG"), []byte(strings.Join(args, "\n")))
	if preview := fakeFZFOption(args, "--preview="); preview != "" {
		output, err := runFakeFZFCommand(strings.ReplaceAll(preview, "{1}", token))
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			return 2
		}
		writeFakeFZFLog(os.Getenv("GIT_FZ_FAKE_FZF_PREVIEW_LOG"), output)
	}
	if action := os.Getenv("GIT_FZ_FAKE_FZF_ACTION"); action != "" {
		key := map[string]string{"stage": "s", "unstage": "u"}[action]
		var bind string
		for _, arg := range args {
			if strings.HasPrefix(arg, "--bind=ctrl-"+key+":") {
				bind = strings.TrimPrefix(arg, "--bind=")
				break
			}
		}
		if bind == "" {
			_, _ = fmt.Fprintf(os.Stderr, "binding for action %q not found in args %#v\n", action, args)
			return 2
		}
		transform, reload, ok := splitFakeFZFBinding(bind, token)
		if !ok {
			_, _ = fmt.Fprintf(os.Stderr, "cannot parse fzf binding %q\n", bind)
			return 2
		}
		output, err := runFakeFZFCommand(transform)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			return 2
		}
		writeFakeFZFLog(os.Getenv("GIT_FZ_FAKE_FZF_ACTION_LOG"), output)
		output, err = runFakeFZFCommand(reload)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			return 2
		}
		writeFakeFZFLog(os.Getenv("GIT_FZ_FAKE_FZF_RELOAD_LOG"), output)
	}
	_, _ = os.Stdout.Write(append([]byte(token), 0))
	return 0
}

func fakeFZFOption(args []string, prefix string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}

func splitFakeFZFBinding(bind, token string) (string, string, bool) {
	const transformPrefix = "transform("
	const reloadSeparator = ")+reload-sync("
	start := strings.Index(bind, transformPrefix)
	if start < 0 {
		return "", "", false
	}
	start += len(transformPrefix)
	end := strings.Index(bind[start:], reloadSeparator)
	if end < 0 {
		return "", "", false
	}
	end += start
	reloadStart := end + len(reloadSeparator)
	if reloadStart >= len(bind) || bind[len(bind)-1] != ')' {
		return "", "", false
	}
	transform := strings.ReplaceAll(bind[start:end], "{+1}", token)
	reload := bind[reloadStart : len(bind)-1]
	return transform, reload, true
}

func runFakeFZFCommand(command string) ([]byte, error) {
	cmd := exec.Command("sh", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("run fake fzf command %q: %w: %s", command, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func writeFakeFZFLog(path string, content []byte) {
	if path == "" {
		return
	}
	_ = os.WriteFile(path, content, 0o600)
}

func buildGitFZBinary(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	binary := filepath.Join(t.TempDir(), "git-fz")
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/git-fz")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
	return binary
}

func runExternalGit(t *testing.T, dir string, extraEnv map[string]string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = filteredExternalEnv(extraEnv)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v\nstdout=%q\nstderr=%q", args, err, stdout.String(), stderr.String())
	}
	return stdout.Bytes()
}

func filteredExternalEnv(extra map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, variable := range os.Environ() {
		key, _, ok := strings.Cut(variable, "=")
		if ok && (key == "GIT_DIR" || key == "GIT_WORK_TREE" || key == "GIT_PREFIX" || key == "PATH") {
			continue
		}
		env = append(env, variable)
	}
	for key, value := range extra {
		env = append(env, key+"="+value)
	}
	return env
}

func writeExternalFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readExternalFile(t *testing.T, dir, name string) []byte {
	t.Helper()
	path := name
	if dir != "" {
		path = filepath.Join(dir, name)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
