// Package config parses lv's configuration: settings, key bindings, colour
// styles and syntax profiles.
//
// Files cascade the way git's do: a machine-wide file, then a per-user one,
// then a per-directory one, each overriding the last. The built-in defaults are
// written in this same language and embedded in the binary, so there is exactly
// one code path and `lv config --list` can show what a file overrides.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/highlight"
)

type kind int

const (
	kBool kind = iota
	kInt
	kStr
	kEnum
	kDur
)

// Scope is when a setting can take effect.
type Scope int

const (
	// Live settings can be changed while lv is running, with :set.
	Live Scope = iota
	// Startup settings shape things built once, as lv starts -- the index,
	// the screen, the file watch -- and can only be set in a configuration
	// file or with --set.
	Startup
)

// Setting describes one `set` key.
type Setting struct {
	Name  string
	Kind  kind
	Def   string
	Enum  []string
	Scope Scope
	Help  string
}

// Settings is the complete set of recognised options. Anything else in a
// `set` directive is an error, so typos surface at startup instead of silently
// doing nothing.
var Settings = []Setting{
	{"follow", kBool, "off", nil, Startup, "start pinned to the end of the file"},
	{"wrap", kBool, "off", nil, Live, "wrap long lines instead of truncating"},
	{"line-numbers", kBool, "off", nil, Live, "show a source line-number gutter"},
	{"ansi", kBool, "on", nil, Live, "draw the colours a log was captured with"},
	{"status-line", kEnum, "auto", []string{"auto", "on", "off"}, Live, "when to draw the status line"},
	{"alt-screen", kBool, "on", nil, Startup, "draw on the alternate screen, restoring the terminal on exit"},
	{"quit-if-one-screen", kBool, "off", nil, Startup, "print and exit when the whole input fits on the screen"},
	{"ask-save", kEnum, "auto", []string{"auto", "on", "off"}, Live, "ask before quitting whether to keep piped input"},
	{"ask-save-rules", kBool, "on", nil, Live, "ask before quitting whether to save the rules"},
	{"case", kEnum, "smart", []string{"smart", "sensitive", "insensitive"}, Live, "pattern case sensitivity"},
	{"tab-width", kInt, "8", nil, Live, "columns per tab stop"},
	{"scroll-step", kInt, "1", nil, Live, "lines moved per step by scroll-up and scroll-down: C-y, C-e and the wheel"},
	{"h-scroll-step", kInt, "8", nil, Live, "columns moved by left/right"},
	{"page-overlap", kInt, "2", nil, Live, "lines kept on screen when paging"},
	{"watch-backstop", kDur, "2s", nil, Startup, "poll interval alongside kernel change notifications"},
	{"watch-fallback", kDur, "200ms", nil, Startup, "poll interval when notifications are unavailable"},
	{"scan-poll", kDur, "200ms", nil, Startup, "how often a caught-up filter scan looks for new data"},
	{"block-size", kInt, "1048576", nil, Startup, "bytes read per syscall"},
	{"max-line-bytes", kInt, "1048576", nil, Startup, "how much of one line is retained"},
	{"max-scan-bytes", kInt, "4096", nil, Live, "bytes of a line examined for highlighting"},
	{"index-stride", kInt, "1024", nil, Startup, "lines between index checkpoints"},
	{"max-checkpoints", kInt, "524288", nil, Startup, "line-index checkpoint ceiling (16 bytes each) before the stride doubles"},
	{"page-cache-advise", kEnum, "auto", []string{"auto", "on", "off"}, Startup, "hand scanned pages back to the kernel"},
	{"profile", kStr, "", nil, Startup, "force a syntax profile instead of detecting one"},
	{"mouse", kBool, "on", nil, Live, "turn the wheel into <WheelUp> and <WheelDown> keys"},
	{"gutter", kStr, "│ ", nil, Live, "separator drawn after the line-number gutter"},
	{"message-timeout", kDur, "4s", nil, Live, "how long a status message stays up"},
	{"scroll-off", kInt, "3", nil, Live, "lines of context kept above and below the cursor"},
	{"cursor-line", kBool, "on", nil, Live, "mark the line the cursor is on"},
	{"clipboard-command", kStr, "", nil, Live, "external clipboard program; empty uses the terminal escape"},
	{"stop-producer", kBool, "on", nil, Live, "on exit, stop the process piping into lv"},
	{"background", kEnum, "auto", []string{"auto", "dark", "light"}, Startup, "terminal background; auto asks the terminal"},
	{"query-timeout", kDur, "150ms", nil, Startup, "how long to wait for the terminal to report its colours"},
	{"cursor-line-tint", kInt, "14", nil, Startup, "percent of the way from background to foreground the derived cursor line sits"},
	{"one-screen-wait", kDur, "250ms", nil, Startup, "how long quit-if-one-screen waits for piped input to finish"},
	{"regex-timeout", kDur, "250ms", nil, Startup, "how long one backtracking regex match may run before the line counts as a miss"},
	{"filter-checkpoint-bytes", kInt, "262144", nil, Startup, "bytes between filtered-view checkpoints, however rare the matches"},
	{"max-highlights", kInt, "64", nil, Live, "matches of one pattern marked on a line"},
	{"history-size", kInt, "100", nil, Live, "patterns, paths and commands remembered for recall"},
	{"osc52-max-bytes", kInt, "65536", nil, Live, "largest yank sent through the terminal clipboard escape"},
	{"idle-release", kDur, "3s", nil, Live, "quiet time before freed memory is returned to the OS"},
}

