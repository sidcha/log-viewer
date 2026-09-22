package highlight

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

// Reason says how a segment's profile was chosen.
type Reason uint8

const (
	Detecting Reason = iota // sample still being gathered; drawn with generic
	Marked                  // a profile's marker named it
	Detected                // its sample scored at or above the threshold
	NoMatch                 // nothing scored; generic
)

func (r Reason) String() string {
	switch r {
	case Detecting:
		return "detecting"
	case Marked:
		return "marker"
	case Detected:
		return "detected"
	}
	return "no match"
}

// Segment is a run of lines, from Off to the next segment, drawn with one
// profile.
type Segment struct {
	Off     int64
	Line    int64 // 0-based number of its first line
	Profile *Profile
	Reason  Reason
	Score   int32 // percent, when Detected; int32 keeps an entry 32 bytes
}

func (s Segment) String() string {
	name := "none"
	if s.Profile != nil {
		name = s.Profile.Name
	}
	switch s.Reason {
	case Marked:
		return fmt.Sprintf("%s (marker, line %d)", name, s.Line+1)
	case Detected:
		return fmt.Sprintf("%s (detected, %d%%)", name, s.Score)
	}
	return fmt.Sprintf("%s (%s)", name, s.Reason)
}

// MaxSegments caps the table at 1 MiB, 32 bytes an entry. Past it markers
// are ignored and the last segment runs to the end, so memory stays O(1) in
// file size however often a device rebooted into the capture.
const MaxSegments = 1 << 15

// Segments is the table of segments found so far, in file order. The
// Detector writes it; the renderer reads it, once per line drawn.
type Segments struct {
	mu      sync.RWMutex
	list    []Segment
	generic *Profile
	version atomic.Uint64
}

// SegmentAt returns the segment holding the line at off. An undecided one
// comes back with the generic profile, which is what it is drawn with.
func (s *Segments) SegmentAt(off int64) Segment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := sort.Search(len(s.list), func(i int) bool { return s.list[i].Off > off }) - 1
	if i < 0 {
		return Segment{Profile: s.generic, Reason: Detecting}
	}
	seg := s.list[i]
	if seg.Reason == Detecting {
		seg.Profile = s.generic
	}
	return seg
}

func (s *Segments) ProfileAt(off int64) *Profile { return s.SegmentAt(off).Profile }
func (s *Segments) Describe(off int64) string    { return s.SegmentAt(off).String() }
func (s *Segments) Version() uint64              { return s.version.Load() }

// Len is the number of segments.
func (s *Segments) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.list)
}

// open appends a segment, reporting false when the table is full.
func (s *Segments) open(seg Segment) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.list) >= MaxSegments {
		return false
	}
	s.list = append(s.list, seg)
	s.version.Add(1)
	return true
}

// settleLast records the decision for the last segment.
func (s *Segments) settleLast(p *Profile, r Reason, score int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	last := &s.list[len(s.list)-1]
	last.Profile, last.Reason, last.Score = p, r, int32(score)
	s.version.Add(1)
}

// restart leaves one undecided segment at the start of the file.
func (s *Segments) restart() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.list = append(s.list[:0], Segment{Reason: Detecting})
	s.version.Add(1)
}
