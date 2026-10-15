package read

import "testing"

// The admission reader declares the members the request schema and
// spec/pool.md give the kind, and a record by identity stands alone.
func TestTheAdmissionKindDeclaresItsMembers(t *testing.T) {
	r := NewRegistry()
	RegisterAdmissionReader(r)
	k, ok := r.Lookup("admission")
	if !ok {
		t.Fatal("no reader of admission")
	}
	if !slicesEqual(k.Members, MemberID, MemberTaskID, MemberProjectID, MemberLimit) {
		t.Errorf("admission: %v", k.Members)
	}
	if !k.Standalone(Selector{ID: "x"}) || k.Standalone(Selector{}) {
		t.Error("a record by identity stands alone and a query does not")
	}
}
