package journal

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/store"
)

// Lines are appended in one order per home: home_sequence runs from 1 and
// rises by exactly one with each line across appends, each line is one event
// in canonical form with its line feed, and the journal sets home_sequence
// and recorded_at.
func TestAppendSequence(t *testing.T) {
	home := newHome(t, false)
	j, clk := open(t, home, "pool", 0)
	if head, err := j.Head(); err != nil || head != 0 {
		t.Fatalf("Head of an empty journal = %d, %v", head, err)
	}
	append_(t, j, event(t, "pool", 1))
	clk.Advance(time.Second)
	append_(t, j, events(t, "pool", 2, 3)...)
	clk.Advance(time.Second)
	append_(t, j, event(t, "pool", 4))

	first := SegmentName(1)
	if first != "events-00000000000000000001.jsonl" {
		t.Fatalf("SegmentName(1) = %q", first)
	}
	if got := segmentFiles(t, home); !slices.Equal(got, []string{first}) {
		t.Fatalf("segments = %v", got)
	}
	data := segmentBytes(t, home, first)
	whole, torn := lines(data)
	if len(torn) != 0 || len(whole) != 4 {
		t.Fatalf("%d whole lines and a tail of %d bytes", len(whole), len(torn))
	}
	stamps := []string{"2026-10-11T08:00:00.000Z", "2026-10-11T08:00:01.000Z", "2026-10-11T08:00:01.000Z", "2026-10-11T08:00:02.000Z"}
	for i, l := range whole {
		if !canon.IsCanonical(l) {
			t.Errorf("line %d is not canonical: %q", i+1, l)
		}
		obj := parsed(t, l)
		if seq, _ := obj["home_sequence"].(int64); seq != int64(i+1) {
			t.Errorf("line %d has home_sequence %v", i+1, obj["home_sequence"])
		}
		if obj["event_id"] != id(ids.Event, i+1) {
			t.Errorf("line %d is event %v", i+1, obj["event_id"])
		}
		if obj["recorded_at"] != stamps[i] {
			t.Errorf("line %d recorded_at = %v, want %s", i+1, obj["recorded_at"], stamps[i])
		}
		// The rest of the envelope is as the caller gave it.
		want := envelope("pool", i+1)
		for k, v := range want {
			if k == "source" || k == "basis" || k == "body" {
				continue
			}
			if obj[k] != v {
				t.Errorf("line %d: %s = %v, want %v", i+1, k, obj[k], v)
			}
		}
	}
	if head, err := j.Head(); err != nil || head != 4 {
		t.Fatalf("Head = %d, %v", head, err)
	}
	// The journal's files are the owner's only, and the lock is where the
	// specification puts it.
	for _, p := range []string{filepath.Join(home, journalDir), filepath.Join(home, journalDir, first)} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s is open beyond its owner: %v", p, info.Mode())
		}
	}
	if _, err := os.Stat(filepath.Join(home, "locks", "journal.lock")); err != nil {
		t.Errorf("locks/journal.lock: %v", err)
	}
}

