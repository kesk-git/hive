package read

import (
	"testing"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/store"
)

func noRead(*Context) (*Result, error) { return &Result{}, nil }

func panics(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: registered", name)
		}
	}()
	f()
}

// A registration that cannot be right is a mistake of the program and stops
// it: a kind a read cannot name, a kind twice, a kind with no reader, and a
// member that does not apply to the kind.
func TestARegistrationThatCannotBeRightPanics(t *testing.T) {
	r := NewRegistry()
	r.Register(Kind{Name: "hold", Members: []string{MemberID, MemberActive, MemberDueBefore}, Read: noRead})
	r.Register(Kind{Name: RequestOutcome, Members: []string{MemberID}, Read: noRead})
	panics(t, "a kind that is not one", func() { r.Register(Kind{Name: "nothing", Read: noRead}) })
	panics(t, "a kind twice", func() { r.Register(Kind{Name: "hold", Read: noRead}) })
	panics(t, "no reader", func() { r.Register(Kind{Name: "item"}) })
	panics(t, "a member that is not one", func() { r.Register(Kind{Name: "item", Members: []string{"owner"}, Read: noRead}) })
	panics(t, "ready on a hold", func() { r.Register(Kind{Name: "admission", Members: []string{MemberReady}, Read: noRead}) })
	if _, ok := r.Lookup("item"); ok || r.Len() != 2 {
		t.Errorf("the kinds registered are %v", r.Names())
	}
	// What the schema and the spec leave to the kind is the kind's to declare;
	// what they say outright is not.
	for kind, wants := range map[string]map[string]bool{
		RequestOutcome: {MemberID: true, MemberTaskID: false, MemberStates: false, MemberLimit: false},
		"item":         {MemberReady: true, MemberActive: false, MemberDueBefore: false, MemberTaskID: true},
		"hold":         {MemberReady: false, MemberActive: true, MemberDueBefore: true},
		"rule":         {MemberActive: false, MemberDueBefore: true},
	} {
		for member, want := range wants {
			if applies(kind, member) != want {
				t.Errorf("%s %s: applies %v", kind, member, !want)
			}
		}
	}
}

func call() *cli.Call {
	return &cli.Call{Home: &cstore.Home{HomeID: "home_01m4xkf4z52va9yrp2031hchs1"}}
}

func rec(id string) Record { return Record{ID: id, Data: map[string]any{"id": id}} }

func header(t *testing.T, res *cli.Result) map[string]any {
	t.Helper()
	return res.Body["projection"].(map[string]any)
}

// A read is complete when nothing is missing; a limit that cuts the list, and
// a record that could not be read, make it incomplete and are named, and an
// identity that selects nothing is not_found and never an empty list.
func TestTheProjectionSaysWhatIsMissing(t *testing.T) {
	at := "2026-10-14T16:00:00.000Z"
	res, err := assemble(call(), "hold", Selector{}, at, 7, &Result{Records: []Record{rec("a"), rec("b")}})
	if err != nil {
		t.Fatal(err)
	}
	h := header(t, res)
	if h["complete"] != true || h["journal_cursor"] != int64(7) || h["read_at"] != at || h["omitted"] != nil || len(res.Body["records"].([]any)) != 2 {
		t.Errorf("a whole list: %v", h)
	}

	res, err = assemble(call(), "hold", Selector{Limit: 1}, at, 0, &Result{Records: []Record{rec("a"), rec("b"), rec("c")}})
	if err != nil {
		t.Fatal(err)
	}
	h = header(t, res)
	omitted := h["omitted"].([]any)
	if h["complete"] != false || len(res.Body["records"].([]any)) != 1 || len(omitted) != 2 || omitted[0].(map[string]any)["reason"] != "limit" || omitted[1].(map[string]any)["id"] != "c" {
		t.Errorf("a list cut by its limit: %v", h)
	}

	res, err = assemble(call(), "hold", Selector{ID: "a"}, at, 0, &Result{Omitted: []Omission{{ID: "a", Reason: store.ReasonMalformed}}})
	if err != nil {
		t.Fatal(err)
	}
	if h = header(t, res); h["complete"] != false || len(res.Body["records"].([]any)) != 0 || h["omitted"].([]any)[0].(map[string]any)["id"] != "a" {
		t.Errorf("an identity whose record cannot be read is an empty answer that says it is not whole: %v", h)
	}

	_, err = assemble(call(), "hold", Selector{ID: "a"}, at, 0, &Result{})
	if f := envelope.AsFailure(err); f == nil || f.Err.Code != envelope.CodeNotFound {
		t.Errorf("an identity with no record: %v", err)
	}
	res, err = assemble(call(), "hold", Selector{}, at, 0, &Result{})
	if err != nil || header(t, res)["complete"] != true {
		t.Errorf("a query that matches nothing: %v, %v", res, err)
	}
}

// Derived facts come in step with the records, for all of them or for none,
// and an omission the response cannot carry is the reader's fault.
func TestTheReaderIsHeldToTheShapeOfTheResponse(t *testing.T) {
	at := "2026-10-14T16:00:00.000Z"
	with := rec("a")
	with.Derived = map[string]any{"task_id": "a"}
	res, err := assemble(call(), "item", Selector{}, at, 0, &Result{Records: []Record{with}})
	if err != nil || len(res.Body["derived"].([]any)) != 1 {
		t.Errorf("derived facts: %v, %v", res, err)
	}
	for name, bad := range map[string]*Result{
		"derived facts for some": {Records: []Record{with, rec("b")}},
		"a reason of its own":    {Omitted: []Omission{{ID: "a", Reason: "unlucky"}}},
		"an omission unnamed":    {Omitted: []Omission{{Reason: store.ReasonLimit}}},
		"a record with no body":  {Records: []Record{{ID: "a"}}},
		"nothing":                nil,
	} {
		_, err := assemble(call(), "item", Selector{}, at, 0, bad)
		if f := envelope.AsFailure(err); f == nil || f.Err.Code != envelope.CodeInternal {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The selector members the request gave are those the reader sees, in the
// order of the schema.
func TestTheSelectorIsReadAsTheRequestGaveIt(t *testing.T) {
	s := selector(map[string]any{"selector": map[string]any{
		"limit": int64(3), "states": []any{"queued", "done"}, "ready": true, "task_id": "task_x", "id": "i",
	}})
	if s.ID != "i" || s.TaskID != "task_x" || !s.Ready || s.Limit != 3 || !slicesEqual(s.States, "queued", "done") ||
		!slicesEqual(s.Present, MemberID, MemberTaskID, MemberStates, MemberReady, MemberLimit) || s.Has(MemberActive) || !s.Has(MemberReady) {
		t.Errorf("selector %+v", s)
	}
}

func slicesEqual(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
