package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/filter"
)

// settleFinder runs the scan to the point the window is full, playing the part
// the event loop plays in a live session: the scan runs on its own goroutine
// and hands its results back through the post channel.
func settleFinder(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.draw() // settles f.height, which is how many matches are wanted
		f := a.find
		if f == nil {
			return
		}
		if !f.scanning && (f.atEnd || len(f.rows) >= f.wanted()) {
			a.draw()
			return
		}
		select {
		case fn := <-a.post:
			fn()
		case <-time.After(time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("finder scan did not settle")
		}
	}
}

func openFinderWith(t *testing.T, a *App, open rune, pattern string) *finder {
	t.Helper()
	a.key(tcell.NewEventKey(tcell.KeyRune, open, tcell.ModNone))
	if a.find == nil {
		t.Fatalf("%q did not open the finder", open)
	}
	typeInto(a, pattern)
	settleFinder(t, a)
	return a.find
}

func rowNums(f *finder) []int64 {
	out := make([]int64, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r.num)
	}
	return out
}

// TestFinderShowsMatchesBeyondTheViewport is the reason the window exists: the
// inline preview it replaced could only describe lines already on screen.
func TestFinderShowsMatchesBeyondTheViewport(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 400; i++ {
		if i == 350 {
			b.WriteString("the needle is far below\n")
			continue
		}
		fmt.Fprintf(&b, "line %03d filler\n", i)
	}
	a, _ := newTestApp(t, b.String())

	f := openFinderWith(t, a, '&', "needle")
	if got := rowNums(f); len(got) != 1 || got[0] != 350 {
		t.Fatalf("matches = %v, want just line 350", got)
	}
	if a.top != 0 {
		t.Errorf("the log scrolled to %d; the finder should leave it alone", a.top)
	}
	if a.filtered() {
		t.Error("nothing should be filtered until the rule is committed")
	}
}

// TestFinderStopsOnceTheWindowIsFull: the scan is bounded by what can be shown,
// not by the size of the file.
func TestFinderStopsOnceTheWindowIsFull(t *testing.T) {
	a, _ := newTestApp(t, numbered(5000))

	f := openFinderWith(t, a, 'm', "line")
	if len(f.rows) < f.height {
		t.Fatalf("gathered %d matches, too few to fill %d rows", len(f.rows), f.height)
	}
	if f.wanted() == 0 || len(f.rows) > f.wanted() {
		t.Fatalf("gathered %d matches for a window wanting %d", len(f.rows), f.wanted())
	}
	if f.scanned >= 5000 {
		t.Errorf("read %d lines of 5000; the scan should stop when the window fills", f.scanned)
	}
}

// TestFinderSearchesFromTheReadingPosition: matches above where you are are not
// what you asked for.
func TestFinderSearchesFromTheReadingPosition(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	a.gotoSourceLine(120)
	a.draw()

	f := openFinderWith(t, a, '/', "ERROR")
	nums := rowNums(f)
	if len(nums) == 0 {
		t.Fatal("no matches")
	}
	if nums[0] < 120 {
		t.Errorf("first match is line %d, before the cursor at 120", nums[0])
	}
}

// TestOnlyTheJumpIsFuzzy: f is the loose one. A search and a rule both mean
// the characters typed, or / would be a different tool than the one everybody
// reaches for it expecting.
func TestOnlyTheJumpIsFuzzy(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))

	f := openFinderWith(t, a, 'f', "ERR bd")
	if f.mode != filter.Fuzzy {
		t.Fatalf("jump mode = %v, want fuzzy", f.mode)
	}
	if len(f.rows) == 0 {
		t.Error("a fuzzy jump should reach ERROR bad through the gaps")
	}
	press(a, tcell.KeyEscape)

	for _, open := range []rune{'/', '?', '&', '!', 'm'} {
		f = openFinderWith(t, a, open, "ERR bd")
		if f.mode != filter.Literal {
			t.Errorf("%q opened in %v, want literal", open, f.mode)
		}
		if len(f.rows) != 0 {
			t.Errorf("%q matched %d lines for a pattern no line contains", open, len(f.rows))
		}
		press(a, tcell.KeyEscape)
	}
}

// TestSearchIsLiteral all the way to the search that n and N walk, not just in
// the window that collected it.
func TestSearchIsLiteral(t *testing.T) {
	a, _ := newTestApp(t, "ERROR here\nE bad R bad R\n")
	a.draw()

	openFinderWith(t, a, '/', "ERROR")
	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))

	if a.search.m == nil {
		t.Fatal("no search installed")
	}
	if got := a.search.m.Mode(); got != filter.Literal {
		t.Fatalf("search reads as %v, want literal", got)
	}
	if a.search.m.Match([]byte("E bad R bad R")) {
		t.Error("the search matched a line that merely has the letters in order")
	}
}

