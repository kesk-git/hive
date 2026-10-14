package read

import (
	"github.com/kesk-git/hive/packages/pool/internal/store"
)

// The reader of the kind specification (spec/pool.md, Reading the Pool). It
// returns the records whole, as they are stored, with no derived facts: a
// specification stands alone, so a read by identity takes no lock and a query
// takes the lock shared.

// SpecificationMembers are the selector members of a read of kind
// specification, as the request schema and spec/pool.md give them to the kind:
// the identity, the project and the limit. The schema attributes the task, the
// states, the ready and active predicates and the due time to other kinds, and
// a specification is held to none of them.
var SpecificationMembers = []string{MemberID, MemberProjectID, MemberLimit}

// RegisterSpecificationReader registers the reader of the kind specification.
func RegisterSpecificationReader(r *Registry) {
	r.Register(Kind{
		Name:       "specification",
		Members:    SpecificationMembers,
		Standalone: func(s Selector) bool { return s.ID != "" },
		Read:       readSpecifications,
	})
}

func readSpecifications(c *Context) (*Result, error) {
	return readKind(c, func(sel Selector, l store.Loaded) (bool, error) {
		return !sel.Has(MemberProjectID) || l.Data["project_id"] == sel.ProjectID, nil
	})
}
