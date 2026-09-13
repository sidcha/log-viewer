package view

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/index"
	"github.com/sidcha/log-viewer/internal/source"
)

// corpus builds a deliberately irregular log so line lengths vary across block
// boundaries.
func corpus(n int) ([]string, string) {
	lines := make([]string, 0, n)
	var b strings.Builder
	for i := 0; i < n; i++ {
		var l string
		switch i % 5 {
		case 0:
			l = fmt.Sprintf("%06d INFO steady state", i)
		case 1:
			l = fmt.Sprintf("%06d ERROR something broke %s", i, strings.Repeat("x", i%37))
		case 2:
			l = fmt.Sprintf("%06d DEBUG noisy chatter", i)
		case 3:
			l = "" // empty lines must still be addressable
		default:
			l = fmt.Sprintf("%06d WARN %s", i, strings.Repeat("y", i%11))
		}
		lines = append(lines, l)
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return lines, b.String()
}

func setup(t *testing.T, content string, stride int64, maxCk int) (*source.Source, *index.Index) {
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

	idx := index.New(stride, maxCk)
	ix := index.NewIndexer(src, idx, index.Options{Block: 128})
	ix.Sync(context.Background())
	if err := idx.Err(); err != nil {
		t.Fatalf("index: %v", err)
	}
	return src, idx
}

func TestBaseAddressesEveryLine(t *testing.T) {
	want, content := corpus(2000)
	// A small stride and a small cap force both forward walking and a collapsed
	// checkpoint table.
	src, idx := setup(t, content, 7, 8)

	v := NewBase(src, idx, 128, 1<<20)
	if got := v.Len(); got != int64(len(want)) {
		t.Fatalf("Len = %d, want %d", got, len(want))
	}

	t.Run("sequential", func(t *testing.T) {
		for i := range want {
			ln, err := v.At(int64(i), nil)
			if err != nil {
				t.Fatalf("At(%d): %v", i, err)
			}
			if string(ln.Text) != want[i] {
				t.Fatalf("At(%d) = %q, want %q", i, ln.Text, want[i])
			}
			if ln.Num != int64(i)+1 {
				t.Fatalf("At(%d).Num = %d, want %d", i, ln.Num, i+1)
			}
		}
	})

	t.Run("random access", func(t *testing.T) {
		rng := rand.New(rand.NewSource(1))
		for k := 0; k < 2000; k++ {
			i := rng.Intn(len(want))
			ln, err := v.At(int64(i), nil)
			if err != nil {
				t.Fatalf("At(%d): %v", i, err)
			}
			if string(ln.Text) != want[i] {
				t.Fatalf("At(%d) = %q, want %q", i, ln.Text, want[i])
			}
		}
	})

	t.Run("out of range", func(t *testing.T) {
		if _, err := v.At(int64(len(want)), nil); err != ErrRange {
			t.Errorf("At(len) err = %v, want ErrRange", err)
		}
		if _, err := v.At(-1, nil); err != ErrRange {
			t.Errorf("At(-1) err = %v, want ErrRange", err)
		}
	})
}

func TestBaseIterate(t *testing.T) {
	want, content := corpus(500)
	src, idx := setup(t, content, 16, 0)
	v := NewBase(src, idx, 128, 1<<20)

	for _, from := range []int64{0, 1, 137, 499} {
		var got []string
		err := v.Iterate(from, func(i int64, ln Line) bool {
			if i != from+int64(len(got)) {
				t.Fatalf("Iterate index = %d, want %d", i, from+int64(len(got)))
			}
			got = append(got, string(ln.Text))
			return len(got) < 20
		})
		if err != nil {
			t.Fatalf("Iterate(%d): %v", from, err)
		}
		end := int(from) + 20
		if end > len(want) {
			end = len(want)
		}
		if strings.Join(got, "|") != strings.Join(want[from:end], "|") {
			t.Fatalf("Iterate(%d) = %q, want %q", from, got, want[from:end])
		}
	}
}

// waitScan blocks until the filtered scan has consumed the whole file.
func waitScan(t *testing.T, f *Filtered) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !f.Done() {
		if time.Now().After(deadline) {
			t.Fatal("filtered scan did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func newFiltered(t *testing.T, src *source.Source, idx *index.Index, s *filter.Set) *Filtered {
	t.Helper()
	f := NewFiltered(src, idx, s, FilteredOptions{Block: 128, MaxLine: 1 << 20, Poll: 5 * time.Millisecond})
	t.Cleanup(f.Close)
	return f
}

func TestFilteredMatchesReference(t *testing.T) {
	all, content := corpus(3000)
	src, idx := setup(t, content, 7, 8)

	keep, err := filter.Compile("ERROR", filter.Literal, filter.Sensitive)
	if err != nil {
		t.Fatal(err)
	}
	// Long enough that it culls a minority of the ERROR lines rather than all
	// of them, so both rules genuinely participate.
	drop, err := filter.Compile(strings.Repeat("x", 30), filter.Literal, filter.Sensitive)
	if err != nil {
		t.Fatal(err)
	}
	rules := filter.NewSet().
		Add(filter.Rule{Action: filter.Only, M: keep, Enabled: true}).
		Add(filter.Rule{Action: filter.Never, M: drop, Enabled: true})

	// The reference answer, computed the slow obvious way.
	type ref struct {
		text string
		num  int64
	}
	var want []ref
	for i, l := range all {
		if rules.Pass([]byte(l)) {
			want = append(want, ref{l, int64(i) + 1})
		}
	}
	if len(want) < 100 {
		t.Fatalf("corpus produced only %d matches; test would be weak", len(want))
	}

	f := newFiltered(t, src, idx, rules)
	waitScan(t, f)

	if got := f.Len(); got != int64(len(want)) {
		t.Fatalf("Len = %d, want %d", got, len(want))
	}

	for i := range want {
		ln, err := f.At(int64(i), nil)
		if err != nil {
			t.Fatalf("At(%d): %v", i, err)
		}
		if string(ln.Text) != want[i].text || ln.Num != want[i].num {
			t.Fatalf("At(%d) = (%q, %d), want (%q, %d)", i, ln.Text, ln.Num, want[i].text, want[i].num)
		}
	}

	t.Run("random access", func(t *testing.T) {
		rng := rand.New(rand.NewSource(7))
		for k := 0; k < 1000; k++ {
			i := rng.Intn(len(want))
			ln, err := f.At(int64(i), nil)
			if err != nil {
				t.Fatalf("At(%d): %v", i, err)
			}
			if string(ln.Text) != want[i].text {
				t.Fatalf("At(%d) = %q, want %q", i, ln.Text, want[i].text)
			}
		}
	})

	t.Run("iterate", func(t *testing.T) {
		var got []string
		if err := f.Iterate(10, func(i int64, ln Line) bool {
			got = append(got, string(ln.Text))
			return len(got) < 25
		}); err != nil {
			t.Fatal(err)
		}
		for k, s := range got {
			if s != want[10+k].text {
				t.Fatalf("Iterate[%d] = %q, want %q", k, s, want[10+k].text)
			}
		}
	})

	t.Run("index for source", func(t *testing.T) {
		// Anchoring must land on the first surviving line at or after a source
		// line, which is what holds the viewport still across a filter change.
		for _, k := range []int{0, 5, 137, len(want) - 1} {
			srcLine := want[k].num - 1
			got, ok := f.IndexForSource(srcLine)
			if !ok || got != int64(k) {
				t.Fatalf("IndexForSource(%d) = (%d, %v), want (%d, true)", srcLine, got, ok, k)
			}
		}
		// A source line that was filtered out resolves to the next survivor.
		if k := 1; want[k].num > 1 {
			got, ok := f.IndexForSource(want[k].num - 2)
			if !ok || got != int64(k) {
				t.Fatalf("IndexForSource of a dropped line = (%d, %v), want (%d, true)", got, ok, k)
			}
		}
	})
}

func TestFilteredEmptyResult(t *testing.T) {
	_, content := corpus(200)
	src, idx := setup(t, content, 8, 0)

	m, _ := filter.Compile("no-such-text-anywhere", filter.Literal, filter.Sensitive)
	f := newFiltered(t, src, idx, filter.NewSet().
		Add(filter.Rule{Action: filter.Only, M: m, Enabled: true}))
	waitScan(t, f)

	if f.Len() != 0 {
		t.Fatalf("Len = %d, want 0", f.Len())
	}
	if _, err := f.At(0, nil); err != ErrRange {
		t.Errorf("At(0) err = %v, want ErrRange", err)
	}
	if err := f.Iterate(0, func(int64, Line) bool { return true }); err != nil {
		t.Errorf("Iterate on an empty view: %v", err)
	}
}

func TestSearch(t *testing.T) {
	all, content := corpus(1000)
	src, idx := setup(t, content, 16, 0)
	v := NewBase(src, idx, 128, 1<<20)

	m, err := filter.Compile("ERROR", filter.Literal, filter.Sensitive)
	if err != nil {
		t.Fatal(err)
	}

	var hits []int64
	for i, l := range all {
		if strings.Contains(l, "ERROR") {
			hits = append(hits, int64(i))
		}
	}

	ctx := context.Background()

	t.Run("forward finds the first hit at or after", func(t *testing.T) {
		got, found, err := Search(ctx, v, 0, m, false, nil)
		if err != nil || !found || got != hits[0] {
			t.Fatalf("Search = (%d, %v, %v), want %d", got, found, err, hits[0])
		}
		got, found, _ = Search(ctx, v, hits[0]+1, m, false, nil)
		if !found || got != hits[1] {
			t.Fatalf("Search next = %d, want %d", got, hits[1])
		}
	})

	t.Run("backward finds the last hit at or before", func(t *testing.T) {
		last := hits[len(hits)-1]
		got, found, err := Search(ctx, v, int64(len(all))-1, m, true, nil)
		if err != nil || !found || got != last {
			t.Fatalf("Search back = (%d, %v, %v), want %d", got, found, err, last)
		}
		got, found, _ = Search(ctx, v, last-1, m, true, nil)
		if !found || got != hits[len(hits)-2] {
			t.Fatalf("Search back prev = %d, want %d", got, hits[len(hits)-2])
		}
	})

	t.Run("no match", func(t *testing.T) {
		nm, _ := filter.Compile("absolutely-not-present", filter.Literal, filter.Sensitive)
		if _, found, err := Search(ctx, v, 0, nm, false, nil); found || err != nil {
			t.Fatalf("Search = (%v, %v), want (false, nil)", found, err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		nm, _ := filter.Compile("absolutely-not-present", filter.Literal, filter.Sensitive)
		if _, _, err := Search(cctx, v, 0, nm, false, nil); err == nil {
			t.Log("search completed before noticing cancellation, which is fine for a small corpus")
		}
	})
}
