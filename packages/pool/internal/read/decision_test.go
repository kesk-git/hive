package read

import (
	"testing"
)

// The members a decision reader declares are those of the request schema as it
// attributes them to the kinds, and a question alone is read by a date.
func TestTheDecisionKindsDeclareTheirMembers(t *testing.T) {
	r := NewRegistry()
	RegisterDecisionReaders(r)
	q, _ := r.Lookup("question")
	d, _ := r.Lookup("decision-round")
	if !slicesEqual(q.Members, MemberID, MemberTaskID, MemberProjectID, MemberStates, MemberDueBefore, MemberLimit) {
		t.Errorf("question: %v", q.Members)
	}
	if !slicesEqual(d.Members, MemberID, MemberProjectID, MemberStates, MemberLimit) {
		t.Errorf("decision-round: %v", d.Members)
	}
	for _, k := range []Kind{q, d} {
		if !k.Standalone(Selector{ID: "x"}) || k.Standalone(Selector{}) {
			t.Errorf("%s: a record by identity stands alone and a query does not", k.Name)
		}
	}
}

// A question is due at its revisit date while it is deferred, which is the
// record's deferral; a record with none is due at no instant, and a date that
// is no instant is an error and never none.
func TestARevisitDateIsTheDeferrals(t *testing.T) {
	at, ok, err := revisitAt(map[string]any{"deferral": map[string]any{"revisit_at": "2026-10-20T09:00:00.000Z", "answer_sequence": int64(1)}})
	if err != nil || !ok || at.UTC().Format("2006-01-02T15:04:05Z") != "2026-10-20T09:00:00Z" {
		t.Errorf("a deferral: %v %v %v", at, ok, err)
	}
	if _, ok, err := revisitAt(map[string]any{"state": "open"}); ok || err != nil {
		t.Errorf("no deferral: %v %v", ok, err)
	}
	if _, _, err := revisitAt(map[string]any{"deferral": map[string]any{"revisit_at": "next week"}}); err == nil {
		t.Error("a date that is no instant was read")
	}
}
