package view

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sidcha/log-viewer/internal/filter"
)

// TestSparseFilterIsAddressableEverywhere drives the byte axis of the
// checkpoint table: matches are hundreds of lines apart, far more than one
// byte stride, so every lookup starts from a position between matches rather
// than from a match of its own.
func TestSparseFilterIsAddressableEverywhere(t *testing.T) {
	const n = 5000
	var (
		b       strings.Builder
		want    []int64 // source line of each match
		matches = map[int]bool{3: true, 4: true, 900: true, 2500: true, 2501: true, 4999: true}
	)
	for i := 0; i < n; i++ {
		if matches[i] {
			fmt.Fprintf(&b, "%05d NEEDLE here\n", i)
			want = append(want, int64(i))
			continue
		}
		fmt.Fprintf(&b, "%05d hay %s\n", i, strings.Repeat("h", i%13))
	}
	src, idx := setup(t, b.String(), 16, 0)

	m, err := filter.Compile("NEEDLE", filter.Literal, filter.Sensitive)
	if err != nil {
		t.Fatal(err)
	}
	rules := filter.NewSet().Add(filter.Rule{Action: filter.Only, M: m, Enabled: true})
	f := NewFiltered(src, idx, rules, FilteredOptions{Block: 128, Poll: 5 * time.Millisecond, CheckpointBytes: 256})
	t.Cleanup(f.Close)
	waitScan(t, f)

	if f.Len() != int64(len(want)) {
		t.Fatalf("Len = %d, want %d", f.Len(), len(want))
	}
	if entries := f.ck.Len(); entries < 50 {
		t.Fatalf("only %d checkpoints; the byte axis did not record", entries)
	}

	// Backwards, so the position cache cannot carry each lookup.
	for i := len(want) - 1; i >= 0; i-- {
		ln, err := f.At(int64(i), nil)
		if err != nil {
			t.Fatalf("At(%d): %v", i, err)
		}
		if ln.Num-1 != want[i] {
			t.Fatalf("At(%d) is source line %d, want %d", i, ln.Num-1, want[i])
		}
	}

	for srcLine := int64(0); srcLine < n; srcLine++ {
		wantIdx := int64(len(want))
		for k, w := range want {
			if w >= srcLine {
				wantIdx = int64(k)
				break
			}
		}
		got, ok := f.IndexForSource(srcLine)
		if got != wantIdx || ok != (wantIdx < int64(len(want))) {
			t.Fatalf("IndexForSource(%d) = (%d, %v), want %d", srcLine, got, ok, wantIdx)
		}
	}

	for from := 0; from < len(want); from++ {
		var got []int64
		if err := f.Iterate(int64(from), func(i int64, ln Line) bool {
			got = append(got, ln.Num-1)
			return true
		}); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(got) != fmt.Sprint(want[from:]) {
			t.Fatalf("Iterate(%d) = %v, want %v", from, got, want[from:])
		}
	}
}
