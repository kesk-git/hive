package item_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// Re-expressed from test/commands/state.test.ts at tasks-axi revision
// 9401ff899c0d1d8ae6b4fd8727b9025abda2032c; see imports/tasks-axi.json.
// Each subtest names its upstream case. Request replay is Hive's idempotent
// retry: spec/commands.md, Replay, says that the same request_id and digest
// returns the recorded response with replayed true and writes nothing. A new
// start request on an in-flight item is refused: spec/pool.md's state table
// has no transition from a state to itself.
func retainedStateRequest(h *home, operation string, body map[string]any) func() *envelope.Response {
	h.t.Helper()
	request, err := canon.Encode(map[string]any{
		"schema": "hive.request", "schema_version": "1.0", "command": "hive-pool", "operation": operation, "operation_version": "1.0",
		"request_id": mint(h.t, ids.Request), "correlation_id": mint(h.t, ids.Correlation), "issued_at": "2026-10-14T09:00:00.000Z",
		"caller": map[string]any{"kind": "test"}, "scope_level": "hive", "home_id": h.homeID,
		"authority": map[string]any{"kind": "none"}, "body": body,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return func() *envelope.Response {
		h.t.Helper()
		var command *cli.Command
		for _, c := range h.exe.Commands {
			if c.Name == "hive-pool" {
				command = c
			}
		}
		out, err := h.exe.Handle(cli.Invocation{Command: command, Operation: operation, Home: h.dir, Request: append(request, '\n')})
		if err != nil {
			h.t.Fatal(err)
		}
		response, err := envelope.DecodeResponse(out.Encoded)
		if err != nil {
			h.t.Fatal(err)
		}
		return response
	}
}

func TestRetainedStateCommands(t *testing.T) {
	for _, name := range []string{
		"rejects malformed primary ids before store lookup",
		"moves a task to in_flight",
		"start/emits a machine-readable task with --json",
		"done/emits a machine-readable task and pruned count with --json",
		"block / unblock/emits a machine-readable task with --json",
		"is idempotent when already in_flight",
		"closes, records the pr, and reports pruned count",
		"closes with a Forgejo pulls URL and preserves it byte-for-byte",
		"is idempotent when already done",
		"backfills metadata on an already done task without pruning",
		"preserves body edits made before an already-done note update",
		"does not duplicate an already-done note on retry",
		"does not rewrite when an already-done note already exists",
		"moves a done task back to queued and clears the closed stamp",
		"adds and clears a blocked-by edge idempotently",
		"rejects invalid blocker ids",
		"requires --by",
		"rejects a missing blocker target before changing dependencies",
		"lists unblocked queued work and excludes blocked tasks",
		"excludes held tasks by default",
		"treats future hold-until as held and past hold-until as ready",
		"shows active held tasks in a separate group with --include-held",
		"filters list to active held tasks with --state held",
		"gives a definitive empty state",
		"records and clears a structured hold idempotently",
		"fails closed on unknown ids",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHome(t)
			id := h.addID(leaf("Retained state item"))
			patch := func(p map[string]any) map[string]any {
				return h.ok(h.do("update", map[string]any{"task_id": id, "patch": p}))
			}
			edge := func(other string) []any { return []any{map[string]any{"type": "blocked-by", "task_id": other}} }
			switch name {
			case "rejects malformed primary ids before store lookup":
				h.unchanged(func() {
					for _, op := range []string{"transition", "update", "hold"} {
						body := map[string]any{"task_id": "bad:id"}
						switch op {
						case "transition":
							body["to"] = "in-flight"
						case "update":
							body["patch"] = map[string]any{"title": "Changed"}
						case "hold":
							body["kind"], body["reason"] = "parked", "Wait"
						}
						refused(t, h.do(op, body), envelope.Invalid, envelope.CodeSchemaViolation)
					}
					refused(t, h.do("release", map[string]any{"hold_id": "bad:id"}), envelope.Invalid, envelope.CodeSchemaViolation)
				})
			case "start/emits a machine-readable task with --json", "done/emits a machine-readable task and pruned count with --json":
				to := "in-flight"
				if name == "done/emits a machine-readable task and pruned count with --json" {
					to = "done"
				}
				response := h.do("transition", map[string]any{"task_id": id, "to": to})
				record := h.ok(response)["item"].(map[string]any)
				if record["task_id"] != id || record["state"] != to || len(response.Receipts) != 1 {
					t.Fatalf("machine-readable transition = %+v", response)
				}
			case "block / unblock/emits a machine-readable task with --json":
				blocker := h.addID(leaf("Machine-readable blocker"))
				response := h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"add_deps": edge(blocker)}})
				record := h.ok(response)["item"].(map[string]any)
				_, facts := h.item(id)
				if record["task_id"] != id || !facts["blocked"].(bool) || !reflect.DeepEqual(facts["active_blockers"], []any{blocker}) {
					t.Fatalf("machine-readable blocked task = %v / %v", record, facts)
				}

			case "moves a task to in_flight":
				if h.move(id, "in-flight")["state"] != "in-flight" {
					t.Fatal("start did not enter in-flight")
				}
			case "is idempotent when already in_flight", "is idempotent when already done":
				to := "in-flight"
				if name == "is idempotent when already done" {
					to = "done"
				}
				send := retainedStateRequest(h, "transition", map[string]any{"task_id": id, "to": to})
				first := send()
				h.ok(first)
				before := h.files("")
				second := send()
				h.ok(second)
				if !second.Replayed || !reflect.DeepEqual(first.Body, second.Body) || !reflect.DeepEqual(first.Receipts, second.Receipts) {
					t.Fatalf("retry did not replay the first outcome: %+v / %+v", first, second)
				}
				if !same(before, h.files("")) {
					t.Fatal("replay wrote files")
				}
				h.unchanged(func() {
					refused(t, h.do("transition", map[string]any{"task_id": id, "to": to}), envelope.Refused, "pool.state_transition")
				})
			case "closes, records the pr, and reports pruned count", "closes with a Forgejo pulls URL and preserves it byte-for-byte":
				url := "https://github.com/acme/app/pull/9"
				if name == "closes with a Forgejo pulls URL and preserves it byte-for-byte" {
					url = "https://forge.example.org/team/app/pulls/39"
				}
				links := []any{map[string]any{"kind": "harvest", "url": url}}
				record := h.move(id, "done", map[string]any{"evidence": links})
				if record["state"] != "done" || record["completed"] != true || record["closed_at"] != "2026-10-14T09:00:00.000Z" || !reflect.DeepEqual(record["links"], links) {
					t.Fatalf("completion = %v", record)
				}
			case "backfills metadata on an already done task without pruning":
				record := h.move(id, "done")
				closed := record["closed_at"]
				h.clk.Advance(24 * time.Hour)
				link := map[string]any{"kind": "harvest", "url": "https://github.com/acme/app/pull/77"}
				patch(map[string]any{"add_links": []any{link}, "add_body_lines": []any{"Backfilled evidence"}})
				after, _ := h.item(id)
				if after["closed_at"] != closed || after["body"] != "Backfilled evidence" || !reflect.DeepEqual(after["links"], []any{link}) {
					t.Fatalf("backfill = %v", after)
				}
			case "preserves body edits made before an already-done note update":
				h.move(id, "done")
				patch(map[string]any{"body": "Concurrent audit note"})
				patch(map[string]any{"add_body_lines": []any{"Backfilled evidence"}})
				record, _ := h.item(id)
				if record["body"] != "Concurrent audit note\nBackfilled evidence" {
					t.Fatalf("body = %v", record["body"])
				}
			case "does not duplicate an already-done note on retry", "does not rewrite when an already-done note already exists":
				h.move(id, "done")
				patch(map[string]any{"add_body_lines": []any{"Retry-safe note"}})
				h.unchanged(func() {
					result := patch(map[string]any{"add_body_lines": []any{"Retry-safe note"}})
					if len(result["changed"].([]any)) != 0 || result["item"].(map[string]any)["body"] != "Retry-safe note" {
						t.Fatalf("note retry = %v", result)
					}
				})
			case "moves a done task back to queued and clears the closed stamp":
				h.move(id, "done")
				record := h.move(id, "queued")
				if record["state"] != "queued" || record["closed_at"] != nil || record["completed"] != nil {
					t.Fatalf("reopen = %v", record)
				}
			case "adds and clears a blocked-by edge idempotently":
				other := h.addID(leaf("Blocker"))
				patch(map[string]any{"add_deps": edge(other)})
				h.unchanged(func() {
					if len(patch(map[string]any{"add_deps": edge(other)})["changed"].([]any)) != 0 {
						t.Fatal("duplicate edge changed fields")
					}
				})
				patch(map[string]any{"remove_deps": edge(other)})
				record, _ := h.item(id)
				if len(record["deps"].([]any)) != 0 {
					t.Fatal("edge survived removal")
				}
			case "requires --by":
				h.unchanged(func() {
					refused(t, h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"add_deps": []any{map[string]any{"type": "blocked-by"}}}}), envelope.Invalid, envelope.CodeSchemaViolation)
				})

			case "rejects invalid blocker ids":
				h.unchanged(func() {
					for _, member := range []string{"add_deps", "remove_deps"} {
						refused(t, h.do("update", map[string]any{"task_id": id, "patch": map[string]any{member: edge("bad:id")}}), envelope.Invalid, envelope.CodeSchemaViolation)
					}
				})
			case "rejects a missing blocker target before changing dependencies":
				h.unchanged(func() {
					refused(t, h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"add_deps": edge(mint(t, ids.Task))}}), envelope.Refused, "pool.dependency_missing")
				})
			case "lists unblocked queued work and excludes blocked tasks":
				other := h.addID(leaf("Blocker"))
				patch(map[string]any{"add_deps": edge(other)})
				body := h.ok(h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"ready": true}}))
				for _, record := range body["records"].([]any) {
					if record.(map[string]any)["task_id"] == id {
						t.Fatal("blocked item is ready")
					}
				}
				if len(body["records"].([]any)) != 1 {
					t.Fatal("independent blocker should be ready")
				}
			case "excludes held tasks by default", "shows active held tasks in a separate group with --include-held", "filters list to active held tasks with --state held":
				holdID := h.hold(id, "resource")
				if h.fact(id, "ready") || !h.fact(id, "held") {
					t.Fatal("held item is ready")
				}
				holds := h.ok(h.do("read", map[string]any{"kind": "hold", "selector": map[string]any{"task_id": id, "active": true}}))["records"].([]any)
				if len(holds) != 1 || holds[0].(map[string]any)["hold_id"] != holdID || holds[0].(map[string]any)["reason"] != "A test reason." {
					t.Fatalf("held view = %v", holds)
				}
			case "treats future hold-until as held and past hold-until as ready":
				for _, tc := range []struct {
					at   string
					held bool
				}{{"2999-01-01T00:00:00.000Z", true}, {"2000-01-01T00:00:00.000Z", false}} {
					next := h.addID(leaf("Dated work"))
					h.ok(h.do("hold", map[string]any{"task_id": next, "kind": "future", "reason": "Wait", "until": map[string]any{"at": tc.at, "zone": "UTC"}}))
					if h.fact(next, "held") != tc.held || h.fact(next, "ready") == tc.held {
						t.Fatalf("until %s has incorrect facts", tc.at)
					}
				}
			case "gives a definitive empty state":
				h.move(id, "done")
				body := h.ok(h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"ready": true}}))
				if len(body["records"].([]any)) != 0 || body["projection"].(map[string]any)["complete"] != true {
					t.Fatalf("empty view = %v", body)
				}
			case "records and clears a structured hold idempotently":
				send := retainedStateRequest(h, "hold", map[string]any{"task_id": id, "kind": "resource", "reason": "Wait for capacity", "until": map[string]any{"at": "2999-01-01T00:00:00.000Z", "zone": "UTC"}})
				first := send()
				body := h.ok(first)
				before := h.files("")
				second := send()
				h.ok(second)
				if !second.Replayed || !reflect.DeepEqual(first.Body, second.Body) || !same(before, h.files("")) {
					t.Fatal("hold retry was not a write-free replay")
				}
				holdID := body["hold"].(map[string]any)["hold_id"].(string)
				h.ok(h.do("release", map[string]any{"hold_id": holdID}))
				h.unchanged(func() { h.ok(h.do("release", map[string]any{"hold_id": holdID})) })
				if h.fact(id, "held") {
					t.Fatal("released hold still holds")
				}
			case "fails closed on unknown ids":
				h.unchanged(func() {
					refused(t, h.do("hold", map[string]any{"task_id": mint(t, ids.Task), "kind": "parked", "reason": "Wait"}), envelope.NotFound, envelope.CodeNotFound)
					refused(t, h.do("release", map[string]any{"hold_id": mint(t, ids.Hold)}), envelope.NotFound, envelope.CodeNotFound)
				})
			}
		})
	}
}

