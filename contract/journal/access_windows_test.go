//go:build windows

package journal

import (
	"io/fs"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/kesk-git/hive/contract/internal/testsupport/access"
	"github.com/kesk-git/hive/contract/store"
)

// isOwnerOnly reports whether the entry at path is open to its owner only.
func isOwnerOnly(info fs.FileInfo, path string) bool {
	return store.CheckOwnerOnly(path) == nil
}

// closeToOthers makes the home at dir open to its owner only again.
func closeToOthers(t testing.TB, dir string) {
	t.Helper()
	access.SecureHome(t, dir)
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
