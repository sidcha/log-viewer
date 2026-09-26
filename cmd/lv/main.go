// Command lv is a terminal log viewer for files that do not fit in memory.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/highlight"
	"github.com/sidcha/log-viewer/internal/index"
	"github.com/sidcha/log-viewer/internal/ruleset"
	"github.com/sidcha/log-viewer/internal/source"
	"github.com/sidcha/log-viewer/internal/termcolor"
	"github.com/sidcha/log-viewer/internal/tty"
	"github.com/sidcha/log-viewer/internal/ui"
	"github.com/sidcha/log-viewer/internal/watch"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

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
	sets       stringList
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

	files, err := parseArgs(fs, &opt, os.Args[1:])
	if err != nil {
		// flag has already printed its own complaint and the usage.
		if !errors.Is(err, errFlagReported) {
			fmt.Fprintf(os.Stderr, "lv: %v\n", err)
		}
		return 2
	}
	if opt.showVer {
		fmt.Printf("lv %s\n", version)
		return 0
	}

	load := config.LoadOptions{Path: opt.configPath, NoUser: opt.noConfig}

	// The settings decide which background to resolve and whether the
	// terminal is worth asking, and the background decides which half of the
	// theme applies: read the files once, then replay them under the answer.
	pre, err := config.Load(load)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lv: %v\n", err)
		return 1
	}
	term := detectColors(pre, &opt)
	bg := "dark"
	if !term.Dark {
		bg = "light"
	}

	cfg, err := pre.WithBackground(bg)
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

	// Open and validate everything before touching the terminal, so a bad
	// path, profile or rules file is an ordinary error message rather than
	// one flashed after an alternate screen has already been entered.
	ins, err := openInputs(cfg, files, opt.profile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lv: %v\n", err)
		return 1
	}
	defer func() {
		for _, in := range ins {
			in.close()
		}
	}()

	// Piped input opens at the top, the way every pager does, so lv can be
	// one: `git config core.pager lv`, MANPAGER, anything that pipes. Landing
	// at the end of `git log` shows the oldest commit first, which is not what
	// anyone asked for. Tailing a stream is what -f is for.
	if len(ins) == 1 && ins[0].src.IsStdin() {
		applyPagerDefaults(cfg)
	}

	// Keep the heap tight: this is a viewer for files far larger than RAM, so
	// trading a little CPU for a smaller resident set is the right default.
	debug.SetGCPercent(50)

	err = serve(cfg, ins)
	for _, in := range ins {
		stopProducer(cfg, in.src)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "lv: %v\n", err)
		return 1
	}
	return 0
}

// input is one file, opened and ready to view.
type input struct {
	spec  fileSpec
	src   *source.Source
	prof  highlight.Profiler
	det   *highlight.Detector
	rules *ruleset.File
}

func (in *input) close() {
	if in.det != nil {
		in.det.Close()
	}
	in.src.Close()
}

// openInputs opens every file named on the command line and prepares its
// syntax profile and rules. On failure it closes whatever it already opened.
//
// The errors read as they did when lv took one file: an open or a rules load
// already names its path, and the profile is one for the whole session. Only
// a bad rule says nothing of where it came from, and needs saying so when
// there is more than one rules file it could be in.
func openInputs(cfg *config.Config, files []fileSpec, forced string) ([]*input, error) {
	var ins []*input
	fail := func(err error) ([]*input, error) {
		for _, in := range ins {
			in.close()
		}
		return nil, err
	}
	for _, spec := range files {
		src, err := openSource(spec.path)
		if err != nil {
			return fail(err)
		}
		in := &input{spec: spec, src: src}
		ins = append(ins, in)
		if in.prof, in.det, err = newProfiler(cfg, src, forced); err != nil {
			return fail(err)
		}
		if spec.rules != "" {
			if in.rules, err = ruleset.Load(spec.rules); err != nil {
				return fail(err)
			}
			// Compile the patterns now as well, so a bad action or an
			// unbalanced bracket is reported here rather than once the
			// alternate screen is already up.
			if _, _, err = in.rules.Set(cfg.Case()); err != nil {
				if len(files) > 1 {
					err = fmt.Errorf("%s: %w", spec.rules, err)
				}
				return fail(err)
			}
		}
	}
	return ins, nil
}

// stopProducer ends the process piping into lv once the viewer has gone.
//
// Closing our end of the pipe is not enough: the producer only finds out on its
// next write, and something like `dmesg -w` on a quiet kernel may not write for
// hours. Until then the shell is still waiting on the pipeline and the terminal
// looks hung, which is not a reasonable thing to leave behind after q.
func stopProducer(cfg *config.Config, src *source.Source) {
	if !src.IsStdin() || !src.Streaming() || !cfg.Options().StopProducer {
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
	forced := cfg.Options().Background
	if opt.background != "" {
		forced = opt.background
	}

	// Asking needs the terminal itself, not stdin, which may be a pipe.
	var term *os.File
	if forced == "auto" {
		if f, err := tty.Open(); err == nil {
			term = f
			defer f.Close()
		}
	}
	// A terminal that never answers costs this wait, once, at startup.
	return termcolor.Detect(term, forced, cfg.Options().QueryTimeout)
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
		Background(color.Blend(bg, fg, float64(cfg.Options().CursorLineTint)/100).Tcell()))
}

