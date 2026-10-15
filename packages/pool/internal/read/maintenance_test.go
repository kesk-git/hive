package read

import "testing"

// The readers of the kinds note and maintenance-queue declare the members the
// request schema gives every kind, the identity and the limit, and nothing the
// contract leaves to another kind; a read by identity takes no lock.
func TestTheMaintenanceKindsDeclareTheirMembers(t *testing.T) {
	r := NewRegistry()
	RegisterMaintenanceReaders(r)
	for _, name := range []string{"note", "maintenance-queue"} {
		k, ok := r.Lookup(name)
		if !ok {
			t.Fatalf("no reader of %s", name)
		}
		if !slicesEqual(k.Members, MemberID, MemberLimit) {
			t.Errorf("%s: members %v", name, k.Members)
		}
		if !k.Standalone(Selector{ID: "x", Present: []string{MemberID}}) || k.Standalone(Selector{}) {
			t.Errorf("%s: a read by identity stands alone and a list does not", name)
		}
	}
}