var settingIndex = func() map[string]*Setting {
	m := make(map[string]*Setting, len(Settings))
	for i := range Settings {
		m[Settings[i].Name] = &Settings[i]
	}
	return m
}()

// Bind associates a key sequence with a command in one key table.
type Bind struct {
	Keys  []Key
	Spec  string
	Cmd   Command
	Table string
}

// Preset is a rule declared in configuration rather than added at runtime.
type Preset struct {
	Action  filter.Action
	Mode    filter.Mode
	Pattern string
	// Color is set when the file pins one; otherwise the viewer assigns it.
	Color color.RGB
	// M is the compiled pattern, under the configured case mode. Resolve
	// fills it in, so a bad pattern fails the load like any other error.
	M filter.Matcher
	// where names the directive, for error messages.
	where string
}

// IsBool reports whether the setting takes on/off, which is what lets :set
// toggle it with no value given.
func (s *Setting) IsBool() bool { return s.Kind == kBool }

// LookupSetting finds a setting by name.
func LookupSetting(name string) (*Setting, bool) {
	s, ok := settingIndex[name]
	return s, ok
}

// Config is the fully resolved configuration.
type Config struct {
	values map[string]string
	// tables holds each key table's trie, and orders its bindings in the
	// order they were declared.
	tables   map[string]*keyNode
	orders   map[string][]Bind
	styles   map[string]tcell.Style
	profiles []*highlight.Profile
	// tokens holds named rule sets that profiles pull in with `use`, and
	// tokenOrder keeps their declaration order for Dump.
	tokens     map[string]*highlight.TokenSet
	tokenOrder []string
	presets    []Preset
	loaded     []string
	sourced    map[string]bool
	// background is the resolved terminal background, selecting which
	// `background dark { }` blocks apply.
	background string
	// styleOrigins records which file defined each style, so a colour derived
	// from the terminal never overrides one the user chose.
	styleOrigins map[string]string
	// origins records which file last set each value, so a surprising
	// effective value can be traced back to the layer responsible.
	origins map[string]string
	// version counts changes to values, for Options holders.
	version uint64

	// parsed caches every file's statements by absolute path, and roots
	// lists the files Load read at the top of the cascade, so the whole
	// configuration can be replayed under another background without
	// reading or tokenizing anything again.
	parsed map[string][]stmt
	roots  []string
}

