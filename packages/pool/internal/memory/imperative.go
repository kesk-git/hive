package memory

import (
	"errors"
	"time"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// What an Imperative reads of the generated knowledge and records into it
// (spec/pool.md, The Imperative): the projection of a subject as it stands, for
// the rendering to embed, and the receipt of an embedded projection by the
// worker's generation, staged into the transaction of the delivery. Both call
// the code the operations of this package are made of and change no behaviour
// of any of them.

// SubjectProjection is the stored projection of the key, a subject projection,
// as it stands: its identity, its current rendering with the text, digest, rule
// inventory and sequence the Imperative embeds, and the record it was read from.
// The rules for the key's scope and audience are resolved at the instant, as
// hive-memory render resolves them; where two rules govern one subject at one
// scope the projection has no resolved-rule digest and is not given, and the
// failure is pool.rule_conflict. A subject that does not exist is
// pool.subject_invalid, and a projection that was never rendered is not found.
// Whether the projection is fresh is Fresh's to say, on the projection returned.
func (s *Snapshot) SubjectProjection(k Key, at time.Time) (Existing, error) {
	if err := k.Valid(); err != nil {
		return Existing{}, invalid(err.Error())
	}
	if k.Tier != TierSubject {
		return Existing{}, invalid("the projection an Imperative embeds is a subject projection")
	}
	sub := s.subject(k.SubjectID)
	if sub == nil {
		return Existing{}, noSubject()
	}
	if !within(sub.Scope, k.Scope) {
		return Existing{}, subjectScope()
	}
	res, err := s.resolve(k.Scope, k.Audience, at)
	if err != nil {
		return Existing{}, internal("the rules of the projection cannot be resolved", err)
	}
	if len(res.Conflicts) > 0 {
		return Existing{}, conflictRefusal(k, len(res.Conflicts))
	}
	e, ok := s.projection(k)
	if !ok {
		return Existing{}, envelope.Common(envelope.CodeNotFound, "no projection has been rendered for the subject")
	}
	return e, nil
}

// ReceivedStep is the receipt of one rendering of a projection by one consumer,
// as hive-memory propagate records it for the step received: the consumer, the
// rendering and its digest, the time, and the consumer's own record where it
// gave one.
type ReceivedStep struct {
	ProjectionID string
	Rendering    int64
	Digest       string
	Consumer     map[string]any
	At           string
	Evidence     map[string]any
}

// StageReceived adds to the transaction the record of the step and its receipt,
// as hive-memory propagate writes them, and returns the transaction. The
// rendering named must be one the projection has, current or earlier, at the
// digest named, or the failure is pool.delivery_inventory. A step already
// recorded for the same consumer, rendering and digest adds nothing, and the
// result says so.
func StageReceived(c *cli.Call, tx poolstore.Transaction, step ReceivedStep) (poolstore.Transaction, bool, error) {
	l, om, err := poolstore.Load(c.Store, kindProj, step.ProjectionID)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return tx, false, envelope.Common(envelope.CodeNotFound, "no projection has that identity")
	case err != nil:
		return tx, false, poolstore.Failure("the projection", err)
	case om != nil:
		return tx, false, envelope.Common(envelope.CodeRecordMalformed, "the projection cannot be read").With("record", om.ID)
	}
	e, err := ParseProjection(l.Data)
	if err != nil {
		return tx, false, malformed("the projection cannot be read", step.ProjectionID, err)
	}
	if err := checkContained(c, e.Scope); err != nil {
		return tx, false, err
	}
	named := e.Current
	if named.Number != step.Rendering {
		found := false
		for _, h := range list(l.Data["history"]) {
			if r, err := parseRendering(h); err == nil && r.Number == step.Rendering {
				named, found = r, true
			}
		}
		if !found {
			return tx, false, refuse(CodeDeliveryInvent, "the projection has no such rendering")
		}
	}
	if named.Digest != step.Digest {
		return tx, false, refuse(CodeDeliveryInvent, "the digest is not the digest of the rendering named")
	}

	for _, h := range list(l.Data["receipt_history"]) {
		h := tree(h)
		if !sameTree(h["consumer"], step.Consumer) {
			continue
		}
		if r := tree(h["received"]); num(r["rendering"]) == step.Rendering && str(r["digest"]) == step.Digest {
			return tx, false, nil
		}
	}

	receiptID, err := ids.Mint(c.Clock, ids.Receipt)
	if err != nil {
		return tx, false, internal("a receipt cannot be made", err)
	}
	next := map[string]any{"rendering": step.Rendering, "digest": step.Digest, "at": step.At, "receipt_id": receiptID}
	if step.Evidence != nil {
		next["evidence"] = copyValue(step.Evidence)
	}
	entry := map[string]any{"consumer": copyValue(step.Consumer), "received": next}

	rec := copyValue(l.Data).(map[string]any)
	rec["receipt_history"] = append(list(rec["receipt_history"]), entry)
	consumers := list(rec["consumers"])
	at := -1
	for i, v := range consumers {
		if sameTree(tree(v)["consumer"], step.Consumer) {
			at = i
		}
	}
	if at < 0 {
		consumers = append(consumers, copyValue(entry))
	} else if cur := tree(consumers[at]); step.Rendering >= num(tree(cur["received"])["rendering"]) {
		cur["received"] = copyValue(next)
	}
	rec["consumers"] = consumers

	b := &builder{c: c, tx: tx}
	_, ref, err := b.put(kindProj, step.ProjectionID, rec, l.Revision)
	if err != nil {
		return tx, false, err
	}
	if err := b.receipt(receiptID, receiptReceived, ref, e.Scope); err != nil {
		return tx, false, err
	}
	return b.tx, true, nil
}
