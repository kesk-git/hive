// Package access holds the helpers of the contract's tests that make a home
// private, and read whether an entry is, in the way of the platform: the mode
// bits on macOS and Linux, and on Windows the access-control list the store
// sets and checks. It makes a home as Hive does, through the store's own
// primitives, so that every entry in it is owned by the user Hive runs as. It
// is a package of its own, beneath testsupport, only because it imports the
// store: were it part of testsupport, which the tests of the store import,
// they would form an import cycle that Go refuses. For the same reason the
// store's own tests cannot import it: they keep short helpers over the store's
// own primitives, not copies of these.
package access

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kesk-git/hive/contract/store"
)

// EmptyHome makes a private home directory with nothing in it, under a
// temporary directory the test removes. The directory is made and secured
// first, so that whatever is made in it afterwards is owner-only.
func EmptyHome(t testing.TB) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "home")
	if err := store.MakeDir(dir); err != nil {
		t.Fatal(err)
	}
	SecureHome(t, dir)
	return dir
}

// SecureHome makes the home at dir what spec/records.md says a home is: open
// to its owner only, on macOS and Linux by mode 0700, on Windows by a
// protected access-control list of its owner alone, which what is made in it
// inherits.
func SecureHome(t testing.TB, dir string) {
	t.Helper()
	if err := store.SecureDir(dir); err != nil {
		t.Fatal(err)
	}
}

// WriteFile puts the file at path, with the content given, in a home as Hive
// does: the directories above it that are not there are made with the store's
// MakeDir, and the file is created with the store's CreateFile, written and
// closed, so that each is owned by the user and open to its owner only. A file
// that is there is emptied first.
func WriteFile(t testing.TB, path string, data []byte) {
	t.Helper()
	if err := store.MakeDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	f, err := store.CreateFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
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
