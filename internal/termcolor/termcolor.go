// Package termcolor finds out what colours the terminal is actually using.
//
// A viewer cannot pick a readable tint for the cursor line from a fixed hex
// value. #1c1c1c is a clear band on a black terminal, invisible on a terminal
// whose background happens to be #1c1c1c, and a dark smear on a light one.
// Absolute colours only ever suit the theme they were chosen against.
//
// So lv asks. Terminals answer OSC 10 and OSC 11 with their real foreground
// and background, which is enough to derive tints that sit a fixed distance
// from whatever the user is looking at, on any theme. Where the terminal will
// not say, COLORFGBG and then an explicit setting decide, and failing all of
// that lv assumes dark.
package termcolor

import (
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sidcha/log-viewer/internal/color"
)

// Colors is what lv managed to learn about the terminal.
type Colors struct {
	Foreground color.RGB
	Background color.RGB
	// Known is true when the terminal answered, so tints can be derived from
	// its real colours rather than guessed.
	Known bool
	// Dark is the best available answer either way.
	Dark bool
}

// Detect works out the terminal's colours.
//
// forced is the `background` setting: "dark" or "light" short-circuits the
// query, which matters for terminals that garble it or over a link too slow to
// wait on.
func Detect(tty *os.File, forced string, timeout time.Duration) Colors {
	switch strings.ToLower(forced) {
	case "dark":
		return Colors{Dark: true}
	case "light":
		return Colors{Dark: false}
	}

	if tty != nil {
		if c, ok := query(tty, timeout); ok {
			return c
		}
	}
	if dark, ok := fromEnv(os.Getenv("COLORFGBG")); ok {
		return Colors{Dark: dark}
	}
	// Dark is the safer assumption: a light tint on a light terminal is
	// invisible, while most terminals in use are dark.
	return Colors{Dark: true}
}

// fromEnv reads COLORFGBG, which several terminals set to "fg;bg" as palette
// indices. Anything below 8, plus 0, counts as dark.
func fromEnv(v string) (dark bool, ok bool) {
	if v == "" {
		return false, false
	}
	parts := strings.Split(v, ";")
	if len(parts) < 2 {
		return false, false
	}
	bg, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return false, false
	}
	// 0-6 and 8 are dark palette slots; 7 and 9-15 are light.
	return bg < 7 || bg == 8, true
}

// parseOSC pulls an rgb: colour out of an OSC 10/11 reply.
//
// Replies look like ESC ] 11 ; rgb:rrrr/gggg/bbbb ST, with components of one to
// four hex digits, which have to be scaled rather than truncated.
func parseOSC(reply string) (color.RGB, bool) {
	i := strings.Index(reply, "rgb:")
	if i < 0 {
		return color.RGB{}, false
	}
	body := reply[i+len("rgb:"):]
	for j, r := range body {
		if r != '/' && !isHex(byte(r)) {
			body = body[:j]
			break
		}
	}

	parts := strings.Split(body, "/")
	if len(parts) != 3 {
		return color.RGB{}, false
	}
	var out [3]uint8
	for k, p := range parts {
		if p == "" || len(p) > 4 {
			return color.RGB{}, false
		}
		v, err := strconv.ParseUint(p, 16, 32)
		if err != nil {
			return color.RGB{}, false
		}
		max := float64(uint64(1)<<(4*len(p))) - 1
		out[k] = uint8(math.Round(float64(v) / max * 255))
	}
	return color.RGB{R: out[0], G: out[1], B: out[2]}, true
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
