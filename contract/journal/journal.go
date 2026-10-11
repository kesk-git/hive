package journal

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/schema"
	"github.com/kesk-git/hive/contract/store"
)

// DefaultSegmentSize is the size at which a segment is closed when Options
// sets none.
const DefaultSegmentSize = 8 << 20

// The journal's place in a home and the form of its files.
const (
	journalDir    = "journal"
	lockName      = "journal"
	segmentPrefix = "events-"
	segmentSuffix = ".jsonl"
	segmentDigits = 20
)

// ownerOnly is the access of every directory and file of a home.
const (
	dirMode  = 0o700
	fileMode = 0o600
)

// The event envelope: its schema, the name every line carries and the newest
// version this build reads and writes.
const (
	eventSchema  = "urn:hive:schema:event:v1"
	eventRecord  = "hive.event"
	eventVersion = "1.0"
)

// packageName is the definition of the common schema that lists the seven
// packages, the only openers of a journal that append.
const packageName = "urn:hive:schema:common:v1#/$defs/package_name"

// The errors of the journal, besides those it shares with the store: a line
// or a segment it cannot read is store.ErrRecordMalformed, a line of a newer
// envelope version is store.ErrUnsupportedVersion and a lock not released
// within the bounded wait is store.ErrLockHeld. Test for one with errors.Is.
var (
	// ErrGone reports a cursor that lies before the segments the home still
	// keeps: the lines after it are no longer there to read.
	ErrGone = errors.New("journal: the lines after the cursor are no longer kept")
	// ErrEvent reports an event handed to Append that is not a valid event
	// envelope, carries a member the journal sets, names another event than
	// its event_id or belongs to another home. Nothing is appended.
	ErrEvent = errors.New("journal: event refused")
	// ErrNotOwner reports an event whose kind does not begin with the name of
	// the package that opened the journal (spec/events.md, The journal).
	ErrNotOwner = errors.New("journal: a package appends only events whose kind begins with its own name")
	// ErrNoPackage reports an Append through a journal opened without the
	// package that appends.
	ErrNoPackage = errors.New("journal: opened without the package that appends")
	// ErrNoHome reports an Append in a home that has no home record yet: an
	// event is recorded under the home's identity, which only that record
	// gives.
	ErrNoHome = errors.New("journal: the home has no home record to append under")
)

// Options configure a Journal.
type Options struct {
	// Clock gives recorded_at and the times of a repair event. nil is the
	// system clock.
	Clock clock.Clock
	// Package is the package whose command opens the journal: the owner of
	// every kind it appends and the source.package of a repair it makes. It
	// may be left empty by a command that only reads.
	Package string
	// SegmentSize is the size in bytes at which a segment is closed. Zero or
	// less is DefaultSegmentSize.
	SegmentSize int64
	// Waiting says how long Append waits for the journal's lock.
	Waiting store.Waiting
}

// Journal is the event journal of one home.
type Journal struct {
	*store.Home
	dir         string
	pkg         string
	clock       clock.Clock
	segmentSize int64
	waiting     store.Waiting
	reg         *schema.Registry
}

// Open checks the home at dir and returns its journal, creating journal/ if
// it is not there. Options.Package, when given, is one of the seven packages
// of the common schema. A home without its home record may be opened, to
// read; Append refuses until the record is there.
func Open(dir string, opts Options) (*Journal, error) {
	reg, err := schema.Embedded()
	if err != nil {
		return nil, err
	}
	if opts.Package != "" {
		name, err := canon.Encode(opts.Package)
		if err != nil {
			return nil, fmt.Errorf("journal: %q is not a package", opts.Package)
		}
		if err := reg.Validate(packageName, name); err != nil {
			return nil, fmt.Errorf("journal: %q is not a package", opts.Package)
		}
	}
	h, err := store.CheckHome(dir)
	if err != nil {
		return nil, err
	}
	j := &Journal{
		Home:        h,
		dir:         filepath.Join(h.Dir, journalDir),
		pkg:         opts.Package,
		clock:       opts.Clock,
		segmentSize: opts.SegmentSize,
		waiting:     opts.Waiting,
		reg:         reg,
	}
	if j.clock == nil {
		j.clock = clock.System{}
	}
	if j.segmentSize <= 0 {
		j.segmentSize = DefaultSegmentSize
	}
	if err := os.MkdirAll(j.dir, dirMode); err != nil {
		return nil, fmt.Errorf("journal: %w", err)
	}
	return j, nil
}