// TestFinderRegexToggle: C-r reads the same text as PCRE, and back again.
func TestFinderRegexToggle(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))

	f := openFinderWith(t, a, '&', `ERROR|nothing`)
	if len(f.rows) != 0 {
		t.Fatal("the alternation should be plain text until PCRE is asked for")
	}

	press(a, tcell.KeyCtrlR)
	settleFinder(t, a)
	if f.mode != filter.Regex {
		t.Fatalf("mode after C-r = %v, want regex", f.mode)
	}
	if len(f.rows) == 0 {
		t.Fatal("PCRE mode did not match the alternation")
	}

	press(a, tcell.KeyCtrlR)
	settleFinder(t, a)
	if f.mode != filter.Literal {
		t.Fatalf("mode after a second C-r = %v, want literal again", f.mode)
	}
}

// TestFinderRegexKeepsPCRESyntax: the point of swapping engines is the syntax
// RE2 cannot express.
func TestFinderRegexKeepsPCRESyntax(t *testing.T) {
	a, _ := newTestApp(t, numbered(30))

	a.key(tcell.NewEventKey(tcell.KeyRune, 'm', tcell.ModNone))
	press(a, tcell.KeyCtrlR)
	typeInto(a, `line \d+ (?=ERROR)`)
	settleFinder(t, a)

	f := a.find
	if f.err != nil {
		t.Fatalf("lookahead rejected: %v", f.err)
	}
	if len(f.rows) == 0 {
		t.Fatal("no matches for a lookahead that should hit every ERROR line")
	}
	for _, r := range f.rows {
		if !strings.Contains(string(r.text), "ERROR") {
			t.Fatalf("matched a line without ERROR: %q", r.text)
		}
	}
}

// TestFinderReportsABadPattern rather than silently matching nothing.
func TestFinderReportsABadPattern(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))

	a.key(tcell.NewEventKey(tcell.KeyRune, '&', tcell.ModNone))
	press(a, tcell.KeyCtrlR)
	typeInto(a, "ERR(")
	a.draw()

	if a.find.err == nil {
		t.Fatal("an unbalanced group was accepted")
	}
	// Enter must not commit a rule that does not compile.
	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))
	if a.find == nil {
		t.Fatal("Enter closed the window on a pattern that does not compile")
	}
}

// TestFinderEnterAppliesTheRule and TestFinderTabJumps are the two ways out of
// the window: Enter commits, Tab only looks.
func TestFinderEnterAppliesTheRule(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))
	full := a.viewLen()

	openFinderWith(t, a, '&', "ERROR")
	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))
	waitFiltered(t, a)

	if a.rules.Count(filter.Only) != 1 {
		t.Fatalf("only rules = %d, want 1", a.rules.Count(filter.Only))
	}
	if a.viewLen() >= full {
		t.Fatalf("the only rule was not applied: %d of %d lines", a.viewLen(), full)
	}
}

func TestFinderTabJumpsWithoutCommitting(t *testing.T) {
	a, _ := newTestApp(t, numbered(300))
	a.draw()

	f := openFinderWith(t, a, '/', "ERROR")
	a.moveSel(2)
	settleFinder(t, a)
	want := f.selected().num

	press(a, tcell.KeyTab)
	if a.find == nil {
		t.Fatal("Tab closed the window")
	}
	if got := a.anchorSource() + 1; got != want {
		t.Errorf("Tab moved to line %d, want %d", got, want)
	}
	if a.rules.Count(filter.Only)+a.rules.Count(filter.Never)+a.rules.Count(filter.Highlight) != 0 {
		t.Error("Tab created a rule")
	}
}

// TestFinderHighlightCommitsAsAHighlight, with the colour the window showed.
func TestFinderHighlightCommitsAsAHighlight(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))

	f := openFinderWith(t, a, 'm', "ERROR")
	want := f.color
	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))

	rules := a.rules.All()
	if len(rules) != 1 || rules[0].Action != filter.Highlight {
		t.Fatalf("rules = %+v, want one highlight", rules)
	}
	if rules[0].Color != want {
		t.Errorf("committed colour %v, want the previewed %v", rules[0].Color, want)
	}
}

// TestFinderNeverStrikesOutWhatItWouldRemove: for a discard rule the listed
// lines are the casualties, and are drawn as such.
func TestFinderNeverStrikesOutWhatItWouldRemove(t *testing.T) {
	a, scr := newTestApp(t, numbered(60))

	f := openFinderWith(t, a, '!', "ERROR")
	if len(f.rows) == 0 {
		t.Fatal("no matches to draw")
	}

	row := testH - a.finderHeight(testH) + 2 // first result row
	var struck bool
	for _, st := range rowStyles(scr, row) {
		if _, _, attr := st.Decompose(); attr&tcell.AttrStrikeThrough != 0 {
			struck = true
			break
		}
	}
	if !struck {
		t.Error("a line the rule would discard is not struck out")
	}
}

