// Package item builds the Pool's items and the holds that are not the decision
// intake's (spec/pool.md, Items and Holds): the operations add, update,
// transition, hold and release of hive-pool, on the transaction boundary of
// the store, and the readers of the kinds item and hold with the derived facts
// computed at the read. The package registers them from its init function, so
// that importing it builds them into the executable.
package item

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/read"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The codes of the Pool that the operations of this package return
// (spec/pool.md, Error codes), and the words the records and events they write
// name things by.
const (
	codeStateTransition         = "pool.state_transition"
	codeBlocked                 = "pool.blocked"
	codeDependencyMissing       = "pool.dependency_missing"
	codeDependencyActive        = "pool.dependency_active"
	codeGenerationOpen          = "pool.generation_open"
	codeNotDispatchable         = "pool.not_dispatchable"
	codeHierarchy               = "pool.hierarchy"
	codeDerivedState            = "pool.derived_state"
	codeSpecificationUnaccepted = "pool.specification_unaccepted"

	versionOne           = "1.0"
	schemaReceipt        = "hive.receipt"
	schemaRequest        = "hive.request"
	definitionReasonCode = "specification_unaccepted"
	definitionHoldReason = "A specification is required and none is accepted for this item."
)

func refuse(code, message string) *envelope.Failure {
	return envelope.Fail(envelope.Refused, code, message)
}

func hierarchy(message string) *envelope.Failure { return refuse(codeHierarchy, message) }

func notFound(what string) *envelope.Failure {
	return envelope.Common(envelope.CodeNotFound, "no "+what+" has that identity")
}

func conflict() *envelope.Failure {
	return envelope.Common(envelope.CodeRevisionConflict, "the record is not at the revision the request expects")
}

// writerTree is the writer of a record this call makes.
func writerTree(c *cli.Call) map[string]any {
	w := c.Writer()
	return map[string]any{
		"package": w.Package, "command": w.Command, "operation": w.Operation,
		"implementation_version": w.ImplementationVersion,
	}
}

// scopeOf is the scope an item's record carries.
func scopeOf(rec map[string]any) envelope.Scope {
	s := envelope.Scope{}
	s.Level, _ = rec["scope_level"].(string)
	s.HomeID, _ = rec["home_id"].(string)
	s.WorkspaceID, _ = rec["workspace_id"].(string)
	s.ProjectID, _ = rec["project_id"].(string)
	s.CloneID, _ = rec["clone_id"].(string)
	return s
}

// putScope writes the scope's flat members into a record or an event, without
// the clone, which a hold does not carry.
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

// copyValue is a deep copy of a decoded JSON value.
func copyValue(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = copyValue(x)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = copyValue(x)
		}
		return out
	}
	return v
}

func copyMap(m map[string]any) map[string]any { return copyValue(m).(map[string]any) }

