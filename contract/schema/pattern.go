package schema

import (
	"fmt"
	"regexp"
	"strings"
)

// compilePattern compiles a `pattern` once it parses under the grammar below,
// the syntax RE2 and ECMAScript share with one meaning, so that the same
// schema gives the same answer in Go and in a browser. Whatever does not parse
// is refused; nothing is translated.
//
//	pattern   = alt
//	alt       = seq { "|" seq }
//	seq       = { "^" | "$" | "\b" | "\B" | atom [ repeat ] }
//	atom      = literal | escape | class | "(" [ "?:" ] alt ")"
//	repeat    = ( "?" | "*" | "+" | "{" n "}" | "{" n "," "}" | "{" n "," m "}" ) [ "?" ]
//	class     = "[" [ "^" ] item { item } "]"
//	item      = member [ "-" member ] | "-" at the start or the end
//	member    = any character but \ [ ] | escape except \b \B \d \D \w \W
//	escape    = "\" punctuation | "\d" | "\D" | "\w" | "\W"
//	          | "\n" | "\r" | "\t" | "\f" | "\v" | "\x" hex hex
//	literal   = any character but \ ^ $ . * + ? ( ) [ ] { } |
//
// Punctuation is one of \ ^ $ . * + ? ( ) [ ] { } | / and, inside a class
// only, -. A count is a decimal number without a leading zero, at most 1000,
// and m is not below n. The dot, \s and \S are not in the grammar: they match
// different characters in the two engines.
func compilePattern(src string) (*regexp.Regexp, error) {
	p := &patternParser{src: []rune(src)}
	if err := p.parse(); err != nil {
		return nil, err
	}
	re, err := regexp.Compile(src)
	if err != nil {
		return nil, fmt.Errorf("not a regular expression: %w", err)
	}
	return re, nil
}

const quantifierMax = 1000 // the repeat count RE2 allows

type patternParser struct {
	src []rune
	pos int
}

func (p *patternParser) fail(format string, args ...any) error {
	return fmt.Errorf("not in the syntax common to RE2 and ECMAScript: at %d, %s", p.pos, fmt.Sprintf(format, args...))
}

func (p *patternParser) more() bool { return p.pos < len(p.src) }

func (p *patternParser) peek() rune { return p.src[p.pos] }

func (p *patternParser) parse() error {
	return p.alt(0)
}

func (p *patternParser) alt(depth int) error {
	for {
		if err := p.seq(depth); err != nil {
			return err
		}
		if !p.more() || p.peek() != '|' {
			return nil
		}
		p.pos++
	}
}

func (p *patternParser) seq(depth int) error {
	for p.more() {
		c := p.peek()
		switch {
		case c == '|' || (c == ')' && depth > 0):
			return nil
		case c == '^' || c == '$':
			p.pos++
			continue
		case c == '\\' && p.pos+1 < len(p.src) && (p.src[p.pos+1] == 'b' || p.src[p.pos+1] == 'B'):
			p.pos += 2
			continue
		}
		if err := p.atom(depth); err != nil {
			return err
		}
		if err := p.repeat(); err != nil {
			return err
		}
	}
	return nil
}

func (p *patternParser) atom(depth int) error {
	c := p.peek()
	switch c {
	case '(':
		p.pos++
		if p.more() && p.peek() == '?' {
			if !strings.HasPrefix(string(p.src[p.pos:]), "?:") {
				return p.fail("only (?: may follow (")
			}
			p.pos += 2
		}
		if err := p.alt(depth + 1); err != nil {
			return err
		}
		if !p.more() || p.peek() != ')' {
			return p.fail("a group is not closed")
		}
		p.pos++
		return nil
	case '[':
		return p.class()
	case '\\':
		_, err := p.escape(false)
		return err
	case '.', '*', '+', '?', '{', '}', ']', ')':
		return p.fail("%c is not an atom; write the class out or escape it", c)
	}
	p.pos++
	return nil
}

