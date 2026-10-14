package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/schema"
	cstore "github.com/kesk-git/hive/contract/store"
)

// Write is one record of a transaction: the whole record of the kind and
// identity given, as the revision after Expected. Expected is the revision the
// body was computed from, or 0 for a record that is being created and must be
// absent. A body holds whole numbers as int64.
type Write struct {
	Kind     string
	ID       string
	Body     map[string]any
	Expected int64
}

// Transaction is everything one request writes to pool/: its records, its
// receipts and its events. The recorded outcome is the harness's own, made
// from the response, and the intent is the contract library's, made from
// what is staged here.
type Transaction struct {
	Records  []Write
	Receipts []envelope.Receipt
	Events   []cstore.Event
}

// Staged is a transaction that passed every check and is ready for the
// harness, whose commit compares each expected revision again under the lock
// before it opens a temporary file.
type Staged struct {
	Records  []cstore.RecordWrite
	Receipts []envelope.Receipt
	Events   []cstore.Event
}

// Stage checks a transaction of the call, whose lock the harness holds, and
// writes nothing. In this order it holds every record to the schema of its
// kind and to the no-value check, every receipt and every event to theirs, and
// the intent the contract library will make of them to the schema of an
// intent; and only then compares each expected revision with what is on disk,
// naming nothing it read. The failure it returns is the request's answer:
// schema_violation or secret_shape_recognized for a body that must not be
// written, revision_conflict or already_exists for a stale expectation, and
// nothing in the home has changed in either case.
func Stage(c *cli.Call, tx Transaction) (*Staged, error) {
	if c.Store == nil || c.Store.Package() != Package {
		return nil, envelope.Common(envelope.CodeInternal, "a transaction of the Pool is staged by a mutating operation of the Pool")
	}
	if len(tx.Records)+len(tx.Receipts)+len(tx.Events) == 0 {
		return nil, envelope.Common(envelope.CodeInternal, "a transaction writes something")
	}
	s := &Staged{}
	seen := map[string]bool{}
	for _, w := range tx.Records {
		k, ok := Lookup(w.Kind)
		if !ok {
			return nil, envelope.Common(envelope.CodeInternal, fmt.Sprintf("%q is not a kind of record the Pool stores", w.Kind))
		}
		path, err := k.Path(w.ID)
		if err != nil {
			return nil, envelope.Common(envelope.CodeInternal, "a record of the transaction has no identity it can be stored under").Because(err)
		}
		if seen[path] {
			return nil, envelope.Common(envelope.CodeInternal, "the transaction writes "+path+" twice")
		}
		seen[path] = true
		content, err := canon.Encode(w.Body)
		if err != nil {
			return nil, envelope.Common(envelope.CodeInternal, "a record of the transaction cannot be encoded: "+path).Because(err)
		}
		// The record as the store will write it, revision and time set, is
		// the record held to its schema.
		rw, _, err := c.Finalize(cstore.RecordWrite{Path: path, Content: content, Expected: w.Expected})
		if err != nil {
			return nil, envelope.Common(envelope.CodeInternal, "a record of the transaction cannot be finalized: "+path).Because(err)
		}
		id, _ := k.ID(w.ID)
		if err := k.Check(rw.Content, id, true); err != nil {
			return nil, Failure("the "+k.Name+" record "+id, err)
		}
		s.Records = append(s.Records, rw)
	}
	for i := range tx.Receipts {
		r := tx.Receipts[i]
		data, err := r.Encode()
		if err != nil {
			return nil, checkFailure("the receipt "+r.ReceiptID, err)
		}
		if err := checkValues(data); err != nil {
			return nil, checkFailure("the receipt "+r.ReceiptID, err)
		}
		if err := checkBody(receiptSchemaPrefix, r.Kind, r.KindVersion, r.Body); err != nil {
			return nil, checkFailure("the receipt "+r.ReceiptID, err)
		}
		path := cstore.ReceiptPath(Package, r.ReceiptID)
		if seen[path] {
			return nil, envelope.Common(envelope.CodeInternal, "the transaction writes "+path+" twice")
		}
		seen[path] = true
		s.Receipts = append(s.Receipts, r)
	}
	events := map[string]bool{}
	for _, e := range tx.Events {
		if events[e.ID] {
			return nil, envelope.Common(envelope.CodeInternal, "the transaction names event "+e.ID+" twice")
		}
		events[e.ID] = true
		if err := checkEvent(c, e); err != nil {
			return nil, checkFailure("the event "+e.ID, err)
		}
		s.Events = append(s.Events, e)
	}
	if err := checkIntent(c, s); err != nil {
		return nil, checkFailure("the intent of the transaction", err)
	}
	for _, rw := range s.Records {
		if err := compare(c.Store, rw); err != nil {
			return nil, Failure(rw.Path, err)
		}
	}
	return s, nil
}