// A segment is closed when the next line would take it past the configured
// size, and the next segment is named for that line's home_sequence. A line
// larger than the size stands alone.
func TestSegments(t *testing.T) {
	home := newHome(t, false)
	probe, _ := open(t, home, "pool", 0)
	append_(t, probe, event(t, "pool", 1))
	one := int64(len(segmentBytes(t, home, SegmentName(1))))
	if err := os.RemoveAll(filepath.Join(home, journalDir)); err != nil {
		t.Fatal(err)
	}

	// Two lines fit in a segment; a third does not.
	j, _ := open(t, home, "pool", 2*one+one/2)
	append_(t, j, events(t, "pool", 1, 5)...)
	want := []string{SegmentName(1), SegmentName(3), SegmentName(5)}
	if got := segmentFiles(t, home); !slices.Equal(got, want) {
		t.Fatalf("segments = %v, want %v", got, want)
	}
	for i, name := range want {
		seqs := sequences(t, segmentBytes(t, home, name))
		var wantSeqs []int64
		for s := int64(2*i + 1); s <= int64(2*i+2) && s <= 5; s++ {
			wantSeqs = append(wantSeqs, s)
		}
		if !slices.Equal(seqs, wantSeqs) {
			t.Errorf("%s holds %v, want %v", name, seqs, wantSeqs)
		}
	}
	// A line larger than a segment goes alone into a segment of its own, and
	// the line after it begins another.
	big := envelope("pool", 6)
	big["kind"] = "pool." + strings.Repeat("x", int(3*one)) + ".happened"
	append_(t, j, encoded(t, big), event(t, "pool", 7))
	want = append(want, SegmentName(6), SegmentName(7))
	if got := segmentFiles(t, home); !slices.Equal(got, want) {
		t.Fatalf("segments = %v, want %v", got, want)
	}
	if seqs := sequences(t, segmentBytes(t, home, SegmentName(6))); !slices.Equal(seqs, []int64{6}) {
		t.Errorf("the big line's segment holds %v", seqs)
	}
	// Closed segments are not written again: a new journal continues in
	// the newest one.
	again, _ := open(t, home, "pool", 2*one+one/2)
	append_(t, again, event(t, "pool", 8))
	if seqs := sequences(t, segmentBytes(t, home, SegmentName(7))); !slices.Equal(seqs, []int64{7, 8}) {
		t.Errorf("the newest segment holds %v", seqs)
	}
	if head, err := again.Head(); err != nil || head != 8 {
		t.Fatalf("Head = %d, %v", head, err)
	}
	// The order read back is the order written, across segments.
	var got []int64
	for _, e := range readAll(t, again, 0) {
		got = append(got, e.HomeSequence)
	}
	if !slices.Equal(got, []int64{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Errorf("read %v", got)
	}
}

// Recovery appends an event only if no line with its event_id is present: an
// event already in the journal, in any retained segment, is skipped, and the
// rest are appended in order.
func TestRecoverAppendsOnce(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 0)
	append_(t, j, events(t, "pool", 1, 2)...)
	recover_(t, j, events(t, "pool", 1, 3)...)
	recover_(t, j, event(t, "pool", 2))
	if seqs := sequences(t, segmentBytes(t, home, SegmentName(1))); !slices.Equal(seqs, []int64{1, 2, 3}) {
		t.Fatalf("lines %v", seqs)
	}
	var got []string
	for _, e := range readAll(t, j, 0) {
		got = append(got, e.EventID)
	}
	if want := []string{id(ids.Event, 1), id(ids.Event, 2), id(ids.Event, 3)}; !slices.Equal(got, want) {
		t.Errorf("events %v", got)
	}
	// Twice in one append is a caller's mistake, not a duplicate to skip.
	for _, f := range []func([]store.Event) error{j.Append, j.Recover} {
		err := f([]store.Event{event(t, "pool", 4), event(t, "pool", 4)})
		wantErr(t, "the same event twice in one append", err, ErrEvent)
	}
	if head, _ := j.Head(); head != 3 {
		t.Errorf("Head = %d after a refused append", head)
	}
	// Presence is found across segments too.
	small, _ := open(t, home, "pool", 1)
	append_(t, small, events(t, "pool", 4, 6)...)
	recover_(t, small, events(t, "pool", 1, 7)...)
	if head, _ := small.Head(); head != 7 {
		t.Errorf("Head = %d, want 7", head)
	}
	if seqs := sequences(t, segmentBytes(t, home, SegmentName(7))); !slices.Equal(seqs, []int64{7}) {
		t.Errorf("the last segment holds %v", seqs)
	}
}

// closeSegments makes the segments but the newest unreadable, as a segment
// the journal must not read, and skips the test where the file system does
// not enforce it.
func closeSegments(t *testing.T, home string) {
	t.Helper()
	files := segmentFiles(t, home)
	for _, name := range files[:len(files)-1] {
		path := filepath.Join(home, journalDir, name)
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(path, fileMode) })
		if f, err := os.Open(path); err == nil {
			f.Close()
			t.Skip("the file system does not keep the owner from reading a file with no access")
		}
	}
}

