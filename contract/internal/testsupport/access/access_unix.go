//go:build unix

package access

import (
	"io/fs"
	"os"
)

// HasMode reports whether the entry at path, which info describes, has the
// access of an owner-only entry of the mode given.
func HasMode(info fs.FileInfo, path string, want os.FileMode) bool {
	return info.Mode().Perm() == want
}
