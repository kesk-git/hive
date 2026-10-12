package journal

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/store"
)

// line is one line ready to be appended: its event_id, its sequence and its
// canonical form with the line feed.
type line struct {
	id   string
	seq  int64
	data []byte
}

// Append appends events to the journal, in order, as the first attempt of a
// transaction (spec/events.md, The journal). Each event's content is its
// envelope as a JSON object, without home_sequence and recorded_at, which the
// journal sets; its kind begins with the name of the package that opened the
// journal, its event_id is the one given beside it and its home_id is the
// one of the home's record, which the home must have. Append takes the
// journal's lock, repairs a torn tail first, and writes every event as one
// line with its line feed in one write, flushed before the next, and the
// directories that hold the segments before it returns. It looks for no
// earlier line of the event: an event is new until recovery says otherwise.
// An event that fails its checks is refused before anything is written.
func (j *Journal) Append(events []store.Event) error { return j.append(events, false) }

// Recover appends events as recovery rolling an intent forward: as Append
// does, except that an event whose event_id is already in a retained segment
// is skipped, and the segments that hold such events are flushed, with the
// directories, before it returns, for the line may have been visible and not
// yet durable when the process stopped.
func (j *Journal) Recover(events []store.Event) error { return j.append(events, true) }

func (j *Journal) append(events []store.Event, recovering bool) error {
	if len(events) == 0 {
		return nil
	}
	if j.pkg == "" {
		return ErrNoPackage
	}
	homeID, err := j.homeID()
	if err != nil {
		return err
	}
	objs := make([]map[string]any, len(events))
	seen := map[string]bool{}
	for i, e := range events {
		obj, err := j.prepare(e, homeID)
		if err != nil {
			return err
		}
		if seen[e.ID] {
			return fmt.Errorf("%w: %s is named twice", ErrEvent, e.ID)
		}
		seen[e.ID] = true
		objs[i] = obj
	}

	l, err := store.Acquire(j.Dir, lockName, false, j.waiting)
	if err != nil {
		return err
	}
	defer l.Release()

	st, err := j.state()
	if err != nil {
		return err
	}
	stamp, err := clock.Stamp(j.clock)
	if err != nil {
		return err
	}
	var repair *line
	if st.tail != nil && len(st.tail.torn) > 0 {
		corr, _ := objs[0]["correlation_id"].(string)
		repair, err = j.repairLine(st, stamp, homeID, corr)
		if err != nil {
			return err
		}
		st.next++
	}
	var present map[string]bool
	if recovering {
		present, err = j.present(st, events)
		if err != nil {
			return err
		}
	}
	var lines []*line
	for i, e := range events {
		if present[e.ID] {
			continue
		}
		obj := objs[i]
		obj["home_sequence"] = st.next
		obj["recorded_at"] = stamp
		data, err := canon.Encode(obj)
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrEvent, e.ID, err)
		}
		if err := j.checkEvent(obj, data, ""); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrEvent, e.ID, err)
		}
		lines = append(lines, &line{e.ID, st.next, append(data, '\n')})
		st.next++
	}

	if repair != nil {
		// The repair line goes over the torn bytes of the segment they were
		// in, whatever its size, and the file is cut to end with it.
		if err := j.writeLine(st.cur.name, st.tail.intact, repair.data, true); err != nil {
			return err
		}
		st.tail.intact += int64(len(repair.data))
		st.tail.torn = nil
	}
	for _, ln := range lines {
		if st.cur == nil || st.tail.intact > 0 && st.tail.intact+int64(len(ln.data)) > j.segmentSize {
			if st.cur != nil {
				if err := j.syncSegment(st.cur.name); err != nil {
					return err
				}
			}
			st.segs = append(st.segs, segment{SegmentName(ln.seq), ln.seq})
			st.cur = &st.segs[len(st.segs)-1]
			st.tail = &tail{}
		}
		if err := j.writeLine(st.cur.name, st.tail.intact, ln.data, false); err != nil {
			return err
		}
		st.tail.intact += int64(len(ln.data))
	}
	return j.syncDirs()
}

// homeID reads the home's identity from its home record as the append begins:
// nothing is recorded under an identity the journal was handed, or cached
// before the record was there.
func (j *Journal) homeID() (string, error) {
	h, err := store.CheckHome(j.Home.Dir)
	if err != nil {
		return "", err
	}
	if h.HomeID == "" {
		return "", ErrNoHome
	}
	return h.HomeID, nil
}

// prepare checks what can be checked of an event before the lock is held,
// and returns its content as an object: it is a JSON object with the given
// event_id, it carries neither member the journal sets, its kind begins with
// the package's name and its home_id is homeID.
func (j *Journal) prepare(e store.Event, homeID string) (map[string]any, error) {
	if err := ids.Check(e.ID, ids.Event); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEvent, err)
	}
	v, err := canon.Parse(e.Content)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrEvent, e.ID, err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: %s: an event is a JSON object", ErrEvent, e.ID)
	}
	if obj["event_id"] != e.ID {
		return nil, fmt.Errorf("%w: %s: the content carries another event_id", ErrEvent, e.ID)
	}
	for _, name := range []string{"home_sequence", "recorded_at"} {
		if _, set := obj[name]; set {
			return nil, fmt.Errorf("%w: %s: %s is set by the journal", ErrEvent, e.ID, name)
		}
	}
	kind, _ := obj["kind"].(string)
	if owner, _, _ := strings.Cut(kind, "."); owner != j.pkg {
		return nil, fmt.Errorf("%w: %s: kind %q is not %s's", ErrNotOwner, e.ID, kind, j.pkg)
	}
	if id, _ := obj["home_id"].(string); id != homeID {
		return nil, fmt.Errorf("%w: %s: home_id %q is not this home's", ErrEvent, e.ID, id)
	}
	return obj, nil
}

