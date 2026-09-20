package config

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/highlight"
)

// styleAt reports the style the named built-in profile paints on the first
// byte of want inside line, and whether any rule covered it. Later spans win,
// as they do on screen.
func styleAt(t *testing.T, profile, line, want string) (tcell.Style, bool) {
	t.Helper()
	c, err := Load(LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	p := c.findProfile(profile)
	if p == nil {
		t.Fatalf("no profile %s", profile)
	}
	i := strings.Index(line, want)
	if i < 0 {
		t.Fatalf("%q not in %q", want, line)
	}
	e := highlight.NewEngine(0, 0)
	var st tcell.Style
	covered := false
	for _, sp := range e.SyntaxSpans(p, []byte(line)) {
		if sp.Start <= i && i < sp.End {
			st, covered = sp.Style, true
		}
	}
	return st, covered
}

func builtinStyle(t *testing.T, name string) tcell.Style {
	t.Helper()
	c, err := Load(LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	return c.Style(name)
}

func TestCommonColoursTokensEveryLogShares(t *testing.T) {
	cases := []struct{ line, token, style string }{
		{"2026-09-23 12:34:56 ERROR disk on fire", "ERROR", "syntax.level.error"},
		{"12:00:01 WARNING low memory", "WARNING", "syntax.level.warn"},
		{"[err] bus reset", "err", "syntax.level.error"},
		{"<wrn> retry", "wrn", "syntax.level.warn"},
		{"ts=1 level=warn msg=x", "warn", "syntax.level.warn"},
		{"link fe80::1 up", "fe80::1", "syntax.ip"},
		{"addr 2001:db8::1 up", "2001:db8::1", "syntax.ip"},
		{"ll fe80::1ff:fe23:4567:890a x", "fe80::1ff:fe23:4567:890a", "syntax.ip"},
		{"from 10.0.0.1:22 ok", "10.0.0.1", "syntax.ip"},
		{"at 12:34:56 now", "12:34:56", "syntax.time"},
		{"hw 00:11:22:33:44:55 up", "00:11:22:33:44:55", "syntax.host"},
		{"open /var/log/syslog now", "/var/log/syslog", "syntax.path"},
		{"see https://example.com/x now", "https://example.com/x", "syntax.path"},
		{"id 123e4567-e89b-12d3-a456-426614174000 x", "123e4567", "syntax.uuid"},
		{"\tat com.x.Y.m(Y.java:12)", "at com", "syntax.trace"},
		{"Traceback (most recent call last):", "Traceback", "syntax.trace"},
		{"goroutine 7 [running]:", "goroutine", "syntax.trace"},
	}
	for _, tc := range cases {
		st, ok := styleAt(t, "generic", tc.line, tc.token)
		if !ok || st != builtinStyle(t, tc.style) {
			t.Errorf("%q in %q: covered=%v, want %s", tc.token, tc.line, ok, tc.style)
		}
	}
}

// A level word in the middle of a message is prose, not a level.
func TestCommonLeavesLevelWordsInProseAlone(t *testing.T) {
	for _, tc := range []struct{ line, word string }{
		{"retrying after error on eth0", "error"},
		{"the job failed quietly", "failed"},
		{"a warning was printed", "warning"},
	} {
		st, ok := styleAt(t, "generic", tc.line, tc.word)
		for _, lvl := range []string{"debug", "info", "notice", "warn", "error", "fatal"} {
			if ok && st == builtinStyle(t, "syntax.level."+lvl) {
				t.Errorf("%q in %q is coloured as a %s level", tc.word, tc.line, lvl)
			}
		}
	}
}

func TestCommonLeavesAPathlessSlashAlone(t *testing.T) {
	if st, ok := styleAt(t, "generic", "ratio a/b here", "/b"); ok && st == builtinStyle(t, "syntax.path") {
		t.Error("the /b of a/b was coloured as a path")
	}
}

// C++ and Rust scope resolution (a::b, std::vector) looks exactly like the
// :: shorthand for a run of zeroed IPv6 groups, but carries no address.
func TestCommonLeavesScopePathsAlone(t *testing.T) {
	for _, tc := range []struct{ line, token string }{
		{"call a::b now", "a::b"},
		{"path a::b::c done", "a::b::c"},
		{"x Ok::Err y", "Ok::Err"},
		{"use std::vector here", "std::vector"},
	} {
		if st, ok := styleAt(t, "generic", tc.line, tc.token); ok && st == builtinStyle(t, "syntax.ip") {
			t.Errorf("%q in %q was coloured as an IP address", tc.token, tc.line)
		}
	}
}

func TestShippedProfilesUseCommon(t *testing.T) {
	c, err := Load(LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"generic", "kernel", "syslog", "iso", "json"} {
		p := c.findProfile(name)
		if p == nil || len(p.Items) == 0 || p.Items[0].Use != "common" {
			t.Errorf("profile %s does not start with use common", name)
		}
	}
}

// JSON numbers carry a sign and an exponent, which common's numbers do not.
func TestJSONColoursWholeNumbers(t *testing.T) {
	const line = `{"a": -42, "b": 1.5e10}`
	want := builtinStyle(t, "syntax.number")
	for _, num := range []string{"-42", "1.5e10"} {
		start := strings.Index(line, num)
		for i := start; i < start+len(num); i++ {
			if st, ok := styleAt(t, "json", line, line[i:]); !ok || st != want {
				t.Errorf("byte %d of %q is not coloured as a number", i-start, num)
			}
		}
	}
}

// oldGeneric is the fallback as it was before tokens common, kept to measure
// what the richer set costs per line.
const oldGeneric = `
style s fg=#010203
profile old {
    rule '\b[0-9]+\b' s
    rule '"[^"]*"' s
    rule '\b([a-zA-Z_][a-zA-Z0-9_.-]*)=' s group=1
    rule '/[A-Za-z0-9_./+-]+' s
    rule '\b[0-9]{1,3}(\.[0-9]{1,3}){3}\b' s
    rule '\b[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}\b' s
    rule '[0-9]{2}:[0-9]{2}:[0-9]{2}([.,][0-9]+)?' s
    rule '[0-9]{4}-[0-9]{2}-[0-9]{2}' s
    rule '\[([a-zA-Z0-9_.:/-]+)\]' s group=1
    rule '(?i)\b(trace|debug)\b' s
    rule '(?i)\b(info)\b' s
    rule '(?i)\b(notice)\b' s
    rule '(?i)\b(warn|warning)\b' s
    rule '(?i)\b(error|err|failed|failure|exception|denied|refused)\b' s
    rule '(?i)\b(fatal|critical|crit|panic|emerg|segfault)\b' s
}
`

const benchLine = `2026-09-23T12:34:56.789Z host app[1234]: [net] level=warn peer 10.0.0.7:443 sent 4 KiB in 12ms path=/var/lib/app/state.db id=123e4567-e89b-12d3-a456-426614174000`

func benchProfile(b *testing.B, p *highlight.Profile) {
	e := highlight.NewEngine(0, 0)
	line := []byte(benchLine)
	b.ReportAllocs()
	for b.Loop() {
		e.SyntaxSpans(p, line)
	}
}

func BenchmarkCommon(b *testing.B) {
	c, err := Load(LoadOptions{NoUser: true})
	if err != nil {
		b.Fatal(err)
	}
	benchProfile(b, c.findProfile("generic"))
}

func BenchmarkOldGeneric(b *testing.B) {
	c := New()
	if err := c.Parse("<bench>", oldGeneric); err != nil {
		b.Fatal(err)
	}
	if err := c.Resolve(); err != nil {
		b.Fatal(err)
	}
	benchProfile(b, c.findProfile("old"))
}
