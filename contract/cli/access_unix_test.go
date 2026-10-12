//go:build unix

package cli

import (
	"io/fs"
	"os"
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

// grantOthers opens the entry at path to everyone, as mode says.
func grantOthers(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