// New returns a configuration holding only the built-in defaults for settings.
// Bindings, styles and profiles come from the embedded default file.
func New() *Config {
	c := &Config{
		values:       make(map[string]string, len(Settings)),
		tables:       make(map[string]*keyNode),
		orders:       make(map[string][]Bind),
		styles:       make(map[string]tcell.Style),
		tokens:       make(map[string]*highlight.TokenSet),
		sourced:      make(map[string]bool),
		parsed:       make(map[string][]stmt),
		origins:      make(map[string]string),
		styleOrigins: make(map[string]string),
	}
	for _, sp := range Settings {
		c.origins[sp.Name] = Builtin.String()
	}
	for _, s := range Settings {
		c.values[s.Name] = s.Def
	}
	return c
}

// Files lists the configuration files that were read, in order.
func (c *Config) Files() []string { return c.loaded }

// ---------------------------------------------------------------- accessors

// Bool reads a boolean setting.
func (c *Config) Bool(name string) bool {
	b, _ := parseBool(c.values[name])
	return b
}

// parseBool reads the words a boolean setting accepts.
func parseBool(s string) (value, ok bool) {
	switch strings.ToLower(s) {
	case "on", "true", "yes", "1":
		return true, true
	case "off", "false", "no", "0":
		return false, true
	}
	return false, false
}

// Int reads an integer setting, falling back to its default.
func (c *Config) Int(name string) int {
	if n, err := strconv.Atoi(c.values[name]); err == nil {
		return n
	}
	if s, ok := settingIndex[name]; ok {
		n, _ := strconv.Atoi(s.Def)
		return n
	}
	return 0
}

// Str reads a string setting.
func (c *Config) Str(name string) string { return c.values[name] }

// Dur reads a duration setting.
func (c *Config) Dur(name string) time.Duration {
	if d, err := time.ParseDuration(c.values[name]); err == nil {
		return d
	}
	if s, ok := settingIndex[name]; ok {
		d, _ := time.ParseDuration(s.Def)
		return d
	}
	return 0
}

// Origins of a value that was not read from a file.
const (
	// OriginCommandLine is a value given as a flag.
	OriginCommandLine = "<command line>"
	// OriginSession is a value changed while lv was running.
	OriginSession = "<session>"
)

// Set overrides a setting during a session, validating it the same way the
// parser does.
func (c *Config) Set(name, value string) error { return c.SetFrom(name, value, OriginSession) }

// SetFrom overrides a setting and records where the value came from, so
// Origin can tell a value somebody chose from a built-in default.
func (c *Config) SetFrom(name, value, origin string) error {
	s, ok := settingIndex[name]
	if !ok {
		return fmt.Errorf("unknown setting %q", name)
	}
	if err := validate(s, value); err != nil {
		return err
	}
	c.values[name] = value
	c.origins[name] = origin
	c.version++
	return nil
}

// Origin names the file that last set a value, or the built-in level.
func (c *Config) Origin(name string) string {
	if o, ok := c.origins[name]; ok {
		return o
	}
	return Builtin.String()
}

// Case is the configured pattern case sensitivity.
func (c *Config) Case() filter.Case {
	cs, _ := filter.ParseCase(c.Str("case"))
	return cs
}

// Background is the resolved terminal background, "dark" or "light".
func (c *Config) Background() string { return c.background }

// SetDerivedStyle installs a style computed from the terminal's own colours,
// unless a configuration file already defined one. What the user wrote always
// wins over what lv worked out.
func (c *Config) SetDerivedStyle(name string, st tcell.Style) bool {
	if origin, ok := c.styleOrigins[name]; ok && origin != builtinName {
		return false
	}
	c.styles[name] = st
	return true
}

// Style looks up a named style, falling back to the default style.
func (c *Config) Style(name string) tcell.Style {
	if s, ok := c.styles[name]; ok {
		return s
	}
	return tcell.StyleDefault
}

// HasStyle reports whether a style name is defined.
func (c *Config) HasStyle(name string) bool {
	_, ok := c.styles[name]
	return ok
}

// Profiles lists the syntax profiles.
func (c *Config) Profiles() []*highlight.Profile { return c.profiles }

// Presets lists highlights declared in configuration.
func (c *Config) Presets() []Preset { return c.presets }

// ------------------------------------------------------------------ parsing

