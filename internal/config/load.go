package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed default.conf
var defaultConf string

// Default returns the embedded built-in configuration text.
func Default() string { return defaultConf }

// Level is one layer of configuration.
//
// Layers cascade the way git's do: every one that exists is read, in order, and
// a later setting replaces an earlier one. A per-directory file can therefore
// adjust one option without restating everything above it.
type Level int

const (
	// Builtin is the configuration embedded in the binary.
	Builtin Level = iota
	// System applies to every user on the machine.
	System
	// Global applies to this user.
	Global
	// Local applies to the working directory.
	Local
)

func (l Level) String() string {
	switch l {
	case Builtin:
		return "builtin"
	case System:
		return "system"
	case Global:
		return "global"
	default:
		return "local"
	}
}

// ParseLevel reads a level name.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(s) {
	case "system":
		return System, nil
	case "global":
		return Global, nil
	case "local":
		return Local, nil
	}
	return Global, fmt.Errorf("unknown level %q, want system, global or local", s)
}

// SystemPath is the machine-wide configuration file: /etc/lv/config, or
// %ProgramData%\lv\config on Windows.
func SystemPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "lv", "config")
	}
	return "/etc/lv/config"
}

// globalDir is where the per-user file lives. Everywhere but Windows that is
// the XDG location, including macOS, where a terminal program's configuration
// conventionally sits beside everyone else's in ~/.config.
func globalDir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return x, nil
	}
	if runtime.GOOS == "windows" {
		return os.UserConfigDir()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

// LocalName is the per-directory configuration file.
const LocalName = ".lvconfig"

// Path returns the file backing a level.
func Path(l Level) (string, error) {
	switch l {
	case System:
		return SystemPath(), nil
	case Global:
		dir, err := globalDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "lv", "config"), nil
	case Local:
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Join(wd, LocalName), nil
	}
	return "", fmt.Errorf("%s has no file", l)
}

// Layers lists the cascading layers, lowest priority first.
func Layers() []Level { return []Level{System, Global, Local} }

// LoadOptions controls configuration discovery.
type LoadOptions struct {
	// Path, if set, is read last and so wins over every layer.
	Path string
	// NoUser skips every layer outside the binary.
	NoUser bool
	// Background selects which `background dark|light { }` blocks apply.
	// Empty skips them all, which is what the first pass wants: it only needs
	// the settings, to find out which background to resolve.
	Background string
}

// Load builds the effective configuration by cascading the layers.
func Load(opt LoadOptions) (*Config, error) {
	c := New()
	c.background = opt.Background
	if err := c.Parse("<builtin>", defaultConf); err != nil {
		return nil, fmt.Errorf("built-in config: %w", err)
	}

	if !opt.NoUser {
		for _, l := range Layers() {
			p, err := Path(l)
			if err != nil {
				continue // no home directory, no working directory: skip
			}
			if err := c.readIfPresent(p); err != nil {
				return nil, err
			}
		}
		if p := os.Getenv("LV_CONFIG"); p != "" {
			if err := c.ParseFile(p); err != nil {
				return nil, err
			}
		}
	}

	if opt.Path != "" {
		if err := c.ParseFile(opt.Path); err != nil {
			return nil, err
		}
	}

	if err := c.Resolve(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) readIfPresent(path string) error {
	if fi, err := os.Stat(path); err != nil || fi.IsDir() {
		return nil
	}
	return c.ParseFile(path)
}

// Write stores a setting in a level's file, replacing any existing entry for
// the same key and leaving the rest of the file, comments included, untouched.
func Write(l Level, name, value string) (string, error) {
	spec, ok := settingIndex[name]
	if !ok {
		return "", fmt.Errorf("unknown setting %q", name)
	}
	if err := validate(spec, value); err != nil {
		return "", err
	}

	path, err := Path(l)
	if err != nil {
		return "", err
	}
	lines, err := readLines(path)
	if err != nil {
		return "", err
	}

	entry := fmt.Sprintf("set %s %s", name, quoteWord(value))
	replaced := false
	for i, set := range topLevelSettings(lines) {
		if set == name {
			lines[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, entry)
	}
	return path, writeLines(path, lines)
}

// Unset removes a setting from a level's file.
func Unset(l Level, name string) (string, error) {
	if _, ok := settingIndex[name]; !ok {
		return "", fmt.Errorf("unknown setting %q", name)
	}
	path, err := Path(l)
	if err != nil {
		return "", err
	}
	lines, err := readLines(path)
	if err != nil {
		return "", err
	}

	sets := topLevelSettings(lines)
	out := lines[:0]
	found := false
	for i, line := range lines {
		if sets[i] == name {
			found = true
			continue
		}
		out = append(out, line)
	}
	if !found {
		return path, fmt.Errorf("%s is not set in %s", name, l)
	}
	return path, writeLines(path, out)
}

// topLevelSettings returns, for each line, the setting it assigns, or "".
//
// Only lines outside any block count. A `set` inside `background light { }`
// applies to one kind of terminal; `lv config` addresses the unconditional
// value, and rewriting the conditional one instead would change the wrong
// thing.
func topLevelSettings(lines []string) []string {
	out := make([]string, len(lines))
	depth := 0
	for i, line := range lines {
		f := strings.Fields(line)
		switch {
		case len(f) == 0 || strings.HasPrefix(f[0], "#"):
		case f[0] == "}":
			depth = max(0, depth-1)
		case f[len(f)-1] == "{":
			depth++
		case depth == 0 && len(f) >= 2 && f[0] == "set":
			out[i] = f[1]
		}
	}
	return out
}

func readLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	text := strings.TrimSuffix(string(b), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func writeLines(path string, lines []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := ""
	if len(lines) > 0 {
		body = strings.Join(lines, "\n") + "\n"
	}
	// Write and rename so an interrupted save cannot truncate the file.
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}
