package highlight

import (
	"bytes"
	"sync"
	"sync/atomic"
	"time"
)

// DetectOptions configures a Detector.
type DetectOptions struct {
	Profiles []*Profile
	Markers  []*Marker
	Generic  *Profile
	// Base is the file's base name, matched against globs for the first
	// segment only: a later segment is not what the file is named for.
	Base      string
	Lines     int           // detect-lines
	Threshold int           // detect-threshold, percent
	Wait      time.Duration // detect-wait
	// Follow means the user asked to watch the input grow, so the part
	// already written is not decided just because it has been read.
	Follow bool
	// Notify is called after a segment is decided. It must not block.
	Notify func()
}

// bannerLines is how far into an undecided segment a profile's marker
// decides that segment instead of opening another: a banner a few lines
// into a file names the file.
const bannerLines = 8

// Detector divides a log into segments and chooses each one's profile. It
// is an index.Observer: it sees each line as the indexer reads it, so it
// adds no reads of its own.
//
// Most lines cost one pass for escapes over their first 128 bytes and one
// byte search per marker anchor (see markerScan). Only while a segment is
// being sampled, or on a marker, is the lock taken.
type Detector struct {
	o    DetectOptions
	segs *Segments
	scan markerScan

	pending atomic.Bool
	scratch []byte // indexer goroutine only

	mu       sync.Mutex
	notify   func()
	woken    bool // a decision since the lock was taken, to notify of
	closed   bool
	caughtUp bool // the input as it was when opened has been read
	sample   [][]byte
	seen     int // lines, blank included, since the pending segment began
	timer    *time.Timer
	epoch    uint64 // bumped per segment and on Close, so a late timer does nothing
}

// NewDetector returns a detector with the first segment open at offset 0.
func NewDetector(o DetectOptions) *Detector {
	d := &Detector{o: o, notify: o.Notify, segs: &Segments{generic: o.Generic}, scan: newMarkerScan(o.Markers)}
	d.segs.restart()
	d.pending.Store(true)
	return d
}

// Segments is the table the detector writes, for the renderer.
func (d *Detector) Segments() *Segments { return d.segs }

// SetNotify sets what is called after a segment is decided, for when the
// thing to wake is built after the detector.
func (d *Detector) SetNotify(f func()) {
	d.mu.Lock()
	d.notify = f
	d.mu.Unlock()
}

// Close stops a pending timer, and no timer decides anything after it.
// Lines and catch-ups observed after Close may still decide, so the owner
// stops the indexer first.
func (d *Detector) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	d.epoch++
	d.stopTimer()
}

// ObserveLine implements index.Observer.
func (d *Detector) ObserveLine(n, off int64, text []byte) {
	pending := d.pending.Load()
	if !pending && len(d.o.Markers) == 0 {
		return
	}
	width := markerWindow
	if pending {
		width = sampleLineBytes
	}
	vis := d.visible(text, width)

	var m *Marker
	if len(d.o.Markers) > 0 {
		m = d.scan.find(vis[:min(len(vis), markerWindow)])
	}
	if m == nil && !pending {
		return
	}

	d.mu.Lock()
	defer d.unlockAndNotify()
	if m != nil && d.onMarker(m, n, off) {
		return
	}
	if d.pending.Load() {
		d.addSample(vis)
	}
}

// ObserveCaughtUp implements index.Observer. The indexer keeps following
// the file, so only the first catch-up says something: the input as it was
// when opened has been read, and its last segment is decided from whatever
// lines it holds. Every later segment waits for detect-lines or
// detect-wait.
func (d *Detector) ObserveCaughtUp(streaming bool) {
	if streaming {
		return
	}
	d.mu.Lock()
	defer d.unlockAndNotify()
	if d.caughtUp {
		return
	}
	d.caughtUp = true
	if !d.o.Follow && d.pending.Load() && len(d.sample) > 0 {
		d.decideFromSample()
	}
}

// ObserveReset implements index.Observer.
func (d *Detector) ObserveReset() {
	d.mu.Lock()
	defer d.unlockAndNotify()
	d.stopTimer()
	d.epoch++
	d.caughtUp = false
	d.segs.restart()
	d.startSample()
	d.wake()
}

// onMarker handles a marker line, reporting whether the line was used up
// by it rather than belonging to a sample.
func (d *Detector) onMarker(m *Marker, n, off int64) bool {
	if d.pending.Load() && d.seen < bannerLines {
		if m.Profile == nil {
			return false
		}
		d.decide(m.Profile, Marked, 0)
		return true
	}
	if d.segs.Len() >= MaxSegments {
		return false
	}
	if d.pending.Load() {
		d.decideFromSample()
	}
	seg := Segment{Off: off, Line: n, Profile: m.Profile, Reason: Marked}
	if m.Profile == nil {
		seg.Reason = Detecting
	}
	d.segs.open(seg)
	d.epoch++
	if m.Profile == nil {
		d.startSample()
	}
	d.wake()
	return true
}

// visible is visiblePrefix without the copy for the usual line, which has
// no escape sequence in the part looked at. The result is valid until the
// next call.
func (d *Detector) visible(text []byte, width int) []byte {
	head := text[:min(len(text), width)]
	if bytes.IndexByte(head, 0x1b) >= 0 {
		d.scratch = visiblePrefix(d.scratch[:0], text, width)
		return d.scratch
	}
	if len(head) == len(text) {
		head = bytes.TrimSuffix(head, []byte{'\r'})
	}
	return head
}

func (d *Detector) addSample(vis []byte) {
	d.seen++
	if isBlank(vis) {
		return
	}
	if len(d.sample) == 0 {
		d.armTimer()
	}
	d.sample = append(d.sample, bytes.Clone(vis))
	if len(d.sample) >= d.o.Lines {
		d.decideFromSample()
	}
}

func (d *Detector) startSample() {
	d.pending.Store(true)
	clear(d.sample)
	d.sample = d.sample[:0]
	d.seen = 0
}

func (d *Detector) decideFromSample() {
	base := ""
	if d.segs.Len() == 1 {
		base = d.o.Base
	}
	if p, pct := Score(d.o.Profiles, d.sample, base, d.o.Threshold); p != nil {
		d.decide(p, Detected, pct)
		return
	}
	d.decide(d.o.Generic, NoMatch, 0)
}

func (d *Detector) decide(p *Profile, r Reason, score int) {
	d.stopTimer()
	d.segs.settleLast(p, r, score)
	d.pending.Store(false)
	clear(d.sample)
	d.sample = d.sample[:0]
	d.seen = 0
	d.wake()
}

// armTimer starts the wait from the segment's first sampled line, not from
// when it opened: a pipe that is quiet before its first line has told us
// nothing yet.
func (d *Detector) armTimer() {
	if d.o.Wait <= 0 || d.closed {
		return
	}
	epoch := d.epoch
	d.timer = time.AfterFunc(d.o.Wait, func() {
		d.mu.Lock()
		defer d.unlockAndNotify()
		if d.epoch == epoch && d.pending.Load() && len(d.sample) > 0 {
			d.decideFromSample()
		}
	})
}

func (d *Detector) stopTimer() {
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

// wake asks for Notify once the lock is let go, so that Notify may call
// back into the detector, and the table it announces is already written.
func (d *Detector) wake() { d.woken = true }

func (d *Detector) unlockAndNotify() {
	var notify func()
	if d.woken {
		notify = d.notify
		d.woken = false
	}
	d.mu.Unlock()
	if notify != nil {
		notify()
	}
}
