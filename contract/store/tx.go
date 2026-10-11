package store

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/ids"
)

// Tx is a mutation of several files of one store, written as one
// transaction (spec/records.md, Writing several records). Its outcome is
// recorded under requests/, its receipts under receipts/, and its events go
// to the store's Journal.
type Tx struct {
	// RequestID is the request that makes the mutation. It names the intent
	// and the recorded outcome.
	RequestID string
	// Records are the records to replace or create.
	Records []RecordWrite
	// Receipts are the receipts to write, each once.
	Receipts []Receipt
	// Events are the events to append to the journal, each once.
	Events []Event
	// Outcome is the recorded outcome of the request, a JSON object, written
	// to requests/<request_id>.json.
	Outcome []byte
}

// RecordWrite is one record of a transaction. Content and Expected are as
// for Writer.Replace.
type RecordWrite struct {
	Path     string
	Content  []byte
	Expected int64
}

// Receipt is one receipt of a transaction: its receipt_id and its content, a
// JSON object.
type Receipt struct {
	ID      string
	Content []byte
}

// Event is one event of a transaction: its event_id and its content, a JSON
// object, in canonical form when the store hands it to the Journal.
type Event struct {
	ID      string
	Content []byte
}

// Committed describes what a transaction wrote.
type Committed struct {
	Records []Written
}

// The intent record. It has no schema file of its own; its shape is fixed
// here and read strictly.
const (
	intentSchema  = "hive.tx-intent"
	intentVersion = "1.0"
)

// intent is a transaction as written to tx/<request_id>.json: every file to
// be written with its expected state and the digest of its new content, and
// every event. The new content is held in the intent, so that recovery can
// write it.
type intent struct {
	request  string
	records  []*intentFile
	receipts []*intentFile
	events   []Event
	outcome  *intentFile
}

// intentFile is one file of an intent. A record being created expects to be
// absent; one being replaced expects a revision. A receipt and the outcome
// always expect to be absent.
type intentFile struct {
	prepared
	absent   bool
	expected int64
	skip     bool // set by recovery for a file already at the intended digest
}

// Commit runs the transaction: it checks every expected state, writes the
// intent, writes the records, the receipts, the events and the outcome, and
// removes the intent. Nothing is written when a check fails. An
// interruption after the intent is written is completed by the next Lock.
func (w *Writer) Commit(tx Tx) (*Committed, error) {
	if w.lock == nil {
		return nil, errReleased
	}
	in, err := w.s.plan(tx)
	if err != nil {
		return nil, err
	}
	if len(in.events) > 0 && w.s.journal == nil {
		return nil, ErrNoJournal
	}
	data, err := w.s.encodeIntent(in)
	if err != nil {
		return nil, err
	}
	if err := w.s.writeFile(IntentPath(in.request), in.request, data); err != nil {
		return nil, err
	}
	if err := w.s.complete(in); err != nil {
		return nil, err
	}
	out := &Committed{}
	for _, r := range in.records {
		out.Records = append(out.Records, Written{r.rel, r.expected + 1, r.digest})
	}
	return out, nil
}

// plan checks a transaction against the store and prepares every file,
// writing nothing: every identifier and path is well formed and named once,
// every record is at its expected revision or absent as expected, and every
// receipt and the outcome are absent.
func (s *Store) plan(tx Tx) (*intent, error) {
	if err := ids.Check(tx.RequestID, ids.Request); err != nil {
		return nil, err
	}
	in := &intent{request: tx.RequestID}
	seen := map[string]bool{}
	once := func(rel string) error {
		if seen[rel] {
			return fmt.Errorf("store: the transaction names %s twice", rel)
		}
		seen[rel] = true
		return nil
	}
	for _, r := range tx.Records {
		if err := checkRecordPath(r.Path); err != nil {
			return nil, err
		}
		if err := once(r.Path); err != nil {
			return nil, err
		}
		if err := s.expectRevision(r.Path, r.Expected); err != nil {
			return nil, err
		}
		p, err := s.prepareRecord(r.Path, r.Content, r.Expected+1)
		if err != nil {
			return nil, err
		}
		in.records = append(in.records, &intentFile{prepared: *p, absent: r.Expected == 0, expected: r.Expected})
	}
	for _, r := range tx.Receipts {
		if err := ids.Check(r.ID, ids.Receipt); err != nil {
			return nil, err
		}
		rel := ReceiptPath(r.ID)
		if err := once(rel); err != nil {
			return nil, err
		}
		if err := s.expectAbsent(rel); err != nil {
			return nil, err
		}
		p, err := s.prepareOnce(rel, r.Content)
		if err != nil {
			return nil, err
		}
		in.receipts = append(in.receipts, &intentFile{prepared: *p, absent: true})
	}
	eventIDs := map[string]bool{}
	for _, e := range tx.Events {
		if err := ids.Check(e.ID, ids.Event); err != nil {
			return nil, err
		}
		if eventIDs[e.ID] {
			return nil, fmt.Errorf("store: the transaction names event %s twice", e.ID)
		}
		eventIDs[e.ID] = true
		obj, err := parseObject("event "+e.ID, e.Content)
		if err != nil {
			return nil, err
		}
		out, err := canon.Encode(obj)
		if err != nil {
			return nil, fmt.Errorf("store: event %s: %w", e.ID, err)
		}
		in.events = append(in.events, Event{e.ID, out})
	}
	if len(tx.Outcome) == 0 {
		return nil, fmt.Errorf("store: the transaction has no outcome to record")
	}
	rel := OutcomePath(tx.RequestID)
	if err := s.expectAbsent(rel); err != nil {
		return nil, err
	}
	p, err := s.prepareOnce(rel, tx.Outcome)
	if err != nil {
		return nil, err
	}
	in.outcome = &intentFile{prepared: *p, absent: true}
	return in, nil
}

