// Command lv is a terminal log viewer for files that do not fit in memory.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/highlight"
	"github.com/sidcha/log-viewer/internal/index"
	"github.com/sidcha/log-viewer/internal/ruleset"
	"github.com/sidcha/log-viewer/internal/source"
	"github.com/sidcha/log-viewer/internal/termcolor"
	"github.com/sidcha/log-viewer/internal/ui"
	"github.com/sidcha/log-viewer/internal/watch"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// sniffBytes is how much of the head of the file is read to detect a syntax
// profile.
const sniffBytes = 1 << 16

// queryTimeout bounds the wait for a terminal that never answers the colour
// query. It is paid once, at startup.
const queryTimeout = 150 * time.Millisecond

// How far the derived tint sits from the terminal background, towards its
// foreground: enough to see, not enough to distract.
const cursorLineMix = 0.14

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

type options struct {
	configPath string
	noConfig   bool
	follow     bool
	wrap       bool
	lineNums   bool
	profile    string
	only       stringList
	never      stringList
	mark       stringList
	sets       stringList
	rulesPath  string
	background string
	dumpConfig bool
	listProf   bool
	showVer    bool
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-V", "--version", "version":
			fmt.Printf("lv %s\n", version)
			return
		case "config":
			os.Exit(runConfig(os.Args[2:]))
		}
	}
	os.Exit(run())
}

func run() int {
	var opt options

	fs := flag.NewFlagSet("lv", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = usage

	fs.StringVar(&opt.configPath, "c", "", "path to a configuration file")
	fs.StringVar(&opt.configPath, "config", "", "path to a configuration file")
	fs.BoolVar(&opt.noConfig, "no-config", false, "ignore user configuration")
	fs.BoolVar(&opt.follow, "f", false, "start in follow mode")
	fs.BoolVar(&opt.follow, "follow", false, "start in follow mode")
	fs.BoolVar(&opt.wrap, "w", false, "wrap long lines")
	fs.BoolVar(&opt.wrap, "wrap", false, "wrap long lines")
	fs.BoolVar(&opt.lineNums, "n", false, "show line numbers")
	fs.BoolVar(&opt.lineNums, "line-numbers", false, "show line numbers")
	fs.StringVar(&opt.profile, "p", "", "syntax profile to use instead of detecting one")
	fs.StringVar(&opt.profile, "profile", "", "syntax profile to use instead of detecting one")
	fs.Var(&opt.only, "o", "show only lines matching this pattern (repeatable)")
	fs.Var(&opt.only, "only", "show only lines matching this pattern (repeatable)")
	fs.Var(&opt.never, "N", "never show lines matching this pattern (repeatable)")
	fs.Var(&opt.never, "never", "never show lines matching this pattern (repeatable)")
	fs.Var(&opt.mark, "H", "highlight this pattern (repeatable)")
	fs.Var(&opt.mark, "highlight", "highlight this pattern (repeatable)")
	fs.StringVar(&opt.background, "background", "", "terminal background: auto, dark or light")
	fs.Var(&opt.sets, "set", "override a setting, name=value (repeatable)")
	fs.StringVar(&opt.rulesPath, "r", "", "load rules from a file saved earlier")
	fs.StringVar(&opt.rulesPath, "rules", "", "load rules from a file saved earlier")
	fs.BoolVar(&opt.dumpConfig, "dump-config", false, "print the effective configuration and exit")
	fs.BoolVar(&opt.listProf, "list-profiles", false, "list syntax profiles and exit")
	fs.BoolVar(&opt.showVer, "V", false, "print version and exit")
	fs.BoolVar(&opt.showVer, "version", false, "print version and exit")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if opt.showVer {
		fmt.Printf("lv %s\n", version)
		return 0
	}

	load := config.LoadOptions{Path: opt.configPath, NoUser: opt.noConfig}

	// Two passes: the first only to learn which background the user asked for,
	// since that decides which half of the theme applies and whether the
	// terminal is worth asking.
	pre, err := config.Load(load)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lv: %v\n", err)
		return 1
	}
	term := detectColors(pre, &opt)
	load.Background = "dark"
	if !term.Dark {
		load.Background = "light"
	}

	cfg, err := config.Load(load)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lv: %v\n", err)
		return 1
	}
	if err := applyFlagOverrides(cfg, &opt); err != nil {
		fmt.Fprintf(os.Stderr, "lv: %v\n", err)
		return 1
	}
	deriveStyles(cfg, term)

	switch {
	case opt.dumpConfig:
		fmt.Print(cfg.Dump())
		return 0
	case opt.listProf:
		for _, p := range cfg.Profiles() {
			fmt.Printf("%-10s %s\n", p.Name, strings.Join(p.Globs, " "))
		}
		return 0
	}

	src, err := openSource(fs.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "lv: %v\n", err)
		return 1
	}
	defer src.Close()

	// Piped input opens at the top, the way every pager does, so lv can be
	// one: `git config core.pager lv`, MANPAGER, anything that pipes. Landing
	// at the end of `git log` shows the oldest commit first, which is not what
	// anyone asked for. Tailing a stream is what -f is for.
	if src.IsStdin() {
		applyPagerDefaults(cfg)
	}

	prof, err := pickProfile(cfg, src, opt.profile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lv: %v\n", err)
		return 1
	}

	// Read the rules before touching the terminal, so a bad path is an
	// ordinary error message rather than one flashed after an alternate
	// screen has already been entered.
	var rules *ruleset.File
	if opt.rulesPath != "" {
		rules, err = ruleset.Load(opt.rulesPath)
		if err == nil {
			// Compile the patterns now as well, so a bad action or an
			// unbalanced bracket is reported here rather than once the
			// alternate screen is already up.
			_, _, err = rules.Set(cfg.Case())
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "lv: %v\n", err)
			return 1
		}
	}

	// Keep the heap tight: this is a viewer for files far larger than RAM, so
	// trading a little CPU for a smaller resident set is the right default.
	debug.SetGCPercent(50)

	err = serve(cfg, src, prof, rules, &opt)
	stopProducer(cfg, src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lv: %v\n", err)
		return 1
	}
	return 0
}