// TestFinderOnlySeesPastTheFilter: a second only rule is a question about
// lines that are currently hidden, so it has to search the whole log.
func TestFinderOnlySeesPastTheFilter(t *testing.T) {
	a, _ := newTestApp(t, numbered(120))

	a.addRule(filter.Only, "line 003", color.RGB{})
	waitFiltered(t, a)
	if a.viewLen() > 2 {
		t.Fatalf("expected a narrow view, got %d lines", a.viewLen())
	}

	f := openFinderWith(t, a, '&', "line 077")
	if len(f.rows) == 0 {
		t.Fatal("an only rule found nothing; it must search past the current filter")
	}
}

// TestFinderNeverRulesStillApplyWhenWidening: lines banished on purpose should
// not come back just because a new only pattern happens to match them.
func TestFinderNeverRulesStillApplyWhenWidening(t *testing.T) {
	a, _ := newTestApp(t, numbered(120))

	a.addRule(filter.Never, "ERROR", color.RGB{})
	waitFiltered(t, a)

	f := openFinderWith(t, a, '&', "line")
	for _, r := range f.rows {
		if strings.Contains(string(r.text), "ERROR") {
			t.Fatalf("offered a line a never rule already discards: %q", r.text)
		}
	}
}

// TestFinderEditRuleReplacesInPlace rather than adding a second rule.
func TestFinderEditRuleReplacesInPlace(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))
	a.addRule(filter.Never, "ERROR", color.RGB{})
	waitFiltered(t, a)

	a.editRule(0)
	if a.find == nil {
		t.Fatal("editing a rule did not open the window")
	}
	if got := string(a.find.buf); got != "ERROR" {
		t.Fatalf("the window opened with %q, want the rule's pattern", got)
	}
	press(a, tcell.KeyCtrlU)
	typeInto(a, "line 01")
	settleFinder(t, a)
	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))

	rules := a.rules.All()
	if len(rules) != 1 {
		t.Fatalf("rules = %d, want the one rule rewritten", len(rules))
	}
	if rules[0].M.Pattern() != "line 01" || rules[0].Action != filter.Never {
		t.Errorf("rule = %v %q, want never \"line 01\"", rules[0].Action, rules[0].M.Pattern())
	}
}

// TestFinderRemembersPatterns across windows, so a pattern typed once can be
// recalled for a different action. The arrows belong to the match list, so
// history is on C-p and C-n as it is at the bottom-row prompt.
func TestFinderRemembersPatterns(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))

	openFinderWith(t, a, 'm', "ERROR")
	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))

	a.key(tcell.NewEventKey(tcell.KeyRune, '!', tcell.ModNone))
	typeInto(a, "half")
	press(a, tcell.KeyCtrlP)
	if got := string(a.find.buf); got != "ERROR" {
		t.Fatalf("C-p recalled %q, want ERROR", got)
	}
	press(a, tcell.KeyCtrlN)
	if got := string(a.find.buf); got != "half" {
		t.Errorf("C-n returned %q, want the half-typed pattern back", got)
	}
}

// TestFinderSearchKeepsItsMode: a search typed as PCRE stays PCRE for n and N.
func TestFinderSearchKeepsItsMode(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))

	a.key(tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone))
	press(a, tcell.KeyCtrlR)
	typeInto(a, `ERROR|WARN`)
	settleFinder(t, a)
	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))

	if a.search.m == nil {
		t.Fatal("no search installed")
	}
	if got := a.search.m.Mode(); got != filter.Regex {
		t.Errorf("the committed search reads as %v, want regex", got)
	}
}

// TestWordSearchIsLiteral: * means this word, not a pattern loosely like it.
func TestWordSearchIsLiteral(t *testing.T) {
	a, _ := newTestApp(t, "alpha beta\nalXpXhXa\n")
	a.draw()

	a.key(tcell.NewEventKey(tcell.KeyRune, '*', tcell.ModNone))
	if a.search.m == nil {
		t.Fatal("* installed no search")
	}
	if got := a.search.m.Mode(); got != filter.Literal {
		t.Fatalf("* searched as %v, want literal", got)
	}
	if a.search.m.Match([]byte("alXpXhXa")) {
		t.Error("* matched a line that merely contains the letters in order")
	}
}

