// Package clipboard copies text to the system clipboard from a terminal.
package clipboard

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sidcha/log-viewer/internal/tty"
)

// Copy puts text on the clipboard.
//
// The default route is the OSC 52 escape sequence, which asks the terminal
// emulator to do it. That is the one mechanism that works when lv is running
// over ssh or inside a container: there is no X display or Wayland socket to
// reach, but the terminal on the far end of the connection has a clipboard.
//
// command, when given, replaces that with an external program such as wl-copy
// or pbcopy, for terminals that do not implement OSC 52 or have it turned off.
// It is the program and its arguments, already split.
//
// Terminals impose their own limits on OSC 52 payloads and mostly fail
// silently past them, which would look like a yank that did nothing, so text
// longer than maxOSC52 bytes is refused with a pointer to the command instead.
func Copy(text string, command []string, maxOSC52 int) error {
	if len(command) > 0 {
		return runCommand(text, command)
	}
	if len(text) > maxOSC52 {
		return fmt.Errorf("%d bytes is too much for the terminal clipboard escape; set clipboard-command", len(text))
	}
	return writeOSC52(text)
}

func runCommand(text string, parts []string) error {
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdin = strings.NewReader(text)
	if out, err := cmd.CombinedOutput(); err != nil {
		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			return fmt.Errorf("%s: %v: %s", parts[0], err, trimmed)
		}
		return fmt.Errorf("%s: %v", parts[0], err)
	}
	return nil
}

// writeOSC52 sends the sequence straight to the controlling terminal rather
// than to stdout, which may be a pipe, and bypasses the screen library so the
// escape is not mistaken for something to draw.
func writeOSC52(text string) error {
	term, err := tty.Open()
	if err != nil {
		return err
	}
	defer term.Close()

	_, err = term.WriteString(Sequence(text))
	return err
}

// Sequence returns the escape that puts text on the clipboard.
func Sequence(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
}
