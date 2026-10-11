package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
)

// tempSuffix ends every temporary file. A record ends in .json, so a
// temporary file is never read as one.
const tempSuffix = ".tmp"

// The directories of a store that hold something other than replaceable
// records (spec/records.md, The home).
const (
	receiptsDir = "receipts"
	requestsDir = "requests"
	txDir       = "tx"
)

// abs returns the absolute path of rel, a slash path below the store.
func (s *Store) abs(rel string) string {
	return filepath.Join(s.dir, filepath.FromSlash(rel))
}

// checkPath checks a path below the store: slash-separated, every segment a
// lowercase file name, ending in .json and never a temporary file.
func checkPath(rel string) error {
	if rel == "" {
		return fmt.Errorf("store: empty path")
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("store: path %q has an empty, . or .. segment", rel)
		}
		for i := 0; i < len(seg); i++ {
			c := seg[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_') {
				return fmt.Errorf("store: path %q is not a lowercase file name", rel)
			}
		}
		if strings.HasSuffix(seg, tempSuffix) {
			return fmt.Errorf("store: path %q names a temporary file", rel)
		}
	}
	if !strings.HasSuffix(rel, ".json") {
		return fmt.Errorf("store: path %q does not end in .json", rel)
	}
	return nil
}

// checkRecordPath is checkPath for a replaceable record, which never sits
// among the receipts, the recorded outcomes or the intents.
func checkRecordPath(rel string) error {
	if err := checkPath(rel); err != nil {
		return err
	}
	switch first, _, _ := strings.Cut(rel, "/"); first {
	case receiptsDir, requestsDir, txDir:
		return fmt.Errorf("store: path %q is not a record path: %s/ holds no replaceable record", rel, first)
	}
	return nil
}

// RecordPath is the path of a replaceable record of one kind, the kind
// written without its package prefix: records/<kind>/<id>.json.
func RecordPath(kind, id string) string { return "records/" + kind + "/" + id + ".json" }

// ReceiptPath is the path of a receipt: receipts/<receipt_id>.json.
func ReceiptPath(receiptID string) string { return receiptsDir + "/" + receiptID + ".json" }

// OutcomePath is the path of a recorded outcome: requests/<request_id>.json.
func OutcomePath(requestID string) string { return requestsDir + "/" + requestID + ".json" }

// IntentPath is the path of the intent of a transaction: tx/<request_id>.json.
func IntentPath(requestID string) string { return txDir + "/" + requestID + ".json" }

// step is the point at which a test interrupts the store, as a crash would.
// In a delivered executable fault is nil and nothing happens.
func (s *Store) step(name string) error {
	if s.fault == nil {
		return nil
	}
	return s.fault(name)
}

// writeFile replaces the file at rel with data by the four steps of
// spec/records.md, Writing one record: the content goes to a temporary file
// in the same directory named for the request, the file is flushed, renamed
// over the record's name, and the directory is flushed. A reader sees the
// whole old file or the whole new one. A write that fails leaves the record
// as it was and removes its temporary file; an interruption leaves the
// temporary file for the next writer to remove.
func (s *Store) writeFile(rel, requestID string, data []byte) error {
	full := s.abs(rel)
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	tmp := full + "." + requestID + tempSuffix
	if err := s.step("create " + rel); err != nil {
		return err
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if err := s.step("fill " + rel); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("store: writing %s: %w", rel, err)
	}
	if err := s.step("sync " + rel); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("store: flushing %s: %w", rel, err)
	}
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
	if err := s.step("syncdir " + rel); err != nil {
		return err
	}
	return syncDir(dir)
}

// removeFile removes the file at rel and flushes its directory.
func (s *Store) removeFile(rel string) error {
	if err := s.step("remove " + rel); err != nil {
		return err
	}
	full := s.abs(rel)
	if err := os.Remove(full); err != nil && !errNotExist(err) {
		return fmt.Errorf("store: %w", err)
	}
	return syncDir(filepath.Dir(full))
}

// syncDir flushes a directory, so that a rename or a removal in it is
// durable.
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

// sweepTemps removes every temporary file under the store. A writer holding
// the lock calls it, so no temporary file it finds is still being written.
func (s *Store) sweepTemps() error {
	dirty := map[string]bool{}
	err := filepath.WalkDir(s.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() && strings.HasSuffix(d.Name(), tempSuffix) {
			if err := os.Remove(p); err != nil {
				return err
			}
			dirty[filepath.Dir(p)] = true
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store: removing temporary files: %w", err)
	}
	for dir := range dirty {
		if err := syncDir(dir); err != nil {
			return err
		}
	}
	return nil
}

// readFile reads the file at rel and checks its form: one JSON document in
// UTF-8, no byte-order mark, one final line feed. A missing file is reported
// with fs.ErrNotExist; any other fault is ErrRecordMalformed.
func (s *Store) readFile(rel string) ([]byte, error) {
	data, err := os.ReadFile(s.abs(rel))
	if err != nil {
		if errNotExist(err) {
			return nil, fmt.Errorf("store: %s: %w", rel, fs.ErrNotExist)
		}
		return nil, fmt.Errorf("%w: %s: %v", ErrRecordMalformed, rel, err)
	}
	if err := canon.CheckFile(data); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrRecordMalformed, rel, err)
	}
	return data, nil
}
