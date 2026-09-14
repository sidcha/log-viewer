package view

import (
	"context"
	"sync"
	"time"

	"github.com/sidcha/log-viewer/internal/checkpoint"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/index"
	"github.com/sidcha/log-viewer/internal/lines"
	"github.com/sidcha/log-viewer/internal/source"
)

const (
	// ckStride is how many matches sit between filtered-view checkpoints.
	ckStride = 1024
	// ckMax caps the checkpoint table at 6 MiB: 24 bytes an entry.
	ckMax = 1 << 18
	// DefaultCheckpointBytes bounds the bytes between filtered-view
	// checkpoints, however rare the matches: a fraction of a millisecond of
	// reading from the page cache.
	DefaultCheckpointBytes = 256 << 10
	// flushMatches / flushLines bound how long the scan goes without publishing
	// progress: often enough to feel live, rarely enough that the lock is free.
	flushMatches = 4096
	flushLines   = 1 << 16
	// cancelEvery is how many lines the scan reads between checks for
	// cancellation, so closing a view never waits on more than a moment's
	// reading.
	cancelEvery = 1024
)

// FilteredOptions configures a filtered view.
type FilteredOptions struct {
	// Block is the read size; MaxLine bounds the retained length of a line.
	Block, MaxLine int
	// Poll is how often a caught-up scan looks for new data when nothing
	// nudges it.
	Poll time.Duration
	// Advise is the page-cache-advise setting: "on", "off" or "auto".
	Advise string
	// CheckpointBytes bounds the bytes between checkpoints; zero selects
	// DefaultCheckpointBytes.
	CheckpointBytes int64
	// Notify is called after every published batch. It must not block.
	Notify func()
	// Narrowing, when set, is what the view being replaced knew about where
	// its matches were not. Only pass it when the new rules can only take
	// lines away (filter.Set.Narrows): the scan then jumps over every stretch
	// the old view had no match in.
	Narrowing *Narrowing
}

// Narrowing is a filtered view's record of where it found nothing.
type Narrowing struct {
	ents []checkpoint.Entry
	gen  uint64
	next int // the first entry the scan has not yet passed
}

// skip returns the run of entries with no match between them that starts at
// off, the last being where a scan standing at off may resume; nil when
// there is nothing to jump over. off must be a line start at or after every
// earlier call's.
func (n *Narrowing) skip(off int64) []checkpoint.Entry {
	for n.next < len(n.ents) && n.ents[n.next].Off < off {
		n.next++
	}
	if n.next >= len(n.ents) || n.ents[n.next].Off != off {
		return nil
	}
	start, end := n.next, n.next
	for end+1 < len(n.ents) && n.ents[end+1].Key == n.ents[start].Key {
		end++
	}
	if end == start {
		return nil
	}
	n.next = end
	return n.ents[start+1 : end+1]
}

// exhausted reports whether the scan is past everything the old view saw.
func (n *Narrowing) exhausted() bool { return n.next >= len(n.ents)-1 }

// Filtered is a view of the lines that survive a filter rules.
//
// It records positions in a sparse checkpoint.Table: every ckStride-th
// matching line, and a position every CheckpointBytes whatever matched.
// Reaching an arbitrary filtered line means seeking to the nearest entry and
// re-applying the predicate forward, which is therefore bounded in bytes as
// well as in matches. A filter that matches a billion lines costs the same
// memory ceiling as one that matches none.
type Filtered struct {
	src   *source.Source
	base  *index.Index
	rules *filter.Set
	opt   FilteredOptions

	mu       sync.RWMutex
	ck       *checkpoint.Table
	gen      uint64 // index generation the table was built under
	n        int64  // matches found so far
	scanned  int64  // bytes consumed
	srcLine  int64  // source lines consumed
	examined int64  // source lines the predicate actually ran on
	done     bool
	err      error

	rd  *lines.Reader
	pos posCache

	cancel context.CancelFunc
	wg     sync.WaitGroup
	// nudge wakes the scan the moment the indexer reports new bytes, so a
	// filtered view of a live stream is as current as an unfiltered one
	// instead of trailing it by a poll interval.
	nudge chan struct{}
}

// Nudge tells the scan that more of the file is available. It never blocks; a
// pending nudge already covers a new one.
func (f *Filtered) Nudge() {
	select {
	case f.nudge <- struct{}{}:
	default:
	}
}

