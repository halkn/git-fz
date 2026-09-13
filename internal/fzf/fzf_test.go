package fzf

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteItemsKeepsPayloadSeparateFromDisplay(t *testing.T) {
	var got bytes.Buffer
	items := []Item{{Payload: `{"path":"a\tb"}`, Display: "M\t日本語\npath"}}
	if err := WriteItems(&got, items); err != nil {
		t.Fatal(err)
	}
	want := EncodePayload(items[0].Payload) + "\t" + items[0].Display + "\x00"
	if got.String() != want {
		t.Fatalf("WriteItems() = %q, want %q", got.String(), want)
	}
}

func TestDecodePayloadRoundTrip(t *testing.T) {
	const payload = `{"path":"space name/日本語\nfile"}`
	token := EncodePayload(payload)
	got, err := DecodePayload(token)
	if err != nil {
		t.Fatal(err)
	}
	if got != payload {
		t.Fatalf("DecodePayload() = %q, want %q", got, payload)
	}
}

func TestSelectReturnsDecodedPayload(t *testing.T) {
	token := EncodePayload("payload")
	dir := t.TempDir()
	fake := filepath.Join(dir, "fzf")
	script := "#!/bin/sh\nprintf '%s\\0' '" + token + "'\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := New(fake)
	runner.Stderr = io.Discard
	got, err := runner.Select(context.Background(), []Item{{Payload: "payload", Display: "display"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "payload" {
		t.Fatalf("Select() = %#v, want [payload]", got)
	}
}

func TestSelectNormalizesCancel(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fzf")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 130\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := New(fake)
	runner.Stderr = io.Discard
	_, err := runner.Select(context.Background(), []Item{{Payload: "payload", Display: "display"}}, Options{})
	if err != ErrCancelled {
		t.Fatalf("Select() error = %v, want ErrCancelled", err)
	}
}

func TestSelectReportsMissingExecutable(t *testing.T) {
	runner := New("/definitely/missing/fzf")
	_, err := runner.Select(context.Background(), []Item{{Payload: "payload", Display: "display"}}, Options{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Select() error = %v, want ErrUnavailable", err)
	}
}

func TestShellQuote(t *testing.T) {
	if got, want := ShellQuote("/tmp/it's git-fz"), "'/tmp/it'\\''s git-fz'"; got != want {
		t.Fatalf("ShellQuote() = %q, want %q", got, want)
	}
}
