package ui

import (
	"strings"
	"testing"
)

func TestByteCensusTellsTextFromBinary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
		want  string
	}{
		{"ascii", []string{"plain text", "more"}, "utf-8"},
		{"multibyte", []string{"héllo wörld ✓"}, "utf-8"},
		{"ansi colour", []string{"\x1b[31mERROR\x1b[0m\tdisk\r"}, "utf-8"},
		// Serial noise at boot is a log with a few bad bytes, not a binary.
		{"stray bytes", []string{strings.Repeat("x", 60) + "\xff\xfe" + strings.Repeat("y", 60)}, "utf-8"},
		{"nul", []string{"text\x00text"}, "binary"},
		{"mostly garbage", []string{"\xff\x01\x02\x03\xfeab"}, "binary"},
		{"nothing", nil, ""},
	} {
		var c byteCensus
		for _, l := range tc.lines {
			c.add([]byte(l), 4096)
		}
		if got := c.encoding(); got != tc.want {
			t.Errorf("%s: encoding = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Only the first limit bytes of a line are looked at, as for highlighting,
// so a huge line costs no more than a short one.
func TestByteCensusLooksOnlyAtTheScanLimit(t *testing.T) {
	var c byteCensus
	c.add([]byte(strings.Repeat("a", 100)+"\x00"), 100)
	if got := c.encoding(); got != "utf-8" {
		t.Errorf("encoding = %q; the NUL past the limit was counted", got)
	}
}

func TestStatusShowsTheEncodingOnScreen(t *testing.T) {
	a, scr := newTestAppWith(t, isoLines(100), "status-line", "on", "status-right", "{encoding}")
	if got := statusRow(a, scr); !strings.HasSuffix(got, "utf-8") {
		t.Errorf("text file: %q", got)
	}

	bin := strings.Repeat("\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x03\x00>\x00\n", 20)
	a, scr = newTestAppWith(t, bin, "status-line", "on", "status-right", "{encoding}")
	if got := statusRow(a, scr); !strings.HasSuffix(got, "binary") {
		t.Errorf("binary file: %q", got)
	}
}