// NewFiltered starts a background scan applying rules to the whole file and
// returns a view over the result. Results stream in, so the view is usable long
// before the scan finishes. Close stops the scan.
func NewFiltered(src *source.Source, base *index.Index, rules *filter.Set, opt FilteredOptions) *Filtered {
	if opt.Poll <= 0 {
		opt.Poll = 150 * time.Millisecond
	}
	if opt.Notify == nil {
		opt.Notify = func() {}
	}
	if opt.CheckpointBytes <= 0 {
		opt.CheckpointBytes = DefaultCheckpointBytes
	}
	rd := lines.NewReader(src, base.Size(), opt.Block)
	rd.SetMaxLine(opt.MaxLine)

	f := &Filtered{
		src:   src,
		base:  base,
		rules: rules,
		opt:   opt,
		ck:    checkpoint.New(ckStride, opt.CheckpointBytes, ckMax, true),
		gen:   base.Gen(),
		rd:    rd,
		nudge: make(chan struct{}, 1),
	}

	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	f.wg.Add(1)
	go f.run(ctx)
	return f
}

// Close stops the background scan and waits for it to exit, which it does
// within cancelEvery lines.
func (f *Filtered) Close() {
	if f.cancel != nil {
		f.cancel()
		f.wg.Wait()
	}
}

// Len implements View.
func (f *Filtered) Len() int64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.n
}

// Progress implements View.
func (f *Filtered) Progress() (int64, int64) {
	f.mu.RLock()
	scanned := f.scanned
	f.mu.RUnlock()
	return scanned, f.base.Size()
}

// Done implements View.
func (f *Filtered) Done() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.done
}

// Err reports a scan failure.
func (f *Filtered) Err() error {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.err
}

// SourceLinesScanned is how many source lines the scan has examined, for the
// "12,345 of 9,876,543" readout.
func (f *Filtered) SourceLinesScanned() int64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.srcLine
}

// Narrowing captures where this view found no matches, for a view with
// narrower rules to skip. It reflects the scan so far.
func (f *Filtered) Narrowing() *Narrowing {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return &Narrowing{ents: f.ck.Entries(), gen: f.gen}
}

// pending is a position the scan found worth recording, held until the next
// publish so the lock is taken once per batch.
type pending struct {
	key, off, src int64
	item          bool
}

// scanState is how far the scan has got. Only the scan goroutine writes it;
// publish copies it into the fields readers see.
type scanState struct {
	n, pos, src, examined int64
}

// run is the background scan. It follows the file: when it reaches the end it
// waits for the indexer to report more bytes and carries on.
func (f *Filtered) run(ctx context.Context) {
	defer f.wg.Done()

	f.mu.RLock()
	gen := f.gen
	f.mu.RUnlock()

	narrow := f.opt.Narrowing
	if narrow != nil && narrow.gen != gen {
		narrow = nil
	}

	var (
		st   scanState
		sc   = f.newScanner(0)
		pend = make([]pending, 0, flushMatches)
		gap  = f.opt.CheckpointBytes / 2
		last = int64(-1) << 62 // offset of the last position offered
	)
	tick := time.NewTicker(f.opt.Poll)
	defer tick.Stop()

	flush := func(done bool) {
		gap = f.publish(pend, st, done) / 2
		pend = pend[:0]
		f.opt.Notify()
	}

	for {
		if ctx.Err() != nil {
			return
		}
		if g := f.base.Gen(); g != gen {
			// The file was truncated or rotated: start over.
			gen = g
			f.reset(g)
			st, last = scanState{}, int64(-1)<<62
			narrow = nil
			sc = f.newScanner(0)
		}

		size := f.base.Size()
		sc.SetLimit(size)
		sc.SeekTo(st.pos)

		var seen int64
		for sc.Scan() {
			if !sc.Terminated() {
				break
			}
			off := sc.Offset()
			if narrow != nil {
				if run := narrow.skip(off); run != nil {
					// Nothing the old rules showed lies in this run, so
					// nothing the new ones show can. Keep its positions,
					// so the walk across it stays bounded here too.
					for _, e := range run {
						pend = append(pend, pending{key: st.n, off: e.Off, src: e.Src})
					}
					to := run[len(run)-1]
					st.src, st.pos, last = to.Src, to.Off, to.Off
					sc.SeekTo(to.Off)
					if len(pend) >= flushMatches {
						flush(false)
					}
					continue
				}
				if narrow.exhausted() {
					narrow = nil
				}
			}
			st.examined++
			if f.rules.Pass(sc.Bytes()) {
				pend = append(pend, pending{key: st.n, off: off, src: st.src, item: true})
				st.n++
				last = off
			} else if gap > 0 && off-last >= gap {
				// Offer a position now and then, whatever matched, so the
				// table can bound the walk between rare matches.
				pend = append(pend, pending{key: st.n, off: off, src: st.src})
				last = off
			}
			st.src++
			st.pos = sc.Next()
			seen++

			if seen%cancelEvery == 0 && ctx.Err() != nil {
				return
			}
			if len(pend) >= flushMatches || seen >= flushLines {
				flush(false)
				seen = 0
			}
		}

		flush(st.pos >= size && f.base.Caught())

		if err := sc.Err(); err != nil {
			f.setErr(err)
			return
		}
		if st.pos < f.base.Size() {
			continue // more arrived while we were scanning
		}
		select {
		case <-ctx.Done():
			return
		case <-f.nudge:
		case <-tick.C:
		}
	}
}

