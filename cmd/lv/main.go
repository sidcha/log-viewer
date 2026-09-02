// Command lv is a terminal log viewer for files that do not fit in memory.
package main

import (
	"fmt"
	"os"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

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
	usage()
	os.Exit(2)
}

func usage() {
	fmt.Fprint(os.Stderr, `lv - log viewer for files larger than memory

usage:
  lv [options] <file>
  <producer> | lv [options]
`)
}
