package item_test

import (
	"reflect"
	"slices"
	"testing"
)

// Re-expressed from test/derive.test.ts, describe "derive", at tasks-axi
// revision 9401ff899c0d1d8ae6b4fd8727b9025abda2032c; see imports/tasks-axi.json.
// Every upstream case is named below. IDs are Pool ULIDs; dates are zoned UTC
// instants. The include-held view is a read of all items with their held facts:
// ready itself stays false for held items under spec/pool.md, Derived facts.
func TestRetainedDerivedFacts(t *testing.T) {
	for _, name := range []string{
		"marks a task blocked when a blocker is not done",
		"does not mark a task blocked when its blocker is done",
		"treats a missing blocker as resolved (not blocking)",
		"ready = queued tasks with no unresolved blocker",
		"excludes active held tasks from ready unless requested",
		"date-gates holds by local YYYY-MM-DD",
		"activeBlockers lists only the unresolved blockers",
		"a blocked-by edge with a free-text reason still blocks (graph keys off the id)",
		"multiple reason-bearing blocked-by edges each participate in readiness",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHome(t)
			b := h.addID(leaf("Unfinished blocker"))
			h.move(b, "in-flight")
			d := h.addID(leaf("Completed blocker"))
			h.move(d, "done")
			dependent := func(title, blocker, reason string) string {
				body := leaf(title)
				body["deps"] = []any{map[string]any{"type": "blocked-by", "task_id": blocker, "reason": reason}}
				return h.addID(body)
			}
			a := dependent("Blocked", b, "Wait for the blocker")
			c := dependent("Unblocked", d, "Already completed")
			e := h.addID(leaf("Independent"))
			f := h.legacyItem("queued")
			missing := h.stored("item", f)
			missing["deps"] = []any{map[string]any{"type": "blocked-by", "task_id": mint(t, "task")}}
			h.plant("item", f, missing)
			switch name {
			case "marks a task blocked when a blocker is not done":
				if !h.fact(a, "blocked") {
					t.Fatal("unfinished blocker did not block")
				}
			case "does not mark a task blocked when its blocker is done":
				if h.fact(c, "blocked") {
					t.Fatal("completed blocker still blocks")
				}
			case "treats a missing blocker as resolved (not blocking)":
				if h.fact(f, "blocked") {
					t.Fatal("missing blocker blocks")
				}
			case "ready = queued tasks with no unresolved blocker":
				body := h.ok(h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"ready": true}}))
				var got []string
				for _, record := range body["records"].([]any) {
					got = append(got, record.(map[string]any)["task_id"].(string))
				}
				want := []string{c, e, f}
				slices.Sort(want)
				slices.Sort(got)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("ready = %v, want %v", got, want)
				}
			case "excludes active held tasks from ready unless requested":
				h.hold(e, "parked")
				if h.fact(e, "ready") || !h.fact(e, "held") {
					t.Fatal("held work has incorrect readiness")
				}
				body := h.ok(h.do("read", map[string]any{"kind": "item", "selector": map[string]any{}}))
				var found bool
				for _, record := range body["records"].([]any) {
					found = found || record.(map[string]any)["task_id"] == e
				}
				if !found {
					t.Fatal("all-items view omitted held work")
				}
			case "date-gates holds by local YYYY-MM-DD":
				place := func(id, at, date string) {
					h.ok(h.do("hold", map[string]any{"task_id": id, "kind": "future", "reason": "Wait until the day", "until": map[string]any{"at": at, "zone": "UTC", "date": date}}))
				}
				place(e, "2999-01-01T00:00:00.000Z", "2999-01-01")
				place(f, "2000-01-01T00:00:00.000Z", "2000-01-01")
				if !h.fact(e, "held") || h.fact(e, "ready") || h.fact(f, "held") || !h.fact(f, "ready") {
					t.Fatal("date gate has incorrect held/ready facts")
				}
			case "activeBlockers lists only the unresolved blockers":
				_, blocked := h.item(a)
				_, resolved := h.item(c)
				if !reflect.DeepEqual(blocked["active_blockers"], []any{b}) || len(resolved["active_blockers"].([]any)) != 0 {
					t.Fatalf("active blockers = %v / %v", blocked, resolved)
				}
			case "a blocked-by edge with a free-text reason still blocks (graph keys off the id)":
				if !h.fact(a, "blocked") || h.fact(a, "ready") {
					t.Fatal("reason-bearing edge did not block")
				}
				record, _ := h.item(a)
				if record["deps"].([]any)[0].(map[string]any)["reason"] != "Wait for the blocker" {
					t.Fatal("reason changed")
				}
			case "multiple reason-bearing blocked-by edges each participate in readiness":
				h.ok(h.do("update", map[string]any{"task_id": c, "patch": map[string]any{"add_deps": []any{map[string]any{"type": "blocked-by", "task_id": b, "reason": "Wait for another blocker"}}}}))
				_, facts := h.item(c)
				if !facts["blocked"].(bool) || facts["ready"].(bool) || !reflect.DeepEqual(facts["active_blockers"], []any{b}) {
					t.Fatalf("derived = %v", facts)
				}
			}
		})
	}
}

// Retained upstream case inventory (test declarations, with table rows in
// their subtests); upstream: test/derive.test.ts.
// - "marks a task blocked when a blocker is not done".
// - "does not mark a task blocked when its blocker is done".
// - "treats a missing blocker as resolved (not blocking)".
// - "ready = queued tasks with no unresolved blocker".
// - "excludes active held tasks from ready unless requested".
// - "date-gates holds by local YYYY-MM-DD".
// - "activeBlockers lists only the unresolved blockers".
// - "a blocked-by edge with a free-text reason still blocks (graph keys off the id)".
// - "multiple reason-bearing blocked-by edges each participate in readiness".
