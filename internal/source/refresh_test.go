package source

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRefreshSeesARotationThatGrew: a replacement no smaller than the
// original is indistinguishable from growth by size alone, and reading on
// would pair old offsets with new bytes.
func TestRefreshSeesARotationThatGrew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	next := filepath.Join(dir, "app.log.new")
	if err := os.WriteFile(next, []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(next, path); err != nil {
		t.Fatal(err)
	}

	change, err := s.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if change != Reset {
		t.Fatalf("Refresh = %v, want reset", change)
	}
	buf := make([]byte, 11)
	if _, err := s.ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "replacement" {
		t.Errorf("read %q after rotation, want the new file", buf)
	}
}

func TestRefreshReportsGrowth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("b\n")
	f.Close()

	if change, err := s.Refresh(); err != nil || change != Grew {
		t.Fatalf("Refresh = %v, %v; want grew", change, err)
	}
}
