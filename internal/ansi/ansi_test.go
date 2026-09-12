package ansi

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func spans(line string) []Span { return Spans([]byte(line), nil) }

// text is the part of line a span covers, which is the property everything
// downstream depends on: a span that included its own escape bytes would paint
// the wrong columns.
func text(line string, s Span) string { return line[s.Start:s.End] }

func TestSpansCoversOnlyTheStyledText(t *testing.T) {
	line := "plain \x1b[31mred\x1b[0m plain"
	got := spans(line)
	if len(got) != 1 {
		t.Fatalf("got %d spans, want 1: %+v", len(got), got)
	}
	if s := text(line, got[0]); s != "red" {
		t.Errorf("span covers %q, want \"red\"", s)
	}
	if got[0].Attrs.Fg != tcell.PaletteColor(1) {
		t.Errorf("fg = %v, want palette 1", got[0].Attrs.Fg)
	}
}

func TestSpansRunsToTheEndOfTheLine(t *testing.T) {
	// Plenty of producers never reset: the newline does it for them.
	line := "\x1b[32mgreen to the end"
	got := spans(line)
	if len(got) != 1 {
		t.Fatalf("got %d spans, want 1", len(got))
	}
	if s := text(line, got[0]); s != "green to the end" {
		t.Errorf("span covers %q", s)
	}
}

func TestSpansIgnoresALineWithoutEscapes(t *testing.T) {
	if got := spans("nothing to see here"); len(got) != 0 {
		t.Errorf("got %+v, want no spans", got)
	}
	// Nor for a sequence that only turns everything off again.
	if got := spans("\x1b[0mplain"); len(got) != 0 {
		t.Errorf("got %+v, want no spans", got)
	}
}

func TestAttributesAccumulateAndReset(t *testing.T) {
	line := "\x1b[1ma\x1b[4mb\x1b[22mc\x1b[0md"
	got := spans(line)
	want := []struct {
		text      string
		bold, und bool
	}{
		{"a", true, false},
		{"b", true, true},
		{"c", false, true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d spans, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if s := text(line, got[i]); s != w.text {
			t.Errorf("span %d covers %q, want %q", i, s, w.text)
		}
		if got[i].Attrs.Bold != w.bold || got[i].Attrs.Underline != w.und {
			t.Errorf("span %d = %+v, want bold=%v underline=%v", i, got[i].Attrs, w.bold, w.und)
		}
	}
}

func TestColourForms(t *testing.T) {
	cases := []struct {
		name string
		seq  string
		want tcell.Color
		bg   bool
	}{
		{"basic fg", "\x1b[31m", tcell.PaletteColor(1), false},
		{"bright fg", "\x1b[91m", tcell.PaletteColor(9), false},
		{"basic bg", "\x1b[44m", tcell.PaletteColor(4), true},
		{"bright bg", "\x1b[104m", tcell.PaletteColor(12), true},
		{"256 fg", "\x1b[38;5;208m", tcell.PaletteColor(208), false},
		{"256 bg", "\x1b[48;5;17m", tcell.PaletteColor(17), true},
		{"256 fg colons", "\x1b[38:5:208m", tcell.PaletteColor(208), false},
		{"rgb fg", "\x1b[38;2;10;20;30m", tcell.NewRGBColor(10, 20, 30), false},
		{"rgb bg", "\x1b[48;2;10;20;30m", tcell.NewRGBColor(10, 20, 30), true},
		{"rgb colons", "\x1b[38:2:10:20:30m", tcell.NewRGBColor(10, 20, 30), false},
		{"rgb colons with colour space", "\x1b[38:2::10:20:30m", tcell.NewRGBColor(10, 20, 30), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := spans(c.seq + "x")
			if len(got) != 1 {
				t.Fatalf("got %d spans", len(got))
			}
			have := got[0].Attrs.Fg
			if c.bg {
				have = got[0].Attrs.Bg
			}
			if have != c.want {
				t.Errorf("colour = %v, want %v", have, c.want)
			}
		})
	}
}

// TestSemicolonRGBIsNotReadAsColonForm: 38;2;0;255;0 followed by another
// parameter has six fields, but the first component is still the red one.
func TestSemicolonRGBIsNotReadAsColonForm(t *testing.T) {
	got := spans("\x1b[38;2;0;255;0;1mx")
	if len(got) != 1 {
		t.Fatalf("got %d spans", len(got))
	}
	if want := tcell.NewRGBColor(0, 255, 0); got[0].Attrs.Fg != want {
		t.Errorf("fg = %v, want %v", got[0].Attrs.Fg, want)
	}
	if !got[0].Attrs.Bold {
		t.Error("the parameter after the colour was dropped")
	}
}

