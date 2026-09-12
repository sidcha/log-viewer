// Package ansi reads the terminal escape sequences a log may already carry.
//
// Logs captured from a serial console, a build, or anything that writes to a
// tty arrive with their colours still in them. Those bytes are not text: drawn
// literally they are noise, and counted as columns they push everything out of
// alignment. lv hides every escape sequence it recognises and turns the colour
// ones back into colour, so a captured log looks the way it did when it was
// produced.
//
// Only SGR — the colour and attribute sequence — is interpreted. The rest of
// the repertoire moves a cursor around a screen that does not exist here, so
// those sequences are recognised only well enough to be skipped.
package ansi

import (
	"github.com/gdamore/tcell/v2"
)

// Attrs is the SGR state in force over a run of bytes.
//
// A colour is tcell.ColorDefault when the sequence did not set one, and the
// attribute flags are only ever turned on: where the file said nothing, what
// lv's own styling decided stands.
type Attrs struct {
	Fg, Bg    tcell.Color
	Bold      bool
	Dim       bool
	Italic    bool
	Underline bool
	Blink     bool
	Reverse   bool
	Strike    bool
}

// Span is a byte range of a line and the state that applies to it.
type Span struct {
	Start, End int
	Attrs      Attrs
}

// IsZero reports whether the state would change nothing.
func (a Attrs) IsZero() bool { return a == Attrs{} }

// Apply lays the state over an existing style.
func (a Attrs) Apply(st tcell.Style) tcell.Style {
	if a.Fg != tcell.ColorDefault {
		st = st.Foreground(a.Fg)
	}
	if a.Bg != tcell.ColorDefault {
		st = st.Background(a.Bg)
	}
	if a.Bold {
		st = st.Bold(true)
	}
	if a.Dim {
		st = st.Dim(true)
	}
	if a.Italic {
		st = st.Italic(true)
	}
	if a.Underline {
		st = st.Underline(true)
	}
	if a.Blink {
		st = st.Blink(true)
	}
	if a.Reverse {
		st = st.Reverse(true)
	}
	if a.Strike {
		st = st.StrikeThrough(true)
	}
	return st
}

// Spans appends one span per run of bytes sharing a non-default SGR state.
//
// Offsets are into line, and the escape sequences themselves are left out of
// the runs they introduce -- they occupy no columns, so a span that covered
// them would map onto the wrong cells.
//
// State does not carry over from the line before. Reaching a line in the
// middle of a large file would otherwise mean reading everything above it to
// find out what colour it starts in, which is not a price a viewer of
// unbounded files can pay. Nearly everything that colours its output closes
// the sequence on the same line.
func Spans(line []byte, dst []Span) []Span {
	var (
		cur   Attrs
		start = -1
		// Parameters are parsed in place; a real sequence has a handful.
		scratch [32]int
	)
	flush := func(end int) {
		if start >= 0 && end > start && !cur.IsZero() {
			// A sequence that changed nothing -- a cursor move, a title, an
			// escape lv does not act on -- splits a run that is really one.
			// The bytes between the pieces occupy no columns, so joining them
			// back up costs nothing and keeps the spans meaning what they say.
			if n := len(dst); n > 0 && dst[n-1].Attrs == cur && dst[n-1].End <= start {
				dst[n-1].End = end
			} else {
				dst = append(dst, Span{Start: start, End: end, Attrs: cur})
			}
		}
		start = -1
	}

	for i := 0; i < len(line); {
		if line[i] != esc {
			if start < 0 {
				start = i
			}
			i++
			continue
		}
		flush(i)
		next, ok := Skip(line, i)
		if !ok {
			// A lone escape byte: not a sequence, so treat it as content.
			if start < 0 {
				start = i
			}
			i++
			continue
		}
		if params, colon, isSGR := sgrParams(line[i:next], scratch[:0]); isSGR {
			cur = applySGR(cur, params, colon)
		}
		i = next
	}
	flush(len(line))
	return dst
}

const (
	esc = 0x1b
	bel = 0x07
)

// Skip returns the index just past an escape sequence beginning at i, and
// whether one was recognised.
//
// Everything with a defined shape is covered, not just the sequences lv acts
// on, because the alternative is drawing the remains of one as text.
func Skip(text []byte, i int) (int, bool) {
	if i >= len(text) || text[i] != esc || i+1 >= len(text) {
		return i, false
	}

	switch text[i+1] {
	case '[': // CSI: parameters, intermediates, one final byte
		j := i + 2
		for j < len(text) && text[j] >= 0x30 && text[j] <= 0x3f {
			j++
		}
		for j < len(text) && text[j] >= 0x20 && text[j] <= 0x2f {
			j++
		}
		if j < len(text) && text[j] >= 0x40 && text[j] <= 0x7e {
			return j + 1, true
		}
		return i, false

	case ']', 'P', 'X', '^', '_':
		// OSC and friends: a string, ended by BEL or by ST (ESC \).
		for j := i + 2; j < len(text); j++ {
			if text[j] == bel {
				return j + 1, true
			}
			if text[j] == esc && j+1 < len(text) && text[j+1] == '\\' {
				return j + 2, true
			}
		}
		return i, false
	}

	// Anything else: optional intermediates, then a single final byte.
	j := i + 1
	for j < len(text) && text[j] >= 0x20 && text[j] <= 0x2f {
		j++
	}
	if j < len(text) && text[j] >= 0x30 && text[j] <= 0x7e {
		return j + 1, true
	}
	return i, false
}

