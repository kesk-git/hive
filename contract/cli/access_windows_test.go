//go:build windows

package cli

import (
	"io/fs"
	"os"
	"os/exec"
	"testing"

	"github.com/kesk-git/hive/contract/store"
)

// secureHome makes the home at dir what spec/records.md says a home is on
// Windows: a protected access-control list of its owner alone, which what is
// made in it inherits.
func secureHome(t testing.TB, dir string) {
	t.Helper()
	if err := store.SecureDir(dir); err != nil {
		t.Fatal(err)
	}
}

// hasMode reports whether the entry at path is open to its owner only. There
// are no mode bits to compare on Windows, so the access-control list is what
// is read.
func hasMode(info fs.FileInfo, path string, want os.FileMode) bool {
	return store.CheckOwnerOnly(path) == nil
}

// grantOthers gives Everyone read access to path, which a check of the home
// refuses; mode is for macOS and Linux.
func grantOthers(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	if out, err := exec.Command("icacls", path, "/grant", "*S-1-1-0:(R)").CombinedOutput(); err != nil {
		t.Fatalf("icacls: %v: %s", err, out)
	}
}
