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
)

// tear cuts the last n bytes of a segment's last line, leaving a final line
// without its line feed, as a crash during an append does, and returns the
// torn bytes left behind.
func tear(t testing.TB, home, name string, n int) []byte {
	t.Helper()
	data := segmentBytes(t, home, name)
	cut := data[:len(data)-n]
	writeSegment(t, home, name, cut)
	whole, torn := lines(cut)
	if len(torn) == 0 {
		t.Fatalf("tear left no torn tail: %d whole lines", len(whole))
	}
	return slices.Clone(torn)
}

// checkRepair checks a repair event line against the torn bytes it removed.
func checkRepair(t testing.TB, line []byte, seq int64, segment, pkg, homeID, correlation string, torn []byte) {
	t.Helper()
	obj := parsed(t, line)
	eid, _ := obj["event_id"].(string)
	if err := ids.Check(eid, ids.Event); err != nil {
		t.Errorf("repair event_id: %v", err)
	}
	for k, want := range map[string]any{
		"kind":              RepairKind,
		"kind_version":      "1.0",
		"home_sequence":     seq,
		"scope_level":       "hive",
		"home_id":           homeID,
		"correlation_id":    correlation,
		"occurred_at":       "2026-10-11T08:00:00.000Z",
		"observed_at":       "2026-10-11T08:00:00.000Z",
		"recorded_at":       "2026-10-11T08:00:00.000Z",
		"occurred_at_known": true,
	} {
		if obj[k] != want {
			t.Errorf("repair %s = %v, want %v", k, obj[k], want)
		}
	}
	source, _ := obj["source"].(map[string]any)
	if source["package"] != pkg || source["source_id"] != RepairSourceID || len(source) != 2 {
		t.Errorf("repair source = %v", source)
	}
	basis, _ := obj["basis"].(map[string]any)
	ref, _ := basis["ref"].(map[string]any)
	if basis["kind"] != "record" || ref["schema"] != RecordSchema || ref["id"] != segment || len(ref) != 2 {
		t.Errorf("repair basis = %v", basis)
	}
	body, _ := obj["body"].(map[string]any)
	if body["segment"] != segment || body["removed_bytes"] != int64(len(torn)) || body["removed_digest"] != canon.FileDigest(torn) || len(body) != 3 {
		t.Errorf("repair body = %v, want %d bytes with digest %s", body, len(torn), canon.FileDigest(torn))
	}
	reg, err := schema.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	bodyData, _ := canon.Encode(body)
	if err := reg.Validate("urn:hive:schema:events:hive.journal.tail-repaired:v1", bodyData); err != nil {
		t.Errorf("repair body against its schema: %v", err)
	}
	if err := reg.Validate("urn:hive:schema:event:v1", line); err != nil {
		t.Errorf("repair event against the envelope: %v", err)
	}
}

// A torn tail is repaired, never read: the next writer removes it and
// appends one tail-repaired event, naming the segment and the length and
// digest of what it removed, before its own events; the last intact
// home_sequence is the repair's minus one. A reader before the repair
// ignores the tail.
func TestTornTailIsRepaired(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 0)
	append_(t, j, events(t, "pool", 1, 2)...)
	name := SegmentName(1)
	intact := segmentBytes(t, home, name)
	intactWhole, _ := lines(intact)
	torn := tear(t, home, name, 7)

	// Before the repair: the tail is ignored by the head and by a reader.
	if head, err := j.Head(); err != nil || head != 1 {
		t.Fatalf("Head with a torn tail = %d, %v", head, err)
	}
	if got := readAll(t, j, 0); len(got) != 1 || got[0].HomeSequence != 1 {
		t.Fatalf("a reader read %d entries past a torn tail", len(got))
	}

	// The repair, made by shadow's command this time.
	shadow, _ := open(t, home, "shadow", 0)
	append_(t, shadow, events(t, "shadow", 3, 4)...)
	data := segmentBytes(t, home, name)
	whole, tail := lines(data)
	if len(tail) != 0 {
		t.Fatalf("a torn tail remains: %q", tail)
	}
	if len(whole) != 4 {
		t.Fatalf("%d lines, want 4", len(whole))
	}
	if !bytes.Equal(whole[0], intactWhole[0]) {
		t.Error("the intact line was changed")
	}
	checkRepair(t, whole[1], 2, name, "shadow", testHomeID, id(ids.Correlation, 3), torn)
	if seqs := sequences(t, data); !slices.Equal(seqs, []int64{1, 2, 3, 4}) {
		t.Errorf("sequences %v", seqs)
	}
	if parsed(t, whole[2])["event_id"] != id(ids.Event, 3) {
		t.Error("the writer's own event does not follow the repair")
	}
	// The repair is itself in the journal, and reads back.
	got := readAll(t, j, 1)
	if len(got) != 3 || got[0].Kind != RepairKind || got[0].HomeSequence != 2 {
		t.Fatalf("read after the repair: %d entries", len(got))
	}
	// The torn event was never acknowledged, so its writer may append it
	// again; it is not a duplicate.
	append_(t, j, event(t, "pool", 2))
	if head, _ := j.Head(); head != 5 {
		t.Errorf("Head = %d after re-appending the torn event", head)
	}
}

