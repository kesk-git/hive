package schema

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// j turns the placeholder @u into the backslash-u of a JSON escape, so that
// the test source says what it means.
func j(s string) string { return strings.ReplaceAll(s, "@u", `\`+`u`) }

func TestDecodeAccepts(t *testing.T) {
	cases := []struct {
		in   string
		want any
	}{
		{`null`, nil},
		{`true`, true},
		{`false`, false},
		{`0`, int64(0)},
		{`-12`, int64(-12)},
		{`9007199254740991`, int64(MaxWhole)},
		{`-9007199254740991`, int64(-MaxWhole)},
		{`"a"`, "a"},
		{`[]`, []any{}},
		{`{}`, map[string]any{}},
		{" \t\r\n{\"a\":[1,{\"b\":null}], \"c\" : \"d\"}\n", map[string]any{"a": []any{int64(1), map[string]any{"b": nil}}, "c": "d"}},
		{j(`"@u00e9"`), "é"},
		{j(`"@u00E9"`), "é"},
		{j(`"@ud83d@ude00"`), "😀"},
		{`"\/\b\f\n\r\t\"\\"`, "/\b\f\n\r\t\"\\"},
		{j(`"@u0000"`), "\x00"},
	}
	for _, c := range cases {
		got, err := Decode([]byte(c.in))
		if err != nil {
			t.Errorf("Decode(%q): %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Decode(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

// Text is kept exactly as written: nothing normalizes, trims or re-escapes it.
func TestDecodeKeepsTextExactly(t *testing.T) {
	for _, s := range []string{
		"é", "e" + string(rune(0x301)), // composed and decomposed stay different
		string(rune(0xfeff)), string(rune(0x2028)) + string(rune(0x2029)),
		string(rune(0xa0)) + " trailing  ", string(rune(0x10ffff)), string(rune(0xfdd0)),
		" leading", "tab\tinside", "a" + string(rune(0x200d)) + "b",
	} {
		got, err := Decode([]byte(`"` + strings.ReplaceAll(s, "\t", `\t`) + `"`))
		if err != nil {
			t.Errorf("%q: %v", s, err)
			continue
		}
		if got != s {
			t.Errorf("Decode kept %q as %q", s, got)
		}
	}
	// The same text, written escaped and written raw, is the same string, and
	// the two normalization forms are not.
	a, _ := Decode([]byte(j(`"@u00e9"`)))
	b, _ := Decode([]byte(`"é"`))
	c, _ := Decode([]byte(j(`"e@u0301"`)))
	if a != b || a == c {
		t.Errorf("escaped %q, raw %q, decomposed %q", a, b, c)
	}
}

func TestDecodeRefuses(t *testing.T) {
	cases := []struct{ in, want string }{
		{"\xef\xbb\xbf{}", "byte-order mark"},
		{"\"\xff\"", "invalid UTF-8"},
		{"\"\xc0\xaf\"", "invalid UTF-8"},
		{"\"\xed\xa0\x80\"", "invalid UTF-8"}, // a surrogate written as UTF-8
		{``, "empty document"},
		{" \n", "empty document"},
		{`{"a":1,"a":2}`, "duplicate member name"},
		{j(`{"a":1,"@u0061":2}`), "duplicate member name"},
		{`1.5`, "not whole"},
		{`1.0`, "not whole"},
		{`1e3`, "not whole"},
		{`1E3`, "not whole"},
		{`-1.5e-3`, "not whole"},
		{`[1,2.5]`, "not whole"},
		{`9007199254740992`, "outside"},
		{`-9007199254740992`, "outside"},
		{`99999999999999999999`, "outside"},
		{`-0`, "negative zero"},
		{`01`, "leading zero"},
		{`-`, "end of document"},
		{`-a`, "expected a digit"},
		{`+1`, "unexpected character"},
		{`.5`, "unexpected character"},
		{`1.`, "not whole"},
		{`NaN`, "unexpected character"},
		{`Infinity`, "unexpected character"},
		{j(`"@ud800"`), "lone surrogate"},
		{j(`"@udc00"`), "lone surrogate"},
		{j(`"@ud800@u0041"`), "lone surrogate"},
		{j(`"@ud800@ud800"`), "lone surrogate"},
		{j(`"@ud800x"`), "lone surrogate"},
		{j(`"@u12"`), "malformed"},
		{j(`"@uzzzz"`), "malformed"},
		{j(`"@u+123"`), "malformed"},
		{`"\x41"`, "unknown escape"},
		{"\"a\tb\"", "control character"},
		{"\"a\nb\"", "control character"},
		{`"abc`, "unterminated"},
		{`"abc\`, "unterminated"},
		{`{"a":1,}`, "member name"},
		{`[1,]`, "unexpected character"},
		{`[1 2]`, "expected ',' or ']'"},
		{`{"a" 1}`, "expected ':'"},
		{`{a:1}`, "member name"},
		{`{"a":1`, "end of document"},
		{`[1`, "end of document"},
		{`nul`, "unexpected character"},
		{`truee`, "after the document"},
		{`{} {}`, "after the document"},
		{`{}x`, "after the document"},
		{`'a'`, "unexpected character"},
	}
	for _, c := range cases {
		_, err := Decode([]byte(c.in))
		if err == nil {
			t.Errorf("Decode(%q) succeeded", c.in)
			continue
		}
		var se *SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("Decode(%q): %T, want *SyntaxError", c.in, err)
		} else if !strings.Contains(se.Msg, c.want) {
			t.Errorf("Decode(%q): %q, want it to say %q", c.in, se.Msg, c.want)
		}
	}
}

func TestDecodeReportsWhere(t *testing.T) {
	for _, c := range []struct {
		in  string
		off int
	}{
		{"{\"a\":\"\xff\"}", 6},
		{`{"a":1,"a":2}`, 7},
		{`[1, 2.5]`, 4},
		{`[1,x]`, 3},
	} {
		_, err := Decode([]byte(c.in))
		var se *SyntaxError
		if !errors.As(err, &se) || se.Offset != c.off {
			t.Errorf("Decode(%q) = %v, want a fault at byte %d", c.in, err, c.off)
		}
	}
}

func TestDecodeDepth(t *testing.T) {
	nest := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	if _, err := Decode([]byte(nest(MaxDepth))); err != nil {
		t.Errorf("depth %d: %v", MaxDepth, err)
	}
	if _, err := Decode([]byte(nest(MaxDepth + 1))); err == nil || !strings.Contains(err.Error(), "nested too deeply") {
		t.Errorf("depth %d: %v", MaxDepth+1, err)
	}
	objects := strings.Repeat(`{"a":`, MaxDepth+1) + "1" + strings.Repeat("}", MaxDepth+1)
	if _, err := Decode([]byte(objects)); err == nil {
		t.Errorf("objects nested %d deep were accepted", MaxDepth+1)
	}
	// A hostile document does not exhaust the stack.
	if _, err := Decode([]byte(strings.Repeat("[", 1_000_000))); err == nil {
		t.Error("a million open brackets were accepted")
	}
}
