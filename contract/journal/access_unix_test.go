//go:build unix

package journal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kesk-git/hive/contract/store"
)

// closeToOthers makes the home at dir open to its owner only again.
func closeToOthers(t testing.TB, dir string) {
	t.Helper()
	os.Chmod(dir, dirMode)
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

// A journal whose directory lists its segments but cannot be searched is
// malformed, as a segment that cannot be read is.
func TestUnsearchableJournalIsMalformed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a closed directory does not refuse a search")
	}
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 0)
	append_(t, j, event(t, "pool", 1))
	dir := filepath.Join(home, journalDir)
	if err := os.Chmod(dir, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, dirMode) })
	_, err := j.Head()
	wantErr(t, "Head", err, store.ErrRecordMalformed)
}
