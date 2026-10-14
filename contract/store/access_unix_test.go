//go:build unix

package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kesk-git/hive/contract/internal/testsupport/access"
)

// The helpers the tests of the contract share for the access of a home.
var (
	secureHome = access.SecureHome
	hasMode    = access.HasMode
	newHome    = access.EmptyHome
)

// grantGroup opens the entry at path to its group, as mode says.
func grantGroup(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	os.Chmod(path, mode)
}

// symlink makes link a symbolic link to target.
func symlink(t testing.TB, target, link string) {
	t.Helper()
	os.Symlink(target, link)
}

// platformLayoutCases are the homes only this platform refuses.
var platformLayoutCases []layoutCase

// refuseReplace makes the replacement of the record at rel by the writer of
// the test fail, and returns what undoes it. A closed directory does not
// refuse root, so the case is not run as root, which ok reports.
func refuseReplace(t testing.TB, s *Store, rel string) (restore func(), ok bool) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Log("running as root: a closed directory does not refuse a write; the pressure case is not run")
		return nil, false
	}
	dir := filepath.Dir(s.abs(rel))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	return func() { os.Chmod(dir, dirMode) }, true
}

// A record in a directory that cannot be searched is malformed, as any record
// that cannot be read is.
func TestUnsearchableRecordIsMalformed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a closed directory does not refuse a search")
	}
	home := newHome(t)
	s, _, _ := open(t, home, "pool")
	write(t, s, task1, record(t, map[string]any{"n": int64(1)}, 1, "2026-10-11T08:00:00.000Z"))
	dir := filepath.Dir(s.abs(task1))
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, dirMode) })
	if _, err := s.ReadRecord(task1); !errors.Is(err, ErrRecordMalformed) {
		t.Fatalf("ReadRecord in an unsearchable directory: %v", err)
	}
}