// The repair line is written over the torn bytes and the file cut to end
// with it, whether the tail is shorter or longer than the line.
func TestRepairCutsTheTail(t *testing.T) {
	// Each case starts from a segment of two whole lines and returns the
	// torn bytes it leaves and how many whole lines stay before them.
	for _, c := range []struct {
		name string
		torn func(t *testing.T, home string) ([]byte, int)
	}{
		{"a tail shorter than the repair line", func(t *testing.T, home string) ([]byte, int) {
			whole, _ := lines(segmentBytes(t, home, SegmentName(1)))
			return tear(t, home, SegmentName(1), len(whole[1])/2), 1
		}},
		{"a tail of one byte", func(t *testing.T, home string) ([]byte, int) {
			whole, _ := lines(segmentBytes(t, home, SegmentName(1)))
			return tear(t, home, SegmentName(1), len(whole[1])), 1
		}},
		{"a tail longer than the repair line", func(t *testing.T, home string) ([]byte, int) {
			data := segmentBytes(t, home, SegmentName(1))
			long := append(slices.Clone(data), bytes.Repeat([]byte("{"), 3000)...)
			writeSegment(t, home, SegmentName(1), long)
			return long[len(data):], 2
		}},
		{"a tail of zero bytes, as a file system may leave", func(t *testing.T, home string) ([]byte, int) {
			data := segmentBytes(t, home, SegmentName(1))
			long := append(slices.Clone(data), make([]byte, 100)...)
			writeSegment(t, home, SegmentName(1), long)
			return long[len(data):], 2
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := newHome(t, false)
			j, _ := open(t, home, "pool", 0)
			append_(t, j, events(t, "pool", 1, 2)...)
			torn, kept := c.torn(t, home)
			intact := segmentBytes(t, home, SegmentName(1))
			intact = intact[:len(intact)-len(torn)]
			append_(t, j, event(t, "pool", 3))
			data := segmentBytes(t, home, SegmentName(1))
			whole, tail := lines(data)
			if len(tail) != 0 || len(whole) != kept+2 || !bytes.HasPrefix(data, intact) {
				t.Fatalf("after the repair: %d lines, tail %q", len(whole), tail)
			}
			checkRepair(t, whole[kept], int64(kept+1), SegmentName(1), "pool", testHomeID, id(ids.Correlation, 3), torn)
			if seq, _ := parsed(t, whole[kept+1])["home_sequence"].(int64); seq != int64(kept+2) {
				t.Errorf("the event after the repair has home_sequence %d", seq)
			}
			if len(data) != len(intact)+len(whole[kept])+1+len(whole[kept+1])+1 {
				t.Errorf("the file holds %d bytes, more than its lines", len(data))
			}
		})
	}
}

// A torn tail at the start of a fresh segment, after a closed one, is
// repaired at the segment's first home_sequence; a torn tail in a full
// segment is repaired in that segment, and the writer's own event begins
// the next.
func TestRepairAtSegmentEdges(t *testing.T) {
	home := newHome(t, false)
	probe, _ := open(t, home, "pool", 0)
	append_(t, probe, event(t, "pool", 1))
	one := int64(len(segmentBytes(t, home, SegmentName(1))))
	os.RemoveAll(filepath.Join(home, journalDir))

	j, _ := open(t, home, "pool", one+one/2)
	append_(t, j, events(t, "pool", 1, 2)...)
	if got := segmentFiles(t, home); !slices.Equal(got, []string{SegmentName(1), SegmentName(2)}) {
		t.Fatalf("segments %v", got)
	}
	t.Run("the first line of a segment", func(t *testing.T) {
		torn := tear(t, home, SegmentName(2), 5)
		if head, _ := j.Head(); head != 1 {
			t.Errorf("Head = %d", head)
		}
		append_(t, j, event(t, "pool", 3))
		whole, tail := lines(segmentBytes(t, home, SegmentName(2)))
		if len(tail) != 0 || len(whole) != 1 {
			t.Fatalf("segment 2: %d lines, tail %q", len(whole), tail)
		}
		checkRepair(t, whole[0], 2, SegmentName(2), "pool", testHomeID, id(ids.Correlation, 3), torn)
		if seqs := sequences(t, segmentBytes(t, home, SegmentName(3))); !slices.Equal(seqs, []int64{3}) {
			t.Errorf("segment 3 holds %v", seqs)
		}
	})
	t.Run("a full segment", func(t *testing.T) {
		// Segment 3 holds line 3 and is nearly full; tear a line appended
		// to it by a journal with a larger size, so that the repair must go
		// where the bytes were while the next event cannot.
		wide, _ := open(t, home, "pool", 10*one)
		append_(t, wide, event(t, "pool", 4))
		torn := tear(t, home, SegmentName(3), 3)
		append_(t, j, event(t, "pool", 5))
		whole, tail := lines(segmentBytes(t, home, SegmentName(3)))
		if len(tail) != 0 || len(whole) != 2 {
			t.Fatalf("segment 3: %d lines, tail %q", len(whole), tail)
		}
		checkRepair(t, whole[1], 4, SegmentName(3), "pool", testHomeID, id(ids.Correlation, 5), torn)
		if seqs := sequences(t, segmentBytes(t, home, SegmentName(5))); !slices.Equal(seqs, []int64{5}) {
			t.Errorf("segment 5 holds %v", seqs)
		}
	})
	var got []int64
	for _, e := range readAll(t, j, 0) {
		got = append(got, e.HomeSequence)
	}
	if !slices.Equal(got, []int64{1, 2, 3, 4, 5}) {
		t.Errorf("read %v", got)
	}
}