// stopProducer ends the process piping into lv once the viewer has gone.
//
// Closing our end of the pipe is not enough: the producer only finds out on its
// next write, and something like `dmesg -w` on a quiet kernel may not write for
// hours. Until then the shell is still waiting on the pipeline and the terminal
// looks hung, which is not a reasonable thing to leave behind after q.
func stopProducer(cfg *config.Config, src *source.Source) {
	if !src.IsStdin() || !src.Streaming() || !cfg.Bool("stop-producer") {
		return
	}

	pid := source.WriterOf(int(os.Stdin.Fd()))
	if pid == 0 {
		// Most often the producer runs as another user, so its /proc entries
		// are unreadable. Say so rather than leave a silent hang.
		fmt.Fprintln(os.Stderr,
			"lv: whatever is writing to lv is still running, so this pipeline will not finish; press Ctrl-C")
		return
	}

	if err := source.Signal(pid, syscall.SIGTERM); err != nil {
		name := source.CommandOf(pid)
		if name == "" {
			name = "pid " + strconv.Itoa(pid)
		}
		fmt.Fprintf(os.Stderr,
			"lv: %s (pid %d) is still writing and could not be stopped (%v); press Ctrl-C\n",
			name, pid, err)
	}
}

// detectColors works out what the terminal looks like.
func detectColors(cfg *config.Config, opt *options) termcolor.Colors {
	forced := cfg.Str("background")
	if opt.background != "" {
		forced = opt.background
	}

	// Asking needs the terminal itself, not stdin, which may be a pipe.
	var tty *os.File
	if forced == "auto" {
		if f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
			tty = f
			defer f.Close()
		}
	}
	return termcolor.Detect(tty, forced, queryTimeout)
}

