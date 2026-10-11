package journal

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	// pending are entries read and not yet returned.
	pending []*Entry
}

// Reader returns a Reader positioned after cursor, the last home_sequence
// its consumer handled, or 0 for the start of the journal. Where the lines
// after the cursor are no longer kept, the first Next reports ErrGone.
func (j *Journal) Reader(cursor int64) (*Reader, error) {
	if cursor < 0 {
		return nil, fmt.Errorf("journal: cursor %d is negative", cursor)
	}
	return &Reader{j: j, cursor: cursor}, nil
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
// called again: false when the journal has no further whole line now.
func (r *Reader) load() (bool, error) {
	segs, err := r.j.segments()
	if err != nil {
		return false, err
	}
	if r.seg == nil {
		if len(segs) == 0 {
			return false, nil
		}
		next := r.cursor + 1
		i := -1
		for k := range segs {
			if segs[k].first <= next {
				i = k
			}
		}
		if i < 0 {
			return false, fmt.Errorf("%w: the journal begins at home_sequence %d", ErrGone, segs[0].first)
		}
		r.seg, r.off, r.expect = &segs[i], 0, segs[i].first
	}
	data, torn, err := r.read()
	if err != nil {
		return false, err
	}
	for len(data) > 0 {
		n := bytes.IndexByte(data, '\n')
		line := data[:n]
		e, err := r.entry(line)
		if err != nil {
			return false, err
		}
		if e.HomeSequence > r.cursor {
			r.pending = append(r.pending, e)
		}
		r.off += int64(n) + 1
		r.expect++
		data = data[n+1:]
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

// read reads the segment from the offset reached: the whole lines, ending
// in a line feed, and whether a torn tail follows them.
func (r *Reader) read() ([]byte, bool, error) {
	f, err := os.Open(filepath.Join(r.j.dir, r.seg.name))
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

// entry decodes a line at the position reached and checks its sequence.
func (r *Reader) entry(line []byte) (*Entry, error) {
	obj, err := r.j.decodeLine(line)
	if err != nil {
		return nil, fmt.Errorf("%w: %s, home_sequence %d", err, r.seg.name, r.expect)
	}
	seq, _ := obj["home_sequence"].(int64)
	if seq != r.expect {
		return nil, fmt.Errorf("%w: journal: %s holds home_sequence %d where %d is expected", store.ErrRecordMalformed, r.seg.name, seq, r.expect)
	}
	id, _ := obj["event_id"].(string)
	kind, _ := obj["kind"].(string)
	return &Entry{seq, id, kind, r.seg.name, line, obj}, nil
}
