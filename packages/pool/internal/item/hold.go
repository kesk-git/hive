package item

import (
	"errors"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// hold places a parked, external, resource or future hold on an item. The
// request schema admits no other kind: a word hold is the decision intake's, an
// admission hold the admission check's and a definition hold the Pool's own.
func hold(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	taskID, _ := body["task_id"].(string)
	if until, ok := body["until"]; ok {
		if err := checkZoned("until", until); err != nil {
			return nil, err
		}
	}
	l, om, err := poolstore.Load(c.Store, "item", taskID)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, notFound("item")
	case err != nil:
		return nil, poolstore.Failure("the item", err)
	case om != nil:
		return nil, envelope.Common(envelope.CodeRecordMalformed, "the item cannot be read")
	}
	if err := checkScope(c, l.Data); err != nil {
		return nil, err
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	kind, _ := body["kind"].(string)
	reason, _ := body["reason"].(string)
	scope := scopeOf(l.Data)
	holdID, rec, err := newHold(c, scope, taskID, kind, reason, "", body["until"])
	if err != nil {
		return nil, err
	}
	b := &builder{c: c}
	final, ref, err := b.put("hold", holdID, rec, 0)
	if err != nil {
		return nil, err
	}
	if _, err := b.receipt("pool.hold.placed", ref, scope, taskID, nil); err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"hold": final})
}

// release releases a parked, external, resource or future hold, and writes
// pool.hold.released with the evidence given; the record stays. A word hold is
// released by the decision intake alone, an admission hold by an admission
// check that admits and a definition hold by hive-spec accept: this operation
// refuses them. A hold already released is returned as it is and nothing is
// written.
func release(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	holdID, _ := body["hold_id"].(string)
	l, om, err := poolstore.Load(c.Store, "hold", holdID)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, notFound("hold")
	case err != nil:
		return nil, poolstore.Failure("the hold", err)
	case om != nil:
		return nil, envelope.Common(envelope.CodeRecordMalformed, "the hold cannot be read")
	}
	if err := checkScope(c, l.Data); err != nil {
		return nil, err
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	switch l.Data["kind"] {
	case "word", "admission", "definition":
		return nil, envelope.Common(envelope.CodeAuthorityInsufficient, "this kind of hold is released by the operation that owns it, and hive-pool release does not release it")
	}
	if l.Data["state"] == "released" {
		return &cli.Result{Body: map[string]any{"hold": l.Data}}, nil
	}
	var evidence []envelope.Evidence
	if ref, ok := body["evidence"].(map[string]any); ok {
		evidence = append(evidence, envelope.Evidence{Role: "evidence", Ref: refFromMap(ref)})
	}
	b := &builder{c: c}
	final, err := b.release(holdID, l.Data, l.Revision, evidence)
	if err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"hold": final})
}

// release writes the release of the hold at the revision it was read, with the
// receipt that establishes it as its released_by, and returns the record.
func (b *builder) release(holdID string, data map[string]any, revision int64, evidence []envelope.Evidence) (map[string]any, error) {
	now, err := b.c.Stamp()
	if err != nil {
		return nil, internal("a release cannot be given a time", err)
	}
	receiptID, err := newReceiptID(b.c)
	if err != nil {
		return nil, err
	}
	next := copyMap(data)
	next["state"], next["released_at"] = "released", now
	next["released_by"] = map[string]any{"schema": schemaReceipt, "id": receiptID}
	final, ref, err := b.put("hold", holdID, next, revision)
	if err != nil {
		return nil, err
	}
	taskID, _ := data["task_id"].(string)
	if _, err := b.receiptAs(receiptID, "pool.hold.released", ref, scopeOf(final), taskID, evidence); err != nil {
		return nil, err
	}
	return final, nil
}
