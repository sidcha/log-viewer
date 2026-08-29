package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitEvent waits for a signal, failing if none arrives in time.
func waitEvent(t *testing.T, w *Watcher, within time.Duration, what string) {
	t.Helper()
	select {
	case <-w.Events():
	case <-time.After(within):
		t.Fatalf("no event for %s within %s", what, within)
	}
}

func drain(w *Watcher) {
	for {
		select {
		case <-w.Events():
		default:
			return
		}
	}
}

func TestNotifiesOnAppendWithoutWaitingForThePoll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A backstop far longer than the assertion window, so an event arriving
	// quickly can only have come from the kernel.
	w := New(path, Options{Backstop: time.Hour, Fallback: time.Hour})
	defer w.Close()

	if !w.Native() {
		t.Skip("kernel notifications unavailable here; only the poll would fire")
	}
	drain(w)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("two\n"); err != nil {
		t.Fatal(err)
	}

	waitEvent(t, w, 2*time.Second, "an append")
}

func TestNotifiesOnRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	w := New(path, Options{Backstop: time.Hour, Fallback: time.Hour})
	defer w.Close()
	if !w.Native() {
		t.Skip("kernel notifications unavailable here")
	}
	drain(w)

	// Rotate: the old inode moves aside and a new file takes the name. The file
	// watch is bound to the old inode, so this only shows up in the directory.
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, w, 2*time.Second, "the rename")

	drain(w)
	if err := os.WriteFile(path, []byte("fresh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, w, 2*time.Second, "the replacement file")
}

// TestRebindsToTheReplacementInode checks that after a rotation the watcher is
// once more reporting writes to the new file, not the one that moved away.
func TestRebindsToTheReplacementInode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A short backstop, because rebinding happens on the poll tick.
	w := New(path, Options{Backstop: 50 * time.Millisecond, Fallback: 50 * time.Millisecond})
	defer w.Close()
	if !w.Native() {
		t.Skip("kernel notifications unavailable here")
	}

	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fresh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Give the rebind a couple of ticks.
	time.Sleep(300 * time.Millisecond)
	drain(w)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("after rotation\n"); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, w, 2*time.Second, "a write to the rotated-in file")
}

func TestBackstopFiresWithoutAnyChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Liveness must not depend on the kernel telling us anything.
	w := New(path, Options{Backstop: 30 * time.Millisecond, Fallback: 30 * time.Millisecond})
	defer w.Close()
	drain(w)

	waitEvent(t, w, 2*time.Second, "the backstop poll")
}

func TestPollingWatcher(t *testing.T) {
	w := NewPolling(20 * time.Millisecond)
	defer w.Close()

	if w.Native() {
		t.Error("a polling watcher should not claim native notifications")
	}
	waitEvent(t, w, 2*time.Second, "the poll")
}

func TestSignalsCoalesce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	w := New(path, Options{Backstop: time.Hour, Fallback: time.Hour})
	defer w.Close()
	if !w.Native() {
		t.Skip("kernel notifications unavailable here")
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := 0; i < 500; i++ {
		if _, err := f.WriteString("line\n"); err != nil {
			t.Fatal(err)
		}
	}

	waitEvent(t, w, 2*time.Second, "the writes")
	// A burst must not queue up hundreds of wake-ups behind it.
	time.Sleep(100 * time.Millisecond)
	n := 0
	for {
		select {
		case <-w.Events():
			n++
			if n > 1 {
				t.Fatalf("500 writes produced more than one pending signal")
			}
			continue
		default:
		}
		break
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	w := NewPolling(10 * time.Millisecond)
	w.Close()
	w.Close()
}

func TestMissingFileStillPolls(t *testing.T) {
	// Opening a viewer on a path that does not exist yet must not wedge.
	dir := t.TempDir()
	w := New(filepath.Join(dir, "not-created-yet.log"),
		Options{Backstop: 30 * time.Millisecond, Fallback: 30 * time.Millisecond})
	defer w.Close()
	waitEvent(t, w, 2*time.Second, "the poll on a missing file")
}
