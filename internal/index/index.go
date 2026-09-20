// Package index maintains a sparse map from line number to byte offset.
//
// A dense table is unaffordable: a 100 GB log holds on the order of 10^9 lines,
// which is 8 GB of int64. Instead the line starts go into a sparse
// checkpoint.Table, and reaching an arbitrary line means seeking to the nearest
// checkpoint and scanning forward at most a stride of lines, or of bytes,
// whichever comes first.
//
// The table is capped. When it would exceed maxCheckpoints, every second entry
// is dropped and the strides double. Index memory is therefore O(1) in file
// size, and the cost is only a longer forward scan.
package index

import (
	"sync"

	"github.com/sidcha/log-viewer/internal/checkpoint"
)

// DefaultStride is the initial number of lines between checkpoints.
const DefaultStride = 1024

// DefaultMaxCheckpoints caps checkpoint memory at 8 MiB: 16 bytes each.
const DefaultMaxCheckpoints = 1 << 19

// DefaultByteStride bounds the bytes between checkpoints, so a file of very
// long lines costs no more to seek in than one of short lines.
const DefaultByteStride = 1 << 20

// Index is safe for concurrent use: one writer (the Indexer) and many readers.
type Index struct {
	mu sync.RWMutex
	ck *checkpoint.Table

	complete int64 // newline-terminated lines seen
	tail     int64 // offset just past the last newline; bytes consumed
	size     int64 // file size as last observed
	gen      uint64
	err      error
}

// New returns an empty index. stride and maxCk may be zero for the defaults.
func New(stride int64, maxCk int) *Index {
	if stride <= 0 {
		stride = DefaultStride
	}
	if maxCk <= 0 {
		maxCk = DefaultMaxCheckpoints
	}
	return &Index{ck: checkpoint.New(stride, DefaultByteStride, maxCk, false)}
}

// Add records a batch of line-start offsets, in order, followed by the new
// consumed offset and observed file size. Called once per scanned block so the
// lock is taken a handful of times per megabyte.
func (x *Index) Add(starts []int64, tail, size int64) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, o := range starts {
		x.ck.Item(x.complete, o, x.complete)
		x.complete++
	}
	x.tail = tail
	if size > x.size {
		x.size = size
	}
}

// Reset discards everything, for a truncated or rotated file. The generation
// counter changes so readers can tell their cached positions are stale.
func (x *Index) Reset() {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.ck.Reset()
	x.complete = 0
	x.tail = 0
	x.size = 0
	x.gen++
	x.err = nil
}

// SetSize records the file size without consuming any bytes, so Lines can
// report the partial trailing line as soon as it appears.
func (x *Index) SetSize(size int64) {
	x.mu.Lock()
	if size > x.size {
		x.size = size
	}
	x.mu.Unlock()
}

// SetErr records a fatal read error.
func (x *Index) SetErr(err error) {
	x.mu.Lock()
	x.err = err
	x.mu.Unlock()
}

// Err reports a read error, if any.
func (x *Index) Err() error {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.err
}

// Lines is the number of addressable lines, including a partial trailing line
// that has not been terminated yet.
func (x *Index) Lines() int64 {
	x.mu.RLock()
	defer x.mu.RUnlock()
	n := x.complete
	if x.tail < x.size {
		n++
	}
	return n
}

// Complete is the number of newline-terminated lines indexed.
func (x *Index) Complete() int64 {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.complete
}

// Stats reports indexing progress: lines found, bytes consumed and file size.
func (x *Index) Stats() (lines, scanned, size int64, stride int64, checkpoints int) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	lines = x.complete
	if x.tail < x.size {
		lines++
	}
	return lines, x.tail, x.size, x.ck.KeyStride(), x.ck.Len()
}

// Caught reports whether indexing has consumed everything available.
func (x *Index) Caught() bool {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.tail >= x.size
}

// Tail is the offset just past the last newline consumed.
func (x *Index) Tail() int64 {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.tail
}

// Size is the last observed file size.
func (x *Index) Size() int64 {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.size
}

// Gen is the generation counter, bumped by Reset.
func (x *Index) Gen() uint64 {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.gen
}

// Checkpoint returns the nearest checkpoint at or before line n: the byte
// offset to seek to and the line number living there. The caller scans forward
// n-line newlines from off.
func (x *Index) Checkpoint(n int64) (off, line int64) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	if n < 0 {
		return 0, 0
	}
	e, ok := x.ck.Lookup(n)
	if !ok {
		return 0, 0
	}
	return e.Off, e.Key
}