// Parse reads configuration text attributed to file.
func (c *Config) Parse(file, src string) error {
	stmts, err := tokenize(file, src)
	if err != nil {
		return err
	}
	c.parsed[file] = stmts
	return c.run(file, stmts)
}

func (c *Config) run(file string, stmts []stmt) error {
	c.loaded = append(c.loaded, file)
	return c.exec(stmts)
}

// ParseFile reads configuration from disk.
func (c *Config) ParseFile(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if c.sourced[abs] {
		return nil // already read; tolerate diamond includes
	}
	c.sourced[abs] = true

	if stmts, ok := c.parsed[abs]; ok {
		return c.run(path, stmts)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	stmts, err := tokenize(path, string(b))
	if err != nil {
		return err
	}
	c.parsed[abs] = stmts
	return c.run(path, stmts)
}

// WithBackground returns the configuration as it would have loaded had the
// terminal background been known from the start: the same files, with the
// `background` blocks for bg applied in their place in the cascade.
//
// Which background applies is itself a setting, and asking the terminal
// needs the settings, so it cannot be known before the first load. The
// replay reuses the statements already read rather than the disk.
func (c *Config) WithBackground(bg string) (*Config, error) {
	n := New()
	n.background = bg
	n.parsed = c.parsed
	for _, root := range c.roots {
		if stmts, ok := c.parsed[root]; ok && root == builtinName {
			if err := n.run(root, stmts); err != nil {
				return nil, err
			}
			continue
		}
		if err := n.ParseFile(root); err != nil {
			return nil, err
		}
	}
	n.roots = c.roots
	if err := n.Resolve(); err != nil {
		return nil, err
	}
	return n, nil
}

func (c *Config) exec(stmts []stmt) error {
	for i := 0; i < len(stmts); i++ {
		st := stmts[i]
		w := st.Words

		if w[len(w)-1] == "{" {
			body, end, err := collectBlock(stmts, i)
			if err != nil {
				return err
			}
			if err := c.block(stmt{File: st.File, Line: st.Line, Words: w[:len(w)-1]}, body); err != nil {
				return err
			}
			i = end
			continue
		}
		if w[0] == "}" {
			return st.errf("unexpected }")
		}
		if err := c.directive(st); err != nil {
			return err
		}
	}
	return nil
}

func collectBlock(stmts []stmt, start int) (body []stmt, end int, err error) {
	depth := 1
	for i := start + 1; i < len(stmts); i++ {
		w := stmts[i].Words
		switch {
		case len(w) == 1 && w[0] == "}":
			depth--
			if depth == 0 {
				return body, i, nil
			}
		case w[len(w)-1] == "{":
			depth++
			body = append(body, stmts[i])
		default:
			body = append(body, stmts[i])
		}
	}
	return nil, 0, stmts[start].errf("unclosed {")
}

func (c *Config) directive(st stmt) error {
	w := st.Words
	switch w[0] {
	case "set":
		if len(w) != 3 {
			return st.errf("usage: set <name> <value>")
		}
		if err := c.SetFrom(w[1], w[2], st.File); err != nil {
			return st.errf("%v", err)
		}

	case "bind", "bind-visual":
		table, w := keyTableFlag(w)
		if w[0] == "bind-visual" {
			table = "visual"
		}
		if len(w) < 3 {
			return st.errf("usage: bind [-T table] <key> <command> [args...]")
		}
		seq, err := ParseSequence(w[1])
		if err != nil {
			return st.errf("%v", err)
		}
		cmd, err := ParseCommand(w[2:])
		if err != nil {
			return st.errf("%v", err)
		}
		if err := c.bind(table, seq, w[1], cmd); err != nil {
			return st.errf("%v", err)
		}

	case "unbind", "unbind-visual":
		table, w := keyTableFlag(w)
		if w[0] == "unbind-visual" {
			table = "visual"
		}
		if len(w) != 2 {
			return st.errf("usage: unbind [-T table] <key>")
		}
		seq, err := ParseSequence(w[1])
		if err != nil {
			return st.errf("%v", err)
		}
		if err := c.unbind(table, seq); err != nil {
			return st.errf("%v", err)
		}

	case "style":
		if len(w) < 3 {
			return st.errf("usage: style <name> <attr>...")
		}
		style, err := parseStyle(w[2:])
		if err != nil {
			return st.errf("%v", err)
		}
		c.styles[w[1]] = style
		c.styleOrigins[w[1]] = st.File

	case "rule":
		if len(w) < 3 {
			return st.errf("usage: rule <only|never|highlight> <pattern> [match=literal|regex] [color=#rrggbb]")
		}
		action, err := ParseAction(w[1])
		if err != nil {
			return st.errf("%v", err)
		}
		p := Preset{Action: action, Pattern: w[2], where: fmt.Sprintf("%s:%d", st.File, st.Line)}
		for _, extra := range w[3:] {
			k, v, ok := strings.Cut(extra, "=")
			if !ok {
				return st.errf("unknown rule option %q", extra)
			}
			switch k {
			case "color":
				col, err := parseRuleColor(v)
				if err != nil {
					return st.errf("%v", err)
				}
				p.Color = col
			case "match":
				mode, err := filter.ParseMode(v)
				if err != nil {
					return st.errf("%v", err)
				}
				p.Mode = mode
			default:
				return st.errf("unknown rule option %q", extra)
			}
		}
		c.presets = append(c.presets, p)

	case "source":
		if len(w) != 2 {
			return st.errf("usage: source <path>")
		}
		path := ExpandHome(w[1])
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(st.File), path)
		}
		if err := c.ParseFile(path); err != nil {
			if os.IsNotExist(err) {
				return nil // optional include
			}
			return st.errf("%v", err)
		}

	default:
		return st.errf("unknown directive %q", w[0])
	}
	return nil
}

