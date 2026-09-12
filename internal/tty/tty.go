// Package tty reaches the controlling terminal directly.
//
// Standard input and output are often pipes -- `producer | lv`, or lv
// running as a pager -- so anything that has to talk to the terminal itself
// opens it by name rather than trusting the standard streams.
package tty

import (
	"os"

	"golang.org/x/term"
)

// Open returns the controlling terminal, open for reading and writing.
func Open() (*os.File, error) { return os.OpenFile(name, os.O_RDWR, 0) }

// Size reports the terminal's size in character cells.
func Size(f *os.File) (rows, cols int, ok bool) {
	w, h, err := term.GetSize(int(f.Fd()))
	if err != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return h, w, true
}
