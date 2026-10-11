package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/ids"
)

// Journal is where a transaction's events go. The home journal of
// spec/events.md implements it; the store opens nothing under journal/
// itself. Append appends the events in order, taking locks/journal.lock
// after the store's lock, and skips an event whose event_id is already in
// the journal, so recovery may call it again for the same events.
type Journal interface {
	Append(events []Event) error
}

// Options configure a Store.
type Options struct {
	// Clock gives the time of every updated_at the store sets. nil is the
	// system clock.
	Clock clock.Clock
	// Journal receives the events of a transaction. A transaction that names
	// events fails with ErrNoJournal when it is nil.
	Journal Journal
	// Waiting says how long the store waits for its lock.
	Waiting Waiting
}

// Store is one package's part of one home: <home>/<package>/, and the
// top-level path of the home the table of spec/records.md gives the package
// besides it (home.json for dispatch).
type Store struct {
	*Home
	pkg     string
	dir     string
	clock   clock.Clock
	journal Journal
	waiting Waiting

	// fault, when set by a test, is called before each step of a write with
	// the step's name; an error it returns stops the store there, as a crash
	// would.
	fault func(step string) error
}

// Open checks the home at dir and returns the store of the package pkg in
// it, creating the package's directory and locks/ if they are not there.
// pkg is one of the packages the home table gives a directory.
func Open(dir, pkg string, opts Options) (*Store, error) {
	if !slices.Contains(storePackages, pkg) {
		return nil, fmt.Errorf("store: %q has no store in a home", pkg)
	}
	h, err := CheckHome(dir)
	if err != nil {
		return nil, err
	}
	s := &Store{
		Home:    h,
		pkg:     pkg,
		dir:     filepath.Join(h.Dir, pkg),
		clock:   opts.Clock,
		journal: opts.Journal,
		waiting: opts.Waiting,
	}
	if s.clock == nil {
		s.clock = clock.System{}
	}
	for _, d := range []string{s.dir, filepath.Join(h.Dir, locksDir)} {
		if err := os.MkdirAll(d, dirMode); err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
	}
	return s, nil
}

// Package is the package whose store this is.
func (s *Store) Package() string { return s.pkg }

// Record is a replaceable record as read from the store.
type Record struct {
	// Data is the file's content: canonical form and one final line feed.
	Data []byte
	// Revision is its record_revision.
	Revision int64
	// Digest is the digest of the record.
	Digest string
}

// ReadRecord reads the record at rel, a path relative to the home, without a
// lock: the record is replaced atomically, so it is whole. A missing record
// is reported with fs.ErrNotExist; a record that cannot be read, is not a
// record file or has no record_revision is ErrRecordMalformed.
func (s *Store) ReadRecord(rel string) (*Record, error) {
	if err := s.checkRecordPath(rel); err != nil {
		return nil, err
	}
	data, err := s.readFile(rel)
	if err != nil {
		return nil, err
	}
	v, err := canon.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrRecordMalformed, rel, err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: %s: a record is a JSON object", ErrRecordMalformed, rel)
	}
	rev, ok := obj["record_revision"].(int64)
	if !ok || ids.CheckNumber(rev) != nil {
		return nil, fmt.Errorf("%w: %s: record_revision is not a whole number from 1", ErrRecordMalformed, rel)
	}
	digest, err := canon.RecordDigest(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrRecordMalformed, rel, err)
	}
	return &Record{data, rev, digest}, nil
}

// Read reads a file written once, a receipt or a recorded outcome, without a
// lock, and checks its form. A missing file is reported with fs.ErrNotExist.
func (s *Store) Read(rel string) ([]byte, error) {
	if err := s.checkPath(rel); err != nil {
		return nil, err
	}
	return s.readFile(rel)
}

// Writer holds the store's transaction lock and is the only way to write to
// the store. Release it when the command is done.
type Writer struct {
	s    *Store
	lock *Lock
}

// Lock takes the store's transaction lock, exclusive, and brings the store to
// a consistent state before returning: it completes the transaction a crash
// left behind, if any, and removes the temporary files a crash left. An
// intent it cannot complete stops the store with ErrRecordMalformed naming
// the intent; nothing discards the intent.
func (s *Store) Lock() (*Writer, error) {
	l, err := Acquire(s.Dir, s.pkg, false, s.waiting)
	if err != nil {
		return nil, err
	}
	if err := s.recover(); err != nil {
		l.Release()
		return nil, err
	}
	return &Writer{s, l}, nil
}

