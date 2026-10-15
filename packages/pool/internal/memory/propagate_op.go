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
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	tx, out, err := stageStep(c, poolstore.Transaction{}, projectionStep{
		ProjectionID: str(body["projection_id"]), Step: str(body["step"]), Rendering: num(body["rendering"]),
		Digest: str(body["digest"]), Consumer: tree(body["consumer"]), At: str(body["at"]), Evidence: tree(body["evidence"]),
	})
	if err != nil {
		return nil, err
	}
	if !out.Changed {
		return &cli.Result{Body: map[string]any{"projection": out.Projection, "consumer": out.Consumer}}, nil
	}
	staged, err := poolstore.Stage(c, tx)
	if err != nil {
		return nil, err
	}
	return staged.Result(map[string]any{"projection": out.Projection, "consumer": out.Consumer}), nil
}

// projectionStep is one step of one consumer on one rendering of a projection:
// the step received or acknowledged, the rendering and its digest, the time, and
// the consumer's own record where it gave one.
type projectionStep struct {
	ProjectionID string
	Step         string
	Rendering    int64
	Digest       string
	Consumer     map[string]any
	At           string
	Evidence     map[string]any
}

// stepOutcome is what staging a step came to: the projection and the
// consumer's entry as they stand, and whether the step added anything to the
// transaction; a step already recorded adds nothing.
type stepOutcome struct {
	Projection map[string]any
	Consumer   map[string]any
	Changed    bool
}

// stageStep is the one implementation of the steps of propagate. It adds the
// record of the step and its receipt to the transaction, and returns it.
func stageStep(c *cli.Call, tx poolstore.Transaction, s projectionStep) (poolstore.Transaction, stepOutcome, error) {
	l, om, err := poolstore.Load(c.Store, kindProj, s.ProjectionID)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return tx, stepOutcome{}, envelope.Common(envelope.CodeNotFound, "no projection has that identity")
	case err != nil:
		return tx, stepOutcome{}, poolstore.Failure("the projection", err)
	case om != nil:
		return tx, stepOutcome{}, envelope.Common(envelope.CodeRecordMalformed, "the projection cannot be read").With("record", om.ID)
	}
	e, err := ParseProjection(l.Data)
	if err != nil {
		return tx, stepOutcome{}, malformed("the projection cannot be read", s.ProjectionID, err)
	}
	if err := checkContained(c, e.Scope); err != nil {
		return tx, stepOutcome{}, err
	}
	named := e.Current
	if named.Number != s.Rendering {
		found := false
		for _, h := range list(l.Data["history"]) {
			if r, err := parseRendering(h); err == nil && r.Number == s.Rendering {
				named, found = r, true
			}
		}
		if !found {
			return tx, stepOutcome{}, refuse(CodeDeliveryInvent, "the projection has no such rendering")
		}
	}
	if named.Digest != s.Digest {
		return tx, stepOutcome{}, refuse(CodeDeliveryInvent, "the digest is not the digest of the rendering named")
	}

	same := func(r map[string]any) bool { return num(r["rendering"]) == s.Rendering && str(r["digest"]) == s.Digest }
	var received map[string]any
	for _, h := range list(l.Data["receipt_history"]) {
		h := tree(h)
		if !sameTree(h["consumer"], s.Consumer) {
			continue
		}
		if r := tree(h["received"]); same(r) {
			received = r
			if s.Step == "received" && h["acknowledged"] == nil {
				return tx, stepOutcome{Projection: l.Data, Consumer: h}, nil
			}
		}
		if a := tree(h["acknowledged"]); s.Step == "acknowledged" && a != nil && same(a) {
			return tx, stepOutcome{Projection: l.Data, Consumer: h}, nil
		}
	}
	if s.Step == "acknowledged" && received == nil {
		return tx, stepOutcome{}, refuse(CodeDeliveryInvent, "the consumer has not received that rendering, so it cannot acknowledge it")
	}

	receiptID, err := ids.Mint(c.Clock, ids.Receipt)
	if err != nil {
		return tx, stepOutcome{}, internal("a receipt cannot be made", err)
	}
	next := map[string]any{"rendering": s.Rendering, "digest": s.Digest, "at": s.At, "receipt_id": receiptID}
	if s.Evidence != nil {
		next["evidence"] = copyValue(s.Evidence)
	}
	entry := map[string]any{"consumer": copyValue(s.Consumer)}
	if s.Step == "received" {
		entry["received"] = next
	} else {
		entry["received"], entry["acknowledged"] = copyValue(received), next
	}

	rec := copyValue(l.Data).(map[string]any)
	rec["receipt_history"] = append(list(rec["receipt_history"]), entry)
	consumers := list(rec["consumers"])
	at := -1
	for i, v := range consumers {
		if sameTree(tree(v)["consumer"], s.Consumer) {
			at = i
		}
	}
	if at < 0 {
		consumers = append(consumers, copyValue(entry))
	} else {
		cur := tree(consumers[at])
		if s.Step == "received" && s.Rendering >= num(tree(cur["received"])["rendering"]) {
			cur["received"] = copyValue(next)
		}
		if s.Step == "acknowledged" && (cur["acknowledged"] == nil || s.Rendering >= num(tree(cur["acknowledged"])["rendering"])) {
			cur["acknowledged"] = copyValue(next)
		}
	}
	rec["consumers"] = consumers

	b := &builder{c: c, tx: tx}
	final, ref, err := b.put(kindProj, s.ProjectionID, rec, l.Revision)
	if err != nil {
		return tx, stepOutcome{}, err
	}
	kind := receiptReceived
	if s.Step == "acknowledged" {
		kind = receiptAcknowledged
	}
	if err := b.receipt(receiptID, kind, ref, e.Scope); err != nil {
		return tx, stepOutcome{}, err
	}
	return b.tx, stepOutcome{Projection: final, Consumer: entry, Changed: true}, nil
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