// The first attempt of a transaction does no presence scan: it reads the tail
// of the newest segment and writes, however much history is kept. Only
// recovery reads the closed segments.
func TestAppendReadsNoClosedSegment(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 1) // one line per segment
	append_(t, j, events(t, "pool", 1, 3)...)
	closeSegments(t, home)

	append_(t, j, events(t, "pool", 4, 5)...)
	if head, err := j.Head(); err != nil || head != 5 {
		t.Fatalf("Head = %d, %v", head, err)
	}
	// Recovery of the events it holds must read them to find them.
	if err := j.Recover(events(t, "pool", 1, 1)); err == nil {
		t.Error("recovery found an event in a segment it cannot read")
	}
	// An event of the newest segment is found without reading the closed ones.
	recover_(t, j, event(t, "pool", 5))
	if head, _ := j.Head(); head != 5 {
		t.Errorf("Head = %d after recovering an event that was there", head)
	}
}

// holds finds an event by its own event_id, and not by an event_id that the
// body of another line mentions.
func TestHolds(t *testing.T) {
	a, b := id(ids.Event, 1), id(ids.Event, 2)
	data := []byte(`{"body":{"event_id":"` + b + `"},"event_id":"` + a + `"}` + "\n" +
		`{"event_id":"` + a + `","x":1}` + "\n")
	if !holds(data, a) {
		t.Error("a is there and was not found")
	}
	if holds(data, b) {
		t.Error("b is only mentioned by a body and was found")
	}
	if holds(data, id(ids.Event, 3)) {
		t.Error("an absent event was found")
	}
	torn := []byte(`{"event_id":"` + b + `"`)
	if holds(torn, b) {
		t.Error("a torn line counts as present")
	}
	if !holds(append(torn, "}\n"...), b) {
		t.Error("a whole one-line file was not searched")
	}
}

// The store appends the events of a transaction through the journal, holding
// its own lock first and the journal's second, and the lines read back are
// the events the transaction named.
func TestStoreAppendsThroughTheJournal(t *testing.T) {
	home := newHome(t, false)
	j, clk := open(t, home, "pool", 0)
	s, err := store.Open(home, "pool", store.Options{Clock: clk, Journal: j, Waiting: waiting(clk)})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Release()
	tx := store.Tx{
		RequestID: id(ids.Request, 1),
		Records:   []store.RecordWrite{{Path: store.RecordPath("pool", "item", "a"), Content: []byte(`{"schema":"pool.item","schema_version":"1.0"}`)}},
		Events:    events(t, "pool", 1, 2),
		Outcome:   []byte(`{"status":"ok"}`),
	}
	if _, err := w.Commit(tx); err != nil {
		t.Fatal(err)
	}
	got := readAll(t, j, 0)
	if len(got) != 2 || got[0].EventID != id(ids.Event, 1) || got[1].EventID != id(ids.Event, 2) {
		t.Fatalf("read %d entries", len(got))
	}
	if got[1].HomeSequence != 2 || got[1].Kind != "pool.thing.happened" || got[1].Segment != SegmentName(1) {
		t.Errorf("entry = %+v", got[1])
	}
	// The store hands the journal an event of another package's kind only
	// by mistake, and the journal refuses it for the store.
	tx.RequestID = id(ids.Request, 2)
	tx.Records[0].Expected = 1
	tx.Events = []store.Event{event(t, "dispatch", 3)}
	_, err = w.Commit(tx)
	wantErr(t, "another package's kind through the store", err, ErrNotOwner)
}