// Package is the package the journal was opened by, or "".
func (j *Journal) Package() string { return j.pkg }

// Head returns the home_sequence of the last durable line, or 0 while the
// journal holds none. A torn tail does not count. Head takes no lock, so a
// writer repairing the tail may change what it reads under it; it reads again
// until a fault is the one it met the time before.
func (j *Journal) Head() (int64, error) {
	var last string
	for {
		st, err := j.state()
		if err == nil {
			return st.next - 1, nil
		}
		if err.Error() == last {
			return 0, err
		}
		last = err.Error()
	}
}

// segment is one segment file: its name and the home_sequence of its first
// line.
type segment struct {
	name  string
	first int64
}

// SegmentName is the file name of the segment whose first line has the
// home_sequence first.
func SegmentName(first int64) string {
	return fmt.Sprintf("%s%0*d%s", segmentPrefix, segmentDigits, first, segmentSuffix)
}

// parseSegment reads a segment's first home_sequence from its name. An
// entry that is not named like a segment is not one; a name of the form that
// holds no sequence is malformed.
func parseSegment(name string) (int64, bool, error) {
	digits, ok := strings.CutPrefix(name, segmentPrefix)
	if !ok {
		return 0, false, nil
	}
	digits, ok = strings.CutSuffix(digits, segmentSuffix)
	if !ok || len(digits) != segmentDigits {
		return 0, false, nil
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, false, nil
		}
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n < 1 || n > canon.MaxNumber {
		return 0, true, fmt.Errorf("%w: journal: %s names no home_sequence", store.ErrRecordMalformed, name)
	}
	if SegmentName(n) != name {
		return 0, true, fmt.Errorf("%w: journal: %s is not the name of a segment", store.ErrRecordMalformed, name)
	}
	return n, true, nil
}

// segments lists the segments of the journal in order of their first
// home_sequence. An entry that is not a segment is left alone.
func (j *Journal) segments() ([]segment, error) {
	entries, err := os.ReadDir(j.dir)
	if err != nil {
		return nil, fmt.Errorf("journal: %w", err)
	}
	var segs []segment
	for _, e := range entries {
		n, isSeg, err := parseSegment(e.Name())
		if err != nil {
			return nil, err
		}
		if !isSeg {
			continue
		}
		if !e.Type().IsRegular() {
			return nil, fmt.Errorf("%w: journal: %s is not a regular file", store.ErrRecordMalformed, e.Name())
		}
		segs = append(segs, segment{e.Name(), n})
	}
	slices.SortFunc(segs, func(a, b segment) int { return cmp.Compare(a.first, b.first) })
	return segs, nil
}

// tail is the end of a segment file: how much of it is intact, its last
// complete line and the bytes of a torn tail after it.
type tail struct {
	// size is the length of the file.
	size int64
	// intact is the length up to and including the last line feed; the
	// bytes after it are torn.
	intact int64
	// last is the last complete line without its line feed, or nil when the
	// file holds none.
	last []byte
	// torn is what follows the last line feed: empty when the tail is whole.
	torn []byte
}

