package highlight

import (
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

var (
	tKernel = &Profile{Name: "kernel", Detects: []*regexp.Regexp{regexp.MustCompile(`^\[[ ]*[0-9]+\.[0-9]+\]`)}}
	tUboot  = &Profile{Name: "uboot"}
	tGen    = &Profile{Name: "generic"}
)

func testMarkers() []*Marker {
	return []*Marker{
		{Lit: []byte("U-Boot "), Re: regexp.MustCompile(`^U-Boot [0-9]{4}\.[0-9]{2}`), Profile: tUboot},
		{Lit: []byte("Starting kernel")},
	}
}

func newTestDetector(o DetectOptions) *Detector {
	if o.Profiles == nil {
		o.Profiles = []*Profile{tKernel, tUboot, tGen}
	}
	if o.Markers == nil {
		o.Markers = testMarkers()
	}
	o.Generic = tGen
	if o.Lines == 0 {
		o.Lines = 4
	}
	if o.Threshold == 0 {
		o.Threshold = 30
	}
	return NewDetector(o)
}

// feed shows the detector each line of text, numbering lines and offsets
// the way the indexer does.
func feed(d *Detector, startLine, startOff int64, text string) (int64, int64) {
	n, off := startLine, startOff
	for _, l := range strings.SplitAfter(text, "\n") {
		if l == "" {
			continue
		}
		d.ObserveLine(n, off, []byte(strings.TrimSuffix(l, "\n")))
		n++
		off += int64(len(l))
	}
	return n, off
}

func segs(d *Detector) string {
	s := d.Segments()
	s.mu.RLock()
	defer s.mu.RUnlock()
	var parts []string
	for _, seg := range s.list {
		parts = append(parts, seg.String())
	}
	return strings.Join(parts, " | ")
}

const bootCapture = "U-Boot 2024.01 (Jan 01 2024)\nDRAM:  1 GiB\n=> boot\nStarting kernel ...\n[    0.000000] Booting Linux\n[    0.001000] Linux version 6.6\n[    0.002000] x\n[    0.003000] y\n"

func TestDetectorSplitsABootCaptureIntoStages(t *testing.T) {
	d := newTestDetector(DetectOptions{})
	defer d.Close()
	feed(d, 0, 0, bootCapture)
	d.ObserveCaughtUp(false)

	want := "uboot (marker, line 1) | kernel (detected, 100%)"
	if got := segs(d); got != want {
		t.Errorf("segments = %s, want %s", got, want)
	}
	s := d.Segments()
	if s.ProfileAt(0) != tUboot || s.ProfileAt(int64(strings.Index(bootCapture, "[    0.002"))) != tKernel {
		t.Error("lines did not get the profile of the segment they are in")
	}
}

func TestDetectorDecidesAfterTheSampleFills(t *testing.T) {
	d := newTestDetector(DetectOptions{Follow: true})
	defer d.Close()
	feed(d, 0, 0, "[ 1.0] a\n[ 1.1] b\n[ 1.2] c\n")
	if got := d.Segments().SegmentAt(0).Reason; got != Detecting {
		t.Fatalf("decided after 3 of 4 lines: %v", got)
	}
	feed(d, 3, 30, "[ 1.3] d\n")
	if got := d.Segments().SegmentAt(0); got.Profile != tKernel || got.Reason != Detected {
		t.Errorf("after 4 lines: %v", got)
	}
}

func TestDetectorDrawsAnUndecidedSegmentWithGeneric(t *testing.T) {
	d := newTestDetector(DetectOptions{Follow: true})
	defer d.Close()
	feed(d, 0, 0, "[ 1.0] a\n")
	if d.Segments().ProfileAt(0) != tGen {
		t.Error("an undecided segment was not drawn with generic")
	}
}

// A pipe that is silent for longer than the wait still gets
// detected from its real output.
func TestDetectorWaitsForTheFirstLineBeforeTiming(t *testing.T) {
	decided := make(chan struct{}, 4)
	d := newTestDetector(DetectOptions{Follow: true, Wait: 20 * time.Millisecond,
		Notify: func() { decided <- struct{}{} }})
	defer d.Close()

	time.Sleep(60 * time.Millisecond) // silence, longer than the wait
	if got := d.Segments().SegmentAt(0).Reason; got != Detecting {
		t.Fatalf("decided on silence: %v", got)
	}
	feed(d, 0, 0, "[ 1.0] a\n[ 1.1] b\n")
	select {
	case <-decided:
	case <-time.After(2 * time.Second):
		t.Fatal("the wait never decided the segment")
	}
	if got := d.Segments().SegmentAt(0).Profile; got != tKernel {
		t.Errorf("decided %v from a partial sample, want kernel", got)
	}
}

func TestDetectorDecidesAFinishedFileWhenCaughtUp(t *testing.T) {
	d := newTestDetector(DetectOptions{})
	defer d.Close()
	feed(d, 0, 0, "[ 1.0] a\n")
	d.ObserveCaughtUp(true) // still streaming: keep waiting
	if d.Segments().SegmentAt(0).Reason != Detecting {
		t.Fatal("decided while a pipe was still streaming")
	}
	d.ObserveCaughtUp(false)
	if d.Segments().SegmentAt(0).Profile != tKernel {
		t.Error("a finished file was not decided when indexing caught up")
	}
}

func TestDetectorTakesABannerAsTheFirstSegmentsProfile(t *testing.T) {
	d := newTestDetector(DetectOptions{})
	defer d.Close()
	feed(d, 0, 0, "\nnoise\nU-Boot 2024.01 (x)\n=> \n")
	if got := segs(d); got != "uboot (marker, line 1)" {
		t.Errorf("segments = %s, want one uboot segment", got)
	}
}

// Markers match through colour and CR line endings.
func TestDetectorSeesMarkersThroughEscapesAndCR(t *testing.T) {
	d := newTestDetector(DetectOptions{})
	defer d.Close()
	n, off := feed(d, 0, 0, "[ 1.0] a\n[ 1.1] b\n[ 1.2] c\n[ 1.3] d\n[ 1.4] e\n[ 1.5] f\n[ 1.6] g\n[ 1.7] h\n[ 1.8] i\n")
	feed(d, n, off, "\x1b[1mU-Boot 2024.01\x1b[0m\r\n")
	if got := d.Segments().Len(); got != 2 {
		t.Errorf("segments = %s, want a second one for the coloured banner", segs(d))
	}
}

func TestDetectorStopsOpeningSegmentsAtTheCap(t *testing.T) {
	d := newTestDetector(DetectOptions{})
	defer d.Close()
	var n, off int64
	filler := strings.Repeat("[ 1.0] x\n", bannerLines)
	for i := 0; i < MaxSegments+5; i++ {
		n, off = feed(d, n, off, filler+"U-Boot 2024.01\n")
	}
	if got := d.Segments().Len(); got != MaxSegments {
		t.Errorf("segments = %d, want the cap %d", got, MaxSegments)
	}
}

// A truncated or replaced file starts detection over.
func TestDetectorStartsOverOnReset(t *testing.T) {
	d := newTestDetector(DetectOptions{})
	defer d.Close()
	feed(d, 0, 0, bootCapture)
	d.ObserveCaughtUp(false)
	d.ObserveReset()
	if got := d.Segments().Len(); got != 1 || d.Segments().SegmentAt(0).Reason != Detecting {
		t.Fatalf("after reset: %s", segs(d))
	}
	feed(d, 0, 0, "[ 1.0] a\n")
	d.ObserveCaughtUp(false)
	if d.Segments().SegmentAt(0).Profile != tKernel {
		t.Error("the new file was not detected")
	}
}

func TestSegmentsVersionChangesOnEveryDecision(t *testing.T) {
	d := newTestDetector(DetectOptions{})
	defer d.Close()
	v0 := d.Segments().Version()
	feed(d, 0, 0, "U-Boot 2024.01\n")
	if d.Segments().Version() == v0 {
		t.Error("a marker decision did not change the version")
	}
}

func TestSegmentFitsIn32Bytes(t *testing.T) {
	if got := unsafe.Sizeof(Segment{}); got > 32 {
		t.Errorf("Segment is %d bytes; MaxSegments assumes at most 32", got)
	}
}

func TestDetectorDecidesNothingOnceClosed(t *testing.T) {
	const wait = 10 * time.Millisecond
	for _, closeFirst := range []bool{false, true} {
		var notified atomic.Int32
		d := newTestDetector(DetectOptions{Follow: true, Wait: wait,
			Notify: func() { notified.Add(1) }})
		if closeFirst {
			d.Close()
			feed(d, 0, 0, "[ 1.0] a\n")
		} else {
			feed(d, 0, 0, "[ 1.0] a\n")
			d.Close()
		}
		time.Sleep(5 * wait)
		if notified.Load() != 0 || d.Segments().SegmentAt(0).Reason != Detecting {
			t.Errorf("close first %v: decided after Close: %s", closeFirst, segs(d))
		}
	}
}

func TestDetectorNotifiesWithoutItsLock(t *testing.T) {
	var d *Detector
	d = newTestDetector(DetectOptions{Notify: func() { d.SetNotify(nil) }})
	done := make(chan struct{})
	go func() {
		feed(d, 0, 0, "U-Boot 2024.01\n")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a Notify that calls back into the detector deadlocked")
	}
}

// Indexing always follows the file, so only the first catch-up means the
// file as it was opened has been read; a segment opened later waits for its
// lines.
func TestDetectorDecidesASegmentOpenedAfterCatchingUpFromItsLines(t *testing.T) {
	d := newTestDetector(DetectOptions{})
	defer d.Close()
	n, off := feed(d, 0, 0, strings.Repeat("hello world\n", bannerLines+1))
	d.ObserveCaughtUp(false)
	n, off = feed(d, n, off, "Starting kernel ...\n")
	d.ObserveCaughtUp(false)
	feed(d, n, off, "[ 1.0] a\n[ 1.1] b\n[ 1.2] c\n[ 1.3] d\n")
	if got := d.Segments().Describe(off); !strings.HasPrefix(got, "kernel (detected") {
		t.Errorf("segment after the marker = %s, want kernel detected (all: %s)", got, segs(d))
	}
}

func TestDetectorLeavesAnEmptyFileUndecidedWhenCaughtUp(t *testing.T) {
	d := newTestDetector(DetectOptions{})
	defer d.Close()
	d.ObserveCaughtUp(false)
	if got := d.Segments().SegmentAt(0).Reason; got != Detecting {
		t.Fatalf("an empty file was decided: %s", segs(d))
	}
	feed(d, 0, 0, "[ 1.0] a\n[ 1.1] b\n[ 1.2] c\n[ 1.3] d\n")
	if got := d.Segments().Describe(0); !strings.HasPrefix(got, "kernel (detected") {
		t.Errorf("after lines were written = %s, want kernel detected", got)
	}
}

func TestDetectorIgnoresATimerArmedBeforeAReset(t *testing.T) {
	const wait = 10 * time.Millisecond
	for _, lineAfterReset := range []bool{false, true} {
		d := newTestDetector(DetectOptions{Follow: true, Wait: wait})
		feed(d, 0, 0, "[ 1.0] a\n")
		d.ObserveReset()
		want := "generic (detecting)"
		if lineAfterReset {
			feed(d, 0, 0, "hello world\n")
			want = "generic (no match)"
		}
		time.Sleep(5 * wait)
		if got := d.Segments().Describe(0); got != want {
			t.Errorf("line after reset %v: %s, want %s", lineAfterReset, got, want)
		}
		d.Close()
	}
}

func TestDetectorSegmentsCanBeReadWhileLinesArrive(t *testing.T) {
	d := newTestDetector(DetectOptions{Follow: true, Wait: time.Millisecond})
	defer d.Close()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			s := d.Segments()
			_ = s.SegmentAt(0).String()
			_ = s.SegmentAt(1 << 20)
			_ = s.Version()
		}
	}()
	var n, off int64
	for i := 0; i < 50; i++ {
		n, off = feed(d, n, off, bootCapture+"hello\n")
		d.ObserveCaughtUp(false)
	}
	close(stop)
	<-done
}
