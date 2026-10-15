package read

import "github.com/kesk-git/hive/packages/pool/internal/store"

// The readers of the kinds note and maintenance-queue (spec/pool.md, Generated
// knowledge, The maintenance queue and the fold; Reading the Pool). Each
// returns whole records as they are stored, with no derived facts.
//
// The request schema gives these kinds no selector member of their own, and
// Reading the Pool names no reader that selects them, so each declares what the
// schema gives every kind: the identity, which selects one record, and the
// limit. A queue has one record, the home's, and a read by its identity or of
// all of them returns it; a note is read by its identity or listed in the order
// of the identities. Nothing narrower is declared: a note's state, scope and
// subject are not selectors the contract gives it.

// NoteMembers are the selector members of a read of kind note.
var NoteMembers = []string{MemberID, MemberLimit}

// QueueMembers are the selector members of a read of kind maintenance-queue.
var QueueMembers = []string{MemberID, MemberLimit}

// RegisterMaintenanceReaders registers the readers of the kinds note and
// maintenance-queue. A record of either kind stands alone, so a read by
// identity takes no lock and a list takes the lock shared.
func RegisterMaintenanceReaders(r *Registry) {
	standalone := func(s Selector) bool { return s.ID != "" }
	read := func(c *Context) (*Result, error) {
		return readKind(c, func(Selector, store.Loaded) (bool, error) { return true, nil })
	}
	r.Register(Kind{Name: "note", Members: NoteMembers, Standalone: standalone, Read: read})
	r.Register(Kind{Name: "maintenance-queue", Members: QueueMembers, Standalone: standalone, Read: read})
}