// sgrParams returns the numeric parameters of a CSI sequence whose final byte
// is m, which is the one that carries colour, and whether they were written
// with colons.
//
// The separator matters for one case only: colons mean the parameters are
// sub-parameters of a single one, and only that form carries the colour-space
// slot in 38:2::r:g:b. Reading a semicolon list the same way would take the
// red component of 38;2;0;255;0;1m for a colour space and shift the colour.
func sgrParams(seq []byte, dst []int) ([]int, bool, bool) {
	if len(seq) < 3 || seq[0] != esc || seq[1] != '[' || seq[len(seq)-1] != 'm' {
		return nil, false, false
	}
	body := seq[2 : len(seq)-1]
	if len(body) > 0 && (body[0] == '?' || body[0] == '>' || body[0] == '<' || body[0] == '=') {
		return nil, false, false // a private sequence that happens to end in m
	}

	colon := false
	n := 0
	for _, c := range body {
		switch {
		case c >= '0' && c <= '9':
			n = n*10 + int(c-'0')
			if n > 1<<20 {
				n = 1 << 20 // a parameter nobody meant; keep it finite
			}
		case c == ';' || c == ':':
			colon = colon || c == ':'
			dst = append(dst, n)
			n = 0
		default:
			return nil, false, false
		}
	}
	// An empty body is SGR 0, and so is a trailing separator.
	return append(dst, n), colon, true
}

func applySGR(a Attrs, params []int, colon bool) Attrs {
	for i := 0; i < len(params); i++ {
		p := params[i]
		switch {
		case p == 0:
			a = Attrs{}
		case p == 1:
			a.Bold = true
		case p == 2:
			a.Dim = true
		case p == 3:
			a.Italic = true
		case p == 4:
			a.Underline = true
		case p == 5 || p == 6:
			a.Blink = true
		case p == 7:
			a.Reverse = true
		case p == 9:
			a.Strike = true
		case p == 21 || p == 22:
			a.Bold, a.Dim = false, false
		case p == 23:
			a.Italic = false
		case p == 24:
			a.Underline = false
		case p == 25:
			a.Blink = false
		case p == 27:
			a.Reverse = false
		case p == 29:
			a.Strike = false
		case p >= 30 && p <= 37:
			a.Fg = tcell.PaletteColor(p - 30)
		case p == 38:
			if c, used, ok := extended(params[i:], colon); ok {
				a.Fg = c
				i += used - 1
			}
		case p == 39:
			a.Fg = tcell.ColorDefault
		case p >= 40 && p <= 47:
			a.Bg = tcell.PaletteColor(p - 40)
		case p == 48:
			if c, used, ok := extended(params[i:], colon); ok {
				a.Bg = c
				i += used - 1
			}
		case p == 49:
			a.Bg = tcell.ColorDefault
		case p >= 90 && p <= 97:
			a.Fg = tcell.PaletteColor(p - 90 + 8)
		case p >= 100 && p <= 107:
			a.Bg = tcell.PaletteColor(p - 100 + 8)
		}
	}
	return a
}

// extended reads the 256-colour and 24-bit forms, returning how many
// parameters were consumed.
//
// Written with colons the components may be preceded by a colour-space slot,
// as in 38:2::255:0:0, which is why that form accepts six fields.
func extended(params []int, colon bool) (tcell.Color, int, bool) {
	if len(params) < 2 {
		return tcell.ColorDefault, 0, false
	}
	switch params[1] {
	case 5:
		if len(params) < 3 {
			return tcell.ColorDefault, 0, false
		}
		n := params[2]
		if n < 0 || n > 255 {
			return tcell.ColorDefault, 0, false
		}
		return tcell.PaletteColor(n), 3, true

	case 2:
		switch {
		case colon && len(params) >= 6:
			return rgb(params[3], params[4], params[5]), 6, true
		case len(params) >= 5:
			return rgb(params[2], params[3], params[4]), 5, true
		}
	}
	return tcell.ColorDefault, 0, false
}

func rgb(r, g, b int) tcell.Color {
	clamp := func(v int) int32 {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return int32(v)
	}
	return tcell.NewRGBColor(clamp(r), clamp(g), clamp(b))
}
