package index

import (
	"context"
	"time"

	"github.com/sidcha/log-viewer/internal/lines"
	"github.com/sidcha/log-viewer/internal/source"
)

// batchLines is how many line offsets accumulate before they are published.
// Large enough that the index lock is uncontended, small enough that the first
// screen appears immediately.
const batchLines = 8192

// adviseThreshold is the file size above which a full index pass hands page
// ranges back to the kernel as it goes, so scanning a huge log does not evict
// everything else on the machine.
const adviseThreshold = 2 << 30

// Options configures an Indexer.
type Options struct {
	// Wake fires when the file may have changed. It is a hint, not a change
	// log: the indexer stats to find out what actually happened. When nil the
	// indexer falls back to its own ticker at Poll.
	Wake <-chan struct{}
	// Poll is the interval for the built-in ticker used when Wake is nil.
	Poll time.Duration
	// Block is the read size for the indexing scan.
	Block int
	// Advise selects page-cache release behaviour: "on", "off" or "auto".
	Advise string
	// Notify is called after every published batch. It must not block.
	Notify func()
}

// Indexer keeps an Index current with a file that may still be growing.
//
// Growth, truncation and rotation are all discovered the same way: a wake-up
// arrives, the file is stat-ed, and the difference decides whether to scan more
// bytes or start over.
type Indexer struct {
	src  *source.Source
	idx  *Index
	opt  Options
	sc   *lines.Scanner
	buf  []int64
	gen  uint64
	warm bool
}

// NewIndexer wires an indexer to a source and index.
func NewIndexer(src *source.Source, idx *Index, opt Options) *Indexer {
	if opt.Poll <= 0 {
		opt.Poll = 150 * time.Millisecond
	}
	if opt.Block <= 0 {
		opt.Block = lines.DefaultBlock
	}
	if opt.Notify == nil {
		opt.Notify = func() {}
	}
	return &Indexer{
		src: src,
		idx: idx,
		opt: opt,
		buf: make([]int64, 0, batchLines),
	}
}

// Sync brings the index up to the file's current size and returns. It does not
// wait for more data, which makes it the entry point for tests and for any
// non-interactive use.
func (ix *Indexer) Sync(ctx context.Context) {
	// Bounded so a writer that is faster than we can scan cannot keep us here
	// forever; the caller loops anyway.
	for round := 0; round < 8; round++ {
		if ctx.Err() != nil {
			return
		}

		change, err := ix.src.Refresh()
		if err != nil {
			ix.idx.SetErr(err)
		}
		if change == source.Reset {
			ix.idx.Reset()
			ix.sc = nil
		}

		size := ix.src.Size()
		ix.idx.SetSize(size)
		if ix.idx.Tail() >= size {
			return
		}
		ix.consume(ctx, size)
		ix.opt.Notify()
	}
}

// Run indexes until ctx is cancelled. It always keeps following the file;
// whether the viewport sticks to the tail is a UI decision, not this one.
func (ix *Indexer) Run(ctx context.Context) {
	wake := ix.opt.Wake
	if wake == nil {
		tick := time.NewTicker(ix.opt.Poll)
		defer tick.Stop()
		ch := make(chan struct{}, 1)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					select {
					case ch <- struct{}{}:
					default:
					}
				}
			}
		}()
		wake = ch
	}

	for ctx.Err() == nil {
		ix.Sync(ctx)

		if !ix.idx.Caught() {
			continue // still behind; keep scanning rather than wait
		}
		if !ix.warm {
			ix.warm = true
			ix.opt.Notify()
		}

		select {
		case <-ctx.Done():
			return
		case <-wake:
		}
	}
}

// consume scans newly available bytes into the index, publishing in batches so
// the UI sees progress on a huge file instead of waiting for the whole pass.
func (ix *Indexer) consume(ctx context.Context, size int64) {
	tail := ix.idx.Tail()

	if ix.sc == nil || ix.idx.Gen() != ix.gen {
		ix.gen = ix.idx.Gen()
		ix.sc = lines.NewScanner(ix.src, tail, size, ix.opt.Block)
		ix.sc.SetAdvise(ix.advising(size))
	}
	ix.sc.SetLimit(size)
	ix.sc.SeekTo(tail)

	starts := ix.buf[:0]
	for ix.sc.Scan() {
		if !ix.sc.Terminated() {
			// Partial trailing line: leave it unconsumed so it is re-read, and
			// keep growing, until its newline arrives.
			break
		}
		starts = append(starts, ix.sc.Offset())
		tail = ix.sc.Next()

		if len(starts) == cap(starts) {
			ix.idx.Add(starts, tail, size)
			starts = starts[:0]
			ix.opt.Notify()
			if ctx.Err() != nil {
				break
			}
		}
	}
	ix.idx.Add(starts, tail, size)
	ix.buf = starts[:0]

	// Rewind so the next round starts exactly where this one stopped, which
	// keeps the partial tail inside the buffer and makes the re-read free.
	ix.sc.SeekTo(tail)

	if err := ix.sc.Err(); err != nil {
		ix.idx.SetErr(err)
	}
}

func (ix *Indexer) advising(size int64) bool {
	switch ix.opt.Advise {
	case "on":
		return true
	case "off":
		return false
	default:
		return size > adviseThreshold
	}
}
