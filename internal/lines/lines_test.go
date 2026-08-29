package lines

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sidcha/log-viewer/internal/source"
)

func write(t *testing.T, content string) *source.Source {
	t.Helper()
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := source.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { src.Close() })
	return src
}

func collect(sc *Scanner) (texts []string, offs []int64, terms []bool) {
	for sc.Scan() {
		texts = append(texts, string(sc.Bytes()))
		offs = append(offs, sc.Offset())
		terms = append(terms, sc.Terminated())
	}
	return
}

func TestScannerBasics(t *testing.T) {
	content := "alpha\nbeta\n\ngamma\n"
	src := write(t, content)

	// A tiny block forces the refill path to be exercised.
	sc := NewScanner(src, 0, src.Size(), 4)
	texts, offs, terms := collect(sc)

	if err := sc.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	want := []string{"alpha", "beta", "", "gamma"}
	if strings.Join(texts, "|") != strings.Join(want, "|") {
		t.Fatalf("texts = %q, want %q", texts, want)
	}
	wantOffs := []int64{0, 6, 11, 12}
	for i := range wantOffs {
		if offs[i] != wantOffs[i] {
			t.Errorf("offset[%d] = %d, want %d", i, offs[i], wantOffs[i])
		}
	}
	for i, term := range terms {
		if !term {
			t.Errorf("line %d should be newline-terminated", i)
		}
	}
}

func TestScannerUnterminatedTail(t *testing.T) {
	src := write(t, "one\ntwo")
	sc := NewScanner(src, 0, src.Size(), 8)
	texts, _, terms := collect(sc)

	if len(texts) != 2 || texts[1] != "two" {
		t.Fatalf("texts = %q", texts)
	}
	if !terms[0] || terms[1] {
		t.Fatalf("terminated = %v, want [true false]", terms)
	}
}

func TestScannerGrowingLimit(t *testing.T) {
	src := write(t, "a\nb\nc\n")
	sc := NewScanner(src, 0, 2, 8) // only "a\n" is visible

	texts, _, _ := collect(sc)
	if len(texts) != 1 || texts[0] != "a" {
		t.Fatalf("texts = %q, want [a]", texts)
	}

	// Raising the limit must let the same scanner carry on, which is what
	// follow mode depends on.
	sc.SetLimit(6)
	more, _, _ := collect(sc)
	if len(more) != 2 || more[0] != "b" || more[1] != "c" {
		t.Fatalf("after SetLimit texts = %q, want [b c]", more)
	}
}

func TestScannerLongLineTruncates(t *testing.T) {
	long := strings.Repeat("x", 100)
	src := write(t, long+"\nshort\n")

	sc := NewScanner(src, 0, src.Size(), 16)
	sc.SetMaxLine(10)

	if !sc.Scan() {
		t.Fatal("expected a line")
	}
	if got := string(sc.Bytes()); got != strings.Repeat("x", 10) {
		t.Fatalf("truncated text = %q", got)
	}
	if !sc.Truncated() {
		t.Error("Truncated() = false, want true")
	}
	if sc.Offset() != 0 {
		t.Errorf("Offset = %d, want 0", sc.Offset())
	}
	if sc.Next() != 101 {
		t.Errorf("Next = %d, want 101", sc.Next())
	}

	// The rest of the file must still line up after the discarded remainder.
	if !sc.Scan() || string(sc.Bytes()) != "short" {
		t.Fatalf("next line = %q", sc.Bytes())
	}
}

func TestSeekToWithinBuffer(t *testing.T) {
	src := write(t, "aaa\nbbb\nccc\nddd\n")
	sc := NewScanner(src, 0, src.Size(), 64)

	if !sc.Scan() || string(sc.Bytes()) != "aaa" {
		t.Fatal("first scan")
	}
	// Backwards inside the buffer, then forwards again.
	sc.SeekTo(0)
	if !sc.Scan() || string(sc.Bytes()) != "aaa" {
		t.Fatal("re-read after backward seek")
	}
	sc.SeekTo(8)
	if !sc.Scan() || string(sc.Bytes()) != "ccc" {
		t.Fatalf("after forward seek got %q", sc.Bytes())
	}
}

func TestSkipLines(t *testing.T) {
	src := write(t, "0\n1\n2\n3\n4\n")
	sc := NewScanner(src, 0, src.Size(), 4)

	pos, done := sc.SkipLines(0, 3)
	if done != 3 || pos != 6 {
		t.Fatalf("SkipLines = (%d, %d), want (6, 3)", pos, done)
	}
	if !sc.Scan() || string(sc.Bytes()) != "3" {
		t.Fatalf("after skip got %q", sc.Bytes())
	}

	// Asking for more lines than exist reports how far it actually got.
	_, done = sc.SkipLines(0, 99)
	if done != 5 {
		t.Fatalf("done = %d, want 5", done)
	}
}

func TestReaderLineAt(t *testing.T) {
	src := write(t, "one\ntwo\nthree\n")
	rd := NewReader(src, src.Size(), 8)

	text, next, trunc, err := rd.LineAt(4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(text) != "two" || next != 8 || trunc {
		t.Fatalf("LineAt = (%q, %d, %v)", text, next, trunc)
	}

	// dst is appended to, so a caller can recycle one buffer.
	buf := make([]byte, 0, 32)
	text, _, _, _ = rd.LineAt(0, buf[:0])
	if string(text) != "one" {
		t.Fatalf("LineAt(0) = %q", text)
	}
}
