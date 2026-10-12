//go:build windows

package journal

import (
	"fmt"
	"os"
	"path/filepath"
)

// syncSegment flushes a segment as it is, so that the lines in it, whoever
// wrote them, are durable. Windows flushes only a handle opened with write
// access, so the segment is opened for writing.
func (j *Journal) syncSegment(name string) error {
	if err := j.step("sync " + name); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(j.dir, name), os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("journal: flushing %s: %w", name, err)
	}
	return nil
}

// syncDir does nothing on Windows, which offers no directory handle that can
// be flushed: a segment is flushed through its own handle, as it is written
// and by syncSegment (spec/records.md, Writing one record).
func syncDir(dir string) error { return nil }
