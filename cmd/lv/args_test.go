package main

import (
	"flag"
	"io"
	"reflect"
	"strings"
	"testing"
)

func parseForTest(args ...string) (*options, []fileSpec, error) {
	var opt options
	fs := flag.NewFlagSet("lv", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	files, err := parseArgs(fs, &opt, args)
	return &opt, files, err
}

func TestPerFileFlagsBindToTheNextFile(t *testing.T) {
	_, files, err := parseForTest("-r", "r1", "-o", "ERROR", "f1", "-o", "WARN", "-o", "FAIL", "-H", "id", "f2", "f3")
	if err != nil {
		t.Fatal(err)
	}
	want := []fileSpec{
		{path: "f1", rules: "r1", only: []string{"ERROR"}},
		{path: "f2", only: []string{"WARN", "FAIL"}, mark: []string{"id"}},
		{path: "f3"},
	}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("got %+v\nwant %+v", files, want)
	}
}

func TestSessionFlagsGoAnywhere(t *testing.T) {
	opt, files, err := parseForTest("f1", "-w", "f2", "-n")
	if err != nil {
		t.Fatal(err)
	}
	if !opt.wrap || !opt.lineNums || len(files) != 2 {
		t.Errorf("wrap %v, line numbers %v, %d files", opt.wrap, opt.lineNums, len(files))
	}
}

func TestNoFilesMeansStdinWithThePendingFlags(t *testing.T) {
	_, files, err := parseForTest("-N", "DEBUG")
	if err != nil {
		t.Fatal(err)
	}
	want := []fileSpec{{path: "-", never: []string{"DEBUG"}}}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("got %+v", files)
	}
}

func TestEverythingAfterDashDashIsAFile(t *testing.T) {
	_, files, err := parseForTest("-o", "x", "--", "-w", "f2")
	if err != nil {
		t.Fatal(err)
	}
	want := []fileSpec{{path: "-w", only: []string{"x"}}, {path: "f2"}}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("got %+v", files)
	}
}

func TestArgumentErrors(t *testing.T) {
	for _, args := range [][]string{
		{"f1", "-r", "x"},            // nothing for -r to apply to
		{"f1", "-o", "x"},            // same for -o
		{"-r", "a", "-r", "b", "f1"}, // two rules files for one file
		{"-", "f1", "-"},             // stdin twice
	} {
		if _, _, err := parseForTest(args...); err == nil {
			t.Errorf("%s: no error", strings.Join(args, " "))
		}
	}
}