// checkFailure is the failure of a receipt, an event or the intent that is not
// as the Pool's own code made it: a body that breaks its schema or holds a
// credential shape is the request's, as for a record, and any other fault of
// the document is the program's.
func checkFailure(what string, err error) *envelope.Failure {
	var shape *schema.ShapeError
	if violation(err) || errors.As(err, &shape) {
		return Failure(what, err)
	}
	return envelope.Common(envelope.CodeInternal, what+" is not one the Pool can write").Because(err)
}

// Result is the handler's result for ok, carrying the staged transaction: the
// harness commits it with the recorded outcome as one transaction.
func (s *Staged) Result(body map[string]any) *cli.Result {
	return &cli.Result{Body: body, Records: s.Records, Receipts: s.Receipts, Events: s.Events}
}

// compare checks that the record at the path of rw is at the revision rw
// expects, or absent when it expects none. It is the harness's own comparison,
// made once more here so that a stale expectation is answered before the
// transaction is handed on.
func compare(st *cstore.Store, rw cstore.RecordWrite) error {
	if rw.Expected == 0 {
		full, err := st.Path(rw.Path)
		if err != nil {
			return err
		}
		_, err = os.Lstat(full)
		switch {
		case err == nil:
			return fmt.Errorf("%w: %s", cstore.ErrAlreadyExists, rw.Path)
		case errors.Is(err, fs.ErrNotExist):
			return nil
		}
		return err
	}
	cur, err := st.ReadRecord(rw.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %s expected at revision %d, found none", cstore.ErrRevisionConflict, rw.Path, rw.Expected)
	case err != nil:
		return err
	case cur.Revision != rw.Expected:
		return fmt.Errorf("%w: %s expected at revision %d, found %d", cstore.ErrRevisionConflict, rw.Path, rw.Expected, cur.Revision)
	}
	return nil
}

// checkValues applies the no-value check to a document whose own schema has
// been checked: every string of it, in a value or in a member name.
func checkValues(data []byte) error {
	v, err := canon.Parse(data)
	if err != nil {
		return err
	}
	return schema.CheckShapes(v)
}

// The schemas an event and a receipt are held to, as the journal and the
// receipt's own schema hold them: the body schema of a kind is named by the
// prefix, the kind and the major of its version.
const (
	eventSchema         = "urn:hive:schema:event:v1"
	eventSchemaPrefix   = "urn:hive:schema:events:"
	receiptSchemaPrefix = "urn:hive:schema:receipts:"
)