// complete writes what an intent says, in the order spec/records.md fixes,
// and removes the intent. A file recovery marked as already written is left.
func (s *Store) complete(in *intent) error {
	for _, r := range in.records {
		if r.skip {
			continue
		}
		if err := s.writeFile(r.rel, in.request, r.data); err != nil {
			return err
		}
	}
	for _, r := range in.receipts {
		if r.skip {
			continue
		}
		if err := s.writeFile(r.rel, in.request, r.data); err != nil {
			return err
		}
	}
	if len(in.events) > 0 {
		if s.journal == nil {
			return ErrNoJournal
		}
		if err := s.step("events " + IntentPath(in.request)); err != nil {
			return err
		}
		if err := s.journal.Append(slices.Clone(in.events)); err != nil {
			return fmt.Errorf("store: appending the events of %s: %w", in.request, err)
		}
	}
	if !in.outcome.skip {
		if err := s.writeFile(in.outcome.rel, in.request, in.outcome.data); err != nil {
			return err
		}
	}
	return s.removeFile(IntentPath(in.request))
}

// recover brings the store to a consistent state under the exclusive lock:
// it rolls every intent forward and removes every temporary file. Intents
// are completed in name order, each in full before the next.
func (s *Store) recover() error {
	entries, err := os.ReadDir(filepath.Join(s.dir, txDir))
	if err != nil && !errNotExist(err) {
		return fmt.Errorf("store: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasSuffix(name, ".json") {
			continue
		}
		rel := txDir + "/" + name
		in, err := s.readIntent(rel)
		if err != nil {
			return err
		}
		if err := s.classify(rel, in); err != nil {
			return err
		}
		if err := s.complete(in); err != nil {
			return err
		}
	}
	return s.sweepTemps()
}

// classify decides, for each file of an intent, whether recovery writes it
// or leaves it (spec/records.md, Writing several records): a file already at
// the intended digest is left; a record at its expected revision, or absent
// as expected, is written; a receipt or outcome absent is written; anything
// else stops the store for that transaction. The decision for every file is
// made before any is written, so a stopped store is left as it was found.
func (s *Store) classify(intentRel string, in *intent) error {
	for _, r := range in.records {
		cur, err := s.ReadRecord(r.rel)
		switch {
		case err == nil && cur.Digest == r.digest:
			r.skip = true
		case errNotExist(err) && r.absent:
		case err == nil && !r.absent && cur.Revision == r.expected:
		case err == nil:
			return fmt.Errorf("%w: intent %s: %s is at revision %d, neither the expected %d nor the intended content", ErrRecordMalformed, intentRel, r.rel, cur.Revision, r.expected)
		case errNotExist(err):
			return fmt.Errorf("%w: intent %s: %s expected at revision %d is absent", ErrRecordMalformed, intentRel, r.rel, r.expected)
		default:
			return fmt.Errorf("%w: intent %s: %v", ErrRecordMalformed, intentRel, err)
		}
	}
	for _, r := range append(slices.Clone(in.receipts), in.outcome) {
		data, err := s.readFile(r.rel)
		switch {
		case errNotExist(err):
		case err != nil:
			return fmt.Errorf("%w: intent %s: %v", ErrRecordMalformed, intentRel, err)
		default:
			digest, derr := canon.RecordDigest(data)
			if derr != nil || digest != r.digest {
				return fmt.Errorf("%w: intent %s: %s is present with other content", ErrRecordMalformed, intentRel, r.rel)
			}
			r.skip = true
		}
	}
	return nil
}

