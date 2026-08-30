package config

import "fmt"

// stmt is one logical configuration line, already split into words.
type stmt struct {
	File  string
	Line  int
	Words []string
}

func (s stmt) errf(format string, a ...any) error {
	return fmt.Errorf("%s:%d: %s", s.File, s.Line, fmt.Sprintf(format, a...))
}

// tokenize splits configuration text into statements.
//
// The syntax is tmux-flavoured and deliberately small:
//
//	word word word      statement, terminated by a newline
//	'literal'           single quotes: no escape processing, ideal for regexes
//	"escaped"           double quotes: \\ \" \n \t \r \e are interpreted
//	# comment           when # starts a word, to end of line
//	trailing \          continues the statement on the next line
//	{ }                 block delimiters, as their own words
//
// Braces and a leading # inside a pattern must be quoted, which is why the
// built-in profiles quote every regex.
func tokenize(file, src string) ([]stmt, error) {
	var (
		out     []stmt
		words   []string
		cur     []byte
		haveCur bool
		line    = 1
		start   = 1
	)

	flushWord := func() {
		if haveCur {
			words = append(words, string(cur))
			cur, haveCur = cur[:0], false
		}
	}
	flushStmt := func() {
		flushWord()
		if len(words) > 0 {
			out = append(out, stmt{File: file, Line: start, Words: words})
			words = nil
		}
	}

	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\n':
			flushStmt()
			line++
			start = line
			i++

		case c == '\r':
			i++

		case c == ' ' || c == '\t':
			flushWord()
			i++

		case c == '#' && !haveCur:
			for i < len(src) && src[i] != '\n' {
				i++
			}

		case c == '\\' && isLineEnd(src, i+1):
			i++
			if src[i] == '\r' {
				i++
			}
			i++ // the newline
			line++

		case c == '\'':
			haveCur = true
			i++
			begin := i
			for i < len(src) && src[i] != '\'' {
				if src[i] == '\n' {
					return nil, fmt.Errorf("%s:%d: unterminated single quote", file, line)
				}
				i++
			}
			if i >= len(src) {
				return nil, fmt.Errorf("%s:%d: unterminated single quote", file, line)
			}
			cur = append(cur, src[begin:i]...)
			i++

		case c == '"':
			haveCur = true
			i++
			for {
				if i >= len(src) || src[i] == '\n' {
					return nil, fmt.Errorf("%s:%d: unterminated double quote", file, line)
				}
				if src[i] == '"' {
					i++
					break
				}
				if src[i] == '\\' && i+1 < len(src) {
					cur = append(cur, unescape(src[i+1]))
					i += 2
					continue
				}
				cur = append(cur, src[i])
				i++
			}

		case c == '{' || c == '}':
			flushWord()
			words = append(words, string(c))
			i++

		default:
			haveCur = true
			cur = append(cur, c)
			i++
		}
	}
	flushStmt()
	return out, nil
}

// ParseLine splits one line of interactive input into words using the same
// quoting rules as the configuration file, so the `:` prompt and a `bind`
// directive accept identical syntax.
func ParseLine(s string) ([]string, error) {
	stmts, err := tokenize("<input>", s)
	if err != nil {
		return nil, err
	}
	switch len(stmts) {
	case 0:
		return nil, nil
	case 1:
		return stmts[0].Words, nil
	default:
		return nil, fmt.Errorf("expected a single command")
	}
}

func isLineEnd(src string, i int) bool {
	if i >= len(src) {
		return false
	}
	if src[i] == '\n' {
		return true
	}
	return src[i] == '\r' && i+1 < len(src) && src[i+1] == '\n'
}

func unescape(c byte) byte {
	switch c {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	case 'e':
		return 0x1b
	case '0':
		return 0
	default:
		return c
	}
}
