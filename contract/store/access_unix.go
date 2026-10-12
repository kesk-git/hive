//go:build unix

package store

import (
	"fmt"
	"io/fs"
	"os"
)

// ownerOnly refuses an entry that grants access to anyone but its owner: one
// whose mode gives its group or others anything. what names the entry in the
// error; path is where it is.
func ownerOnly(path string, info fs.FileInfo, what string) error {
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("%w: %s is open beyond its owner (mode %04o)", ErrLayout, what, perm)
	}
	return nil
}

// SecureDir makes the directory at dir open to its owner only, mode 0700,
// which is the access spec/records.md gives a home on macOS and Linux. Every
// file and directory made in it is made with its own mode, 0600 and 0700.
func SecureDir(dir string) error {
	if err := os.Chmod(dir, dirMode); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// OpenPrivate opens the file at path for writing, creating it when it is not
// there, open to its owner only, mode 0600. The mode is set through the opened
// file, never a second lookup of the name, before anything is written; a file
// that was there keeps its own mode only until then.
func OpenPrivate(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, fileMode)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(fileMode); err != nil {
		f.Close()
		return nil, fmt.Errorf("store: %w", err)
	}
	return f, nil
}

// checkHomeDir has nothing to refuse on macOS and Linux: the mode of the home
// is all its access.
func checkHomeDir(path string) error { return nil }
