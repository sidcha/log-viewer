package config

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
)

// Key is a normalised key press, usable as a map key.
//
// Normalisation matters because tcell reports Ctrl-A as KeyCtrlA with ModCtrl
// already set and rune 0x01. Folding that to a single canonical form means
// `bind C-a` and the event compare equal without special cases at dispatch.
type Key struct {
	Key tcell.Key
	Ch  rune
	Mod tcell.ModMask
}

// FromEvent normalises a tcell key event into a Key.
func FromEvent(ev *tcell.EventKey) Key {
	k, ch, mod := ev.Key(), ev.Rune(), ev.Modifiers()
	if k == tcell.KeyRune {
		// Shift is already expressed in the rune itself.
		return Key{Key: k, Ch: ch, Mod: mod &^ tcell.ModShift}
	}
	if k >= tcell.KeyCtrlA && k <= tcell.KeyCtrlZ {
		// Control is already expressed in the key code across this range.
		mod &^= tcell.ModCtrl
	}
	// Shift and Control on a named key -- an arrow, a page key -- are not
	// encoded anywhere else, so they are kept: S-PgUp is not PgUp.
	return Key{Key: k, Mod: mod}
}

var namedKeys = map[string]tcell.Key{
	"enter":     tcell.KeyEnter,
	"cr":        tcell.KeyEnter,
	"return":    tcell.KeyEnter,
	"tab":       tcell.KeyTab,
	"btab":      tcell.KeyBacktab,
	"escape":    tcell.KeyEscape,
	"esc":       tcell.KeyEscape,
	"bspace":    tcell.KeyBackspace2,
	"bs":        tcell.KeyBackspace2,
	"backspace": tcell.KeyBackspace2,
	"del":       tcell.KeyDelete,
	"up":        tcell.KeyUp,
	"down":      tcell.KeyDown,
	"left":      tcell.KeyLeft,
	"right":     tcell.KeyRight,
	"home":      tcell.KeyHome,
	"end":       tcell.KeyEnd,
	"pgup":      tcell.KeyPgUp,
	"ppage":     tcell.KeyPgUp,
	"pageup":    tcell.KeyPgUp,
	"pgdn":      tcell.KeyPgDn,
	"npage":     tcell.KeyPgDn,
	"pagedown":  tcell.KeyPgDn,
	"insert":    tcell.KeyInsert,
	"ic":        tcell.KeyInsert,
	"delete":    tcell.KeyDelete,
	"dc":        tcell.KeyDelete,
}

var keyNames = func() map[tcell.Key]string {
	m := map[tcell.Key]string{}
	for _, n := range []string{"enter", "tab", "btab", "escape", "bspace", "up", "down",
		"left", "right", "home", "end", "pgup", "pgdn", "insert", "delete"} {
		k := namedKeys[n]
		if _, dup := m[k]; !dup {
			m[k] = titled(n)
		}
	}
	return m
}()

