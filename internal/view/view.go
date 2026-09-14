// Package view turns the index and the filter rules into addressable sequences
// of lines that the UI can page through.
package view

import (
	"errors"

	"github.com/sidcha/log-viewer/internal/index"
	"github.com/sidcha/log-viewer/internal/lines"
	"github.com/sidcha/log-viewer/internal/source"
)

// ErrRange means the requested line does not exist (yet).
var ErrRange = errors.New("line out of range")

// Line is one addressed line. Text is only valid until the next call on the
// same View, or until the Iterate callback returns.
type Line struct {
	Text      []byte
	Num       int64 // 1-based line number in the source file
	Off       int64 // byte offset of the line
	Next      int64 // byte offset of the following line
	Truncated bool
}

// View is an addressable sequence of lines.
type View interface {
	// Len is the number of lines currently addressable. It grows as indexing
	// and filtering progress and as the file is appended to.
	Len() int64
	// At returns line i, appending its text to dst.
	At(i int64, dst []byte) (Line, error)
	// Iterate walks forward from view line `from`, stopping when fn returns
	// false. It uses its own reader, so it does not disturb At.
	Iterate(from int64, fn func(i int64, ln Line) bool) error
	// Progress reports bytes processed and bytes available.
	Progress() (scanned, total int64)
	// Done reports whether everything currently available has been processed.
	Done() bool
}

// Base is the unfiltered whole-file view.
type Base struct {
	src   *source.Source
	idx   *index.Index
	rd    *lines.Reader
	block int

	pos posCache
}

// NewBase returns a view over the whole file.
func NewBase(src *source.Source, idx *index.Index, block, maxLine int) *Base {
	rd := lines.NewReader(src, idx.Size(), block)
	rd.SetMaxLine(maxLine)
	return &Base{src: src, idx: idx, rd: rd, block: block}
}

// Len implements View.
func (b *Base) Len() int64 { return b.idx.Lines() }

// Progress implements View.
func (b *Base) Progress() (int64, int64) { return b.idx.Tail(), b.idx.Size() }

// Done implements View.
func (b *Base) Done() bool { return b.idx.Caught() }

// At implements View.
func (b *Base) At(i int64, dst []byte) (Line, error) {
	if i < 0 || i >= b.idx.Lines() {
		return Line{}, ErrRange
	}
	b.rd.SetLimit(b.idx.Size())

	gen := b.idx.Gen()
	off, err := b.locate(gen, i)
	if err != nil {
		return Line{}, err
	}
	text, next, trunc, err := b.rd.LineAt(off, dst)
	if err != nil {
		return Line{}, err
	}
	b.pos.put(gen, position{idx: i, off: off, src: i})
	if b.rd.Scanner().Terminated() {
		// Only a finished line says where the next one starts: a partial
		// tail may yet grow.
		b.pos.put(gen, position{idx: i + 1, off: next, src: i + 1})
	}
	return Line{Text: text, Num: i + 1, Off: off, Next: next, Truncated: trunc}, nil
}

// locate finds the byte offset of view line i, starting from whichever is
// closer: the nearest index checkpoint or a remembered position.
func (b *Base) locate(gen uint64, i int64) (int64, error) {
	from, fromLine := b.idx.Checkpoint(i)
	if p, ok := b.pos.nearest(gen, i); ok && p.idx > fromLine {
		from, fromLine = p.off, p.idx
	}

	if k := i - fromLine; k > 0 {
		pos, done := b.rd.Scanner().SkipLines(from, k)
		if done < k {
			return 0, ErrRange
		}
		from = pos
	}
	return from, nil
}

// Iterate implements View.
func (b *Base) Iterate(from int64, fn func(int64, Line) bool) error {
	if from < 0 {
		from = 0
	}
	size := b.idx.Size()
	ckOff, ckLine := b.idx.Checkpoint(from)

	sc := lines.Borrow(b.src, ckOff, size, b.block)
	defer sc.Release()
	if k := from - ckLine; k > 0 {
		if _, done := sc.SkipLines(ckOff, k); done < k {
			return sc.Err()
		}
	}

	i := from
	for sc.Scan() {
		ln := Line{
			Text:      sc.Bytes(),
			Num:       i + 1,
			Off:       sc.Offset(),
			Next:      sc.Next(),
			Truncated: sc.Truncated(),
		}
		if !fn(i, ln) {
			return sc.Err()
		}
		i++
	}
	return sc.Err()
}