// TestCommandLineRegexPrefix: the bottom row has no key to toggle a mode, so
// it takes one in the pattern.
func TestCommandLineRegexPrefix(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))

	a.runCommandLine(`only re:ERR(OR)?`)
	waitFiltered(t, a)

	rules := a.rules.All()
	if len(rules) != 1 {
		t.Fatalf("rules = %d, want 1", len(rules))
	}
	if got := rules[0].M.Mode(); got != filter.Regex {
		t.Errorf("rule mode = %v, want regex", got)
	}
	if rules[0].M.Pattern() != "ERR(OR)?" {
		t.Errorf("pattern = %q, want the prefix stripped", rules[0].M.Pattern())
	}

	// Without the prefix the same text is a rule about those characters.
	a.runCommandLine(`never ERR(OR)?`)
	rules = a.rules.All()
	if got := rules[1].M.Mode(); got != filter.Literal {
		t.Errorf("rule mode = %v, want literal", got)
	}
}

// TestJumpGoesThereAndLeavesNothingBehind: f is navigation, not a search, so
// it must not disturb the highlight or what n and N are stepping through.
func TestJumpGoesThereAndLeavesNothingBehind(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	a.doSearch("ERROR", false)
	before := a.search

	f := openFinderWith(t, a, 'f', "line 150")
	if len(f.rows) == 0 {
		t.Fatal("no matches")
	}
	want := f.selected().num
	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))

	if a.find != nil {
		t.Error("Enter did not close the window")
	}
	if got := a.anchorSource() + 1; got != want {
		t.Errorf("landed on line %d, want %d", got, want)
	}
	if a.search.pat != before.pat || a.search.m == nil {
		t.Errorf("the jump replaced the search with %q", a.search.pat)
	}
	if a.rules.Len() != 0 {
		t.Error("the jump left a rule behind")
	}
}

// TestJumpIsFuzzy, because you are reaching for a line you half remember.
func TestJumpIsFuzzy(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))

	f := openFinderWith(t, a, 'f', "ERbd")
	if f.mode != filter.Fuzzy {
		t.Fatalf("jump mode = %v, want fuzzy", f.mode)
	}
	if len(f.rows) == 0 {
		t.Error("a fuzzy jump should reach ERROR bad through the gaps")
	}
	for _, r := range f.rows {
		if !strings.Contains(string(r.text), "ERROR bad") {
			t.Errorf("matched an unrelated line: %q", r.text)
		}
	}
}

// TestJumpWrapsToTheTop: a jump is the one window where what you want may well
// be behind you, and there is no n to carry on with.
func TestJumpWrapsToTheTop(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	a.gotoSourceLine(150)
	a.draw()

	// "line 007" is the eighth line: the text counts from zero.
	f := openFinderWith(t, a, 'f', "line 007")
	nums := rowNums(f)
	if len(nums) != 1 || nums[0] != 8 {
		t.Fatalf("matches = %v, want line 8 from above the cursor", nums)
	}
	if !f.wrapped {
		t.Error("the search did not record that it came round again")
	}

	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))
	if got := a.anchorSource() + 1; got != 8 {
		t.Errorf("landed on line %d, want 8", got)
	}
}

// TestJumpPrefersWhatIsAhead: wrapping is a fallback, not the starting point.
func TestJumpPrefersWhatIsAhead(t *testing.T) {
	a, _ := newTestApp(t, numbered(400))
	a.gotoSourceLine(200)
	a.draw()

	f := openFinderWith(t, a, 'f', "ERROR")
	nums := rowNums(f)
	if len(nums) == 0 {
		t.Fatal("no matches")
	}
	if nums[0] < 200 {
		t.Errorf("first match is line %d, above the cursor at 200", nums[0])
	}
	if f.wrapped {
		t.Error("wrapped while there were still matches ahead")
	}
}

// TestJumpStopsWhereItStarted rather than going round for ever.
func TestJumpStopsWhereItStarted(t *testing.T) {
	a, _ := newTestApp(t, numbered(300))
	a.gotoSourceLine(150)
	a.draw()

	f := openFinderWith(t, a, 'f', "nothing matches this")
	if !f.atEnd {
		t.Fatal("the search did not finish")
	}
	if len(f.rows) != 0 {
		t.Fatalf("matched %d lines, want none", len(f.rows))
	}
	// The whole file, read once: no line is offered twice.
	if f.scanned > 300 {
		t.Errorf("read %d lines of a 300-line file", f.scanned)
	}
}

// TestFinderArrowsMoveTheSelection, which is what the list is for.
func TestFinderArrowsMoveTheSelection(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))

	f := openFinderWith(t, a, '/', "ERROR")
	if len(f.rows) < 3 {
		t.Fatalf("only %d matches, too few to move through", len(f.rows))
	}
	press(a, tcell.KeyDown)
	press(a, tcell.KeyDown)
	if f.sel != 2 {
		t.Fatalf("selection = %d after two Downs, want 2", f.sel)
	}
	press(a, tcell.KeyUp)
	if f.sel != 1 {
		t.Fatalf("selection = %d after Up, want 1", f.sel)
	}
	if got := string(f.buf); got != "ERROR" {
		t.Errorf("moving through the list edited the pattern to %q", got)
	}
}