// Two commands contend for the journal: the second waits the bounded time
// and stops with lock_held, appending nothing, then appends once the first
// lets go.
func TestAppendWaitsForTheLock(t *testing.T) {
	home := newHome(t, false)
	j, clk := open(t, home, "pool", 0)
	held, err := store.Acquire(j.Dir, "journal", false, waiting(clk))
	if err != nil {
		t.Fatal(err)
	}
	start := clk.Now()
	err = j.Append(events(t, "pool", 1, 1))
	wantErr(t, "append against a held lock", err, store.ErrLockHeld)
	if store.Code(err) != "lock_held" {
		t.Errorf("Code = %q", store.Code(err))
	}
	if waited := clk.Now().Sub(start); waited < 100*time.Millisecond || waited > 200*time.Millisecond {
		t.Errorf("waited %v", waited)
	}
	if files := segmentFiles(t, home); len(files) != 0 {
		t.Errorf("a refused append wrote %v", files)
	}
	held.Release()
	append_(t, j, event(t, "pool", 1))
	// The lock is released after an append.
	l, err := store.Acquire(j.Dir, "journal", false, waiting(clk))
	if err != nil {
		t.Fatalf("the lock was kept after Append: %v", err)
	}
	l.Release()
}

// An event that is not the journal's to append, or not whole, is refused
// before anything is written, and the rest of its batch with it.
func TestAppendRefusals(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 0)
	append_(t, j, event(t, "pool", 1))
	before := segmentBytes(t, home, SegmentName(1))

	alter := func(f func(m map[string]any)) store.Event {
		m := envelope("pool", 2)
		f(m)
		return encoded(t, m)
	}
	cases := []struct {
		name string
		ev   store.Event
		want error
	}{
		{"another package's kind", event(t, "dispatch", 2), ErrNotOwner},
		{"the journal's own kind", alter(func(m map[string]any) {
			m["kind"] = RepairKind
			m["source"] = map[string]any{"package": "pool", "source_id": "journal"}
		}), ErrNotOwner},
		{"a kind of no package", alter(func(m map[string]any) { m["kind"] = "thing.happened" }), ErrNotOwner},
		{"event_id differs from the content's", store.Event{ID: id(ids.Event, 3), Content: event(t, "pool", 2).Content}, ErrEvent},
		{"an identifier of another kind", store.Event{ID: id(ids.Receipt, 2), Content: event(t, "pool", 2).Content}, ErrEvent},
		{"home_sequence given", alter(func(m map[string]any) { m["home_sequence"] = int64(2) }), ErrEvent},
		{"recorded_at given", alter(func(m map[string]any) { m["recorded_at"] = "2026-10-11T08:00:00.000Z" }), ErrEvent},
		{"another home", alter(func(m map[string]any) { m["home_id"] = id(ids.Home, 9) }), ErrEvent},
		{"no basis", alter(func(m map[string]any) { delete(m, "basis") }), ErrEvent},
		{"a member the envelope does not have", alter(func(m map[string]any) { m["note"] = "x" }), ErrEvent},
		{"source.package is another package", alter(func(m map[string]any) { m["source"] = map[string]any{"package": "shadow", "source_id": "shadow"} }), ErrEvent},
		{"a source of another package", alter(func(m map[string]any) { m["source"] = map[string]any{"package": "pool", "source_id": "shadow"} }), ErrEvent},
		{"a source of another package with parts", alter(func(m map[string]any) {
			m["source"] = map[string]any{"package": "pool", "source_id": "shadow:github:o/r"}
		}), ErrEvent},
		{"the journal's own source", alter(func(m map[string]any) { m["source"] = map[string]any{"package": "pool", "source_id": "journal"} }), ErrEvent},
		{"a body for a kind that registers none", alter(func(m map[string]any) { m["body"] = map[string]any{"x": int64(1)} }), ErrEvent},
		{"a hive scope with a workspace", alter(func(m map[string]any) { m["workspace_id"] = id(ids.Workspace, 1) }), ErrEvent},
		{"a generation without a task", alter(func(m map[string]any) { m["generation"] = int64(1) }), ErrEvent},
		{"an unknown occurred_at that differs from observed_at", alter(func(m map[string]any) { m["occurred_at_known"] = false }), ErrEvent},
		{"a time that is not an instant", alter(func(m map[string]any) { m["observed_at"] = "2026-13-11T08:00:00.000Z" }), ErrEvent},
		{"a newer envelope version", alter(func(m map[string]any) { m["schema_version"] = "1.1" }), ErrEvent},
		{"not a JSON object", store.Event{ID: id(ids.Event, 2), Content: []byte(`[1]`)}, ErrEvent},
		{"not JSON", store.Event{ID: id(ids.Event, 2), Content: []byte(`{"event_id":`)}, ErrEvent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := j.Append([]store.Event{event(t, "pool", 3), c.ev})
			wantErr(t, c.name, err, c.want)
			if after := segmentBytes(t, home, SegmentName(1)); !bytes.Equal(after, before) {
				t.Errorf("the refused append wrote something")
			}
		})
	}
	// A source the package observes is its own.
	append_(t, j, encoded(t, func() map[string]any {
		m := envelope("pool", 4)
		m["source"] = map[string]any{"package": "pool", "source_id": "pool:feed:x/y", "source_sequence": int64(1)}
		return m
	}()))
	before = segmentBytes(t, home, SegmentName(1))
	// A journal opened to read only cannot append.
	reader, err := Open(home, Options{})
	if err != nil {
		t.Fatal(err)
	}
	wantErr(t, "no package", reader.Append(events(t, "pool", 2, 2)), ErrNoPackage)
	if _, err := Open(home, Options{Package: "contract"}); err == nil {
		t.Error("Open accepted a package that is not one of the seven")
	}
	// Nothing to append is nothing done.
	if err := j.Append(nil); err != nil {
		t.Errorf("an empty append: %v", err)
	}
	if after := segmentBytes(t, home, SegmentName(1)); !bytes.Equal(after, before) {
		t.Error("an empty append wrote something")
	}
}

