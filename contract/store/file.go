package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/internal/flushseam"
)

// tempSuffix ends the name of every temporary file, which sits in the store's
// tmp/ directory and nowhere else. A record ends in .json, so a temporary file
// is never read as one.
const tempSuffix = ".tmp"

// The directories of a store that hold something other than replaceable
// records (spec/records.md, The home).
const (
	receiptsDir = "receipts"
	requestsDir = "requests"
	txDir       = "tx"
	tmpDir      = "tmp"
)

// HomeFilePath is the path of home.json, the one path outside a package's own
// directory that a package may write: dispatch's, by the table of
// spec/records.md.
const HomeFilePath = homeFile

// inner returns rel without the directory of the package, or rel itself for
// home.json.
func (s *Store) inner(rel string) string {
	return strings.TrimPrefix(rel, s.pkg+"/")
}

// checkPath checks a path relative to the home: it lies inside the package's
// own directory, or is home.json for dispatch, and is slash-separated, every
// segment a lowercase file name, ending in .json. Every other path is refused,
// reviews/ and anything under it among them.
func (s *Store) checkPath(rel string) error {
	if rel == "" {
		return fmt.Errorf("store: empty path")
	}
	if rel != HomeFilePath || s.pkg != "dispatch" {
		if !strings.HasPrefix(rel, s.pkg+"/") {
			return fmt.Errorf("store: path %q is outside the store of %s", rel, s.pkg)
		}
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
	}
	if !strings.HasSuffix(rel, ".json") {
		return fmt.Errorf("store: path %q does not end in .json", rel)
	}
	return nil
}

// checkRecordPath is checkPath for a replaceable record, which never sits
// among the receipts, the recorded outcomes, the intents or the temporary
// files.
func (s *Store) checkRecordPath(rel string) error {
	if err := s.checkPath(rel); err != nil {
		return err
	}
	switch first, _, _ := strings.Cut(s.inner(rel), "/"); first {
	case receiptsDir, requestsDir, txDir, tmpDir:
		return fmt.Errorf("store: path %q is not a record path: %s/ holds no replaceable record", rel, first)
	}
	return nil
}

// RecordPath is the path of a replaceable record of one kind of the package
// pkg, the kind written without its package prefix:
// <pkg>/records/<kind>/<id>.json.
func RecordPath(pkg, kind, id string) string { return pkg + "/records/" + kind + "/" + id + ".json" }

// ReceiptPath is the path of a receipt of the package pkg:
// <pkg>/receipts/<receipt_id>.json.
func ReceiptPath(pkg, receiptID string) string {
	return pkg + "/" + receiptsDir + "/" + receiptID + ".json"
}

// OutcomePath is the path of a recorded outcome of the package pkg:
// <pkg>/requests/<request_id>.json.
func OutcomePath(pkg, requestID string) string {
	return pkg + "/" + requestsDir + "/" + requestID + ".json"
}

// IntentPath is the path of the intent of a transaction of the package pkg:
// <pkg>/tx/<request_id>.json.
func IntentPath(pkg, requestID string) string { return pkg + "/" + txDir + "/" + requestID + ".json" }

// step is the point at which a test interrupts the store, as a crash would.
// In a delivered executable fault is nil and nothing happens.
func (s *Store) step(name string) error {
	if s.fault == nil {
		return nil
	}
	return s.fault(name)
}

// writeFile replaces the file at rel with data by the steps of
// spec/records.md, Writing one record: the content goes to a temporary file
// named for the request in the store's tmp/ directory, the file is flushed,
// renamed over the record's name, and both directories the rename touched are
// flushed. Each platform does the rename and the last flush its own way, in
// publish and syncDir. A reader sees the whole old file or the whole new one.
// A write that fails leaves the record as it was and removes its temporary
// file; an interruption leaves the temporary file for the next writer to empty
// tmp/ of.
// When it returns, the file and every directory on its way down from the home
// are durable.
func (s *Store) writeFile(rel, requestID string, data []byte) error {
	full, err := s.Path(rel)
	if err != nil {
		return err
	}
	tmp, err := s.Path(s.pkg + "/" + tmpDir + "/" + requestID + tempSuffix)
	if err != nil {
		return err
	}
	tmpDirPath := filepath.Dir(tmp)
	for _, d := range []string{tmpDirPath, filepath.Dir(full)} {
		if err := MakeDir(d); err != nil {
			return fmt.Errorf("store: %w", err)
		}
	}
	if err := s.step("create " + rel); err != nil {
		return err
	}
	f, err := CreateFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
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
	if err := Flush(f); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("store: flushing %s: %w", rel, err)
	}
	if err := s.publish(f, tmp, full, rel); err != nil {
		return err
	}
	if err := s.syncChain(rel); err != nil {
		return err
	}
	if err := s.step("syncdir " + s.relDir(tmpDirPath)); err != nil {
		return err
	}
	return syncDir(tmpDirPath)
}

