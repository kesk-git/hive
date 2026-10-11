package canon

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/kesk-git/hive/spec"
)

func read(t testing.TB, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func names(t testing.TB, dir, ext string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", dir, "*"+ext))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no vectors under testdata/%s: %v", dir, err)
	}
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = strings.TrimSuffix(filepath.Base(p), ext)
	}
	return out
}

// The valid vectors: for each name, the input, its canonical form and the
// digest of that form. The expected files were produced by a separate
// implementation, not by this package.
func TestValidVectors(t *testing.T) {
	for _, name := range names(t, "valid", ".json") {
		t.Run(name, func(t *testing.T) {
			base := filepath.Join("testdata", "valid", name)
			in := read(t, base+".json")
			want := read(t, base+".canonical")
			digest := strings.TrimSpace(string(read(t, base+".digest")))

			got, err := Canonicalize(in)
			if err != nil {
				t.Fatalf("Canonicalize: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("canonical form\n got %q\nwant %q", got, want)
			}
			if !IsCanonical(want) {
				t.Error("the expected form is not canonical")
			}
			again, err := Canonicalize(got)
			if err != nil || !bytes.Equal(again, got) {
				t.Errorf("not idempotent: %q, %v", again, err)
			}
			if d, err := RecordDigest(in); err != nil || d != digest {
				t.Errorf("RecordDigest = %q, %v; want %q", d, err, digest)
			}
			if d := FileDigest(want); d != digest {
				t.Errorf("FileDigest(canonical) = %q; want %q", d, digest)
			}
			if !ValidDigest(digest) {
				t.Errorf("vector digest %q is not a digest", digest)
			}
			line, err := Line(in)
			if err != nil || !bytes.Equal(line, append(append([]byte(nil), want...), '\n')) {
				t.Errorf("Line = %q, %v", line, err)
			}
			sameMeaning(t, in, got)
		})
	}
}

// sameMeaning checks with the standard library that canonicalizing changed
// no value: both documents decode to the same tree.
func sameMeaning(t *testing.T, a, b []byte) {
	t.Helper()
	decode := func(data []byte) any {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("the standard library cannot read %q: %v", data, err)
		}
		return v
	}
	va, vb := decode(a), decode(b)
	// -0 reads as the number "-0" there and "0" in the canonical form.
	if !reflect.DeepEqual(normalizeZero(va), normalizeZero(vb)) {
		t.Errorf("canonicalizing changed the value\n in  %q\n out %q", a, b)
	}
}

func normalizeZero(v any) any {
	switch x := v.(type) {
	case json.Number:
		if x == "-0" {
			return json.Number("0")
		}
	case []any:
		for i := range x {
			x[i] = normalizeZero(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = normalizeZero(x[k])
		}
	}
	return v
}

func TestInvalidVectors(t *testing.T) {
	kinds := map[string]error{
		"syntax":    ErrSyntax,
		"trailing":  ErrTrailing,
		"notwhole":  ErrNotWhole,
		"range":     ErrRange,
		"duplicate": ErrDuplicateKey,
		"utf8":      ErrUTF8,
		"depth":     ErrDepth,
	}
	for _, name := range names(t, "invalid", ".json") {
		kind, _, _ := strings.Cut(name, "--")
		want, ok := kinds[kind]
		if !ok {
			t.Fatalf("vector %q names no known error", name)
		}
		t.Run(name, func(t *testing.T) {
			in := read(t, filepath.Join("testdata", "invalid", name+".json"))
			if _, err := Parse(in); !errors.Is(err, want) {
				t.Fatalf("Parse error = %v; want %v", err, want)
			}
			if out, err := Canonicalize(in); err == nil || out != nil {
				t.Fatalf("Canonicalize = %q, %v", out, err)
			}
			if d, err := RecordDigest(in); err == nil || d != "" {
				t.Fatalf("RecordDigest = %q, %v", d, err)
			}
		})
	}
}

func TestErrorsNameTheOffset(t *testing.T) {
	_, err := Parse([]byte(`{"a":1,"b":x}`))
	if err == nil || !strings.Contains(err.Error(), "offset 11") {
		t.Fatalf("error = %v; want it to name offset 11", err)
	}
}

func TestEncodeGoValues(t *testing.T) {
	v := map[string]any{
		"b":     []any{int(1), int8(-2), int16(3), int32(4), int64(5), uint(6), uint8(7), uint16(8), uint32(9), uint64(10)},
		"a":     nil,
		"c":     true,
		"d":     "x\xe2\x80\xa8y",
		"empty": map[string]any{},
		"list":  []any{},
	}
	got, err := Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"a\":null,\"b\":[1,-2,3,4,5,6,7,8,9,10],\"c\":true,\"d\":\"x\xe2\x80\xa8y\",\"empty\":{},\"list\":[]}"
	if string(got) != want {
		t.Fatalf("Encode\n got %s\nwant %s", got, want)
	}
}

func TestEncodeRefuses(t *testing.T) {
	type named string
	tests := []struct {
		name string
		v    any
		want error
	}{
		{"float", 1.0, ErrUnsupportedType},
		{"float32", float32(2), ErrUnsupportedType},
		{"named string", named("x"), ErrUnsupportedType},
		{"struct", struct{}{}, ErrUnsupportedType},
		{"typed slice", []string{"a"}, ErrUnsupportedType},
		{"typed map", map[string]string{}, ErrUnsupportedType},
		{"raw message", json.RawMessage(`1`), ErrUnsupportedType},
		{"too large", int64(MaxNumber + 1), ErrRange},
		{"too small", int64(-MaxNumber - 1), ErrRange},
		{"min int64", int64(math.MinInt64), ErrRange},
		{"too large unsigned", uint64(MaxNumber + 1), ErrRange},
		{"max uint64", uint64(math.MaxUint64), ErrRange},
		{"invalid UTF-8 value", "a\xffb", ErrUTF8},
		{"invalid UTF-8 key", map[string]any{"a\xff": 1}, ErrUTF8},
		{"invalid UTF-8 deep", []any{map[string]any{"k": []any{"\xc3"}}}, ErrUTF8},
		{"unsupported deep", map[string]any{"k": []any{1.5}}, ErrUnsupportedType},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if out, err := Encode(tc.v); !errors.Is(err, tc.want) || out != nil {
				t.Fatalf("Encode = %q, %v; want %v", out, err, tc.want)
			}
		})
	}
	var deep any = []any{}
	for i := 0; i < MaxDepth; i++ {
		deep = []any{deep}
	}
	if _, err := Encode(deep); !errors.Is(err, ErrDepth) {
		t.Errorf("deep value: %v; want ErrDepth", err)
	}
}

