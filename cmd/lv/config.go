package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/sidcha/log-viewer/internal/config"
)

// runConfig implements `lv config`, which reads and writes the same cascading
// files the viewer loads at startup.
//
//	lv config --list                 every setting and its effective value
//	lv config <name>                 print one value
//	lv config <name> <value>         set one value
//	lv config --unset <name>         remove one value
//	lv config --edit                 open the file in $EDITOR
//
// Writes go to the per-user file unless --system or --local says otherwise.
// Git defaults writes to the repository, but lv's narrowest level is the
// working directory, and silently leaving a file behind in whichever directory
// someone happened to be in would surprise people.
func runConfig(args []string) int {
	var (
		list       bool
		unset      bool
		edit       bool
		showOrigin bool
		system     bool
		global     bool
		local      bool
	)

	fs := flag.NewFlagSet("lv config", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { configUsage() }
	fs.BoolVar(&list, "list", false, "list all settings and their values")
	fs.BoolVar(&list, "l", false, "list all settings and their values")
	fs.BoolVar(&unset, "unset", false, "remove a setting")
	fs.BoolVar(&edit, "edit", false, "open the configuration file in $EDITOR")
	fs.BoolVar(&edit, "e", false, "open the configuration file in $EDITOR")
	fs.BoolVar(&showOrigin, "show-origin", false, "with --list, show where each value came from")
	fs.BoolVar(&system, "system", false, "use the system file")
	fs.BoolVar(&global, "global", false, "use the per-user file (default for writes)")
	fs.BoolVar(&local, "local", false, "use the per-directory file")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	level := config.Global
	switch {
	case system && (global || local), global && local:
		fmt.Fprintln(os.Stderr, "lv: give at most one of --system, --global, --local")
		return 2
	case system:
		level = config.System
	case local:
		level = config.Local
	}

	rest := fs.Args()

	switch {
	case edit:
		return editConfig(level)
	case unset:
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "lv: --unset takes one setting name")
			return 2
		}
		path, err := config.Unset(level, rest[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "lv: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "unset %s in %s\n", rest[0], path)
		return 0
	case list:
		return listConfig(showOrigin)
	}

	switch len(rest) {
	case 1:
		return getConfig(rest[0])
	case 2:
		path, err := config.Write(level, rest[0], rest[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "lv: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "set %s = %s in %s\n", rest[0], rest[1], path)
		return 0
	default:
		configUsage()
		return 2
	}
}

func loadForConfig() (*config.Config, int) {
	c, err := config.Load(config.LoadOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "lv: %v\n", err)
		return nil, 1
	}
	return c, 0
}

func getConfig(name string) int {
	if _, ok := config.LookupSetting(name); !ok {
		fmt.Fprintf(os.Stderr, "lv: unknown setting %q\n", name)
		return 1
	}
	c, code := loadForConfig()
	if c == nil {
		return code
	}
	fmt.Println(c.Str(name))
	return 0
}

func listConfig(showOrigin bool) int {
	c, code := loadForConfig()
	if c == nil {
		return code
	}
	width := 0
	for _, s := range config.Settings {
		if len(s.Name) > width {
			width = len(s.Name)
		}
	}
	for _, s := range config.Settings {
		if showOrigin {
			fmt.Printf("%-*s  %-12s  %s\n", width, s.Name, c.Str(s.Name), c.Origin(s.Name))
			continue
		}
		fmt.Printf("%-*s  %s\n", width, s.Name, c.Str(s.Name))
	}
	return 0
}

func editConfig(level config.Level) int {
	path, err := config.Path(level)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lv: %v\n", err)
		return 1
	}

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		fmt.Fprintf(os.Stderr, "lv: set $EDITOR, or edit %s directly\n", path)
		return 1
	}

	// $EDITOR is split the way a config file line is, so a quoted path with a
	// space in it stays one word. The file need not exist yet; the editor will
	// create it.
	parts, err := config.ParseLine(editor)
	if err != nil || len(parts) == 0 {
		fmt.Fprintf(os.Stderr, "lv: $EDITOR %q: %v\n", editor, err)
		return 1
	}
	parts = append(parts, path)
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "lv: %v\n", err)
		return 1
	}

	// Refuse to leave a file behind that lv itself would reject at startup.
	if _, err := config.Load(config.LoadOptions{}); err != nil {
		fmt.Fprintf(os.Stderr, "lv: %v\n", err)
		return 1
	}
	return 0
}

func configUsage() {
	fmt.Fprint(os.Stderr, `usage:
  lv config --list [--show-origin]   show every setting and its value
  lv config <name>                   print one value
  lv config <name> <value>           set one value
  lv config --unset <name>           remove one value
  lv config --edit                   open the file in $EDITOR

levels (files are read in this order, each overriding the last):
  --system   `+config.SystemPath()+`
  --global   per user, the default for writes
  --local    ./`+config.LocalName+`
`)
}
