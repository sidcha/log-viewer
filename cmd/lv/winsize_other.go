//go:build !unix

package main

import "os"

// winsize is unavailable here, which only means quit-if-one-screen never
// triggers: the viewer opens as it always has.
func winsize(tty *os.File) (rows, cols int, ok bool) { return 0, 0, false }
