package ui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/filter"
)

// The window has to agree with what pressing Enter will do.
//
// Every bug reported against it so far has been a disagreement rather than a
// wrong answer: a preview that could only describe the lines already on screen,
// a count that contradicted the filter, a search that listed nothing and then
// lit up matches the moment it was committed, a ? that previewed forwards and
// searched backwards. The tests below state the agreement itself, so the next
// change to the scan, the match modes or the commit path has to keep it.

// corpus is the test log, one line per entry, indexed the way an unfiltered
// view indexes it.
func corpus(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, numberedLine(i))
	}
	return out
}

func corpusText(lines []string) string { return strings.Join(lines, "\n") + "\n" }

// containsFuzzy is the reference fuzzy match: the pattern's characters in
// order, anything between.
func containsFuzzy(line, pat string) bool {
	rest := line
	for _, r := range pat {
		i := strings.IndexRune(rest, r)
		if i < 0 {
			return false
		}
		rest = rest[i+utf8.RuneLen(r):]
	}
	return true
}

// wantRows is the order the window should offer matches in, worked out the
// slow obvious way: from the cursor down, then from the top if it wraps.
func wantRows(lines []string, start int, wraps bool, match func(string) bool) []int64 {
	var out []int64
	for i := start; i < len(lines); i++ {
		if match(lines[i]) {
			out = append(out, int64(i+1))
		}
	}
	if !wraps {
		return out
	}
	for i := 0; i < start && i < len(lines); i++ {
		if match(lines[i]) {
			out = append(out, int64(i+1))
		}
	}
	return out
}

// TestWindowListsTheRightMatchesInTheRightOrder, for every key that opens one.
func TestWindowListsTheRightMatchesInTheRightOrder(t *testing.T) {
	lines := corpus(300)

	kinds := []struct {
		key   rune
		wraps bool
		fuzzy bool
	}{
		{'/', true, false},
		{'?', true, false},
		{'f', true, true},
		{'&', false, false},
		{'!', false, false},
		{'m', false, false},
	}
	// A pattern present throughout, one present only near the top, and one
	// present only near the bottom.
	patterns := []string{"ERROR", "line 00", "line 29"}
	starts := []int{0, 150, 299}

	for _, k := range kinds {
		for _, pat := range patterns {
			for _, start := range starts {
				name := fmt.Sprintf("%c/%s/from%d", k.key, pat, start)
				t.Run(name, func(t *testing.T) {
					a, _ := newTestApp(t, corpusText(lines))
					a.setCursor(int64(start))
					a.draw()

					match := func(s string) bool { return strings.Contains(s, pat) }
					if k.fuzzy {
						match = func(s string) bool { return containsFuzzy(s, pat) }
					}
					want := wantRows(lines, start, k.wraps, match)

					f := openFinderWith(t, a, k.key, pat)
					got := rowNums(f)

					if len(got) > len(want) {
						t.Fatalf("listed %d matches, only %d exist", len(got), len(want))
					}
					for i := range got {
						if got[i] != want[i] {
							t.Fatalf("row %d = line %d, want %d\n got %v\nwant %v",
								i, got[i], want[i], got, want[:len(got)])
						}
					}
					// A window that stopped early must have stopped because it
					// was full, not because it gave up.
					if f.atEnd && len(got) != len(want) {
						t.Errorf("the scan finished with %d of %d matches", len(got), len(want))
					}
					if !f.atEnd && len(got) < f.height {
						t.Errorf("the scan is unfinished with only %d rows for %d visible", len(got), f.height)
					}
				})
			}
		}
	}
}

// TestScanTopsUpWithoutOverReading: moving the selection down asks for more
// matches, and each pass must fill the gap rather than gather a fresh
// windowful on top of what it already had.
//
// Reading too much is invisible on a small file and ruinous on a large one,
// which is the whole reason the scan is bounded by what can be shown.
func TestScanTopsUpWithoutOverReading(t *testing.T) {
	a, _ := newTestApp(t, numbered(5000))
	a.draw()

	f := openFinderWith(t, a, 'm', "line") // matches every line
	for i := 0; i < 4; i++ {
		a.moveSel(f.height)
		settleFinder(t, a)

		if len(f.rows) > f.wanted() {
			t.Fatalf("pass %d gathered %d matches for a window wanting %d",
				i, len(f.rows), f.wanted())
		}
		if f.scanned > int64(len(f.rows))+int64(f.height)+finderLookahead {
			t.Fatalf("pass %d read %d lines for %d matches", i, f.scanned, len(f.rows))
		}
	}

	seen := map[int64]bool{}
	var last int64
	for i, n := range rowNums(f) {
		if seen[n] {
			t.Fatalf("line %d listed twice", n)
		}
		seen[n] = true
		if i > 0 && n <= last {
			t.Fatalf("row %d is line %d, after line %d: the list went backwards", i, n, last)
		}
		last = n
	}
}

