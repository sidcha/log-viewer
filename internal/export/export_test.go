package export

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/index"
	"github.com/sidcha/log-viewer/internal/source"
	"github.com/sidcha/log-viewer/internal/view"
)

func build(t *testing.T, content string) (*source.Source, *index.Index, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "log")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := source.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { src.Close() })

	idx := index.New(8, 0)
	ix := index.NewIndexer(src, idx, index.Options{Block: 64})
	ix.Sync(context.Background())
	return src, idx, dir
}

func sample(n int) (content string, errLines []string) {
	var b strings.Builder
	for i := 0; i < n; i++ {
		var l string
		if i%3 == 0 {
			l = fmt.Sprintf("%04d ERROR failure number %d", i, i)
			errLines = append(errLines, l)
		} else {
			l = fmt.Sprintf("%04d INFO all is well", i)
		}
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String(), errLines
}

func TestSaveWholeView(t *testing.T) {
	content, _ := sample(300)
	src, idx, dir := build(t, content)

	v := view.NewBase(src, idx, 64, 1<<20)
	out := filepath.Join(dir, "out.log")

	p, err := Save(context.Background(), src, v, out, nil)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if p.Lines != 300 {
		t.Errorf("Lines = %d, want 300", p.Lines)
	}
	if !p.Done {
		t.Error("Done = false")
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Error("saved file does not match the source byte for byte")
	}
}

func TestSaveFilteredView(t *testing.T) {
	content, errLines := sample(400)
	src, idx, dir := build(t, content)

	m, err := filter.Compile("ERROR", filter.Literal, filter.Sensitive)
	if err != nil {
		t.Fatal(err)
	}
	rules := filter.NewSet().Add(filter.Rule{Action: filter.Only, M: m, Enabled: true})

	f := view.NewFiltered(src, idx, rules, view.FilteredOptions{Block: 64, MaxLine: 1 << 20, Poll: 5 * time.Millisecond})
	defer f.Close()

	// Save deliberately runs its own pass, so it must be correct even before
	// the view's own scan has finished.
	out := filepath.Join(dir, "errors.log")
	p, err := Save(context.Background(), src, f, out, nil)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	want := strings.Join(errLines, "\n") + "\n"
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("filtered save mismatch:\n got %d bytes\nwant %d bytes", len(got), len(want))
	}
	if p.Lines != int64(len(errLines)) {
		t.Errorf("Lines = %d, want %d", p.Lines, len(errLines))
	}
}

func TestSaveKeepsFinalPartialLine(t *testing.T) {
	content := "one\ntwo\nno trailing newline"
	src, idx, dir := build(t, content)

	v := view.NewBase(src, idx, 64, 1<<20)
	out := filepath.Join(dir, "out.log")
	if _, err := Save(context.Background(), src, v, out, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	if string(got) != content {
		t.Fatalf("got %q, want %q", got, content)
	}
}

func TestSaveRefusesToOverwriteSource(t *testing.T) {
	content, _ := sample(10)
	src, idx, _ := build(t, content)
	v := view.NewBase(src, idx, 64, 1<<20)

	if _, err := Save(context.Background(), src, v, src.Name(), nil); err == nil {
		t.Fatal("Save should refuse to write over the file being viewed")
	}
	// The source must be untouched.
	got, _ := os.ReadFile(src.Name())
	if string(got) != content {
		t.Fatal("the source file was modified")
	}
}

func TestSaveCancelledLeavesNoFile(t *testing.T) {
	content, _ := sample(20000)
	src, idx, dir := build(t, content)
	v := view.NewBase(src, idx, 64, 1<<20)

	ctx, cancel := context.WithCancel(context.Background())
	out := filepath.Join(dir, "partial.log")

	_, err := Save(ctx, src, v, out, func(p Progress) { cancel() })
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Error("a cancelled save must not leave a destination file behind")
	}
	// Nor a temporary one.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".lv-") {
			t.Errorf("left a temporary file behind: %s", e.Name())
		}
	}
}
