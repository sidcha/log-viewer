package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/sidcha/log-viewer/internal/highlight"
)

// A device that reboots while it is watched shows up as a new segment: the
// bootloader's lines take its profile the moment its banner is read.
func TestARebootInAFollowedLogOpensASegment(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 70; i++ {
		b.WriteString("[    1.000000] kernel line\n")
	}
	a, scr, ixr := newTestAppConf(t, b.String(), `
profile uboot {
    marker 'U-Boot ' '^U-Boot [0-9]{4}\.[0-9]{2}'
    rule '^=> .*' syntax.level.warn
}
`)
	if err := a.cfg.Set("cursor-line", "off"); err != nil {
		t.Fatal(err)
	}
	segs := a.eng.Profiles.(*highlight.Segments)
	if got := segs.Describe(0); !strings.HasPrefix(got, "kernel (detected") {
		t.Fatalf("before the reboot: %s", got)
	}

	f, err := os.OpenFile(a.src.Name(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("U-Boot 2024.01 (Jan 01 2024)\n=> boot\n")
	f.Close()
	ixr.Sync(t.Context())

	off := int64(len(b.String()))
	if got := segs.Describe(off); got != "uboot (marker, line 71)" {
		t.Errorf("banner segment = %q", got)
	}
	run(a, "goto-end")
	a.draw()
	row := -1
	for y := 0; y < testH; y++ {
		if strings.HasPrefix(rowText(scr, y), "=> boot") {
			row = y
		}
	}
	if row < 0 {
		t.Fatal("the prompt line is not on screen")
	}
	// Column 3, not 0: the cursor is drawn on the first cell of the last line.
	if _, _, st, _ := scr.GetContent(3, row); st != a.cfg.Style("syntax.level.warn") {
		t.Error("the bootloader line was not drawn with its segment's profile")
	}
}

// Indexing an unfollowed file still watches it grow, so a stretch that
// starts after it was opened is decided from its own lines, however the
// writes happen to be split.
func TestAStretchAppendedToAnUnfollowedLogIsDetected(t *testing.T) {
	head := strings.Repeat("hello world\n", 10)
	a, _, ixr := newTestAppConf(t, head, "marker 'Starting kernel'\n")
	segs := a.eng.Profiles.(*highlight.Segments)

	appendTo := func(s string) {
		f, err := os.OpenFile(a.src.Name(), os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(s)
		f.Close()
		ixr.Sync(t.Context())
	}
	appendTo("Starting kernel ...\n")
	appendTo(strings.Repeat("[    1.000000] kernel line\n", 64))

	off := int64(len(head) + len("Starting kernel ...\n"))
	if got := segs.Describe(off); !strings.HasPrefix(got, "kernel (detected") {
		t.Errorf("appended kernel lines = %q, want kernel detected", got)
	}
}
