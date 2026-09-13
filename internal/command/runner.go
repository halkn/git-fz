package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/halkn/git-fz/internal/fzf"
	"github.com/halkn/git-fz/internal/git"
)

type Picker interface {
	Select(context.Context, []fzf.Item, fzf.Options) ([]string, error)
}

type Runner struct {
	Git        *git.Client
	Picker     Picker
	Executable string
}

func NewRunner(gitClient *git.Client, picker Picker, executable string) *Runner {
	if executable == "" {
		executable, _ = os.Executable()
	}
	return &Runner{Git: gitClient, Picker: picker, Executable: executable}
}

func (r *Runner) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		if len(args) == 0 {
			return errors.New(usage())
		}
		_, err := io.WriteString(stdout, usage())
		return err
	}

	switch args[0] {
	case "switch":
		return r.runSwitch(ctx, stdout, stderr)
	case "log":
		return r.runLog(ctx, stdout, stderr)
	case "stage":
		return r.runStage(ctx, stdout, stderr)
	case "__preview":
		return r.runPreview(ctx, args[1:], stdout)
	case "__stage-source":
		return r.runStageSource(ctx, stdout)
	case "__stage-action":
		return r.runStageAction(ctx, args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage())
	}
}

func usage() string {
	return `Usage: git fz <command>

Commands:
  switch  Select a local or remote branch and switch to it
  log     Select a commit and print its SHA
  stage   Select changed files to stage or unstage

Requirements: git and fzf must be available on PATH.
`
}

func (r *Runner) runSwitch(ctx context.Context, stdout, stderr io.Writer) error {
	branches, err := r.Git.ListBranches(ctx)
	if err != nil {
		return err
	}
	items := make([]fzf.Item, 0, len(branches))
	for _, branch := range branches {
		payload, err := marshal(branch)
		if err != nil {
			return err
		}
		items = append(items, fzf.Item{Payload: payload, Display: branchDisplay(branch)})
	}

	selected, err := r.Picker.Select(ctx, items, fzf.Options{
		Prompt:  "Switch> ",
		Header:  "Enter switch  ESC cancel",
		Preview: fzf.ShellQuote(r.Executable) + " __preview branch {1}",
	})
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		return fzf.ErrCancelled
	}
	var branch git.Branch
	if err := json.Unmarshal([]byte(selected[0]), &branch); err != nil {
		return fmt.Errorf("decode selected branch: %w", err)
	}
	result, err := r.Git.Switch(ctx, branch)
	if err != nil {
		return err
	}
	return writeResult(result, stdout, stderr)
}

func branchDisplay(branch git.Branch) string {
	marker := " "
	if branch.IsCurrent {
		marker = "*"
	}
	if branch.IsRemote() {
		return fmt.Sprintf("%s [remote %s] %s", marker, branch.Remote, branch.Name)
	}
	return fmt.Sprintf("%s [local] %s", marker, branch.Name)
}

func (r *Runner) runLog(ctx context.Context, stdout, stderr io.Writer) error {
	commits, err := r.Git.ListCommits(ctx)
	if err != nil {
		return err
	}
	items := make([]fzf.Item, 0, len(commits))
	for _, commit := range commits {
		payload, err := marshal(commit)
		if err != nil {
			return err
		}
		items = append(items, fzf.Item{Payload: payload, Display: commitDisplay(commit)})
	}
	selected, err := r.Picker.Select(ctx, items, fzf.Options{
		Prompt:  "Log> ",
		Header:  "Enter print SHA  ESC cancel",
		Preview: fzf.ShellQuote(r.Executable) + " __preview commit {1}",
	})
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		return fzf.ErrCancelled
	}
	var commit git.Commit
	if err := json.Unmarshal([]byte(selected[0]), &commit); err != nil {
		return fmt.Errorf("decode selected commit: %w", err)
	}
	_, err = fmt.Fprintln(stdout, commit.SHA)
	return err
}

func commitDisplay(commit git.Commit) string {
	date := strings.TrimSpace(commit.Date)
	if date == "" {
		return fmt.Sprintf("%.12s %s", commit.SHA, commit.Subject)
	}
	return fmt.Sprintf("%.12s %s (%s)", commit.SHA, commit.Subject, date)
}

func (r *Runner) runStage(ctx context.Context, stdout, stderr io.Writer) error {
	changes, err := r.Git.ListChanges(ctx)
	if err != nil {
		return err
	}
	items, err := changeItems(changes)
	if err != nil {
		return err
	}
	action := fzf.ShellQuote(r.Executable) + " __stage-action"
	source := fzf.ShellQuote(r.Executable) + " __stage-source"
	selected, err := r.Picker.Select(ctx, items, fzf.Options{
		Prompt:  "Stage> ",
		Header:  "TAB select  ctrl-s stage  ctrl-u unstage  ENTER finish  ESC cancel",
		Multi:   true,
		Preview: fzf.ShellQuote(r.Executable) + " __preview change {1}",
		Bind: []string{
			"ctrl-s:execute-silent(" + action + " stage {+1})+reload(" + source + ")",
			"ctrl-u:execute-silent(" + action + " unstage {+1})+reload(" + source + ")",
		},
	})
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		return fzf.ErrCancelled
	}
	result, err := r.Git.StatusShort(ctx)
	if err != nil {
		return err
	}
	if len(result.Stdout) == 0 {
		_, err = io.WriteString(stdout, "clean\n")
		return err
	}
	return writeResult(result, stdout, stderr)
}

