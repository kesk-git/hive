package canon

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// MaxDepth is the deepest nesting of arrays and objects that Parse accepts.
// No record of this specification comes near it.
const MaxDepth = 512

// MaxNumber is the largest magnitude of a number in a record: 2^53 - 1.
const MaxNumber = 1<<53 - 1

// The errors of reading a document. Each is wrapped with the byte offset at
// which it was found; test for one with errors.Is.
var (
	// ErrSyntax reports text that is not JSON.
	ErrSyntax = errors.New("canon: not valid JSON")
	// ErrTrailing reports content after the first JSON value.
	ErrTrailing = errors.New("canon: content after the JSON value")
	// ErrNotWhole reports a number with a fraction or an exponent.
	ErrNotWhole = errors.New("canon: number is not whole")
	// ErrRange reports a number beyond ±9,007,199,254,740,991.
	ErrRange = errors.New("canon: number outside the whole-number range")
	// ErrDuplicateKey reports an object that names one member twice, after
	// escapes are resolved.
	ErrDuplicateKey = errors.New("canon: duplicate object member")
	// ErrUTF8 reports a string that cannot be kept whole: bytes that are not
	// UTF-8, or an escape that is half of a surrogate pair.
	ErrUTF8 = errors.New("canon: string is not well-formed Unicode")
	// ErrDepth reports nesting deeper than MaxDepth.
	ErrDepth = errors.New("canon: nesting too deep")
)

func fail(kind error, off int, detail string) error {
	if detail == "" {
		return fmt.Errorf("%w at offset %d", kind, off)
	}
	return fmt.Errorf("%w at offset %d: %s", kind, off, detail)
}

// Parse reads one JSON document into Go values: map[string]any, []any,
// string, bool, nil and int64. It is stricter than the standard library's
// decoder: it refuses duplicate members, fractional and exponent numbers,
// numbers beyond ±2^53-1, bytes that are not UTF-8 and lone surrogate
// escapes, and it never replaces or normalizes any text. Whitespace around
// the value is allowed; anything else after it is not.
func Parse(data []byte) (any, error) {
	p := &parser{data: data}
	p.skipSpace()
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos != len(p.data) {
		return nil, fail(ErrTrailing, p.pos, "")
	}
	return v, nil
}

type parser struct {
	data []byte
	pos  int
}

func (p *parser) skipSpace() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) value(depth int) (any, error) {
	if p.pos >= len(p.data) {
		return nil, fail(ErrSyntax, p.pos, "unexpected end of input")
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		return p.str()
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	case c == 't':
		return p.literal("true", true)
	case c == 'f':
		return p.literal("false", false)
	case c == 'n':
		return p.literal("null", nil)
	default:
		return nil, fail(ErrSyntax, p.pos, fmt.Sprintf("unexpected byte 0x%02x", c))
	}
}

func (p *parser) literal(word string, v any) (any, error) {
	if len(p.data)-p.pos < len(word) || string(p.data[p.pos:p.pos+len(word)]) != word {
		return nil, fail(ErrSyntax, p.pos, "misspelled literal")
	}
	p.pos += len(word)
	return v, nil
}

func (p *parser) object(depth int) (any, error) {
	if depth >= MaxDepth {
		return nil, fail(ErrDepth, p.pos, "")
	}
	p.pos++ // {
	obj := map[string]any{}
	p.skipSpace()
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++
		return obj, nil
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			return nil, fail(ErrSyntax, p.pos, "object member name expected")
		}
		keyAt := p.pos
		key, err := p.str()
		if err != nil {
			return nil, err
		}
		if _, dup := obj[key]; dup {
			return nil, fail(ErrDuplicateKey, keyAt, fmt.Sprintf("%q", key))
		}
		p.skipSpace()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return nil, fail(ErrSyntax, p.pos, "colon expected")
		}
		p.pos++
		p.skipSpace()
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		obj[key] = v
		p.skipSpace()
		if p.pos >= len(p.data) {
			return nil, fail(ErrSyntax, p.pos, "unexpected end of input")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return obj, nil
		default:
			return nil, fail(ErrSyntax, p.pos, "comma or closing brace expected")
		}
	}
}