// syncFound flushes the file at rel, which recovery finds already written and
// a crash may have left visible but not flushed, and then the directories
// above it, as syncChain does.
func (s *Store) syncFound(rel string) error {
	full, err := s.Path(rel)
	if err != nil {
		return err
	}
	if err := syncFile(full); err != nil {
		return err
	}
	return s.syncChain(rel)
}

// syncChain flushes the directory of the file at rel and every directory
// above it up to the home's, so that the file's entry, and the entry of every
// directory created on the way to it, are durable.
func (s *Store) syncChain(rel string) error {
	if err := s.step("syncdir " + rel); err != nil {
		return err
	}
	full, err := s.Path(rel)
	if err != nil {
		return err
	}
	for dir := filepath.Dir(full); ; dir = filepath.Dir(dir) {
		if err := syncDir(dir); err != nil {
			return err
		}
		if dir == s.Dir {
			return nil
		}
		if err := s.step("syncdir " + s.relDir(filepath.Dir(dir))); err != nil {
			return err
		}
	}
}

// Flush flushes the open file f to the medium, as the writes of a record and
// of a journal segment do before they go on. It is the one place the store
// and the journal ask the file system to flush a file; a test binary may turn
// the real flush off through contract/internal/flushseam, and every step
// around it stays.
func Flush(f *os.File) error {
	if flushseam.Skip {
		return nil
	}
	return f.Sync()
}

// syncDir flushes a directory of the home, as SyncDir does for the store.
func syncDir(dir string) error { return SyncDir("store", dir) }

// relDir names a directory by its slash path below the home, for the steps
// of a test.
func (s *Store) relDir(dir string) string {
	rel, err := filepath.Rel(s.Dir, dir)
	if err != nil {
		return dir + "/"
	}
	return filepath.ToSlash(rel) + "/"
}

// removeFile removes the file at rel and flushes its directory.
func (s *Store) removeFile(rel string) error {
	if err := s.step("remove " + rel); err != nil {
		return err
	}
	full, err := s.Path(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !errNotExist(err) {
		return fmt.Errorf("store: %w", err)
	}
	return syncDir(filepath.Dir(full))
}

// clearTmp empties the store's tmp/ directory of what a crash left in it. A
// writer holding the lock calls it, so nothing in it is still being written.
// Nothing outside tmp/ is ever removed for being a temporary file.
func (s *Store) clearTmp() error {
	rel := s.pkg + "/" + tmpDir
	dir, err := s.Path(rel)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if errNotExist(err) || err == nil && len(entries) == 0 {
		return nil
	}
	if err != nil {
		return fmt.Errorf("store: emptying %s: %w", tmpDir, err)
	}
	for _, e := range entries {
		if err := s.removeAll(rel + "/" + e.Name()); err != nil {
			return fmt.Errorf("store: emptying %s: %w", tmpDir, err)
		}
	}
	return syncDir(dir)
}

// removeAll removes the entry at rel and, for a directory, everything in it,
// each entry resolved through the home's Path before it is touched.
func (s *Store) removeAll(rel string) error {
	p, err := s.Path(rel)
	if err != nil {
		return err
	}
	info, err := os.Lstat(p)
	if errNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := s.removeAll(rel + "/" + e.Name()); err != nil {
				return err
			}
		}
	}
	if err := os.Remove(p); err != nil && !errNotExist(err) {
		return err
	}
	return nil
}

// readFile reads the file at rel and checks its form: one JSON document in
// UTF-8, no byte-order mark, one final line feed. A missing file is reported
// with fs.ErrNotExist; any other fault is ErrRecordMalformed.
func (s *Store) readFile(rel string) ([]byte, error) {
	full, err := s.Path(rel)
	var data []byte
	if err == nil {
		data, err = ReadFile(full)
	}
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
