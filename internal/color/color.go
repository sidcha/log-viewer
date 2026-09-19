// Package color is the one colour type lv computes with: rule colours,
// the terminal's own colours, and the tints derived from them.
package color

import (
	"fmt"
	"math"
	"strconv"

	"github.com/gdamore/tcell/v2"
)

// RGB is a 24-bit colour. The zero value doubles as "no colour chosen".
type RGB struct{ R, G, B uint8 }

// Hex renders the colour as #rrggbb.
func (c RGB) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// ParseHex reads #rrggbb.
func ParseHex(s string) (RGB, error) {
	if len(s) != 7 || s[0] != '#' {
		return RGB{}, fmt.Errorf("colour %q: expected #rrggbb", s)
	}
	n, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return RGB{}, fmt.Errorf("colour %q: %w", s, err)
	}
	return RGB{uint8(n >> 16), uint8(n >> 8), uint8(n)}, nil
}

// Luminance is the perceived brightness of the colour, 0 to 1.
func (c RGB) Luminance() float64 {
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
}

// Dark reports whether text on this colour should be light.
func (c RGB) Dark() bool { return c.Luminance() < 0.5 }

// Blend mixes a towards b by t, 0 to 1.
func Blend(a, b RGB, t float64) RGB {
	mix := func(x, y uint8) uint8 {
		v := float64(x) + (float64(y)-float64(x))*t
		return uint8(math.Round(math.Min(255, math.Max(0, v))))
	}
	return RGB{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B)}
}

// Tcell converts the colour for drawing.
func (c RGB) Tcell() tcell.Color { return tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B)) }

// FromTcell converts a drawing colour back, resolving a palette colour to
// the RGB value tcell assumes for it.
func FromTcell(c tcell.Color) RGB {
	r, g, b := c.RGB()
	return RGB{uint8(r), uint8(g), uint8(b)}
}

// Hue is the colour's position on the hue circle, 0 to 1.
func (c RGB) Hue() float64 {
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

// FromHSL builds a colour from hue, saturation and lightness, each 0 to 1.
func FromHSL(h, s, l float64) RGB {
	if s == 0 {
		v := uint8(math.Round(l * 255))
		return RGB{v, v, v}
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	return RGB{
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
