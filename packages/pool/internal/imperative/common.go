package imperative

import (
	"errors"
	"slices"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/memory"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The codes of the Pool that the operations of this package return
// (spec/pool.md, Error codes), the receipts they write and the versions they
// write records and renderings at.
const (
	codeNotDispatchable   = "pool.not_dispatchable"
	codeRuleConflict      = memory.CodeRuleConflict
	codeDeliveryInventory = memory.CodeDeliveryInvent
	codeProjectionStale   = "pool.projection_stale"
	codeNotOpen           = "pool.generation_not_open"

	receiptRevised   = "pool.imperative.revised"
	receiptDelivered = "pool.imperative.delivered"

	versionOne    = "1.0"
	schemaVersion = "1.1"
	schemaReceipt = "hive.receipt"
	schemaRequest = "hive.request"
	kindImp       = "imperative"

	// RendererVersion is the version of the renderer that writes the text of
	// an Imperative; a change of the layout or of what it reads changes it.
	RendererVersion = "imperative-renderer-1"
)

// registeredEvents are the receipt kinds the Pool registers an event for
// (spec/pool.md, Receipts and events); pool.imperative.delivered is a receipt
// alone.
var registeredEvents = []string{receiptRevised}

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

func malformed(what, id string) *envelope.Failure {
	return envelope.Common(envelope.CodeRecordMalformed, what).With("record", id)
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

// putScope writes the scope's flat members into a record or an event.
func putScope(m map[string]any, s envelope.Scope) {
	m["scope_level"], m["home_id"] = s.Level, s.HomeID
	if s.WorkspaceID != "" {
		m["workspace_id"] = s.WorkspaceID
	}
	if s.ProjectID != "" {
		m["project_id"] = s.ProjectID
	}
	if s.CloneID != "" {
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

func strOf(v any) string {
	s, _ := v.(string)
	return s
}

func intOf(v any) int64 {
	n, _ := v.(int64)
	return n
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

// expects says whether the request names the expected revision of the record.
func expects(c *cli.Call, schema, id string) bool {
	return slices.ContainsFunc(c.Request.ExpectedRevisions, func(e envelope.ExpectedRevision) bool {
		return e.Schema == schema && e.ID == id
	})
}

// checkExpected compares every expected revision the request supplies with the
// record it names, whether or not the operation writes that record. The
// comparison is made under the lock the operation holds, before anything is
// computed; the transaction compares the records it writes again before it
// commits. A record the Pool does not hold is not at the revision named.
func checkExpected(c *cli.Call) error {
	for _, e := range c.Request.ExpectedRevisions {
		stale := envelope.Common(envelope.CodeRevisionConflict, "the record is not at the revision the request expects")
		kind, ok := strings.CutPrefix(e.Schema, "pool.")
		if _, known := poolstore.Lookup(kind); !ok || !known {
			return stale
		}
		l, om, err := poolstore.Load(c.Store, kind, e.ID)
		switch {
		case errors.Is(err, poolstore.ErrNotFound):
			return stale
		case err != nil:
			return poolstore.Failure("a record the request expects", err)
		case om != nil:
			return envelope.Common(envelope.CodeRecordMalformed, "a record the request expects cannot be read").With("record", om.ID)
		case l.Revision != e.RecordRevision:
			return stale
		}
	}
	return nil
}

// loadImperative reads the Imperative of the item under the lock the operation
// holds.
func loadImperative(st *cstore.Store, taskID string) (*poolstore.Loaded, error) {
	l, om, err := poolstore.Load(st, kindImp, taskID)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, notFound("Imperative")
	case err != nil:
		return nil, poolstore.Failure("the Imperative", err)
	case om != nil:
		return nil, malformed("the Imperative cannot be read", om.ID)
	}
	return l, nil
}

// revisionNamed finds the revision of the Imperative record by its number.
func revisionNamed(rec map[string]any, n int64) map[string]any {
	for _, v := range listOf(rec["revisions"]) {
		if r := mapOf(v); intOf(r["revision"]) == n {
			return r
		}
	}
	return nil
}

// revisionDigest is the digest of a revision: the canonical form of its class
// and its four sections.
func revisionDigest(rev map[string]any) (string, error) {
	form := map[string]any{}
	for _, name := range []string{"class", "task", "setup", "rules", "definition_of_done"} {
		form[name] = rev[name]
	}
	data, err := canon.Encode(form)
	if err != nil {
		return "", err
	}
	return canon.FileDigest(data), nil
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
	k, ok := poolstore.Lookup(kind)
	if !ok {
		return nil, nil, internal("a record cannot be finalized", errors.New(kind+" is not a kind"))
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
	b.tx.Records = append(b.tx.Records, poolstore.Write{Kind: kind, ID: id, Body: final, Expected: expected})
	return final, &envelope.RecordRef{Schema: k.Schema, ID: id, HomeID: b.c.Home.HomeID, RecordRevision: expected + 1, Digest: digest}, nil
}

// receipt adds the receipt of the kind about the record to the transaction,
// in the scope given, with the event the Pool registers beside it where it
// registers one.
func (b *builder) receipt(kind string, subject *envelope.RecordRef, scope envelope.Scope, evidence []envelope.Evidence) error {
	r, err := b.c.NewReceipt(kind, versionOne, subject, evidence, nil)
	if err != nil {
		return internal("a receipt cannot be made", err)
	}
	r.Scope = scope
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
	putScope(event, scope)
	if scope.TaskID != "" {
		event["task_id"] = scope.TaskID
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
// staged that lies outside the scope of the request.
func (b *builder) stage(body map[string]any) (*cli.Result, error) {
	for _, w := range b.tx.Records {
		if w.Kind != kindImp {
			continue
		}
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

// errDuplicate reports an inventory that names one entry twice.
var errDuplicate = errors.New("an inventory names one entry twice")
