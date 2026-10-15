package memory

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/read"
)

// CheckOp is hive-memory check (spec/pool.md, Generated knowledge): read-only,
// under the lock shared, it reports each projection in the scope of the
// request against the budget record: its state, computed at the clock from the
// rules in force and never stored; its size and whether it is over the budget
// its entry gives it; its stale sources; the rule lines it shares with another
// projection of its scope and audience, which are never the same always line in
// the Hive and Fleet audiences, since that is two audiences and not one; and the
// consumers whose received or acknowledged digest is not the current one. It
// repairs nothing.
//
// The misses counted against each subject belong to the lane that records and
// counts them: until then the report's subjects member is empty, which says
// nothing of any subject.
func CheckOp(c *cli.Call) (*cli.Result, error) {
	st, release, err := read.OpenShared(c)
	if err != nil {
		return nil, err
	}
	defer release()
	snap, err := Load(st, c.Home)
	if err != nil {
		return nil, err
	}
	now := c.Clock.Now()
	checkedAt, err := c.Stamp()
	if err != nil {
		return nil, internal("the time of the check cannot be written", err)
	}
	want := str(c.Request.Body["projection_id"])
	rows := []any{}
	for _, e := range snap.Projections {
		if (want != "" && e.ID != want) || checkContained(c, e.Scope) != nil {
			continue
		}
		f, err := snap.Fresh(e, now)
		if err != nil {
			return nil, envelope.Common(envelope.CodeRecordMalformed, "a rule cannot be placed in time").Because(err)
		}
		max := snap.Budget.Max(e.ID, e.Key())
		duplicates := []any{}
		for _, r := range e.Current.Rules {
			for _, o := range snap.Projections {
				if o.ID == e.ID || o.Scope != e.Scope || o.Audience != e.Audience {
					continue
				}
				for _, or := range o.Current.Rules {
					if or.RuleID == r.RuleID {
						duplicates = append(duplicates, map[string]any{"rule_id": r.RuleID, "also_in": o.ID})
					}
				}
			}
		}
		drift := []any{}
		for _, v := range list(e.Data["consumers"]) {
			cons := tree(v)
			ack := tree(cons["acknowledged"])
			if str(tree(cons["received"])["digest"]) != e.Current.Digest || (ack != nil && str(ack["digest"]) != e.Current.Digest) {
				drift = append(drift, copyValue(cons))
			}
		}
		rows = append(rows, map[string]any{
			"projection_id": e.ID, "audience": e.Audience, "state": f.State, "bytes": e.Current.Bytes,
			"over_budget": e.Current.Bytes > max, "stale_sources": staleTrees(c.Home.HomeID, f.Stale),
			"duplicates": duplicates, "drift": drift, "max_bytes": max,
		})
	}
	if want != "" && len(rows) == 0 {
		return nil, envelope.Common(envelope.CodeNotFound, "no projection in the scope of the request has that identity")
	}
	return &cli.Result{Body: map[string]any{
		"checked_at": checkedAt, "projections": rows, "budget_revision": snap.Budget.Revision, "subjects": []any{},
	}}, nil
}
