package rulewrite

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/memory"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
)

// Amend is hive-rule amend (spec/pool.md, Rules): it appends an attributed
// amendment of the rule's operation, extend or end with a new expiry time, lift
// with the receipt that met the lift condition, or note. The rule's words, dates
// and original expiry are never changed; the latest amendment's new expiry is the
// time the resolver uses. A lift is checked against the receipt the request
// carries whole, and the Pool reads no other store for it. The amend re-renders
// the projections of the rule's scope in its transaction and is refused with
// pool.projection_over_budget when one would exceed its budget.
//
// The request's expected_revisions names the rule's revision, as the schema of
// the request states, and a request without it is invalid.
func Amend(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	id, kind := str(body["rule_id"]), str(body["kind"])
	if !expects(c, "pool.rule", id) {
		return nil, invalid("hive-rule amend names the expected revision of the rule, pool.rule " + id)
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	l, r, err := loadRule(c, id)
	if err != nil {
		return nil, err
	}
	if err := checkContained(c, r.Scope); err != nil {
		return nil, err
	}
	now := c.Clock.Now()
	stamp, err := c.Stamp()
	if err != nil {
		return nil, internal("the time of the amendment cannot be written", err)
	}

	b := &builder{c: c}
	receiptKind := receiptAmended
	if kind == rule.AmendmentLift {
		receiptKind = receiptLifted
	}
	receipt, err := b.receipt(receiptKind, nil)
	if err != nil {
		return nil, err
	}
	amendment := map[string]any{
		"sequence": int64(len(r.Amendments)) + 1, "kind": kind, "words": str(body["words"]),
		"authority": c.Request.Tree()["authority"], "recorded_at": stamp, "recorded_by": c.Request.Tree()["caller"],
		"request_id": c.Request.RequestID, "receipt_id": receipt.ReceiptID,
	}
	next := copyValue(l.Data).(map[string]any)
	switch kind {
	case rule.AmendmentExtend, rule.AmendmentEnd:
		at := str(body["new_expires_at"])
		t, err := clock.Parse(at)
		if err != nil {
			return nil, invalid("new_expires_at is not a calendar instant")
		}
		amendment["new_expires_at"] = at
		// An end whose time has come is the rule's status; one that lies ahead
		// is read from the clock when it arrives, as every expiry is.
		if kind == rule.AmendmentEnd && !t.After(now) {
			next["status"] = rule.StatusEnded
		}
	case rule.AmendmentLift:
		receiptBody := tree(body["receipt"])
		if !meetsCondition(r, receiptBody) {
			return nil, envelope.Common(envelope.CodeAuthorityInsufficient, "the receipt is not of the kind and about the subject the rule's lift condition names")
		}
		amendment["receipt"] = copyValue(receiptBody)
		amendment["evidence"] = evidenceOf(body, receiptBody)
		next["status"] = rule.StatusLifted
	}
	next["amendments"] = append(list(next["amendments"]), amendment)
	next["updated_at"] = stamp

	// The rule as it will stand, for the planner: the revision it is written at.
	planned := copyValue(next).(map[string]any)
	planned["record_revision"] = l.Revision + 1
	pr, err := rule.ParseRule(planned)
	if err != nil {
		return nil, internal("the amended rule cannot be read", err)
	}
	snap, err := memory.Load(c.Store, c.Home)
	if err != nil {
		return nil, err
	}
	if _, err := snap.Plan(now, memory.Write{Rules: []*rule.Rule{pr}}); err != nil {
		return nil, err
	}

	final, ref, err := b.put("rule", id, next, l.Revision)
	if err != nil {
		return nil, err
	}
	if err := b.seal(receipt, ref, r.Scope, true); err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"rule": final})
}

// meetsCondition says whether the receipt, as the request carries it whole, is of
// the kind and about the subject the rule's lift condition names. A rule with no
// lift condition has none to meet.
func meetsCondition(r *rule.Rule, receipt map[string]any) bool {
	if r.Lift == nil || receipt == nil {
		return false
	}
	candidate := *r
	candidate.Amendments = []rule.Amendment{{Kind: rule.AmendmentLift, Receipt: receipt}}
	return candidate.Lifted()
}

// evidenceOf is the receipt of a lift by reference: the reference the caller
// names, or one made from the receipt itself.
func evidenceOf(body, receipt map[string]any) map[string]any {
	if ev := tree(body["evidence"]); ev != nil {
		return copyValue(ev).(map[string]any)
	}
	ref := map[string]any{"schema": "hive.receipt", "id": str(receipt["receipt_id"])}
	if home := str(receipt["home_id"]); home != "" {
		ref["home_id"] = home
	}
	return ref
}