// A last line the journal cannot read is never repaired by guessing: the
// append stops with record_malformed and writes nothing, and a line of a
// newer envelope version is unsupported_version.
func TestAppendStopsAtAMalformedTail(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 0)
	append_(t, j, event(t, "pool", 1))
	good := segmentBytes(t, home, SegmentName(1))

	cases := []struct {
		name string
		tail []byte
		want error
		code string
	}{
		{"not JSON", []byte("not an event\n"), store.ErrRecordMalformed, "record_malformed"},
		{"not canonical", []byte(`{ "schema": "hive.event" }` + "\n"), store.ErrRecordMalformed, "record_malformed"},
		{"not an event", []byte(`{"schema":"pool.item","schema_version":"1.0"}` + "\n"), store.ErrRecordMalformed, "record_malformed"},
		{"a newer version", func() []byte {
			m := parsed(t, bytes.TrimSuffix(good, []byte("\n")))
			m["schema_version"] = "1.1"
			m["home_sequence"] = int64(2)
			return append(encoded(t, m).Content, '\n')
		}(), store.ErrUnsupportedVersion, "unsupported_version"},
		{"before the segment's first", func() []byte {
			m := parsed(t, bytes.TrimSuffix(good, []byte("\n")))
			m["home_sequence"] = int64(1)
			return append(encoded(t, m).Content, '\n')
		}(), store.ErrRecordMalformed, "record_malformed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name := SegmentName(1)
			data := append(slices.Clone(good), c.tail...)
			if c.name == "before the segment's first" {
				name = SegmentName(2)
				data = c.tail
			}
			writeSegment(t, home, name, data)
			err := j.Append(events(t, "pool", 2, 2))
			wantErr(t, "append", err, c.want)
			if store.Code(err) != c.code {
				t.Errorf("Code = %q, want %s", store.Code(err), c.code)
			}
			if after := segmentBytes(t, home, name); !bytes.Equal(after, data) {
				t.Error("the segment was changed")
			}
			if _, err := j.Head(); !errors.Is(err, c.want) {
				t.Errorf("Head: %v", err)
			}
			if name != SegmentName(1) {
				os.Remove(filepath.Join(home, journalDir, name))
			}
		})
	}
	// An empty newest segment continues from its name only where the segment
	// before it ends just short of it.
	writeSegment(t, home, SegmentName(1), good)
	writeSegment(t, home, SegmentName(3), nil)
	wantErr(t, "a gap before an empty segment", j.Append(events(t, "pool", 2, 2)), store.ErrRecordMalformed)
	os.Remove(filepath.Join(home, journalDir, SegmentName(3)))
	writeSegment(t, home, SegmentName(2), nil)
	append_(t, j, event(t, "pool", 2))
	if seqs := sequences(t, segmentBytes(t, home, SegmentName(2))); !slices.Equal(seqs, []int64{2}) {
		t.Errorf("the empty segment holds %v", seqs)
	}
	// An entry that is not a segment is left alone; one named like a segment
	// that is not a file is not read around.
	if err := os.WriteFile(filepath.Join(home, journalDir, "notes.txt"), []byte("mine\n"), fileMode); err != nil {
		t.Fatal(err)
	}
	append_(t, j, event(t, "pool", 3))
	if err := os.Mkdir(filepath.Join(home, journalDir, SegmentName(9)), dirMode); err != nil {
		t.Fatal(err)
	}
	wantErr(t, "a directory named like a segment", j.Append(events(t, "pool", 4, 4)), store.ErrRecordMalformed)
}

