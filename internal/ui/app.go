// Package ui is the terminal front end: event loop, rendering, prompts.
//
// lv has one front end, and tcell is it. The packages this one draws with --
// highlight, ansi, config, color -- use tcell's Style and Color types
// directly rather than a neutral styling type of their own. That keeps a style
// read from a config file the same value that is drawn, with no conversion
// between. The matching core, filter and view, depends on none of it, and a
// second front end would start by giving those packages a style type of their
// own.
package ui

import (
	"context"
	"fmt"
	"runtime/debug"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/ansi"
	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/view"
)

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

	// buffer is the file on screen; its fields read as the App's own.
	*buffer
	bufs []*buffer
	// shown mirrors buffer for the indexers' goroutines.
	shown atomic.Pointer[buffer]

	lay *layout

	// opt is the settings, resolved; read it through opts, which refreshes it
	// whenever the configuration has changed since.
	opt     config.Options
	optVer  uint64
	mouseOn bool

	search  searchState
	prompt  *prompt
	find    *finder
	ov      *overlay
	confirm *confirm
	busy    *busyOp

	msg      string
	msgStyle tcell.Style
	msgUntil time.Time

	post chan func()
	wake chan struct{}
	done chan struct{}
	quit bool

	// frame scratch, reused to keep drawing allocation-free
	linebuf []byte
	// screenBytes is what the lines drawn this frame are made of, for the
	// encoding status field.
	screenBytes byteCensus
	// walkbuf belongs to the word motions, which read lines other than the
	// one being drawn.
	walkbuf   []byte
	styles    []tcell.Style
	locs      [][2]int
	ansiSpans []ansi.Span
	rows      int
	cols      int
	// where the cursor column landed on screen this frame, or -1
	curX int
	curY int

	lastAct time.Time
	freed   bool

	// half-typed key sequence and numeric count
	pending []config.Key
	count   int64

	histPattern []string
	histPath    []string
	histCmd     []string
}

// maxCount bounds a typed repeat count so a leaning-on-the-keyboard accident
// cannot ask for an astronomical motion.
const maxCount = 1 << 40

type searchState struct {
	m    filter.Matcher
	pat  string
	back bool
}

// New builds an App with no files; AddBuffer opens them. The caller owns the
// screen's lifetime.
func New(scr tcell.Screen, cfg *config.Config) *App {
	a := &App{
		scr:     scr,
		cfg:     cfg,
		lay:     newLayout(cfg.Options().TabWidth),
		post:    make(chan func(), 32),
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
		lastAct: time.Now(),
	}
	a.reconfigure()
	return a
}

// opts returns the settings, re-resolving them if anything changed since they
// were last read. Every setting the viewer uses is read through here, so there
// is one source of truth and a :set takes effect everywhere at once.
func (a *App) opts() *config.Options {
	if a.cfg.Version() != a.optVer {
		a.reconfigure()
	}
	return &a.opt
}

// reconfigure resolves the settings and hands them to the parts that keep
// their own copy.
func (a *App) reconfigure() {
	a.opt = a.cfg.Options()
	a.optVer = a.cfg.Version()
	o := &a.opt

	a.lay.setTab(o.TabWidth)
	if o.Mouse != a.mouseOn {
		if o.Mouse {
			a.scr.EnableMouse(tcell.MouseButtonEvents)
		} else {
			a.scr.DisableMouse()
		}
		a.mouseOn = o.Mouse
	}
	for _, b := range a.bufs {
		if (b.eng.MaxScan != o.MaxScanBytes || b.eng.MaxMatches != o.MaxHighlights) && o.MaxScanBytes > 0 && o.MaxHighlights > 0 {
			b.eng.MaxScan, b.eng.MaxMatches = o.MaxScanBytes, o.MaxHighlights
			b.eng.Forget()
		}
		if o.Wrap {
			b.hoff = 0
		}
	}
}

// requestFrame asks the event loop to repaint. It never blocks.
func (a *App) requestFrame() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// Wake asks for a repaint on behalf of work done for the buffer on screen.
// It is safe to call from any goroutine.
func (a *App) Wake() {
	if b := a.shown.Load(); b != nil {
		b.wake()
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

	pace := newPacer(frameInterval)
	defer pace.stop()
	paint := func() {
		a.draw()
		pace.painted(time.Now())
	}
	// Background work asks for a frame; the pacer decides when it gets one.
	request := func() {
		if pace.request(time.Now()) {
			paint()
		}
	}

	if a.follow {
		a.gotoEnd()
	}
	paint()

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
			// A key press is answered at once: the pacer only holds back
			// frames nobody asked for.
			paint()

		case f := <-a.post:
			f()
			request()

		case <-a.wake:
			request()

		case <-pace.C():
			pace.fired()
			paint()

		case <-tick.C:
			if a.tickDirty() {
				request()
			}

		case <-ctx.Done():
			return nil
		}
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
	// Go's allocator holds freed spans as heap headroom, which is exactly the
	// behaviour a viewer for very large files should not have; once the UI
	// has been quiet a while, hand them back.
	if !dirty && !a.freed && time.Since(a.lastAct) > a.opts().IdleRelease {
		a.freed = true
		debug.FreeOSMemory()
	}
	return dirty
}

