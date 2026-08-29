package filter

import (
	"math"
	"math/rand"
)

// Color is an RGB colour assigned to a rule.
//
// It is deliberately not a tcell type: matching has no business depending on a
// terminal library, and the UI converts at the point of drawing.
type Color struct{ R, G, B uint8 }

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
func NextColor(used []Color) Color {
	hues := make([]float64, 0, len(used))
	for _, c := range used {
		if c != (Color{}) {
			hues = append(hues, hueOf(c))
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
	return hslToRGB(best, colorSat, colorLight)
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

// Luminance is the perceived brightness of the colour, 0 to 1. The UI uses it
// to choose readable text over the colour.
func (c Color) Luminance() float64 {
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
}

func hueOf(c Color) float64 {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	d := max - min
	if d == 0 {
		return 0
	}

	var h float64
	switch max {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h /= 6
	if h < 0 {
		h++
	}
	return h
}

func hslToRGB(h, s, l float64) Color {
	if s == 0 {
		v := uint8(math.Round(l * 255))
		return Color{v, v, v}
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	return Color{
		R: uint8(math.Round(hueToChannel(p, q, h+1.0/3.0) * 255)),
		G: uint8(math.Round(hueToChannel(p, q, h) * 255)),
		B: uint8(math.Round(hueToChannel(p, q, h-1.0/3.0) * 255)),
	}
}

func hueToChannel(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6.0:
		return p + (q-p)*6*t
	case t < 1.0/2.0:
		return q
	case t < 2.0/3.0:
		return p + (q-p)*(2.0/3.0-t)*6
	default:
		return p
	}
}
