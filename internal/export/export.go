// Package export writes the current view to a file.
package export

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sidcha/log-viewer/internal/source"
	"github.com/sidcha/log-viewer/internal/view"
)

// Progress is reported periodically during a save.
type Progress struct {
	Lines   int64
	Bytes   int64
	Scanned int64
	Total   int64
	Done    bool
}

// reportEvery bounds how often progress is published and cancellation checked.
const reportEvery = 4096

// Save streams v to path.
//
// It writes to a sibling temporary file and renames on success, so an
// interrupted or failed save never leaves a half-written file in place.
func Save(ctx context.Context, src *source.Source, v view.View, path string, report func(Progress)) (Progress, error) {
	var p Progress

	abs, err := filepath.Abs(path)
	if err != nil {
		return p, err
	}
	if same, err := sameFile(abs, src.Name()); err == nil && same {
		return p, errors.New("refusing to overwrite the file being viewed")
	}

	tmp, err := os.CreateTemp(filepath.Dir(abs), "."+filepath.Base(abs)+".lv-*")
	if err != nil {
		return p, err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	w := bufio.NewWriterSize(tmp, 1<<20)
	p.Total = src.Size()

	var (
		counter int
		werr    error
		cancel  bool
	)
	ierr := v.Iterate(0, func(i int64, ln Line) bool {
		n, err := writeLine(src, w, ln)
		if err != nil {
			werr = err
			return false
		}
		p.Lines++
		p.Bytes += n
		p.Scanned = ln.Next

		counter++
		if counter >= reportEvery {
			counter = 0
			if report != nil {
				report(p)
			}
			if ctx.Err() != nil {
				cancel = true
				return false
			}
		}
		return true
	})

	switch {
	case werr != nil:
		return p, werr
	case ierr != nil:
		return p, ierr
	case cancel:
		return p, ctx.Err()
	}

	if err := w.Flush(); err != nil {
		return p, err
	}
	if err := tmp.Close(); err != nil {
		return p, err
	}
	if err := os.Rename(tmpName, abs); err != nil {
		return p, err
	}
	tmpName = "" // adopted; do not remove

	p.Done = true
	if report != nil {
		report(p)
	}
	return p, nil
}

// Line aliases view.Line so callers of this package need not import view for
// the callback signature.
type Line = view.Line

// writeLine reproduces the line exactly as it appears in the source, including
// its terminator or the lack of one on a final partial line.
func writeLine(src *source.Source, w *bufio.Writer, ln Line) (int64, error) {
	span := ln.Next - ln.Off
	switch {
	case span == int64(len(ln.Text)):
		// Unterminated final line.
		n, err := w.Write(ln.Text)
		return int64(n), err
	case span == int64(len(ln.Text))+1:
		n, err := w.Write(ln.Text)
		if err != nil {
			return int64(n), err
		}
		if err := w.WriteByte('\n'); err != nil {
			return int64(n), err
		}
		return int64(n) + 1, nil
	default:
		// The line was longer than the retention cap, so Text is only a prefix.
		// Copy the exact byte range instead of writing a mangled line.
		return copyRange(src, w, ln.Off, ln.Next)
	}
}

func copyRange(src *source.Source, w *bufio.Writer, off, end int64) (int64, error) {
	buf := make([]byte, 1<<16)
	var total int64
	for off < end {
		n := int64(len(buf))
		if rem := end - off; rem < n {
			n = rem
		}
		got, err := src.ReadAt(buf[:n], off)
		if got > 0 {
			if _, werr := w.Write(buf[:got]); werr != nil {
				return total, werr
			}
			total += int64(got)
			off += int64(got)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return total, err
		}
		if got == 0 {
			return total, fmt.Errorf("short read at offset %d", off)
		}
	}
	return total, nil
}

func sameFile(a, b string) (bool, error) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(fa, fb), nil
}
