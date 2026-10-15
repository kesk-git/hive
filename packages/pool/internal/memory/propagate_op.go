package memory

import (
	"errors"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// PropagateOp is hive-memory propagate (spec/pool.md, Generated knowledge): it
// records one consumer's receipt, or acknowledgment, of a rendering at a digest,
// each with its own receipt, and returns a step already recorded for the same
// consumer, rendering and digest unchanged. The rendering named must be one the
// projection has, current or earlier, at the digest named, and an
// acknowledgment follows the consumer's receipt of that rendering. The
// consumer's entry advances to the newer step; every step is kept in the
// receipt history, so a consumer that has moved on is still read as what it
// received.
func PropagateOp(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	id, step := str(body["projection_id"]), str(body["step"])
	rendering, digest := num(body["rendering"]), str(body["digest"])
	consumer := tree(body["consumer"])

	if err := checkExpected(c); err != nil {
		return nil, err
	}
	l, om, err := poolstore.Load(c.Store, kindProj, id)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, envelope.Common(envelope.CodeNotFound, "no projection has that identity")
	case err != nil:
		return nil, poolstore.Failure("the projection", err)
	case om != nil:
		return nil, envelope.Common(envelope.CodeRecordMalformed, "the projection cannot be read").With("record", om.ID)
	}
	e, err := ParseProjection(l.Data)
	if err != nil {
		return nil, malformed("the projection cannot be read", id, err)
	}
	if err := checkContained(c, e.Scope); err != nil {
		return nil, err
	}
	named := e.Current
	if named.Number != rendering {
		found := false
		for _, h := range list(l.Data["history"]) {
			if r, err := parseRendering(h); err == nil && r.Number == rendering {
				named, found = r, true
			}
		}
		if !found {
			return nil, refuse(CodeDeliveryInvent, "the projection has no such rendering")
		}
	}
	if named.Digest != digest {
		return nil, refuse(CodeDeliveryInvent, "the digest is not the digest of the rendering named")
	}

	same := func(s map[string]any) bool { return num(s["rendering"]) == rendering && str(s["digest"]) == digest }
	var received map[string]any
	for _, h := range list(l.Data["receipt_history"]) {
		h := tree(h)
		if !sameTree(h["consumer"], consumer) {
			continue
		}
		if r := tree(h["received"]); same(r) {
			received = r
			if step == "received" && h["acknowledged"] == nil {
				return &cli.Result{Body: map[string]any{"projection": l.Data, "consumer": h}}, nil
			}
		}
		if a := tree(h["acknowledged"]); step == "acknowledged" && a != nil && same(a) {
			return &cli.Result{Body: map[string]any{"projection": l.Data, "consumer": h}}, nil
		}
	}
	if step == "acknowledged" && received == nil {
		return nil, refuse(CodeDeliveryInvent, "the consumer has not received that rendering, so it cannot acknowledge it")
	}

	receiptID, err := ids.Mint(c.Clock, ids.Receipt)
	if err != nil {
		return nil, internal("a receipt cannot be made", err)
	}
	next := map[string]any{"rendering": rendering, "digest": digest, "at": str(body["at"]), "receipt_id": receiptID}
	if ev := tree(body["evidence"]); ev != nil {
		next["evidence"] = ev
	}
	entry := map[string]any{"consumer": copyValue(consumer)}
	if step == "received" {
		entry["received"] = next
	} else {
		entry["received"], entry["acknowledged"] = copyValue(received), next
	}

	rec := copyValue(l.Data).(map[string]any)
	rec["receipt_history"] = append(list(rec["receipt_history"]), entry)
	consumers := list(rec["consumers"])
	at := -1
	for i, v := range consumers {
		if sameTree(tree(v)["consumer"], consumer) {
			at = i
		}
	}
	if at < 0 {
		consumers = append(consumers, copyValue(entry))
	} else {
		cur := tree(consumers[at])
		if step == "received" && rendering >= num(tree(cur["received"])["rendering"]) {
			cur["received"] = copyValue(next)
		}
		if step == "acknowledged" && (cur["acknowledged"] == nil || rendering >= num(tree(cur["acknowledged"])["rendering"])) {
			cur["acknowledged"] = copyValue(next)
		}
	}
	rec["consumers"] = consumers

	b := &builder{c: c}
	final, ref, err := b.put(kindProj, id, rec, l.Revision)
	if err != nil {
		return nil, err
	}
	kind := receiptReceived
	if step == "acknowledged" {
		kind = receiptAcknowledged
	}
	if err := b.receipt(receiptID, kind, ref, e.Scope); err != nil {
		return nil, err
	}
	staged, err := poolstore.Stage(c, b.tx)
	if err != nil {
		return nil, err
	}
	return staged.Result(map[string]any{"projection": final, "consumer": entry}), nil
}

// sameTree says whether two decoded values are one value.
func sameTree(a, b any) bool {
	x, err := canon.Encode(a)
	if err != nil {
		return false
	}
	y, err := canon.Encode(b)
	return err == nil && string(x) == string(y)
}
