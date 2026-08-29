// Package index maintains a sparse map from line number to byte offset.
//
// A dense table is unaffordable: a 100 GB log holds on the order of 10^9 lines,
// which is 8 GB of int64. Instead only every stride-th line start is recorded,
// and reaching an arbitrary line means seeking to the nearest checkpoint and
// scanning forward at most stride-1 newlines — one block read.
//
// The checkpoint slice itself is capped. When it would exceed maxCheckpoints,
// every second entry is dropped and the stride doubles. Index memory is
// therefore O(1) in file size, and the cost is only a longer forward scan.
package index

import "sync"

// DefaultStride is the initial number of lines between checkpoints.
const DefaultStride = 1024

// DefaultMaxCheckpoints caps checkpoint memory at 8 MiB.
const DefaultMaxCheckpoints = 1 << 20

// Index is safe for concurrent use: one writer (the Indexer) and many readers.
type Index struct {
	mu     sync.RWMutex
	stride int64
	maxCk  int
	off    []int64 // off[i] is the byte offset of line i*stride

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
	if maxCk < 4 {
		// Below this the table would collapse on almost every append and the
		// stride would run away.
		maxCk = 4
	}
	return &Index{stride: stride, maxCk: maxCk}
}

// Add records a batch of line-start offsets, in order, followed by the new
// consumed offset and observed file size. Called once per scanned block so the
// lock is taken a handful of times per megabyte.
func (x *Index) Add(starts []int64, tail, size int64) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, o := range starts {
		if x.complete%x.stride == 0 {
			x.off = append(x.off, o)
			// Enforce the cap inside the loop, not once per batch: a single
			// Add may carry far more lines than a stride, and the whole point
			// of the ceiling is that it cannot be exceeded.
			if len(x.off) >= x.maxCk {
				x.collapse()
			}
		}
		x.complete++
	}
	x.tail = tail
	if size > x.size {
		x.size = size
	}
}

// collapse halves the checkpoint table and doubles the stride, preserving the
// invariant off[i] == offset of line i*stride.
func (x *Index) collapse() {
	n := 0
	for i := 0; i < len(x.off); i += 2 {
		x.off[n] = x.off[i]
		n++
	}
	x.off = x.off[:n]
	x.stride *= 2
}

// Reset discards everything, for a truncated or rotated file. The generation
// counter changes so readers can tell their cached positions are stale.
func (x *Index) Reset() {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.off = x.off[:0]
	x.complete = 0
	x.tail = 0
	x.size = 0
	x.stride = DefaultStride
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

// Stats reports indexing progress: lines found, bytes consumed and file size.
func (x *Index) Stats() (lines, scanned, size int64, stride int64, checkpoints int) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	lines = x.complete
	if x.tail < x.size {
		lines++
	}
	return lines, x.tail, x.size, x.stride, len(x.off)
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
	if len(x.off) == 0 || n < 0 {
		return 0, 0
	}
	i := n / x.stride
	if i >= int64(len(x.off)) {
		i = int64(len(x.off)) - 1
	}
	return x.off[i], i * x.stride
}
