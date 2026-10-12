//go:build unix

package store

import (
	"fmt"
	"os"
)

// OpenRead opens the file at path for reading, as every reader of a home's
// files does: on macOS and Linux a plain open, which never blocks the
// replacement of the file.
func OpenRead(path string) (*os.File, error) { return os.Open(path) }

// ReadFile reads the whole file at path, as OpenRead opens it.
func ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// createTemp creates the temporary file at tmp for writing, truncating a file
// a failed attempt left under that name.
func createTemp(tmp string) (*os.File, error) {
	return os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
}

// publish closes the flushed temporary file f at tmp and renames it over the
// record at full, which is the file system's own atomic rename. A write that
// fails removes the temporary file; an interruption at the step leaves it for
// the next writer to empty tmp/ of.
func (s *Store) publish(f *os.File, tmp, full, rel string) error {
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("store: closing %s: %w", rel, err)
	}
	if err := s.step("rename " + rel); err != nil {
		return err
	}
	if err := os.Rename(tmp, full); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// syncDir flushes a directory, so that a rename or a removal in it is
// durable. Go's flush on macOS is the full flush to the medium.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("store: flushing %s: %w", dir, err)
	}
	return nil
}

// syncFile flushes the file at path itself. On macOS and Linux a flush of the
// directories above it is what recovery needs, so there is nothing to do.
func syncFile(path string) error { return nil }
