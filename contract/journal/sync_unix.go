//go:build unix

package journal

import (
	"fmt"
	"os"
	"path/filepath"
)

// syncSegment flushes a segment as it is, so that the lines in it, whoever
// wrote them, are durable.
func (j *Journal) syncSegment(name string) error {
	if err := j.step("sync " + name); err != nil {
		return err
	}
	f, err := os.Open(filepath.Join(j.dir, name))
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("journal: flushing %s: %w", name, err)
	}
	return nil
}

// syncDir flushes a directory, so that a file created in it is durable. Go's
// flush on macOS is the full flush to the medium.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("journal: flushing %s: %w", dir, err)
	}
	return nil
}
