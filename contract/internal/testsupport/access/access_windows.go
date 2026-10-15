//go:build windows

package access

import (
	"io/fs"
	"os"

	"github.com/kesk-git/hive/contract/store"
)

// HasMode reports whether the entry at path is open to its owner only. There
// are no mode bits to compare on Windows, so the access-control list is what
// is read.
func HasMode(info fs.FileInfo, path string, want os.FileMode) bool {
	return store.CheckOwnerOnly(path) == nil
}
