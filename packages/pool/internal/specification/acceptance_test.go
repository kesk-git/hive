package specification_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// The acceptance test of the Pool's specifications and the effective
// acceptance of an item's criteria (spec/pool.md, Specifications): the
// operations define, accept and close of hive-spec and the reader of the kind
// specification, in process over a home of their own. It holds the operations
// to the contract: the whole revisions define writes, the binding-by-binding
// invalidation it decides, the items that resolve through an invalidated
// binding, what accept binds and releases, the closure that is recorded once
// and replays, and the refusals the contract names.

// leaf is a Prey with no Hunt, an item of its own that is worked directly.
func leaf(title string, needs bool) map[string]any {
	b := under("prey", title, "")
	delete(b, "deps")
	if needs {
		b["specification_required"] = true
	}
	return b
}

func sameJSON(t testing.TB, what string, a, b any) {
	t.Helper()
	x, err1 := canon.Encode(map[string]any{"v": a})
	y, err2 := canon.Encode(map[string]any{"v": b})
	if err1 != nil || err2 != nil || string(x) != string(y) {
		t.Errorf("%s:\n  %s\n  %s", what, x, y)
	}
}

// revisionOf is the revision of the specification record.
func revisionOf(t testing.TB, rec map[string]any, n int) map[string]any {
	t.Helper()
	for _, v := range rec["revisions"].([]any) {
		if r := v.(map[string]any); r["revision"] == int64(n) {
			return r
		}
	}
	t.Fatalf("no revision %d in %v", n, rec["revisions"])
	return nil
}

// without is a copy of the record with the members taken out, the ones an
// ordinary rewrite sets.
func without(rec map[string]any, members ...string) map[string]any {
	out := map[string]any{}
	for k, v := range rec {
		if !slices.Contains(members, k) {
			out[k] = v
		}
	}
	return out
}

// rewritten are the members every rewrite of a record sets.
var rewritten = []string{"record_revision", "updated_at", "writer"}