// TestAnEmptyWindowMeansThereIsNothingToFind. This is the reported bug stated
// as an invariant: the window may not answer "no matches" for a pattern the
// search it commits would go on to find.
func TestAnEmptyWindowMeansThereIsNothingToFind(t *testing.T) {
	lines := corpus(200)

	for _, key := range []rune{'/', '?', 'f'} {
		t.Run(string(key), func(t *testing.T) {
			a, _ := newTestApp(t, corpusText(lines))
			a.setCursor(150) // most of the file is behind us
			a.draw()

			// A pattern that exists, only above the cursor.
			f := openFinderWith(t, a, key, "line 004")
			if len(f.rows) == 0 {
				t.Fatal("the window found nothing for a pattern that is in the file")
			}
			a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))
			waitSearch(t, a)
			if got := a.anchorSource() + 1; got != 5 {
				t.Errorf("landed on line %d, want the match at 5", got)
			}

			// And a pattern that is genuinely absent stays absent.
			a2, _ := newTestApp(t, corpusText(lines))
			a2.draw()
			f2 := openFinderWith(t, a2, key, "no such text")
			if len(f2.rows) != 0 {
				t.Fatalf("listed %d matches for a pattern in no line", len(f2.rows))
			}
			if !f2.atEnd {
				t.Error("the scan did not finish on a pattern that matches nothing")
			}
			before := a2.cursorLine()
			a2.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))
			waitSearch(t, a2)
			if got := a2.cursorLine(); got != before {
				t.Errorf("the cursor moved to %d for a pattern that matches nothing", got)
			}
		})
	}
}

// TestCommittedSearchMatchesExactlyWhatWasListed, so the highlight that
// appears cannot include lines the window never offered.
func TestCommittedSearchMatchesExactlyWhatWasListed(t *testing.T) {
	lines := corpus(120)
	a, _ := newTestApp(t, corpusText(lines))
	a.draw()

	f := openFinderWith(t, a, '/', "ERROR")
	listed := map[int64]bool{}
	for _, n := range rowNums(f) {
		listed[n] = true
	}
	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))
	waitSearch(t, a)

	if a.search.m == nil {
		t.Fatal("no search was installed")
	}
	for i, line := range lines {
		got := a.search.m.Match([]byte(line))
		// Every listed line matches; an unlisted line only matches if the
		// window simply had not reached it.
		if listed[int64(i+1)] && !got {
			t.Errorf("line %d was listed but the committed search does not match it", i+1)
		}
		if got && !listed[int64(i+1)] && f.atEnd {
			t.Errorf("the committed search matches line %d, which the finished window never listed", i+1)
		}
	}
}

// TestCommittedRuleRemovesExactlyWhatWasListed for a never rule, where the
// listed lines are the casualties.
func TestCommittedRuleRemovesExactlyWhatWasListed(t *testing.T) {
	lines := corpus(120)
	a, _ := newTestApp(t, corpusText(lines))
	a.draw()
	before := a.viewLen()

	openFinderWith(t, a, '!', "ERROR")
	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))
	waitFiltered(t, a)

	var want int64
	for _, line := range lines {
		if !strings.Contains(line, "ERROR") {
			want++
		}
	}
	if got := a.viewLen(); got != want {
		t.Errorf("view has %d lines, want %d of %d", got, want, before)
	}
}

// TestCommittedOnlyKeepsExactlyWhatWasListed, the other direction.
func TestCommittedOnlyKeepsExactlyWhatWasListed(t *testing.T) {
	lines := corpus(120)
	a, _ := newTestApp(t, corpusText(lines))
	a.draw()

	openFinderWith(t, a, '&', "ERROR")
	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))
	waitFiltered(t, a)

	var want int64
	for _, line := range lines {
		if strings.Contains(line, "ERROR") {
			want++
		}
	}
	if got := a.viewLen(); got != want {
		t.Errorf("view has %d lines, want %d", got, want)
	}
}

