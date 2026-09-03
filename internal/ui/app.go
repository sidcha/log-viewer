// Package ui is the terminal front end: event loop, rendering, prompts.
package ui

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/highlight"
	"github.com/sidcha/log-viewer/internal/index"
	"github.com/sidcha/log-viewer/internal/source"
	"github.com/sidcha/log-viewer/internal/view"
)

// idleAfter is how long the UI must be quiet before memory is handed back to
// the OS. Go's allocator holds freed spans as heap headroom, which is exactly
// the behaviour a viewer for very large files should not have.
const idleAfter = 3 * time.Second

// busyOp is a cancellable background operation with progress.
type busyOp struct {
	what   string
	cancel context.CancelFunc
	done   atomic.Int64
	total  atomic.Int64
}

// App holds all viewer state. Every field is owned by the event-loop goroutine;
// background work communicates via the post channel.
type App struct {
	scr tcell.Screen
	cfg *config.Config
	src *source.Source
	idx *index.Index

	base  *view.Base
	filt  *view.Filtered
	rules *filter.Set
	gen   int // bumped whenever the active view is replaced

	eng *highlight.Engine
	lay *layout

	top      int64
	hoff     int
	follow   bool
	wrap     bool
	lineNums bool
	bypass   bool
	statusOn string

	search searchState
	prompt *prompt
	prev   *preview
	ov     *overlay
	busy   *busyOp

	// A position request that cannot be honoured until the filter scan has
	// reached the relevant part of the file.
	pendingAnchor int64
	havePending   bool

	msg      string
	msgStyle tcell.Style
	msgUntil time.Time

	post chan func()
	wake chan struct{}
	done chan struct{}
	quit bool

	// frame scratch, reused to keep drawing allocation-free
	linebuf []byte
	styles  []tcell.Style
	locs    [][2]int
	rows    int
	cols    int

	lastLen  int64
	lastScan int64
	lastAct  time.Time
	freed    bool

	histPattern []string
	histPath    []string
	histCmd     []string
}

type searchState struct {
	m    filter.Matcher
	pat  string
	back bool
	cur  int64
}

// New builds an App. The caller owns the screen's lifetime.
func New(scr tcell.Screen, cfg *config.Config, src *source.Source, idx *index.Index, profile *highlight.Profile) *App {
	block := cfg.Int("block-size")
	maxLine := cfg.Int("max-line-bytes")

	a := &App{
		scr:      scr,
		cfg:      cfg,
		src:      src,
		idx:      idx,
		base:     view.NewBase(src, idx, block, maxLine),
		rules:    filter.NewSet(),
		eng:      highlight.NewEngine(cfg.Int("max-scan-bytes"), 64),
		lay:      newLayout(cfg.Int("tab-width")),
		follow:   cfg.Bool("follow"),
		wrap:     cfg.Bool("wrap"),
		lineNums: cfg.Bool("line-numbers"),
		statusOn: cfg.Str("status-line"),
		post:     make(chan func(), 32),
		wake:     make(chan struct{}, 1),
		done:     make(chan struct{}),
		lastAct:  time.Now(),
	}
	a.eng.Profile = profile
	a.search.cur = -1

	set := a.rules
	for _, p := range cfg.Presets() {
		m, err := filter.Compile(p.Pattern, cfg.Case())
		if err != nil {
			a.errorf("rule %q: %v", p.Pattern, err)
			continue
		}
		set = set.Add(filter.Rule{Action: p.Action, M: m, Enabled: true, Color: p.Color})
	}
	a.applyRules(set, false)
	return a
}

// Wake asks the event loop to repaint. It is safe to call from any goroutine
// and never blocks: a pending wake already covers a new one.
func (a *App) Wake() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

func (a *App) postFn(f func()) {
	select {
	case a.post <- f:
	case <-a.done:
	}
}

// Run drives the event loop until the user quits or ctx is cancelled.
func (a *App) Run(ctx context.Context) error {
	defer close(a.done)

	events := make(chan tcell.Event, 256)
	stop := make(chan struct{})
	go a.scr.ChannelEvents(events, stop)
	defer close(stop)

	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()

	if a.follow {
		a.gotoEnd()
	}
	a.draw()

	for !a.quit {
		select {
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			a.handle(ev)
			// Drain the queue before painting so a held-down key does not
			// render one frame per repeat.
			a.drain(events)
			a.lastAct = time.Now()
			a.freed = false

		case f := <-a.post:
			f()

		case <-a.wake:

		case <-tick.C:
			if !a.tickDirty() {
				continue
			}

		case <-ctx.Done():
			return nil
		}
		a.draw()
	}
	return nil
}

func (a *App) drain(events chan tcell.Event) {
	for i := 0; i < 128; i++ {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			a.handle(ev)
		default:
			return
		}
	}
}

