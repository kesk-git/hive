package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/ids"
)

var epoch = time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)

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

// newHome makes an empty home under a temporary directory, which the test
// removes.
func newHome(t testing.TB) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	return dir
}

// fakeJournal stands in for the home journal: it keeps each event once, by
// event_id, in the order appended. appends counts the calls of either
// method, recovers those of Recover alone.
type fakeJournal struct {
	order    []string
	lines    map[string][]byte
	appends  int
	recovers int
	fail     error
}

func newJournal() *fakeJournal { return &fakeJournal{lines: map[string][]byte{}} }

func (j *fakeJournal) Recover(events []Event) error {
	j.recovers++
	return j.Append(events)
}

func (j *fakeJournal) Append(events []Event) error {
	j.appends++
	if j.fail != nil {
		return j.fail
	}
	for _, e := range events {
		if _, ok := j.lines[e.ID]; ok {
			continue
		}
		j.order = append(j.order, e.ID)
		j.lines[e.ID] = append([]byte(nil), e.Content...)
	}
	return nil
}

// open opens the store of pkg in home with a fake clock at epoch and a fake
// journal, waiting for its lock by moving that clock.
func open(t testing.TB, home, pkg string) (*Store, *clock.Fake, *fakeJournal) {
	t.Helper()
	clk := clock.NewFake(epoch)
	j := newJournal()
	s, err := Open(home, pkg, Options{
		Clock:   clk,
		Journal: j,
		Waiting: Waiting{Clock: clk, Limit: 100 * time.Millisecond, Pause: clk.Advance},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, clk, j
}

// lock takes the store's lock, failing the test if it cannot.
func lock(t testing.TB, s *Store) *Writer {
	t.Helper()
	w, err := s.Lock()
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// tree is the state of a directory: every file by slash path below it, with
// the digest of its bytes.
func tree(t testing.TB, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		sum := sha256.Sum256(data)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// sameTree fails the test when two states differ.
func sameTree(t testing.TB, what string, got, want map[string]string) {
	t.Helper()
	var keys []string
	for k := range got {
		keys = append(keys, k)
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		g, gok := got[k]
		w, wok := want[k]
		switch {
		case !gok:
			t.Errorf("%s: %s is missing", what, k)
		case !wok:
			t.Errorf("%s: %s should not be there", what, k)
		case g != w:
			t.Errorf("%s: %s differs", what, k)
		}
	}
}

// files lists the slash paths below dir, sorted.
func files(t testing.TB, dir string) []string {
	t.Helper()
	var out []string
	for k := range tree(t, dir) {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// temps lists the files of every tmp/ directory below dir.
func temps(t testing.TB, dir string) []string {
	t.Helper()
	var out []string
	for _, f := range files(t, dir) {
		if strings.Contains("/"+f, "/"+tmpDir+"/") {
			out = append(out, f)
		}
	}
	return out
}

// write puts a file in a store by hand, as a crash or a stranger would leave
// it, making the directories it needs.
func write(t testing.TB, s *Store, rel string, data []byte) {
	t.Helper()
	full := s.abs(rel)
	if err := os.MkdirAll(filepath.Dir(full), dirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, fileMode); err != nil {
		t.Fatal(err)
	}
}

// record is the content of a record with the given members, in canonical
// form with a line feed, as the store would write it at revision rev and time
// stamp.
func record(t testing.TB, members map[string]any, rev int64, stamp string) []byte {
	t.Helper()
	m := map[string]any{}
	for k, v := range members {
		m[k] = v
	}
	m["record_revision"] = rev
	m["updated_at"] = stamp
	out, err := canon.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	return append(out, '\n')
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

// trace runs fn with every step of the store recorded and nothing faulted.
func trace(t testing.TB, s *Store, fn func()) []string {
	t.Helper()
	var steps []string
	s.fault = func(step string) error {
		steps = append(steps, step)
		return nil
	}
	fn()
	s.fault = nil
	if len(steps) == 0 {
		t.Fatal("no step was recorded")
	}
	return steps
}

// faultAt makes the store fail, as a crash would, at the n-th occurrence of
// step, counting from 1, and reports whether it did.
func faultAt(s *Store, step string, n int) *bool {
	hit := new(bool)
	seen := 0
	s.fault = func(name string) error {
		if name != step {
			return nil
		}
		seen++
		if seen == n {
			*hit = true
			return errCrash
		}
		return nil
	}
	return hit
}

type crash struct{}

func (crash) Error() string { return "test: the process crashed here" }

var errCrash crash

// stamp is the clock's timestamp.
func stamp(t testing.TB, c clock.Clock) string {
	t.Helper()
	s, err := clock.Stamp(c)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// wholeRecord checks that the file at rel is one of the contents given, in
// full, or absent when "" is among them.
func wholeRecord(t testing.TB, s *Store, rel string, allowed ...[]byte) {
	t.Helper()
	data, err := os.ReadFile(s.abs(rel))
	if errNotExist(err) {
		for _, a := range allowed {
			if a == nil {
				return
			}
		}
		t.Fatalf("%s is absent", rel)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range allowed {
		if a != nil && bytes.Equal(data, a) {
			return
		}
	}
	t.Fatalf("%s holds neither the old nor the new record: %q", rel, data)
}