// keyTableFlag takes a `-T table` option off a bind or unbind directive,
// defaulting to the normal table, and returns the words without it.
func keyTableFlag(w []string) (string, []string) {
	if len(w) >= 3 && w[1] == "-T" {
		return w[2], append([]string{w[0]}, w[3:]...)
	}
	return "normal", w
}

// blockHandlers runs each kind of `name ... { }` block. It is filled in init
// because a background block's body is run back through block.
var blockHandlers map[string]func(c *Config, head stmt, body []stmt) error

func init() {
	blockHandlers = map[string]func(c *Config, head stmt, body []stmt) error{
		"background": (*Config).backgroundBlock,
		"profile":    (*Config).profileBlock,
		"tokens":     (*Config).tokensBlock,
	}
}

func (c *Config) block(head stmt, body []stmt) error {
	run, ok := blockHandlers[head.Words[0]]
	if !ok {
		return head.errf("unknown block %q", head.Words[0])
	}
	return run(c, head, body)
}

// backgroundBlock holds the parts of a theme that only make sense against a
// dark or a light terminal.
func (c *Config) backgroundBlock(head stmt, body []stmt) error {
	if len(head.Words) != 2 {
		return head.errf("usage: background <dark|light> { ... }")
	}
	switch head.Words[1] {
	case "dark", "light":
	default:
		return head.errf("background takes dark or light, not %q", head.Words[1])
	}
	if head.Words[1] != c.background {
		return nil // a theme for the other kind of terminal
	}
	return c.exec(body)
}

// profileBlock declares or extends a syntax profile.
func (c *Config) profileBlock(head stmt, body []stmt) error {
	if len(head.Words) != 2 {
		return head.errf("usage: profile <name> { ... }")
	}
	name := head.Words[1]

	p := c.findProfile(name)
	if p == nil {
		p = &highlight.Profile{Name: name}
		c.profiles = append(c.profiles, p)
	}

	for _, st := range body {
		w := st.Words
		switch w[0] {
		case "match":
			if len(w) < 2 {
				return st.errf("usage: match <glob>...")
			}
			p.Globs = append(p.Globs, w[1:]...)

		case "detect":
			if len(w) != 2 {
				return st.errf("usage: detect <regex>")
			}
			re, err := regexp.Compile(w[1])
			if err != nil {
				return st.errf("%v", err)
			}
			p.Detects = append(p.Detects, re)

		case "clear":
			p.Items = nil

		case "rule":
			it, err := ruleItem(st)
			if err != nil {
				return err
			}
			p.Items = append(p.Items, it)

		case "use":
			it, err := useItem(st)
			if err != nil {
				return err
			}
			p.Items = append(p.Items, it)

		default:
			return st.errf("unknown profile directive %q", w[0])
		}
	}
	return nil
}

