//go:build unix

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// winsize is the terminal's size in character cells.
//
// It has to be known before the screen library starts, because whether the
// whole input fits on one screen decides whether the screen is taken over at
// all. Asking the tty directly is the only way to find out that costs nothing
// and disturbs nothing.
func winsize(tty *os.File) (rows, cols int, ok bool) {
	ws, err := unix.IoctlGetWinsize(int(tty.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Row == 0 || ws.Col == 0 {
		return 0, 0, false
	}
	return int(ws.Row), int(ws.Col), true
}