// A segment's name is twenty digits naming a home_sequence; anything else
// named events-...jsonl is not a segment, or is a malformed one.
func TestSegmentNames(t *testing.T) {
	for _, name := range []string{"events-1.jsonl", "events-0000000000000000001.jsonl", "events-000000000000000000001.jsonl", "Events-00000000000000000001.jsonl", "events-0000000000000000000a.jsonl", "events-00000000000000000001.json", "events-00000000000000000001.jsonl.tmp"} {
		if _, isSeg, err := parseSegment(name); isSeg || err != nil {
			t.Errorf("%s: a segment %v, error %v", name, isSeg, err)
		}
	}
	for _, name := range []string{"events-00000000000000000000.jsonl", "events-99999999999999999999.jsonl", "events-09007199254740992.jsonl"} {
		if _, _, err := parseSegment(name); err == nil && len(name) == len(SegmentName(1)) {
			t.Errorf("%s was accepted", name)
		}
	}
	n, isSeg, err := parseSegment(SegmentName(canon.MaxNumber))
	if !isSeg || err != nil || n != canon.MaxNumber {
		t.Errorf("the largest sequence: %d %v %v", n, isSeg, err)
	}
}

// Nothing is recorded under an identity the journal was handed: an append
// reads the home record as it begins, and is refused, writing nothing, while
// the home has none. A journal may be opened before the record exists, and
// appends under it once it does.
func TestAppendNeedsTheHomeRecord(t *testing.T) {
	bare := newHome(t, true)
	j, _ := open(t, bare, "pool", 0)
	if j.HomeID != "" {
		t.Fatalf("HomeID = %q before the home record", j.HomeID)
	}
	for _, f := range []func([]store.Event) error{j.Append, j.Recover} {
		wantErr(t, "no home record", f(events(t, "pool", 1, 1)), ErrNoHome)
	}
	if head, err := j.Head(); err != nil || head != 0 {
		t.Errorf("Head = %d, %v", head, err)
	}
	if files := segmentFiles(t, bare); len(files) != 0 {
		t.Errorf("a refused append wrote %v", files)
	}
	if got := readAll(t, j, 0); len(got) != 0 {
		t.Errorf("read %d entries", len(got))
	}

	// The record arrives after the journal was opened: the journal appends
	// under it, and refuses an event of any other home.
	data, err := os.ReadFile("testdata/home/home.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bare, "home.json"), data, fileMode); err != nil {
		t.Fatal(err)
	}
	other := envelope("pool", 2)
	other["home_id"] = id(ids.Home, 9)
	wantErr(t, "another home", j.Append([]store.Event{encoded(t, other)}), ErrEvent)
	append_(t, j, event(t, "pool", 1))
	got := readAll(t, j, 0)
	if len(got) != 1 || got[0].Event["home_id"] != testHomeID {
		t.Fatalf("read %d entries", len(got))
	}
}