// A journal whose first line was torn is repaired at home_sequence 1, under
// the home record's identity.
func TestRepairOfTheFirstLine(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 0)
	append_(t, j, event(t, "pool", 1))
	torn := tear(t, home, SegmentName(1), 4)
	if head, _ := j.Head(); head != 0 {
		t.Errorf("Head = %d", head)
	}
	append_(t, j, event(t, "pool", 2))
	whole, tail := lines(segmentBytes(t, home, SegmentName(1)))
	if len(tail) != 0 || len(whole) != 2 {
		t.Fatalf("%d lines, tail %q", len(whole), tail)
	}
	checkRepair(t, whole[0], 1, SegmentName(1), "pool", testHomeID, id(ids.Correlation, 2), torn)
	if seqs := sequences(t, segmentBytes(t, home, SegmentName(1))); !slices.Equal(seqs, []int64{1, 2}) {
		t.Errorf("sequences %v", seqs)
	}
}

// The torn segment of testdata/ is repaired like any other: the fixture
// shows what a crash leaves and what the repair makes of it.
func TestRepairOfTheFixture(t *testing.T) {
	home := newHome(t, false)
	data, err := os.ReadFile(filepath.Join("testdata", "torn", SegmentName(1)))
	if err != nil {
		t.Fatal(err)
	}
	writeSegment(t, home, SegmentName(1), data)
	_, torn := lines(data)
	if len(torn) == 0 {
		t.Fatal("the fixture has no torn tail")
	}
	j, _ := open(t, home, "dispatch", 0)
	append_(t, j, event(t, "dispatch", 9))
	got := readAll(t, j, 0)
	if len(got) != 4 {
		t.Fatalf("read %d entries", len(got))
	}
	checkRepair(t, got[2].Line, 3, SegmentName(1), "dispatch", testHomeID, id(ids.Correlation, 9), torn)
	if got[3].EventID != id(ids.Event, 9) || got[3].HomeSequence != 4 {
		t.Errorf("last entry = %+v", got[3])
	}
}

// The fixtures of the repair event's body: every valid one passes its
// schema and every invalid one fails it.
func TestRepairBodyFixtures(t *testing.T) {
	reg, err := schema.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	const ref = "urn:hive:schema:events:hive.journal.tail-repaired:v1"
	for _, dir := range []string{"valid", "invalid"} {
		files, err := filepath.Glob(filepath.Join("testdata", "tail-repaired", dir, "*.json"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no %s fixtures: %v", dir, err)
		}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			err = reg.Validate(ref, data)
			if dir == "valid" && err != nil {
				t.Errorf("%s: %v", f, err)
			}
			if dir == "invalid" && err == nil {
				t.Errorf("%s: passed", f)
			}
		}
	}
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 0)
	// The store's recovery can hand the journal the same batch after a
	// repair, and nothing is written twice.
	append_(t, j, events(t, "pool", 1, 2)...)
	tear(t, home, SegmentName(1), 3)
	recover_(t, j, events(t, "pool", 1, 2)...)
	recover_(t, j, events(t, "pool", 1, 2)...)
	var got []int64
	var kinds []string
	for _, e := range readAll(t, j, 0) {
		got = append(got, e.HomeSequence)
		kinds = append(kinds, e.Kind)
	}
	if !slices.Equal(got, []int64{1, 2, 3}) || kinds[1] != RepairKind || kinds[2] != "pool.thing.happened" {
		t.Errorf("read %v %v", got, kinds)
	}
}

