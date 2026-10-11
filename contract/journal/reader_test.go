package journal

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/schema"
	"github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/spec"
)

// A reader keeps a cursor, the last home_sequence it handled, resumes after
// it across segments, and sees the lines appended after it reached the end.
func TestReaderResumesAfterTheCursor(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 1) // one line per segment
	append_(t, j, events(t, "pool", 1, 5)...)

	var got []int64
	for _, e := range readAll(t, j, 0) {
		got = append(got, e.HomeSequence)
		if e.Segment != SegmentName(e.HomeSequence) || e.EventID != id(ids.Event, int(e.HomeSequence)) || e.Kind != "pool.thing.happened" {
			t.Errorf("entry %+v", e)
		}
		if !canon.IsCanonical(e.Line) || e.Event["event_id"] != e.EventID {
			t.Errorf("entry %d: line or event wrong", e.HomeSequence)
		}
	}
	if !slices.Equal(got, []int64{1, 2, 3, 4, 5}) {
		t.Fatalf("from the start: %v", got)
	}
	got = nil
	for _, e := range readAll(t, j, 3) {
		got = append(got, e.HomeSequence)
	}
	if !slices.Equal(got, []int64{4, 5}) {
		t.Fatalf("after 3: %v", got)
	}
	if entries := readAll(t, j, 5); len(entries) != 0 {
		t.Errorf("after the head: %d entries", len(entries))
	}
	if entries := readAll(t, j, 50); len(entries) != 0 {
		t.Errorf("beyond the head: %d entries", len(entries))
	}

	// The cursor follows what Next returned, and the same Reader goes on
	// when more is appended, within a segment and into a new one.
	wide, _ := open(t, home, "pool", DefaultSegmentSize)
	r, err := j.Reader(2)
	if err != nil {
		t.Fatal(err)
	}
	if r.Cursor() != 2 {
		t.Errorf("Cursor = %d before reading", r.Cursor())
	}
	for want := int64(3); want <= 5; want++ {
		e, err := r.Next()
		if err != nil || e == nil || e.HomeSequence != want || r.Cursor() != want {
			t.Fatalf("Next = %+v, %v; cursor %d; want %d", e, err, r.Cursor(), want)
		}
	}
	if e, err := r.Next(); e != nil || err != nil {
		t.Fatalf("at the end: %+v, %v", e, err)
	}
	append_(t, wide, events(t, "pool", 6, 7)...) // both into segment 5
	append_(t, j, event(t, "pool", 8))           // a new segment
	for want := int64(6); want <= 8; want++ {
		e, err := r.Next()
		if err != nil || e == nil || e.HomeSequence != want {
			t.Fatalf("after appending, Next = %+v, %v; want %d", e, err, want)
		}
	}
	if e, err := r.Next(); e != nil || err != nil || r.Cursor() != 8 {
		t.Fatalf("at the new end: %+v, %v, cursor %d", e, err, r.Cursor())
	}
	if _, err := j.Reader(-1); err == nil {
		t.Error("a negative cursor was accepted")
	}
}

// When the segments before a cursor are no longer kept, a cursor that lies
// before what remains is told so, and one inside what remains reads on.
func TestReaderAfterRetention(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 1)
	append_(t, j, events(t, "pool", 1, 4)...)
	for _, n := range []int64{1, 2} {
		if err := os.Remove(filepath.Join(home, journalDir, SegmentName(n))); err != nil {
			t.Fatal(err)
		}
	}
	for _, cursor := range []int64{0, 1} {
		r, err := j.Reader(cursor)
		if err != nil {
			t.Fatal(err)
		}
		_, err = r.Next()
		wantErr(t, "a cursor before the kept segments", err, ErrGone)
	}
	var got []int64
	for _, e := range readAll(t, j, 2) {
		got = append(got, e.HomeSequence)
	}
	if !slices.Equal(got, []int64{3, 4}) {
		t.Errorf("after 2: %v", got)
	}
	// The writer goes on from the newest segment.
	append_(t, j, event(t, "pool", 5))
	if head, _ := j.Head(); head != 5 {
		t.Errorf("Head = %d", head)
	}
	// An empty journal has nothing to say about any cursor.
	empty, _ := open(t, newHome(t, false), "pool", 0)
	if entries := readAll(t, empty, 7); len(entries) != 0 {
		t.Errorf("an empty journal returned %d entries", len(entries))
	}
}