// deriveStyles replaces the tints that only make sense relative to the
// terminal's actual colours.
//
// A cursor line picked as a fixed hex value is a clear band on one theme,
// invisible on another whose background happens to match it, and a smear on a
// third. Derived from the real background it sits the same distance from it
// whatever that is. Anything the user set in a config file is left alone.
func deriveStyles(cfg *config.Config, term termcolor.Colors) {
	if !term.Known {
		return
	}
	bg, fg := term.Background, term.Foreground

	cfg.SetDerivedStyle("cursor.line", tcell.StyleDefault.
		Background(toTcell(termcolor.Blend(bg, fg, cursorLineMix))))
}

func toTcell(c termcolor.RGB) tcell.Color {
	return tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))
}

func serve(cfg *config.Config, src *source.Source, prof *highlight.Profile, rules *ruleset.File, opt *options) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	idx := index.New(int64(cfg.Int("index-stride")), cfg.Int("max-checkpoints"))

	if cfg.Bool("quit-if-one-screen") {
		if done, err := dumpIfItFits(cfg, src); done || err != nil {
			return err
		}
	}
	if !cfg.Bool("alt-screen") {
		leaveOnScreen()
	}

	scr, err := newScreen()
	if err != nil {
		return err
	}
	if err := scr.Init(); err != nil {
		return err
	}
	defer scr.Fini()

	scr.SetStyle(cfg.Style("default"))
	// A block, so normal mode reads the way it does in vi.
	scr.SetCursorStyle(tcell.CursorStyleSteadyBlock)
	if cfg.Bool("mouse") {
		scr.EnableMouse(tcell.MouseButtonEvents)
	}
	scr.Clear()

	app := ui.New(scr, cfg, src, idx, prof)
	defer app.Close()

	w := newWatcher(cfg, src)
	defer w.Close()

	ixr := index.NewIndexer(src, idx, index.Options{
		Wake:   w.Events(),
		Block:  cfg.Int("block-size"),
		Advise: cfg.Str("page-cache-advise"),
		Notify: app.Wake,
	})
	ictx, icancel := context.WithCancel(ctx)
	defer icancel()
	go ixr.Run(ictx)

	// The file comes first so patterns given on the command line add to it
	// rather than being replaced by it.
	if err := app.ApplyRuleFile(rules, opt.rulesPath); err != nil {
		return err
	}
	if err := app.ApplyStartup(opt.only, opt.never, opt.mark); err != nil {
		return err
	}
	return app.Run(ctx)
}

// newWatcher picks how change notifications arrive. A spooled pipe has no path
// of its own, so it can only be polled.
func newWatcher(cfg *config.Config, src *source.Source) *watch.Watcher {
	if src.IsStdin() {
		return watch.NewPolling(cfg.Dur("watch-fallback"))
	}
	return watch.New(src.Name(), watch.Options{
		Backstop: cfg.Dur("watch-backstop"),
		Fallback: cfg.Dur("watch-fallback"),
	})
}

// leaveOnScreen keeps what was drawn on the terminal after lv exits, instead
// of restoring whatever was there before.
//
// It is what less does under -X, which is why git asks for LESS=FRX: the point
// of paging `git log` is usually to read a hash or a message and then use it,
// and a pager that wipes it the moment you press q makes you run the command
// again. The alternate screen is still right for opening a file to work in, so
// this is a setting rather than a rule.
//
// tcell has no API for it, but it honours the environment variable.
func leaveOnScreen() {
	_ = os.Setenv("TCELL_ALTSCREEN", "disable")
}

