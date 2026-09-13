package ui

import (
	"testing"
	"time"
)

func TestPacerFoldsABurstIntoOneFrame(t *testing.T) {
	p := newPacer(50 * time.Millisecond)
	defer p.stop()
	now := time.Now()

	if !p.request(now) {
		t.Fatal("the first request should paint at once")
	}
	p.painted(now)

	for i := 1; i <= 100; i++ {
		if p.request(now.Add(time.Duration(i) * 100 * time.Microsecond)) {
			t.Fatalf("request %d inside the interval painted", i)
		}
	}
	select {
	case <-p.C():
		p.fired()
	case <-time.After(time.Second):
		t.Fatal("the folded frame never arrived")
	}
	select {
	case <-p.C():
		t.Fatal("a burst scheduled more than one frame")
	case <-time.After(80 * time.Millisecond):
	}
}

func TestPacerPaintsAtOnceAfterAQuietSpell(t *testing.T) {
	p := newPacer(10 * time.Millisecond)
	defer p.stop()
	now := time.Now()
	p.painted(now)
	if !p.request(now.Add(20 * time.Millisecond)) {
		t.Error("a request after the interval should paint at once")
	}
}
