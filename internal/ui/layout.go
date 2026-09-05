package ui

import (
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// layout converts a raw log line into terminal cells.
//
// Cell index equals display column: a tab expands to spaces, a double-width
// rune occupies its cell plus a zero-width continuation cell, and control bytes
// become a single visible dot. That equivalence is what lets horizontal
// scrolling be a plain slice offset.
//
// All buffers are reused across every line of every frame, so drawing allocates
// nothing.
type layout struct {
	cells []rune
	width []int8
	cbyte []int32 // source byte offset each cell came from
	b2c   []int32 // byte offset -> first cell index at or after it
	tab   int
	used  int // bytes of the line consumed
}

func newLayout(tab int) *layout {
	if tab <= 0 {
		tab = 8
	}
	return &layout{tab: tab}
}

func (l *layout) push(r rune, w int8, b int) {
	l.cells = append(l.cells, r)
	l.width = append(l.width, w)
	l.cbyte = append(l.cbyte, int32(b))
}

// build lays out text into at most maxCols columns.
func (l *layout) build(text []byte, maxCols int) {
	l.cells = l.cells[:0]
	l.width = l.width[:0]
	l.cbyte = l.cbyte[:0]

	i := 0
	for i < len(text) && len(l.cells) < maxCols {
		c := text[i]

		switch {
		case c == '\t':
			n := l.tab - (len(l.cells) % l.tab)
			for k := 0; k < n && len(l.cells) < maxCols; k++ {
				l.push(' ', 1, i)
			}
			i++
			continue

		case c == 0x1b && i+1 < len(text) && text[i+1] == '[':
			// Hide CSI sequences rather than rendering them as noise. lv styles
			// lines itself; a log that carries its own colours would otherwise
			// show escape gibberish in every line.
			if j, ok := skipCSI(text, i); ok {
				i = j
				continue
			}
			l.push('·', 1, i)
			i++
			continue

		case c < 0x20 || c == 0x7f:
			l.push('·', 1, i)
			i++
			continue

		case c < 0x80:
			l.push(rune(c), 1, i)
			i++
			continue
		}

		r, size := utf8.DecodeRune(text[i:])
		if r == utf8.RuneError && size <= 1 {
			l.push('·', 1, i)
			i++
			continue
		}
		w := runewidth.RuneWidth(r)
		switch {
		case w <= 0:
			// Combining marks have no column of their own; drop them rather
			// than disturb the column/byte equivalence.
		case w == 1:
			l.push(r, 1, i)
		default:
			l.push(r, 2, i)
			l.push(0, 0, i) // continuation cell
		}
		i += size
	}
	l.used = i
	l.mapBytes()
}

// mapBytes builds the byte-offset to cell-index table used to place highlight
// spans, which are expressed in bytes.
func (l *layout) mapBytes() {
	need := l.used + 1
	if cap(l.b2c) < need {
		l.b2c = make([]int32, need)
	}
	l.b2c = l.b2c[:need]

	ci := 0
	for b := 0; b < need; b++ {
		for ci < len(l.cbyte) && int(l.cbyte[ci]) < b {
			ci++
		}
		l.b2c[b] = int32(ci)
	}
}

// cellRange maps a byte range onto a cell range, clamped to what was laid out.
func (l *layout) cellRange(start, end int) (int, int) {
	if start < 0 {
		start = 0
	}
	if end > l.used {
		end = l.used
	}
	if start >= end || len(l.b2c) == 0 {
		return 0, 0
	}
	if start > l.used {
		return 0, 0
	}
	return int(l.b2c[start]), int(l.b2c[end])
}

// cols is the number of columns the laid-out line occupies.
func (l *layout) cols() int { return len(l.cells) }

// skipCSI returns the index just past a CSI escape sequence starting at i.
func skipCSI(text []byte, i int) (int, bool) {
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
}

// displayColumn is the terminal column a byte offset falls on, applying the
// same tab and width rules as build without laying the line out.
func displayColumn(text []byte, byteOff, tab int) int {
	if tab <= 0 {
		tab = 8
	}
	col := 0
	for i := 0; i < len(text) && i < byteOff; {
		c := text[i]
		switch {
		case c == '\t':
			col += tab - (col % tab)
			i++
		case c == 0x1b && i+1 < len(text) && text[i+1] == '[':
			if j, ok := skipCSI(text, i); ok {
				i = j
				continue
			}
			col++
			i++
		case c < 0x20 || c == 0x7f || c < 0x80:
			col++
			i++
		default:
			r, size := utf8.DecodeRune(text[i:])
			if r == utf8.RuneError && size <= 1 {
				col++
				i++
				continue
			}
			w := runewidth.RuneWidth(r)
			if w > 0 {
				col += w
			}
			i += size
		}
	}
	return col
}
