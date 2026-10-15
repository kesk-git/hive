package memory

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/packages/pool/internal/budget"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// BudgetOp is hive-memory budget (spec/pool.md, Generated knowledge, the budget
// record). It is the budget record's writer, which it calls and does not
// rewrite, with the check that current and reachable renderings fit: the
// planner renders, under the budgets the request supplies, every projection
// that stands and every one reachable, at the clock and at each later instant
// the live set changes, and a decrease that cannot fit is refused with
// pool.projection_over_budget before anything is staged, never applied first.
// The projections that stand are rendered again in the same transaction, at the
// new revision of the record, which every projection records.
//
// The request names the expected revision of the budget record, as spec/pool.md
// lists the check for this operation; a request without one is invalid.
func BudgetOp(c *cli.Call) (*cli.Result, error) {
	named := false
	for _, e := range c.Request.ExpectedRevisions {
		named = named || (e.Schema == schemaBudget && e.ID == budgetID)
	}
	if !named {
		return nil, invalid("hive-memory budget names the expected revision of the budget record, " + schemaBudget + " " + budgetID)
	}
	change, err := budget.Prepare(c)
	if err != nil {
		return nil, err
	}
	snap, err := Load(c.Store, c.Home.HomeID)
	if err != nil {
		return nil, err
	}
	entries := map[string]int64{}
	for _, e := range change.After {
		entries[e.ProjectionID] = e.MaxBytes
	}
	w := Write{Budget: entries}
	if _, err := snap.Plan(c.Clock.Now(), w); err != nil {
		return nil, err
	}
	result, err := change.Stage()
	if err != nil {
		return nil, err
	}
	b := &builder{c: c}
	var targets []target
	for _, e := range snap.Projections {
		targets = append(targets, target{key: e.Key()})
	}
	if _, _, err := b.renderAll(snap.after(w), targets, snap.Budget.Revision+1); err != nil {
		return nil, err
	}
	if len(b.tx.Records) == 0 {
		return result, nil
	}
	staged, err := poolstore.Stage(c, b.tx)
	if err != nil {
		return nil, err
	}
	result.Records = append(result.Records, staged.Records...)
	result.Receipts = append(result.Receipts, staged.Receipts...)
	result.Events = append(result.Events, staged.Events...)
	return result, nil
}
