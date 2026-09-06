//go:build !unix

package termcolor

import (
	"os"
	"time"
)

// query has no portable form outside unix, so lv falls back to COLORFGBG and
// the background setting there.
func query(tty *os.File, timeout time.Duration) (Colors, bool) { return Colors{}, false }
