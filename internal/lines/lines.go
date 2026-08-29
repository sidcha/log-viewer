// Package lines turns a byte source into lines without allocating per line.
//
// Scanner is the single primitive: a buffered forward reader that knows the
// file offset of every line it yields and can be re-seeked cheaply. Indexing,
// filtering, exporting and rendering are all built on it.
package lines

import (
	"bytes"
	"errors"
	"io"

	"github.com/sidcha/log-viewer/internal/source"
)

const (
	// DefaultBlock is the read granularity. One syscall per block.
	DefaultBlock = 1 << 20
	// DefaultMaxLine bounds how much of a single line is retained. Lines longer
	// than this are reported truncated: they are still counted and their offsets
	// are still correct, but matching and rendering only see this prefix.
	DefaultMaxLine = 1 << 20
	// advisePeriod is how far behind the read head we hand pages back to the
	// kernel during a bulk sequential pass.
	advisePeriod = 8 << 20
)

// Scanner reads lines forward from a source.Source.
//
// A Scanner is not safe for concurrent use; every consumer gets its own. The
// slice returned by Bytes is only valid until the next call.
type Scanner struct {
	src     *source.Source
	buf     []byte
	r, w    int   // valid bytes live in buf[r:w]
	readOff int64 // file offset just past buf[w]
	limit   int64 // never read at or past this offset
	maxLine int

	off   int64 // offset of the current line
	next  int64 // offset of the following line
	line  []byte
	long  []byte // holds the retained prefix of an over-long line
	trunc bool
	term  bool // the current line ended with a newline
	err   error

	advise    bool
	advisedTo int64
}

// NewScanner returns a Scanner positioned at off and limited to limit bytes
// into the file. block is the read size; zero selects DefaultBlock.
func NewScanner(src *source.Source, off, limit int64, block int) *Scanner {
	if block <= 0 {
		block = DefaultBlock
	}
	s := &Scanner{
		src:     src,
		buf:     make([]byte, block),
		readOff: off,
		limit:   limit,
		maxLine: DefaultMaxLine,
		off:     off,
		next:    off,
	}
	return s
}

// SetMaxLine bounds the retained length of a single line.
func (s *Scanner) SetMaxLine(n int) {
	if n > 0 {
		s.maxLine = n
	}
}

// SetLimit raises (or lowers) the end of the readable region. Raising it lets a
// Scanner that hit the end of a growing file continue where it stopped.
func (s *Scanner) SetLimit(limit int64) { s.limit = limit }

// SetAdvise enables handing page-cache ranges back to the kernel as the scan
// moves past them. Use it for full-file passes, not for rendering.
func (s *Scanner) SetAdvise(on bool) {
	s.advise = on
	s.advisedTo = s.Pos()
}

// Pos is the offset of the next byte that has not been consumed.
func (s *Scanner) Pos() int64 { return s.readOff - int64(s.w-s.r) }

// SeekTo repositions the scanner. If the target is anywhere in the current
// buffer — forwards or backwards — this is free, which is what makes both
// sequential rendering and re-reading a growing tail cheap.
func (s *Scanner) SeekTo(off int64) {
	if bufBase := s.readOff - int64(s.w); s.err == nil && off >= bufBase && off <= s.readOff {
		s.r = int(off - bufBase)
		s.off, s.next = off, off
		return
	}
	s.r, s.w = 0, 0
	s.readOff = off
	s.off, s.next = off, off
	s.err = nil
}

// Scan advances to the next line, returning false at the limit or on error.
func (s *Scanner) Scan() bool {
	if s.err != nil {
		return false
	}
	s.trunc = false
	s.off = s.Pos()
	if s.off >= s.limit {
		return false
	}

	for {
		if i := bytes.IndexByte(s.buf[s.r:s.w], '\n'); i >= 0 {
			s.line = s.buf[s.r : s.r+i]
			s.r += i + 1
			s.next = s.off + int64(i) + 1
			s.term = true
			return true
		}
		if s.w-s.r >= s.maxLine {
			// Retain the prefix, then discard the rest of the line. Copy it out
			// first: skipToNewline refills the buffer underneath us.
			s.long = append(s.long[:0], s.buf[s.r:s.r+s.maxLine]...)
			s.line = s.long
			s.trunc = true
			lineOff := s.off
			s.next, s.term = s.skipToNewline(lineOff + int64(s.maxLine))
			s.off = lineOff
			return true
		}
		if !s.fill() {
			if s.w > s.r {
				// Trailing line with no newline yet.
				s.line = s.buf[s.r:s.w]
				s.next = s.off + int64(s.w-s.r)
				s.r = s.w
				s.term = false
				return true
			}
			return false
		}
	}
}