func serve(cfg *config.Config, ins []*input) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	o := cfg.Options()

	if o.QuitIfOneScreen && len(ins) == 1 {
		if done, err := dumpIfItFits(cfg, ins[0].src); done || err != nil {
			return err
		}
	}
	if !o.AltScreen {
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
	scr.Clear()

	app := ui.New(scr, cfg)
	defer app.Close()

	ictx, icancel := context.WithCancel(ctx)
	defer icancel()
	for _, in := range ins {
		idx := index.New(int64(o.IndexStride), o.MaxCheckpoints)
		wake := app.AddBuffer(in.src, idx, in.prof)
		w := newWatcher(cfg, in.src)
		defer w.Close()

		// A nil *Detector in the interface would not be a nil Observer.
		var obs index.Observer
		if in.det != nil {
			in.det.SetNotify(wake)
			obs = in.det
		}
		ixr := index.NewIndexer(in.src, idx, index.Options{
			Wake:     w.Events(),
			Block:    o.BlockSize,
			Advise:   o.PageCacheAdvise,
			Notify:   wake,
			Observer: obs,
		})
		go ixr.Run(ictx)

		// The file comes first so patterns given on the command line add to
		// it rather than being replaced by it.
		if err := app.ApplyRuleFile(in.rules, in.spec.rules); err != nil {
			return fmt.Errorf("%s: %w", in.spec.rules, err)
		}
		if err := app.ApplyStartup(in.spec.only, in.spec.never, in.spec.mark); err != nil {
			return fmt.Errorf("%s: %w", in.spec.path, err)
		}
	}
	app.ShowFirst()
	return app.Run(ctx)
}

// newWatcher picks how change notifications arrive. A spooled pipe has no path
// of its own, so it can only be polled.
func newWatcher(cfg *config.Config, src *source.Source) *watch.Watcher {
	if src.IsStdin() {
		return watch.NewPolling(cfg.Options().WatchFallback)
	}
	return watch.New(src.Name(), watch.Options{
		Backstop: cfg.Options().WatchBackstop,
		Fallback: cfg.Options().WatchFallback,
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

func openSource(path string) (*source.Source, error) {
	if path != "-" {
		return source.Open(path)
	}
	fi, err := os.Stdin.Stat()
	if err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return nil, fmt.Errorf("no input: give a file, or pipe one in")
	}
	return source.FromStdin()
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
		if err := cfg.SetFrom(name, value, config.OriginCommandLine); err != nil {
			return err
		}
	}
	if opt.follow {
		_ = cfg.SetFrom("follow", "on", config.OriginCommandLine)
	}
	if opt.wrap {
		_ = cfg.SetFrom("wrap", "on", config.OriginCommandLine)
	}
	if opt.lineNums {
		_ = cfg.SetFrom("line-numbers", "on", config.OriginCommandLine)
	}
	if opt.profile != "" {
		if err := cfg.SetFrom("profile", opt.profile, config.OriginCommandLine); err != nil {
			return err
		}
	}
	if opt.background != "" {
		if err := cfg.SetFrom("background", opt.background, config.OriginCommandLine); err != nil {
			return err
		}
	}
	return nil
}

// newProfiler decides how lines get their syntax profile. One named on
// the command line or in the configuration applies to the whole file;
// otherwise a Detector finds one per segment as the indexer reads.
func newProfiler(cfg *config.Config, src *source.Source, forced string) (highlight.Profiler, *highlight.Detector, error) {
	if forced == "" {
		forced = cfg.Options().Profile
	}
	switch forced {
	case "none":
		return highlight.Fixed{}, nil, nil
	case "":
		det := highlight.NewDetector(cfg.DetectOptions(src.Name()))
		return det.Segments(), det, nil
	}
	p := highlight.Find(cfg.Profiles(), forced)
	if p == nil {
		return nil, nil, fmt.Errorf("unknown profile %q (have: %s)",
			forced, strings.Join(highlight.Names(cfg.Profiles()), ", "))
	}
	return highlight.Fixed{P: p, Why: "forced"}, nil, nil
}

func usage() {
	fmt.Fprint(os.Stderr, `lv - log viewer for files larger than memory

usage:
  lv [options] [[file options] <file>]...
  <producer> | lv [options]
  lv config ...              read and write configuration

options:
  -c, --config PATH      read this configuration file
      --no-config        ignore user configuration
  -f, --follow           start pinned to the end, and stay there as it grows
  -w, --wrap             wrap long lines
  -n, --line-numbers     show a line-number gutter
  -p, --profile NAME     force a syntax profile ("none" disables highlighting)
  -o, --only PATTERN     for the next file: show only matching lines (repeatable)
  -N, --never PATTERN    for the next file: never show matching lines (repeatable)
  -H, --highlight PAT    for the next file: mark a pattern without filtering (repeatable)
  -r, --rules PATH       for the next file: load rules saved from an earlier session
      --background MODE  auto, dark or light; auto asks the terminal
      --set NAME=VALUE   override one setting (repeatable)
      --dump-config      print the effective configuration and exit
      --list-profiles    list syntax profiles and exit
  -V, --version          print version and exit

Press F1 inside lv for key bindings.
`)
}
