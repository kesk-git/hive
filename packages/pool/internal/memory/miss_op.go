package memory

import (
	"errors"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// MissOp is hive-memory miss (spec/pool.md, Generated knowledge, Misses). It
// receives the exact cited rule revision, the acting session or task generation,
// the step, the observation time and the provenance of the incident, and checks
// every retained received history and every Imperative delivery of that actor
// before it writes.
//
// The Pool records a miss against the rule's filing subject and the step when the
// actor holds neither a pool.projection.received receipt of a projection that
// carried the rule at the revision cited nor a delivery whose rules name it at
// that revision, each no later than the incident; it writes the miss and
// pool.rule.missed, and answers recorded true with the record. When the actor
// holds one, it answers recorded false, with no miss and no receipt.
//
// A request that names miss_id corrects the provenance of that miss only, under
// the revision it expects: the former provenance is kept in provenance_history,
// pool.miss.revised is written, and the rule, actor, step and observation time
// are what they were.
func MissOp(c *cli.Call) (*cli.Result, error) {
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	if id := str(c.Request.Body["miss_id"]); id != "" {
		return reviseMiss(c, id)
	}
	return recordMiss(c)
}

func recordMiss(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	cited := tree(body["rule"])
	revision := num(cited["record_revision"])
	rl, err := readCited(c.Store, str(cited["rule_id"]), revision)
	if err != nil {
		return nil, err
	}
	observed, err := parseInstant(str(body["observed_at"]))
	if err != nil {
		return nil, invalid("observed_at is not a calendar instant")
	}
	actor := tree(body["actor"])
	held, err := receivedRule(c.Store, actor, rl.ID, rl.Revision, observed)
	if err != nil {
		return nil, err
	}
	if held {
		return &cli.Result{Body: map[string]any{"recorded": false}}, nil
	}
	stamp, err := c.Stamp()
	if err != nil {
		return nil, internal("the time of the miss cannot be written", err)
	}
	b := &builder{c: c}
	final, err := b.stageMiss(rl, actor, str(body["step"]), copyValue(body["provenance"]).(map[string]any), str(body["observed_at"]), stamp)
	if err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"recorded": true, "miss": final})
}

// stageMiss adds a new miss and its receipt to the transaction, and returns the
// record as it will be written.
func (b *builder) stageMiss(rl *citedRule, actor map[string]any, step string, provenance map[string]any, observedAt, stamp string) (map[string]any, error) {
	id, err := mintMissID(b.c)
	if err != nil {
		return nil, internal("a miss cannot be given an identity", err)
	}
	rec := map[string]any{
		"schema": schemaMiss, "schema_version": missVersion, "record_revision": int64(1),
		"miss_id": id, "subject_id": rl.Subject.SubjectID,
		"rule":  map[string]any{"rule_id": rl.ID, "record_revision": rl.Revision},
		"actor": copyValue(actor), "step": step, "provenance": provenance, "provenance_history": []any{},
		"observed_at": observedAt, "created_at": stamp, "updated_at": stamp,
	}
	putScope(rec, rl.Subject.Scope)
	final, ref, err := b.put(kindMiss, id, rec, 0)
	if err != nil {
		return nil, err
	}
	if err := b.receipt("", receiptMissed, ref, rl.Subject.Scope); err != nil {
		return nil, err
	}
	return final, nil
}

// reviseMiss corrects the provenance of the miss under the revision the request
// expects. It changes nothing else of the miss, and it deletes nothing: the former
// provenance stays in provenance_history with who changed it and when.
func reviseMiss(c *cli.Call, id string) (*cli.Result, error) {
	if !expectsMiss(c, id) {
		return nil, invalid("a correction of a miss names the expected revision of the miss, " + schemaMiss + " " + id)
	}
	l, om, err := poolstore.Load(c.Store, kindMiss, id)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, envelope.Common(envelope.CodeNotFound, "no miss has that identity")
	case err != nil:
		return nil, poolstore.Failure("the miss", err)
	case om != nil:
		return nil, malformed("the miss cannot be read", om.ID, nil)
	}
	m, err := ParseMiss(l.Data)
	if err != nil {
		return nil, malformed("the miss cannot be read", id, err)
	}
	body := c.Request.Body
	given := tree(body["rule"])
	if str(given["rule_id"]) != m.RuleID || num(given["record_revision"]) != m.RuleRevision ||
		!sameTree(body["actor"], m.Actor) || str(body["step"]) != m.Step || str(body["observed_at"]) != m.ObservedAt {
		return nil, invalid("a correction changes the provenance of a miss only: its rule, actor, step and observation time are what they were")
	}
	next := copyValue(body["provenance"])
	if sameTree(next, l.Data["provenance"]) {
		return &cli.Result{Body: map[string]any{"recorded": true, "miss": l.Data}}, nil
	}
	stamp, err := c.Stamp()
	if err != nil {
		return nil, internal("the time of the correction cannot be written", err)
	}
	rec := copyValue(l.Data).(map[string]any)
	rec["provenance_history"] = append(list(rec["provenance_history"]), map[string]any{
		"provenance": copyValue(l.Data["provenance"]), "changed_at": stamp,
		"changed_by": c.Request.Tree()["caller"], "request_id": c.Request.RequestID,
	})
	rec["provenance"], rec["updated_at"] = next, stamp
	b := &builder{c: c}
	final, ref, err := b.put(kindMiss, id, rec, l.Revision)
	if err != nil {
		return nil, err
	}
	if err := b.receipt("", receiptMissRevised, ref, m.Scope); err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"recorded": true, "miss": final})
}

// expectsMiss says whether the request names the expected revision of the miss.
func expectsMiss(c *cli.Call, id string) bool {
	for _, e := range c.Request.ExpectedRevisions {
		if e.Schema == schemaMiss && e.ID == id {
			return true
		}
	}
	return false
}