// equalValues reports whether two decoded values are the same document.
func equalValues(a, b any) bool {
	x, err1 := canon.Encode(map[string]any{"v": a})
	y, err2 := canon.Encode(map[string]any{"v": b})
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// finalize is the record as the store will write it at the revision after
// expected, with the digest of that record. A body that names its own digest in
// a receipt takes it from here, so that what is named is what is stored.
func finalize(c *cli.Call, kind, id string, body map[string]any, expected int64) (map[string]any, string, error) {
	k, ok := poolstore.Lookup(kind)
	if !ok {
		return nil, "", fmt.Errorf("item: %q is not a kind", kind)
	}
	path, err := k.Path(id)
	if err != nil {
		return nil, "", err
	}
	content, err := canon.Encode(body)
	if err != nil {
		return nil, "", err
	}
	rw, digest, err := c.Finalize(cstore.RecordWrite{Path: path, Content: content, Expected: expected})
	if err != nil {
		return nil, "", err
	}
	v, err := canon.Parse(rw.Content)
	if err != nil {
		return nil, "", err
	}
	return v.(map[string]any), digest, nil
}

// builder gathers the transaction of one request.
type builder struct {
	c  *cli.Call
	tx poolstore.Transaction
}

// put adds the record to the transaction, finalized, and returns it with the
// reference a receipt names it by.
func (b *builder) put(kind, id string, body map[string]any, expected int64) (map[string]any, *envelope.RecordRef, error) {
	body["writer"] = writerTree(b.c)
	final, digest, err := finalize(b.c, kind, id, body, expected)
	if err != nil {
		return nil, nil, internal("a record cannot be finalized", err)
	}
	b.tx.Records = append(b.tx.Records, poolstore.Write{Kind: kind, ID: id, Body: final, Expected: expected})
	schema := "pool." + kind
	ref := &envelope.RecordRef{Schema: schema, ID: id, HomeID: b.c.Home.HomeID, RecordRevision: expected + 1, Digest: digest}
	return final, ref, nil
}

// receipt adds the receipt of the kind about the subject, with the event the
// Pool registers beside it, to the transaction. Both take the subject's scope
// and task_id.
func (b *builder) receipt(kind string, subject *envelope.RecordRef, scope envelope.Scope, taskID string, evidence []envelope.Evidence) (string, error) {
	return b.receiptAs("", kind, subject, scope, taskID, evidence)
}

// receiptAs is receipt with the identity of the receipt minted beforehand, as a
// record that names the receipt which establishes it needs. An empty identity
// is minted here.
func (b *builder) receiptAs(receiptID, kind string, subject *envelope.RecordRef, scope envelope.Scope, taskID string, evidence []envelope.Evidence) (string, error) {
	r, err := b.c.NewReceipt(kind, versionOne, subject, evidence, nil)
	if err != nil {
		return "", internal("a receipt cannot be made", err)
	}
	if receiptID != "" {
		r.ReceiptID = receiptID
	}
	r.Scope = scope
	r.Scope.TaskID = taskID
	b.tx.Receipts = append(b.tx.Receipts, r)
	eventID, err := ids.Mint(b.c.Clock, ids.Event)
	if err != nil {
		return "", internal("an event cannot be made", err)
	}
	now, err := b.c.Stamp()
	if err != nil {
		return "", internal("an event cannot be made", err)
	}
	event := map[string]any{
		"schema": "hive.event", "schema_version": versionOne, "event_id": eventID,
		"kind": kind, "kind_version": versionOne,
		"source":         map[string]any{"package": poolstore.Package, "source_id": poolstore.Package},
		"correlation_id": b.c.Request.CorrelationID,
		"causation":      map[string]any{"schema": schemaRequest, "id": b.c.Request.RequestID},
		"occurred_at":    now, "observed_at": now,
		"basis": map[string]any{"kind": "receipt", "ref": map[string]any{"schema": schemaReceipt, "id": r.ReceiptID}},
		"body":  map[string]any{},
	}
	putScope(event, scope, true)
	event["task_id"] = taskID
	content, err := canon.Encode(event)
	if err != nil {
		return "", internal("an event cannot be written", err)
	}
	b.tx.Events = append(b.tx.Events, cstore.Event{ID: eventID, Content: content})
	return r.ReceiptID, nil
}

// stage checks the transaction and returns the handler's result. No record is
// staged that lies outside the scope of the request, a cascaded one included.
func (b *builder) stage(body map[string]any) (*cli.Result, error) {
	for _, w := range b.tx.Records {
		if err := checkScope(b.c, w.Body); err != nil {
			return nil, err
		}
	}
	staged, err := poolstore.Stage(b.c, b.tx)
	if err != nil {
		return nil, err
	}
	return staged.Result(body), nil
}

func internal(what string, err error) *envelope.Failure {
	return envelope.Common(envelope.CodeInternal, what).Because(err)
}

// loadGraph reads the items and the holds under the lock the operation holds.
// A record that cannot be read ends the request: a guard that reads the
// records above an item cannot be trusted over a hole in them.
func loadGraph(c *cli.Call) (*read.Graph, error) {
	g, omitted, err := read.LoadGraph(c.Store, c.Clock.Now())
	if err != nil {
		return nil, poolstore.Failure("the Pool", err)
	}
	if len(omitted) > 0 {
		return nil, envelope.Common(envelope.CodeRecordMalformed, "a record of the Pool cannot be read, so the facts of an item cannot be known").With("record", omitted[0].ID)
	}
	return g, nil
}

// checkScope refuses a request whose scope does not contain the scope of the
// record it acts on, before anything is written: a hive-scope request contains
// every record of its home, a workspace-scope request its workspace and that
// workspace's projects, a project-scope request only that project. The clone is
// not compared.
func checkScope(c *cli.Call, rec map[string]any) error {
	have, want := scopeOf(rec), c.Request.Scope
	in := have.HomeID == want.HomeID
	switch want.Level {
	case envelope.ScopeWorkspace:
		in = in && have.WorkspaceID == want.WorkspaceID
	case envelope.ScopeProject:
		in = in && have.Level == envelope.ScopeProject && have.WorkspaceID == want.WorkspaceID && have.ProjectID == want.ProjectID
	}
	if !in {
		return envelope.Common(envelope.CodeScopeMismatch, "the record is outside the scope of the request")
	}
	return nil
}

// checkExpected compares every expected revision the request supplies with the
// record it names, whether or not the operation writes that record. The
// comparison is made under the lock the operation holds, before anything is
// computed; the transaction compares the records it writes again before it
// commits. A record the Pool does not hold is not at the revision named.
func checkExpected(c *cli.Call) error {
	for _, e := range c.Request.ExpectedRevisions {
		kind, ok := strings.CutPrefix(e.Schema, "pool.")
		if _, known := poolstore.Lookup(kind); !ok || !known {
			return conflict()
		}
		l, om, err := poolstore.Load(c.Store, kind, e.ID)
		switch {
		case errors.Is(err, poolstore.ErrNotFound):
			return conflict()
		case err != nil:
			return poolstore.Failure("a record the request expects", err)
		case om != nil:
			return envelope.Common(envelope.CodeRecordMalformed, "a record the request expects cannot be read").With("record", om.ID)
		case l.Revision != e.RecordRevision:
			return conflict()
		}
	}
	return nil
}

// checkInstant refuses a time member of a body that is not a real calendar
// instant: a timestamp, or the date a zoned time was written as. Both are
// written before they are read by the Pool, so an impossible date is refused
// where it enters.
func checkInstant(what string, v any) error {
	switch v := v.(type) {
	case map[string]any:
		for _, name := range []string{"at", "not_before", "anchor"} {
			if x, ok := v[name]; ok {
				if err := checkInstant(what+"."+name, x); err != nil {
					return err
				}
			}
		}
		if d, ok := v["date"].(string); ok {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return envelope.Common(envelope.CodeSchemaViolation, what+".date is not a calendar date")
			}
		}
		if r, ok := v["recurrence"]; ok {
			return checkInstant(what+".recurrence", r)
		}
	case string:
		if !clock.Valid(v) {
			return envelope.Common(envelope.CodeSchemaViolation, what+" is not a calendar instant")
		}
	}
	return nil
}

