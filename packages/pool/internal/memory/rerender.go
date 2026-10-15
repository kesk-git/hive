package memory

import (
	"cmp"
	"slices"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// Rerender adds to the transaction of the call the re-rendering that a write to
// the rule store or to a subject owes (spec/pool.md, Generated knowledge): what
// BudgetOp renders for a budget write, rendered for the records as the write
// leaves them. Every projection that stands is rendered again against them, and
// one that is equal in text, exact source revisions, rule inventory and budget
// revision to the rendering it has writes nothing. A projection the write brings
// and none yet holds is rendered with its entry in the budget record, initialized
// from the defaults, as hive-memory render does, and the budget record's new
// revision is the one every rendering of the transaction records.
//
// The projections a write brings are the always projections of a rule's scope
// for an always rule, the projections of its filing subject at its scope for a
// subject rule, in the audiences it applies to, and those of a written subject at
// its scope in every audience, whose facts render for each. The write is checked
// with Plan before Rerender is called, so a rendering over its budget is already
// refused; Rerender refuses one again, and a rendering that conflicts.
//
// The transaction returned holds the transaction given and the projections,
// receipts and events of the renderings, and the budget record where a
// projection is new. It is for the caller to stage.
func (s *Snapshot) Rerender(c *cli.Call, w Write, tx poolstore.Transaction) (poolstore.Transaction, error) {
	after := s.after(w)
	var targets []target
	for _, e := range s.Projections {
		targets = append(targets, target{key: e.Key()})
	}
	var brought []Key
	for _, k := range bringing(w) {
		if _, stands := s.projection(k); !stands && !slices.Contains(brought, k) {
			brought = append(brought, k)
		}
	}
	slices.SortFunc(brought, func(a, b Key) int { return cmp.Compare(a.String(), b.String()) })
	budgetRevision := s.Budget.Revision
	if len(brought) > 0 {
		budgetRevision++
	}
	for _, k := range brought {
		targets = append(targets, target{key: k})
	}

	b := &builder{c: c, tx: tx}
	projections, _, err := b.renderAll(after, targets, budgetRevision)
	if err != nil {
		return tx, err
	}
	if len(brought) > 0 {
		next := copyValue(s.budgetData).(map[string]any)
		entries := list(next["projections"])
		for i, k := range brought {
			p := projections[len(s.Projections)+i]
			entries = append(entries, map[string]any{"projection_id": str(p["projection_id"]), "max_bytes": s.Budget.Max("", k)})
		}
		next["projections"] = entries
		if _, _, err := b.put("budget", budgetID, next, s.Budget.Revision); err != nil {
			return tx, err
		}
	}
	return b.tx, nil
}

// bringing are the keys of the projections the write brings: those a rendering
// of the records it leaves is owed for its rules and its subjects.
func bringing(w Write) []Key {
	var keys []Key
	add := func(k Key) {
		if k.Valid() == nil {
			keys = append(keys, k)
		}
	}
	for _, r := range w.Rules {
		if r.Kind != rule.KindOperating || r.TaskID != "" {
			continue
		}
		switch r.Tier {
		case TierAlways:
			add(Key{Scope: r.Scope, Tier: TierAlways, Audience: "fleet"})
			add(Key{Scope: r.Scope, Tier: TierAlways, Audience: "hive"})
		case TierSubject:
			audiences := r.Applies
			if len(audiences) == 0 {
				audiences = roles
			}
			for _, a := range audiences {
				add(Key{Scope: r.Scope, Tier: TierSubject, Audience: a, SubjectID: r.FiledUnder})
			}
		}
	}
	for _, sub := range w.Subjects {
		for _, a := range roles {
			add(Key{Scope: sub.Scope, Tier: TierSubject, Audience: a, SubjectID: sub.SubjectID})
		}
	}
	return keys
}
