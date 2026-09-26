package main

import (
	"errors"
	"flag"
	"fmt"
)

// fileSpec is one file to open and the rules it starts with.
type fileSpec struct {
	path              string // "-" is stdin
	rules             string
	only, never, mark []string
}

func (s *fileSpec) empty() bool {
	return s.rules == "" && s.only == nil && s.never == nil && s.mark == nil
}

// errFlagReported is a parse error flag has already printed.
var errFlagReported = errors.New("bad flag")

// onceString is a flag that may be given once per file.
type onceString struct{ p *string }

func (o onceString) String() string {
	if o.p == nil {
		return ""
	}
	return *o.p
}

func (o onceString) Set(v string) error {
	if *o.p != "" {
		return errors.New("given twice for one file")
	}
	*o.p = v
	return nil
}

// parseArgs defines every flag lv accepts and reads the command line.
//
// Rules flags apply to the file named after them, so `-r a.rules f1 -o ERROR
// f2` gives each file its own; every other flag is for the whole session and
// may appear anywhere. flag stops at the first argument that is not a flag,
// which is exactly where one file's flags end: take that file and carry on
// from the next.
func parseArgs(fs *flag.FlagSet, opt *options, args []string) ([]fileSpec, error) {
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
	fs.StringVar(&opt.background, "background", "", "terminal background: auto, dark or light")
	fs.Var(&opt.sets, "set", "override a setting, name=value (repeatable)")
	fs.BoolVar(&opt.dumpConfig, "dump-config", false, "print the effective configuration and exit")
	fs.BoolVar(&opt.listProf, "list-profiles", false, "list syntax profiles and exit")
	fs.BoolVar(&opt.showVer, "V", false, "print version and exit")
	fs.BoolVar(&opt.showVer, "version", false, "print version and exit")

	var pending fileSpec
	fs.Var(onceString{&pending.rules}, "r", "load rules from a file saved earlier, for the next file")
	fs.Var(onceString{&pending.rules}, "rules", "load rules from a file saved earlier, for the next file")
	fs.Var((*stringList)(&pending.only), "o", "show only lines matching this pattern in the next file (repeatable)")
	fs.Var((*stringList)(&pending.only), "only", "show only lines matching this pattern in the next file (repeatable)")
	fs.Var((*stringList)(&pending.never), "N", "never show lines matching this pattern in the next file (repeatable)")
	fs.Var((*stringList)(&pending.never), "never", "never show lines matching this pattern in the next file (repeatable)")
	fs.Var((*stringList)(&pending.mark), "H", "highlight this pattern in the next file (repeatable)")
	fs.Var((*stringList)(&pending.mark), "highlight", "highlight this pattern in the next file (repeatable)")

	var files []fileSpec
	take := func(path string) error {
		if path == "-" {
			for _, f := range files {
				if f.path == "-" {
					return errors.New("stdin given twice")
				}
			}
		}
		spec := pending
		spec.path = path
		files = append(files, spec)
		pending = fileSpec{}
		return nil
	}

	for rest := args; ; {
		if err := fs.Parse(rest); err != nil {
			return nil, errFlagReported
		}
		parsed := len(rest) - fs.NArg()
		afterDashDash := parsed > 0 && rest[parsed-1] == "--"
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		if afterDashDash {
			for _, path := range rest {
				if err := take(path); err != nil {
					return nil, err
				}
			}
			break
		}
		if err := take(rest[0]); err != nil {
			return nil, err
		}
		rest = rest[1:]
	}

	switch {
	case len(files) == 0:
		pending.path = "-"
		files = append(files, pending)
	case !pending.empty():
		return nil, fmt.Errorf("-r, -o, -N and -H apply to the file after them; none follows")
	}
	return files, nil
}
