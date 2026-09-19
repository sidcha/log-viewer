package atomicfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReplacesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "dir", "f")
	for _, body := range []string{"first\n", "second\n"} {
		if err := Write(path, 0o644, func(w io.Writer) error {
			_, err := io.WriteString(w, body)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(path); string(b) != body {
			t.Fatalf("file = %q, want %q", b, body)
		}
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
}

func TestAFailedWriteLeavesTheOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	os.WriteFile(path, []byte("keep\n"), 0o644)

	boom := errors.New("boom")
	err := Write(path, 0o644, func(w io.Writer) error {
		io.WriteString(w, "half")
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "keep\n" {
		t.Errorf("file = %q, want the old contents", b)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Errorf("left %d files behind", len(ents)-1)
	}
}
