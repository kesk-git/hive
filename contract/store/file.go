package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
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

// The one top-level path of the home that a package may write besides its own
// directory in this lane is home.json, which the table of spec/records.md gives
// to dispatch. A package names it by its path in the home, and a path of its
// own directory never is that path. No other path outside a package's
// directory is its to write.
// HomeFilePath is the path of home.json, as dispatch names it.
const HomeFilePath = homeFile

// owned reports whether rel names the top-level path of the home that the
// package owns, rather than a path of its own directory.
func (s *Store) owned(rel string) bool {
	return s.pkg == "dispatch" && rel == homeFile
}

// abs returns the absolute path of rel, a slash path below the store or the
// top-level path of the home the package owns.
func (s *Store) abs(rel string) string {
	if s.owned(rel) {
		return filepath.Join(s.Dir, filepath.FromSlash(rel))
	}
	return filepath.Join(s.dir, filepath.FromSlash(rel))
}

// homePath is the path of rel relative to the home, the form an intent holds.
func (s *Store) homePath(rel string) string {
	if s.owned(rel) {
		return rel
	}
	return s.pkg + "/" + rel
}

// fromHomePath is the inverse of homePath. A path of the home that is neither
// the package's own directory nor the top-level path it owns is not the
// package's to write.
func (s *Store) fromHomePath(p string) (string, error) {
	if s.owned(p) {
		return p, nil
	}
	if rel, ok := strings.CutPrefix(p, s.pkg+"/"); ok && !s.owned(rel) {
		return rel, nil
	}
	return "", fmt.Errorf("store: path %q is outside the store of %s", p, s.pkg)
}

// checkPath checks a path below the store, or the top-level path the package
// owns: slash-separated, every segment a lowercase file name, ending in
// .json.
func (s *Store) checkPath(rel string) error {
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
	switch first, _, _ := strings.Cut(rel, "/"); first {
	case receiptsDir, requestsDir, txDir, tmpDir:
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
// named for the request in the store's tmp/ directory, the file is flushed,
// renamed over the record's name, and both directories the rename touched are
// flushed. A reader sees the whole old file or the whole new one. A write that
// fails leaves the record as it was and removes its temporary file; an
// interruption leaves the temporary file for the next writer to empty tmp/ of.
// When it returns, the file and every directory on its way down from the home
// are durable.
func (s *Store) writeFile(rel, requestID string, data []byte) error {
	full := s.abs(rel)
	tmpDirPath := filepath.Join(s.dir, tmpDir)
	for _, d := range []string{tmpDirPath, filepath.Dir(full)} {
		if err := os.MkdirAll(d, dirMode); err != nil {
			return fmt.Errorf("store: %w", err)
		}
	}
	tmp := filepath.Join(tmpDirPath, requestID+tempSuffix)
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
	if err := s.syncChain(rel); err != nil {
		return err
	}
	if err := s.step("syncdir " + s.relDir(tmpDirPath)); err != nil {
		return err
	}
	return syncDir(tmpDirPath)
}

// syncChain flushes the directory of the file at rel and every directory
// above it up to the home's, so that the file's entry, and the entry of every
// directory created on the way to it, are durable. Recovery calls it for a
// file it finds already written, which a crash may have left visible but not
// flushed.
func (s *Store) syncChain(rel string) error {
	if err := s.step("syncdir " + rel); err != nil {
		return err
	}
	for dir := filepath.Dir(s.abs(rel)); ; dir = filepath.Dir(dir) {
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

// clearTmp empties the store's tmp/ directory of what a crash left in it. A
// writer holding the lock calls it, so nothing in it is still being written.
// Nothing outside tmp/ is ever removed for being a temporary file.
func (s *Store) clearTmp() error {
	dir := filepath.Join(s.dir, tmpDir)
	entries, err := os.ReadDir(dir)
	if errNotExist(err) || err == nil && len(entries) == 0 {
		return nil
	}
	if err != nil {
		return fmt.Errorf("store: emptying %s: %w", tmpDir, err)
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return fmt.Errorf("store: emptying %s: %w", tmpDir, err)
		}
	}
	return syncDir(dir)
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
