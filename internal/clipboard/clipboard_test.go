package clipboard

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSequenceIsValidOSC52(t *testing.T) {
	const text = "hello\nworld\n"
	got := Sequence(text)

	if !strings.HasPrefix(got, "\x1b]52;c;") {
		t.Fatalf("sequence does not start with the OSC 52 introducer: %q", got)
	}
	if !strings.HasSuffix(got, "\a") {
		t.Fatalf("sequence is not terminated with BEL: %q", got)
	}

	payload := strings.TrimSuffix(strings.TrimPrefix(got, "\x1b]52;c;"), "\a")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
	if string(decoded) != text {
		t.Errorf("decoded %q, want %q", decoded, text)
	}
}

func TestCopyViaCommand(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "clip.txt")
	script := filepath.Join(dir, "sink.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat > "+out+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	const text = "some selected text"
	if err := Copy(text, script); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != text {
		t.Errorf("command received %q, want %q", b, text)
	}
}

func TestCopyReportsCommandFailure(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fail.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho no thanks >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := Copy("x", script)
	if err == nil {
		t.Fatal("a failing clipboard command should be reported")
	}
	// The program's own complaint is the useful part of the message.
	if !strings.Contains(err.Error(), "no thanks") {
		t.Errorf("error = %v, want it to carry the command's output", err)
	}

	if err := Copy("x", "definitely-not-a-program"); err == nil {
		t.Error("a missing clipboard command should be reported")
	}
	if err := Copy("x", "   "); err == nil {
		t.Error("an all-whitespace clipboard command should be reported")
	}
}

// TestLargeCopyRefusesTheEscape guards against a silent no-op: terminals drop
// oversized OSC 52 payloads without saying anything.
func TestLargeCopyRefusesTheEscape(t *testing.T) {
	big := strings.Repeat("x", maxOSC52+1)
	err := Copy(big, "")
	if err == nil {
		t.Fatal("an oversized copy through the terminal escape should be refused")
	}
	if !strings.Contains(err.Error(), "clipboard-command") {
		t.Errorf("error = %v, want it to point at the way out", err)
	}

	// An external command has no such limit.
	dir := t.TempDir()
	out := filepath.Join(dir, "clip.txt")
	script := filepath.Join(dir, "sink.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat > "+out+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Copy(big, script); err != nil {
		t.Fatalf("Copy via command: %v", err)
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != int64(len(big)) {
		t.Errorf("command received %d bytes, want %d", fi.Size(), len(big))
	}
}