// readTail reads the end of the segment file at path, from the end back to
// the start of its last complete line.
func readTail(path string) (*tail, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: journal: %v", store.ErrRecordMalformed, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("journal: %w", err)
	}
	size := info.Size()
	const chunk = 64 << 10
	var buf []byte // a suffix of the file
	pos := size    // where buf starts in the file
	lf := -1       // the last line feed in buf, once found
	for {
		if lf < 0 {
			lf = bytes.LastIndexByte(buf, '\n')
		}
		if lf >= 0 {
			prev := bytes.LastIndexByte(buf[:lf], '\n')
			if prev >= 0 || pos == 0 {
				return &tail{
					size:   size,
					intact: pos + int64(lf) + 1,
					last:   slices.Clone(buf[prev+1 : lf]),
					torn:   slices.Clone(buf[lf+1:]),
				}, nil
			}
		} else if pos == 0 {
			return &tail{size: size, torn: slices.Clone(buf)}, nil
		}
		n := min(int64(chunk), pos)
		more := make([]byte, int(n)+len(buf))
		if _, err := f.ReadAt(more[:n], pos-n); err != nil {
			return nil, fmt.Errorf("journal: reading %s: %w", filepath.Base(path), err)
		}
		copy(more[n:], buf)
		buf = more
		pos -= n
		if lf >= 0 {
			lf += int(n)
		}
	}
}

// state is what a writer learns under the lock: the segments, the newest
// one with its tail, and the next home_sequence.
type state struct {
	segs []segment
	// cur is the newest segment, or nil while there is none.
	cur *segment
	// tail is the end of cur.
	tail *tail
	// next is the home_sequence the next line takes.
	next int64
}

// state reads the segments and the tail of the newest. The next
// home_sequence follows the last intact line; a newest segment without one
// starts at the sequence its name gives, which the segment before it, when
// there is one, must end just short of.
func (j *Journal) state() (*state, error) {
	segs, err := j.segments()
	if err != nil {
		return nil, err
	}
	st := &state{segs: segs, next: 1}
	if len(segs) == 0 {
		return st, nil
	}
	st.cur = &segs[len(segs)-1]
	st.tail, err = readTail(filepath.Join(j.dir, st.cur.name))
	if err != nil {
		return nil, err
	}
	if st.tail.last != nil {
		seq, err := j.lastLine(st.cur.name, st.tail.last)
		if err != nil {
			return nil, err
		}
		if seq < st.cur.first {
			return nil, fmt.Errorf("%w: journal: the last line of %s has home_sequence %d, before the segment's first", store.ErrRecordMalformed, st.cur.name, seq)
		}
		st.next = seq + 1
		return st, nil
	}
	st.next = st.cur.first
	if len(segs) >= 2 {
		prev := segs[len(segs)-2]
		t, err := readTail(filepath.Join(j.dir, prev.name))
		if err != nil {
			return nil, err
		}
		if t.last == nil || len(t.torn) > 0 {
			return nil, fmt.Errorf("%w: journal: closed segment %s does not end in a whole line", store.ErrRecordMalformed, prev.name)
		}
		seq, err := j.lastLine(prev.name, t.last)
		if err != nil {
			return nil, err
		}
		if seq+1 != st.cur.first {
			return nil, fmt.Errorf("%w: journal: %s ends at home_sequence %d but %s begins at %d", store.ErrRecordMalformed, prev.name, seq, st.cur.name, st.cur.first)
		}
	}
	return st, nil
}

// lastLine reads a segment's last line as an event and returns its
// home_sequence.
func (j *Journal) lastLine(name string, line []byte) (int64, error) {
	obj, err := j.decodeLine(line, name)
	if err != nil {
		return 0, fmt.Errorf("%w: the last line of %s", err, name)
	}
	seq, _ := obj["home_sequence"].(int64)
	return seq, nil
}

// errNotExist reports whether err says a file is missing.
func errNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

// syncDirs flushes the journal's directory and the home's above it, so that
// every segment and journal/ itself are durable, whenever and by whomever they
// were created.
func (j *Journal) syncDirs() error {
	if err := syncDir(j.dir); err != nil {
		return err
	}
	return syncDir(j.Home.Dir)
}

// syncFile flushes a file as it is, so that lines read from it are durable.
func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("journal: flushing %s: %w", filepath.Base(path), err)
	}
	return nil
}

// syncDir flushes a directory, so that a file created in it is durable.
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
