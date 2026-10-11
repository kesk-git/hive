package journal

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/kesk-git/hive/contract/store"
)

// Entry is one line of the journal as read.
type Entry struct {
	// HomeSequence is the line's position in the journal.
	HomeSequence int64
	// EventID and Kind are the event's.
	EventID string
	Kind    string
	// Segment is the file name of the segment the line is in.
	Segment string
	// Line is the line as written, canonical form without the line feed.
	Line []byte
	// Event is the line decoded.
	Event map[string]any
	// Late is true when an event of the same source.source_id that is later
	// in the source's order was recorded before this one: later by
	// source.source_sequence when both carry one, otherwise by occurred_at.
	// A late event is returned as it was recorded (spec/events.md, The three
	// times).
	Late bool
}

// mark is the latest a source has been seen to be: its greatest
// source_sequence, when it has carried one, and its greatest occurred_at.
type mark struct {
	seq      int64
	hasSeq   bool
	occurred string
}

// Reader reads the journal after a cursor, in order. It takes no lock: a
// line is whole once its line feed is there, and a torn tail is ignored
// until a writer has repaired it. A Reader is for one goroutine.
type Reader struct {
	j *Journal
	// cursor is the home_sequence of the last entry Next returned.
	cursor int64
	// seg is the segment being read, nil before the first line is looked
	// for; off is how many of its bytes have been read as whole lines, and
	// expect the home_sequence its next line carries.
	seg    *segment
	off    int64
	expect int64
	// marks are what the lines read so far say of each source, by source_id,
	// for the lateness of the lines that follow.
	marks map[string]*mark
	// pending are entries read and not yet returned.
	pending []*Entry
}

// Reader returns a Reader positioned after cursor, the last home_sequence
// its consumer handled, or 0 for the start of the journal. Where the lines
// after the cursor are no longer kept, the first Next reports ErrGone. Before
// it returns the first line after the cursor the Reader reads the retained
// lines up to the cursor, so that lateness is relative to the journal as it is
// kept, however the Reader began.
func (j *Journal) Reader(cursor int64) (*Reader, error) {
	if cursor < 0 {
		return nil, fmt.Errorf("journal: cursor %d is negative", cursor)
	}
	return &Reader{j: j, cursor: cursor, marks: map[string]*mark{}}, nil
}

// Cursor is the home_sequence of the last entry Next returned, or the cursor
// the Reader was given.
func (r *Reader) Cursor() int64 { return r.cursor }

// Next returns the next line of the journal, or nil when no further line is
// durable now. A line that cannot be read, a gap in the sequence or a torn
// tail in a closed segment is store.ErrRecordMalformed; after an error the
// Reader is not usable.
func (r *Reader) Next() (*Entry, error) {
	for len(r.pending) == 0 {
		more, err := r.load()
		if err != nil {
			return nil, err
		}
		if !more {
			return nil, nil
		}
	}
	e := r.pending[0]
	r.pending = r.pending[1:]
	r.cursor = e.HomeSequence
	return e, nil
}

// load reads more whole lines into pending and reports whether it may be
// called again: false when the journal has no further whole line now. It
// reconciles the position with the segments kept every time: a segment read
// to its end and since removed is left behind, and one with lines unread that
// is gone is ErrGone.
func (r *Reader) load() (bool, error) {
	segs, err := r.j.segments()
	if err != nil {
		return false, err
	}
	switch {
	case r.seg == nil:
		if len(segs) == 0 {
			return false, nil
		}
		if segs[0].first > r.cursor+1 {
			return false, fmt.Errorf("%w: the journal begins at home_sequence %d", ErrGone, segs[0].first)
		}
		r.seg, r.off, r.expect = &segs[0], 0, segs[0].first
	case !slices.ContainsFunc(segs, func(s segment) bool { return s.name == r.seg.name }):
		k := slices.IndexFunc(segs, func(s segment) bool { return s.first == r.expect })
		if k < 0 {
			return false, fmt.Errorf("%w: %s, which held home_sequence %d, is gone", ErrGone, r.seg.name, r.expect)
		}
		r.seg, r.off = &segs[k], 0
	}
	entries, torn, err := r.lines()
	if errNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		e.Late = r.late(e.Event)
		if e.HomeSequence > r.cursor {
			r.pending = append(r.pending, e)
		}
		r.off += int64(len(e.Line)) + 1
		r.expect++
	}
	if len(r.pending) > 0 {
		return true, nil
	}
	// No whole line is left in this segment. A later segment means it is
	// closed: it ends whole and the next begins where it stopped.
	var later *segment
	for k := range segs {
		if segs[k].first > r.seg.first && (later == nil || segs[k].first < later.first) {
			later = &segs[k]
		}
	}
	if later == nil {
		return false, nil
	}
	if torn {
		return false, fmt.Errorf("%w: journal: closed segment %s ends in a torn line", store.ErrRecordMalformed, r.seg.name)
	}
	if later.first != r.expect {
		return false, fmt.Errorf("%w: journal: %s ends at home_sequence %d but %s begins at %d", store.ErrRecordMalformed, r.seg.name, r.expect-1, later.name, later.first)
	}
	r.seg, r.off, r.expect = later, 0, later.first
	return true, nil
}