func strs(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

// plantWordHold plants a hold of the Mind's word, which the decision intake
// alone places.
func (h *home) plantWordHold(taskID string) string {
	h.t.Helper()
	id := mint(h.t, ids.Hold)
	h.plant("hold", id, map[string]any{
		"schema": "pool.hold", "schema_version": "1.0", "record_revision": int64(1),
		"hold_id": id, "task_id": taskID, "scope_level": "hive", "home_id": h.homeID,
		"kind": "word", "reason": "Held for the Hive Mind.", "state": "active",
		"placed_at":  "2026-10-12T09:00:00.000Z",
		"placed_by":  map[string]any{"request_id": mint(h.t, ids.Request), "caller": map[string]any{"kind": "test"}},
		"created_at": "2026-10-12T09:00:00.000Z", "updated_at": "2026-10-12T09:00:00.000Z", "writer": writer("hold-question"),
	})
	return id
}

// withGeneration rewrites the stored item with one generation of the state, as
// open-generation, a later lane's operation, leaves it.
func (h *home) withGeneration(id, state string) []any {
	h.t.Helper()
	rec := h.item(id)
	g := map[string]any{
		"generation": int64(1), "state": state, "opened_at": "2026-10-14T09:00:00.000Z",
		"placement_requested": "mac", "imperative_revision": int64(1), "admission_id": mint(h.t, ids.Admission),
	}
	if state == "open" {
		rec["state"] = "in-flight"
	} else {
		g["closed_at"], g["outcome"] = "2026-10-14T10:00:00.000Z", "delivered"
		g["closed_by"] = map[string]any{"schema": "hive.receipt", "id": mint(h.t, ids.Receipt)}
	}
	rec["generations"] = []any{g}
	h.plant("item", id, rec)
	return rec["generations"].([]any)
}

// withImperative plants an Imperative from the record fixtures, keyed by the
// item and moved into the home.
func (h *home) withImperative(taskID string) {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas", "records_pool.imperative", "valid", "project-swarm-claude-mac.json"))
	if err != nil {
		h.t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		h.t.Fatal(err)
	}
	rec := v.(map[string]any)
	for _, k := range []string{"workspace_id", "project_id", "clone_id"} {
		delete(rec, k)
	}
	rec["scope_level"], rec["home_id"], rec["task_id"] = "hive", h.homeID, taskID
	h.plant("imperative", taskID, rec)
}

// A new specification is revision 1, a revision of it is the next, whole, and
// the revisions before it stay as they were.
func TestDefineWritesRevisionOneAndTheNextRevisionWhole(t *testing.T) {
	h := newHome(t)
	subj := subject(t)
	resp := h.spec("define", define("Counter reset", "The first prose.", criteria(), map[string]any{"touched_subjects": []any{subj}, "plan_stage": int64(2)}))
	body := h.ok(resp)
	rec := body["specification"].(map[string]any)
	id := rec["specification_id"].(string)
	if rec["title"] != "Counter reset" || rec["current_revision"] != int64(1) || rec["record_revision"] != int64(1) || len(rec["revisions"].([]any)) != 1 {
		t.Fatalf("revision 1: %v", rec)
	}
	one := revisionOf(t, rec, 1)
	if !canon.ValidDigest(one["digest"].(string)) || one["text"] != "The first prose." || one["plan_stage"] != int64(2) || one["request_id"] != resp.RequestID {
		t.Errorf("revision 1 %v", one)
	}
	if _, has := one["changed_criteria"]; has {
		t.Errorf("the first revision lists changed criteria: %v", one["changed_criteria"])
	}
	sameJSON(t, "touched_subjects", one["touched_subjects"], []any{subj})
	sameJSON(t, "criteria", one["criteria"], criteria())
	if one["defined_by"].(map[string]any)["kind"] != "test" || one["authority"].(map[string]any)["kind"] != "none" {
		t.Errorf("who defined it: %v %v", one["defined_by"], one["authority"])
	}
	if len(body["invalidated"].([]any)) != 0 || !slices.Equal(kinds(resp), []string{"pool.specification.defined"}) {
		t.Errorf("invalidated %v, receipts %v", body["invalidated"], kinds(resp))
	}
	sameJSON(t, "the stored record is the response's", rec, h.stored("specification", id))
	if resp.Receipts[0].Subject == nil || resp.Receipts[0].Subject.ID != id || resp.Receipts[0].Subject.RecordRevision != 1 {
		t.Errorf("the subject of the receipt: %+v", resp.Receipts[0].Subject)
	}

	next := []any{
		criterion("kept", "The collected sample is kept for a day.", "A test reads the sample a day later."),
		criterion("audited", "Each reset is logged.", "A test reads the log."),
	}
	resp = h.revise(id, "The second prose.", next, map[string]any{"touched_subjects": []any{subj}})
	two := h.ok(resp)["specification"].(map[string]any)
	if two["record_revision"] != int64(2) || two["current_revision"] != int64(2) || len(two["revisions"].([]any)) != 2 || two["title"] != "Counter reset" {
		t.Fatalf("revision 2: %v", two)
	}
	sameJSON(t, "revision 1 is kept whole", revisionOf(t, two, 1), one)
	r2 := revisionOf(t, two, 2)
	sameJSON(t, "criteria", r2["criteria"], next)
	changed := strs(r2["changed_criteria"])
	slices.Sort(changed)
	if !slices.Equal(changed, []string{"audited", "kept", "resets"}) {
		t.Errorf("changed_criteria %v", changed)
	}
	if r2["digest"] == one["digest"] || r2["text"] != "The second prose." {
		t.Errorf("revision 2 %v", r2)
	}
	if got := h.ok(h.pool("read", map[string]any{"kind": "specification", "selector": map[string]any{"id": id}}))["records"].([]any); len(got) != 1 {
		t.Errorf("a read of the specification finds %d records", len(got))
	}

	// A caller that read an older record revision is a conflict, and nothing
	// is written.
	h.quiet(func() {
		b := define("", "A third.", next, nil)
		b["specification_id"] = id
		refused(t, h.spec("define", b, rev{"pool.specification", id, 1}), envelope.Conflict, envelope.CodeRevisionConflict)
	})
}

// A revision that changes nothing an accepted item depends on keeps every
// binding, so that a prose-only revision never makes a dispatchable item
// undispatchable.
func TestAProseOnlyRevisionKeepsEveryBinding(t *testing.T) {
	h := newHome(t)
	p := h.addID(leaf("Needs a specification", true))
	if h.item(p)["definition"] != "needed" || len(h.activeHolds(p, "definition")) != 1 {
		t.Fatalf("the item is added needed with its hold")
	}
	id := h.defined(criteria(), nil)
	h.accepted(id, 1, p)
	before := h.bytes("item", p)

	resp := h.revise(id, "Only the prose changed.", criteria(), nil)
	body := h.ok(resp)
	if len(body["invalidated"].([]any)) != 0 || kinds(resp)[0] != "pool.specification.defined" || len(kinds(resp)) != 1 {
		t.Errorf("invalidated %v, receipts %v", body["invalidated"], kinds(resp))
	}
	if r2 := revisionOf(t, body["specification"].(map[string]any), 2); len(r2["changed_criteria"].([]any)) != 0 {
		t.Errorf("changed_criteria %v", r2["changed_criteria"])
	}
	if h.bytes("item", p) != before {
		t.Error("the item changed")
	}
	acceptances := body["specification"].(map[string]any)["acceptances"].([]any)
	if len(acceptances) != 1 || acceptances[0].(map[string]any)["state"] != "accepted" {
		t.Errorf("acceptances %v", acceptances)
	}
	if len(h.activeHolds(p, "definition")) != 0 {
		t.Error("a definition hold was placed")
	}
}

// Invalidation is decided per binding, against the revision each item is bound
// to: a revision unchanged against the one before it still invalidates an item
// bound to an earlier revision it differs from, and leaves one bound to the
// revision before it alone.
func TestAnItemBoundToAnEarlierRevisionIsInvalidatedByARevisionUnchangedAgainstTheOneBefore(t *testing.T) {
	h := newHome(t)
	early, late := h.addID(leaf("Bound to revision 1", false)), h.addID(leaf("Bound to revision 2", false))
	id := h.defined(criteria(), nil)
	changed := []any{criterion("resets", "The counter resets at once.", "A test reads the counter twice."), criteria()[1]}
	h.ok(h.revise(id, "Revision 2.", changed, nil))

	// What a build that decided invalidation from the last acceptance left
	// behind: an item still bound to revision 1, under a revision 2 that
	// differs from it.
	rec := h.stored("specification", id)
	var entries []any
	for item, n := range map[string]int{early: 1, late: 2} {
		digest := revisionOf(t, rec, n)["digest"]
		entries = append(entries, map[string]any{
			"revision": int64(n), "task_id": item, "digest": digest, "state": "accepted",
			"accepted_at": "2026-10-14T09:00:00.000Z", "accepted_by": map[string]any{"kind": "test"}, "receipt_id": mint(t, ids.Receipt),
		})
		it := h.item(item)
		it["specification"] = map[string]any{"specification_id": id, "revision": int64(n), "digest": digest}
		it["definition"] = "accepted"
		h.plant("item", item, it)
	}
	rec["acceptances"] = entries
	h.plant("specification", id, rec)
	keptBefore := h.bytes("item", late)

	resp := h.revise(id, "Revision 3, the same criteria as revision 2.", changed, nil)
	body := h.ok(resp)
	if !slices.Equal(strs(body["invalidated"]), []string{early}) {
		t.Fatalf("invalidated %v", body["invalidated"])
	}
	if r3 := revisionOf(t, body["specification"].(map[string]any), 3); len(r3["changed_criteria"].([]any)) != 0 {
		t.Errorf("revision 3 changes nothing against revision 2: %v", r3["changed_criteria"])
	}
	got := h.item(early)
	if _, bound := got["specification"]; bound || got["definition"] != "defining" || len(h.activeHolds(early, "definition")) != 1 {
		t.Errorf("the invalidated item %v", got)
	}
	if h.bytes("item", late) != keptBefore {
		t.Error("the item bound to revision 2 changed")
	}
	for _, v := range body["specification"].(map[string]any)["acceptances"].([]any) {
		a := v.(map[string]any)
		switch a["task_id"] {
		case early:
			if a["state"] != "invalidated" || a["invalidated_by"] != int64(3) || a["invalidated_at"] == nil {
				t.Errorf("the early acceptance %v", a)
			}
		case late:
			if a["state"] != "accepted" {
				t.Errorf("the late acceptance %v", a)
			}
		}
	}
	if count(kinds(resp), "pool.specification.invalidated") != 1 || count(kinds(resp), "pool.specification.defined") != 1 || count(kinds(resp), "pool.hold.placed") != 1 {
		t.Errorf("receipts %v", kinds(resp))
	}
}

// A change of a criterion's identity, text or evidence, or of a subject added
// or removed, invalidates an item bound to the revision; nothing else does.
func TestWhatInvalidatesABinding(t *testing.T) {
	s1, s2 := subject(t), subject(t)
	base := func() []any { return criteria() }
	for _, tc := range []struct {
		name       string
		crit       func() []any
		subjects   []any
		invalidate bool
	}{
		{"a criterion's text", func() []any {
			return []any{criterion("resets", "The counter resets at once.", "A test reads the counter twice."), base()[1]}
		}, []any{s1}, true},
		{"a criterion's evidence", func() []any {
			return []any{criterion("resets", "The counter resets after its sample is collected.", "A test reads the counter thrice."), base()[1]}
		}, []any{s1}, true},
		{"a criterion's identity", func() []any {
			return []any{criterion("zeroes", "The counter resets after its sample is collected.", "A test reads the counter twice."), base()[1]}
		}, []any{s1}, true},
		{"a criterion added", func() []any {
			return append(base(), criterion("logged", "Each reset is logged.", "A test reads the log."))
		}, []any{s1}, true},
		{"a criterion removed", func() []any { return base()[:1] }, []any{s1}, true},
		{"a subject added", base, []any{s1, s2}, true},
		{"a subject removed", base, []any{}, true},
		{"the prose alone", base, []any{s1}, false},
		{"the requirements a criterion answers", func() []any {
			c := base()
			c[0].(map[string]any)["requirements"] = []any{map[string]any{"kind": "doc", "path": "docs/example-notes.md"}}
			return c
		}, []any{s1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHome(t)
			p := h.addID(leaf("Bound", false))
			id := h.defined(base(), map[string]any{"touched_subjects": []any{s1}})
			h.accepted(id, 1, p)
			before := h.bytes("item", p)
			body := h.ok(h.revise(id, "Revision 2.", tc.crit(), map[string]any{"touched_subjects": tc.subjects}))
			got := strs(body["invalidated"])
			if tc.invalidate {
				if !slices.Equal(got, []string{p}) || h.item(p)["definition"] != "defining" {
					t.Errorf("invalidated %v, definition %v", got, h.item(p)["definition"])
				}
			} else if len(got) != 0 || h.bytes("item", p) != before {
				t.Errorf("invalidated %v", got)
			}
		})
	}
}

// tree is a Target that needs a specification and is accepted, with the items
// beneath it a revision can reach.
type tree struct {
	spec                          string
	T, P1, H1, H2, H3, P2, P3, H5 string
	other                         string
}

// newTree builds:
//
//	T (bound)
//	├─ P1 (a Prey that holds Hunts)
//	│  ├─ H1 (resolves through T, depth 2)
//	│  ├─ H2 (resolves through T, and has landed)
//	│  └─ H3 (its own binding to another specification)
//	├─ P2 (a Prey with no Hunt, resolves through T)
//	└─ P3 (its own binding to the other specification)
//	   └─ H5 (resolves through P3)
func newTree(h *home) *tree {
	t := h.t
	x := &tree{}
	x.T = h.addID(target("T", true))
	x.spec = h.defined(criteria(), nil)
	h.accepted(x.spec, 1, x.T)
	x.P1 = h.addID(under("prey", "P1", x.T))
	x.H1 = h.addID(under("hunt", "H1", x.P1))
	x.H2 = h.addID(under("hunt", "H2", x.P1))
	x.H3 = h.addID(under("hunt", "H3", x.P1))
	x.P2 = h.addID(under("prey", "P2", x.T))
	x.P3 = h.addID(under("prey", "P3", x.T))
	x.H5 = h.addID(under("hunt", "H5", x.P3))
	x.other = h.defined([]any{criterion("other", "Another outcome holds.", "A test shows it.")}, nil)
	h.accepted(x.other, 1, x.H3)
	h.accepted(x.other, 1, x.P3)
	h.move(x.H2, "done")
	for _, id := range []string{x.T, x.P1, x.H1, x.H2, x.H3, x.P2, x.P3, x.H5} {
		if got := h.item(id)["definition"]; got != "accepted" || len(h.activeHolds(id, "definition")) != 0 {
			t.Fatalf("%s is %v with holds %v", id, got, h.activeHolds(id, "definition"))
		}
	}
	return x
}

// Every item that resolves its effective specification through an invalidated
// binding, at any depth, a dispatchable one that has not landed, returns to
// defining with a definition hold; a landed one, a container and one with a
// binding of its own are untouched; and accept releases the holds again.
func TestItemsThatResolveThroughAnInvalidatedBindingReturnToDefining(t *testing.T) {
	h := newHome(t)
	x := newTree(h)
	snapshot := map[string]map[string]any{}
	bytes := map[string]string{}
	for _, id := range []string{x.T, x.P1, x.H1, x.H2, x.H3, x.P2, x.P3, x.H5} {
		snapshot[id], bytes[id] = h.item(id), h.bytes("item", id)
	}

	changed := []any{criterion("resets", "The counter resets at once.", "A test reads the counter twice."), criteria()[1]}
	resp := h.revise(x.spec, "Revision 2.", changed, nil)
	body := h.ok(resp)
	if !slices.Equal(strs(body["invalidated"]), []string{x.T}) {
		t.Fatalf("invalidated %v", body["invalidated"])
	}
	// The bound item loses its binding; the dispatchable ones that resolve
	// through it keep every other member.
	if _, bound := h.item(x.T)["specification"]; bound {
		t.Error("the Target keeps its binding")
	}
	for _, id := range []string{x.T, x.H1, x.P2} {
		got := h.item(id)
		if got["definition"] != "defining" || len(h.activeHolds(id, "definition")) != 1 {
			t.Errorf("%s: definition %v, holds %v", id, got["definition"], h.activeHolds(id, "definition"))
		}
		sameJSON(t, id+" changes nothing else", without(got, append(rewritten, "definition")...), without(snapshot[id], append(rewritten, "definition", "specification")...))
	}
	for _, id := range []string{x.P1, x.H2, x.H3, x.P3, x.H5} {
		if h.bytes("item", id) != bytes[id] || len(h.activeHolds(id, "definition")) != 0 {
			t.Errorf("%s was touched", id)
		}
	}
	if count(kinds(resp), "pool.hold.placed") != 3 || count(kinds(resp), "pool.specification.invalidated") != 1 {
		t.Errorf("receipts %v", kinds(resp))
	}

	// Accepting the new revision for the Target releases its hold and those of
	// the items that resolve through it, and returns their definition.
	next := h.ok(h.spec("accept", acceptBody(x.spec, 2, x.T)))
	for _, id := range []string{x.T, x.H1, x.P2} {
		if got := h.item(id); got["definition"] != "accepted" || len(h.activeHolds(id, "definition")) != 0 {
			t.Errorf("%s: definition %v, holds %v", id, got["definition"], h.activeHolds(id, "definition"))
		}
	}
	bound := h.item(x.T)["specification"].(map[string]any)
	if bound["revision"] != int64(2) || bound["digest"] != revisionOf(t, next["specification"].(map[string]any), 2)["digest"] {
		t.Errorf("binding %v", bound)
	}
	for _, id := range []string{x.P1, x.H2, x.H3, x.P3, x.H5} {
		if h.bytes("item", id) != bytes[id] {
			t.Errorf("%s was touched by the accept", id)
		}
	}
}

// A define leaves a generation already open and the Imperative it was
// delivered with exactly as they were, though the item it runs for returns to
// defining.
func TestADefineLeavesAnOpenGenerationAndItsImperativeAlone(t *testing.T) {
	h := newHome(t)
	T := h.addID(target("T", true))
	id := h.defined(criteria(), nil)
	h.accepted(id, 1, T)
	P := h.addID(under("prey", "P", T))
	H := h.addID(under("hunt", "H", P))
	generations := h.withGeneration(H, "open")
	h.withImperative(H)
	imperative := h.bytes("imperative", H)

	changed := []any{criterion("resets", "The counter resets at once.", "A test reads the counter twice."), criteria()[1]}
	h.ok(h.revise(id, "Revision 2.", changed, nil))

	got := h.item(H)
	if got["definition"] != "defining" || got["state"] != "in-flight" || len(h.activeHolds(H, "definition")) != 1 {
		t.Errorf("the item %v", got)
	}
	sameJSON(t, "the open generation", got["generations"], generations)
	if h.bytes("imperative", H) != imperative {
		t.Error("the Imperative changed")
	}
}

// accept appends the acceptance, binds the revision by identity, revision and
// digest, returns the item and those that resolve through it to accepted with
// their definition holds released, records need_test and the product questions
// whole, and leaves a word hold in place.
func TestAcceptBindsTheRevisionAndReleasesTheHolds(t *testing.T) {
	h := newHome(t)
	T := h.addID(target("T", true))
	P := h.addID(under("prey", "P", T))
	H := h.addID(under("hunt", "H", P))
	word := h.plantWordHold(T)
	for _, id := range []string{T, P, H} {
		if h.item(id)["definition"] != "needed" || len(h.activeHolds(id, "definition")) != 1 {
			t.Fatalf("%s is added needed with a hold", id)
		}
	}
	id := h.defined(criteria(), nil)
	q1, q2 := mint(t, ids.Question), mint(t, ids.Question)
	req := acceptBody(id, 1, T)
	req["open_product_questions"] = []any{q1, q2}
	req["need_test"] = map[string]any{"outcome": "The counter resets.", "coverage": "new-definition", "reason": "Nothing accepted covered it."}
	resp := h.spec("accept", req)
	body := h.ok(resp)

	rec := body["specification"].(map[string]any)
	digest := revisionOf(t, rec, 1)["digest"]
	item := body["item"].(map[string]any)
	sameJSON(t, "the binding", item["specification"], map[string]any{"specification_id": id, "revision": int64(1), "digest": digest})
	if item["definition"] != "accepted" || item["task_id"] != T {
		t.Errorf("item %v", item)
	}
	sameJSON(t, "the item is stored", item, h.item(T))
	for _, id := range []string{T, P, H} {
		if h.item(id)["definition"] != "accepted" || len(h.activeHolds(id, "definition")) != 0 {
			t.Errorf("%s: %v, holds %v", id, h.item(id)["definition"], h.activeHolds(id, "definition"))
		}
	}
	if len(h.activeHolds(T, "word")) != 1 || h.stored("hold", word)["state"] != "active" {
		t.Error("the word hold was released")
	}
	a := rec["acceptances"].([]any)[0].(map[string]any)
	if len(rec["acceptances"].([]any)) != 1 || a["task_id"] != T || a["revision"] != int64(1) || a["digest"] != digest || a["state"] != "accepted" || a["accepted_by"].(map[string]any)["kind"] != "test" {
		t.Errorf("acceptance %v", a)
	}
	sameJSON(t, "need_test", a["need_test"], req["need_test"])
	sameJSON(t, "open_product_questions", a["open_product_questions"], []any{q1, q2})
	acceptedReceipt := ""
	for _, r := range resp.Receipts {
		if r.Kind == "pool.specification.accepted" {
			acceptedReceipt = r.ReceiptID
			if r.Subject == nil || r.Subject.ID != id || r.Subject.RecordRevision != rec["record_revision"] {
				t.Errorf("the subject of the receipt %+v", r.Subject)
			}
		}
	}
	if a["receipt_id"] != acceptedReceipt || acceptedReceipt == "" || count(kinds(resp), "pool.hold.released") != 3 {
		t.Errorf("receipts %v, receipt_id %v", kinds(resp), a["receipt_id"])
	}

	t.Run("an item accepted at a revision once", func(t *testing.T) {
		h.quiet(func() {
			refused(t, h.spec("accept", acceptBody(id, 1, T)), envelope.Conflict, envelope.CodeAlreadyExists)
		})
	})
	t.Run("a revision that is not the current one", func(t *testing.T) {
		other := h.addID(leaf("Other", false))
		h.ok(h.revise(id, "Revision 2.", criteria(), nil))
		h.quiet(func() {
			refused(t, h.spec("accept", acceptBody(id, 1, other)), envelope.Conflict, envelope.CodeRevisionConflict)
		})
	})
	t.Run("a request that omits the test or the questions", func(t *testing.T) {
		other := h.addID(leaf("Other", false))
		for _, drop := range []string{"need_test", "open_product_questions"} {
			b := acceptBody(id, 2, other)
			delete(b, drop)
			h.quiet(func() { refused(t, h.spec("accept", b), envelope.Invalid, envelope.CodeSchemaViolation) })
		}
	})
}

// An existing coverage is checked against the accepted covering specification
// it names.
func TestAnExistingCoverageNamesAnAcceptedSpecification(t *testing.T) {
	h := newHome(t)
	u, v := h.addID(leaf("U", false)), h.addID(leaf("V", false))
	id := h.defined(criteria(), nil)
	covering := h.defined([]any{criterion("other", "Another outcome holds.", "A test shows it.")}, nil)
	cover := func(digest any) map[string]any {
		b := acceptBody(id, 1, u)
		b["need_test"] = map[string]any{
			"outcome": "The counter resets.", "coverage": "existing", "reason": "The other specification covers it.",
			"covering_specification": map[string]any{"specification_id": covering, "revision": int64(1), "digest": digest},
		}
		return b
	}
	digest := revisionOf(t, h.stored("specification", covering), 1)["digest"]

	h.quiet(func() {
		refused(t, h.spec("accept", cover(digest)), envelope.Refused, "pool.specification_unaccepted")
	})
	h.accepted(covering, 1, v)
	h.quiet(func() {
		refused(t, h.spec("accept", cover("sha256:"+strings.Repeat("e", 64))), envelope.Refused, "pool.specification_unaccepted")
	})
	body := h.ok(h.spec("accept", cover(digest)))
	a := body["specification"].(map[string]any)["acceptances"].([]any)[0].(map[string]any)
	sameJSON(t, "need_test", a["need_test"], cover(digest)["need_test"])
}

// close records, in one transaction, that the accepted requirements were met,
// once; one that does not commit stays pending and replays under the same
// request.
func TestCloseIsRecordedOnceAndAnInterruptedCloseReplays(t *testing.T) {
	h := newHome(t)
	T := h.addID(target("T", true))
	id := h.defined(criteria(), nil)
	h.accepted(id, 1, T)
	P := h.addID(under("prey", "P", T))
	H := h.addID(under("hunt", "H", P))
	h.withGeneration(H, "closed")
	landing := func() map[string]any { return map[string]any{"schema": "hive.receipt", "id": mint(t, ids.Receipt)} }
	closeBody := func(task string, generation int64) map[string]any {
		return map[string]any{
			"specification_id": id, "revision": int64(1), "task_id": task, "generation": generation,
			"landed_revision": strings.Repeat("a", 40), "landing": landing(),
			"proof": []any{landing(), landing()}, "resolved_findings": []any{landing()},
		}
	}

	t.Run("an item with no accepted revision, and a generation it never had", func(t *testing.T) {
		stray := h.addID(leaf("Stray", false))
		h.quiet(func() {
			refused(t, h.spec("close", closeBody(stray, 1)), envelope.Refused, "pool.specification_unaccepted")
			refused(t, h.spec("close", closeBody(H, 2)), envelope.NotFound, envelope.CodeNotFound)
		})
	})

	// An interrupted commit leaves the closure pending.
	req := mint(t, ids.Request)
	body := closeBody(H, 1)
	h.exe.Fault = func(step string) error {
		if strings.HasPrefix(step, "rename pool/records/") {
			return os.ErrClosed
		}
		return nil
	}
	resp := h.send(req, "hive-spec", "close", body)
	h.exe.Fault = nil
	if resp.Status != envelope.Unknown {
		t.Fatalf("a close that does not commit is %s: %+v", resp.Status, resp.Error)
	}
	if rec := h.stored("specification", id); rec["closures"] != nil && len(rec["closures"].([]any)) != 0 {
		t.Fatal("the closure was written before the crash")
	}

	again := h.send(req, "hive-spec", "close", body)
	out := h.ok(again)
	closure := out["closure"].(map[string]any)
	if closure["revision"] != int64(1) || closure["task_id"] != H || closure["generation"] != int64(1) || closure["landed_revision"] != strings.Repeat("a", 40) {
		t.Errorf("closure %v", closure)
	}
	if len(closure["proof"].([]any)) != 2 || len(closure["resolved_findings"].([]any)) != 1 || closure["closed_by"].(map[string]any)["kind"] != "test" {
		t.Errorf("closure %v", closure)
	}
	stored := h.stored("specification", id)
	if len(stored["closures"].([]any)) != 1 || len(kinds(again)) != 1 || kinds(again)[0] != "pool.specification.closed" || closure["receipt_id"] != again.Receipts[0].ReceiptID {
		t.Errorf("closures %v, receipts %v", stored["closures"], kinds(again))
	}
	sameJSON(t, "the response carries the record", out["specification"], stored)
	closed := 0
	for _, text := range h.files("pool/receipts") {
		if strings.Contains(text, `"pool.specification.closed"`) {
			closed++
		}
	}
	if closed != 1 {
		t.Errorf("%d closed receipts", closed)
	}
	if again.Receipts[0].Scope.Generation != 1 || again.Receipts[0].Scope.TaskID != H {
		t.Errorf("the closed receipt names task %q generation %d", again.Receipts[0].Scope.TaskID, again.Receipts[0].Scope.Generation)
	}
	events := 0
	for name, text := range h.files("journal") {
		if !strings.Contains(text, `"pool.specification.closed"`) {
			continue
		}
		events++
		if !strings.Contains(text, `"generation":1`) || !strings.Contains(text, `"task_id":"`+H+`"`) {
			t.Errorf("the closed event %s names no generation: %s", name, text)
		}
	}
	if events != 1 {
		t.Errorf("%d closed events", events)
	}

	// The same request again is the recorded answer, and another request for
	// the same closure is refused.
	replay := h.send(req, "hive-spec", "close", body)
	if replay.Status != envelope.Ok || !replay.Replayed {
		t.Errorf("replay %s %+v", replay.Status, replay.Error)
	}
	h.quiet(func() {
		refused(t, h.spec("close", closeBody(H, 1)), envelope.Conflict, envelope.CodeAlreadyExists)
	})
}

// An item added beneath a parent that resolves an effective specification
// takes accepted when it needs one, and no hold; one with nothing to resolve
// takes needed and the hold.
func TestAnItemAddedBeneathAParentWithAnEffectiveSpecificationIsAccepted(t *testing.T) {
	h := newHome(t)
	T := h.addID(target("T", true))
	before := h.addID(under("prey", "Before", T))
	if h.item(before)["definition"] != "needed" || len(h.activeHolds(before, "definition")) != 1 {
		t.Fatal("beneath an unaccepted Target an item is needed with its hold")
	}
	id := h.defined(criteria(), nil)
	h.accepted(id, 1, T)
	P := h.addID(under("prey", "P", T))
	H := h.addID(under("hunt", "H", P))
	for _, x := range []string{P, H} {
		if got := h.item(x); got["definition"] != "accepted" || len(h.activeHolds(x, "definition")) != 0 {
			t.Errorf("%s is %v with holds %v", x, got["definition"], h.activeHolds(x, "definition"))
		}
	}
	lone := h.addID(leaf("Lone", true))
	if h.item(lone)["definition"] != "needed" || len(h.activeHolds(lone, "definition")) != 1 {
		t.Error("a Prey with no parent has nothing to resolve")
	}
}

// A touched_subjects list that names a subject twice, and a new specification
// with no title, are refused as invalid and write nothing.
func TestDefineRefusesADuplicateSubjectAndAnUntitledSpecification(t *testing.T) {
	h := newHome(t)
	s := subject(t)
	h.quiet(func() {
		refused(t, h.spec("define", define("Titled", "Prose.", criteria(), map[string]any{"touched_subjects": []any{s, s}})), envelope.Invalid, envelope.CodeSchemaViolation)
		refused(t, h.spec("define", define("", "Prose.", criteria(), nil)), envelope.Invalid, envelope.CodeSchemaViolation)
		omitted := define("Titled", "Prose.", criteria(), nil)
		delete(omitted, "touched_subjects")
		refused(t, h.spec("define", omitted), envelope.Invalid, envelope.CodeSchemaViolation)
	})
	id := h.defined(criteria(), nil)
	h.quiet(func() {
		b := define("", "Prose.", criteria(), map[string]any{"touched_subjects": []any{s, s}})
		b["specification_id"] = id
		refused(t, h.spec("define", b), envelope.Invalid, envelope.CodeSchemaViolation)
	})
	if got := h.stored("specification", id)["current_revision"]; got != int64(1) {
		t.Errorf("current_revision %v", got)
	}
}

// An existing coverage holds only while an item stands bound to the covering
// revision: an acceptance entry left in the history by a binding that was
// since lost does not cover.
func TestAnExistingCoverageNeedsAStandingBinding(t *testing.T) {
	h := newHome(t)
	a, u := h.addID(leaf("A", false)), h.addID(leaf("U", false))
	id := h.defined(criteria(), nil)
	covering := h.defined(criteria(), nil)
	h.accepted(covering, 1, a)

	prose := h.ok(h.revise(covering, "Only the prose changes.", criteria(), map[string]any{"touched_subjects": []any{}}))
	if len(prose["invalidated"].([]any)) != 0 {
		t.Fatalf("a prose-only revision invalidated %v", prose["invalidated"])
	}
	h.accepted(covering, 2, a)
	changed := h.ok(h.revise(covering, "Criteria change.", []any{criterion("other", "Another outcome holds.", "A test shows it.")}, map[string]any{"touched_subjects": []any{}}))
	if len(changed["invalidated"].([]any)) != 1 {
		t.Fatalf("invalidated %v", changed["invalidated"])
	}

	digest := revisionOf(t, h.stored("specification", covering), 1)["digest"]
	b := acceptBody(id, 1, u)
	b["need_test"] = map[string]any{
		"outcome": "The counter resets.", "coverage": "existing", "reason": "The covering specification covers it.",
		"covering_specification": map[string]any{"specification_id": covering, "revision": int64(1), "digest": digest},
	}
	h.quiet(func() {
		refused(t, h.spec("accept", b), envelope.Refused, "pool.specification_unaccepted")
	})
}

// define is declared at the contract's version, 1.2, and also accepts 1.1 and
// 1.0: 1.1 adds product bindings and 1.2 touched subjects, each an optional
// member of the one body, so a request at any of the three is accepted.
func TestDefineIsAcceptedAtEveryVersionTheContractNames(t *testing.T) {
	for _, version := range []string{"1.2", "1.1", "1.0"} {
		t.Run(version, func(t *testing.T) {
			h := newHome(t)
			extra := map[string]any{}
			if version == "1.2" {
				extra["touched_subjects"] = []any{subject(t)}
			}
			h.ok(h.sendAt(version, "", "hive-spec", "define", define("Counter reset", "The first prose.", criteria(), extra)))
		})
	}
}