// newScreen builds a screen driven by the controlling terminal rather than
// stdin, so `producer | lv` still receives key presses.
func newScreen() (tcell.Screen, error) {
	if tty, err := tcell.NewDevTty(); err == nil {
		if scr, err := tcell.NewTerminfoScreenFromTty(tty); err == nil {
			return scr, nil
		}
	}
	return tcell.NewScreen()
}

func openSource(args []string) (*source.Source, error) {
	switch {
	case len(args) > 1:
		return nil, fmt.Errorf("expected at most one file, got %d", len(args))

	case len(args) == 1 && args[0] != "-":
		return source.Open(args[0])

	default:
		fi, err := os.Stdin.Stat()
		if err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			return nil, fmt.Errorf("no input: give a file, or pipe one in")
		}
		return source.FromStdin()
	}
}

// applyPagerDefaults adjusts the settings that should differ when lv is
// reading someone else's output rather than a file someone opened.
//
// Only defaults are touched: anything the user set in a config file or on the
// command line has already been recorded with an origin, and keeps it.
func applyPagerDefaults(cfg *config.Config) {
	// A piped document is finite and short far more often than not, so behave
	// the way less does under git's LESS=FRX: if it fits, print it and go.
	if cfg.Origin("quit-if-one-screen") == config.Builtin.String() {
		_ = cfg.Set("quit-if-one-screen", "on")
	}
}

func applyFlagOverrides(cfg *config.Config, opt *options) error {
	for _, kv := range opt.sets {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf("--set expects name=value, got %q", kv)
		}
		if err := cfg.Set(name, value); err != nil {
			return err
		}
	}
	if opt.follow {
		_ = cfg.Set("follow", "on")
	}
	if opt.wrap {
		_ = cfg.Set("wrap", "on")
	}
	if opt.lineNums {
		_ = cfg.Set("line-numbers", "on")
	}
	if opt.profile != "" {
		if err := cfg.Set("profile", opt.profile); err != nil {
			return err
		}
	}
	if opt.background != "" {
		if err := cfg.Set("background", opt.background); err != nil {
			return err
		}
	}
	return nil
}

// pickProfile resolves the syntax profile, sniffing the head of the file when
// no name was forced.
func pickProfile(cfg *config.Config, src *source.Source, forced string) (*highlight.Profile, error) {
	if forced == "" {
		forced = cfg.Str("profile")
	}
	if forced == "none" {
		return nil, nil
	}

	var head []byte
	if forced == "" {
		buf := make([]byte, sniffBytes)
		n, err := src.ReadAt(buf, 0)
		if err != nil && err != io.EOF && n == 0 {
			return nil, err
		}
		head = buf[:n]
	}

	p := highlight.Select(cfg.Profiles(), forced, src.Name(), head)
	if p == nil && forced != "" {
		return nil, fmt.Errorf("unknown profile %q (have: %s)",
			forced, strings.Join(highlight.Names(cfg.Profiles()), ", "))
	}
	return p, nil
}

func usage() {
	fmt.Fprint(os.Stderr, `lv - log viewer for files larger than memory

usage:
  lv [options] <file>
  <producer> | lv [options]
  lv config ...              read and write configuration

options:
  -c, --config PATH      read this configuration file
      --no-config        ignore user configuration
  -f, --follow           start pinned to the end, and stay there as it grows
  -w, --wrap             wrap long lines
  -n, --line-numbers     show a line-number gutter
  -p, --profile NAME     force a syntax profile ("none" disables highlighting)
  -o, --only PATTERN     show only matching lines (repeatable)
  -N, --never PATTERN    never show matching lines (repeatable)
  -H, --highlight PAT    mark a pattern without filtering (repeatable)
  -r, --rules PATH       load rules saved from an earlier session
      --background MODE  auto, dark or light; auto asks the terminal
      --set NAME=VALUE   override one setting (repeatable)
      --dump-config      print the effective configuration and exit
      --list-profiles    list syntax profiles and exit
  -V, --version          print version and exit

Press F1 inside lv for key bindings.
`)
}
