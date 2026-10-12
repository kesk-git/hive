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

// SecureFile makes the open file f open to its owner only, mode 0600.
func SecureFile(f *os.File) error {
	if err := f.Chmod(fileMode); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}