// checkEvent holds an event of the Pool to what the journal will hold it to
// when it appends, so that the journal's refusal never comes after the intent
// is written: the envelope, its first words, the body its kind registers or no
// body where it registers none, and the no-value check. home_sequence and
// recorded_at are the journal's to set.
func checkEvent(c *cli.Call, e cstore.Event) error {
	v, err := canon.Parse(e.Content)
	if err != nil {
		return err
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return errors.New("an event is a JSON object")
	}
	for _, name := range []string{"home_sequence", "recorded_at"} {
		if _, ok := obj[name]; ok {
			return fmt.Errorf("%s is set by the journal", name)
		}
	}
	if obj["event_id"] != e.ID {
		return errors.New("the event_id of the event is not the one it is named by")
	}
	stamp, err := clock.Stamp(c.Clock)
	if err != nil {
		return err
	}
	obj["home_sequence"], obj["recorded_at"] = int64(1), stamp
	full, err := canon.Encode(obj)
	if err != nil {
		return err
	}
	reg, err := Schemas()
	if err != nil {
		return err
	}
	if err := reg.ValidateOwned(eventSchema, full); err != nil {
		return err
	}
	if id, _ := obj["home_id"].(string); c.Home == nil || id != c.Home.HomeID {
		return fmt.Errorf("home_id %q is not this home's", id)
	}
	for _, name := range []string{"occurred_at", "observed_at"} {
		if s, _ := obj[name].(string); !clock.Valid(s) {
			return fmt.Errorf("%s is not a timestamp", name)
		}
	}
	if known, ok := obj["occurred_at_known"].(bool); ok && !known && obj["occurred_at"] != obj["observed_at"] {
		return errors.New("occurred_at is unknown, so it must repeat observed_at")
	}
	kind, _ := obj["kind"].(string)
	source, _ := obj["source"].(map[string]any)
	if owner, _, _ := strings.Cut(kind, "."); owner != Package || source["package"] != Package {
		return fmt.Errorf("kind %s is not an event of the Pool", kind)
	}
	if id, _ := source["source_id"].(string); id != Package && !strings.HasPrefix(id, Package+":") {
		return fmt.Errorf("source_id %s is not a source of the Pool", id)
	}
	kindVersion, _ := obj["kind_version"].(string)
	body, _ := obj["body"].(map[string]any)
	return checkBody(eventSchemaPrefix, kind, kindVersion, body)
}

// checkBody holds the body of an event or a receipt to the schema its kind
// registers, or to being empty where it registers none.
func checkBody(prefix, kind, kindVersion string, body map[string]any) error {
	reg, err := Schemas()
	if err != nil {
		return err
	}
	major, _, _ := strings.Cut(kindVersion, ".")
	data, err := canon.Encode(body)
	if err != nil {
		return err
	}
	switch err := reg.ValidateOwned(prefix+kind+":v"+major, data); {
	case errors.Is(err, schema.ErrUnknownSchema):
		if len(body) > 0 {
			return fmt.Errorf("kind %s registers no body schema, so its body is empty", kind)
		}
	case err != nil:
		return err
	}
	return nil
}

// checkIntent makes the intent the contract library will write for the staged
// transaction, without the outcome the harness adds, and holds it to the
// schema of an intent: what goes to tx/ is the first place the bodies reach
// disk. The digests of the preview are those of the canonical bodies.
func checkIntent(c *cli.Call, s *Staged) error {
	file := func(path string, content []byte, expected int64, once bool) (map[string]any, error) {
		body, err := canon.Parse(content)
		if err != nil {
			return nil, err
		}
		m := map[string]any{"path": path, "digest": canon.FileDigest(bytesOf(body)), "body": body}
		if once || expected == 0 {
			m["expected"] = "absent"
		} else {
			m["expected"], m["expected_revision"] = "revision", expected
		}
		return m, nil
	}
	records := make([]any, 0, len(s.Records))
	for _, r := range s.Records {
		m, err := file(r.Path, r.Content, r.Expected, false)
		if err != nil {
			return err
		}
		records = append(records, m)
	}
	receipts := make([]any, 0, len(s.Receipts))
	for i := range s.Receipts {
		data, err := s.Receipts[i].Encode()
		if err != nil {
			return err
		}
		m, err := file(cstore.ReceiptPath(Package, s.Receipts[i].ReceiptID), data, 0, true)
		if err != nil {
			return err
		}
		receipts = append(receipts, m)
	}
	events := make([]any, 0, len(s.Events))
	for _, e := range s.Events {
		body, err := canon.Parse(e.Content)
		if err != nil {
			return err
		}
		events = append(events, map[string]any{"event_id": e.ID, "body": body})
	}
	doc, err := canon.Encode(map[string]any{
		"schema": "hive.tx-intent", "schema_version": "1.0", "request_id": c.Request.RequestID,
		"package": Package, "records": records, "receipts": receipts, "events": events,
	})
	if err != nil {
		return err
	}
	reg, err := Schemas()
	if err != nil {
		return err
	}
	return reg.Validate("urn:hive:schema:tx-intent:v1", doc)
}

// bytesOf is the canonical form of a parsed document.
func bytesOf(v any) []byte {
	out, _ := canon.Encode(v)
	return out
}
