package specification

import (
	"errors"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/pool/internal/read"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The operation accept of hive-spec (spec/pool.md, Specifications). It appends
// the acceptance of the current revision for one item, binds the revision by
// identity, revision and digest into the item's specification, sets its
// definition to accepted and releases its definition hold and those of the
// items that resolve through it. It records need_test and
// open_product_questions whole, checks an existing coverage against the
// accepted specification it names, and leaves a word hold in place.

// Accept records the acceptance of the revision for the item.
func Accept(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	// The stage-2 producer requires both members for a newly issued request.
	needTest := mapOf(body["need_test"])
	if needTest == nil {
		return nil, invalid("an acceptance carries need_test")
	}
	if _, ok := body["open_product_questions"].([]any); !ok {
		return nil, invalid("an acceptance carries open_product_questions, empty where none is open")
	}
	specID, _ := body["specification_id"].(string)
	taskID, _ := body["task_id"].(string)
	revision, _ := body["revision"].(int64)

	g, err := loadGraph(c)
	if err != nil {
		return nil, err
	}
	n := g.Items[taskID]
	if n == nil {
		return nil, notFound("item")
	}
	cur, err := loadSpecification(c, specID)
	if err != nil {
		return nil, err
	}
	if err := checkScope(c, n.Data); err != nil {
		return nil, err
	}
	if err := checkScope(c, cur.Data); err != nil {
		return nil, err
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	if current, _ := cur.Data["current_revision"].(int64); revision != current {
		return nil, conflict()
	}
	rev := revisionNamed(cur.Data, revision)
	if rev == nil {
		return nil, envelope.Common(envelope.CodeRecordMalformed, "the specification does not hold its current revision")
	}
	for _, v := range listOf(cur.Data["acceptances"]) {
		if a := mapOf(v); a["task_id"] == taskID && a["revision"] == revision && a["state"] == "accepted" {
			return nil, envelope.Common(envelope.CodeAlreadyExists, "the item is accepted at this revision already")
		}
	}
	if needTest["coverage"] == "existing" {
		if err := checkCovering(c, g, cur, mapOf(needTest["covering_specification"])); err != nil {
			return nil, err
		}
	}

	now, err := c.Stamp()
	if err != nil {
		return nil, internal("an acceptance cannot be given a time", err)
	}
	receiptID, err := ids.Mint(c.Clock, ids.Receipt)
	if err != nil {
		return nil, internal("a receipt cannot be given an identity", err)
	}
	digest, _ := rev["digest"].(string)
	acceptance := map[string]any{
		"revision": revision, "task_id": taskID, "digest": digest, "state": "accepted",
		"accepted_at": now, "accepted_by": callerTree(c), "receipt_id": receiptID,
		"need_test": copyValue(needTest), "open_product_questions": copyValue(body["open_product_questions"]),
	}
	if v, ok := body["admission"]; ok {
		acceptance["admission"] = copyValue(v)
	}

	// The item is bound; it and the items that resolve through it have their
	// definition hold released, and a definition awaiting the specification
	// returns to accepted.
	item := copyMap(n.Data)
	item["specification"] = map[string]any{"specification_id": specID, "revision": revision, "digest": digest}
	item["definition"] = "accepted"
	b := &builder{c: c}
	finalItem, _, err := b.put("item", taskID, item, n.Revision)
	if err != nil {
		return nil, err
	}
	for _, id := range append([]string{taskID}, resolvers(g, taskID)...) {
		if id != taskID {
			if err := b.accept(g.Items[id]); err != nil {
				return nil, err
			}
		}
		for _, h := range activeDefinitionHolds(g, id) {
			if err := b.releaseHold(h); err != nil {
				return nil, err
			}
		}
	}

	rec := copyMap(cur.Data)
	rec["acceptances"] = append(listOf(rec["acceptances"]), acceptance)
	final, specRef, err := b.put("specification", specID, rec, cur.Revision)
	if err != nil {
		return nil, err
	}
	if err := b.receiptAs(receiptID, "pool.specification.accepted", specRef, scopeOf(n.Data), taskID, nil); err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"specification": final, "item": finalItem})
}

// accept returns the definition of an item that resolves through the accepted
// item to accepted, where it was waiting for a specification.
func (b *builder) accept(n *read.Node) error {
	if d := n.Data["definition"]; d != "needed" && d != "defining" {
		return nil
	}
	next := copyMap(n.Data)
	next["definition"] = "accepted"
	_, _, err := b.put("item", n.ID, next, n.Revision)
	return err
}

// checkCovering holds an existing coverage to the covering specification it
// names: that specification, at that revision and digest, must stand bound to
// an item. An acceptance entry alone is history and does not stand.
func checkCovering(c *cli.Call, g *read.Graph, cur *poolstore.Loaded, ref map[string]any) error {
	unaccepted := refuse(codeSpecificationUnaccepted, "the covering specification is not an accepted specification")
	id, _ := ref["specification_id"].(string)
	revision, _ := ref["revision"].(int64)
	digest, _ := ref["digest"].(string)
	covering := cur
	if id != cur.ID {
		var err error
		if covering, err = loadSpecification(c, id); err != nil {
			var f *envelope.Failure
			if errors.As(err, &f) && f.Err.Code == envelope.CodeNotFound {
				return unaccepted
			}
			return err
		}
	}
	rev := revisionNamed(covering.Data, revision)
	if rev == nil || rev["digest"] != digest {
		return unaccepted
	}
	for _, id := range g.Order {
		if bound, r, d, ok := bindingOf(g.Items[id].Data); ok && bound == covering.ID && r == revision && d == digest {
			return nil
		}
	}
	return unaccepted
}
