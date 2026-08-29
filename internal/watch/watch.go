// Package watch reports when a file might have changed.
//
// Notifications come from the kernel (inotify, kqueue, ReadDirectoryChangesW)
// so a line appended to a log shows up in microseconds rather than whenever a
// poll interval happens to expire, and an idle viewer wakes up never.
//
// A slow poll runs alongside as a correctness backstop. Kernel notifications
// are not universally reliable: they do not work on NFS or CIFS, some FUSE
// filesystems drop them, and inotify watches are a per-user resource that can
// be exhausted. Missing an append is a visible bug in a log viewer, so the
// backstop guarantees liveness even where notifications never arrive, while
// the kernel provides the latency in the common case.
//
// A directory watch runs next to the file watch. When a log is rotated the file
// watch stays bound to the old inode, and the arrival of the replacement is
// only visible as a create in the parent directory.
package watch

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	// DefaultBackstop is how often the poll runs when kernel notifications are
	// working. It only has to catch what the kernel missed.
	DefaultBackstop = 2 * time.Second
	// DefaultFallback is the poll interval when there are no kernel
	// notifications at all, where it is the only source of liveness.
	DefaultFallback = 200 * time.Millisecond
)

// Watcher signals on its channel when the watched path may have changed.
//
// Signals are coalescing hints, not a change log: a receiver is expected to
// stat or re-read to find out what actually happened. Construction never fails;
// if the kernel cannot supply notifications the watcher degrades to polling and
// says so through Native.
type Watcher struct {
	events chan struct{}
	fsw    *fsnotify.Watcher
	done   chan struct{}
	wg     sync.WaitGroup
	native bool
}

// Options configures a Watcher.
type Options struct {
	// Backstop is the poll interval used alongside kernel notifications.
	Backstop time.Duration
	// Fallback is the poll interval used when notifications are unavailable.
	Fallback time.Duration
}

// New watches path. It also watches the parent directory so that a rotated log
// is noticed when its replacement appears.
func New(path string, opt Options) *Watcher {
	if opt.Backstop <= 0 {
		opt.Backstop = DefaultBackstop
	}
	if opt.Fallback <= 0 {
		opt.Fallback = DefaultFallback
	}

	w := &Watcher{
		events: make(chan struct{}, 1),
		done:   make(chan struct{}),
	}

	fsw, err := fsnotify.NewWatcher()
	if err == nil {
		// Watching the directory alone is enough to stay correct across
		// rotation, so a failure to watch the file itself is not fatal.
		dirErr := fsw.Add(filepath.Dir(path))
		fileErr := fsw.Add(path)
		if dirErr != nil && fileErr != nil {
			fsw.Close()
		} else {
			w.fsw = fsw
			w.native = true
		}
	}

	interval := opt.Backstop
	if !w.native {
		interval = opt.Fallback
	}

	w.wg.Add(1)
	go w.run(path, interval)
	return w
}

// NewPolling returns a watcher that only polls, for sources with no path of
// their own such as a spooled pipe.
func NewPolling(interval time.Duration) *Watcher {
	if interval <= 0 {
		interval = DefaultFallback
	}
	w := &Watcher{
		events: make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
	w.wg.Add(1)
	go w.run("", interval)
	return w
}

// Events fires when the path may have changed.
func (w *Watcher) Events() <-chan struct{} { return w.events }

// Native reports whether kernel notifications are in use. When false the
// watcher is polling, and latency is the fallback interval.
func (w *Watcher) Native() bool { return w.native }

// Close stops watching.
func (w *Watcher) Close() {
	select {
	case <-w.done:
		return // already closed
	default:
	}
	close(w.done)
	if w.fsw != nil {
		w.fsw.Close()
	}
	w.wg.Wait()
}

func (w *Watcher) run(path string, interval time.Duration) {
	defer w.wg.Done()

	tick := time.NewTicker(interval)
	defer tick.Stop()

	var (
		fsEvents chan fsnotify.Event
		fsErrors chan error
		readd    bool
	)
	if w.fsw != nil {
		fsEvents = w.fsw.Events
		fsErrors = w.fsw.Errors
	}

	for {
		select {
		case <-w.done:
			return

		case <-tick.C:
			if readd && w.fsw != nil {
				// The file was replaced; bind the watch to the new inode.
				if err := w.fsw.Add(path); err == nil {
					readd = false
				}
			}
			w.signal()

		case ev, ok := <-fsEvents:
			if !ok {
				fsEvents = nil
				continue
			}
			if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
				readd = true
			}
			// Directory events for unrelated files are noise; ignore them so a
			// busy log directory does not wake the viewer constantly.
			if ev.Name == path || filepath.Base(ev.Name) == filepath.Base(path) {
				w.signal()
			}

		case _, ok := <-fsErrors:
			if !ok {
				fsErrors = nil
				continue
			}
			// The kernel lost events. The backstop poll covers us, and a stat
			// is all the receiver needs to recover.
			w.signal()
		}
	}
}

// signal delivers a coalescing hint. A pending signal already covers a new one,
// so a burst of writes costs one wake-up rather than thousands.
func (w *Watcher) signal() {
	select {
	case w.events <- struct{}{}:
	default:
	}
}