func changeItems(changes []git.Change) ([]fzf.Item, error) {
	items := make([]fzf.Item, 0, len(changes))
	for _, change := range changes {
		payload, err := marshal(change)
		if err != nil {
			return nil, err
		}
		items = append(items, fzf.Item{
			Payload: payload,
			Display: fmt.Sprintf("%s %s", change.Status(), change.Path),
		})
	}
	return items, nil
}

func (r *Runner) runPreview(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) != 2 {
		return errors.New("usage: git-fz __preview <branch|commit|change> <payload>")
	}
	payload, err := fzf.DecodePayload(args[1])
	if err != nil {
		return err
	}

	var result git.Result
	switch args[0] {
	case "branch":
		var branch git.Branch
		if err := json.Unmarshal([]byte(payload), &branch); err != nil {
			return fmt.Errorf("decode preview branch: %w", err)
		}
		result, err = r.Git.BranchLog(ctx, branch.Ref)
	case "commit":
		var commit git.Commit
		if err := json.Unmarshal([]byte(payload), &commit); err != nil {
			return fmt.Errorf("decode preview commit: %w", err)
		}
		result, err = r.Git.ShowCommit(ctx, commit.SHA)
	case "change":
		var change git.Change
		if err := json.Unmarshal([]byte(payload), &change); err != nil {
			return fmt.Errorf("decode preview change: %w", err)
		}
		return r.writeChangePreview(ctx, change, stdout)
	default:
		return fmt.Errorf("unknown preview kind %q", args[0])
	}
	if err != nil {
		_, _ = io.WriteString(stdout, err.Error()+"\n")
		return nil
	}
	_, err = stdout.Write(result.Stdout)
	return err
}

func (r *Runner) writeChangePreview(ctx context.Context, change git.Change, stdout io.Writer) error {
	if change.CanUnstage() {
		result, err := r.Git.Diff(ctx, change.Path, true)
		if err != nil {
			_, _ = io.WriteString(stdout, err.Error()+"\n")
		} else if _, err := stdout.Write(result.Stdout); err != nil {
			return err
		}
	}
	if change.CanStage() {
		var result git.Result
		var err error
		if change.IndexStatus == '?' && change.WorktreeStatus == '?' {
			result, err = r.Git.UntrackedDiff(ctx, change.Path)
			if err != nil {
				var commandError *git.CommandError
				if !errors.As(err, &commandError) {
					_, _ = io.WriteString(stdout, err.Error()+"\n")
				}
			}
		} else {
			result, err = r.Git.Diff(ctx, change.Path, false)
		}
		if err != nil {
			if result.Stdout != nil {
				if _, writeErr := stdout.Write(result.Stdout); writeErr != nil {
					return writeErr
				}
			}
		} else if _, err := stdout.Write(result.Stdout); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) runStageSource(ctx context.Context, stdout io.Writer) error {
	changes, err := r.Git.ListChanges(ctx)
	if errors.Is(err, git.ErrNoChanges) {
		return nil
	}
	if err != nil {
		return err
	}
	items, err := changeItems(changes)
	if err != nil {
		return err
	}
	return fzf.WriteItems(stdout, items)
}

func (r *Runner) runStageAction(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) < 2 || (args[0] != "stage" && args[0] != "unstage") {
		return errors.New("usage: git-fz __stage-action <stage|unstage> <payload>...")
	}
	changes := make([]git.Change, 0, len(args)-1)
	seen := make(map[string]struct{}, len(args)-1)
	for _, token := range args[1:] {
		payload, err := fzf.DecodePayload(token)
		if err != nil {
			return err
		}
		var change git.Change
		if err := json.Unmarshal([]byte(payload), &change); err != nil {
			return fmt.Errorf("decode selected change: %w", err)
		}
		if _, ok := seen[change.Path]; ok {
			continue
		}
		seen[change.Path] = struct{}{}
		if args[0] == "stage" && change.CanStage() {
			changes = append(changes, change)
		}
		if args[0] == "unstage" && change.CanUnstage() {
			changes = append(changes, change)
		}
	}
	if len(changes) == 0 {
		return nil
	}
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		paths = append(paths, change.Path)
	}
	var result git.Result
	var err error
	if args[0] == "stage" {
		result, err = r.Git.Add(ctx, paths...)
	} else {
		result, err = r.Git.Unstage(ctx, paths...)
	}
	if err != nil {
		return err
	}
	return writeResult(result, stdout, stderr)
}

func marshal(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode fzf payload: %w", err)
	}
	return string(data), nil
}

func writeResult(result git.Result, stdout, stderr io.Writer) error {
	if len(result.Stdout) > 0 {
		if _, err := stdout.Write(result.Stdout); err != nil {
			return err
		}
	}
	if len(result.Stderr) > 0 {
		if _, err := stderr.Write(result.Stderr); err != nil {
			return err
		}
	}
	return nil
}
