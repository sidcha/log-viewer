package termcolor

import (
	"testing"
	"time"

	"github.com/sidcha/log-viewer/internal/color"
)

func TestParseOSC(t *testing.T) {
	cases := map[string]color.RGB{
		`]11;rgb:0000/0000/0000` + "\x1b\\": {R: 0, G: 0, B: 0},
		`]11;rgb:ffff/ffff/ffff`:            {R: 255, G: 255, B: 255},
		`]11;rgb:1c1c/1c1c/1c1c`:            {R: 0x1c, G: 0x1c, B: 0x1c},
		`]10;rgb:d0/d0/d0`:                  {R: 0xd0, G: 0xd0, B: 0xd0},
		`]11;rgb:002b/3640/4242`:            {R: 0x00, G: 0x36, B: 0x42}, // solarized dark
		`]11;rgb:f/f/f`:                     {R: 255, G: 255, B: 255},
	}
	for reply, want := range cases {
		got, ok := parseOSC(reply)
		if !ok {
			t.Errorf("parseOSC(%q) failed", reply)
			continue
		}
		if got != want {
			t.Errorf("parseOSC(%q) = %v, want %v", reply, got, want)
		}
	}

	for _, bad := range []string{"", "no colour here", "]11;rgb:", "]11;rgb:00/00", "]11;rgb:zz/00/00", "]11;rgb:00000/0/0"} {
		if _, ok := parseOSC(bad); ok {
			t.Errorf("parseOSC(%q) should have failed", bad)
		}
	}
}

// TestScalesComponentWidths matters because terminals answer with one to four
// hex digits per channel and they all mean the same colour.
func TestScalesComponentWidths(t *testing.T) {
	for _, reply := range []string{`]11;rgb:ffff/0000/0000`, `]11;rgb:ff/00/00`, `]11;rgb:f/0/0`} {
		got, ok := parseOSC(reply)
		if !ok || got != (color.RGB{R: 255, G: 0, B: 0}) {
			t.Errorf("parseOSC(%q) = %v, %v; want pure red", reply, got, ok)
		}
	}
}

func TestLuminanceAndDark(t *testing.T) {
	if !(color.RGB{R: 0, G: 0, B: 0}).Dark() {
		t.Error("black should be dark")
	}
	if (color.RGB{R: 255, G: 255, B: 255}).Dark() {
		t.Error("white should not be dark")
	}
	if !(color.RGB{R: 0x1c, G: 0x1c, B: 0x1c}).Dark() {
		t.Error("#1c1c1c should be dark")
	}
	if !(color.RGB{R: 0x00, G: 0x2b, B: 0x36}).Dark() {
		t.Error("solarized dark background should be dark")
	}
	if (color.RGB{R: 0xfd, G: 0xf6, B: 0xe3}).Dark() {
		t.Error("solarized light background should not be dark")
	}
}

func TestBlend(t *testing.T) {
	black, white := color.RGB{R: 0, G: 0, B: 0}, color.RGB{R: 255, G: 255, B: 255}
	if got := color.Blend(black, white, 0); got != black {
		t.Errorf("Blend at 0 = %v", got)
	}
	if got := color.Blend(black, white, 1); got != white {
		t.Errorf("Blend at 1 = %v", got)
	}
	mid := color.Blend(black, white, 0.5)
	if mid.R < 126 || mid.R > 129 {
		t.Errorf("Blend at 0.5 = %v, want about half", mid)
	}

	// A tint has to move away from the background in both directions.
	darkTint := color.Blend(color.RGB{R: 0x1c, G: 0x1c, B: 0x1c}, white, 0.12)
	if darkTint.Luminance() <= (color.RGB{R: 0x1c, G: 0x1c, B: 0x1c}).Luminance() {
		t.Error("a tint on a dark background should be lighter than it")
	}
	lightTint := color.Blend(color.RGB{R: 0xfd, G: 0xf6, B: 0xe3}, color.RGB{R: 0x65, G: 0x7b, B: 0x83}, 0.12)
	if lightTint.Luminance() >= (color.RGB{R: 0xfd, G: 0xf6, B: 0xe3}).Luminance() {
		t.Error("a tint on a light background should be darker than it")
	}
}

func TestFromEnv(t *testing.T) {
	cases := map[string]struct {
		dark, ok bool
	}{
		"15;0":            {true, true},  // white on black
		"0;15":            {false, true}, // black on white
		"default;default": {false, false},
		"7":               {false, false},
		"":                {false, false},
		"15;default;0":    {true, true},
	}
	for v, want := range cases {
		dark, ok := fromEnv(v)
		if ok != want.ok || (ok && dark != want.dark) {
			t.Errorf("fromEnv(%q) = (%v, %v), want (%v, %v)", v, dark, ok, want.dark, want.ok)
		}
	}
}

// TestDetectHonoursTheSetting: an explicit background must not be second
// guessed by a terminal query, which is the escape hatch when detection is
// wrong or too slow.
func TestDetectHonoursTheSetting(t *testing.T) {
	if c := Detect(nil, "dark", time.Millisecond); !c.Dark || c.Known {
		t.Errorf("Detect(dark) = %+v", c)
	}
	if c := Detect(nil, "light", time.Millisecond); c.Dark || c.Known {
		t.Errorf("Detect(light) = %+v", c)
	}
}

func TestDetectFallsBackToDark(t *testing.T) {
	t.Setenv("COLORFGBG", "")
	c := Detect(nil, "auto", time.Millisecond)
	if !c.Dark {
		t.Error("with nothing to go on, Detect should assume dark")
	}
	if c.Known {
		t.Error("a guess should not claim to know the terminal's colours")
	}
}

func TestDetectUsesEnv(t *testing.T) {
	t.Setenv("COLORFGBG", "0;15")
	if c := Detect(nil, "auto", time.Millisecond); c.Dark {
		t.Error("COLORFGBG said light; Detect said dark")
	}
	t.Setenv("COLORFGBG", "15;0")
	if c := Detect(nil, "auto", time.Millisecond); !c.Dark {
		t.Error("COLORFGBG said dark; Detect said light")
	}
}