// ruleItem parses a `rule <regex> <style> [group=N]` line of a profile or
// token set.
func ruleItem(st stmt) (highlight.Item, error) {
	w := st.Words
	if len(w) < 3 {
		return highlight.Item{}, st.errf("usage: rule <regex> <style> [group=N]")
	}
	re, err := regexp.Compile(w[1])
	if err != nil {
		return highlight.Item{}, st.errf("%v", err)
	}
	r := highlight.Rule{Re: re, StyleName: w[2]}
	for _, extra := range w[3:] {
		k, v, ok := strings.Cut(extra, "=")
		if !ok || k != "group" {
			return highlight.Item{}, st.errf("unknown rule option %q", extra)
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return highlight.Item{}, st.errf("bad group %q", v)
		}
		r.Group = n
	}
	return highlight.Item{Rule: r}, nil
}

// useItem parses a `use <tokens>` line.
func useItem(st stmt) (highlight.Item, error) {
	if len(st.Words) != 2 {
		return highlight.Item{}, st.errf("usage: use <tokens>")
	}
	return highlight.Item{Use: st.Words[1], Where: fmt.Sprintf("%s:%d", st.File, st.Line)}, nil
}

// tokensBlock declares a token set, replacing any earlier one of that name.
func (c *Config) tokensBlock(head stmt, body []stmt) error {
	if len(head.Words) != 2 {
		return head.errf("usage: tokens <name> { ... }")
	}
	name := head.Words[1]
	set := &highlight.TokenSet{Name: name}
	for _, st := range body {
		var it highlight.Item
		var err error
		switch st.Words[0] {
		case "rule":
			it, err = ruleItem(st)
		case "use":
			it, err = useItem(st)
		default:
			return st.errf("tokens take rule and use lines, not %q", st.Words[0])
		}
		if err != nil {
			return err
		}
		set.Items = append(set.Items, it)
	}
	if _, ok := c.tokens[name]; !ok {
		c.tokenOrder = append(c.tokenOrder, name)
	}
	c.tokens[name] = set
	return nil
}

