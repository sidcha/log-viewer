package ui

import "time"

// frameInterval is the shortest time between frames that background work can
// ask for: about sixty a second, faster than a person reads.
const frameInterval = 16 * time.Millisecond

// pacer turns a stream of repaint requests into at most one frame per
// interval.
//
// The indexer and a filter scan wake the viewer after every batch they
// publish, which on a fast disk is hundreds of times a second. Painting on
// each of them would spend the CPU the scan needs on frames nobody can see,
// and each frame runs every syntax rule over every visible line. Requests
// inside the interval are folded into one frame at its end.
type pacer struct {
	interval time.Duration
	last     time.Time
	timer    *time.Timer
	armed    bool
}

func newPacer(interval time.Duration) *pacer {
	t := time.NewTimer(time.Hour)
	t.Stop()
	return &pacer{interval: interval, timer: t}
}

// request reports whether a frame may be painted now. When it may not, a frame
// is scheduled for when it may, and arrives on C.
func (p *pacer) request(now time.Time) bool {
	if p.armed {
		return false
	}
	if wait := p.interval - now.Sub(p.last); wait > 0 {
		p.timer.Reset(wait)
		p.armed = true
		return false
	}
	return true
}

// painted records that a frame went out.
func (p *pacer) painted(now time.Time) { p.last = now }

// C delivers the scheduled frame.
func (p *pacer) C() <-chan time.Time { return p.timer.C }

// fired acknowledges a delivery from C.
func (p *pacer) fired() { p.armed = false }

func (p *pacer) stop() { p.timer.Stop() }