// imperativeExists reports whether the item has an Imperative record. A record
// that cannot be read counts as one.
func imperativeExists(c *cli.Call, taskID string) (bool, error) {
	_, om, err := poolstore.Load(c.Store, "imperative", taskID)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return false, nil
	case err != nil:
		return false, poolstore.Failure("the Imperative of an item", err)
	}
	_ = om
	return true, nil
}

// holdRef is the reference a held failure names the hold by.
func holdRef(c *cli.Call, h *read.Hold) envelope.RecordRef {
	return envelope.RecordRef{Schema: "pool.hold", ID: h.ID, HomeID: c.Home.HomeID, RecordRevision: h.Revision}
}

// callerTree is the caller of the request, as a hold records who placed it.
func callerTree(c *cli.Call) any { return c.Request.Tree()["caller"] }

// newHold is a hold record of the kind on the item, active, as the request
// placed it.
func newHold(c *cli.Call, scope envelope.Scope, taskID, kind, reason, reasonCode string, until any) (string, map[string]any, error) {
	id, err := ids.Mint(c.Clock, ids.Hold)
	if err != nil {
		return "", nil, internal("a hold cannot be given an identity", err)
	}
	now, err := c.Stamp()
	if err != nil {
		return "", nil, internal("a hold cannot be given a time", err)
	}
	rec := map[string]any{
		"schema": "pool.hold", "schema_version": versionOne, "record_revision": int64(1),
		"hold_id": id, "task_id": taskID, "kind": kind, "reason": reason, "state": "active",
		"placed_at":  now,
		"placed_by":  map[string]any{"request_id": c.Request.RequestID, "caller": callerTree(c)},
		"created_at": now, "updated_at": now, "writer": writerTree(c),
	}
	putScope(rec, scope, false)
	if reasonCode != "" {
		rec["reason_code"] = reasonCode
	}
	if until != nil {
		rec["until"] = copyValue(until)
	}
	return id, rec, nil
}

// evidenceOf are the record references among the links a request gives, as the
// evidence of a receipt.
func evidenceOf(links []any) []envelope.Evidence {
	var out []envelope.Evidence
	for _, v := range links {
		m, _ := v.(map[string]any)
		ref, ok := m["ref"].(map[string]any)
		if !ok {
			continue
		}
		out = append(out, envelope.Evidence{Role: "evidence", Ref: refFromMap(ref)})
	}
	return out
}

// refFromMap reads a record_ref of a request body.
func refFromMap(m map[string]any) envelope.RecordRef {
	r := envelope.RecordRef{}
	r.Schema, _ = m["schema"].(string)
	r.ID, _ = m["id"].(string)
	r.HomeID, _ = m["home_id"].(string)
	r.RecordRevision, _ = m["record_revision"].(int64)
	r.Digest, _ = m["digest"].(string)
	return r
}

// addLines appends each line to the body that does not already hold it as a
// line of its own.
func addLines(body string, lines []any) string {
	have := map[string]bool{}
	for _, l := range splitLines(body) {
		have[l] = true
	}
	for _, v := range lines {
		l, _ := v.(string)
		if have[l] {
			continue
		}
		have[l] = true
		body = appendLine(body, l)
	}
	return body
}

// appendLine appends one line to the body, after a line break where the body
// does not end in one.
func appendLine(body, line string) string {
	if body == "" {
		return line
	}
	if body[len(body)-1] != '\n' {
		body += "\n"
	}
	return body + line
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// contains reports whether the links already hold the link.
func contains(links []any, link any) bool {
	return slices.ContainsFunc(links, func(l any) bool { return equalValues(l, link) })
}

// newReceiptID mints the identity of a receipt that a record names before the
// receipt is made.
func newReceiptID(c *cli.Call) (string, error) {
	id, err := ids.Mint(c.Clock, ids.Receipt)
	if err != nil {
		return "", internal("a receipt cannot be given an identity", err)
	}
	return id, nil
}
