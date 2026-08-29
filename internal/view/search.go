package view

import (
	"context"

	"github.com/sidcha/log-viewer/internal/filter"
)

// searchWindow is how many lines a backward search walks forward at a time.
// Backward iteration is not natural over a forward-only line index, so we step
// back a window and scan it forward, keeping the last match.
const searchWindow = 1 << 15

// checkEvery bounds how often a search tests for cancellation.
const checkEvery = 4096

// Search looks for the first line at or after `from` (or at or before, when
// backward) whose text matches m. It reports the view line index and whether
// anything was found.
//
// It runs on the caller's goroutine and is cancellable: the UI runs it in the
// background and shows progress, so a fruitless search over 100 GB can be
// abandoned with a keystroke.
func Search(ctx context.Context, v View, from int64, m filter.Matcher, backward bool, progress func(done, total int64)) (int64, bool, error) {
	if backward {
		return searchBackward(ctx, v, from, m, progress)
	}
	return searchForward(ctx, v, from, m, progress)
}

func searchForward(ctx context.Context, v View, from int64, m filter.Matcher, progress func(done, total int64)) (int64, bool, error) {
	total := v.Len()
	if from < 0 {
		from = 0
	}

	var (
		hit     int64 = -1
		counter int
		cancel  bool
	)
	err := v.Iterate(from, func(i int64, ln Line) bool {
		if m.Match(ln.Text) {
			hit = i
			return false
		}
		counter++
		if counter >= checkEvery {
			counter = 0
			if progress != nil {
				progress(i-from, total-from)
			}
			if ctx.Err() != nil {
				cancel = true
				return false
			}
		}
		return true
	})
	if err != nil {
		return 0, false, err
	}
	if cancel {
		return 0, false, ctx.Err()
	}
	return hit, hit >= 0, nil
}

func searchBackward(ctx context.Context, v View, from int64, m filter.Matcher, progress func(done, total int64)) (int64, bool, error) {
	if from < 0 {
		return 0, false, nil
	}
	total := from

	for end := from; end >= 0; {
		start := end - searchWindow + 1
		if start < 0 {
			start = 0
		}

		var hit int64 = -1
		err := v.Iterate(start, func(i int64, ln Line) bool {
			if i > end {
				return false
			}
			if m.Match(ln.Text) {
				hit = i
			}
			return true
		})
		if err != nil {
			return 0, false, err
		}
		if hit >= 0 {
			return hit, true, nil
		}
		if progress != nil && total > 0 {
			progress(from-start, total)
		}
		if ctx.Err() != nil {
			return 0, false, ctx.Err()
		}
		if start == 0 {
			break
		}
		end = start - 1
	}
	return 0, false, nil
}