// repairLine makes the repair event for the torn tail of st.cur: it takes
// the next home_sequence, names the segment and the length and digest of
// what is removed, and is based on the journal's own record.
func (j *Journal) repairLine(st *state, stamp, homeID, correlation string) (*line, error) {
	id, err := ids.Mint(j.clock, ids.Event)
	if err != nil {
		return nil, err
	}
	obj := map[string]any{
		"schema":            eventRecord,
		"schema_version":    eventVersion,
		"event_id":          id,
		"kind":              RepairKind,
		"kind_version":      repairKindVersion,
		"source":            map[string]any{"package": j.pkg, "source_id": RepairSourceID},
		"home_sequence":     st.next,
		"scope_level":       "hive",
		"home_id":           homeID,
		"correlation_id":    correlation,
		"occurred_at":       stamp,
		"observed_at":       stamp,
		"recorded_at":       stamp,
		"occurred_at_known": true,
		"basis": map[string]any{
			"kind": "record",
			"ref":  map[string]any{"schema": RecordSchema, "id": st.cur.name},
		},
		"body": map[string]any{
			"segment":        st.cur.name,
			"removed_bytes":  int64(len(st.tail.torn)),
			"removed_digest": canon.FileDigest(st.tail.torn),
		},
	}
	data, err := canon.Encode(obj)
	if err != nil {
		return nil, fmt.Errorf("journal: repair event: %w", err)
	}
	if err := j.checkEvent(obj, data, st.cur.name); err != nil {
		return nil, fmt.Errorf("journal: repair event: %w", err)
	}
	return &line{id, st.next, append(data, '\n')}, nil
}

// present finds which of the events are already in the journal, by event_id,
// reading the segments from the newest back, and flushes each segment in which
// it finds one. A line is counted only when the event_id is its own, not one
// its body mentions.
func (j *Journal) present(st *state, events []store.Event) (map[string]bool, error) {
	found := map[string]bool{}
	for i := len(st.segs) - 1; i >= 0 && len(found) < len(events); i-- {
		path := filepath.Join(j.dir, st.segs[i].name)
		data, err := store.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%w: journal: %v", store.ErrRecordMalformed, err)
		}
		if i == len(st.segs)-1 && st.tail != nil {
			data = data[:min(st.tail.intact, int64(len(data)))]
		}
		hit := false
		for _, e := range events {
			if !found[e.ID] && holds(data, e.ID) {
				found[e.ID], hit = true, true
			}
		}
		if hit {
			if err := j.syncSegment(st.segs[i].name); err != nil {
				return nil, err
			}
		}
	}
	return found, nil
}

// holds reports whether one of the lines of data is the event with event_id
// id. Canonical form writes the member exactly once per line, so the bytes
// are searched first and the line that holds them is then read to confirm
// that the member is the line's own.
func holds(data []byte, id string) bool {
	needle := []byte(`"event_id":"` + id + `"`)
	for off := 0; ; {
		i := bytes.Index(data[off:], needle)
		if i < 0 {
			return false
		}
		i += off
		start := bytes.LastIndexByte(data[:i], '\n') + 1
		end := bytes.IndexByte(data[i:], '\n')
		if end < 0 {
			end = len(data)
		} else {
			end += i
		}
		if v, err := canon.Parse(data[start:end]); err == nil {
			if obj, ok := v.(map[string]any); ok && obj["event_id"] == id {
				return true
			}
		}
		off = end
	}
}

// writeLine writes data, one line with its line feed, at offset off of the
// segment named name, in one write, and flushes it. With cut set the file is
// cut to end with the line, which removes a torn tail longer than it. A
// segment that is not there yet is created; the directories that hold it are
// flushed once, by the append, after its last line.
func (j *Journal) writeLine(name string, off int64, data []byte, cut bool) error {
	path := filepath.Join(j.dir, name)
	f, err := os.OpenFile(path, os.O_RDWR, fileMode)
	if errNotExist(err) {
		if off != 0 {
			return fmt.Errorf("journal: %s is missing", name)
		}
		f, err = os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, fileMode)
	}
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	if _, err := f.WriteAt(data, off); err != nil {
		f.Close()
		return fmt.Errorf("journal: writing %s: %w", name, err)
	}
	if cut {
		if err := f.Truncate(off + int64(len(data))); err != nil {
			f.Close()
			return fmt.Errorf("journal: cutting %s: %w", name, err)
		}
	}
	if err := j.step("sync " + name); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("journal: flushing %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("journal: closing %s: %w", name, err)
	}
	return nil
}
