package view

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sidcha/log-viewer/internal/filter"
)

// TestNarrowingSkipsWhatTheOldRulesRuledOut: a narrower rule set must give the
// same answer as a fresh scan, while reading only near the old matches.
func TestNarrowingSkipsWhatTheOldRulesRuledOut(t *testing.T) {
	const n = 20000
	var b strings.Builder
	var want []int64
	for i := 0; i < n; i++ {
		switch {
		case i%2000 == 7:
			fmt.Fprintf(&b, "%05d ERROR disk full\n", i)
			want = append(want, int64(i))
		case i%2000 == 8:
			fmt.Fprintf(&b, "%05d ERROR healthz timeout\n", i)
		default:
			fmt.Fprintf(&b, "%05d INFO steady %s\n", i, strings.Repeat("s", i%17))
		}
	}
	src, idx := setup(t, b.String(), 64, 0)

	errs, _ := filter.Compile("ERROR", filter.Literal, filter.Sensitive)
	healthz, _ := filter.Compile("healthz", filter.Literal, filter.Sensitive)
	wide := filter.NewSet().Add(filter.Rule{Action: filter.Only, M: errs, Enabled: true})
	narrow := wide.Add(filter.Rule{Action: filter.Never, M: healthz, Enabled: true})
	if !narrow.Narrows(wide) {
		t.Fatal("precondition: adding a never rule narrows")
	}

	opt := FilteredOptions{Block: 256, Poll: 5 * time.Millisecond, CheckpointBytes: 512}
	old := NewFiltered(src, idx, wide, opt)
	t.Cleanup(old.Close)
	waitScan(t, old)

	opt.Narrowing = old.Narrowing()
	f := NewFiltered(src, idx, narrow, opt)
	t.Cleanup(f.Close)
	waitScan(t, f)

	if f.Len() != int64(len(want)) {
		t.Fatalf("Len = %d, want %d", f.Len(), len(want))
	}
	for i := len(want) - 1; i >= 0; i-- {
		ln, err := f.At(int64(i), nil)
		if err != nil || ln.Num-1 != want[i] {
			t.Fatalf("At(%d) = line %d, %v; want line %d", i, ln.Num-1, err, want[i])
		}
	}
	for k, w := range want {
		if got, ok := f.IndexForSource(w); !ok || got != int64(k) {
			t.Fatalf("IndexForSource(%d) = %d, %v; want %d", w, got, ok, k)
		}
	}
	if got := f.SourceLinesScanned(); got != n {
		t.Fatalf("SourceLinesScanned = %d, want %d: the skip lost count of lines", got, n)
	}

	f.mu.RLock()
	examined := f.examined
	f.mu.RUnlock()
	if examined > n/4 {
		t.Errorf("examined %d of %d lines; the narrowing skipped too little", examined, n)
	}
	t.Logf("examined %d of %d lines", examined, n)
}

// TestNarrowingKeepsUpWithAGrowingFile: past what the old view had seen, the
// scan reads every line again.
func TestNarrowingKeepsUpWithAGrowingFile(t *testing.T) {
	_, content := corpus(2000)
	src, idx := setup(t, content, 16, 0)

	errs, _ := filter.Compile("ERROR", filter.Literal, filter.Sensitive)
	xs, _ := filter.Compile("xxxxx", filter.Literal, filter.Sensitive)
	wide := filter.NewSet().Add(filter.Rule{Action: filter.Only, M: errs, Enabled: true})
	narrow := wide.Add(filter.Rule{Action: filter.Never, M: xs, Enabled: true})

	opt := FilteredOptions{Block: 128, Poll: 5 * time.Millisecond, CheckpointBytes: 256}
	old := NewFiltered(src, idx, wide, opt)
	t.Cleanup(old.Close)
	waitScan(t, old)
	nw := old.Narrowing()
	// Cut the old view's knowledge short, as if it had been replaced mid-scan.
	nw.ents = nw.ents[:len(nw.ents)/2]

	opt.Narrowing = nw
	f := NewFiltered(src, idx, narrow, opt)
	t.Cleanup(f.Close)
	waitScan(t, f)

	ref := NewFiltered(src, idx, narrow, FilteredOptions{Block: 128, Poll: 5 * time.Millisecond})
	t.Cleanup(ref.Close)
	waitScan(t, ref)

	if f.Len() != ref.Len() {
		t.Fatalf("Len = %d, a fresh scan finds %d", f.Len(), ref.Len())
	}
	for i := int64(0); i < ref.Len(); i++ {
		a, _ := f.At(i, nil)
		b, _ := ref.At(i, nil)
		if a.Num != b.Num {
			t.Fatalf("At(%d) = line %d, a fresh scan says %d", i, a.Num, b.Num)
		}
	}
}
