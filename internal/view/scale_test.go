package view

import (
	"bufio"
	"bytes"
	"context"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/index"
	"github.com/sidcha/log-viewer/internal/source"
)

// rssBudgetMiB is the ceiling this test holds lv's core to, whatever the file
// size. The claim being tested is that resident memory is a function of the
// terminal and the checkpoint caps, not of the log.
const rssBudgetMiB = 512

// peakRSS reads the high-water mark the kernel has recorded for this process.
func peakRSS(t *testing.T) int64 {
	t.Helper()
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Skip("no /proc/self/status; cannot measure RSS on this platform")
	}
	for _, line := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(line, "VmHWM:"); ok {
			f := strings.Fields(rest)
			if len(f) >= 1 {
				kb, err := strconv.ParseInt(f[0], 10, 64)
				if err == nil {
					return kb / 1024
				}
			}
		}
	}
	t.Skip("VmHWM not reported")
	return 0
}

// countReference counts lines and pattern matches with a plain buffered scan,
// independent of the index and checkpoint machinery under test.
func countReference(t *testing.T, path, pattern string) (lines, matches int64) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 1<<20)
	pat := []byte(pattern)
	buf := make([]byte, 1<<20)
	var partial bool

	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			lines += int64(bytes.Count(chunk, []byte{'\n'}))
			partial = chunk[n-1] != '\n'
		}
		if err != nil {
			break
		}
	}
	if partial {
		lines++
	}

	// Second pass for matches, line by line, so the answer is exact.
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if bytes.Contains(sc.Bytes(), pat) {
			matches++
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return lines, matches
}

// TestScale exercises a file larger than RAM. It is opt-in:
//
//	make scale LV_SCALE_FILE=/path/to/huge.log
func TestScale(t *testing.T) {
	path := os.Getenv("LV_SCALE_FILE")
	if path == "" {
		t.Skip("set LV_SCALE_FILE to a large log file to run this")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	sizeMiB := fi.Size() >> 20
	t.Logf("file: %s (%d MiB)", path, sizeMiB)

	const pattern = "ERROR"
	refStart := time.Now()
	wantLines, wantMatches := countReference(t, path, pattern)
	t.Logf("reference: %d lines, %d matches containing %q (%s)",
		wantLines, wantMatches, pattern, time.Since(refStart).Round(time.Millisecond))

	src, err := source.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	idx := index.New(0, 0)
	ix := index.NewIndexer(src, idx, index.Options{Advise: "on"})

	start := time.Now()
	ix.Sync(context.Background())
	indexTime := time.Since(start)
	if err := idx.Err(); err != nil {
		t.Fatalf("index error: %v", err)
	}

	gotLines, _, _, stride, ckpts := idx.Stats()
	t.Logf("indexed in %s (%.0f MiB/s); %d lines, stride %d, %d checkpoints (%d KiB)",
		indexTime.Round(time.Millisecond),
		float64(sizeMiB)/indexTime.Seconds(),
		gotLines, stride, ckpts, int64(ckpts)*8/1024)

	if gotLines != wantLines {
		t.Fatalf("indexed %d lines, reference says %d", gotLines, wantLines)
	}

	v := NewBase(src, idx, 0, 1<<20)

	// Random access must agree with a forward walk from the same point. This is
	// the property the sparse index exists to provide.
	t.Run("random access agrees with iteration", func(t *testing.T) {
		rng := rand.New(rand.NewSource(42))
		for probe := 0; probe < 200; probe++ {
			start := rng.Int63n(gotLines - 40)

			var seq []string
			if err := v.Iterate(start, func(i int64, ln Line) bool {
				seq = append(seq, string(ln.Text))
				return len(seq) < 32
			}); err != nil {
				t.Fatal(err)
			}
			for k, want := range seq {
				ln, err := v.At(start+int64(k), nil)
				if err != nil {
					t.Fatalf("At(%d): %v", start+int64(k), err)
				}
				if string(ln.Text) != want {
					t.Fatalf("At(%d) = %q, iteration says %q", start+int64(k), ln.Text, want)
				}
				if ln.Num != start+int64(k)+1 {
					t.Fatalf("At(%d).Num = %d", start+int64(k), ln.Num)
				}
			}
		}
	})

	t.Run("filtering finds every match", func(t *testing.T) {
		m, err := filter.Compile(pattern, filter.Sensitive)
		if err != nil {
			t.Fatal(err)
		}
		rules := filter.NewSet().Add(filter.Rule{Action: filter.Only, M: m, Enabled: true})

		f := NewFiltered(src, idx, rules, 0, 1<<20, 50*time.Millisecond, nil)
		defer f.Close()

		start := time.Now()
		deadline := time.Now().Add(25 * time.Minute)
		for !f.Done() {
			if time.Now().After(deadline) {
				t.Fatal("filter scan did not finish")
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Logf("filtered in %s (%.0f MiB/s); %d matches",
			time.Since(start).Round(time.Millisecond),
			float64(sizeMiB)/time.Since(start).Seconds(), f.Len())

		if f.Len() != wantMatches {
			t.Fatalf("filtered view has %d lines, reference says %d", f.Len(), wantMatches)
		}

		// Spot-check that filtered lines really do match and that their source
		// line numbers are strictly increasing.
		rng := rand.New(rand.NewSource(7))
		var prev int64
		for probe := 0; probe < 200; probe++ {
			i := rng.Int63n(f.Len())
			ln, err := f.At(i, nil)
			if err != nil {
				t.Fatalf("At(%d): %v", i, err)
			}
			if !bytes.Contains(ln.Text, []byte(pattern)) {
				t.Fatalf("filtered line %d does not contain %q: %q", i, pattern, ln.Text)
			}
			if i > 0 {
				before, err := f.At(i-1, nil)
				if err != nil {
					t.Fatal(err)
				}
				if before.Num >= ln.Num {
					t.Fatalf("line numbers not increasing: %d then %d", before.Num, ln.Num)
				}
			}
			prev = ln.Num
		}
		_ = prev
	})

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	rss := peakRSS(t)
	t.Logf("peak RSS %d MiB, Go heap in use %d MiB, for a %d MiB file",
		rss, ms.HeapInuse>>20, sizeMiB)

	if rss > rssBudgetMiB {
		t.Fatalf("peak RSS %d MiB exceeds the %d MiB budget", rss, rssBudgetMiB)
	}
}
