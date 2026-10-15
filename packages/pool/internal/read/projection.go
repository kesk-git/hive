package read

import (
	"github.com/kesk-git/hive/packages/pool/internal/store"
)

// The reader of the kind projection (spec/pool.md, Reading the Pool,
// Generated knowledge). It returns each projection whole, as it is stored, with
// its freshness beside it: the freshness is computed at the read from the rules
// in force at the clock, never stored, and is derived by the package that
// keeps the projections, which this package does not import, since the rule
// resolver it reads through imports this one. A projection selected by
// identity reads several records, so a read of one takes the lock shared, as
// every other read of it does.

// ProjectionMembers are the selector members of a read of kind projection, as
// the request schema and spec/pool.md give them to the kind: the identity, the
// project and the limit. The schema attributes the task, the states, the ready
// and active predicates and the due time to other kinds.
var ProjectionMembers = []string{MemberID, MemberProjectID, MemberLimit}

// Derive computes the freshness of the projections, given as they are stored,
// at the clock: one derived member for each, in order, naming the projection.
type Derive func(c *Context, projections []map[string]any) ([]map[string]any, error)

// RegisterProjectionReader registers the reader of the kind projection with the
// derivation of its freshness.
func RegisterProjectionReader(r *Registry, derive Derive) {
	r.Register(Kind{
		Name:    "projection",
		Members: ProjectionMembers,
		Read: func(c *Context) (*Result, error) {
			res, err := readKind(c, func(sel Selector, l store.Loaded) (bool, error) {
				return !sel.Has(MemberProjectID) || l.Data["project_id"] == sel.ProjectID, nil
			})
			if err != nil || len(res.Records) == 0 {
				return res, err
			}
			data := make([]map[string]any, len(res.Records))
			for i, r := range res.Records {
				data[i] = r.Data
			}
			derived, err := derive(c, data)
			if err != nil {
				return nil, err
			}
			for i := range res.Records {
				res.Records[i].Derived = derived[i]
			}
			return res, nil
		},
	})
}