func TestWholeNumberBounds(t *testing.T) {
	for in, want := range map[string]string{
		"9007199254740991":  "9007199254740991",
		"-9007199254740991": "-9007199254740991",
		"-0":                "0",
		"0":                 "0",
		"10":                "10",
	} {
		got, err := Canonicalize([]byte(in))
		if err != nil || string(got) != want {
			t.Errorf("Canonicalize(%s) = %q, %v; want %s", in, got, err, want)
		}
	}
	v, err := Parse([]byte("[9007199254740991]"))
	if err != nil || v.([]any)[0] != int64(MaxNumber) {
		t.Fatalf("Parse = %v, %v", v, err)
	}
}

func TestTextKeptExactly(t *testing.T) {
	// Normalization forms, padding and a leading mark inside text all
	// survive: the two spellings of \xc3\xa9 stay two different strings.
	in := "{\"a\":\"caf\xc3\xa9\",\"b\":\"cafe\xcc\x81\",\"c\":\" \xc2\xa0 \",\"d\":\"\xef\xbb\xbf\"}"
	v, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["a"] == m["b"] {
		t.Fatal("two normalization forms became one string")
	}
	if m["a"] != "caf\xc3\xa9" || m["b"] != "cafe\xcc\x81" || m["c"] != " \xc2\xa0 " || m["d"] != "\xef\xbb\xbf" {
		t.Fatalf("text changed: %q", m)
	}
	out, _ := Encode(v)
	if string(out) != in {
		t.Fatalf("Encode = %q; want %q", out, in)
	}
}

func TestDigests(t *testing.T) {
	for in, want := range map[string]string{
		"":    "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"abc": "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
	} {
		if got := FileDigest([]byte(in)); got != want {
			t.Errorf("FileDigest(%q) = %s; want %s", in, got, want)
		}
	}
	// The digest of a record ignores its spacing and the order of its
	// members, and the digest of a file does not.
	a := []byte("{ \"b\": 1, \"a\": [2] }\n")
	b := []byte(`{"a":[2],"b":1}`)
	da, _ := RecordDigest(a)
	db, _ := RecordDigest(b)
	if da != db || da != FileDigest(b) {
		t.Errorf("record digests differ: %s %s %s", da, db, FileDigest(b))
	}
	if FileDigest(a) == FileDigest(b) {
		t.Error("file digests of different bytes are equal")
	}
}

func TestValidDigest(t *testing.T) {
	good := "sha256:" + strings.Repeat("0123456789abcdef", 4)
	if !ValidDigest(good) {
		t.Fatalf("ValidDigest(%q) = false", good)
	}
	for _, s := range []string{
		"",
		"sha256:",
		good[:len(good)-1],
		good + "0",
		strings.ToUpper(good),
		"SHA256:" + strings.Repeat("0", 64),
		"sha512:" + strings.Repeat("0", 64),
		"sha256:" + strings.Repeat("g", 64),
		"sha256:" + strings.Repeat("A", 64),
		"sha256: " + strings.Repeat("0", 63),
		strings.Repeat("0", 64),
	} {
		if ValidDigest(s) {
			t.Errorf("ValidDigest(%q) = true", s)
		}
	}
	re := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	if !re.MatchString(FileDigest(nil)) {
		t.Error("FileDigest does not match the schema pattern")
	}
}

