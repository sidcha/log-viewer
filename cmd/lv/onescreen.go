package main

import (
	"io"
	"os"
	"time"

	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/lines"
	"github.com/sidcha/log-viewer/internal/source"
	"github.com/sidcha/log-viewer/internal/tty"
	"github.com/sidcha/log-viewer/internal/ui"
)

// Short output does not deserve a full-screen viewer.
//
// `git log --oneline -3` should print three lines and give the prompt back,
// not take the terminal over and wait to be dismissed. That is what less does
// under -F, and why git asks for LESS=FRX; without it lv is unusable as a
// pager for the dozens of commands whose output is usually tiny.

// settleWait bounds how long lv will wait to find out whether the input is
// short, before giving up and opening the viewer.
//
// Something finite has usually finished by now, and anything still arriving is
// a stream, which is exactly the case that must not be waited on: a viewer
// that sat silently waiting for `dmesg -w` to produce a screenful would look
// like a hang.
const settleWait = 250 * time.Millisecond

// dumpIfItFits writes the whole input to stdout and reports that there is
// nothing left to do.
//
// It declines whenever it cannot be sure: no terminal, input still arriving,
// too many bytes to possibly fit. Declining costs nothing -- the viewer opens
// as usual over input that has not been consumed.
func dumpIfItFits(cfg *config.Config, src *source.Source) (bool, error) {
	term, err := tty.Open()
	if err != nil {
		return false, nil
	}
	defer term.Close()

	rows, cols, ok := tty.Size(term)
	if !ok || rows < 2 || cols < 2 {
		return false, nil
	}

	// One cell cannot hold less than one byte, so anything larger than the
	// screen has too many to fit and there is no point waiting for the rest.
	budget := int64(rows) * int64(cols)
	if !settled(src, budget) {
		return false, nil
	}

	size := src.Size()
	if size == 0 {
		// Nothing arrived at all: say nothing and exit, as a pager should.
		return true, nil
	}
	if size > budget {
		return false, nil
	}
	if !fitsOnScreen(cfg, src, rows, cols) {
		return false, nil
	}

	if _, err := io.Copy(os.Stdout, io.NewSectionReader(src, 0, size)); err != nil {
		return false, err
	}
	// A last line with no newline of its own would otherwise leave the shell
	// prompt halfway across the terminal.
	if err := endWithNewline(src, size); err != nil {
		return false, err
	}
	return true, nil
}

// settled waits for the input to finish, or to grow past any hope of fitting.
func settled(src *source.Source, budget int64) bool {
	deadline := time.Now().Add(settleWait)
	for src.Streaming() {
		if src.Size() > budget {
			return false
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
	return src.Err() == nil
}

// fitsOnScreen counts the rows the input would occupy once drawn, which is not
// its line count: a long line wraps, and escape sequences take no columns at
// all.
func fitsOnScreen(cfg *config.Config, src *source.Source, rows, cols int) bool {
	tab := cfg.Options().TabWidth
	used := 0

	sc := lines.NewScanner(src, 0, src.Size(), cfg.Options().BlockSize)
	for sc.Scan() {
		w := ui.DisplayWidth(sc.Bytes(), tab)
		n := 1
		if w > cols {
			n = (w + cols - 1) / cols
		}
		used += n
		if used > rows {
			return false
		}
	}
	return sc.Err() == nil
}

// endWithNewline adds the line ending the input was missing, if it was.
func endWithNewline(src *source.Source, size int64) error {
	var last [1]byte
	if _, err := src.ReadAt(last[:], size-1); err != nil {
		return err
	}
	if last[0] == '\n' {
		return nil
	}
	_, err := os.Stdout.Write([]byte{'\n'})
	return err
}
