package rulewrite

import (
	"time"

	"github.com/kesk-git/hive/contract/canon"
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
// time the resolver uses. A rule no longer in force takes a note only. A lift is
// checked against the receipt the request carries whole, and the Pool reads no
// other store for it. The amend re-renders the projections of the rule's scope
// in its transaction and is refused with pool.projection_over_budget when one
// would exceed its budget.
//
// The request's expected_revisions names the rule's revision when the caller
// supplies it, and is checked then; neither the contract's sentence nor the
// request's schema requires one.
func Amend(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	id, kind := str(body["rule_id"]), str(body["kind"])
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
	if r.Subject == postureSubject {
		if err := rule.CheckPosture(c.Store, c.Request.Scope, c.Request.Authority); err != nil {
			return nil, err
		}
	}
	now := c.Clock.Now()
	if kind != rule.AmendmentNote {
		if err := checkStillInForce(r, now); err != nil {
			return nil, err
		}
	}
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
		if err := checkDirection(r, kind, t); err != nil {
			return nil, err
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
		evidence, err := evidenceOf(body, receiptBody)
		if err != nil {
			return nil, err
		}
		amendment["receipt"] = copyValue(receiptBody)
		amendment["evidence"] = evidence
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
	write := memory.Write{Rules: []*rule.Rule{pr}}
	if err := snap.RefuseConflicts(now, write); err != nil {
		return nil, err
	}
	if err := checkProductConflict(snap.Rules, now, pr); err != nil {
		return nil, err
	}
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

// checkStillInForce refuses an extend, an end or a lift of a rule no longer in
// force: ended, expired or lifted, or past its effective expiry by the clock. A
// rule ended at a later time is in force until then, and a note is taken by any
// rule.
func checkStillInForce(r *rule.Rule, now time.Time) error {
	if r.Status != rule.StatusInForce {
		return invalid("the rule is "+r.Status+" and no longer in force, so it takes a note only").With("status", r.Status)
	}
	past, err := r.ExpiredAt(now)
	if err != nil {
		return envelope.Common(envelope.CodeRecordMalformed, "the rule's expiry is not a calendar instant").With("record", r.RuleID).Because(err)
	}
	if past {
		expiry, _, _ := r.EffectiveExpiry()
		at, _ := clock.Format(expiry)
		return invalid("the rule is past its expiry time, "+at+", and no longer in force, so it takes a note only").With("status", r.Status).With("expires_at", at)
	}
	return nil
}

// checkDirection refuses an extend that does not move the rule's effective
// expiry later and an end that does not move it earlier; a rule with no expiry
// never expires, so it cannot be extended and any end is earlier.
func checkDirection(r *rule.Rule, kind string, t time.Time) error {
	current, has, err := r.EffectiveExpiry()
	if err != nil {
		return envelope.Common(envelope.CodeRecordMalformed, "the rule's expiry is not a calendar instant").With("record", r.RuleID).Because(err)
	}
	if kind == rule.AmendmentExtend && has && t.After(current) || kind == rule.AmendmentEnd && (!has || t.Before(current)) {
		return nil
	}
	if !has {
		return invalid("an extend moves the expiry later than the rule's current effective expiry, and the rule has none")
	}
	at, _ := clock.Format(current)
	direction := "later"
	if kind == rule.AmendmentEnd {
		direction = "earlier"
	}
	return invalid("an "+kind+" moves the expiry "+direction+" than the rule's current effective expiry, "+at).With("expires_at", at)
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
// names, which must name the receipt the request carries whole, by its schema
// and identity and by its home, its revision, which is the first, and its digest
// where the reference gives them; or one made from the receipt itself.
func evidenceOf(body, receipt map[string]any) (map[string]any, error) {
	ref := map[string]any{"schema": envelope.ReceiptSchema, "id": str(receipt["receipt_id"])}
	if home := str(receipt["home_id"]); home != "" {
		ref["home_id"] = home
	}
	ev := tree(body["evidence"])
	if ev == nil {
		return ref, nil
	}
	content, err := canon.Encode(receipt)
	if err != nil {
		return nil, internal("the receipt cannot be encoded", err)
	}
	same := ev["schema"] == ref["schema"] && ev["id"] == ref["id"]
	if home, given := ev["home_id"]; given && home != receipt["home_id"] {
		same = false
	}
	if revision, given := ev["record_revision"]; given && whole(revision) != 1 {
		same = false
	}
	if digest, given := ev["digest"]; given && digest != canon.FileDigest(content) {
		same = false
	}
	if !same {
		return nil, invalid("evidence does not name the receipt the request carries whole")
	}
	return copyValue(ev).(map[string]any), nil
}
