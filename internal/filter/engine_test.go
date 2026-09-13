package filter

import (
	"fmt"
	"testing"

	"github.com/dlclark/regexp2"
)

// TestRegexEngineChoice: patterns RE2 can run go to RE2; the rest still work.
func TestRegexEngineChoice(t *testing.T) {
	for pat, wantRE2 := range map[string]bool{
		`\d+ ERROR`:       true,
		`[[:alpha:]]+=`:   true,
		`timeout|refused`: true,
		`foo(?=bar)`:      false,
		`(\w+) \1`:        false,
		`(?<=id=)\d+`:     false,
	} {
		m := mustCompileMode(t, pat, Regex, Sensitive)
		if _, isRE2 := m.(*re2Matcher); isRE2 != wantRE2 {
			t.Errorf("%q ran on RE2 = %v, want %v", pat, isRE2, wantRE2)
		}
		if m.Mode() != Regex || m.Pattern() != pat {
			t.Errorf("%q reports mode %v pattern %q", pat, m.Mode(), m.Pattern())
		}
	}
}

// TestRegexEnginesAgree: which engine runs a pattern must not change what it
// matches, including on text that is not ASCII, where \w, \d and \b are
// easiest to read differently.
func TestRegexEnginesAgree(t *testing.T) {
	patterns := []string{
		`\d+`, `\w+`, `\bERR\b`, `\s+`, `^\S+`, `[[:upper:]]{3}`, `x$`, `^$`,
		`a.c`, `(?i)straße`, `[^a-z ]+`, `\pL+`, `é+`, `\W`, `\D\d`,
	}
	lines := []string{
		"", "ERR 42 ok", "café au lait 12", "naïve ERRor", "x", "ends with x",
		"数字 ٣ and 3", "tab\there", "STRASSE straße", "a c abc",
		"__init__ 0x1F", "ERRé ERR", " wide space",
	}
	for _, pat := range patterns {
		for _, fold := range []bool{false, true} {
			m, err := newRegex(pat, fold)
			if err != nil {
				t.Fatalf("%q: %v", pat, err)
			}
			if _, ok := m.(*re2Matcher); !ok {
				t.Fatalf("%q did not choose RE2; the comparison would be empty", pat)
			}
			opts := regexp2.RegexOptions(regexp2.RE2)
			if fold {
				opts |= regexp2.IgnoreCase
			}
			back := &pcreMatcher{re: regexp2.MustCompile(pat, opts), raw: pat}
			for _, line := range lines {
				b := []byte(line)
				if got, want := m.Match(b), back.Match(b); got != want {
					t.Errorf("%q fold=%v on %q: RE2 %v, backtracking %v", pat, fold, line, got, want)
				}
				if got, want := fmt.Sprint(m.Find(b, nil, 0)), fmt.Sprint(back.Find(b, nil, 0)); got != want {
					t.Errorf("%q fold=%v Find on %q: RE2 %s, backtracking %s", pat, fold, line, got, want)
				}
			}
		}
	}
}
