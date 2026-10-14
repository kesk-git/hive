package read

import "testing"

// The specification reader declares the members the request schema and
// spec/pool.md give the kind, and a record by identity stands alone.
func TestTheSpecificationKindDeclaresItsMembers(t *testing.T) {
	r := NewRegistry()
	RegisterSpecificationReader(r)
	k, ok := r.Lookup("specification")
	if !ok {
		t.Fatal("no reader of specification")
	}
	if !slicesEqual(k.Members, MemberID, MemberProjectID, MemberLimit) {
		t.Errorf("specification: %v", k.Members)
	}
	if !k.Standalone(Selector{ID: "x"}) || k.Standalone(Selector{}) {
		t.Error("a record by identity stands alone and a query does not")
	}
}
