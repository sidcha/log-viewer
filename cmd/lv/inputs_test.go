package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sidcha/log-viewer/internal/config"
)

// A single file is how lv has always been used, so its startup errors must
// read exactly as they did before several files could be opened.
func TestOpenInputsSingleFileErrors(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "a.log")
	missing := filepath.Join(dir, "nosuch.log")
	badJSON := filepath.Join(dir, "r.bad")
	badRule := filepath.Join(dir, "r.rule")
	for path, body := range map[string]string{
		log:     "x\n",
		badJSON: "{",
		badRule: `{"version":1,"rules":[{"action":"bogus","pattern":"x"}]}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// A terminal on stdin means nothing was piped in.
	tty, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	stdin := os.Stdin
	os.Stdin = tty
	defer func() { os.Stdin = stdin }()

	cases := []struct {
		name   string
		spec   fileSpec
		forced string
		want   string // the whole message, or its start when it ends in "…"
	}{
		{"missing file", fileSpec{path: missing}, "",
			"open " + missing + ": no such file or directory"},
		{"unknown profile", fileSpec{path: log}, "bogus",
			`unknown profile "bogus" (…`},
		{"missing rules file", fileSpec{path: log, rules: missing}, "",
			"open " + missing + ": no such file or directory"},
		{"unreadable rules file", fileSpec{path: log, rules: badJSON}, "",
			badJSON + ": unexpected end of JSON input"},
		{"bad rule", fileSpec{path: log, rules: badRule}, "",
			`rule 1: unknown action "bogus"…`},
		{"nothing piped in", fileSpec{path: "-"}, "",
			"no input: give a file, or pipe one in"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ins, err := openInputs(cfg, []fileSpec{c.spec}, c.forced)
			for _, in := range ins {
				in.close()
			}
			if err == nil {
				t.Fatal("no error")
			}
			got := err.Error()
			if prefix, ok := strings.CutSuffix(c.want, "…"); ok {
				if !strings.HasPrefix(got, prefix) {
					t.Errorf("got %q, want it to start %q", got, prefix)
				}
			} else if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// With several rules files, a bad rule has to say which file it is in.
func TestOpenInputsNamesTheFileOfABadRule(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "a.log")
	good := filepath.Join(dir, "good.rules")
	bad := filepath.Join(dir, "bad.rules")
	for path, body := range map[string]string{
		log:  "x\n",
		good: `{"version":1,"rules":[{"action":"only","pattern":"x"}]}`,
		bad:  `{"version":1,"rules":[{"action":"bogus","pattern":"x"}]}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, err = openInputs(cfg, []fileSpec{{path: log, rules: good}, {path: log, rules: bad}}, "")
	if err == nil || !strings.HasPrefix(err.Error(), bad+": rule 1: ") {
		t.Errorf("got %v, want it to start %q", err, bad+": rule 1: ")
	}
}
