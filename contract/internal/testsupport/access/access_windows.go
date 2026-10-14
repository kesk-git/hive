//go:build windows

package access

import (
	"io/fs"
	"os"
	"testing"

	"github.com/kesk-git/hive/contract/store"
)

// SecureHome makes the home at dir what spec/records.md says a home is on
// Windows: a protected access-control list of its owner alone, which what is
// made in it inherits.
func SecureHome(t testing.TB, dir string) {
	t.Helper()
	if err := store.SecureDir(dir); err != nil {
		t.Fatal(err)
	}
}

// HasMode reports whether the entry at path is open to its owner only. There
// are no mode bits to compare on Windows, so the access-control list is what
// is read.
func HasMode(info fs.FileInfo, path string, want os.FileMode) bool {
	return store.CheckOwnerOnly(path) == nil
}
