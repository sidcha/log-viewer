// Package source owns the bytes being viewed.
//
// Everything above this package addresses the log by byte offset and reads
// through ReadAt, which maps to pread(2): there is no shared file offset, so
// the renderer, the indexer, a filter scan and an export can all read
// concurrently without coordinating.
package source

import (
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
)

// Change describes what happened to the underlying file since the last Refresh.
type Change int

const (
	// Unchanged means the file is the same size and the same inode.
	Unchanged Change = iota
	// Grew means bytes were appended; existing offsets remain valid.
	Grew
	// Reset means the file was truncated or replaced (log rotation). Every
	// offset previously handed out is meaningless and the index must be rebuilt.
	Reset
)

func (c Change) String() string {
	switch c {
	case Grew:
		return "grew"
	case Reset:
		return "reset"
	default:
		return "unchanged"
	}
}

// Source is a random-access view of a file that may be growing underneath us.
//
// It is safe for concurrent use.
type Source struct {
	path   string
	stdin  bool
	size   atomic.Int64
	closed atomic.Bool

	mu sync.RWMutex // guards f and fi across a rotation reopen
	f  *os.File
	// fi identifies the file being read, so a replacement at the same path
	// is recognised as a different file rather than as growth.
	fi os.FileInfo

	// set when spooling stdin
	spoolDone atomic.Bool
	spoolErr  atomic.Pointer[spoolError]
	tempPath  string
}

type spoolError struct{ err error }

// Open opens path for reading.
func Open(path string) (*Source, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	s := &Source{path: path, f: f}
	if err := s.adopt(f); err != nil {
		f.Close()
		return nil, err
	}
	s.spoolDone.Store(true)
	return s, nil
}

// FromStdin spools standard input into a temporary file and returns a Source
// over it. The temporary file is unlinked immediately, so it disappears when the
// process exits however it exits.
//
// This keeps the rest of lv uniform: a pipe is just a file that is still
// growing, and follow mode already handles that.
func FromStdin() (*Source, error) {
	tmp, err := os.CreateTemp("", "lv-stdin-*")
	if err != nil {
		return nil, err
	}
	name := tmp.Name()

	// Open the second handle by name *before* unlinking, so both descriptors
	// refer to the same inode. Unlinking afterwards keeps them both valid and
	// leaves nothing on disk however the process exits.
	rd, err := os.Open(name)
	if err != nil {
		tmp.Close()
		os.Remove(name)
		return nil, err
	}
	tempPath := ""
	if err := os.Remove(name); err != nil {
		// Some platforms refuse to unlink an open file; clean up on Close.
		tempPath = name
	}

	s := &Source{path: "(stdin)", stdin: true, f: rd, tempPath: tempPath}
	if err := s.adopt(rd); err != nil {
		rd.Close()
		tmp.Close()
		return nil, err
	}

	go s.spool(tmp)
	return s, nil
}

func (s *Source) spool(w *os.File) {
	defer w.Close()
	buf := make([]byte, 1<<20)
	var total int64
	for {
		n, rerr := os.Stdin.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				s.spoolErr.Store(&spoolError{werr})
				break
			}
			total += int64(n)
			s.size.Store(total)
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				s.spoolErr.Store(&spoolError{rerr})
			}
			break
		}
	}
	s.spoolDone.Store(true)
}

func (s *Source) adopt(f *os.File) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.fi = fi
	s.mu.Unlock()
	s.size.Store(fi.Size())
	return nil
}

// Name is what the file should be called in the UI.
func (s *Source) Name() string { return s.path }

// IsStdin reports whether this source is a spooled pipe.
func (s *Source) IsStdin() bool { return s.stdin }

// Size is the last known size, without a syscall. Refresh updates it.
func (s *Source) Size() int64 { return s.size.Load() }

// Streaming reports whether more bytes may still arrive on their own: a pipe
// that has not hit EOF.
func (s *Source) Streaming() bool { return s.stdin && !s.spoolDone.Load() }

// Err reports a spooling failure, if any.
func (s *Source) Err() error {
	if e := s.spoolErr.Load(); e != nil {
		return e.err
	}
	return nil
}

// ReadAt implements io.ReaderAt.
func (s *Source) ReadAt(p []byte, off int64) (int, error) {
	s.mu.RLock()
	f := s.f
	s.mu.RUnlock()
	if f == nil {
		return 0, os.ErrClosed
	}
	return f.ReadAt(p, off)
}

// Refresh stats the file and reports how it changed. On Reset the file is
// reopened, so callers must discard every cached offset.
func (s *Source) Refresh() (Change, error) {
	if s.stdin {
		// A spooled pipe only ever grows, and spool() keeps size current.
		return Grew, s.Err()
	}

	fi, err := os.Stat(s.path)
	if err != nil {
		// A rotated-away file may momentarily not exist. Report Unchanged so
		// the caller keeps showing what it has instead of blanking the screen.
		if errors.Is(err, os.ErrNotExist) {
			return Unchanged, nil
		}
		return Unchanged, err
	}

	old := s.Size()

	s.mu.RLock()
	sameFile := os.SameFile(fi, s.fi)
	s.mu.RUnlock()

	switch {
	case !sameFile:
		if err := s.reopen(); err != nil {
			return Unchanged, err
		}
		return Reset, nil
	case fi.Size() < old:
		// Truncated in place.
		s.size.Store(fi.Size())
		return Reset, nil
	case fi.Size() > old:
		s.size.Store(fi.Size())
		return Grew, nil
	}
	return Unchanged, nil
}

func (s *Source) reopen() error {
	f, err := os.Open(s.path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	old := s.f
	s.f = f
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return s.adopt(f)
}

// Close releases the file.
func (s *Source) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	s.mu.Lock()
	f := s.f
	s.f = nil
	s.mu.Unlock()
	if s.tempPath != "" {
		_ = os.Remove(s.tempPath)
	}
	if f != nil {
		return f.Close()
	}
	return nil
}

// Dontneed tells the kernel we are finished with a byte range so the page cache
// can drop it. This is what keeps a full-file scan from evicting everything else
// on the machine; it is advisory and failure is not interesting.
func (s *Source) Dontneed(off, length int64) {
	if length <= 0 {
		return
	}
	s.mu.RLock()
	f := s.f
	s.mu.RUnlock()
	if f == nil {
		return
	}
	fadviseDontneed(f, off, length)
}