// tickDirty decides whether the periodic tick needs to repaint anything, so an
// idle viewer costs nothing.
func (a *App) tickDirty() bool {
	v := a.activeView()
	n := v.Len()
	scanned, _ := v.Progress()

	dirty := n != a.lastLen || scanned != a.lastScan || a.busy != nil || a.havePending
	if a.msg != "" && time.Now().After(a.msgUntil) {
		// Drop it here rather than leaving it expired-but-set, which would
		// report dirty on every tick and keep the UI repainting forever.
		a.msg = ""
		dirty = true
	}
	if !dirty && !a.freed && time.Since(a.lastAct) > idleAfter {
		a.freed = true
		debug.FreeOSMemory()
	}
	return dirty
}

// activeView is the view the user is looking at: the filtered one unless there
// is no filter or the bypass toggle is held.
func (a *App) activeView() view.View {
	if a.filt != nil && !a.bypass {
		return a.filt
	}
	return a.base
}

func (a *App) filtered() bool { return a.filt != nil && !a.bypass }

// ------------------------------------------------------------ event handling

func (a *App) handle(ev tcell.Event) {
	switch ev := ev.(type) {
	case *tcell.EventResize:
		a.scr.Sync()

	case *tcell.EventKey:
		a.key(ev)

	case *tcell.EventMouse:
		a.mouse(ev)

	case *tcell.EventError:
		a.errorf("%v", ev)
	}
}

func (a *App) key(ev *tcell.EventKey) {
	if a.prompt != nil {
		a.promptKey(ev)
		return
	}
	if a.ov != nil && a.overlayKey(ev) {
		return
	}
	if cmd, ok := a.cfg.Command(config.FromEvent(ev)); ok {
		a.exec(cmd)
	}
}

func (a *App) mouse(ev *tcell.EventMouse) {
	if !a.cfg.Bool("mouse") {
		return
	}
	step := int64(a.cfg.Int("scroll-step") * 3)
	if step < 1 {
		step = 3
	}
	switch {
	case ev.Buttons()&tcell.WheelUp != 0:
		if a.ov != nil {
			a.ov.scroll(-int(step))
			return
		}
		a.follow = false
		a.moveBy(-step)
	case ev.Buttons()&tcell.WheelDown != 0:
		if a.ov != nil {
			a.ov.scroll(int(step))
			return
		}
		a.moveBy(step)
	}
}

// --------------------------------------------------------------- positioning

func (a *App) viewLen() int64 { return a.activeView().Len() }

func (a *App) moveBy(n int64) {
	a.setTop(a.top + n)
}

func (a *App) setTop(t int64) {
	max := a.viewLen() - 1
	if max < 0 {
		max = 0
	}
	if t > max {
		t = max
	}
	if t < 0 {
		t = 0
	}
	a.top = t
	if a.follow && t < a.lastTop() {
		a.follow = false
	}
}

// lastTop is the top line that shows the final screenful.
func (a *App) lastTop() int64 {
	t := a.viewLen() - int64(a.contentRows())
	if t < 0 {
		return 0
	}
	return t
}

func (a *App) gotoEnd() {
	a.top = a.lastTop()
}

func (a *App) contentRows() int {
	if a.rows > 0 {
		return a.rows
	}
	_, h := a.scr.Size()
	if h > 1 {
		return h - 1
	}
	return 1
}

// reveal scrolls so that view line i is on screen, biased towards the upper
// third so there is context below a search hit.
func (a *App) reveal(i int64) {
	rows := int64(a.contentRows())
	if i >= a.top && i < a.top+rows {
		return
	}
	a.follow = false
	a.setTop(i - rows/3)
}

// anchorSource is the source line number currently at the top of the screen,
// used to hold position across a rule change.
func (a *App) anchorSource() int64 {
	ln, err := a.activeView().At(a.top, a.linebuf[:0])
	if err != nil {
		return 0
	}
	a.linebuf = ln.Text
	return ln.Num - 1
}

// ------------------------------------------------------------------ messages

func (a *App) infof(format string, args ...any) {
	a.msg = fmt.Sprintf(format, args...)
	a.msgStyle = a.cfg.Style("status")
	a.msgUntil = time.Now().Add(a.cfg.Dur("message-timeout"))
}

func (a *App) errorf(format string, args ...any) {
	a.msg = fmt.Sprintf(format, args...)
	a.msgStyle = a.cfg.Style("status.error")
	a.msgUntil = time.Now().Add(a.cfg.Dur("message-timeout"))
}

func (a *App) clearMsg() {
	a.msg = ""
}

func (a *App) liveMsg() bool {
	return a.msg != "" && time.Now().Before(a.msgUntil)
}

// --------------------------------------------------------------- background

func (a *App) cancelBusy() {
	if a.busy != nil {
		a.busy.cancel()
		a.busy = nil
	}
}

// Close releases the filtered view's scan goroutine.
func (a *App) Close() {
	a.cancelBusy()
	if a.filt != nil {
		a.filt.Close()
		a.filt = nil
	}
}

// Source exposes the source for callers that need it.
func (a *App) Source() *source.Source { return a.src }

// styleFor turns a rule's colour into a drawable style, choosing text that
// reads against it.
func (a *App) styleFor(c filter.Color) tcell.Style {
	bg := tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))
	fg := tcell.ColorWhite
	if c.Luminance() > 0.5 {
		fg = tcell.ColorBlack
	}
	return tcell.StyleDefault.Background(bg).Foreground(fg)
}