// activeView is the view the user is looking at: the filtered one unless there
// is no filter or the bypass toggle is held.
func (a *App) activeView() view.View {
	if a.filtered() {
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
	a.dispatch(a.keyTable(), config.FromEvent(ev))
}

// dispatch feeds one key press into the pending sequence and count, and runs
// whatever the active key table binds it to.
//
// Counts come first and are collected until a non-digit arrives, so 5j moves
// five lines and 42G jumps to line 42. A leading 0 is the motion, not the start
// of a count, which is why the digit test depends on whether a count is already
// building. Only the tables for the log itself take counts: in the match
// window a digit is part of the pattern.
func (a *App) dispatch(table string, k config.Key) {
	counts := table == "normal" || table == "visual"
	if counts && len(a.pending) == 0 && k.Mod == 0 && k.Key == tcell.KeyRune {
		if d := k.Ch - '0'; d >= 0 && d <= 9 && !(d == 0 && a.count == 0) {
			a.count = min(a.count*10+int64(d), maxCount)
			return
		}
	}

	a.pending = append(a.pending, k)
	cmd, m := a.cfg.LookupIn(table, a.pending)
	switch m {
	case config.Partial:
		return
	case config.Full:
		count := a.count
		a.pending, a.count = a.pending[:0], 0
		a.exec(cmd, count)
	default:
		typed := len(a.pending) == 1 && k.Key == tcell.KeyRune
		a.pending, a.count = a.pending[:0], 0
		if typed {
			a.selfInsert(k.Ch)
		}
	}
}

// pendingKeys renders the half-typed sequence for the status line, the way vim
// shows it in the bottom right.
func (a *App) pendingKeys() string {
	if a.count == 0 && len(a.pending) == 0 {
		return ""
	}
	out := ""
	if a.count > 0 {
		out = strconv.FormatInt(a.count, 10)
	}
	return out + config.SequenceString(a.pending)
}

// mouse turns a wheel turn into a key, so what it does is a binding in
// whichever table has the keyboard.
func (a *App) mouse(ev *tcell.EventMouse) {
	if !a.opts().Mouse {
		return
	}
	switch {
	case ev.Buttons()&tcell.WheelUp != 0:
		a.dispatch(a.keyTable(), config.Key{Key: config.KeyWheelUp})
	case ev.Buttons()&tcell.WheelDown != 0:
		a.dispatch(a.keyTable(), config.Key{Key: config.KeyWheelDown})
	}
}

// --------------------------------------------------------------- positioning

func (a *App) viewLen() int64 { return a.activeView().Len() }

// lastTop is the top line that shows the final screenful.
func (a *App) lastTop() int64 {
	t := a.viewLen() - int64(a.contentRows())
	if t < 0 {
		return 0
	}
	return t
}

// pinToTail puts the cursor on the last line with the window under it.
//
// Follow mode has to carry the cursor, not just the window. Leaving it behind
// means yanking copies a line that scrolled off long ago, and the first motion
// snaps the view back to wherever the cursor was stranded.
func (a *App) pinToTail() {
	n := a.viewLen()
	if n == 0 {
		a.cur, a.top = 0, 0
		return
	}
	a.cur = n - 1
	a.top = a.lastTop()
	a.restoreColumn()
}

func (a *App) gotoEnd() {
	a.pinToTail()
}

// contentWidth is the usable width for log text. It falls back to asking the
// screen, because a motion can run before the first frame has been drawn.
func (a *App) contentWidth() int {
	cols := a.cols
	if cols <= 0 {
		cols, _ = a.scr.Size()
	}
	return cols - a.gutterWidth()
}

func (a *App) contentRows() int {
	if a.rows > 0 {
		return a.rows
	}
	// Before the first frame, work it out the same way draw does. Guessing one
	// row too few here would make H and L land in the wrong place.
	_, h := a.scr.Size()
	if a.showStatus() {
		h--
	}
	if h < 1 {
		h = 1
	}
	return h
}

// reveal puts the cursor on view line i, which is where a search hit belongs:
// moving only the window would leave the cursor elsewhere, and the next motion
// would jump away from the match.
func (a *App) reveal(i int64) {
	rows := int64(a.contentRows())
	onScreen := i >= a.top && i < a.top+rows

	a.follow = false
	a.setCursor(i)
	if !onScreen {
		// Bias towards the upper third so there is context below the hit.
		a.top = i - rows/3
		a.clampTop()
	}
}

// anchorSource is the source line number the cursor is on, used to hold the
// reading position across a rule change.
func (a *App) anchorSource() int64 {
	ln, err := a.activeView().At(a.cursorLine(), a.linebuf[:0])
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
	a.msgUntil = time.Now().Add(a.opts().MessageTimeout)
}

func (a *App) errorf(format string, args ...any) {
	a.msg = fmt.Sprintf(format, args...)
	a.msgStyle = a.cfg.Style("status.error")
	a.msgUntil = time.Now().Add(a.opts().MessageTimeout)
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

// Close releases every buffer's filter scan.
func (a *App) Close() {
	a.cancelBusy()
	for _, b := range a.bufs {
		b.close()
	}
}

// styleFor turns a rule's colour into a drawable style, choosing text that
// reads against it.
func (a *App) styleFor(c color.RGB) tcell.Style {
	fg := tcell.ColorWhite
	if !c.Dark() {
		fg = tcell.ColorBlack
	}
	return tcell.StyleDefault.Background(c.Tcell()).Foreground(fg)
}
