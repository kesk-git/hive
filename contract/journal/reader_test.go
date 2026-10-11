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
	"github.com/kesk-git/hive/contract/store"
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

// A reader that has consumed the lines of a segment goes on when ordinary
// retention removes that segment and the next one remains; one whose unread
// lines are removed is told so.
func TestReaderWhileRetentionRemoves(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 1)
	append_(t, j, events(t, "pool", 1, 3)...)
	r, err := j.Reader(0)
	if err != nil {
		t.Fatal(err)
	}
	if e, err := r.Next(); err != nil || e == nil || e.HomeSequence != 1 {
		t.Fatalf("Next = %+v, %v", e, err)
	}
	if err := os.Remove(filepath.Join(home, journalDir, SegmentName(1))); err != nil {
		t.Fatal(err)
	}
	for want := int64(2); want <= 3; want++ {
		e, err := r.Next()
		if err != nil || e == nil || e.HomeSequence != want {
			t.Fatalf("after the removal, Next = %+v, %v; want %d", e, err, want)
		}
	}
	if e, err := r.Next(); e != nil || err != nil {
		t.Fatalf("at the end: %+v, %v", e, err)
	}

	// Lines appended to a segment the reader had reached the end of, and
	// removed with it before it read them, are gone.
	home = newHome(t, false)
	wide, _ := open(t, home, "pool", 0)
	append_(t, wide, event(t, "pool", 1))
	r, _ = wide.Reader(0)
	if e, err := r.Next(); err != nil || e == nil {
		t.Fatalf("Next = %+v, %v", e, err)
	}
	append_(t, wide, event(t, "pool", 2))
	narrow, _ := open(t, home, "pool", 1)
	append_(t, narrow, events(t, "pool", 3, 4)...)
	if err := os.Remove(filepath.Join(home, journalDir, SegmentName(1))); err != nil {
		t.Fatal(err)
	}
	_, err = r.Next()
	wantErr(t, "unread lines removed", err, ErrGone)
}

// sourced is the n-th test event of shadow, from the source sourceID with
// source_sequence seq (none when 0), that occurred at occurred.
func sourced(t testing.TB, n int, sourceID string, seq int64, occurred string) store.Event {
	t.Helper()
	m := envelope("shadow", n)
	source := map[string]any{"package": "shadow", "source_id": sourceID}
	if seq != 0 {
		source["source_sequence"] = seq
	}
	m["source"] = source
	m["occurred_at"] = occurred
	m["observed_at"] = "2026-10-11T07:59:50.000Z"
	return encoded(t, m)
}

// late lists which of the entries are marked late, by home_sequence.
func late(entries []*Entry) []int64 {
	var out []int64
	for _, e := range entries {
		if e.Late {
			out = append(out, e.HomeSequence)
		}
	}
	return out
}

// An event recorded after a later event of the same source was recorded is
// late (spec/events.md, The three times): later by source_sequence where
// both carry one, otherwise by occurred_at. A late line is returned as it was
// recorded, in the place it was recorded.
func TestReaderMarksLateEvents(t *testing.T) {
	const gh, other = "shadow:github:o/r", "shadow"
	cases := []struct {
		name string
		evs  []store.Event
		want []int64
	}{
		{"a lower source_sequence after a higher, earlier in time", []store.Event{
			sourced(t, 1, gh, 2, "2026-10-11T07:58:02.000Z"),
			sourced(t, 2, gh, 1, "2026-10-11T07:58:01.000Z"),
		}, []int64{2}},
		{"a source_sequence that decides against the time", []store.Event{
			sourced(t, 1, gh, 2, "2026-10-11T07:58:00.000Z"),
			sourced(t, 2, gh, 3, "2026-10-11T07:57:00.000Z"),
			sourced(t, 3, gh, 1, "2026-10-11T07:59:00.000Z"),
		}, []int64{3}},
		{"in order", []store.Event{
			sourced(t, 1, gh, 1, "2026-10-11T07:58:01.000Z"),
			sourced(t, 2, gh, 2, "2026-10-11T07:58:02.000Z"),
			sourced(t, 3, gh, 2, "2026-10-11T07:58:02.000Z"),
		}, nil},
		{"no source_sequence: an earlier time after a later", []store.Event{
			sourced(t, 1, gh, 0, "2026-10-11T07:58:02.000Z"),
			sourced(t, 2, gh, 0, "2026-10-11T07:58:01.000Z"),
			sourced(t, 3, gh, 0, "2026-10-11T07:58:03.000Z"),
			sourced(t, 4, gh, 0, "2026-10-11T07:58:03.000Z"),
		}, []int64{2}},
		{"one carries a source_sequence and the other does not", []store.Event{
			sourced(t, 1, gh, 5, "2026-10-11T07:58:02.000Z"),
			sourced(t, 2, gh, 0, "2026-10-11T07:58:01.000Z"),
			sourced(t, 3, gh, 0, "2026-10-11T07:58:03.000Z"),
		}, []int64{2}},
		{"two sources interleaved", []store.Event{
			sourced(t, 1, gh, 2, "2026-10-11T07:58:02.000Z"),
			sourced(t, 2, other, 1, "2026-10-11T07:58:00.000Z"),
			sourced(t, 3, gh, 1, "2026-10-11T07:58:01.000Z"),
			sourced(t, 4, other, 2, "2026-10-11T07:58:01.000Z"),
			sourced(t, 5, other, 0, "2026-10-11T07:57:00.000Z"),
		}, []int64{3, 5}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := newHome(t, false)
			j, _ := open(t, home, "shadow", 0)
			append_(t, j, c.evs...)
			got := readAll(t, j, 0)
			if len(got) != len(c.evs) {
				t.Fatalf("read %d entries, appended %d", len(got), len(c.evs))
			}
			if l := late(got); !slices.Equal(l, c.want) {
				t.Errorf("late = %v, want %v", l, c.want)
			}
			// Nothing is dropped, re-dated or reordered by lateness.
			for i, e := range got {
				if e.HomeSequence != int64(i+1) || e.EventID != id(ids.Event, i+1) {
					t.Errorf("entry %d is %d %s", i+1, e.HomeSequence, e.EventID)
				}
				ev := parsed(t, c.evs[i].Content)
				ev["home_sequence"], ev["recorded_at"] = e.Event["home_sequence"], e.Event["recorded_at"]
				want, _ := canon.Encode(ev)
				if !bytes.Equal(e.Line, want) {
					t.Errorf("entry %d was changed:\n%s\n%s", i+1, e.Line, want)
				}
			}
		})
	}
}

