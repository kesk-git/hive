package memory

import (
	"errors"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

const (
	receiptItemAdded = "pool.item.added"
	schemaItem       = "pool.item"
	kindItem         = "item"
	itemVersion      = "1.0"
)

// routeTitles are the titles of the items a note becomes, one for each reason
// the fold routes it. They only name the work; the note's own words are the
// item's body.
var routeTitles = map[string]string{
	reasonSubjectUnknown: "File a maintenance note that has no known subject",
	reasonFactConflict:   "File a maintenance note whose fact identity is taken",
	reasonOverBudget:     "File a maintenance note that is over its subject's budget",
}

// routeItem stages the item a note becomes (spec/pool.md, Generated knowledge):
// a queued Prey of the supervisor that owns the home, with deliverable lictor,
// at the note's scope, whose body is the note's exact text, whose links carry
// the note's evidence, and which is discovered-from the task the note's observer
// names, where it names one. The item carries no class, delivery, worker,
// placement of its own choosing or generation: its requested place is the
// default the Pool gives any item that names none. It stages the item with its
// pool.item.added receipt and event in the transaction of the fold, as the
// decision item of a question is staged, and returns its identity.
func (b *builder) routeItem(n *Note, reason string) (string, error) {
	taskID, err := ids.Mint(b.c.Clock, ids.Task)
	if err != nil {
		return "", internal("an item cannot be given an identity", err)
	}
	now, err := b.c.Stamp()
	if err != nil {
		return "", internal("an item cannot be given a time", err)
	}
	links := []any{}
	for _, e := range list(n.Provenance["evidence"]) {
		links = append(links, map[string]any{"kind": "record", "ref": copyValue(e)})
	}
	deps := []any{}
	// The edge is to an item this Pool holds: where the origin is not known to
	// it, the note is routed without one.
	if origin := n.originatingTask(); origin != "" {
		held, err := b.holdsItem(origin)
		if err != nil {
			return "", err
		}
		if held {
			deps = append(deps, map[string]any{"type": "discovered-from", "task_id": origin})
		}
	}
	rec := map[string]any{
		"schema": schemaItem, "schema_version": itemVersion, "record_revision": int64(1),
		"task_id": taskID, "title": routeTitles[reason], "body": n.Text, "level": "prey", "state": "queued",
		"deliverable": "lictor", "definition": "not-required", "placement_requested": "policy",
		"deps": deps, "links": links, "generations": []any{}, "created_at": now, "updated_at": now,
	}
	putScope(rec, n.Scope)
	_, ref, err := b.put(kindItem, taskID, rec, 0)
	if err != nil {
		return "", err
	}
	scope := envelopeOf(n.Scope)
	scope.TaskID = taskID
	return taskID, b.itemAdded(ref, scope)
}

// holdsItem says whether the Pool holds the item. One it cannot read is a source
// that fails the fold.
func (b *builder) holdsItem(taskID string) (bool, error) {
	_, om, err := poolstore.Load(b.c.Store, kindItem, taskID)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return false, nil
	case err != nil:
		return false, poolstore.Failure("an item", err)
	case om != nil:
		return false, malformed("an item a note was discovered from cannot be read", taskID, nil)
	}
	return true, nil
}

// itemAdded adds the receipt that the item was added, with the event the Pool
// registers beside it, both in the item's scope and task.
func (b *builder) itemAdded(subject *envelope.RecordRef, scope envelope.Scope) error {
	r, err := b.c.NewReceipt(receiptItemAdded, versionOne, subject, nil, nil)
	if err != nil {
		return internal("a receipt cannot be made", err)
	}
	r.Scope = scope
	b.tx.Receipts = append(b.tx.Receipts, r)
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
		"kind": receiptItemAdded, "kind_version": versionOne,
		"source":         map[string]any{"package": poolstore.Package, "source_id": poolstore.Package},
		"correlation_id": b.c.Request.CorrelationID,
		"causation":      map[string]any{"schema": schemaRequest, "id": b.c.Request.RequestID},
		"occurred_at":    now, "observed_at": now,
		"basis": map[string]any{"kind": "receipt", "ref": map[string]any{"schema": schemaReceipt, "id": r.ReceiptID}},
		"body":  map[string]any{},
	}
	event["scope_level"], event["home_id"] = scope.Level, scope.HomeID
	if scope.WorkspaceID != "" {
		event["workspace_id"] = scope.WorkspaceID
	}
	if scope.ProjectID != "" {
		event["project_id"] = scope.ProjectID
	}
	event["task_id"] = scope.TaskID
	content, err := canon.Encode(event)
	if err != nil {
		return internal("an event cannot be written", err)
	}
	b.tx.Events = append(b.tx.Events, cstore.Event{ID: eventID, Content: content})
	return nil
}

// envelopeOf is the scope of a record as the envelope carries it.
func envelopeOf(s rule.Scope) envelope.Scope {
	return envelope.Scope{Level: s.Level, HomeID: s.HomeID, WorkspaceID: s.WorkspaceID, ProjectID: s.ProjectID}
}

// queueRef is the reference to the maintenance queue at a revision.
func queueRef(c *cli.Call, revision int64, digest string) *envelope.RecordRef {
	return &envelope.RecordRef{Schema: schemaQueue, ID: queueID, HomeID: c.Home.HomeID, RecordRevision: revision, Digest: digest}
}

// stage checks the transaction and hands it to the harness, with the body of
// the response.
func (b *builder) stage(body map[string]any) (*cli.Result, error) {
	staged, err := poolstore.Stage(b.c, b.tx)
	if err != nil {
		return nil, err
	}
	return staged.Result(body), nil
}
