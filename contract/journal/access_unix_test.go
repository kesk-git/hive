//go:build unix

package journal

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The access a test home, and what is made in it, has on macOS and Linux is
// the mode its maker gives it, so there is nothing to set.
func secureHome(t testing.TB, dir string) {}

// hasMode reports whether the entry at path, which info describes, has the
// access of an owner-only entry of the mode given.
func hasMode(info fs.FileInfo, path string, want os.FileMode) bool {
	return info.Mode().Perm() == want
}

// isOwnerOnly reports whether the entry at path, which info describes, gives
// its group and others nothing.
func isOwnerOnly(info fs.FileInfo, path string) bool {
	return info.Mode().Perm()&0o077 == 0
}

// grantOthers opens the home at dir to everyone.
func grantOthers(t testing.TB, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

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
