package store

import (
	"fmt"
	"os"
)

// CheckOwnerOnly refuses, as ErrLayout, the entry at path when it grants
// access to anyone but its owner: by its mode on macOS and Linux, by its
// access-control list on Windows (spec/records.md, The home). It does not
// follow a symbolic link.
func CheckOwnerOnly(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return ownerOnly(path, info, path)
}
