package ui

import (
	"sync/atomic"

	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/highlight"
	"github.com/sidcha/log-viewer/internal/index"
	"github.com/sidcha/log-viewer/internal/ruleset"
	"github.com/sidcha/log-viewer/internal/source"
	"github.com/sidcha/log-viewer/internal/view"
)

// buffer is one open file and everything about how it is being read: its
// views, its rules and where the reader is in it. App embeds the one on
// screen, so switching files swaps all of it at once and nothing about one
// file leaks into another.
type buffer struct {
	app *App

	src *source.Source
	idx *index.Index

	base *view.Base
	filt *view.Filtered
	// filtPtr mirrors filt for goroutines other than the event loop.
	filtPtr atomic.Pointer[view.Filtered]
	rules   *filter.Set
	gen     int // bumped whenever the active view is replaced

	// eng is per file: its marks come from this file's rules and its span
	// cache is keyed by offsets that only mean something within one index.
	eng *highlight.Engine

	top       int64
	cur       int64 // cursor line, in view coordinates
	curCol    int   // cursor byte offset within that line
	wantCol   int   // column to return to after moving between lines
	sel       selection
	hoff      int
	follow    bool
	bypass    bool
	searchCur int64 // view line of the last search hit, or -1

	// Where the rules came from, and what they looked like when last written,
	// so quitting knows whether there is anything to lose.
	rulesPath   string
	savedDigest string
	// streamSaved records that piped input has been written somewhere that
	// outlives the process, and everFollowed that this session was watching
	// the input arrive rather than reading through it.
	streamSaved  bool
	everFollowed bool

	// A position request that cannot be honoured until the filter scan has
	// reached the relevant part of the file.
	pendingAnchor int64
	havePending   bool

	lastLen  int64
	lastScan int64
}

// wake tells this buffer's filter scan that more of the file has arrived,
// and asks for a frame only if the buffer is on screen. It runs on the
// indexer's goroutine.
func (b *buffer) wake() {
	if f := b.filtPtr.Load(); f != nil {
		f.Nudge()
	}
	if b.app.shown.Load() == b {
		b.app.requestFrame()
	}
}

func (b *buffer) hasUnsavedRules(cs filter.Case) bool {
	f := ruleset.From(b.rules, cs)
	return !f.Empty() && f.Digest() != b.savedDigest
}

func (b *buffer) close() {
	if b.filt != nil {
		b.filt.Close()
		b.filt = nil
		b.filtPtr.Store(nil)
	}
}

// AddBuffer opens another file in the viewer and puts it on screen. The
// returned function is what the file's indexer and detector call when there
// is more to see; it is safe from any goroutine.
func (a *App) AddBuffer(src *source.Source, idx *index.Index, prof highlight.Profiler) func() {
	opt := a.opts()
	b := &buffer{
		app:   a,
		src:   src,
		idx:   idx,
		base:  view.NewBase(src, idx, opt.BlockSize, opt.MaxLineBytes),
		rules: filter.NewSet(),
		eng:   highlight.NewEngine(opt.MaxScanBytes, opt.MaxHighlights),
		// Following is where the session starts, not a setting it tracks:
		// scrolling away turns it off, and a later :set must not undo that.
		follow:       opt.Follow,
		everFollowed: opt.Follow,
		searchCur:    -1,
	}
	b.eng.Profiles = prof
	a.bufs = append(a.bufs, b)
	a.showBuffer(len(a.bufs) - 1)

	set := a.rules
	for _, p := range a.cfg.Presets() {
		set = set.Add(filter.Rule{Action: p.Action, M: p.M, Enabled: true, Color: p.Color})
	}
	a.applyRules(set, false)
	return b.wake
}

// showBuffer puts buffer i on screen. A selection belongs to the file it
// was made in, so it ends here.
func (a *App) showBuffer(i int) {
	if a.buffer != nil {
		a.endVisual()
	}
	a.buffer = a.bufs[i]
	a.shown.Store(a.buffer)
}

func (a *App) bufferIndex() int {
	for i, b := range a.bufs {
		if b == a.buffer {
			return i
		}
	}
	return -1
}