// LockShared takes the store's lock shared, for a reader that needs several
// records to agree. A transaction a crash left behind is completed first, by
// the roll-forward Lock runs, so the records the reader sees are those of a
// whole transaction; where recovery stops the store, LockShared fails with
// the error Lock fails with. The shared lock itself writes nothing.
func (s *Store) LockShared() (*Lock, error) {
	for {
		l, err := Acquire(s.Dir, s.pkg, true, s.waiting)
		if err != nil {
			return nil, err
		}
		pending, err := s.intents()
		if err != nil {
			l.Release()
			return nil, err
		}
		if len(pending) == 0 {
			return l, nil
		}
		l.Release()
		w, err := s.Lock()
		if err != nil {
			return nil, err
		}
		w.Release()
	}
}

// Release releases the lock. The Writer is not usable afterwards.
func (w *Writer) Release() error {
	if w.lock == nil {
		return nil
	}
	l := w.lock
	w.lock = nil
	return l.Release()
}

var errReleased = errors.New("store: the writer has released its lock")

// Written describes a record the store wrote.
type Written struct {
	Path     string
	Revision int64
	Digest   string
}

// Replace replaces one record (spec/records.md, Writing one record). content
// is the new record as a JSON object in any spacing; the store sets its
// record_revision to expected+1 and its updated_at to the clock's time, and
// writes it in canonical form. expected is the record_revision on disk, or 0
// for a record being created, which must be absent: a different state is
// ErrRevisionConflict or ErrAlreadyExists and nothing is written. requestID
// names the request that writes, and so the temporary file.
func (w *Writer) Replace(requestID, rel string, content []byte, expected int64) (Written, error) {
	if w.lock == nil {
		return Written{}, errReleased
	}
	if err := ids.Check(requestID, ids.Request); err != nil {
		return Written{}, err
	}
	if err := w.s.checkRecordPath(rel); err != nil {
		return Written{}, err
	}
	if err := w.s.expectRevision(rel, expected); err != nil {
		return Written{}, err
	}
	p, err := w.s.prepareRecord(rel, content, expected+1)
	if err != nil {
		return Written{}, err
	}
	if err := w.s.writeFile(rel, requestID, p.data); err != nil {
		return Written{}, err
	}
	return Written{rel, expected + 1, p.digest}, nil
}

// expectRevision checks that the record at rel has the revision expected, or
// is absent when expected is 0.
func (s *Store) expectRevision(rel string, expected int64) error {
	if expected == 0 {
		return s.expectAbsent(rel)
	}
	if err := ids.CheckNumber(expected); err != nil {
		return fmt.Errorf("store: expected revision of %s: %w", rel, err)
	}
	cur, err := s.ReadRecord(rel)
	switch {
	case errNotExist(err):
		return fmt.Errorf("%w: %s expected at revision %d, found none", ErrRevisionConflict, rel, expected)
	case err != nil:
		return err
	case cur.Revision != expected:
		return fmt.Errorf("%w: %s expected at revision %d, found %d", ErrRevisionConflict, rel, expected, cur.Revision)
	}
	return nil
}

// expectAbsent checks that no file is at rel.
func (s *Store) expectAbsent(rel string) error {
	_, err := os.Lstat(s.abs(rel))
	switch {
	case err == nil:
		return fmt.Errorf("%w: %s", ErrAlreadyExists, rel)
	case errNotExist(err):
		return nil
	}
	return fmt.Errorf("store: %w", err)
}

// prepared is a file ready to be written.
type prepared struct {
	rel    string
	data   []byte // canonical form and one final line feed
	digest string
}

// prepareRecord makes the content of a record at revision rev: the object
// content, with record_revision and updated_at set, in canonical form.
func (s *Store) prepareRecord(rel string, content []byte, rev int64) (*prepared, error) {
	obj, err := parseObject(rel, content)
	if err != nil {
		return nil, err
	}
	stamp, err := clock.Stamp(s.clock)
	if err != nil {
		return nil, err
	}
	obj["record_revision"] = rev
	obj["updated_at"] = stamp
	return s.encode(rel, obj)
}

// prepareOnce makes the content of a file written once: the object content
// in canonical form, with nothing set.
func (s *Store) prepareOnce(rel string, content []byte) (*prepared, error) {
	obj, err := parseObject(rel, content)
	if err != nil {
		return nil, err
	}
	return s.encode(rel, obj)
}

func parseObject(rel string, content []byte) (map[string]any, error) {
	v, err := canon.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("store: content for %s: %w", rel, err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("store: content for %s is not a JSON object", rel)
	}
	return obj, nil
}

// encode writes v in canonical form.
func (s *Store) encode(rel string, v any) (*prepared, error) {
	out, err := canon.Encode(v)
	if err != nil {
		return nil, fmt.Errorf("store: content for %s: %w", rel, err)
	}
	data := append(out, '\n')
	return &prepared{rel, data, canon.FileDigest(out)}, nil
}
