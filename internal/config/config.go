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

// Setting describes one `set` key.
type Setting struct {
	Name string
	Kind kind
	Def  string
	Enum []string
	Help string
}

// Settings is the complete set of recognised options. Anything else in a
// `set` directive is an error, so typos surface at startup instead of silently
// doing nothing.
var Settings = []Setting{
	{"follow", kBool, "off", nil, "start pinned to the end of the file"},
	{"wrap", kBool, "off", nil, "wrap long lines instead of truncating"},
	{"line-numbers", kBool, "off", nil, "show a source line-number gutter"},
	{"ansi", kBool, "on", nil, "draw the colours a log was captured with"},
	{"status-line", kEnum, "auto", []string{"auto", "on", "off"}, "when to draw the status line"},
	{"alt-screen", kBool, "on", nil, "draw on the alternate screen, restoring the terminal on exit"},
	{"quit-if-one-screen", kBool, "off", nil, "print and exit when the whole input fits on the screen"},
	{"ask-save", kEnum, "auto", []string{"auto", "on", "off"}, "ask before quitting whether to keep piped input"},
	{"ask-save-rules", kBool, "on", nil, "ask before quitting whether to save the rules"},
	{"case", kEnum, "smart", []string{"smart", "sensitive", "insensitive"}, "pattern case sensitivity"},
	{"tab-width", kInt, "8", nil, "columns per tab stop"},
	{"scroll-step", kInt, "1", nil, "lines moved by up/down"},
	{"h-scroll-step", kInt, "8", nil, "columns moved by left/right"},
	{"page-overlap", kInt, "2", nil, "lines kept on screen when paging"},
	{"watch-backstop", kDur, "2s", nil, "poll interval alongside kernel change notifications"},
	{"watch-fallback", kDur, "200ms", nil, "poll interval when notifications are unavailable"},
	{"scan-poll", kDur, "200ms", nil, "how often a caught-up filter scan looks for new data"},
	{"block-size", kInt, "1048576", nil, "bytes read per syscall"},
	{"max-line-bytes", kInt, "1048576", nil, "how much of one line is retained"},
	{"max-scan-bytes", kInt, "4096", nil, "bytes of a line examined for highlighting"},
	{"index-stride", kInt, "1024", nil, "lines between index checkpoints"},
	{"max-checkpoints", kInt, "1048576", nil, "checkpoint ceiling before the stride doubles"},
	{"page-cache-advise", kEnum, "auto", []string{"auto", "on", "off"}, "hand scanned pages back to the kernel"},
	{"profile", kStr, "", nil, "force a syntax profile instead of detecting one"},
	{"mouse", kBool, "on", nil, "enable mouse wheel scrolling"},
	{"gutter", kStr, "│ ", nil, "separator drawn after the line-number gutter"},
	{"message-timeout", kDur, "4s", nil, "how long a status message stays up"},
	{"scroll-off", kInt, "3", nil, "lines of context kept above and below the cursor"},
	{"cursor-line", kBool, "on", nil, "mark the line the cursor is on"},
	{"clipboard-command", kStr, "", nil, "external clipboard program; empty uses the terminal escape"},
	{"stop-producer", kBool, "on", nil, "on exit, stop the process piping into lv"},
	{"background", kEnum, "auto", []string{"auto", "dark", "light"}, "terminal background; auto asks the terminal"},
}

var settingIndex = func() map[string]*Setting {
	m := make(map[string]*Setting, len(Settings))
	for i := range Settings {
		m[Settings[i].Name] = &Settings[i]
	}
	return m
}()

// Bind associates a key sequence with a command.
type Bind struct {
	Keys   []Key
	Spec   string
	Cmd    Command
	Visual bool
}

// Preset is a rule declared in configuration rather than added at runtime.
type Preset struct {
	Action  filter.Action
	Mode    filter.Mode
	Pattern string
	// Color is set when the file pins one; otherwise the viewer assigns it.
	Color filter.Color
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
	values   map[string]string
	root     *keyNode
	vroot    *keyNode
	order    []Bind
	vorder   []Bind
	styles   map[string]tcell.Style
	profiles []*highlight.Profile
	presets  []Preset
	loaded   []string
	sourced  map[string]bool
	// background is the resolved terminal background, selecting which
	// `background dark { }` blocks apply.
	background string
	// styleOrigins records which file defined each style, so a colour derived
	// from the terminal never overrides one the user chose.
	styleOrigins map[string]string
	// origins records which file last set each value, so a surprising
	// effective value can be traced back to the layer responsible.
	origins map[string]string
}