// lines reads the whole lines of the segment from the offset reached and
// decodes them, and reports whether a torn tail follows them. A writer that
// repairs the tail overwrites bytes the reader may have read part of, so a
// fault is reported only once the bytes read are those of the read before it.
func (r *Reader) lines() ([]*Entry, bool, error) {
	var prev []byte
	for {
		data, torn, err := r.read()
		if err != nil {
			return nil, false, err
		}
		entries, err := r.decode(data)
		if err == nil || bytes.Equal(data, prev) {
			return entries, torn, err
		}
		prev = data
	}
}

// read reads the segment from the offset reached: the whole lines, ending
// in a line feed, and whether a torn tail follows them.
func (r *Reader) read() ([]byte, bool, error) {
	f, err := os.Open(filepath.Join(r.j.dir, r.seg.name))
	if errNotExist(err) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("%w: journal: %v", store.ErrRecordMalformed, err)
	}
	defer f.Close()
	if _, err := f.Seek(r.off, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("journal: %w", err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, false, fmt.Errorf("journal: reading %s: %w", r.seg.name, err)
	}
	end := bytes.LastIndexByte(data, '\n') + 1
	return data[:end], end < len(data), nil
}

// decode reads whole lines, each ending in a line feed, as the entries that
// follow the position reached.
func (r *Reader) decode(data []byte) ([]*Entry, error) {
	var out []*Entry
	for expect := r.expect; len(data) > 0; expect++ {
		n := bytes.IndexByte(data, '\n')
		e, err := r.entry(data[:n], expect)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
		data = data[n+1:]
	}
	return out, nil
}

// entry decodes a line and checks that its sequence is the one expected.
func (r *Reader) entry(line []byte, expect int64) (*Entry, error) {
	obj, err := r.j.decodeLine(line, r.seg.name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s, home_sequence %d", err, r.seg.name, expect)
	}
	seq, _ := obj["home_sequence"].(int64)
	if seq != expect {
		return nil, fmt.Errorf("%w: journal: %s holds home_sequence %d where %d is expected", store.ErrRecordMalformed, r.seg.name, seq, expect)
	}
	id, _ := obj["event_id"].(string)
	kind, _ := obj["kind"].(string)
	return &Entry{HomeSequence: seq, EventID: id, Kind: kind, Segment: r.seg.name, Line: line, Event: obj}, nil
}

// late reports whether an event is late and takes it into the marks of its
// source. It is late when the source was already seen further on: by
// source_sequence where both the event and the marks carry one, otherwise by
// occurred_at. Timestamps are of one fixed width, so they compare as text.
func (r *Reader) late(event map[string]any) bool {
	source, _ := event["source"].(map[string]any)
	sourceID, _ := source["source_id"].(string)
	seq, hasSeq := source["source_sequence"].(int64)
	occurred, _ := event["occurred_at"].(string)
	m := r.marks[sourceID]
	if m == nil {
		m = &mark{}
		r.marks[sourceID] = m
	}
	late := m.occurred > occurred
	if hasSeq && m.hasSeq {
		late = m.seq > seq
	}
	if hasSeq && (!m.hasSeq || seq > m.seq) {
		m.seq, m.hasSeq = seq, true
	}
	m.occurred = max(m.occurred, occurred)
	return late
}