func (f *Filtered) newScanner(off int64) *lines.Scanner {
	size := f.base.Size()
	sc := lines.NewScanner(f.src, off, size, f.opt.Block)
	sc.SetAdvise(lines.Advise(f.opt.Advise, size))
	return sc
}

// publish records the pending positions and the scan's progress, and returns
// the table's current byte stride so the scan can pace its offers.
func (f *Filtered) publish(pend []pending, st scanState, done bool) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range pend {
		if p.item {
			f.ck.Item(p.key, p.off, p.src)
		} else {
			f.ck.Pass(p.key, p.off, p.src)
		}
	}
	f.n = st.n
	f.scanned = st.pos
	f.srcLine = st.src
	f.examined = st.examined
	f.done = done
	return f.ck.ByteStride()
}

func (f *Filtered) reset(gen uint64) {
	f.mu.Lock()
	f.ck.Reset()
	f.gen = gen
	f.n, f.scanned, f.srcLine, f.examined = 0, 0, 0, 0
	f.done = false
	// The position cache is deliberately not touched here: it belongs to the
	// goroutine calling At, and its entries carry the index generation, so a
	// reset invalidates them without this goroutine writing to them.
	f.mu.Unlock()
}

func (f *Filtered) setErr(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

// lookup returns the nearest recorded position at or before filtered line i.
func (f *Filtered) lookup(i int64) (position, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	e, ok := f.ck.Lookup(i)
	return position{idx: e.Key, off: e.Off, src: e.Src}, ok
}

// IndexForSource returns the view index of the first surviving line whose
// source line number is at or after src.
//
// This is how a position is preserved when a filter is added or removed. That
// index is the number of surviving lines before src, so it is counted from the
// nearest checkpoint at or before src -- a walk of at most one stride.
func (f *Filtered) IndexForSource(src int64) (int64, bool) {
	f.mu.RLock()
	e, ok := f.ck.LookupSrc(src)
	total, scanned := f.n, f.scanned
	f.mu.RUnlock()
	if !ok {
		return 0, false
	}

	match, cur := e.Key, e.Src
	if cur < src {
		sc := lines.NewScanner(f.src, e.Off, scanned, f.opt.Block)
		for cur < src && sc.Scan() {
			if f.rules.Pass(sc.Bytes()) {
				match++
			}
			cur++
		}
	}
	if match >= total {
		return total, false
	}
	return match, true
}

// At implements View.
func (f *Filtered) At(i int64, dst []byte) (Line, error) {
	if i < 0 || i >= f.Len() {
		return Line{}, ErrRange
	}

	from, ok := f.lookup(i)
	if !ok {
		return Line{}, ErrRange
	}
	gen := f.base.Gen()
	if p, ok := f.pos.nearest(gen, i); ok && p.off > from.off {
		from = p
	}

	sc := f.rd.Scanner()
	sc.SetLimit(f.base.Size())
	sc.SeekTo(from.off)

	match, src := from.idx, from.src
	for sc.Scan() {
		if f.rules.Pass(sc.Bytes()) {
			if match == i {
				ln := Line{
					Text:      append(dst, sc.Bytes()...),
					Num:       src + 1,
					Off:       sc.Offset(),
					Next:      sc.Next(),
					Truncated: sc.Truncated(),
				}
				f.pos.put(gen, position{idx: i, off: ln.Off, src: src})
				f.pos.put(gen, position{idx: i + 1, off: ln.Next, src: src + 1})
				return ln, nil
			}
			match++
		}
		src++
	}
	return Line{}, ErrRange
}

// Iterate implements View.
func (f *Filtered) Iterate(from int64, fn func(int64, Line) bool) error {
	if from < 0 {
		from = 0
	}
	// Iterate must not depend on how far the background scan has got: an export
	// is expected to be complete and correct the moment it is asked for. A
	// checkpoint is only a shortcut, so fall back to scanning from the start.
	start, _ := f.lookup(from)

	sc := lines.NewScanner(f.src, start.off, f.base.Size(), f.opt.Block)
	match, src := start.idx, start.src
	for sc.Scan() {
		if f.rules.Pass(sc.Bytes()) {
			if match >= from {
				ln := Line{
					Text:      sc.Bytes(),
					Num:       src + 1,
					Off:       sc.Offset(),
					Next:      sc.Next(),
					Truncated: sc.Truncated(),
				}
				if !fn(match, ln) {
					return sc.Err()
				}
			}
			match++
		}
		src++
	}
	return sc.Err()
}
