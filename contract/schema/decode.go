package schema

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxWhole is the largest magnitude of a number a record may hold.
const MaxWhole = 1<<53 - 1

// MaxDepth is the deepest nesting of arrays and objects Decode accepts and the
// validator follows. It bounds the work a hostile or accidental document can
// cause; no record of Hive comes near it.
const MaxDepth = 512

// SyntaxError reports where a document stops being a Hive record.
type SyntaxError struct {
	Offset int    // byte offset of the fault
	Msg    string // what is wrong, never the offending text
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("json: %s at byte %d", e.Msg, e.Offset)
}

// Decode reads one JSON document in the encoding of spec/records.md: UTF-8
// without a byte-order mark, every object member name unique, every string
// exactly as written, every number whole and within ±MaxWhole.
//
// It yields nil, bool, string, int64, []any and map[string]any. Framing that
// belongs to the file, such as the single final line feed, is not checked
// here; any JSON whitespace around the document is allowed.
func Decode(data []byte) (any, error) {
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		return nil, &SyntaxError{0, "byte-order mark"}
	}
	if !utf8.Valid(data) {
		off := 0
		for off < len(data) {
			r, n := utf8.DecodeRune(data[off:])
			if r == utf8.RuneError && n == 1 {
				break
			}
			off += n
		}
		return nil, &SyntaxError{off, "invalid UTF-8"}
	}
	p := &parser{data: data}
	p.space()
	if p.pos == len(data) {
		return nil, &SyntaxError{p.pos, "empty document"}
	}
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.space()
	if p.pos != len(data) {
		return nil, p.fault("data after the document")
	}
	return v, nil
}

type parser struct {
	data []byte
	pos  int
}

func (p *parser) fault(msg string) error { return &SyntaxError{p.pos, msg} }

func (p *parser) space() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) literal(word string, v any) (any, error) {
	if !bytes.HasPrefix(p.data[p.pos:], []byte(word)) {
		return nil, p.fault("unexpected character")
	}
	p.pos += len(word)
	return v, nil
}

func (p *parser) value(depth int) (any, error) {
	if p.pos >= len(p.data) {
		return nil, p.fault("unexpected end of document")
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		return p.object(depth + 1)
	case c == '[':
		return p.array(depth + 1)
	case c == '"':
		return p.str()
	case c == 't':
		return p.literal("true", true)
	case c == 'f':
		return p.literal("false", false)
	case c == 'n':
		return p.literal("null", nil)
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	}
	return nil, p.fault("unexpected character")
}

func (p *parser) object(depth int) (any, error) {
	if depth > MaxDepth {
		return nil, p.fault("nested too deeply")
	}
	p.pos++ // {
	obj := map[string]any{}
	p.space()
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++
		return obj, nil
	}
	for {
		p.space()
		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			return nil, p.fault("expected a member name")
		}
		keyAt := p.pos
		key, err := p.str()
		if err != nil {
			return nil, err
		}
		if _, dup := obj[key.(string)]; dup {
			return nil, &SyntaxError{keyAt, "duplicate member name"}
		}
		p.space()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return nil, p.fault("expected ':'")
		}
		p.pos++
		p.space()
		v, err := p.value(depth)
		if err != nil {
			return nil, err
		}
		obj[key.(string)] = v
		p.space()
		if p.pos >= len(p.data) {
			return nil, p.fault("unexpected end of document")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return obj, nil
		default:
			return nil, p.fault("expected ',' or '}'")
		}
	}
}

func (p *parser) array(depth int) (any, error) {
	if depth > MaxDepth {
		return nil, p.fault("nested too deeply")
	}
	p.pos++ // [
	arr := []any{}
	p.space()
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		return arr, nil
	}
	for {
		p.space()
		v, err := p.value(depth)
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
		p.space()
		if p.pos >= len(p.data) {
			return nil, p.fault("unexpected end of document")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return arr, nil
		default:
			return nil, p.fault("expected ',' or ']'")
		}
	}
}

func (p *parser) number() (any, error) {
	start := p.pos
	if p.data[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.data) {
		return nil, p.fault("unexpected end of document")
	}
	switch c := p.data[p.pos]; {
	case c == '0':
		p.pos++
		if p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			return nil, p.fault("leading zero in a number")
		}
	case c >= '1' && c <= '9':
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
	default:
		return nil, p.fault("expected a digit")
	}
	if p.pos < len(p.data) {
		switch p.data[p.pos] {
		case '.', 'e', 'E':
			return nil, &SyntaxError{start, "number is not whole: a fraction or an exponent"}
		}
	}
	text := string(p.data[start:p.pos])
	if text == "-0" {
		return nil, &SyntaxError{start, "negative zero"}
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n > MaxWhole || n < -MaxWhole {
		return nil, &SyntaxError{start, "number outside ±9007199254740991"}
	}
	return n, nil
}

// str reads a string. A lone surrogate escape cannot be kept whole as UTF-8,
// so it is refused rather than replaced.
func (p *parser) str() (any, error) {
	p.pos++ // opening quote
	var sb strings.Builder
	run := p.pos
	for {
		if p.pos >= len(p.data) {
			return nil, p.fault("unterminated string")
		}
		c := p.data[p.pos]
		switch {
		case c == '"':
			sb.Write(p.data[run:p.pos])
			p.pos++
			return sb.String(), nil
		case c < 0x20:
			return nil, p.fault("control character in a string")
		case c != '\\':
			p.pos++
			continue
		}
		sb.Write(p.data[run:p.pos])
		p.pos++
		if p.pos >= len(p.data) {
			return nil, p.fault("unterminated string")
		}
		esc := p.data[p.pos]
		p.pos++
		switch esc {
		case '"', '\\', '/':
			sb.WriteByte(esc)
		case 'b':
			sb.WriteByte('\b')
		case 'f':
			sb.WriteByte('\f')
		case 'n':
			sb.WriteByte('\n')
		case 'r':
			sb.WriteByte('\r')
		case 't':
			sb.WriteByte('\t')
		case 'u':
			r, err := p.unicode()
			if err != nil {
				return nil, err
			}
			sb.WriteRune(r)
		default:
			p.pos--
			return nil, p.fault("unknown escape")
		}
		run = p.pos
	}
}

func (p *parser) hex4() (rune, bool) {
	if p.pos+4 > len(p.data) {
		return 0, false
	}
	n, err := strconv.ParseUint(string(p.data[p.pos:p.pos+4]), 16, 32)
	if err != nil {
		return 0, false
	}
	p.pos += 4
	return rune(n), true
}

func (p *parser) unicode() (rune, error) {
	at := p.pos - 2
	r, ok := p.hex4()
	if !ok {
		return 0, &SyntaxError{at, "malformed \\u escape"}
	}
	switch {
	case r >= 0xDC00 && r <= 0xDFFF:
		return 0, &SyntaxError{at, "lone surrogate escape"}
	case r >= 0xD800 && r <= 0xDBFF:
		if !bytes.HasPrefix(p.data[p.pos:], []byte(`\u`)) {
			return 0, &SyntaxError{at, "lone surrogate escape"}
		}
		p.pos += 2
		lo, ok := p.hex4()
		if !ok || lo < 0xDC00 || lo > 0xDFFF {
			return 0, &SyntaxError{at, "lone surrogate escape"}
		}
		return 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00), nil
	}
	return r, nil
}
