//go:build windows

package journal

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
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

// isOwnerOnly reports whether the entry at path is open to its owner only.
func isOwnerOnly(info fs.FileInfo, path string) bool {
	return store.CheckOwnerOnly(path) == nil
}

// grantOthers gives Everyone read access to the home at dir, which a check of
// the home refuses.
func grantOthers(t testing.TB, dir string) {
	t.Helper()
	if out, err := exec.Command("icacls", dir, "/grant", "*S-1-1-0:(R)").CombinedOutput(); err != nil {
		t.Fatalf("icacls: %v: %s", err, out)
	}
}

// closeToOthers makes the home at dir open to its owner only again.
func closeToOthers(t testing.TB, dir string) {
	t.Helper()
	secureHome(t, dir)
}

// closeSegments makes the segments but the newest unreadable, as a segment
// the journal must not read: each is held open without sharing until the test
// ends, so that every other open of it is refused.
func closeSegments(t *testing.T, home string) {
	t.Helper()
	files := segmentFiles(t, home)
	for _, name := range files[:len(files)-1] {
		path, err := syscall.UTF16PtrFromString(filepath.Join(home, journalDir, name))
		if err != nil {
			t.Fatal(err)
		}
		h, err := syscall.CreateFile(path, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { syscall.CloseHandle(h) })
	}
}