// repairEvent is a well-formed repair event of segment 1, as a map a test
// may alter.
func repairEvent() map[string]any {
	m := envelope("pool", 1)
	m["kind"], m["source"] = RepairKind, map[string]any{"package": "pool", "source_id": RepairSourceID}
	m["home_sequence"], m["recorded_at"] = int64(1), m["observed_at"]
	m["basis"] = map[string]any{"kind": "record", "ref": map[string]any{"schema": RecordSchema, "id": SegmentName(1)}}
	m["body"] = map[string]any{"segment": SegmentName(1), "removed_bytes": int64(1), "removed_digest": canon.FileDigest([]byte("x"))}
	return m
}

// The journal's own line is held to what makes it the journal's: the one
// version of its kind, a basis that is the journal's record of the segment
// the line is in, a body that names that segment, and the body's schema. The
// same check holds a line read, a line appended and the last line of a
// segment.
func TestRepairEventIsChecked(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 0)
	check := func(m map[string]any, segment string) error {
		data, err := canon.Encode(m)
		if err != nil {
			t.Fatal(err)
		}
		return j.checkEvent(m, data, segment)
	}
	if err := check(repairEvent(), SegmentName(1)); err != nil {
		t.Fatalf("a well-formed repair failed: %v", err)
	}
	for _, c := range []struct {
		name    string
		alter   func(m map[string]any)
		segment string
		want    error
	}{
		{"a repair of zero bytes", func(m map[string]any) { m["body"].(map[string]any)["removed_bytes"] = int64(0) }, SegmentName(1), nil},
		{"a source other than the journal", func(m map[string]any) { m["source"].(map[string]any)["source_id"] = "pool" }, SegmentName(1), nil},
		{"a newer minor version of the kind", func(m map[string]any) { m["kind_version"] = "1.1" }, SegmentName(1), store.ErrUnsupportedVersion},
		{"a newer major version of the kind", func(m map[string]any) { m["kind_version"] = "2.0" }, SegmentName(1), store.ErrUnsupportedVersion},
		{"a basis that is not a record", func(m map[string]any) { m["basis"].(map[string]any)["kind"] = "receipt" }, SegmentName(1), nil},
		{"a basis of another schema", func(m map[string]any) {
			m["basis"].(map[string]any)["ref"].(map[string]any)["schema"] = "pool.thing"
		}, SegmentName(1), nil},
		{"a basis of another segment", func(m map[string]any) {
			m["basis"].(map[string]any)["ref"].(map[string]any)["id"] = SegmentName(2)
		}, SegmentName(1), nil},
		{"a body that names another segment than the basis", func(m map[string]any) { m["body"].(map[string]any)["segment"] = SegmentName(2) }, SegmentName(1), nil},
		{"a line in a segment it does not name", func(m map[string]any) {}, SegmentName(2), nil},
		{"a line checked with no segment", func(m map[string]any) {}, "", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := repairEvent()
			c.alter(m)
			err := check(m, c.segment)
			if err == nil {
				t.Fatal("passed")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Errorf("error %v, want %v", err, c.want)
			}
			if c.want == nil && errors.Is(err, store.ErrUnsupportedVersion) {
				t.Errorf("a malformed repair reported as unsupported: %v", err)
			}
		})
	}

	// A line read is held to the same: a repair in the wrong segment, or of
	// a version this build does not read, stops the reader as the fault it is.
	for _, c := range []struct {
		name  string
		alter func(m map[string]any)
		want  error
	}{
		{"another segment", func(m map[string]any) {
			m["basis"].(map[string]any)["ref"].(map[string]any)["id"] = SegmentName(2)
			m["body"].(map[string]any)["segment"] = SegmentName(2)
		}, store.ErrRecordMalformed},
		{"a newer kind version", func(m map[string]any) { m["kind_version"] = "1.1" }, store.ErrUnsupportedVersion},
	} {
		t.Run("read "+c.name, func(t *testing.T) {
			home := newHome(t, false)
			j, _ := open(t, home, "pool", 0)
			m := repairEvent()
			c.alter(m)
			data, err := canon.Encode(m)
			if err != nil {
				t.Fatal(err)
			}
			writeSegment(t, home, SegmentName(1), append(data, '\n'))
			r, err := j.Reader(0)
			if err != nil {
				t.Fatal(err)
			}
			_, err = r.Next()
			wantErr(t, "reading the line", err, c.want)
			_, err = j.Head()
			wantErr(t, "the head", err, c.want)
		})
	}
}
