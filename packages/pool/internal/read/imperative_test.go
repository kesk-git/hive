package read

import "testing"

// The imperative reader declares the members the request schema and
// spec/pool.md give the kind, and a record by identity stands alone.
func TestTheImperativeKindDeclaresItsMembers(t *testing.T) {
	r := NewRegistry()
	RegisterImperativeReader(r)
	k, ok := r.Lookup("imperative")
	if !ok {
		t.Fatal("no reader of imperative")
	}
	if !slicesEqual(k.Members, MemberID, MemberProjectID, MemberLimit) {
		t.Errorf("imperative: %v", k.Members)
	}
	if !k.Standalone(Selector{ID: "x"}) || k.Standalone(Selector{}) {
		t.Error("a record by identity stands alone and a query does not")
	}
}
