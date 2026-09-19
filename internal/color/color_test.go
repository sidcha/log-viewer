package color

import (
	"math"
	"testing"
)

func TestHexRoundTrip(t *testing.T) {
	for _, s := range []string{"#000000", "#ffffff", "#8decc8", "#0a3069"} {
		c, err := ParseHex(s)
		if err != nil {
			t.Fatal(err)
		}
		if c.Hex() != s {
			t.Errorf("%s -> %v -> %s", s, c, c.Hex())
		}
	}
	for _, bad := range []string{"", "8decc8", "#8decc", "#8decczz", "#8decc88"} {
		if _, err := ParseHex(bad); err == nil {
			t.Errorf("ParseHex(%q) accepted", bad)
		}
	}
}

func TestHSLRoundTripsTheHue(t *testing.T) {
	for _, h := range []float64{0, 0.1, 0.33, 0.5, 0.75, 0.95} {
		got := FromHSL(h, 0.72, 0.74).Hue()
		if math.Abs(got-h) > 0.01 {
			t.Errorf("hue %v came back as %v", h, got)
		}
	}
}

func TestTcellRoundTrip(t *testing.T) {
	c := RGB{12, 200, 99}
	if got := FromTcell(c.Tcell()); got != c {
		t.Errorf("%v -> %v", c, got)
	}
}

func TestBlend(t *testing.T) {
	a, b := RGB{0, 0, 0}, RGB{200, 100, 50}
	if got := Blend(a, b, 0.5); got != (RGB{100, 50, 25}) {
		t.Errorf("Blend = %v", got)
	}
	if !a.Dark() || b.Luminance() <= a.Luminance() {
		t.Error("luminance ordering is wrong")
	}
}
