package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// parseStyle builds a tcell.Style from words such as:
//
//	fg=red bg=#202020 bold
//	fg=color214 underline
//
// Colours are W3C names, #rrggbb, colorN palette indices, or `default`. A bare
// #rrggbb is not accepted as its own word because a word starting with # is a
// comment; that is why colours always carry an fg=/bg= prefix.
func parseStyle(words []string) (tcell.Style, error) {
	s := tcell.StyleDefault
	for _, w := range words {
		k, v, hasVal := strings.Cut(w, "=")
		switch strings.ToLower(k) {
		case "fg", "foreground":
			if !hasVal {
				return s, fmt.Errorf("fg needs a colour")
			}
			c, err := parseColor(v)
			if err != nil {
				return s, err
			}
			s = s.Foreground(c)
		case "bg", "background":
			if !hasVal {
				return s, fmt.Errorf("bg needs a colour")
			}
			c, err := parseColor(v)
			if err != nil {
				return s, err
			}
			s = s.Background(c)
		case "bold":
			s = s.Bold(true)
		case "dim":
			s = s.Dim(true)
		case "italic":
			s = s.Italic(true)
		case "underline":
			s = s.Underline(true)
		case "reverse":
			s = s.Reverse(true)
		case "blink":
			s = s.Blink(true)
		case "strike", "strikethrough":
			s = s.StrikeThrough(true)
		case "none", "default":
			s = tcell.StyleDefault
		default:
			return s, fmt.Errorf("unknown style attribute %q", w)
		}
	}
	return s, nil
}

func parseColor(v string) (tcell.Color, error) {
	lv := strings.ToLower(v)
	switch lv {
	case "default", "none", "":
		return tcell.ColorDefault, nil
	}
	if strings.HasPrefix(lv, "#") {
		if len(lv) != 7 {
			return 0, fmt.Errorf("colour %q: expected #rrggbb", v)
		}
		n, err := strconv.ParseInt(lv[1:], 16, 32)
		if err != nil {
			return 0, fmt.Errorf("colour %q: %v", v, err)
		}
		return tcell.NewHexColor(int32(n)), nil
	}
	if rest, ok := strings.CutPrefix(lv, "color"); ok {
		n, err := strconv.Atoi(rest)
		if err == nil && n >= 0 && n <= 255 {
			return tcell.PaletteColor(n), nil
		}
		return 0, fmt.Errorf("colour %q: palette index must be 0-255", v)
	}
	if c, ok := tcell.ColorNames[lv]; ok {
		return c, nil
	}
	return 0, fmt.Errorf("unknown colour %q", v)
}

// styleWords is the inverse of parseStyle, for --dump-config.
func styleWords(s tcell.Style) string {
	fg, bg, attr := s.Decompose()

	var parts []string
	if fg != tcell.ColorDefault {
		parts = append(parts, "fg="+colorWord(fg))
	}
	if bg != tcell.ColorDefault {
		parts = append(parts, "bg="+colorWord(bg))
	}
	for _, a := range []struct {
		mask tcell.AttrMask
		name string
	}{
		{tcell.AttrBold, "bold"},
		{tcell.AttrDim, "dim"},
		{tcell.AttrItalic, "italic"},
		{tcell.AttrUnderline, "underline"},
		{tcell.AttrReverse, "reverse"},
		{tcell.AttrBlink, "blink"},
		{tcell.AttrStrikeThrough, "strike"},
	} {
		if attr&a.mask != 0 {
			parts = append(parts, a.name)
		}
	}
	if len(parts) == 0 {
		return "default"
	}
	return strings.Join(parts, " ")
}

func colorWord(c tcell.Color) string {
	if c&tcell.ColorIsRGB != 0 {
		r, g, b := c.RGB()
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
	for name, v := range tcell.ColorNames {
		if v == c {
			return name
		}
	}
	return fmt.Sprintf("color%d", int(c&^tcell.ColorValid))
}