// encodeIntent writes an intent as a record file.
func (s *Store) encodeIntent(in *intent) ([]byte, error) {
	file := func(f *intentFile) (map[string]any, error) {
		content, err := canon.Parse(f.data)
		if err != nil {
			return nil, err
		}
		m := map[string]any{"path": f.rel, "digest": f.digest, "content": content}
		if f.absent {
			m["expected"] = "absent"
		} else {
			m["expected"] = "revision"
			m["expected_revision"] = f.expected
		}
		return m, nil
	}
	list := func(fs []*intentFile) ([]any, error) {
		out := make([]any, 0, len(fs))
		for _, f := range fs {
			m, err := file(f)
			if err != nil {
				return nil, err
			}
			out = append(out, m)
		}
		return out, nil
	}
	records, err := list(in.records)
	if err != nil {
		return nil, err
	}
	receipts, err := list(in.receipts)
	if err != nil {
		return nil, err
	}
	events := make([]any, 0, len(in.events))
	for _, e := range in.events {
		content, err := canon.Parse(e.Content)
		if err != nil {
			return nil, err
		}
		events = append(events, map[string]any{"event_id": e.ID, "content": content})
	}
	outcome, err := file(in.outcome)
	if err != nil {
		return nil, err
	}
	doc := map[string]any{
		"schema":         intentSchema,
		"schema_version": intentVersion,
		"request_id":     in.request,
		"package":        s.pkg,
		"records":        records,
		"receipts":       receipts,
		"events":         events,
		"outcome":        outcome,
	}
	out, err := canon.Encode(doc)
	if err != nil {
		return nil, fmt.Errorf("store: intent: %w", err)
	}
	return append(out, '\n'), nil
}

// readIntent reads the intent at rel strictly. A torn or otherwise malformed
// intent is ErrRecordMalformed naming it; it is never discarded.
func (s *Store) readIntent(rel string) (*intent, error) {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: intent %s: %s", ErrRecordMalformed, rel, fmt.Sprintf(format, args...))
	}
	data, err := s.readFile(rel)
	if err != nil {
		return nil, bad("%v", err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		return nil, bad("%v", err)
	}
	doc, ok := v.(map[string]any)
	if !ok {
		return nil, bad("not a JSON object")
	}
	if doc["schema"] != intentSchema || doc["schema_version"] != intentVersion {
		return nil, bad("not a %s %s record", intentSchema, intentVersion)
	}
	if doc["package"] != s.pkg {
		return nil, bad("the intent is for another store")
	}
	request, _ := doc["request_id"].(string)
	if ids.Check(request, ids.Request) != nil || IntentPath(request) != rel {
		return nil, bad("request_id does not name the intent")
	}
	for _, key := range []string{"records", "receipts", "events", "outcome"} {
		if _, ok := doc[key]; !ok {
			return nil, bad("%s is missing", key)
		}
	}
	if len(doc) != 8 {
		return nil, bad("unknown member")
	}
	in := &intent{request: request}
	file := func(v any, record bool) (*intentFile, error) {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, bad("a file entry is not an object")
		}
		rel, _ := m["path"].(string)
		if err := checkPath(rel); err != nil {
			return nil, bad("%v", err)
		}
		if record != (checkRecordPath(rel) == nil) {
			return nil, bad("%s is in the wrong list", rel)
		}
		digest, _ := m["digest"].(string)
		if !canon.ValidDigest(digest) {
			return nil, bad("%s has no digest", rel)
		}
		content, ok := m["content"].(map[string]any)
		if !ok {
			return nil, bad("%s has no content", rel)
		}
		out, err := canon.Encode(content)
		if err != nil || canon.FileDigest(out) != digest {
			return nil, bad("the content of %s does not match its digest", rel)
		}
		f := &intentFile{prepared: prepared{rel, append(out, '\n'), digest}}
		switch expected := m["expected"]; {
		case expected == "absent" && len(m) == 4:
			f.absent = true
		case expected == "revision" && record && len(m) == 5:
			rev, ok := m["expected_revision"].(int64)
			if !ok || ids.CheckNumber(rev) != nil {
				return nil, bad("%s has no expected revision", rel)
			}
			f.expected = rev
		default:
			return nil, bad("%s has no expected state", rel)
		}
		return f, nil
	}
	list := func(key string, record bool) ([]*intentFile, error) {
		arr, ok := doc[key].([]any)
		if !ok {
			return nil, bad("%s is not a list", key)
		}
		var out []*intentFile
		for _, v := range arr {
			f, err := file(v, record)
			if err != nil {
				return nil, err
			}
			out = append(out, f)
		}
		return out, nil
	}
	if in.records, err = list("records", true); err != nil {
		return nil, err
	}
	if in.receipts, err = list("receipts", false); err != nil {
		return nil, err
	}
	for _, r := range in.receipts {
		if dir, _, _ := strings.Cut(r.rel, "/"); dir != receiptsDir {
			return nil, bad("%s is not a receipt", r.rel)
		}
	}
	events, ok := doc["events"].([]any)
	if !ok {
		return nil, bad("events is not a list")
	}
	for _, v := range events {
		m, ok := v.(map[string]any)
		if !ok || len(m) != 2 {
			return nil, bad("an event entry is not an event")
		}
		id, _ := m["event_id"].(string)
		if ids.Check(id, ids.Event) != nil {
			return nil, bad("an event has no event_id")
		}
		content, ok := m["content"].(map[string]any)
		if !ok {
			return nil, bad("event %s has no content", id)
		}
		out, err := canon.Encode(content)
		if err != nil {
			return nil, bad("event %s: %v", id, err)
		}
		in.events = append(in.events, Event{id, out})
	}
	if in.outcome, err = file(doc["outcome"], false); err != nil {
		return nil, err
	}
	if in.outcome.rel != OutcomePath(request) {
		return nil, bad("the outcome is not that of the request")
	}
	return in, nil
}