// escape reads one escape and says whether it stands for a single character
// (and so may end a range) rather than a set.
func (p *patternParser) escape(inClass bool) (single bool, err error) {
	p.pos++
	if !p.more() {
		return false, p.fail("a lone backslash")
	}
	e := p.peek()
	p.pos++
	switch {
	case strings.ContainsRune(`\^$.*+?()[]{}|/`, e), e == '-' && inClass:
		return true, nil
	case strings.ContainsRune("nrtfv", e):
		return true, nil
	case strings.ContainsRune("dDwW", e):
		return false, nil
	case e == 'x':
		if p.pos+2 > len(p.src) || !isHex(p.src[p.pos]) || !isHex(p.src[p.pos+1]) {
			return false, p.fail("\\x needs exactly two hexadecimal digits")
		}
		p.pos += 2
		return true, nil
	case e == 's' || e == 'S':
		return false, p.fail("\\%c matches different characters in RE2 and ECMAScript; write the class out", e)
	}
	return false, p.fail("escape \\%c is not in the grammar", e)
}

func isHex(c rune) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func (p *patternParser) class() error {
	p.pos++
	if p.more() && p.peek() == '^' {
		p.pos++
	}
	first := true
	for {
		if !p.more() {
			return p.fail("a class is not closed")
		}
		if p.peek() == ']' {
			if first {
				return p.fail("a class lists no member")
			}
			p.pos++
			return nil
		}
		if p.peek() == '-' && !first && p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
			return p.fail("a - between members is a range of two characters")
		}
		first = false
		lo, single, err := p.member()
		if err != nil {
			return err
		}
		if !p.more() || p.peek() != '-' || p.pos+1 >= len(p.src) || p.src[p.pos+1] == ']' {
			continue
		}
		p.pos++
		hi, hiSingle, err := p.member()
		if err != nil {
			return err
		}
		if !single || !hiSingle {
			return p.fail("a range runs between two characters, not a class")
		}
		if hi < lo {
			return p.fail("a range runs backwards")
		}
	}
}

// member reads one class member and returns the character it stands for,
// where it stands for one.
func (p *patternParser) member() (rune, bool, error) {
	c := p.peek()
	switch c {
	case '\\':
		if p.pos+1 < len(p.src) && p.src[p.pos+1] == 'b' {
			return 0, false, p.fail("\\b in a class is a backspace in one engine only")
		}
		start := p.pos
		single, err := p.escape(true)
		if err != nil || !single {
			return 0, false, err
		}
		return escapedRune(p.src[start+1 : p.pos]), true, nil
	case '[', ']':
		return 0, false, p.fail("%c in a class must be escaped", c)
	}
	p.pos++
	return c, true, nil
}

// escapedRune is the character a single-character escape (without its
// backslash) stands for.
func escapedRune(e []rune) rune {
	switch e[0] {
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	case 'f':
		return '\f'
	case 'v':
		return '\v'
	case 'x':
		var n rune
		for _, h := range e[1:3] {
			n = n<<4 | rune(strings.IndexRune("0123456789abcdef", h|0x20))
		}
		return n
	}
	return e[0]
}

func (p *patternParser) repeat() error {
	if !p.more() {
		return nil
	}
	switch p.peek() {
	case '?', '*', '+':
		p.pos++
	case '{':
		p.pos++
		lo, err := p.count()
		if err != nil {
			return err
		}
		if p.more() && p.peek() == ',' {
			p.pos++
			if p.more() && p.peek() != '}' {
				hi, err := p.count()
				if err != nil {
					return err
				}
				if hi < lo {
					return p.fail("a repeat counts down")
				}
			}
		}
		if !p.more() || p.peek() != '}' {
			return p.fail("a repeat is not closed")
		}
		p.pos++
	default:
		return nil
	}
	if p.more() && p.peek() == '?' {
		p.pos++
	}
	if p.more() {
		switch p.peek() {
		case '?', '*', '+', '{':
			return p.fail("a repeat applies to an atom, and an atom has one")
		}
	}
	return nil
}

// count reads a plain decimal number without a leading zero, at most
// quantifierMax.
func (p *patternParser) count() (int, error) {
	start, n := p.pos, 0
	for p.more() && p.peek() >= '0' && p.peek() <= '9' {
		n = n*10 + int(p.peek()-'0')
		if n > quantifierMax {
			return 0, p.fail("a count exceeds %d", quantifierMax)
		}
		p.pos++
	}
	switch {
	case p.pos == start:
		return 0, p.fail("a count is a decimal number")
	case p.src[start] == '0' && p.pos-start > 1:
		return 0, p.fail("a count has no leading zero")
	}
	return n, nil
}