// The digest pattern of the common schema is the form this package writes.
func TestDigestAgreesWithCommonSchema(t *testing.T) {
	data, err := spec.Schemas.ReadFile("schemas/common.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Pattern string `json:"pattern"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	re, err := regexp.Compile(doc.Defs["digest"].Pattern)
	if err != nil || doc.Defs["digest"].Pattern == "" {
		t.Fatalf("no digest pattern: %v", err)
	}
	if d := FileDigest([]byte("x")); !re.MatchString(d) || !ValidDigest(d) {
		t.Errorf("%q does not match %s", d, re)
	}
	for _, s := range []string{"sha256:" + strings.Repeat("A", 64), "sha256:00"} {
		if re.MatchString(s) || ValidDigest(s) {
			t.Errorf("%q is accepted", s)
		}
	}
}

func TestCheckFile(t *testing.T) {
	for _, in := range []string{"{}\n", "{\"a\":1}\n", "[1, 2]\n", "\"x\"\n", "null\n", "{ }\n"} {
		if err := CheckFile([]byte(in)); err != nil {
			t.Errorf("CheckFile(%q) = %v", in, err)
		}
	}
	for in, want := range map[string]error{
		"":                   ErrFileForm,
		"\n":                 ErrFileForm,
		"{}":                 ErrFileForm,
		"{}\n\n":             ErrFileForm,
		"{}\r\n":             nil,
		"\xef\xbb\xbf{}\n":   ErrFileForm,
		"{\"a\":1.5}\n":      ErrNotWhole,
		"{\"a\":1}\n{}\n":    ErrTrailing,
		"{\"a\":\"\xff\"}\n": ErrUTF8,
	} {
		err := CheckFile([]byte(in))
		switch {
		case want == nil && err != nil:
			t.Errorf("CheckFile(%q) = %v; want nil", in, err)
		case want != nil && !errors.Is(err, want):
			t.Errorf("CheckFile(%q) = %v; want %v", in, err, want)
		}
	}
}

func TestIsCanonical(t *testing.T) {
	for in, want := range map[string]bool{
		`{"a":1,"b":[true,null]}`: true,
		`{"b":1,"a":2}`:           false,
		`{"a": 1}`:                false,
		"{\"a\":1}\n":             false,
		"\"" + "\\" + "u0041\"":   false,
		`{"a":1.5}`:               false,
		``:                        false,
	} {
		if got := IsCanonical([]byte(in)); got != want {
			t.Errorf("IsCanonical(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestUTF16Order(t *testing.T) {
	ordered := []string{"", "\r", "1", "a", "ab", "\u0080", "\xc3\xb6", "\xe2\x82\xac", "\U00010000", "\U0001f600", "\xee\x80\x80", "\xef\xac\xb3", "\xef\xbf\xbf"}
	// The U+10000 and U+1F600 entries sort before U+E000 in UTF-16, which
	// byte order would reverse.
	for i := range ordered {
		for j := range ordered {
			if got, want := lessUTF16(ordered[i], ordered[j]), i < j; got != want {
				t.Errorf("lessUTF16(%q, %q) = %v; want %v", ordered[i], ordered[j], got, want)
			}
		}
	}
}

// FuzzCanonicalize holds the properties that must be true of any input: a
// document that canonicalizes does so to something that parses to the same
// value, canonicalizes to itself, and has the digest of its own bytes.
func FuzzCanonicalize(f *testing.F) {
	for _, name := range names(f, "valid", ".json") {
		f.Add(read(f, filepath.Join("testdata", "valid", name+".json")))
	}
	for _, name := range names(f, "invalid", ".json") {
		f.Add(read(f, filepath.Join("testdata", "invalid", name+".json")))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		out, err := Canonicalize(in)
		if err != nil {
			return
		}
		again, err := Canonicalize(out)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("not idempotent: %q -> %q -> %q, %v", in, out, again, err)
		}
		v1, _ := Parse(in)
		v2, err := Parse(out)
		if err != nil || !reflect.DeepEqual(v1, v2) {
			t.Fatalf("canonical form changed the value: %q -> %q, %v", in, out, err)
		}
		if d, err := RecordDigest(in); err != nil || d != FileDigest(out) {
			t.Fatalf("RecordDigest = %q, %v", d, err)
		}
		sameMeaningFuzz(t, in, out)
	})
}

func sameMeaningFuzz(t *testing.T, a, b []byte) {
	t.Helper()
	var va, vb any
	da, db := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
	da.UseNumber()
	db.UseNumber()
	if err := da.Decode(&va); err != nil {
		return // the standard library refuses what a strict parser accepted only in odd corners
	}
	if err := db.Decode(&vb); err != nil {
		t.Fatalf("standard library cannot read canonical form %q: %v", b, err)
	}
	if !reflect.DeepEqual(normalizeZero(va), normalizeZero(vb)) {
		// The standard library replaces invalid UTF-8; the strict parser refuses it first.
		t.Fatalf("meaning changed: %q -> %q", a, b)
	}
}
