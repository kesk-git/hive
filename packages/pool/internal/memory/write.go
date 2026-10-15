package memory

import (
	"errors"
	"slices"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

const (
	schemaReceipt = "hive.receipt"
	schemaRequest = "hive.request"
	kindProj      = "projection"
)

// builder gathers the transaction of one request.
type builder struct {
	c  *cli.Call
	tx poolstore.Transaction
}

func writerTree(c *cli.Call) map[string]any {
	w := c.Writer()
	return map[string]any{"package": w.Package, "command": w.Command, "operation": w.Operation, "implementation_version": w.ImplementationVersion}
}

// put adds the record to the transaction, finalized, and returns it with the
// reference a receipt names it by.
func (b *builder) put(kind, id string, body map[string]any, expected int64) (map[string]any, *envelope.RecordRef, error) {
	body["writer"] = writerTree(b.c)
	k, ok := poolstore.Lookup(kind)
	if !ok {
		return nil, nil, internal("a record cannot be finalized", nil)
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

// receipt adds the receipt of the kind about the record, in the scope of the
// projection. A rendering has an event beside it, which the Pool registers.
func (b *builder) receipt(receiptID, kind string, subject *envelope.RecordRef, scope rule.Scope) error {
	r, err := b.c.NewReceipt(kind, versionOne, subject, nil, nil)
	if err != nil {
		return internal("a receipt cannot be made", err)
	}
	if receiptID != "" {
		r.ReceiptID = receiptID
	}
	r.Scope = envelopeScope(scope)
	b.tx.Receipts = append(b.tx.Receipts, r)
	if kind != receiptRendered {
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
	content, err := canon.Encode(event)
	if err != nil {
		return internal("an event cannot be written", err)
	}
	b.tx.Events = append(b.tx.Events, cstore.Event{ID: eventID, Content: content})
	return nil
}

// target is one rendering a request makes: the key and the instant the rules
// are resolved for.
type target struct {
	key Key
	at  string
}

// renderAll renders each target of the view, which is the snapshot as it will
// stand, against the budget revision, and stages the projection of each whose
// rendering is not equal to the one it has: a rendering equal in text, exact
// source revisions, rule inventory and budget revision writes nothing. It
// refuses a rendering that conflicts, and one that exceeds its budget. It
// returns the projection of each target as it stands afterwards, and whether it
// was written.
func (b *builder) renderAll(view *Snapshot, targets []target, budgetRevision int64) ([]map[string]any, []bool, error) {
	now, err := b.c.Stamp()
	if err != nil {
		return nil, nil, internal("the time of the rendering cannot be written", err)
	}
	var out []map[string]any
	var written []bool
	for _, t := range targets {
		at := b.c.Clock.Now()
		if t.at != "" {
			if at, err = parseInstant(t.at); err != nil {
				return nil, nil, invalid("at is not a calendar instant")
			}
		}
		r, err := view.render(t.key, at, budgetRevision)
		if err != nil {
			return nil, nil, err
		}
		if len(r.Conflicts) > 0 {
			return nil, nil, conflictRefusal(t.key, len(r.Conflicts))
		}
		e, exists := view.projection(t.key)
		id := e.ID
		if max := view.Budget.Max(id, t.key); r.Bytes > max {
			return nil, nil, overBudget(id, t.key, at, r.Bytes, max)
		}
		if exists && equalRendering(e, r, budgetRevision) {
			out, written = append(out, e.Data), append(written, false)
			continue
		}
		r.RenderedAt = now
		var body map[string]any
		var expected int64
		if exists {
			body, expected = copyValue(e.Data).(map[string]any), e.Revision
			r.Number = e.Current.Number + 1
			body["history"] = append(list(body["history"]), copyValue(body["current"]))
		} else {
			if id, err = ids.Mint(b.c.Clock, ids.Projection); err != nil {
				return nil, nil, internal("a projection cannot be given an identity", err)
			}
			r.Number = 1
			body = map[string]any{
				"schema": "pool.projection", "schema_version": "2.0", "projection_id": id,
				"audience": t.key.Audience, "tier": t.key.Tier, "consumers": []any{}, "receipt_history": []any{},
				"history": []any{}, "created_at": now,
			}
			putScope(body, t.key.Scope)
			if t.key.SubjectID != "" {
				body["subject_id"] = t.key.SubjectID
			}
		}
		body["current"], body["budget_revision"] = r.tree(b.c.Home.HomeID), budgetRevision
		final, ref, err := b.put(kindProj, id, body, expected)
		if err != nil {
			return nil, nil, err
		}
		if err := b.receipt("", receiptRendered, ref, t.key.Scope); err != nil {
			return nil, nil, err
		}
		out, written = append(out, final), append(written, true)
	}
	return out, written, nil
}

// equalRendering says whether the rendering is equal to the projection's
// current one in text, exact source revisions, rule inventory and budget
// revision; matching text alone never hides changed sources.
func equalRendering(e Existing, r *Rendering, budgetRevision int64) bool {
	return e.BudgetRevision == budgetRevision && e.Current.Text == r.Text &&
		slices.Equal(e.Current.Sources, r.Sources) && slices.Equal(e.Current.Rules, r.Rules)
}

// checkContained refuses a projection outside the scope of the request.
func checkContained(c *cli.Call, scope rule.Scope) error {
	want := rule.Scope{Level: c.Request.Scope.Level, HomeID: c.Request.Scope.HomeID, WorkspaceID: c.Request.Scope.WorkspaceID, ProjectID: c.Request.Scope.ProjectID}
	if !within(want, scope) {
		return envelope.Common(envelope.CodeScopeMismatch, "the projection is outside the scope of the request")
	}
	return nil
}

// checkExpected compares every expected revision the request supplies with the
// record it names, whether or not the operation writes that record, under the
// lock the operation holds and before anything is computed. A record the Pool
// does not hold is not at the revision named.
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
