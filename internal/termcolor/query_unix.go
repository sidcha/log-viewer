//go:build unix

package termcolor

import (
	"os"
	"strings"
	"time"

	"github.com/sidcha/log-viewer/internal/color"
	"golang.org/x/sys/unix"
)

// query asks the terminal for its foreground and background with OSC 10 and
// OSC 11.
//
// The terminal has to be in raw mode to read the reply, and it has to go back
// exactly as it was afterwards: this runs before the screen library starts, and
// leaving the tty altered would wreck everything after it.
func query(tty *os.File, timeout time.Duration) (Colors, bool) {
	fd := int(tty.Fd())

	saved, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return Colors{}, false // not a terminal
	}
	defer unix.IoctlSetTermios(fd, ioctlSetTermios, saved)

	raw := *saved
	raw.Lflag &^= unix.ICANON | unix.ECHO
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 1 // 100ms granularity, used only as a backstop
	if err := unix.IoctlSetTermios(fd, ioctlSetTermios, &raw); err != nil {
		return Colors{}, false
	}

	// Ask for both, then read whatever comes back: the replies arrive in
	// order, but a terminal that only knows one will answer just that.
	if _, err := tty.WriteString("\x1b]10;?\x1b\\\x1b]11;?\x1b\\"); err != nil {
		return Colors{}, false
	}

	reply := readReply(tty, timeout)
	if reply == "" {
		return Colors{}, false
	}

	var c Colors
	for _, chunk := range splitOSC(reply) {
		rgb, ok := parseOSC(chunk)
		if !ok {
			continue
		}
		switch {
		case strings.Contains(chunk, "]10;"):
			c.Foreground = rgb
		case strings.Contains(chunk, "]11;"):
			c.Background = rgb
			c.Known = true
		}
	}
	if !c.Known {
		return Colors{}, false
	}
	if c.Foreground == (color.RGB{}) {
		// No foreground reported: assume the obvious contrast for this
		// background, which is all the derived tints need.
		if c.Background.Dark() {
			c.Foreground = color.RGB{R: 255, G: 255, B: 255}
		}
	}
	c.Dark = c.Background.Dark()
	return c, true
}

// readReply collects bytes until the terminal stops talking or the deadline
// passes. A terminal that ignores the query costs exactly this wait, once.
func readReply(tty *os.File, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 256)
	var out strings.Builder

	for time.Now().Before(deadline) {
		n, err := tty.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			// Two replies, each ending in ST or BEL.
			if strings.Count(out.String(), "\x1b\\")+strings.Count(out.String(), "\a") >= 2 {
				break
			}
			continue
		}
		if err != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return out.String()
}

// splitOSC breaks a run of replies apart at their introducers.
func splitOSC(s string) []string {
	var out []string
	for {
		i := strings.Index(s, "\x1b]")
		if i < 0 {
			break
		}
		s = s[i+1:]
		end := len(s)
		if j := strings.Index(s, "\x1b\\"); j >= 0 && j < end {
			end = j
		}
		if j := strings.IndexByte(s, '\a'); j >= 0 && j < end {
			end = j
		}
		out = append(out, s[:end])
		s = s[end:]
	}
	return out
}
