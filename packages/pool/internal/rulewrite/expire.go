package rulewrite

import (
	"errors"
	"io/fs"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/memory"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// Expire is hive-rule expire (spec/pool.md, Rules): it records that the rule's
// expiry was noticed and honoured, with the time it was noticed. Before the
// expiry time, which is the latest amendment's new expiry or else the original,
// it is refused with pool.rule_not_expired; a second call returns the first
// receipt and writes nothing. The receipt records the noticing: the exclusion
// happened by the clock. The expire re-renders the projections of the rule's
// scope in its transaction and is refused with pool.projection_over_budget when
// one would exceed its budget, as the restoration of a rule it superseded may
// make it.
func Expire(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	id := str(body["rule_id"])
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	l, r, err := loadRule(c, id)
	if err != nil {
		return nil, err
	}
	if err := checkRuleContained(c, r); err != nil {
		return nil, err
	}
	if r.Expiry != nil {
		return again(c, l.Data, str(r.Expiry["receipt_id"]))
	}
	now := c.Clock.Now()
	expiry, ok, err := r.EffectiveExpiry()
	switch {
	case err != nil:
		return nil, envelope.Common(envelope.CodeRecordMalformed, "the rule's expiry is not a calendar instant").With("record", id).Because(err)
	case !ok:
		return nil, refuse(CodeNotExpired, "the rule has no expiry time to honour")
	case now.Before(expiry):
		at, _ := clock.Format(expiry)
		return nil, refuse(CodeNotExpired, "the rule's expiry time has not come").With("expires_at", at)
	}
	noticed := str(body["noticed_at"])
	if _, err := clock.Parse(noticed); err != nil {
		return nil, invalid("noticed_at is not a calendar instant")
	}
	stamp, err := c.Stamp()
	if err != nil {
		return nil, internal("the time of the expiry cannot be written", err)
	}

	b := &builder{c: c}
	var evidence []envelope.Evidence
	if wake := tree(body["wake"]); wake != nil {
		evidence = append(evidence, envelope.Evidence{Role: "wake", Ref: envelope.RecordRef{
			Schema: str(wake["schema"]), ID: str(wake["id"]), HomeID: str(wake["home_id"]),
			RecordRevision: whole(wake["record_revision"]), Digest: str(wake["digest"]),
		}})
	}
	receipt, err := b.receipt(receiptExpired, evidence)
	if err != nil {
		return nil, err
	}
	next := copyValue(l.Data).(map[string]any)
	next["status"] = rule.StatusExpired
	next["expiry"] = map[string]any{"receipt_id": receipt.ReceiptID, "recorded_at": stamp, "noticed_at": noticed}
	next["updated_at"] = stamp

	planned := copyValue(next).(map[string]any)
	planned["record_revision"] = l.Revision + 1
	pr, err := rule.ParseRule(planned)
	if err != nil {
		return nil, internal("the expired rule cannot be read", err)
	}
	snap, err := memory.Load(c.Store, c.Home)
	if err != nil {
		return nil, err
	}
	write := memory.Write{Rules: []*rule.Rule{pr}}
	if _, err := snap.Plan(now, write); err != nil {
		return nil, err
	}

	final, ref, err := b.put("rule", id, next, l.Revision)
	if err != nil {
		return nil, err
	}
	if err := b.seal(receipt, ref, scopeOfRule(r), true); err != nil {
		return nil, err
	}
	return b.stageRendered(snap, write, map[string]any{"rule": final})
}

// again is the answer to an expiry already recorded: the rule as it stands and
// the first receipt, returned and not written again.
func again(c *cli.Call, data map[string]any, receiptID string) (*cli.Result, error) {
	raw, err := c.Store.Read(cstore.ReceiptPath(poolstore.Package, receiptID))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, envelope.Common(envelope.CodeRecordMalformed, "the receipt of the rule's expiry cannot be read").With("receipt_id", receiptID)
		}
		return nil, poolstore.Failure("the receipt of the rule's expiry", err)
	}
	first, err := envelope.DecodeReceipt(raw)
	if err != nil {
		return nil, envelope.Common(envelope.CodeRecordMalformed, "the receipt of the rule's expiry cannot be read").With("receipt_id", receiptID).Because(err)
	}
	return &cli.Result{Body: map[string]any{"rule": data}, Durable: []envelope.Receipt{*first}}, nil
}
