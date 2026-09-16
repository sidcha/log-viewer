package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/highlight"
)

// TestEscapesTakeNoColumns is what "the file opens correctly" means: the text
// lines up exactly as it would with the escapes removed.
func TestEscapesTakeNoColumns(t *testing.T) {
	a, scr := newTestApp(t, "\x1b[31mred\x1b[0m and \x1b]0;a title\x07plain\n")
	a.draw()

	if got := strings.TrimRight(rowText(scr, 0), " "); got != "red and plain" {
		t.Errorf("row 0 = %q, want the text without its escapes", got)
	}
}

// TestTrailingCarriageReturnIsNotDrawn: a log captured over a serial link ends
// every line with one, and it is the line ending, not a character.
func TestTrailingCarriageReturnIsNotDrawn(t *testing.T) {
	a, scr := newTestApp(t, "one\r\ntwo\r\n")
	a.draw()

	for row, want := range []string{"one", "two"} {
		if got := strings.TrimRight(rowText(scr, row), " "); got != want {
			t.Errorf("row %d = %q, want %q", row, got, want)
		}
	}
}

// TestCarriageReturnInsideALineIsStillShown, because there it is not a line
// ending but something the producer did, and hiding it would silently join
// text that never appeared together.
func TestCarriageReturnInsideALineIsStillShown(t *testing.T) {
	a, scr := newTestApp(t, "a\rb\n")
	a.draw()

	if got := strings.TrimRight(rowText(scr, 0), " "); got != "a·b" {
		t.Errorf("row 0 = %q, want the interior CR marked", got)
	}
}

func TestANSIColoursTheText(t *testing.T) {
	a, scr := newTestApp(t, "plain \x1b[31mred\x1b[0m plain\n")
	a.draw()

	st := rowStyles(scr, 0)
	if got := fg(st[0]); got == tcell.PaletteColor(1) {
		t.Error("the text before the sequence was coloured")
	}
	for i, x := range []int{6, 7, 8} { // r, e, d
		if got := fg(st[x]); got != tcell.PaletteColor(1) {
			t.Errorf("column %d (of red, char %d) = %v, want palette 1", x, i, got)
		}
	}
	if got := fg(st[10]); got == tcell.PaletteColor(1) {
		t.Error("the colour ran past the reset")
	}
}

// TestANSIOffLeavesTheTextAlone, for anyone whose log is full of sequences
// they would rather not see honoured.
func TestANSIOffLeavesTheTextAlone(t *testing.T) {
	a, scr := newTestApp(t, "plain \x1b[31mred\x1b[0m plain\n")
	if err := a.cfg.Set("ansi", "off"); err != nil {
		t.Fatal(err)
	}
	a.draw()

	if got := fg(rowStyles(scr, 0)[6]); got == tcell.PaletteColor(1) {
		t.Error("colours were drawn with ansi off")
	}
	// The escapes are still hidden: they are not text under any setting.
	if got := strings.TrimRight(rowText(scr, 0), " "); got != "plain red plain" {
		t.Errorf("row 0 = %q", got)
	}
}

// TestTheLineSColoursWinOverTheSyntaxProfile: lv's profile is a guess at the
// shape of the line, and the producer's colours are not a guess. Where the two
// cover the same bytes, what the file says wins.
//
// The profile here is built for the test rather than taken from the shipped
// ones, whose rules mostly stop matching next to an escape sequence at all:
// they are written with \b, and the m that ends a colour sequence is a word
// character, so \bERROR does not match [35mERROR.
func TestTheLineSColoursWinOverTheSyntaxProfile(t *testing.T) {
	a, scr := newTestApp(t, "plain \x1b[35mthing\x1b[0m\n")
	syntax := tcell.StyleDefault.Foreground(tcell.ColorGreen)
	a.eng.Profile = &highlight.Profile{
		Name:  "test",
		Rules: []highlight.Rule{{Re: regexp.MustCompile(`thing`), Style: syntax}},
	}
	a.draw()

	st := rowStyles(scr, 0)
	if got := fg(st[0]); got == tcell.PaletteColor(5) {
		t.Error("the colour reached text the line never coloured")
	}
	if got := fg(st[6]); got != tcell.PaletteColor(5) {
		t.Errorf("the coloured word is %v, want the magenta the line asked for", got)
	}
}

// TestTheSyntaxProfileKeepsWhatTheColoursDoNotCover, so a line the producer
// coloured only in part is not left half styled.
func TestTheSyntaxProfileKeepsWhatTheColoursDoNotCover(t *testing.T) {
	a, scr := newTestApp(t, "plain \x1b[35mthing\x1b[0m\n")
	syntax := tcell.StyleDefault.Foreground(tcell.ColorGreen)
	a.eng.Profile = &highlight.Profile{
		Name: "test",
		Rules: []highlight.Rule{
			{Re: regexp.MustCompile(`plain`), Style: syntax},
			{Re: regexp.MustCompile(`thing`), Style: syntax},
		},
	}
	a.draw()

	st := rowStyles(scr, 0)
	if got := fg(st[0]); got != tcell.ColorGreen {
		t.Errorf("the uncoloured word is %v, want the profile's green", got)
	}
	if got := fg(st[6]); got != tcell.PaletteColor(5) {
		t.Errorf("the coloured word is %v, want magenta", got)
	}
}

// TestRulesWinOverTheLineSColours: what the user is hunting for has to be
// findable in a log that is already painted end to end.
func TestRulesWinOverTheLineSColours(t *testing.T) {
	a, scr := newTestApp(t, "\x1b[31mred needle here\x1b[0m\n")
	a.addRule(filter.Highlight, "needle", filter.Color{R: 0x00, G: 0xff, B: 0xff})
	a.draw()

	st := rowStyles(scr, 0)
	before, on := fg(st[0]), fg(st[4]) // "red " then "needle"
	if before == on {
		t.Errorf("the rule did not paint over the line's colour: %v", on)
	}
	if before != tcell.PaletteColor(1) {
		t.Errorf("the rest of the line lost its colour: %v", before)
	}
}

// TestSearchWinsOverTheLineSColours, for the same reason.
func TestSearchWinsOverTheLineSColours(t *testing.T) {
	a, scr := newTestApp(t, "\x1b[31mred needle here\x1b[0m\n")
	a.doSearch("needle", false)
	a.draw()

	want, _, _ := a.cfg.Style("match").Decompose()
	if got := fg(rowStyles(scr, 0)[4]); got != want {
		t.Errorf("match colour = %v, want %v", got, want)
	}
}

// TestHighlightSpansLandOnTheRightColumns: the escape bytes are still in the
// line, so a match reported at a byte offset has to be mapped past them.
func TestHighlightSpansLandOnTheRightColumns(t *testing.T) {
	a, scr := newTestApp(t, "\x1b[32mabc\x1b[0m needle\n")
	a.doSearch("needle", false)
	a.draw()

	match, _, _ := a.cfg.Style("match").Decompose()
	st := rowStyles(scr, 0)
	for x := 4; x < 10; x++ { // "needle" begins at column 4 on screen
		if got := fg(st[x]); got != match {
			t.Fatalf("column %d = %v, want the match colour %v", x, got, match)
		}
	}
	if got := fg(st[3]); got == match {
		t.Error("the match started a column early")
	}
}