// The reader validates every line and never guesses: a line it cannot read,
// a gap in the sequence, a closed segment that ends in a torn line and a
// line of a newer envelope version each stop it, naming the fault.
func TestReaderRefusesAMalformedJournal(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 1)
	append_(t, j, events(t, "pool", 1, 3)...)
	good := map[int64][]byte{}
	for n := int64(1); n <= 3; n++ {
		good[n] = segmentBytes(t, home, SegmentName(n))
	}
	restore := func() {
		for n, data := range good {
			writeSegment(t, home, SegmentName(n), data)
		}
		for _, name := range segmentFiles(t, home) {
			if n, _, _ := parseSegment(name); n > 3 {
				os.Remove(filepath.Join(home, journalDir, name))
			}
		}
	}
	altered := func(n int64, f func(m map[string]any)) []byte {
		m := parsed(t, bytes.TrimSuffix(good[n], []byte("\n")))
		f(m)
		return append(encoded(t, m).Content, '\n')
	}
	cases := []struct {
		name   string
		damage func()
		want   error
		before []int64 // the entries read before the fault
	}{
		{"a line that is not JSON", func() { writeSegment(t, home, SegmentName(2), []byte("??\n")) }, store.ErrRecordMalformed, []int64{1}},
		{"a line that is not canonical", func() {
			writeSegment(t, home, SegmentName(2), append([]byte(" "), good[2]...))
		}, store.ErrRecordMalformed, []int64{1}},
		{"a line that fails the envelope", func() {
			writeSegment(t, home, SegmentName(2), altered(2, func(m map[string]any) { delete(m, "basis") }))
		}, store.ErrRecordMalformed, []int64{1}},
		{"a line whose kind is another package's", func() {
			writeSegment(t, home, SegmentName(2), altered(2, func(m map[string]any) { m["kind"] = "shadow.thing.happened" }))
		}, store.ErrRecordMalformed, []int64{1}},
		{"a line with a body its kind does not register", func() {
			writeSegment(t, home, SegmentName(2), altered(2, func(m map[string]any) { m["body"] = map[string]any{"x": int64(1)} }))
		}, store.ErrRecordMalformed, []int64{1}},
		{"a line out of sequence", func() {
			writeSegment(t, home, SegmentName(2), altered(2, func(m map[string]any) { m["home_sequence"] = int64(3) }))
		}, store.ErrRecordMalformed, []int64{1}},
		{"a line of a newer version", func() {
			writeSegment(t, home, SegmentName(2), altered(2, func(m map[string]any) { m["schema_version"] = "1.1" }))
		}, store.ErrUnsupportedVersion, []int64{1}},
		{"a gap between segments", func() { os.Remove(filepath.Join(home, journalDir, SegmentName(2))) }, store.ErrRecordMalformed, []int64{1}},
		{"a closed segment ending in a torn line", func() {
			writeSegment(t, home, SegmentName(2), append(slices.Clone(good[2]), "{\"torn"...))
		}, store.ErrRecordMalformed, []int64{1, 2}},
		{"a segment that begins before its name", func() {
			writeSegment(t, home, SegmentName(2), append(slices.Clone(good[1]), good[2]...))
		}, store.ErrRecordMalformed, []int64{1}},
		{"a segment named like one that is not a file", func() {
			os.Mkdir(filepath.Join(home, journalDir, SegmentName(4)), dirMode)
		}, store.ErrRecordMalformed, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			restore()
			c.damage()
			r, err := j.Reader(0)
			if err != nil {
				t.Fatal(err)
			}
			var got []int64
			for {
				e, err := r.Next()
				if err != nil {
					wantErr(t, c.name, err, c.want)
					break
				}
				if e == nil {
					t.Fatalf("%s: read to the end without a fault: %v", c.name, got)
				}
				got = append(got, e.HomeSequence)
			}
			if !slices.Equal(got, c.before) {
				t.Errorf("%s: read %v before the fault, want %v", c.name, got, c.before)
			}
		})
	}
	// A torn tail in the newest segment is not a fault; a file that is not a
	// segment is left alone.
	restore()
	writeSegment(t, home, SegmentName(3), append(slices.Clone(good[3]), "{\"torn"...))
	os.WriteFile(filepath.Join(home, journalDir, "README"), []byte("not a segment\n"), fileMode)
	var got []int64
	for _, e := range readAll(t, j, 0) {
		got = append(got, e.HomeSequence)
	}
	if !slices.Equal(got, []int64{1, 2, 3}) {
		t.Errorf("read %v", got)
	}
}

