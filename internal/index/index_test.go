package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sidcha/log-viewer/internal/source"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func openSrc(t *testing.T, path string) *source.Source {
	t.Helper()
	src, err := source.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { src.Close() })
	return src
}

// syncIndex builds a complete index without the follow loop.
func syncIndex(t *testing.T, src *source.Source, stride int64, maxCk int) *Index {
	t.Helper()
	idx := New(stride, maxCk)
	ix := NewIndexer(src, idx, Options{Block: 64})
	ix.Sync(context.Background())
	if err := idx.Err(); err != nil {
		t.Fatalf("index error: %v", err)
	}
	return idx
}

func TestIndexerCountsLines(t *testing.T) {
	var b strings.Builder
	const n = 500
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	src := openSrc(t, writeFile(t, "log", b.String()))

	idx := syncIndex(t, src, 8, 0)
	if got := idx.Lines(); got != n {
		t.Fatalf("Lines = %d, want %d", got, n)
	}
	if !idx.Caught() {
		t.Error("Caught() = false after a full sync")
	}
}

func TestCheckpointsAddressEveryLine(t *testing.T) {
	// Fixed-width lines make the expected offset trivially computable.
	const (
		n     = 300
		width = 8 // "line NNN" is 8 bytes, plus the newline
	)
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "line %03d\n", i)
	}
	src := openSrc(t, writeFile(t, "log", b.String()))

	const stride = 16
	idx := syncIndex(t, src, stride, 0)

	for i := int64(0); i < n; i++ {
		off, line := idx.Checkpoint(i)
		if line > i {
			t.Fatalf("Checkpoint(%d) line = %d, must not be ahead", i, line)
		}
		if i-line >= stride {
			t.Fatalf("Checkpoint(%d) line = %d, more than a stride behind", i, line)
		}
		if want := line * (width + 1); off != want {
			t.Fatalf("Checkpoint(%d) off = %d, want %d", i, off, want)
		}
	}
}

func TestStrideCollapseKeepsInvariant(t *testing.T) {
	// Every line is 10 bytes, so line k starts at 10k and the invariant is
	// checkable directly.
	idx := New(2, 4)
	const lines = 200
	starts := make([]int64, 0, lines)
	for i := 0; i < lines; i++ {
		starts = append(starts, int64(i)*10)
	}
	idx.Add(starts, lines*10, lines*10)

	_, _, _, stride, ck := idx.Stats()
	if stride <= 2 {
		t.Fatalf("stride = %d, expected the table to have collapsed", stride)
	}
	if ck > 4 {
		t.Fatalf("checkpoints = %d, expected the cap to hold", ck)
	}

	for i := int64(0); i < lines; i++ {
		off, line := idx.Checkpoint(i)
		if line > i || i-line >= stride {
			t.Fatalf("Checkpoint(%d) = (%d, %d) outside one stride of %d", i, off, line, stride)
		}
		if off != line*10 {
			t.Fatalf("Checkpoint(%d) off = %d, want %d", i, off, line*10)
		}
	}
}

func TestPartialTailIsVisibleThenCompleted(t *testing.T) {
	path := writeFile(t, "log", "a\nb\npartial")
	src := openSrc(t, path)

	idx := syncIndex(t, src, 4, 0)
	if got := idx.Lines(); got != 3 {
		t.Fatalf("Lines = %d, want 3 (the partial tail counts)", got)
	}

	// Completing the line and adding another must not double-count.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(" now done\nlast\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	ix := NewIndexer(src, idx, Options{Block: 64})
	ix.Sync(context.Background())
	if got := idx.Lines(); got != 4 {
		t.Fatalf("Lines after append = %d, want 4", got)
	}
}

func TestTruncationResetsIndex(t *testing.T) {
	path := writeFile(t, "log", "one\ntwo\nthree\n")
	src := openSrc(t, path)

	idx := syncIndex(t, src, 4, 0)
	gen := idx.Gen()
	if idx.Lines() != 3 {
		t.Fatalf("Lines = %d, want 3", idx.Lines())
	}

	if err := os.WriteFile(path, []byte("fresh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ix := NewIndexer(src, idx, Options{Block: 64})
	ix.Sync(context.Background())

	if idx.Lines() != 1 {
		t.Fatalf("Lines after truncation = %d, want 1", idx.Lines())
	}
	if idx.Gen() == gen {
		t.Error("generation did not change across a reset")
	}
}

func TestRunFollowsAppends(t *testing.T) {
	path := writeFile(t, "log", "first\n")
	src := openSrc(t, path)

	idx := New(4, 0)
	woke := make(chan struct{}, 64)
	ix := NewIndexer(src, idx, Options{
		Poll:  5 * time.Millisecond,
		Block: 64,
		Notify: func() {
			select {
			case woke <- struct{}{}:
			default:
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ix.Run(ctx)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("second\nthird\n"); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(3 * time.Second)
	for idx.Lines() < 3 {
		select {
		case <-deadline:
			t.Fatalf("follow did not pick up appends; Lines = %d", idx.Lines())
		case <-time.After(5 * time.Millisecond):
		}
	}
	select {
	case <-woke:
	default:
		t.Error("Notify was never called")
	}
}

// TestRunFollowsOnWakeChannel checks that the indexer picks up an append as
// soon as it is told, rather than on a timer.
func TestRunFollowsOnWakeChannel(t *testing.T) {
	path := writeFile(t, "log", "first\n")
	src := openSrc(t, path)

	idx := New(4, 0)
	wake := make(chan struct{}, 1)
	ix := NewIndexer(src, idx, Options{
		Wake:  wake,
		Poll:  time.Hour, // must not be what makes this work
		Block: 64,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ix.Run(ctx)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("second\nthird\n"); err != nil {
		t.Fatal(err)
	}
	wake <- struct{}{}

	deadline := time.After(5 * time.Second)
	for idx.Lines() < 3 {
		select {
		case <-deadline:
			t.Fatalf("indexer did not act on the wake-up; Lines = %d", idx.Lines())
		case <-time.After(2 * time.Millisecond):
		}
	}
}