// TestApplyOnlyAddsToAStyle: a sequence that asks for bold has said nothing
// about colour, so what lv decided for those bytes must survive.
func TestApplyOnlyAddsToAStyle(t *testing.T) {
	base := tcell.StyleDefault.Foreground(tcell.ColorGreen).Italic(true)
	st := Attrs{Bold: true}.Apply(base)

	fg, _, attr := st.Decompose()
	if fg != tcell.ColorGreen {
		t.Errorf("fg = %v, want the base's green", fg)
	}
	if attr&tcell.AttrBold == 0 {
		t.Error("bold was not applied")
	}
	if attr&tcell.AttrItalic == 0 {
		t.Error("the base's italic was cleared")
	}

	// A colour in the sequence does replace one in the base.
	st = Attrs{Fg: tcell.PaletteColor(1)}.Apply(base)
	if fg, _, _ := st.Decompose(); fg != tcell.PaletteColor(1) {
		t.Errorf("fg = %v, want the sequence's colour", fg)
	}
}

func TestSkipRecognisesTheRepertoire(t *testing.T) {
	cases := map[string]string{
		"CSI SGR":          "\x1b[1;31m",
		"CSI cursor":       "\x1b[2J",
		"CSI with params":  "\x1b[?25l",
		"CSI intermediate": "\x1b[0 q",
		"OSC BEL":          "\x1b]0;a window title\x07",
		"OSC ST":           "\x1b]8;;https://example.com\x1b\\",
		"DCS":              "\x1bP0;1|17/ab\x1b\\",
		"charset":          "\x1b(B",
		"single":           "\x1bc",
	}
	for name, seq := range cases {
		t.Run(name, func(t *testing.T) {
			line := []byte(seq + "after")
			n, ok := Skip(line, 0)
			if !ok {
				t.Fatalf("not recognised: %q", seq)
			}
			if got := string(line[n:]); got != "after" {
				t.Errorf("skipped to %q, want \"after\"", got)
			}
		})
	}
}

// TestSkipRefusesAnUnfinishedSequence, so a lone escape byte at the end of a
// line does not swallow the rest of it.
func TestSkipRefusesAnUnfinishedSequence(t *testing.T) {
	for _, s := range []string{"\x1b", "\x1b[", "\x1b[31", "\x1b]0;no terminator"} {
		if n, ok := Skip([]byte(s), 0); ok {
			t.Errorf("%q was accepted, skipping to %d", s, n)
		}
	}
}

// TestStrayEscapeDoesNotEndTheColour: ESC followed by a letter is a two-byte
// escape and is hidden, but it says nothing about colour, so the text after it
// keeps what was in force and stays one run.
func TestStrayEscapeDoesNotEndTheColour(t *testing.T) {
	line := "\x1b[31mred\x1bstill red"
	got := spans(line)
	if len(got) != 1 {
		t.Fatalf("got %d spans, want one unbroken run: %+v", len(got), got)
	}
	if got[0].Attrs.Fg != tcell.PaletteColor(1) {
		t.Errorf("colour lost: %+v", got[0].Attrs)
	}
	if s := text(line, got[0]); !strings.HasSuffix(s, "till red") {
		t.Errorf("span covers %q, want it to reach the end of the line", s)
	}
}

// TestSpansAreNotSplitByEscapesThatChangeNothing, which serial output is full
// of: cursor moves, titles, bracketed-paste toggles.
func TestSpansAreNotSplitByEscapesThatChangeNothing(t *testing.T) {
	got := spans("\x1b[31mone\x1b[2Ktwo\x1b]0;title\x07three")
	if len(got) != 1 {
		t.Fatalf("got %d spans, want 1: %+v", len(got), got)
	}
}

// TestUnfinishedEscapeIsContent: a line that ends mid-sequence must not lose
// the escape byte into nowhere.
func TestUnfinishedEscapeIsContent(t *testing.T) {
	line := "\x1b[31mred\x1b"
	got := spans(line)
	if len(got) != 1 {
		t.Fatalf("got %d spans: %+v", len(got), got)
	}
	if s := text(line, got[0]); s != "red\x1b" {
		t.Errorf("span covers %q, want the trailing escape kept as content", s)
	}
}

func TestSpansReuseTheBuffer(t *testing.T) {
	buf := make([]Span, 0, 8)
	for i := 0; i < 3; i++ {
		buf = Spans([]byte("\x1b[31ma\x1b[32mb"), buf[:0])
		if len(buf) != 2 {
			t.Fatalf("pass %d: got %d spans", i, len(buf))
		}
	}
}

func TestSpansDoesNotAllocate(t *testing.T) {
	line := []byte("\x1b[1;31mERROR\x1b[0m something \x1b[38;5;208mwarned\x1b[0m")
	buf := Spans(line, nil)
	n := testing.AllocsPerRun(50, func() { buf = Spans(line, buf[:0]) })
	if n > 0 {
		t.Errorf("Spans allocates %v times per line; drawing must not", n)
	}
}

func TestSpansSurvivesGarbage(t *testing.T) {
	// Whatever a serial line coughs up, it must not hang or panic.
	inputs := []string{
		"\x1b", "\x1b[", "\x1b[[[[", "\x1b[999999999999999999999m",
		"\x1b[;;;;m", "\x1b[m", "\x1b[38;5m", "\x1b[38;2;1m", "\x1b[48;5;999m",
		"\x1b]", "\x1b]0;", "\x1bP", strings.Repeat("\x1b[31m", 100),
		"\x00\x01\x02\x1b[31m\xff\xfe",
	}
	for _, in := range inputs {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
			_ = Spans([]byte(in), nil)
		})
	}
}
