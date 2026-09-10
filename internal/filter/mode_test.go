package filter

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestParseModeRoundTrip(t *testing.T) {
	for _, m := range []Mode{Literal, Fuzzy, Regex} {
		got, err := ParseMode(m.String())
		if err != nil || got != m {
			t.Errorf("ParseMode(%q) = %v, %v", m.String(), got, err)
		}
	}
	for _, s := range []string{"", "plain", "substring", "REGEX", "pcre", "regexp"} {
		if _, err := ParseMode(s); err != nil {
			t.Errorf("ParseMode(%q): %v", s, err)
		}
	}
	if _, err := ParseMode("glob"); err == nil {
		t.Error("an unknown mode name was accepted")
	}
}

func TestFuzzyMatchesInOrderWithGaps(t *testing.T) {
	m := mustCompileMode(t, "err", Fuzzy, Sensitive)
	for _, s := range []string{"err", "e r r", "the error", "eXXrXXr"} {
		if !m.Match([]byte(s)) {
			t.Errorf("fuzzy %q should match %q", "err", s)
		}
	}
	for _, s := range []string{"", "er", "rre", "e r"} {
		if m.Match([]byte(s)) {
			t.Errorf("fuzzy %q should not match %q", "err", s)
		}
	}
}

func TestFuzzyIsCaseFoldedBySmartCase(t *testing.T) {
	if !mustCompileMode(t, "err", Fuzzy, Smart).Match([]byte("an ERROR")) {
		t.Error("an all-lower-case fuzzy pattern should fold")
	}
	if mustCompileMode(t, "ERR", Fuzzy, Smart).Match([]byte("an error")) {
		t.Error("an upper-case fuzzy pattern should stay case sensitive")
	}
}

// TestFuzzyFindMarksTheCharactersItUsed, so the window can show where the
// pattern landed rather than just that it did.
func TestFuzzyFindMarksTheCharactersItUsed(t *testing.T) {
	m := mustCompileMode(t, "ac", Fuzzy, Sensitive)
	got := m.Find([]byte("abc"), nil, 0)
	want := [][2]int{{0, 1}, {2, 3}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("Find = %v, want %v", got, want)
	}

	// A run that does not complete is not a match, and marks nothing.
	if got := m.Find([]byte("abb"), nil, 0); len(got) != 0 {
		t.Fatalf("Find on a non-match = %v, want nothing", got)
	}
}

// TestFuzzyFindHandlesMultibyteRunes: positions are byte offsets into the line,
// because that is what the renderer measures in.
func TestFuzzyFindHandlesMultibyteRunes(t *testing.T) {
	line := []byte("αβγ x")
	m := mustCompileMode(t, "γx", Fuzzy, Sensitive)
	got := m.Find(line, nil, 0)
	if len(got) != 2 {
		t.Fatalf("Find = %v, want two positions", got)
	}
	if s := string(line[got[0][0]:got[0][1]]); s != "γ" {
		t.Errorf("first position covers %q, want γ", s)
	}
	if s := string(line[got[1][0]:got[1][1]]); s != "x" {
		t.Errorf("second position covers %q, want x", s)
	}
}

// TestRegexUsesPCRESyntax: lookaround, backreferences and lazy quantifiers are
// the reason the regex mode is not RE2.
func TestRegexUsesPCRESyntax(t *testing.T) {
	cases := []struct {
		pat  string
		line string
		want bool
	}{
		{`foo(?=bar)`, "foobar", true},
		{`foo(?=bar)`, "foobaz", false},
		{`foo(?!bar)`, "foobaz", true},
		{`(?<=id=)\d+`, "id=42", true},
		{`(\w+) \1`, "ho ho", true},
		{`(\w+) \1`, "ho hum", false},
		{`<.+?>`, "<a><b>", true},
	}
	for _, c := range cases {
		m := mustCompileMode(t, c.pat, Regex, Sensitive)
		if got := m.Match([]byte(c.line)); got != c.want {
			t.Errorf("regex %q on %q = %v, want %v", c.pat, c.line, got, c.want)
		}
	}
}

func TestRegexRejectsABadPattern(t *testing.T) {
	if _, err := Compile("ERR(", Regex, Sensitive); err == nil {
		t.Error("an unbalanced group compiled")
	}
	// The same text is perfectly good as a literal.
	if m, err := Compile("ERR(", Literal, Sensitive); err != nil {
		t.Errorf("literal: %v", err)
	} else if !m.Match([]byte("ERR( happened")) {
		t.Error("literal did not match its own characters")
	}
}

func TestRegexFindReportsByteRanges(t *testing.T) {
	m := mustCompileMode(t, `\d+`, Regex, Sensitive)
	line := []byte("αβ 12 γ 345")
	got := m.Find(line, nil, 0)
	if len(got) != 2 {
		t.Fatalf("Find = %v, want two matches", got)
	}
	if s := string(line[got[0][0]:got[0][1]]); s != "12" {
		t.Errorf("first match = %q, want 12", s)
	}
	if s := string(line[got[1][0]:got[1][1]]); s != "345" {
		t.Errorf("second match = %q, want 345", s)
	}

	// The limit bounds how much work drawing one line can cost.
	if got := m.Find([]byte("1 2 3 4 5"), nil, 2); len(got) != 2 {
		t.Errorf("Find with limit 2 returned %d matches", len(got))
	}
}

// TestRegexEmptyMatchTerminates: an expression that can match nothing must not
// spin at the end of the line.
func TestRegexEmptyMatchTerminates(t *testing.T) {
	m := mustCompileMode(t, `x*`, Regex, Sensitive)
	done := make(chan [][2]int, 1)
	go func() { done <- m.Find([]byte("abxxc"), nil, 0) }()
	select {
	case got := <-done:
		if len(got) != 1 {
			t.Errorf("Find = %v, want the one non-empty match", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Find did not terminate on a pattern that can match nothing")
	}
}

// TestRegexBacktrackingIsBounded: a backtracking engine reading lines nobody
// wrote for it needs a leash, and a timed-out line is simply not a match.
func TestRegexBacktrackingIsBounded(t *testing.T) {
	m := mustCompileMode(t, `(a+)+$`, Regex, Sensitive)
	line := []byte(strings.Repeat("a", 40) + "b")

	start := time.Now()
	if m.Match(line) {
		t.Error("the pattern should not match")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("a pathological match took %s; the timeout did not hold", d)
	}
}

func TestRegexIsCaseFoldedBySmartCase(t *testing.T) {
	if !mustCompileMode(t, "err..", Regex, Smart).Match([]byte("an ERROR")) {
		t.Error("an all-lower-case regex should fold under smart case")
	}
	if mustCompileMode(t, "ERR..", Regex, Smart).Match([]byte("an error")) {
		t.Error("an upper-case regex should stay case sensitive")
	}
}
