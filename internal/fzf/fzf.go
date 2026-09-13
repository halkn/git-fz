package fzf

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

var (
	ErrCancelled   = errors.New("fzf selection cancelled")
	ErrUnavailable = errors.New("fzf executable not found")
)

type Item struct {
	Payload string
	Display string
}

type Options struct {
	Prompt  string
	Header  string
	Preview string
	Bind    []string
	Multi   bool
}

type Runner struct {
	Path   string
	Stderr io.Writer
}

func New(path string) *Runner { return &Runner{Path: path} }

func (r *Runner) executable() (string, error) {
	if r.Path != "" {
		path, err := exec.LookPath(r.Path)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return path, nil
	}
	path, err := exec.LookPath("fzf")
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return path, nil
}

func EncodePayload(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func DecodePayload(token string) (string, error) {
	payload, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", fmt.Errorf("decode fzf payload: %w", err)
	}
	return string(payload), nil
}

func WriteItems(w io.Writer, items []Item) error {
	for _, item := range items {
		if strings.IndexByte(item.Payload, 0) >= 0 || strings.IndexByte(item.Display, 0) >= 0 {
			return errors.New("fzf item contains NUL")
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\x00", EncodePayload(item.Payload), item.Display); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) Select(ctx context.Context, items []Item, options Options) ([]string, error) {
	if len(items) == 0 {
		return nil, ErrCancelled
	}
	executable, err := r.executable()
	if err != nil {
		return nil, err
	}

	var input bytes.Buffer
	if err := WriteItems(&input, items); err != nil {
		return nil, err
	}
	args := buildArgs(options)

	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdin = &input
	cmd.Stderr = r.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && (exitError.ExitCode() == 1 || exitError.ExitCode() == 130) {
			return nil, ErrCancelled
		}
		return nil, fmt.Errorf("run fzf: %w", err)
	}

	fields := bytes.Split(output.Bytes(), []byte{0})
	selected := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) == 0 {
			continue
		}
		payload, err := DecodePayload(string(field))
		if err != nil {
			return nil, err
		}
		selected = append(selected, payload)
	}
	if len(selected) == 0 {
		return nil, ErrCancelled
	}
	return selected, nil
}

func buildArgs(options Options) []string {
	args := []string{
		"--read0",
		"--print0",
		"--delimiter=\t",
		"--with-nth=2..",
		"--accept-nth=1",
		"--layout=reverse",
		"--height=80%",
	}
	if options.Prompt != "" {
		args = append(args, "--prompt="+options.Prompt)
	}
	if options.Header != "" {
		args = append(args, "--header="+options.Header)
	}
	if options.Preview != "" {
		args = append(args, "--preview="+options.Preview)
	}
	if options.Multi {
		args = append(args, "--multi")
	}
	for _, bind := range options.Bind {
		args = append(args, "--bind="+bind)
	}
	return args
}

func ShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
