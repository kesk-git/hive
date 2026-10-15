package read

import "testing"

// The rule and subject readers declare the members the request schema and
// spec/pool.md give the kinds, due_before among those of rules, and a record
// by identity stands alone.
func TestTheRuleKindsDeclareTheirMembers(t *testing.T) {
	r := NewRegistry()
	RegisterRuleReaders(r)
	for name, members := range map[string][]string{
		"rule":    {MemberID, MemberProjectID, MemberStates, MemberDueBefore, MemberLimit},
		"subject": {MemberID, MemberProjectID, MemberLimit},
	} {
		k, ok := r.Lookup(name)
		if !ok {
			t.Fatalf("no reader of %s", name)
		}
		if !slicesEqual(k.Members, members...) {
			t.Errorf("%s: %v", name, k.Members)
		}
		if !k.Standalone(Selector{ID: "x"}) || k.Standalone(Selector{}) {
			t.Errorf("%s: a record by identity stands alone and a query does not", name)
		}
	}
}

// The expiry a rule is due at is the one the clock reads: the new expiry of the
// latest extend or end amendment by sequence, a note leaving it as it was.
func TestTheExpiryTheReadDuesARuleBy(t *testing.T) {
	for _, tc := range []struct {
		name string
		data map[string]any
		want string
	}{
		{"none", map[string]any{}, ""},
		{"original", map[string]any{"expires_at": "2026-10-20T00:00:00.000Z"}, "2026-10-20T00:00:00.000Z"},
		{"latest by sequence", map[string]any{"expires_at": "2026-10-20T00:00:00.000Z", "amendments": []any{
			map[string]any{"sequence": int64(2), "new_expires_at": "2026-10-30T00:00:00.000Z"},
			map[string]any{"sequence": int64(1), "new_expires_at": "2026-10-25T00:00:00.000Z"},
			map[string]any{"sequence": int64(3)},
		}}, "2026-10-30T00:00:00.000Z"},
	} {
		at, ok, err := expiryOf(tc.data)
		if err != nil || ok != (tc.want != "") || (ok && at.UTC().Format("2006-01-02T15:04:05.000Z") != tc.want) {
			t.Errorf("%s: %v %v %v", tc.name, at, ok, err)
		}
	}
}
