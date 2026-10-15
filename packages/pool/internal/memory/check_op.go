package memory

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
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
// It also reports the misses counted against each subject in the scope of the
// request: the number of distinct verified independent root incidents, the
// misses that contribute to it, and whether a decision is due, which is so once
// the count reaches two and no earlier question names the subject in its
// origin. It records no question and places no hold; a Shadow wakes the
// supervisor that owns the home, which raises the card.
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
	subjects, err := missRows(c, st)
	if err != nil {
		return nil, err
	}
	return &cli.Result{Body: map[string]any{
		"checked_at": checkedAt, "projections": rows, "budget_revision": snap.Budget.Revision, "subjects": subjects,
	}}, nil
}

// missRows are the subjects with a miss in the scope of the request, each with
// its count over the misses within it.
func missRows(c *cli.Call, st *cstore.Store) ([]any, error) {
	misses, err := loadMisses(st)
	if err != nil {
		return nil, err
	}
	counts := countMisses(misses, func(m *Miss) bool { return checkContained(c, m.Scope) == nil })
	if err := decisionsDue(st, counts); err != nil {
		return nil, err
	}
	rows := make([]any, len(counts))
	for i, s := range counts {
		rows[i] = s.tree()
	}
	return rows, nil
}
