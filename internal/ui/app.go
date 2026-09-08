// Package ui is the terminal front end: event loop, rendering, prompts.
package ui

import (
	"context"
	"fmt"
	"runtime/debug"
	"strconv"
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

	base *view.Base
	filt *view.Filtered
	// filtPtr mirrors filt for goroutines other than the event loop.
	filtPtr atomic.Pointer[view.Filtered]
	rules   *filter.Set
	gen     int // bumped whenever the active view is replaced

	eng *highlight.Engine
	lay *layout

	top     int64
	cur     int64 // cursor line, in view coordinates
	curCol  int   // cursor byte offset within that line
	wantCol int   // column to return to after moving between lines
	sel     selection
	hoff    int
	// previewBase lifts the only rules while a further one is being typed, so
	// the lines it would bring back can be seen.
	previewBase bool
	follow      bool
	wrap        bool
	lineNums    bool
	bypass      bool
	statusOn    string

	search  searchState
	prompt  *prompt
	prev    *preview
	ov      *overlay
	confirm *confirm
	busy    *busyOp

	// Where the rules came from, and what they looked like when last written,
	// so quitting knows whether there is anything to lose.
	rulesPath   string
	savedDigest string

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
	// walkbuf belongs to the word motions, which read lines other than the
	// one being drawn.
	walkbuf []byte
	styles  []tcell.Style
	locs    [][2]int
	rows    int
	cols    int
	// where the cursor column landed on screen this frame, or -1
	curX int
	curY int

	lastLen  int64
	lastScan int64
	lastAct  time.Time
	freed    bool

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

// Wake asks the event loop to repaint, and tells any filter scan that more of
// the file has arrived. It is safe to call from any goroutine and never blocks.
//
// The filtered view is reached through an atomic pointer because this runs on
// the indexer's goroutine while the UI goroutine owns the field itself.
func (a *App) Wake() {
	if f := a.filtPtr.Load(); f != nil {
		f.Nudge()
	}
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
	if a.filtered() {
		return a.filt
	}
	return a.base
}

func (a *App) filtered() bool { return a.filt != nil && !a.bypass && !a.previewBase }

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
	if a.confirm != nil {
		a.confirmKey(ev)
		return
	}
	if a.prompt != nil {
		a.promptKey(ev)
		return
	}
	if a.ov != nil && a.overlayKey(ev) {
		return
	}
	a.dispatch(config.FromEvent(ev))
}

// dispatch feeds one key press into the pending sequence and count.
//
// Counts come first and are collected until a non-digit arrives, so 5j moves
// five lines and 42G jumps to line 42. A leading 0 is the motion, not the start
// of a count, which is why the digit test depends on whether a count is already
// building.
func (a *App) dispatch(k config.Key) {
	if len(a.pending) == 0 && k.Mod == 0 && k.Key == tcell.KeyRune {
		if d := k.Ch - '0'; d >= 0 && d <= 9 && !(d == 0 && a.count == 0) {
			a.count = a.count*10 + int64(d)
			if a.count > maxCount {
				a.count = maxCount
			}
			return
		}
	}

	a.pending = append(a.pending, k)
	cmd, m := a.lookup(a.pending)
	switch m {
	case config.Partial:
		return
	case config.Full:
		count := a.count
		a.pending, a.count = a.pending[:0], 0
		a.exec(cmd, count)
	default:
		a.pending, a.count = a.pending[:0], 0
	}
}

// lookup resolves a sequence against the visual-mode map first when a
// selection is active, so y can copy the selection while still being the
// prefix of yy everywhere else.
func (a *App) lookup(seq []config.Key) (config.Command, config.Match) {
	if a.visualActive() {
		if cmd, m := a.cfg.LookupVisual(seq); m != config.NoMatch {
			return cmd, m
		}
	}
	return a.cfg.Lookup(seq)
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
		a.scrollView(-step)
	case ev.Buttons()&tcell.WheelDown != 0:
		if a.ov != nil {
			a.ov.scroll(int(step))
			return
		}
		a.scrollView(step)
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
		a.filtPtr.Store(nil)
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
