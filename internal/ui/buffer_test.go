package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/highlight"
	"github.com/sidcha/log-viewer/internal/index"
	"github.com/sidcha/log-viewer/internal/source"
)

// addTestBuffer opens content as another file in a and indexes it fully.
// It returns the buffer's wake function.
func addTestBuffer(t *testing.T, a *App, name, content string) func() {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := source.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { src.Close() })
	idx := index.New(8, 0)
	ixr := index.NewIndexer(src, idx, index.Options{Block: 256})
	ixr.Sync(t.Context())
	return a.AddBuffer(src, idx, highlight.Fixed{})
}

func drainWake(a *App) bool {
	select {
	case <-a.wake:
		return true
	default:
		return false
	}
}

func TestEachBufferKeepsItsOwnPositionAndRules(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	keys(a, "40j")
	a.addRule(filter.Only, "ERROR", color.RGB{})

	addTestBuffer(t, a, "second.log", numbered(100))
	if a.bufferIndex() != 1 {
		t.Fatalf("AddBuffer left buffer %d on screen, want 1", a.bufferIndex())
	}
	if a.cursorLine() != 0 || a.rules.Len() != 0 {
		t.Fatalf("new buffer inherited state: cursor %d, %d rule(s)", a.cursorLine(), a.rules.Len())
	}
	keys(a, "7j")

	a.showBuffer(0)
	if a.rules.Len() != 1 || !a.filtered() {
		t.Errorf("first buffer lost its rule")
	}
	first := a.cursorLine()

	a.showBuffer(1)
	if a.cursorLine() != 7 {
		t.Errorf("second buffer's cursor is %d, want 7", a.cursorLine())
	}
	a.showBuffer(0)
	if a.cursorLine() != first {
		t.Errorf("first buffer's cursor moved from %d to %d", first, a.cursorLine())
	}
}

// TestHiddenBufferDoesNotAskForFrames: ten files indexing in the background
// must not repaint a screen that shows only one of them.
func TestHiddenBufferDoesNotAskForFrames(t *testing.T) {
	a, _ := newTestApp(t, numbered(50))
	wakeSecond := addTestBuffer(t, a, "second.log", numbered(50))
	a.showBuffer(0)
	drainWake(a)

	wakeSecond()
	if drainWake(a) {
		t.Error("a hidden buffer's wake asked for a frame")
	}

	a.showBuffer(1)
	wakeSecond()
	if !drainWake(a) {
		t.Error("the shown buffer's wake did not ask for a frame")
	}
}

// TestSearchResultDoesNotMoveAnotherBuffer: a search finishing after a
// switch is a result for a buffer no longer on screen, so it is dropped
// rather than moving the cursor of whatever buffer replaced it.
func TestSearchResultDoesNotMoveAnotherBuffer(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	addTestBuffer(t, a, "second.log", numbered(200))
	keys(a, "2j") // buffer 1 on a non-match line
	a.showBuffer(0)

	// showBuffer is the silent switch, so it can happen mid-search here;
	// the key-driven switch refuses while busy.
	a.doSearch("ERROR", false)
	a.showBuffer(1)
	waitSearch(t, a)

	if a.cursorLine() != 2 {
		t.Errorf("a result for a buffer no longer on screen moved the cursor to %d, want 2", a.cursorLine())
	}
}
