package read

import (
	"github.com/kesk-git/hive/packages/pool/internal/store"
)

// The reader of the kind imperative (spec/pool.md, The Imperative and Reading
// the Pool). It returns the canonical Imperative of an item whole, every
// revision and every delivery, as it is stored, with no derived facts: an
// Imperative stands alone, so a read by identity takes no lock and a query
// takes the lock shared.

// ImperativeMembers are the selector members of a read of kind imperative, as
// the request schema and spec/pool.md give them to the kind: the identity, which
// is the item's task_id, the project and the limit. The schema attributes the
// task_id selector to the holds and admissions and questions that concern one
// item, and the states, the ready and active predicates and the due time to
// other kinds.
var ImperativeMembers = []string{MemberID, MemberProjectID, MemberLimit}

// RegisterImperativeReader registers the reader of the kind imperative.
func RegisterImperativeReader(r *Registry) {
	r.Register(Kind{
		Name:       "imperative",
		Members:    ImperativeMembers,
		Standalone: func(s Selector) bool { return s.ID != "" },
		Read:       readImperatives,
	})
}

func readImperatives(c *Context) (*Result, error) {
	return readKind(c, func(sel Selector, l store.Loaded) (bool, error) {
		return !sel.Has(MemberProjectID) || l.Data["project_id"] == sel.ProjectID, nil
	})
}
