package admission

import (
	"errors"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	cstore "github.com/kesk-git/hive/contract/store"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The words the records, receipts and events of create and check name things
// by. The version of every receipt kind and of the records is 1.0, the one
// versionOne of approve.go names.
const (
	schemaReceipt = "hive.receipt"
	schemaRequest = "hive.request"

	receiptCreated  = "pool.admission.created"
	receiptAdmitted = "pool.admission.admitted"
	receiptRefused  = "pool.admission.refused"
	receiptHeld     = "pool.admission.held"

	// eventChecked is the one event of the three check receipts, and the other
	// two are those of the Pool's hold writers.
	eventChecked  = "pool.admission.checked"
	eventPlaced   = "pool.hold.placed"
	eventReleased = "pool.hold.released"
)

func internal(what string, err error) *envelope.Failure {
	return envelope.Common(envelope.CodeInternal, what).Because(err)
}

func malformed(what, id string) *envelope.Failure {
	return envelope.Common(envelope.CodeRecordMalformed, what).With("record", id)
}

func notFound(what string) *envelope.Failure {
	return envelope.Common(envelope.CodeNotFound, "no "+what+" has that identity")
}

func str(v any) string { s, _ := v.(string); return s }

func tree(v any) map[string]any { m, _ := v.(map[string]any); return m }

func list(v any) []any { l, _ := v.([]any); return l }

func integer(v any) int64 { n, _ := v.(int64); return n }

// scopeOf is the scope a record of the Pool carries.
func scopeOf(rec map[string]any) envelope.Scope {
	s := envelope.Scope{}
	s.Level, _ = rec["scope_level"].(string)
	s.HomeID, _ = rec["home_id"].(string)
	s.WorkspaceID, _ = rec["workspace_id"].(string)
	s.ProjectID, _ = rec["project_id"].(string)
	s.CloneID, _ = rec["clone_id"].(string)
	return s
}

// putScope writes the scope's flat members into a record or an event. A hold
// does not carry the clone, and an event is given the task beside the scope.
func putScope(m map[string]any, s envelope.Scope, clone bool) {
	m["scope_level"], m["home_id"] = s.Level, s.HomeID
	if s.WorkspaceID != "" {
		m["workspace_id"] = s.WorkspaceID
	}
	if s.ProjectID != "" {
		m["project_id"] = s.ProjectID
	}
	if clone && s.CloneID != "" {
		m["clone_id"] = s.CloneID
	}
}

func writerTree(c *cli.Call) map[string]any {
	w := c.Writer()
	return map[string]any{"package": w.Package, "command": w.Command, "operation": w.Operation, "implementation_version": w.ImplementationVersion}
}

// builder gathers the transaction of one request.
type builder struct {
	c  *cli.Call
	tx poolstore.Transaction
}

// put adds the record to the transaction, finalized, and returns it with the
// reference a receipt names it by.
func (b *builder) put(kindName, id string, body map[string]any, expected int64) (map[string]any, *envelope.RecordRef, error) {
	body["writer"] = writerTree(b.c)
	k, ok := poolstore.Lookup(kindName)
	if !ok {
		return nil, nil, internal("a record cannot be finalized", errors.New(kindName+" is not a kind"))
	}
	path, err := k.Path(id)
	if err != nil {
		return nil, nil, internal("a record has no path", err)
	}
	content, err := canon.Encode(body)
	if err != nil {
		return nil, nil, internal("a record cannot be encoded", err)
	}
	rw, digest, err := b.c.Finalize(cstore.RecordWrite{Path: path, Content: content, Expected: expected})
	if err != nil {
		return nil, nil, internal("a record cannot be finalized", err)
	}
	v, err := canon.Parse(rw.Content)
	if err != nil {
		return nil, nil, internal("a record cannot be finalized", err)
	}
	final := v.(map[string]any)
	b.tx.Records = append(b.tx.Records, poolstore.Write{Kind: kindName, ID: id, Body: final, Expected: expected})
	return final, &envelope.RecordRef{Schema: k.Schema, ID: id, HomeID: b.c.Home.HomeID, RecordRevision: expected + 1, Digest: digest}, nil
}

// receipt makes the receipt of the kind for this call, in the scope given. Its
// subject is set by seal, once the record it is about is final.
func (b *builder) receipt(kind string, evidence []envelope.Evidence) (envelope.Receipt, error) {
	r, err := b.c.NewReceipt(kind, versionOne, nil, evidence, nil)
	if err != nil {
		return r, internal("a receipt cannot be made", err)
	}
	return r, nil
}

// seal adds the receipt, about the record, in the scope given, and with it the
// event of the kind where there is one.
func (b *builder) seal(r envelope.Receipt, subject *envelope.RecordRef, scope envelope.Scope, event string) error {
	r.Subject = subject
	r.Scope = scope
	b.tx.Receipts = append(b.tx.Receipts, r)
	if event == "" {
		return nil
	}
	return b.event(event, r.ReceiptID, scope)
}

// event appends the event of the kind, with an empty body, whose basis is the
// receipt, in the scope given.
func (b *builder) event(kind, receiptID string, scope envelope.Scope) error {
	eventID, err := ids.Mint(b.c.Clock, ids.Event)
	if err != nil {
		return internal("an event cannot be made", err)
	}
	now, err := b.c.Stamp()
	if err != nil {
		return internal("an event cannot be made", err)
	}
	e := map[string]any{
		"schema": "hive.event", "schema_version": versionOne, "event_id": eventID,
		"kind": kind, "kind_version": versionOne,
		"source":         map[string]any{"package": poolstore.Package, "source_id": poolstore.Package},
		"correlation_id": b.c.Request.CorrelationID,
		"causation":      map[string]any{"schema": schemaRequest, "id": b.c.Request.RequestID},
		"occurred_at":    now, "observed_at": now,
		"basis": map[string]any{"kind": "receipt", "ref": map[string]any{"schema": schemaReceipt, "id": receiptID}},
		"body":  map[string]any{},
	}
	putScope(e, scope, true)
	e["task_id"] = scope.TaskID
	content, err := canon.Encode(e)
	if err != nil {
		return internal("an event cannot be written", err)
	}
	b.tx.Events = append(b.tx.Events, cstore.Event{ID: eventID, Content: content})
	return nil
}

// stage checks the transaction. No record is staged that lies outside the
// scope of the request.
func (b *builder) stage() (*poolstore.Staged, error) {
	for _, w := range b.tx.Records {
		if err := checkScope(b.c, scopeOf(w.Body)); err != nil {
			return nil, err
		}
	}
	return poolstore.Stage(b.c, b.tx)
}
