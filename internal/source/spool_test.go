package source

import "testing"

func TestSpooledStdinIsReportedAsStreaming(t *testing.T) {
	// Streaming is what tells the exit path whether a producer may still be
	// attached; a finished one must not be reported.
	s := &Source{stdin: true}
	if !s.Streaming() {
		t.Error("a spool that has not finished should be streaming")
	}
	s.spoolDone.Store(true)
	if s.Streaming() {
		t.Error("a finished spool should not be streaming")
	}
	if (&Source{}).Streaming() {
		t.Error("a file source should never be streaming")
	}
}