func titled(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ParseKey turns a tmux-style key specification into a Key.
//
// Accepted forms: a single character (`q`, `/`), a named key (`Enter`, `PgUp`,
// `F5`), `Space`, and the modifier prefixes `C-` and `M-`, which may combine.
func ParseKey(spec string) (Key, error) {
	if spec == "" {
		return Key{}, fmt.Errorf("empty key")
	}

	var mod tcell.ModMask
	rest := spec
	for {
		switch {
		case len(rest) > 2 && (rest[:2] == "C-" || rest[:2] == "c-"):
			mod |= tcell.ModCtrl
			rest = rest[2:]
		case len(rest) > 2 && (rest[:2] == "M-" || rest[:2] == "m-"):
			mod |= tcell.ModAlt
			rest = rest[2:]
		case len(rest) > 2 && (rest[:2] == "S-" || rest[:2] == "s-"):
			mod |= tcell.ModShift
			rest = rest[2:]
		default:
			goto done
		}
	}
done:

	lower := strings.ToLower(rest)

	if lower == "space" {
		if mod&tcell.ModCtrl != 0 {
			return Key{Key: tcell.KeyCtrlSpace, Mod: mod &^ tcell.ModCtrl}, nil
		}
		return Key{Key: tcell.KeyRune, Ch: ' ', Mod: mod}, nil
	}

	if k, ok := namedKeys[lower]; ok {
		if mod&tcell.ModCtrl != 0 {
			return Key{}, fmt.Errorf("cannot apply C- to %q", rest)
		}
		return Key{Key: k, Mod: mod}, nil
	}

	if len(lower) >= 2 && lower[0] == 'f' {
		if n, err := strconv.Atoi(lower[1:]); err == nil && n >= 1 && n <= 12 {
			return Key{Key: tcell.KeyF1 + tcell.Key(n-1), Mod: mod}, nil
		}
	}

	r, size := utf8.DecodeRuneInString(rest)
	if size != len(rest) || r == utf8.RuneError {
		return Key{}, fmt.Errorf("unknown key %q", spec)
	}

	if mod&tcell.ModCtrl != 0 {
		lr := r | 0x20 // fold to lower case for the control range
		if lr >= 'a' && lr <= 'z' {
			return Key{Key: tcell.KeyCtrlA + tcell.Key(lr-'a'), Mod: mod &^ tcell.ModCtrl}, nil
		}
		switch r {
		case '[':
			return Key{Key: tcell.KeyEscape, Mod: mod &^ tcell.ModCtrl}, nil
		case '\\':
			return Key{Key: tcell.KeyCtrlBackslash, Mod: mod &^ tcell.ModCtrl}, nil
		case ']':
			return Key{Key: tcell.KeyCtrlRightSq, Mod: mod &^ tcell.ModCtrl}, nil
		case '^':
			return Key{Key: tcell.KeyCtrlCarat, Mod: mod &^ tcell.ModCtrl}, nil
		case '_':
			return Key{Key: tcell.KeyCtrlUnderscore, Mod: mod &^ tcell.ModCtrl}, nil
		}
		return Key{}, fmt.Errorf("cannot apply C- to %q", rest)
	}

	return Key{Key: tcell.KeyRune, Ch: r, Mod: mod}, nil
}

// String renders a Key back into the specification syntax, for the help screen.
func (k Key) String() string {
	var b strings.Builder
	if k.Mod&tcell.ModAlt != 0 {
		b.WriteString("M-")
	}
	if k.Mod&tcell.ModShift != 0 {
		b.WriteString("S-")
	}

	switch {
	case k.Key == tcell.KeyRune && k.Ch == ' ':
		b.WriteString("Space")
	case k.Key == tcell.KeyRune:
		b.WriteRune(k.Ch)
	case k.Key >= tcell.KeyF1 && k.Key <= tcell.KeyF12:
		fmt.Fprintf(&b, "F%d", int(k.Key-tcell.KeyF1)+1)
	case k.Key == tcell.KeyCtrlSpace:
		b.WriteString("C-Space")
	default:
		if n, ok := keyNames[k.Key]; ok {
			b.WriteString(n)
		} else if k.Key >= tcell.KeyCtrlA && k.Key <= tcell.KeyCtrlZ {
			fmt.Fprintf(&b, "C-%c", rune('a'+int(k.Key-tcell.KeyCtrlA)))
		} else {
			fmt.Fprintf(&b, "Key(%d)", int(k.Key))
		}
	}
	return b.String()
}

// ParseSequence turns a binding specification into the keys that must be
// pressed in order.
//
// Three notations are accepted, because vim users and tmux users write key
// bindings differently and both should work:
//
//	C-a        one key, tmux style
//	PgUp       one named key
//	gg         a bare sequence: two presses of g
//	<C-w>s     vim style, where anything in angle brackets is one key
//
// A bare multi-character spec is only read as a sequence when it is not
// already the name of a single key, so `PgUp` stays one key rather than
// becoming five.
func ParseSequence(spec string) ([]Key, error) {
	if spec == "" {
		return nil, fmt.Errorf("empty key")
	}
	if strings.Contains(spec, "<") {
		return parseAngle(spec)
	}
	if k, err := ParseKey(spec); err == nil {
		return []Key{k}, nil
	}

	// A bare run of characters is a sequence of presses, but only a short one.
	// Real sequences are two or three keys; anything longer is far more likely
	// to be a misspelled key name, and silently binding `PgDown` to six
	// separate presses would be a miserable thing to debug.
	if utf8.RuneCountInString(spec) > maxBareSequence {
		return nil, fmt.Errorf("unknown key %q (write a multi-key sequence in angle brackets, as in <C-w>s)", spec)
	}

	var out []Key
	for _, r := range spec {
		k, err := ParseKey(string(r))
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}

// maxBareSequence bounds how many keys an unbracketed specification may name.
const maxBareSequence = 3

func parseAngle(spec string) ([]Key, error) {
	var out []Key
	for i := 0; i < len(spec); {
		if spec[i] != '<' {
			r, size := utf8.DecodeRuneInString(spec[i:])
			if r == utf8.RuneError && size <= 1 {
				return nil, fmt.Errorf("bad key %q", spec)
			}
			k, err := ParseKey(string(r))
			if err != nil {
				return nil, err
			}
			out = append(out, k)
			i += size
			continue
		}

		end := strings.IndexByte(spec[i:], '>')
		if end < 0 {
			return nil, fmt.Errorf("unclosed < in %q", spec)
		}
		inner := spec[i+1 : i+end]
		if inner == "" {
			return nil, fmt.Errorf("empty <> in %q", spec)
		}
		k, err := ParseKey(inner)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
		i += end + 1
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty key")
	}
	return out, nil
}

// SequenceString renders a key sequence the way it would be written in a
// configuration file.
func SequenceString(seq []Key) string {
	var b strings.Builder
	for _, k := range seq {
		s := k.String()
		// Anything that is not a single bare character needs brackets, or the
		// result would not read back as the same sequence.
		if utf8.RuneCountInString(s) == 1 {
			b.WriteString(s)
		} else {
			b.WriteByte('<')
			b.WriteString(s)
			b.WriteByte('>')
		}
	}
	return b.String()
}
