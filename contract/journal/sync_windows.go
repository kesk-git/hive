//go:build windows

package journal

import (
	"fmt"
	"os"

	"github.com/kesk-git/hive/contract/store"
)

// syncSegment flushes a segment as it is, so that the lines in it, whoever
// wrote them, are durable. Windows flushes only a handle opened with write
// access, so the segment is opened for writing.
func (j *Journal) syncSegment(name string) error {
	if err := j.step("sync " + name); err != nil {
		return err
	}
	path, err := j.path(name)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	defer f.Close()
	if err := store.Flush(f); err != nil {
		return fmt.Errorf("journal: flushing %s: %w", name, err)
	}
	return nil
}