// TestNothingChangesWhileTheWindowIsOpen: the log stays exactly where it was
// until Enter, so a pattern can be tried and abandoned freely.
func TestNothingChangesWhileTheWindowIsOpen(t *testing.T) {
	for _, key := range []rune{'/', '?', 'f', '&', '!', 'm'} {
		t.Run(string(key), func(t *testing.T) {
			a, _ := newTestApp(t, numbered(200))
			a.addRule(filter.Never, "line 01", color.RGB{})
			waitFiltered(t, a)
			a.setCursor(40)
			a.doSearch("INFO", false)
			waitSearch(t, a)
			a.draw()

			type snapshot struct {
				top, cur, viewLen int64
				rules             string
				search            string
			}
			take := func() snapshot {
				return snapshot{a.top, a.cur, a.viewLen(), a.rules.Fingerprint(), a.search.pat}
			}
			before := take()

			openFinderWith(t, a, key, "ERROR")
			if got := take(); got != before {
				t.Errorf("opening and typing changed the session:\n got %+v\nwant %+v", got, before)
			}

			press(a, tcell.KeyEscape)
			a.draw()
			if got := take(); got != before {
				t.Errorf("cancelling changed the session:\n got %+v\nwant %+v", got, before)
			}
		})
	}
}

// TestTabLooksWithoutCommitting for every window that can jump.
func TestTabLooksWithoutCommitting(t *testing.T) {
	for _, key := range []rune{'/', '?', 'f', '&', '!', 'm'} {
		t.Run(string(key), func(t *testing.T) {
			a, _ := newTestApp(t, numbered(200))
			a.draw()

			f := openFinderWith(t, a, key, "ERROR")
			if len(f.rows) < 2 {
				t.Fatalf("only %d matches to move through", len(f.rows))
			}
			a.moveSel(1)
			settleFinder(t, a)
			want := f.selected().num

			press(a, tcell.KeyTab)
			if a.find == nil {
				t.Fatal("Tab closed the window")
			}
			if got := a.anchorSource() + 1; got != want {
				t.Errorf("Tab went to line %d, want %d", got, want)
			}
			if a.rules.Len() != 0 {
				t.Error("Tab made a rule")
			}
			if a.search.m != nil {
				t.Error("Tab installed a search")
			}
		})
	}
}

// TestTheModeShownIsTheModeCommitted: the title says PCRE, so the rule that is
// saved had better be read as PCRE too.
func TestTheModeShownIsTheModeCommitted(t *testing.T) {
	cases := []struct {
		key  rune
		want filter.Mode
	}{
		{'/', filter.Literal},
		{'f', filter.Fuzzy},
		{'&', filter.Literal},
		{'!', filter.Literal},
		{'m', filter.Literal},
	}
	for _, c := range cases {
		t.Run(string(c.key), func(t *testing.T) {
			for _, regex := range []bool{false, true} {
				a, _ := newTestApp(t, numbered(60))
				a.draw()

				a.key(tcell.NewEventKey(tcell.KeyRune, c.key, tcell.ModNone))
				if regex {
					press(a, tcell.KeyCtrlR)
				}
				typeInto(a, "ERROR")
				settleFinder(t, a)

				want := c.want
				if regex {
					want = filter.Regex
				}
				if got := a.find.mode; got != want {
					t.Fatalf("window mode = %v, want %v", got, want)
				}
				a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))

				var got filter.Mode
				switch {
				case a.rules.Len() > 0:
					got = a.rules.All()[0].M.Mode()
				case a.search.m != nil:
					got = a.search.m.Mode()
				default:
					if c.key == 'f' {
						return // a jump commits nothing to inspect
					}
					t.Fatal("nothing was committed")
				}
				if got != want {
					t.Errorf("committed as %v, want the %v the window showed", got, want)
				}
			}
		})
	}
}

// TestTheDirectionShownIsTheDirectionCommitted: ? previewed forwards and then
// searched forwards, so the backward key never worked.
func TestTheDirectionShownIsTheDirectionCommitted(t *testing.T) {
	for _, c := range []struct {
		key  rune
		back bool
	}{{'/', false}, {'?', true}} {
		t.Run(string(c.key), func(t *testing.T) {
			a, _ := newTestApp(t, numbered(200))
			a.setCursor(100)
			a.draw()

			a.key(tcell.NewEventKey(tcell.KeyRune, c.key, tcell.ModNone))
			if a.find.back != c.back {
				t.Fatalf("window back = %v, want %v", a.find.back, c.back)
			}
			typeInto(a, "ERROR")
			settleFinder(t, a)
			a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))
			waitSearch(t, a)

			if a.search.back != c.back {
				t.Errorf("committed back = %v, want %v", a.search.back, c.back)
			}
		})
	}
}
