package specification

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/read"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The codes of the Pool that the operations of this package return
// (spec/pool.md, Error codes), and the words the records they write name
// things by.
const (
	codeSpecificationUnaccepted = "pool.specification_unaccepted"

	versionOne           = "1.0"
	schemaVersion        = "1.2"
	schemaReceipt        = "hive.receipt"
	schemaRequest        = "hive.request"
	definitionReasonCode = "specification_unaccepted"
	definitionHoldReason = "A specification is required and none is accepted for this item."
)

// registeredEvents are the receipt kinds the Pool registers an event for
// (spec/pool.md, Receipts and events); pool.specification.defined is a receipt
// alone.
var registeredEvents = []string{
	"pool.hold.placed", "pool.hold.released",
	"pool.specification.accepted", "pool.specification.invalidated", "pool.specification.closed",
}

func refuse(code, message string) *envelope.Failure {
	return envelope.Fail(envelope.Refused, code, message)
}

func invalid(message string) *envelope.Failure {
	return envelope.Common(envelope.CodeSchemaViolation, message)
}

func notFound(what string) *envelope.Failure {
	return envelope.Common(envelope.CodeNotFound, "no "+what+" has that identity")
}

func internal(what string, err error) *envelope.Failure {
	return envelope.Common(envelope.CodeInternal, what).Because(err)
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

// callerTree and authorityTree are the caller and the authority of the
// request, as a record holds them.
func callerTree(c *cli.Call) any    { return c.Request.Tree()["caller"] }
func authorityTree(c *cli.Call) any { return c.Request.Tree()["authority"] }

// scopeOf is the scope a record carries.
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
// the clone unless asked.
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

func listOf(v any) []any {
	l, _ := v.([]any)
	return l
}

func mapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// finalize is the record as the store will write it at the revision after
// expected, with the digest of that record.
func finalize(c *cli.Call, kind, id string, body map[string]any, expected int64) (map[string]any, string, error) {
	k, ok := poolstore.Lookup(kind)
	if !ok {
		return nil, "", fmt.Errorf("specification: %q is not a kind", kind)
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
	ref := &envelope.RecordRef{Schema: "pool." + kind, ID: id, HomeID: b.c.Home.HomeID, RecordRevision: expected + 1, Digest: digest}
	return final, ref, nil
}

// receipt adds the receipt of the kind about the subject to the transaction,
// with the event the Pool registers beside it where it registers one. Both take
// the scope and the task_id given.
func (b *builder) receipt(kind string, subject *envelope.RecordRef, scope envelope.Scope, taskID string, evidence []envelope.Evidence) error {
	return b.receiptAs("", kind, subject, scope, taskID, evidence)
}

// receiptAs is receipt with the identity of the receipt minted beforehand, as a
// record that names the receipt which establishes it needs. An empty identity
// is minted here.
func (b *builder) receiptAs(receiptID, kind string, subject *envelope.RecordRef, scope envelope.Scope, taskID string, evidence []envelope.Evidence) error {
	r, err := b.c.NewReceipt(kind, versionOne, subject, evidence, nil)
	if err != nil {
		return internal("a receipt cannot be made", err)
	}
	if receiptID != "" {
		r.ReceiptID = receiptID
	}
	r.Scope = scope
	r.Scope.TaskID = taskID
	b.tx.Receipts = append(b.tx.Receipts, r)
	if !slices.Contains(registeredEvents, kind) {
		return nil
	}
	eventID, err := ids.Mint(b.c.Clock, ids.Event)
	if err != nil {
		return internal("an event cannot be made", err)
	}
	now, err := b.c.Stamp()
	if err != nil {
		return internal("an event cannot be made", err)
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
	if taskID != "" {
		event["task_id"] = taskID
	}
	if scope.Generation != 0 {
		event["generation"] = scope.Generation
	}
	content, err := canon.Encode(event)
	if err != nil {
		return internal("an event cannot be written", err)
	}
	b.tx.Events = append(b.tx.Events, cstore.Event{ID: eventID, Content: content})
	return nil
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

// loadGraph reads the items and the holds under the lock the operation holds.
// A record that cannot be read ends the request: a rule that reads the records
// above and below an item cannot be trusted over a hole in them.
func loadGraph(c *cli.Call) (*read.Graph, error) {
	g, omitted, err := read.LoadGraph(c.Store, c.Clock.Now())
	if err != nil {
		return nil, poolstore.Failure("the Pool", err)
	}
	if len(omitted) > 0 {
		return nil, envelope.Common(envelope.CodeRecordMalformed, "a record of the Pool cannot be read, so the specification of an item cannot be known").With("record", omitted[0].ID)
	}
	return g, nil
}

// loadSpecification reads the specification record under the lock.
func loadSpecification(c *cli.Call, id string) (*poolstore.Loaded, error) {
	l, om, err := poolstore.Load(c.Store, "specification", id)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, notFound("specification")
	case err != nil:
		return nil, poolstore.Failure("the specification", err)
	case om != nil:
		return nil, envelope.Common(envelope.CodeRecordMalformed, "the specification cannot be read")
	}
	return l, nil
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

// newHold is a definition hold record on the item, active, as the Pool places
// it (spec/pool.md, Holds).
func newHold(c *cli.Call, scope envelope.Scope, taskID string) (string, map[string]any, error) {
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
		"hold_id": id, "task_id": taskID, "kind": "definition", "reason": definitionHoldReason,
		"reason_code": definitionReasonCode, "state": "active",
		"placed_at":  now,
		"placed_by":  map[string]any{"request_id": c.Request.RequestID, "caller": callerTree(c)},
		"created_at": now, "updated_at": now, "writer": writerTree(c),
	}
	putScope(rec, scope, false)
	return id, rec, nil
}

// placeHold adds a definition hold on the item to the transaction, with the
// receipt that establishes it.
func (b *builder) placeHold(taskID string, scope envelope.Scope) error {
	holdID, hold, err := newHold(b.c, scope, taskID)
	if err != nil {
		return err
	}
	_, ref, err := b.put("hold", holdID, hold, 0)
	if err != nil {
		return err
	}
	return b.receipt("pool.hold.placed", ref, scope, taskID, nil)
}

// releaseHold writes the release of the hold at the revision it was read, with
// the receipt that establishes it as its released_by.
func (b *builder) releaseHold(h *read.Hold) error {
	now, err := b.c.Stamp()
	if err != nil {
		return internal("a release cannot be given a time", err)
	}
	receiptID, err := ids.Mint(b.c.Clock, ids.Receipt)
	if err != nil {
		return internal("a receipt cannot be given an identity", err)
	}
	next := copyMap(h.Data)
	next["state"], next["released_at"] = "released", now
	next["released_by"] = map[string]any{"schema": schemaReceipt, "id": receiptID}
	final, ref, err := b.put("hold", h.ID, next, h.Revision)
	if err != nil {
		return err
	}
	return b.receiptAs(receiptID, "pool.hold.released", ref, scopeOf(final), h.TaskID, nil)
}

// activeDefinitionHolds are the definition holds on the item that are active.
func activeDefinitionHolds(g *read.Graph, id string) []*read.Hold {
	var out []*read.Hold
	for _, h := range g.Holds[id] {
		if h.Kind == "definition" && h.State == "active" {
			out = append(out, h)
		}
	}
	return out
}

// evidenceRef is a record_ref of a request body, as the evidence of a receipt
// in the role given.
func evidenceRef(role string, ref map[string]any) envelope.Evidence {
	r := envelope.RecordRef{}
	r.Schema, _ = ref["schema"].(string)
	r.ID, _ = ref["id"].(string)
	r.HomeID, _ = ref["home_id"].(string)
	r.RecordRevision, _ = ref["record_revision"].(int64)
	r.Digest, _ = ref["digest"].(string)
	return envelope.Evidence{Role: role, Ref: r}
}