// New returns a configuration holding only the built-in defaults for settings.
// Bindings, styles and profiles come from the embedded default file.
func New() *Config {
	c := &Config{
		values:       make(map[string]string, len(Settings)),
		root:         &keyNode{},
		vroot:        &keyNode{},
		styles:       make(map[string]tcell.Style),
		sourced:      make(map[string]bool),
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
	v := strings.ToLower(c.values[name])
	return v == "on" || v == "true" || v == "yes" || v == "1"
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

// Set overrides a setting, validating it the same way the parser does. Command
// line flags go through here.
func (c *Config) Set(name, value string) error {
	s, ok := settingIndex[name]
	if !ok {
		return fmt.Errorf("unknown setting %q", name)
	}
	if err := validate(s, value); err != nil {
		return err
	}
	c.values[name] = value
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

// Command returns the command bound to a single key, for callers that do not
// need sequence handling.
func (c *Config) Command(k Key) (Command, bool) {
	cmd, m := c.Lookup([]Key{k})
	return cmd, m == Full
}

// Binds lists normal-mode bindings in declaration order, for the help overlay.
func (c *Config) Binds() []Bind { return c.order }

// VisualBinds lists visual-mode bindings.
func (c *Config) VisualBinds() []Bind { return c.vorder }

// Background is the resolved terminal background, "dark" or "light".
func (c *Config) Background() string { return c.background }

// SetDerivedStyle installs a style computed from the terminal's own colours,
// unless a configuration file already defined one. What the user wrote always
// wins over what lv worked out.
func (c *Config) SetDerivedStyle(name string, st tcell.Style) bool {
	if origin, ok := c.styleOrigins[name]; ok && origin != "<builtin>" {
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

	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return c.Parse(path, string(b))
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
		if err := c.Set(w[1], w[2]); err != nil {
			return st.errf("%v", err)
		}
		c.origins[w[1]] = st.File

	case "bind":
		if len(w) < 3 {
			return st.errf("usage: bind <key> <command> [args...]")
		}
		seq, err := ParseSequence(w[1])
		if err != nil {
			return st.errf("%v", err)
		}
		cmd, err := ParseCommand(w[2:])
		if err != nil {
			return st.errf("%v", err)
		}
		c.bindSeq(seq, w[1], cmd)

	case "bind-visual":
		if len(w) < 3 {
			return st.errf("usage: bind-visual <key> <command> [args...]")
		}
		seq, err := ParseSequence(w[1])
		if err != nil {
			return st.errf("%v", err)
		}
		cmd, err := ParseCommand(w[2:])
		if err != nil {
			return st.errf("%v", err)
		}
		c.bindVisual(seq, w[1], cmd)

	case "unbind-visual":
		if len(w) != 2 {
			return st.errf("usage: unbind-visual <key>")
		}
		seq, err := ParseSequence(w[1])
		if err != nil {
			return st.errf("%v", err)
		}
		c.unbindVisual(seq)

	case "unbind":
		if len(w) != 2 {
			return st.errf("usage: unbind <key>")
		}
		seq, err := ParseSequence(w[1])
		if err != nil {
			return st.errf("%v", err)
		}
		c.unbindSeq(seq)

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
		p := Preset{Action: action, Pattern: w[2]}
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
		path := expandHome(w[1])
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

func (c *Config) block(head stmt, body []stmt) error {
	// A background block holds the parts of a theme that only make sense
	// against a dark or a light terminal.
	if head.Words[0] == "background" {
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

	if head.Words[0] != "profile" || len(head.Words) != 2 {
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
			p.Detect = re

		case "clear":
			p.Rules = nil

		case "rule":
			if len(w) < 3 {
				return st.errf("usage: rule <regex> <style> [group=N]")
			}
			re, err := regexp.Compile(w[1])
			if err != nil {
				return st.errf("%v", err)
			}
			r := highlight.Rule{Re: re, StyleName: w[2]}
			for _, extra := range w[3:] {
				k, v, ok := strings.Cut(extra, "=")
				if !ok || k != "group" {
					return st.errf("unknown rule option %q", extra)
				}
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 {
					return st.errf("bad group %q", v)
				}
				r.Group = n
			}
			p.Rules = append(p.Rules, r)

		default:
			return st.errf("unknown profile directive %q", w[0])
		}
	}
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
	return nil
}

func validate(s *Setting, value string) error {
	switch s.Kind {
	case kBool:
		switch strings.ToLower(value) {
		case "on", "off", "true", "false", "yes", "no", "1", "0":
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

func parseRuleColor(v string) (filter.Color, error) {
	c, err := parseColor(v)
	if err != nil {
		return filter.Color{}, err
	}
	r, g, b := c.RGB()
	return filter.Color{R: uint8(r), G: uint8(g), B: uint8(b)}, nil
}

func expandHome(p string) string {
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
		fmt.Fprintf(&b, "set %-18s %-12s # %s\n", s.Name, quoteWord(c.values[s.Name]), s.Help)
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
			if p.Color != (filter.Color{}) {
				fmt.Fprintf(&b, " color=#%02x%02x%02x", p.Color.R, p.Color.G, p.Color.B)
			}
			b.WriteByte('\n')
		}
	}

	b.WriteString("\n# key bindings\n")
	for _, bd := range c.order {
		fmt.Fprintf(&b, "bind %-10s %s\n", quoteWord(bd.Spec), bd.Cmd)
	}
	for _, bd := range c.vorder {
		fmt.Fprintf(&b, "bind-visual %-10s %s\n", quoteWord(bd.Spec), bd.Cmd)
	}

	for _, p := range c.profiles {
		fmt.Fprintf(&b, "\nprofile %s {\n", p.Name)
		if len(p.Globs) > 0 {
			fmt.Fprintf(&b, "    match %s\n", strings.Join(p.Globs, " "))
		}
		if p.Detect != nil {
			fmt.Fprintf(&b, "    detect %s\n", quoteWord(p.Detect.String()))
		}
		for _, r := range p.Rules {
			fmt.Fprintf(&b, "    rule %s %s", quoteWord(r.Re.String()), r.StyleName)
			if r.Group > 0 {
				fmt.Fprintf(&b, " group=%d", r.Group)
			}
			b.WriteByte('\n')
		}
		b.WriteString("}\n")
	}
	return b.String()
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
