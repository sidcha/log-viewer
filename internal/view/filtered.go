package view

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/index"
	"github.com/sidcha/log-viewer/internal/lines"
	"github.com/sidcha/log-viewer/internal/source"
)

const (
	// ckStride is how many matches sit between filtered-view checkpoints.
	ckStride = 1024
	// ckMax caps the checkpoint table, as in package index.
	ckMax = 1 << 20
	// flushMatches / flushLines bound how long the scan goes without publishing
	// progress: often enough to feel live, rarely enough that the lock is free.
	flushMatches = 4096
	flushLines   = 1 << 16
)

// Filtered is a view of the lines that survive a filter rules.
//
// It uses the same sparse-checkpoint trick as the line index: the file offset
// and source line number of every ckStride-th matching line are recorded, and
// reaching an arbitrary filtered line means seeking to the nearest checkpoint
// and re-applying the predicate forward. A filter that matches a billion lines
// costs the same 8 MB ceiling as the index does.
type Filtered struct {
	src   *source.Source
	base  *index.Index
	rules *filter.Set
	block int
	poll  time.Duration

	mu      sync.RWMutex
	stride  int64
	ckOff   []int64
	ckSrc   []int64
	n       int64 // matches found so far
	scanned int64 // bytes consumed
	srcLine int64 // source lines consumed
	done    bool
	err     error

	rd    *lines.Reader
	cache [2]cacheEntry

	cancel context.CancelFunc
	wg     sync.WaitGroup
	notify func()
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
func NewFiltered(src *source.Source, base *index.Index, rules *filter.Set, block, maxLine int, poll time.Duration, notify func()) *Filtered {
	if poll <= 0 {
		poll = 150 * time.Millisecond
	}
	if notify == nil {
		notify = func() {}
	}
	rd := lines.NewReader(src, base.Size(), block)
	rd.SetMaxLine(maxLine)

	f := &Filtered{
		src:    src,
		base:   base,
		rules:  rules,
		block:  block,
		poll:   poll,
		stride: ckStride,
		rd:     rd,
		notify: notify,
		nudge:  make(chan struct{}, 1),
	}

	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	f.wg.Add(1)
	go f.run(ctx)
	return f
}

// Close stops the background scan and waits for it to exit.
func (f *Filtered) Close() {
	if f.cancel != nil {
		f.cancel()
		f.wg.Wait()
	}
}

// Rules is the rule set this view was built from.
func (f *Filtered) Rules() *filter.Set { return f.rules }

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

// run is the background scan. It follows the file: when it reaches the end it
// waits for the indexer to report more bytes and carries on.
func (f *Filtered) run(ctx context.Context) {
	defer f.wg.Done()

	size := f.base.Size()
	sc := lines.NewScanner(f.src, 0, size, f.block)
	sc.SetAdvise(size > 2<<30)

	tick := time.NewTicker(f.poll)
	defer tick.Stop()

	var (
		offs = make([]int64, 0, flushMatches)
		srcs = make([]int64, 0, flushMatches)
		gen  = f.base.Gen()
	)

	for {
		if ctx.Err() != nil {
			return
		}

		if g := f.base.Gen(); g != gen {
			// The file was truncated or rotated: start over.
			gen = g
			f.reset()
			sc = lines.NewScanner(f.src, 0, f.base.Size(), f.block)
		}

		size = f.base.Size()
		sc.SetLimit(size)

		var (
			pos  = f.consumed()
			src  = f.sourceLines()
			seen int64
		)
		sc.SeekTo(pos)

		for sc.Scan() {
			if !sc.Terminated() {
				break
			}
			if f.rules.Pass(sc.Bytes()) {
				offs = append(offs, sc.Offset())
				srcs = append(srcs, src)
			}
			src++
			pos = sc.Next()
			seen++

			if len(offs) >= flushMatches || seen >= flushLines {
				f.publish(offs, srcs, pos, src, false)
				offs, srcs, seen = offs[:0], srcs[:0], 0
				f.notify()
				if ctx.Err() != nil {
					return
				}
			}
		}

		caught := pos >= size && f.base.Caught()
		f.publish(offs, srcs, pos, src, caught)
		offs, srcs = offs[:0], srcs[:0]
		f.notify()

		if err := sc.Err(); err != nil {
			f.setErr(err)
			return
		}

		if pos < f.base.Size() {
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

func (f *Filtered) consumed() int64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.scanned
}

func (f *Filtered) sourceLines() int64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.srcLine
}

func (f *Filtered) publish(offs, srcs []int64, scanned, srcLine int64, done bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range offs {
		if f.n%f.stride == 0 {
			f.ckOff = append(f.ckOff, offs[i])
			f.ckSrc = append(f.ckSrc, srcs[i])
		}
		f.n++
	}
	if len(f.ckOff) >= ckMax {
		k := 0
		for i := 0; i < len(f.ckOff); i += 2 {
			f.ckOff[k], f.ckSrc[k] = f.ckOff[i], f.ckSrc[i]
			k++
		}
		f.ckOff, f.ckSrc = f.ckOff[:k], f.ckSrc[:k]
		f.stride *= 2
	}
	f.scanned = scanned
	f.srcLine = srcLine
	f.done = done
}

func (f *Filtered) reset() {
	f.mu.Lock()
	f.ckOff, f.ckSrc = f.ckOff[:0], f.ckSrc[:0]
	f.n, f.scanned, f.srcLine = 0, 0, 0
	f.stride = ckStride
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

// checkpoint returns the nearest recorded position at or before filtered line i.
func (f *Filtered) checkpoint(i int64) (off, src, match int64, ok bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if len(f.ckOff) == 0 || i < 0 {
		return 0, 0, 0, false
	}
	j := i / f.stride
	if j >= int64(len(f.ckOff)) {
		j = int64(len(f.ckOff)) - 1
	}
	return f.ckOff[j], f.ckSrc[j], j * f.stride, true
}

// IndexForSource returns the view index of the first surviving line whose
// source line number is at or after src.
//
// This is how a position is preserved when a filter is added or removed: the
// checkpoint table is sorted by source line, so a binary search plus a short
// forward walk finds the anchor without rescanning the file.
func (f *Filtered) IndexForSource(src int64) (int64, bool) {
	f.mu.RLock()
	if len(f.ckSrc) == 0 {
		f.mu.RUnlock()
		return 0, false
	}
	j := sort.Search(len(f.ckSrc), func(i int) bool { return f.ckSrc[i] >= src })
	var off, curSrc, match int64
	if j == 0 {
		off, curSrc, match = f.ckOff[0], f.ckSrc[0], 0
	} else {
		off, curSrc, match = f.ckOff[j-1], f.ckSrc[j-1], int64(j-1)*f.stride
	}
	total := f.n
	f.mu.RUnlock()

	if curSrc >= src {
		return match, true
	}

	sc := lines.NewScanner(f.src, off, f.base.Size(), f.block)
	for sc.Scan() && match < total {
		if f.rules.Pass(sc.Bytes()) {
			if curSrc >= src {
				return match, true
			}
			match++
		}
		curSrc++
	}
	return total, false
}

// At implements View.
func (f *Filtered) At(i int64, dst []byte) (Line, error) {
	if i < 0 || i >= f.Len() {
		return Line{}, ErrRange
	}

	off, src, match, ok := f.checkpoint(i)
	if !ok {
		return Line{}, ErrRange
	}
	gen := f.base.Gen()
	for _, c := range f.cache {
		if c.okay && c.gen == gen && c.idx <= i && c.idx > match {
			off, src, match = c.off, c.src, c.idx
		}
	}

	sc := f.rd.Scanner()
	sc.SetLimit(f.base.Size())
	sc.SeekTo(off)

	for sc.Scan() {
		if f.rules.Pass(sc.Bytes()) {
			if match == i {
				text := append(dst, sc.Bytes()...)
				ln := Line{
					Text:      text,
					Num:       src + 1,
					Off:       sc.Offset(),
					Next:      sc.Next(),
					Truncated: sc.Truncated(),
				}
				f.cache[0] = cacheEntry{idx: i, off: sc.Offset(), src: src, gen: gen, okay: true}
				f.cache[1] = cacheEntry{idx: i + 1, off: sc.Next(), src: src + 1, gen: gen, okay: true}
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
	off, src, match := int64(0), int64(0), int64(0)
	if ckOff, ckSrc, ckMatch, ok := f.checkpoint(from); ok {
		off, src, match = ckOff, ckSrc, ckMatch
	}

	sc := lines.NewScanner(f.src, off, f.base.Size(), f.block)
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
