package read

import (
	"github.com/kesk-git/hive/packages/pool/internal/store"
)

// The reader of the kind admission (spec/pool.md, Admission and Reading the
// Pool). It returns the admission records whole, as they are stored, with no
// derived facts: an admission stands alone, so a read by identity takes no
// lock and a query takes the lock shared.

// AdmissionMembers are the selector members of a read of kind admission, as the
// request schema and spec/pool.md give them to the kind: the identity, which is
// the admission_id, the item whose admissions are wanted, the project and the
// limit. The schema attributes the states to items, holds, rules, questions and
// decision rounds, the ready and active predicates and the due time to other
// kinds, and spec/pool.md reads an admission "in state admitted" from the
// record's own state: the reader filters by none of them.
var AdmissionMembers = []string{MemberID, MemberTaskID, MemberProjectID, MemberLimit}

// RegisterAdmissionReader registers the reader of the kind admission.
func RegisterAdmissionReader(r *Registry) {
	r.Register(Kind{
		Name:       "admission",
		Members:    AdmissionMembers,
		Standalone: func(s Selector) bool { return s.ID != "" },
		Read:       readAdmissions,
	})
}

func readAdmissions(c *Context) (*Result, error) {
	return readKind(c, func(sel Selector, l store.Loaded) (bool, error) {
		switch {
		case sel.Has(MemberTaskID) && l.Data["task_id"] != sel.TaskID:
			return false, nil
		case sel.Has(MemberProjectID) && l.Data["project_id"] != sel.ProjectID:
			return false, nil
		}
		return true, nil
	})
}
