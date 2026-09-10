package filter

import (
	"fmt"
	"strings"
	"testing"
)

func mustCompile(t *testing.T, pat string, c Case) Matcher {
	t.Helper()
	return mustCompileMode(t, pat, Literal, c)
}

func mustCompileMode(t *testing.T, pat string, mode Mode, c Case) Matcher {
	t.Helper()
	m, err := Compile(pat, mode, c)
	if err != nil {
		t.Fatalf("Compile(%q, %v): %v", pat, mode, err)
	}
	return m
}

// TestCompileHonoursTheRequestedMode: the mode is what the caller asked for,
// never inferred from the spelling of the pattern.
func TestCompileHonoursTheRequestedMode(t *testing.T) {
	// A pattern full of metacharacters still compiles to a plain substring.
	lit := mustCompileMode(t, `err(or)?`, Literal, Sensitive)
	if _, ok := lit.(*literal); !ok {
		t.Errorf("literal mode compiled to %T, want the substring matcher", lit)
	}
	if !lit.Match([]byte("err(or)? happened")) || lit.Match([]byte("error happened")) {
		t.Error("literal mode should match the pattern's characters, not interpret them")
	}

	for _, tc := range []struct {
		mode Mode
		want Mode
	}{{Literal, Literal}, {Fuzzy, Fuzzy}, {Regex, Regex}} {
		if got := mustCompileMode(t, "abc", tc.mode, Sensitive).Mode(); got != tc.want {
			t.Errorf("Compile(%v).Mode() = %v", tc.mode, got)
		}
	}
}

func TestSmartCase(t *testing.T) {
	lower := mustCompile(t, "error", Smart)
	if !lower.Match([]byte("fatal ERROR here")) {
		t.Error("an all-lower-case pattern should be case-insensitive under smart case")
	}

	upper := mustCompile(t, "ERROR", Smart)
	if upper.Match([]byte("an error occurred")) {
		t.Error("a pattern with upper case should be case-sensitive under smart case")
	}
	if !upper.Match([]byte("an ERROR occurred")) {
		t.Error("exact case should still match")
	}
}

func TestExplicitCaseModes(t *testing.T) {
	if !mustCompile(t, "ERROR", Insensitive).Match([]byte("error")) {
		t.Error("insensitive should ignore case")
	}
	if mustCompile(t, "error", Sensitive).Match([]byte("ERROR")) {
		t.Error("sensitive should respect case")
	}
}

// TestFoldedSearchMatchesReference cross-checks the vectorised fold scan
// against the obvious-but-allocating implementation.
func TestFoldedSearchMatchesReference(t *testing.T) {
	pats := []string{"a", "ab", "xyz", "error", "]", "Zz"}
	subjects := []string{
		"", "a", "A", "bab", "BAB", "xxxyzxxx", "XYZ",
		"an Error occurred", "no match at all", "]bracket[", "zZzZ",
		strings.Repeat("ab", 40) + "XYZ",
	}
	for _, p := range pats {
		m := newLiteral(p, true)
		lp := strings.ToLower(p)
		for _, s := range subjects {
			want := strings.Contains(strings.ToLower(s), lp)
			if got := m.Match([]byte(s)); got != want {
				t.Errorf("fold Match(%q, %q) = %v, want %v", p, s, got, want)
			}
		}
	}
}

func TestFindReturnsEveryOccurrence(t *testing.T) {
	m := mustCompile(t, "ab", Sensitive)
	got := m.Find([]byte("abXabYab"), nil, 0)
	want := [][2]int{{0, 2}, {3, 5}, {6, 8}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("Find = %v, want %v", got, want)
	}

	// Overlapping folded matches advance past the previous hit.
	mf := newLiteral("aa", true)
	if got := mf.Find([]byte("aAaA"), nil, 0); len(got) != 2 {
		t.Fatalf("folded Find = %v, want 2 matches", got)
	}
}