// A reader of a line of a newer envelope version refuses it as unsupported
// and does not call it malformed, and a reader of a malformed line does not
// call it unsupported.
func TestReaderErrorsAreDistinct(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 0)
	append_(t, j, event(t, "pool", 1))
	data := segmentBytes(t, home, SegmentName(1))
	m := parsed(t, bytes.TrimSuffix(data, []byte("\n")))
	m["schema_version"] = "2.0"
	writeSegment(t, home, SegmentName(1), append(encoded(t, m).Content, '\n'))
	r, _ := j.Reader(0)
	_, err := r.Next()
	if !errors.Is(err, store.ErrUnsupportedVersion) || errors.Is(err, store.ErrRecordMalformed) {
		t.Errorf("a newer version: %v", err)
	}
	if store.Code(err) != "unsupported_version" {
		t.Errorf("Code = %q", store.Code(err))
	}
	writeSegment(t, home, SegmentName(1), []byte("x\n"))
	r, _ = j.Reader(0)
	_, err = r.Next()
	if errors.Is(err, store.ErrUnsupportedVersion) || !errors.Is(err, store.ErrRecordMalformed) {
		t.Errorf("a malformed line: %v", err)
	}
}

// Packages is the package_name enumeration of the common schema, so that
// the journal and the schemas name the same seven.
func TestPackagesMatchTheSchema(t *testing.T) {
	data, err := spec.Schemas.ReadFile("schemas/common.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := schema.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	defs, _ := doc.(map[string]any)["$defs"].(map[string]any)
	pkg, _ := defs["package_name"].(map[string]any)
	enum, _ := pkg["enum"].([]any)
	var want []string
	for _, v := range enum {
		want = append(want, v.(string))
	}
	if !slices.Equal(Packages, want) {
		t.Errorf("Packages = %v, the schema says %v", Packages, want)
	}
}

// Open refuses what the store's home check refuses, and otherwise creates
// journal/ for the owner only.
func TestOpenChecksTheHome(t *testing.T) {
	home := newHome(t, false)
	if err := os.Chmod(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(home, Options{Package: "pool"}); !errors.Is(err, store.ErrLayout) {
		t.Errorf("an open home: %v", err)
	}
	os.Chmod(home, dirMode)
	j, err := Open(home, Options{Package: "pool"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(home, journalDir))
	if err != nil || !info.IsDir() || info.Mode().Perm() != dirMode {
		t.Errorf("journal/: %v %v", info, err)
	}
	if j.Package() != "pool" || j.HomeID != testHomeID {
		t.Errorf("Package = %q, HomeID = %q", j.Package(), j.HomeID)
	}
	if _, err := Open(filepath.Join(home, "missing"), Options{}); err == nil {
		t.Error("a missing home was opened")
	}
}
