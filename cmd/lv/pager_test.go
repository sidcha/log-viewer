package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/source"
)

// lv has to be usable wherever a pager can be configured -- core.pager,
// GIT_PAGER, MANPAGER, PAGER -- which means behaving like one when it is
// reading someone else's output.

func TestPipedInputOpensAtTheTop(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bool("follow") {
		t.Fatal("follow starts on")
	}

	applyPagerDefaults(cfg)

	if cfg.Bool("follow") {
		t.Error("piped input follows; `git log` would open on the oldest commit")
	}
}

func TestPipedInputQuitsIfItFits(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bool("quit-if-one-screen") {
		t.Fatal("a named file should open the viewer, not print and exit")
	}

	applyPagerDefaults(cfg)

	if !cfg.Bool("quit-if-one-screen") {
		t.Error("short piped output takes the screen over; `git log --oneline` should just print")
	}
}

// TestPagerDefaultsYieldToTheUser: they are defaults, not decisions.
func TestPagerDefaultsYieldToTheUser(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("set quit-if-one-screen off\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.LoadOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}

	applyPagerDefaults(cfg)

	if cfg.Bool("quit-if-one-screen") {
		t.Error("the pager default overrode a setting from a config file")
	}
}

// TestPagerDefaultsYieldToTheCommandLine: `core.pager = "lv --set
// quit-if-one-screen=off"` is the documented way to keep the setting in one
// place, so the flag has to count as the user's choice.
func TestPagerDefaultsYieldToTheCommandLine(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	opt := options{sets: stringList{"quit-if-one-screen=off"}}
	if err := applyFlagOverrides(cfg, &opt); err != nil {
		t.Fatal(err)
	}

	applyPagerDefaults(cfg)

	if cfg.Bool("quit-if-one-screen") {
		t.Error("the pager default overrode --set")
	}
	if got := cfg.Origin("quit-if-one-screen"); got != config.OriginCommandLine {
		t.Errorf("origin = %q, want the command line", got)
	}
}

// TestOneScreenDumpIsRefusedWithoutATerminal, since there is no screen to
// measure and nothing to decide.
func TestOneScreenDumpIsRefusedWithoutATerminal(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "x.log")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := source.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	// The test process has no controlling terminal of a known size; whatever
	// the answer, it must not be "dumped it" and must not fail.
	done, err := dumpIfItFits(cfg, src)
	if err != nil {
		t.Fatalf("dumpIfItFits: %v", err)
	}
	_ = done
}

func TestFitsOnScreenCountsWrappedRows(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}

	write := func(body string) *source.Source {
		t.Helper()
		path := filepath.Join(t.TempDir(), "x.log")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		src, err := source.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { src.Close() })
		return src
	}

	cases := []struct {
		name string
		body string
		rows int
		cols int
		want bool
	}{
		{"short", "one\ntwo\n", 10, 80, true},
		{"exactly full", strings.Repeat("x\n", 10), 10, 80, true},
		{"one too many", strings.Repeat("x\n", 11), 10, 80, false},
		{"one long line wraps", strings.Repeat("x", 100) + "\n", 1, 80, false},
		{"and fits when there is room", strings.Repeat("x", 100) + "\n", 2, 80, true},
		// Escape sequences take no columns, so a line full of them still fits.
		{"escapes are free", "\x1b[31m" + strings.Repeat("x", 70) + "\x1b[0m\n", 1, 80, true},
		// A tab is not one column.
		{"tabs expand", "\tx\n", 1, 4, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := fitsOnScreen(cfg, write(c.body), c.rows, c.cols); got != c.want {
				t.Errorf("fitsOnScreen(%d rows, %d cols) = %v, want %v", c.rows, c.cols, got, c.want)
			}
		})
	}
}
