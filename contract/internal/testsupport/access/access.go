// Package access holds the helpers of the contract's tests that make a home
// private, and read whether an entry is, in the way of the platform: the mode
// bits on macOS and Linux, and on Windows the access-control list the store
// sets and checks. It is a package of its own, beneath testsupport, only
// because on Windows it imports the store: were it part of testsupport, which
// the tests of the store import, they would form an import cycle that Go
// refuses. For the same reason the tests of the store on Windows keep their
// own copies of these helpers; everywhere else they use these.
package access

import (
	"os"
	"path/filepath"
	"testing"
)

// EmptyHome makes a private home directory with nothing in it, under a
// temporary directory the test removes.
func EmptyHome(t testing.TB) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	SecureHome(t, dir)
	return dir
}