// The empty --pr value is represented by an empty harvest URL, rather than
// by flags: the request schema refuses it before any transition is applied.
func TestRetainedStateEmptyEvidence(t *testing.T) {
	t.Run("rejects an empty link flag before closing the task", func(t *testing.T) {
		h := newHome(t)
		id := h.addID(leaf("Empty evidence"))
		h.unchanged(func() {
			refused(t, h.do("transition", map[string]any{"task_id": id, "to": "done", "evidence": []any{map[string]any{"kind": "harvest", "url": ""}}}), envelope.Invalid, envelope.CodeSchemaViolation)
		})
	})
}

// Retained upstream case inventory (test declarations, with table rows in
// their subtests); upstream: test/commands/state.test.ts.
// - "rejects malformed primary ids before store lookup".
// - "moves a task to in_flight".
// - "is idempotent when already in_flight".
// - "emits a machine-readable task with --json".
// - "closes, records the pr, and reports pruned count".
// - "closes with a Forgejo pulls URL and preserves it byte-for-byte".
// - "emits a machine-readable task and pruned count with --json".
// - "rejects an empty link flag before closing the task".
// - "is idempotent when already done".
// - "backfills metadata on an already done task without pruning".
// - "preserves body edits made before an already-done note update".
// - "does not duplicate an already-done note on retry".
// - "does not rewrite when an already-done note already exists".
// - "moves a done task back to queued and clears the closed stamp".
// - "adds and clears a blocked-by edge idempotently".
// - "emits a machine-readable task with --json".
// - "requires --by".
// - "rejects invalid blocker ids".
// - "rejects a missing blocker target before changing dependencies".
// - "lists unblocked queued work and excludes blocked tasks".
// - "excludes held tasks by default".
// - "treats future hold-until as held and past hold-until as ready".
// - "shows active held tasks in a separate group with --include-held".
// - "filters list to active held tasks with --state held".
// - "gives a definitive empty state".
// - "records and clears a structured hold idempotently".
// - "fails closed on unknown ids".
//
// Not carried:
// - "rejects extra positional arguments before mutating".
// - "rejects a missing value before consuming the next flag".
// - "rejects unknown flags before closing the task".
// - "rejects extra positional arguments before mutating".
// - "rejects extra positional arguments before changing dependencies".
// - "carries repo scope into empty ready suggestions".
// - "rejects unknown flags".
// - "rejects a %s repo filter" (empty, whitespace, multiline).
// Reason: spec/import-plan-tasks-axi.md, Excluded: "Presentation and
// packaging: argument parsing, output encoding, suggestions, the home and
// setup commands, the skill asset and the version fast path. None is the
// Pool's behavior; Hive's command contract is the interface."
//
// Not carried:
// - "prunes and archives by default to the configured keep".
// - "retries prune when the task is already done".
// - "rejects negative keep before closing the task".
// Reason: spec/import-plan-tasks-axi.md, Local changes: "prune is not
// carried for Hive's records and survives only as archive lookup for
// import".
//
// Not carried:
// - "moves a task to another backlog file".
// - "emits a machine-readable result with --json".
// - "resolves a directory to its data/backlog.md".
// - "moves the task from a fresh locked source read".
// - "leaves the source intact when destination validation fails".
// - "preserves a missing created date when moving legacy tasks".
// - "moves opposite directions without deadlocking".
// - "rejects moving a task that active tasks still block on".
// - "requires --to".
// - "rejects a whitespace --to before moving".
// - "aborts the whole set when a member id does not exist".
// - "requires at least one id".
// - "moves a connected blocker/dependent pair atomically, reason byte-exact".
// - "emits the moved ids under --json for a multi-id move".
// - "refuses a set that leaves its blocker behind, naming the link".
// - "moves a full three-item chain together".
// Reason: spec/import-plan-tasks-axi.md, Excluded: "The multi-item move is
// left for a later stage" and "Nothing of it is translated at stage 2."
//
// Not claimed by this file pending the behavioral decision:
// - "rejects non-canonical pull URLs without mutating".
// - "rejects a malformed pr link before closing the task".
// - "rejects a whitespace note before closing the task".
// - "rejects a self-block before changing dependencies".
// - "rejects missing or unsafe hold fields before mutating".