// A reader that resumes after a cursor knows what the lines before it said,
// so a line late against one the consumer had already handled is late; the
// cursor remains the last home_sequence handled.
func TestReaderLatenessAcrossResume(t *testing.T) {
	const gh = "shadow:github:o/r"
	for _, size := range []int64{0, 1} { // one segment, then one line each
		home := newHome(t, false)
		j, _ := open(t, home, "shadow", size)
		append_(t, j,
			sourced(t, 1, gh, 2, "2026-10-11T07:58:02.000Z"),
			sourced(t, 2, "shadow", 1, "2026-10-11T07:58:00.000Z"),
			sourced(t, 3, gh, 1, "2026-10-11T07:58:01.000Z"),
			sourced(t, 4, gh, 3, "2026-10-11T07:58:03.000Z"),
			sourced(t, 5, gh, 2, "2026-10-11T07:58:02.000Z"),
		)
		if l := late(readAll(t, j, 0)); !slices.Equal(l, []int64{3, 5}) {
			t.Fatalf("size %d: late from the start = %v", size, l)
		}
		for cursor, want := range map[int64][]int64{1: {3, 5}, 2: {3, 5}, 3: {5}, 4: {5}, 5: nil, 9: nil} {
			r, err := j.Reader(cursor)
			if err != nil {
				t.Fatal(err)
			}
			var got []*Entry
			for {
				e, err := r.Next()
				if err != nil {
					t.Fatal(err)
				}
				if e == nil {
					break
				}
				if e.HomeSequence <= cursor {
					t.Errorf("size %d, cursor %d: returned %d", size, cursor, e.HomeSequence)
				}
				if r.Cursor() != e.HomeSequence {
					t.Errorf("Cursor = %d after %d", r.Cursor(), e.HomeSequence)
				}
				got = append(got, e)
			}
			if l := late(got); !slices.Equal(l, want) {
				t.Errorf("size %d, cursor %d: late = %v, want %v", size, cursor, l, want)
			}
			if cursor == 9 && r.Cursor() != 9 {
				t.Errorf("Cursor = %d beyond the head", r.Cursor())
			}
		}
		// A reader that goes on as lines are appended keeps its marks.
		r, _ := j.Reader(5)
		if e, err := r.Next(); e != nil || err != nil {
			t.Fatalf("at the end: %+v, %v", e, err)
		}
		append_(t, j, sourced(t, 6, gh, 1, "2026-10-11T07:58:00.000Z"), sourced(t, 7, gh, 4, "2026-10-11T07:58:04.000Z"))
		e6, _ := r.Next()
		e7, _ := r.Next()
		if e6 == nil || e7 == nil || !e6.Late || e7.Late {
			t.Errorf("size %d: after appending, %+v %+v", size, e6, e7)
		}
	}
}

// A reader never takes a torn tail for a line, nor a writer repairing it for a
// fault: while writers tear and repair the tail of the segment, a reader and
// Head see whole lines and no error.
func TestReaderWhileTheTailIsRepaired(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 0)
	writer, _ := open(t, home, "pool", 0)
	append_(t, j, event(t, "pool", 1))
	path := filepath.Join(home, journalDir, SegmentName(1))

	done := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		defer close(done)
		for n := 2; n < 60; n++ {
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, fileMode)
			if err != nil {
				errs <- err
				return
			}
			_, err = f.Write(bytes.Repeat([]byte("x"), 40+n))
			f.Close()
			if err != nil {
				errs <- err
				return
			}
			if err := writer.Append([]store.Event{event(t, "pool", n)}); err != nil {
				errs <- err
				return
			}
		}
	}()
	r, _ := j.Reader(0)
	for stop := false; !stop; {
		select {
		case <-done:
			stop = true
		default:
		}
		for {
			e, err := r.Next()
			if err != nil {
				t.Fatalf("Next: %v", err)
			}
			if e == nil {
				break
			}
		}
		if _, err := j.Head(); err != nil {
			t.Fatalf("Head: %v", err)
		}
	}
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
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
