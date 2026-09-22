package highlight_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/highlight"
)

// TestCorpusChoosesTheProfileItsDirectoryNames runs detection over every
// sample in testdata/formats/<profile>/ with the shipped profiles.
func TestCorpusChoosesTheProfileItsDirectoryNames(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("testdata/formats/*/*")
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus: %v", err)
	}
	for _, f := range files {
		want := filepath.Base(filepath.Dir(f))
		head, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		det := highlight.NewDetector(cfg.DetectOptions(f))
		var off int64
		for n, line := range bytes.SplitAfter(head, []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			det.ObserveLine(int64(n), off, bytes.TrimSuffix(line, []byte("\n")))
			off += int64(len(line))
		}
		det.ObserveCaughtUp(false)
		got := det.Segments().ProfileAt(0)
		det.Close()
		if got == nil || got.Name != want {
			name := "<nil>"
			if got != nil {
				name = got.Name
			}
			t.Errorf("%s: chose %s, want %s", f, name, want)
		}
	}
}
