package read

import "testing"

// The projection reader declares the members the request schema and
// spec/pool.md give the kind, and a read of one takes the lock shared, since its
// freshness reads the rules, the subject and the budget beside it.
func TestTheProjectionKindDeclaresItsMembers(t *testing.T) {
	r := NewRegistry()
	RegisterProjectionReader(r, nil)
	k, ok := r.Lookup("projection")
	if !ok {
		t.Fatal("no reader of projection")
	}
	if !slicesEqual(k.Members, MemberID, MemberProjectID, MemberLimit) {
		t.Errorf("members %v", k.Members)
	}
	if k.Standalone != nil {
		t.Error("a projection read takes the lock shared, by identity as by query")
	}
}