// Bytes is the current line without its newline. Valid until the next Scan.
func (s *Scanner) Bytes() []byte { return s.line }

// Offset is the file offset of the current line.
func (s *Scanner) Offset() int64 { return s.off }

// Next is the file offset of the line after the current one.
func (s *Scanner) Next() int64 { return s.next }

// Truncated reports whether the current line was longer than the retention cap.
func (s *Scanner) Truncated() bool { return s.trunc }

// Terminated reports whether the current line ended with a newline. A false
// result means this is the partial tail of a file that is still being written;
// the indexer must not consume it.
func (s *Scanner) Terminated() bool { return s.term }

// Err reports a read failure. Reaching the limit is not an error.
func (s *Scanner) Err() error {
	if errors.Is(s.err, io.EOF) {
		return nil
	}
	return s.err
}

// SkipLines advances past n line terminators starting at off and returns the
// offset it reached, plus how many lines it actually skipped (fewer than n if
// the limit was hit first). It never retains line contents.
func (s *Scanner) SkipLines(off int64, n int64) (int64, int64) {
	s.SeekTo(off)
	var done int64
	for done < n {
		if i := bytes.IndexByte(s.buf[s.r:s.w], '\n'); i >= 0 {
			s.r += i + 1
			done++
			continue
		}
		s.r = s.w
		if !s.fill() {
			break
		}
	}
	pos := s.Pos()
	s.off, s.next = pos, pos
	return pos, done
}

// skipToNewline discards bytes from `from` up to and including the next
// newline, repositions the buffer there, and reports whether a newline was
// actually found before the limit.
func (s *Scanner) skipToNewline(from int64) (int64, bool) {
	s.SeekTo(from)
	for {
		if i := bytes.IndexByte(s.buf[s.r:s.w], '\n'); i >= 0 {
			s.r += i + 1
			return s.Pos(), true
		}
		s.r = s.w
		if !s.fill() {
			return s.Pos(), false
		}
	}
}

// fill makes room and reads one more block. It reports whether any bytes were
// added.
func (s *Scanner) fill() bool {
	if s.r > 0 {
		copy(s.buf, s.buf[s.r:s.w])
		s.w -= s.r
		s.r = 0
	}
	if s.w == len(s.buf) {
		if len(s.buf) >= s.maxLine {
			return false
		}
		grown := len(s.buf) * 2
		if grown > s.maxLine {
			grown = s.maxLine
		}
		nb := make([]byte, grown)
		copy(nb, s.buf[:s.w])
		s.buf = nb
	}

	avail := s.limit - s.readOff
	if avail <= 0 {
		return false
	}
	room := int64(len(s.buf) - s.w)
	if room > avail {
		room = avail
	}

	n, err := s.src.ReadAt(s.buf[s.w:s.w+int(room)], s.readOff)
	s.w += n
	s.readOff += int64(n)
	if err != nil && !errors.Is(err, io.EOF) {
		s.err = err
	}
	if s.advise && s.readOff-s.advisedTo >= advisePeriod {
		// Keep one period behind the head so a small backward seek still hits
		// the page cache.
		upto := s.readOff - advisePeriod
		if upto > s.advisedTo {
			s.src.Dontneed(s.advisedTo, upto-s.advisedTo)
			s.advisedTo = upto
		}
	}
	return n > 0
}

// Reader is a random-access line reader built on a Scanner. Consecutive reads
// at increasing offsets reuse the buffer, so drawing a screen is one block read.
type Reader struct {
	sc *Scanner
}

// NewReader returns a Reader over src. The limit is raised with SetLimit as the
// file grows.
func NewReader(src *source.Source, limit int64, block int) *Reader {
	return &Reader{sc: NewScanner(src, 0, limit, block)}
}

// SetLimit raises the readable end of the file.
func (r *Reader) SetLimit(limit int64) { r.sc.SetLimit(limit) }

// SetMaxLine bounds retained line length.
func (r *Reader) SetMaxLine(n int) { r.sc.SetMaxLine(n) }

// Scanner exposes the underlying scanner for callers that want to walk forward.
func (r *Reader) Scanner() *Scanner { return r.sc }

// LineAt appends the line starting at off to dst and returns it along with the
// offset of the following line.
func (r *Reader) LineAt(off int64, dst []byte) (text []byte, next int64, trunc bool, err error) {
	r.sc.SeekTo(off)
	if !r.sc.Scan() {
		return dst, off, false, r.sc.Err()
	}
	return append(dst, r.sc.Bytes()...), r.sc.Next(), r.sc.Truncated(), r.sc.Err()
}
