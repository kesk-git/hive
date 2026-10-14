package journal

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/store"
)

// dirMode and fileMode are the modes a test gives what it makes in a home by
// hand, those the library gives on macOS and Linux.
const (
	dirMode  = 0o700
	fileMode = 0o600
)

var epoch = time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)

// testHomeID is the home_id of testdata/home/home.json.
const testHomeID = "home_01arz3ndektsv4rrffq69g5fav"

// alphabet is Crockford base 32 in lowercase, for invented identifiers.
const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// id invents the n-th identifier of kind p: a valid, lowercase ULID whose
// last two characters count n.
func id(p ids.Prefix, n int) string {
	s := string(p) + "_0" + strings.Repeat("0", 23) + string(alphabet[n/32%32]) + string(alphabet[n%32])
	if err := ids.Check(s, p); err != nil {
		panic(err)
	}
	return s
}

// newHome makes a home under a temporary directory, with the home record of
// testdata/ unless bare is set.
func newHome(t testing.TB, bare bool) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	secureHome(t, dir)
	if bare {
		return dir
	}
	data, err := os.ReadFile("testdata/home/home.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "home.json"), data, fileMode); err != nil {
		t.Fatal(err)
	}
	return dir
}

// waiting returns a Waiting run by the clock given: a pause moves it.
func waiting(clk *clock.Fake) store.Waiting {
	return store.Waiting{Clock: clk, Limit: 100 * time.Millisecond, Pause: clk.Advance}
}

// open opens the journal of home for pkg with a fake clock at epoch and the
// segment size given, 0 for the default.
func open(t testing.TB, home, pkg string, segmentSize int64) (*Journal, *clock.Fake) {
	t.Helper()
	clk := clock.NewFake(epoch)
	j, err := Open(home, Options{Clock: clk, Package: pkg, SegmentSize: segmentSize, Waiting: waiting(clk)})
	if err != nil {
		t.Fatal(err)
	}
	return j, clk
}

// stamp is the clock's timestamp.
func stamp(t testing.TB, c clock.Clock) string {
	t.Helper()
	s, err := clock.Stamp(c)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// eventEnvelope is the content of the n-th test event of pkg, without the two
// members the journal sets, as a map a test may alter.
func eventEnvelope(pkg string, n int) map[string]any {
	return map[string]any{
		"schema":            "hive.event",
		"schema_version":    "1.0",
		"event_id":          id(ids.Event, n),
		"kind":              pkg + ".thing.happened",
		"kind_version":      "1.0",
		"source":            map[string]any{"package": pkg, "source_id": pkg},
		"scope_level":       "hive",
		"home_id":           testHomeID,
		"correlation_id":    id(ids.Correlation, n),
		"occurred_at":       "2026-10-11T07:59:00.000Z",
		"observed_at":       "2026-10-11T07:59:30.000Z",
		"occurred_at_known": true,
		"basis":             map[string]any{"kind": "record", "ref": map[string]any{"schema": pkg + ".thing", "id": "thing-" + string(alphabet[n%32])}},
		"body":              map[string]any{},
	}
}

// event is the n-th test event of pkg as the store hands it to the journal.
func event(t testing.TB, pkg string, n int) store.Event {
	t.Helper()
	return encoded(t, eventEnvelope(pkg, n))
}

// encoded is an envelope as a store.Event, in canonical form.
func encoded(t testing.TB, obj map[string]any) store.Event {
	t.Helper()
	data, err := canon.Encode(obj)
	if err != nil {
		t.Fatal(err)
	}
	eid, _ := obj["event_id"].(string)
	return store.Event{ID: eid, Content: data}
}

// events are the test events of pkg numbered from first to last.
func events(t testing.TB, pkg string, first, last int) []store.Event {
	t.Helper()
	var out []store.Event
	for n := first; n <= last; n++ {
		out = append(out, event(t, pkg, n))
	}
	return out
}

// append_ appends events, failing the test if the journal refuses.
func append_(t testing.TB, j *Journal, evs ...store.Event) {
	t.Helper()
	if err := j.Append(evs); err != nil {
		t.Fatal(err)
	}
}

// recover_ appends events as recovery does, failing the test if the journal
// refuses.
func recover_(t testing.TB, j *Journal, evs ...store.Event) {
	t.Helper()
	if err := j.Recover(evs); err != nil {
		t.Fatal(err)
	}
}

// segmentFiles lists the segment files of the journal of home, in name
// order.
func segmentFiles(t testing.TB, home string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(home, journalDir))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// segmentBytes reads a segment file of the journal of home.
func segmentBytes(t testing.TB, home, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, journalDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// writeSegment puts a segment file in the journal of home as a crash or a
// stranger would leave it.
func writeSegment(t testing.TB, home, name string, data []byte) {
	t.Helper()
	dir := filepath.Join(home, journalDir)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, fileMode); err != nil {
		t.Fatal(err)
	}
}

// lines splits a segment's bytes into its whole lines without their line
// feeds, and returns the torn tail besides.
func lines(data []byte) (whole [][]byte, torn []byte) {
	end := bytes.LastIndexByte(data, '\n') + 1
	if end > 0 {
		whole = bytes.Split(data[:end-1], []byte("\n"))
	}
	return whole, data[end:]
}

// parsed reads a JSON object.
func parsed(t testing.TB, data []byte) map[string]any {
	t.Helper()
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatalf("%v: %q", err, data)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("not an object: %q", data)
	}
	return m
}

// sequences reads home_sequence from every whole line of a segment.
func sequences(t testing.TB, data []byte) []int64 {
	t.Helper()
	whole, _ := lines(data)
	var out []int64
	for _, l := range whole {
		seq, _ := parsed(t, l)["home_sequence"].(int64)
		out = append(out, seq)
	}
	return out
}

// readAll reads every entry after cursor.
func readAll(t testing.TB, j *Journal, cursor int64) []*Entry {
	t.Helper()
	r, err := j.Reader(cursor)
	if err != nil {
		t.Fatal(err)
	}
	var out []*Entry
	for {
		e, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		if e == nil {
			return out
		}
		out = append(out, e)
	}
}

// wantErr fails the test unless err is target.
func wantErr(t testing.TB, what string, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("%s: error %v, want %v", what, err, target)
	}
}
