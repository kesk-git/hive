// Package canon writes Hive's canonical form of a record, RFC 8785 (the JSON
// Canonicalization Scheme), and the digests defined over it.
//
// A record holds no fractional or exponent number and none outside
// ±9,007,199,254,740,991, so the RFC's number rule reduces to writing an
// integer in decimal, and the reader refuses anything else rather than
// rounding it (spec/records.md, Encoding). Text is kept exactly as written:
// nothing here normalizes, trims or replaces a string, and a string that
// cannot be kept whole is refused.
package canon

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// ErrUnsupportedType reports a Go value Encode cannot write: anything but
// map[string]any, []any, string, bool, nil and the integer types.
var ErrUnsupportedType = errors.New("canon: value of a type that is not JSON data")

// Canonicalize reads one JSON document and returns its canonical form, with
// no trailing line feed.
func Canonicalize(data []byte) ([]byte, error) {
	v, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return Encode(v)
}

// Encode writes v in canonical form, with no trailing line feed. v is built
// of map[string]any, []any, string, bool, nil and integers of any width, as
// Parse returns, and every integer must be within ±2^53-1.
func Encode(v any) ([]byte, error) {
	return appendValue(nil, v, 0)
}

// Line is the canonical form of data followed by a line feed: one line of a
// journal and the end of a record file.
func Line(data []byte) ([]byte, error) {
	out, err := Canonicalize(data)
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// IsCanonical reports whether data, without a trailing line feed, is exactly
// its own canonical form.
func IsCanonical(data []byte) bool {
	out, err := Canonicalize(data)
	return err == nil && string(out) == string(data)
}

func appendValue(dst []byte, v any, depth int) ([]byte, error) {
	switch x := v.(type) {
	case nil:
		return append(dst, "null"...), nil
	case bool:
		return strconv.AppendBool(dst, x), nil
	case string:
		return appendString(dst, x)
	case map[string]any:
		if depth >= MaxDepth {
			return nil, ErrDepth
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return lessUTF16(keys[i], keys[j]) })
		dst = append(dst, '{')
		for i, k := range keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			var err error
			if dst, err = appendString(dst, k); err != nil {
				return nil, err
			}
			dst = append(dst, ':')
			if dst, err = appendValue(dst, x[k], depth+1); err != nil {
				return nil, err
			}
		}
		return append(dst, '}'), nil
	case []any:
		if depth >= MaxDepth {
			return nil, ErrDepth
		}
		dst = append(dst, '[')
		for i, e := range x {
			if i > 0 {
				dst = append(dst, ',')
			}
			var err error
			if dst, err = appendValue(dst, e, depth+1); err != nil {
				return nil, err
			}
		}
		return append(dst, ']'), nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return appendInt(dst, rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u := rv.Uint()
		if u > MaxNumber {
			return nil, fmt.Errorf("%w: %d", ErrRange, u)
		}
		return appendInt(dst, int64(u))
	}
	return nil, fmt.Errorf("%w: %T", ErrUnsupportedType, v)
}

func appendInt(dst []byte, n int64) ([]byte, error) {
	if n > MaxNumber || n < -MaxNumber || n == math.MinInt64 {
		return nil, fmt.Errorf("%w: %d", ErrRange, n)
	}
	return strconv.AppendInt(dst, n, 10), nil
}

const hexDigits = "0123456789abcdef"

// appendString writes s as RFC 8785 does: only the quotation mark, the
// reverse solidus and the control characters are escaped, the control
// characters without a short form as \u00xx in lower case, and everything
// else is written as it is.
func appendString(dst []byte, s string) ([]byte, error) {
	if !utf8.ValidString(s) {
		return nil, fmt.Errorf("%w: a string holds bytes that are not UTF-8", ErrUTF8)
	}
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			dst = append(dst, '\\', '"')
		case c == '\\':
			dst = append(dst, '\\', '\\')
		case c == '\b':
			dst = append(dst, '\\', 'b')
		case c == '\f':
			dst = append(dst, '\\', 'f')
		case c == '\n':
			dst = append(dst, '\\', 'n')
		case c == '\r':
			dst = append(dst, '\\', 'r')
		case c == '\t':
			dst = append(dst, '\\', 't')
		case c < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&15])
		default:
			dst = append(dst, c)
		}
	}
	return append(dst, '"'), nil
}

// lessUTF16 orders two strings by their UTF-16 code units, as RFC 8785
// sorts object members. It differs from byte order only where a character
// beyond the Basic Multilingual Plane meets one from U+E000 up.
func lessUTF16(a, b string) bool {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}

// DigestPrefix begins every digest.
const DigestPrefix = "sha256:"

// FileDigest is the digest of a file: SHA-256 of its bytes, written
// sha256: and 64 lowercase hexadecimal digits.
func FileDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return DigestPrefix + hex.EncodeToString(sum[:])
}

// RecordDigest is the digest of a record: the digest of its canonical form,
// without a trailing line feed. It does not depend on how the record was
// spaced or ordered where it was read from.
func RecordDigest(data []byte) (string, error) {
	out, err := Canonicalize(data)
	if err != nil {
		return "", err
	}
	return FileDigest(out), nil
}

// ValidDigest reports whether s is a digest: sha256: and 64 lowercase
// hexadecimal digits.
func ValidDigest(s string) bool {
	if len(s) != len(DigestPrefix)+64 || s[:len(DigestPrefix)] != DigestPrefix {
		return false
	}
	for i := len(DigestPrefix); i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// ErrFileForm reports a record file that is not UTF-8 JSON without a
// byte-order mark and ending in a single line feed.
var ErrFileForm = errors.New("canon: not a record file: JSON in UTF-8, no byte-order mark, one final line feed")

// CheckFile checks the form of a record file (spec/records.md, Encoding): one
// JSON document in UTF-8, no byte-order mark, ending in a single line feed.
// It does not require the document to be in canonical form.
func CheckFile(data []byte) error {
	n := len(data)
	if n < 2 || data[n-1] != '\n' || data[n-2] == '\n' {
		return fmt.Errorf("%w: it must end in exactly one line feed", ErrFileForm)
	}
	if n >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return fmt.Errorf("%w: it begins with a byte-order mark", ErrFileForm)
	}
	_, err := Parse(data)
	return err
}
