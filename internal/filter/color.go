package filter

import (
	"math"
	"math/rand"

	"github.com/sidcha/log-viewer/internal/color"
)

// Colours are generated as backgrounds with dark text, so saturation and
// lightness are fixed and only the hue varies. That keeps every rule equally
// legible and leaves the whole hue circle to tell them apart.
const (
	colorSat   = 0.72
	colorLight = 0.74
	// minSeparation is the hue gap, as a fraction of the circle, below which
	// two colours are considered too similar to sit on screen together.
	minSeparation = 0.11
	// candidates bounds how hard NextColor tries before taking its best guess.
	candidates = 48
)

// NextColor picks a colour for a new rule: random, but biased away from the
// colours already on screen.
//
// Purely random hues collide often enough to be a nuisance — with six rules the
// odds of some pair being nearly indistinguishable are better than even. Drawing
// several candidates and keeping the one furthest from every colour in use gets
// the variety of random assignment while keeping highlights telling apart.
func NextColor(used []color.RGB) color.RGB {
	hues := make([]float64, 0, len(used))
	for _, c := range used {
		if c != (color.RGB{}) {
			hues = append(hues, c.Hue())
		}
	}

	best, bestGap := 0.0, -1.0
	for i := 0; i < candidates; i++ {
		h := rand.Float64()
		gap := minHueGap(h, hues)
		if gap > bestGap {
			best, bestGap = h, gap
		}
		if bestGap >= minSeparation {
			break
		}
	}
	return color.FromHSL(best, colorSat, colorLight)
}

// minHueGap is the distance around the hue circle from h to the nearest of
// hues, in [0, 0.5]. An empty set is infinitely far away.
func minHueGap(h float64, hues []float64) float64 {
	if len(hues) == 0 {
		return 1
	}
	min := 1.0
	for _, o := range hues {
		d := math.Abs(h - o)
		if d > 0.5 {
			d = 1 - d
		}
		if d < min {
			min = d
		}
	}
	return min
}
