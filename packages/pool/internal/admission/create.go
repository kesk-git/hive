package admission

import (
	"errors"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/pool/internal/imperative"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The operation create of hive-admit (spec/pool.md, Admission). It records the
// request immutably: the words as given, who gave them, the authority they
// carried and the Imperative revision they concern, which must be current. It
// checks nothing, and writes pool.admission.created.

// Create records the admission request the request carries.
func Create(c *cli.Call) (*cli.Result, error) {
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	body := c.Request.Body
	taskID := str(body["task_id"])
	l, om, err := poolstore.Load(c.Store, "item", taskID)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, notFound("item")
	case err != nil:
		return nil, poolstore.Failure("the item", err)
	case om != nil:
		return nil, malformed("the item cannot be read", om.ID)
	}
	scope := scopeOf(l.Data)
	if err := checkScope(c, scope); err != nil {
		return nil, err
	}
	imp, err := imperative.LoadCurrent(c.Store, taskID)
	if err != nil {
		return nil, err
	}
	revision := integer(body["imperative_revision"])
	if revision != imp.Number {
		return nil, envelope.Common(envelope.CodeRevisionConflict, "the Imperative revision is not the current one")
	}
	rev := imp.Named(revision)
	digest := str(rev["digest"])
	if rev == nil || digest == "" {
		return nil, malformed("the Imperative revision cannot be read", taskID)
	}

	id, err := ids.Mint(c.Clock, ids.Admission)
	if err != nil {
		return nil, internal("an admission cannot be given an identity", err)
	}
	now, err := c.Stamp()
	if err != nil {
		return nil, internal("an admission cannot be given a time", err)
	}
	request := map[string]any{
		"words": body["words"], "caller": c.Request.Tree()["caller"],
		"authority": authorityTree(c.Request.Authority), "received_at": now,
	}
	if source, ok := body["source"]; ok {
		request["source"] = copyTree(source)
	}
	rec := map[string]any{
		"schema": "pool.admission", "schema_version": versionOne, "record_revision": int64(1),
		"admission_id": id, "task_id": taskID, "imperative_revision": revision, "imperative_digest": digest,
		"request": request, "checks": []any{}, "state": "created", "created_at": now, "updated_at": now,
	}
	putScope(rec, scope, true)

	b := &builder{c: c}
	final, ref, err := b.put(kind, id, rec, 0)
	if err != nil {
		return nil, err
	}
	r, err := b.receipt(receiptCreated, nil)
	if err != nil {
		return nil, err
	}
	scope.TaskID = taskID
	if err := b.seal(r, ref, scope, ""); err != nil {
		return nil, err
	}
	staged, err := b.stage()
	if err != nil {
		return nil, err
	}
	return staged.Result(map[string]any{"admission": final}), nil
}
