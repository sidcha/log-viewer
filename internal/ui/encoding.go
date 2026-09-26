package ui

import "unicode/utf8"

// byteCensus counts, over the lines on screen, the bytes that no text file
// would hold: NULs, control characters other than tab, CR and ESC (which
// starts every ANSI colour), and bytes that are not valid UTF-8.
type byteCensus struct {
	total, odd int
	nul        bool
}

// binaryShare is the fraction of odd bytes, one in ten, past which the screen
// is binary rather than a log with some noise in it.
const binaryShare = 10

// add counts the first limit bytes of text; highlighting looks no further
// either, so a huge line costs no more than a short one.
func (c *byteCensus) add(text []byte, limit int) {
	if len(text) > limit {
		text = text[:limit]
	}
	for i := 0; i < len(text); {
		b := text[i]
		switch {
		case b == 0:
			c.nul = true
			c.odd++
			i++
		case b < 0x20 && b != '\t' && b != '\r' && b != 0x1b, b == 0x7f:
			c.odd++
			i++
		case b < utf8.RuneSelf:
			i++
		default:
			r, size := utf8.DecodeRune(text[i:])
			if r == utf8.RuneError && size <= 1 {
				c.odd++
			}
			i += size
		}
		c.total++
	}
}

// encoding names what the counted bytes are: utf-8, binary, or "" when
// nothing was counted.
func (c *byteCensus) encoding() string {
	switch {
	case c.total == 0:
		return ""
	case c.nul || c.odd*binaryShare > c.total:
		return "binary"
	default:
		return "utf-8"
	}
}