func (p *parser) array(depth int) (any, error) {
	if depth >= MaxDepth {
		return nil, fail(ErrDepth, p.pos, "")
	}
	p.pos++ // [
	arr := []any{}
	p.skipSpace()
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		return arr, nil
	}
	for {
		p.skipSpace()
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
		p.skipSpace()
		if p.pos >= len(p.data) {
			return nil, fail(ErrSyntax, p.pos, "unexpected end of input")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return arr, nil
		default:
			return nil, fail(ErrSyntax, p.pos, "comma or closing bracket expected")
		}
	}
}

func (p *parser) number() (any, error) {
	start := p.pos
	neg := false
	if p.data[p.pos] == '-' {
		neg = true
		p.pos++
	}
	if p.pos >= len(p.data) || p.data[p.pos] < '0' || p.data[p.pos] > '9' {
		return nil, fail(ErrSyntax, p.pos, "digit expected")
	}
	digits := p.pos
	if p.data[p.pos] == '0' {
		p.pos++
		if p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			return nil, fail(ErrSyntax, p.pos, "leading zero")
		}
	} else {
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
	}
	if p.pos < len(p.data) {
		switch p.data[p.pos] {
		case '.', 'e', 'E':
			return nil, fail(ErrNotWhole, start, "")
		}
	}
	var n int64
	for _, d := range p.data[digits:p.pos] {
		n = n*10 + int64(d-'0')
		if n > MaxNumber {
			return nil, fail(ErrRange, start, "")
		}
	}
	if neg {
		n = -n
	}
	return n, nil
}

func (p *parser) str() (string, error) {
	start := p.pos
	p.pos++ // opening quote
	var out []byte
	for {
		if p.pos >= len(p.data) {
			return "", fail(ErrSyntax, start, "unterminated string")
		}
		c := p.data[p.pos]
		switch {
		case c == '"':
			p.pos++
			return string(out), nil
		case c < 0x20:
			return "", fail(ErrSyntax, p.pos, "control character in string")
		case c == '\\':
			r, err := p.escape()
			if err != nil {
				return "", err
			}
			out = utf8.AppendRune(out, r)
		case c < utf8.RuneSelf:
			out = append(out, c)
			p.pos++
		default:
			r, size := utf8.DecodeRune(p.data[p.pos:])
			if r == utf8.RuneError && size == 1 {
				return "", fail(ErrUTF8, p.pos, "bytes are not UTF-8")
			}
			out = append(out, p.data[p.pos:p.pos+size]...)
			p.pos += size
		}
	}
}

// escape reads the escape sequence at p.pos, which is a backslash.
func (p *parser) escape() (rune, error) {
	at := p.pos
	if p.pos+1 >= len(p.data) {
		return 0, fail(ErrSyntax, at, "unterminated escape")
	}
	c := p.data[p.pos+1]
	p.pos += 2
	switch c {
	case '"', '\\', '/':
		return rune(c), nil
	case 'b':
		return '\b', nil
	case 'f':
		return '\f', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 't':
		return '\t', nil
	case 'u':
		r, ok := p.hex4()
		if !ok {
			return 0, fail(ErrSyntax, at, "bad \\u escape")
		}
		switch {
		case r >= 0xD800 && r < 0xDC00:
			if p.pos+1 < len(p.data) && p.data[p.pos] == '\\' && p.data[p.pos+1] == 'u' {
				save := p.pos
				p.pos += 2
				if lo, ok := p.hex4(); ok && lo >= 0xDC00 && lo < 0xE000 {
					return 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00), nil
				}
				p.pos = save
			}
			return 0, fail(ErrUTF8, at, "high surrogate without its low surrogate")
		case r >= 0xDC00 && r < 0xE000:
			return 0, fail(ErrUTF8, at, "low surrogate without its high surrogate")
		}
		return r, nil
	default:
		return 0, fail(ErrSyntax, at, "unknown escape")
	}
}

// hex4 reads four hexadecimal digits at p.pos.
func (p *parser) hex4() (rune, bool) {
	if p.pos+4 > len(p.data) {
		return 0, false
	}
	var r rune
	for _, c := range p.data[p.pos : p.pos+4] {
		switch {
		case c >= '0' && c <= '9':
			r = r<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	p.pos += 4
	return r, true
}