func (c *Config) findProfile(name string) *highlight.Profile {
	for _, p := range c.profiles {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// Resolve binds style names to styles after all files have been read, so a user
// file can redefine a style the built-in profiles reference.
func (c *Config) Resolve() error {
	for _, p := range c.profiles {
		if err := highlight.Expand(p, c.tokens); err != nil {
			return err
		}
	}

	var missing []string
	for _, p := range c.profiles {
		for i := range p.Rules {
			name := p.Rules[i].StyleName
			if !c.HasStyle(name) {
				missing = append(missing, fmt.Sprintf("profile %s: style %q", p.Name, name))
				continue
			}
			p.Rules[i].Style = c.Style(name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("undefined styles:\n  %s", strings.Join(missing, "\n  "))
	}

	// Rules compile once every file is read, because the case mode and the
	// regex timeout they are compiled under can be set after them. The
	// timeout is process-wide, so every later pattern compiles under it too.
	filter.SetMatchTimeout(c.Dur("regex-timeout"))
	for i := range c.presets {
		p := &c.presets[i]
		m, err := filter.Compile(p.Pattern, p.Mode, c.Case())
		if err != nil {
			return fmt.Errorf("%s: rule %q: %w", p.where, p.Pattern, err)
		}
		p.M = m
	}
	return nil
}

func validate(s *Setting, value string) error {
	switch s.Kind {
	case kBool:
		if _, ok := parseBool(value); ok {
			return nil
		}
		return fmt.Errorf("%s: expected on/off, got %q", s.Name, value)
	case kInt:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s: expected an integer, got %q", s.Name, value)
		}
		if n < 0 {
			return fmt.Errorf("%s: must not be negative", s.Name)
		}
	case kDur:
		if _, err := time.ParseDuration(value); err != nil {
			return fmt.Errorf("%s: expected a duration such as 150ms, got %q", s.Name, value)
		}
	case kEnum:
		for _, e := range s.Enum {
			if value == e {
				return nil
			}
		}
		return fmt.Errorf("%s: expected one of %s, got %q", s.Name, strings.Join(s.Enum, "/"), value)
	}
	return nil
}

// ParseAction is re-exported so the command line and the configuration file
// name the actions identically.
func ParseAction(s string) (filter.Action, error) { return filter.ParseAction(s) }

func parseRuleColor(v string) (color.RGB, error) {
	c, err := parseColor(v)
	if err != nil {
		return color.RGB{}, err
	}
	return color.FromTcell(c), nil
}

// ExpandHome resolves a leading ~ the way a shell would, for paths read from
// a file or typed at a prompt.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// Dump renders the effective configuration in the input syntax.
func (c *Config) Dump() string {
	var b strings.Builder

	b.WriteString("# effective lv configuration\n")
	if len(c.loaded) > 0 {
		b.WriteString("# read from: " + strings.Join(c.loaded, ", ") + "\n")
	}

	b.WriteString("\n# settings\n")
	for _, s := range Settings {
		help := s.Help
		if s.Scope == Startup {
			help += " (at startup)"
		}
		fmt.Fprintf(&b, "set %-18s %-12s # %s\n", s.Name, quoteWord(c.values[s.Name]), help)
	}

	b.WriteString("\n# styles\n")
	names := make([]string, 0, len(c.styles))
	for n := range c.styles {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, "style %-18s %s\n", n, styleWords(c.styles[n]))
	}

	if len(c.presets) > 0 {
		b.WriteString("\n# rules\n")
		for _, p := range c.presets {
			fmt.Fprintf(&b, "rule %-9s %s", p.Action, quoteWord(p.Pattern))
			if p.Mode != filter.Literal {
				fmt.Fprintf(&b, " match=%s", p.Mode)
			}
			if p.Color != (color.RGB{}) {
				fmt.Fprintf(&b, " color=%s", p.Color.Hex())
			}
			b.WriteByte('\n')
		}
	}

	b.WriteString("\n# key bindings\n")
	for _, t := range Tables {
		flag := ""
		if t.Name != "normal" {
			flag = "-T " + t.Name + " "
		}
		for _, bd := range c.orders[t.Name] {
			fmt.Fprintf(&b, "bind %s%-10s %s\n", flag, quoteWord(bd.Spec), bd.Cmd)
		}
	}

	for _, name := range c.tokenOrder {
		fmt.Fprintf(&b, "\ntokens %s {\n", name)
		dumpItems(&b, c.tokens[name].Items)
		b.WriteString("}\n")
	}

	for _, p := range c.profiles {
		fmt.Fprintf(&b, "\nprofile %s {\n", p.Name)
		if len(p.Globs) > 0 {
			fmt.Fprintf(&b, "    match %s\n", strings.Join(p.Globs, " "))
		}
		for _, re := range p.Detects {
			fmt.Fprintf(&b, "    detect %s\n", quoteWord(re.String()))
		}
		dumpItems(&b, p.Items)
		b.WriteString("}\n")
	}
	return b.String()
}

func dumpItems(b *strings.Builder, items []highlight.Item) {
	for _, it := range items {
		if it.Use != "" {
			fmt.Fprintf(b, "    use %s\n", it.Use)
			continue
		}
		r := it.Rule
		fmt.Fprintf(b, "    rule %s %s", quoteWord(r.Re.String()), r.StyleName)
		if r.Group > 0 {
			fmt.Fprintf(b, " group=%d", r.Group)
		}
		b.WriteByte('\n')
	}
}

// quoteWord renders a word so that tokenize reads it back unchanged.
func quoteWord(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\"'#{}\\") {
		return s
	}
	if !strings.Contains(s, "'") {
		// Single quotes are literal, which keeps regexes readable.
		return "'" + s + "'"
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(s[i])
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(s[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}
