package store

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/internal/testsupport"
)

var epoch = time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)

// The helpers the tests of the contract share.
var (
	id     = testsupport.ID
	tree   = testsupport.Tree
	parsed = testsupport.Parsed
)

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

// abs is the absolute path of rel, a path relative to the home, unchecked, for
// a test that looks at the disk or changes it by hand.
func (s *Store) abs(rel string) string {
	return filepath.Join(s.Dir, filepath.FromSlash(rel))
}

// newHome makes an empty home under a temporary directory, which the test
// removes, as Hive makes one: the directory is made and secured first, so that
// everything made in it is owner-only and owned by the user.
func newHome(t testing.TB) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "home")
	makeDir(t, dir)
	secureHome(t, dir)
	return dir
}

// secureHome makes the home at dir what spec/records.md says a home is: open
// to its owner only, on Windows by a protected access-control list of the
// owner alone, which what is made in it inherits.
func secureHome(t testing.TB, dir string) {
	t.Helper()
	if err := SecureDir(dir); err != nil {
		t.Fatal(err)
	}
}

// makeDir makes the directory at path in a home, and the directories above it
// that are not there, through the store's own primitive, so that each is
// owner-only and owned by the user, as Hive makes it.
func makeDir(t testing.TB, path string) {
	t.Helper()
	if err := MakeDir(path); err != nil {
		t.Fatal(err)
	}
}

// makeFile puts the file at path in a home, with the content given, through
// the store's own primitive, so that it is owner-only and owned by the user,
// as Hive makes it. A file that is there is emptied first; the directory it is
// in must be there.
func makeFile(t testing.TB, path string, data []byte) {
	t.Helper()
	f, err := CreateFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// write puts a file in a store by hand, as a crash or a stranger would leave
// it, making the directories it needs.
func write(t testing.TB, s *Store, rel string, data []byte) {
	t.Helper()
	full := s.abs(rel)
	makeDir(t, filepath.Dir(full))
	makeFile(t, full, data)
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
