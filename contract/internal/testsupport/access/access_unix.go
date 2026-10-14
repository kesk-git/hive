//go:build unix

package access

import (
	"io/fs"
	"os"
	"testing"
)

// SecureHome makes the home at dir what the platform says a home is. The
// access a test home, and what is made in it, has on macOS and Linux is the
// mode its maker gives it, so there is nothing to set.
func SecureHome(t testing.TB, dir string) {}

// HasMode reports whether the entry at path, which info describes, has the
// access of an owner-only entry of the mode given.
func HasMode(info fs.FileInfo, path string, want os.FileMode) bool {
	return info.Mode().Perm() == want
}
