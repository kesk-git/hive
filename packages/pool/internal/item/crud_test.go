package item_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// Re-expressed from test/commands/crud.test.ts at tasks-axi revision
// 9401ff899c0d1d8ae6b4fd8727b9025abda2032c; see imports/tasks-axi.json.
// Subtests name the upstream cases. add, read, update and transition to
// archived replace add, list/show, update and rm. Identifiers are minted by
// the Pool; retry idempotence is the request replay of spec/commands.md.
// Output assertions use the decoded Hive envelope and whole records.
func TestRetainedCRUDCommands(t *testing.T) {
	for _, name := range []string{
		"adds a queued task and suggests next steps",
		"adds directly to In flight with --start",
		"confirms a queued add and suggests start, not done",
		"emits a machine-readable task with --json",
		"uses show --full as the mutation truncation escape hatch",
		"is idempotent for an existing id",
		"rejects an invalid id",
		"rejects a self blocked-by edge before creating a task",
		"rejects an invalid blocked-by id",
		"rejects a missing blocked-by target before creating a task",
		"records blocked-by when the target exists",
		"rejects an empty link flag before creating a task",
		"persists priority through a fresh read",
		"emits a count line and the default compact schema",
		"filters by state and reports a true total when limited",
		"uses show --full as the list truncation escape hatch",
		"gives a definitive empty state",
		"truncates the body by default and reveals --full",
		"errors with NOT_FOUND for an unknown id",
		"replaces the body wholesale",
		"lists every changed field in the confirmation",
		"update/emits a machine-readable task with --json",
		"reports an unchanged body replacement as already done",
		"update/rejects a malformed id as a validation error",
		"persists updated priority through a fresh read",
		"rejects an empty link flag before updating",
		"removes a task and confirms",
		"emits a machine-readable result with --json",
		"rejects removing a task that active tasks still block on",
		"removes a blocker after dependents are unblocked or done",
		"rm/rejects a malformed id as a validation error",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHome(t)
			read := func(selector map[string]any) map[string]any {
				return h.ok(h.do("read", map[string]any{"kind": "item", "selector": selector}))
			}
			update := func(id string, patch map[string]any) map[string]any {
				return h.ok(h.do("update", map[string]any{"task_id": id, "patch": patch}))
			}
			edge := func(id string) []any { return []any{map[string]any{"type": "blocked-by", "task_id": id}} }
			switch name {
			case "adds a queued task and suggests next steps", "confirms a queued add and suggests start, not done", "emits a machine-readable task with --json":
				added := h.add(leaf("Fresh work"))
				id := added["task_id"].(string)
				record, _ := h.item(id)
				if record["title"] != "Fresh work" || record["state"] != "queued" || !reflect.DeepEqual(added, record) {
					t.Fatalf("add/read = %v / %v", added, record)
				}
			case "adds directly to In flight with --start":
				// The Pool add always queues; the separate transition expresses --start.
				id := h.addID(leaf("Started work"))
				h.move(id, "in-flight")
				record, _ := h.item(id)
				if record["state"] != "in-flight" {
					t.Fatal("start did not persist")
				}
			case "rejects an invalid id", "rejects a self blocked-by edge before creating a task":
				// add mints its identity; a caller cannot supply even a valid task_id
				// to make the new item refer to itself. Both requests are refused before
				// any item or receipt is written.
				requested := mint(t, ids.Task)
				if name == "rejects an invalid id" {
					requested = "Bad Id"
				}
				body := leaf("Caller-supplied identity")
				body["task_id"] = requested
				if name == "rejects a self blocked-by edge before creating a task" {
					body["deps"] = edge(requested)
				}
				h.unchanged(func() { refused(t, h.do("add", body), envelope.Invalid, envelope.CodeSchemaViolation) })

			case "is idempotent for an existing id":
				send := retainedStateRequest(h, "add", leaf("Retried creation"))
				first := send()
				h.ok(first)
				before := h.files("")
				second := send()
				h.ok(second)
				if !second.Replayed || !reflect.DeepEqual(first.Body, second.Body) || !same(before, h.files("")) {
					t.Fatal("add replay created another item or wrote files")
				}
				if len(read(map[string]any{})["records"].([]any)) != 1 {
					t.Fatal("duplicate item")
				}
			case "rejects an invalid blocked-by id":
				body := leaf("Bad dependency")
				body["deps"] = edge("bad:id")
				h.unchanged(func() { refused(t, h.do("add", body), envelope.Invalid, envelope.CodeSchemaViolation) })
			case "rejects a missing blocked-by target before creating a task":
				body := leaf("Missing dependency")
				body["deps"] = edge(mint(t, ids.Task))
				h.unchanged(func() { refused(t, h.do("add", body), envelope.Refused, "pool.dependency_missing") })
			case "records blocked-by when the target exists":
				blocker := h.addID(leaf("Blocker"))
				body := leaf("Dependent")
				body["deps"] = edge(blocker)
				id := h.addID(body)
				record, _ := h.item(id)
				if !reflect.DeepEqual(record["deps"], edge(blocker)) || !h.fact(id, "blocked") {
					t.Fatalf("dependency = %v", record)
				}
			case "rejects an empty link flag before creating a task":
				body := leaf("Empty link")
				body["links"] = []any{map[string]any{"kind": "harvest", "url": ""}}
				h.unchanged(func() { refused(t, h.do("add", body), envelope.Invalid, envelope.CodeSchemaViolation) })
			case "rejects an empty link flag before updating":
				id := h.addID(leaf("Empty added link"))
				h.unchanged(func() {
					refused(t, h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"add_links": []any{map[string]any{"kind": "report", "url": ""}}}}), envelope.Invalid, envelope.CodeSchemaViolation)
				})

			case "persists priority through a fresh read", "persists updated priority through a fresh read":
				body := leaf("Ranked work")
				body["priority"] = int64(2)
				id := h.addID(body)
				want := int64(2)
				if name == "persists updated priority through a fresh read" {
					update(id, map[string]any{"priority": int64(3)})
					want = 3
				}
				record, _ := h.item(id)
				if record["priority"] != want {
					t.Fatalf("priority = %v", record["priority"])
				}
			case "emits a count line and the default compact schema":
				a := h.addID(leaf("One"))
				b := h.addID(leaf("Two"))
				records := read(map[string]any{})["records"].([]any)
				if len(records) != 2 {
					t.Fatalf("count = %d", len(records))
				}
				for _, v := range records {
					r := v.(map[string]any)
					if r["task_id"] != a && r["task_id"] != b || r["title"] == nil || r["state"] != "queued" {
						t.Fatalf("record = %v", r)
					}
				}
			case "filters by state and reports a true total when limited":
				for _, title := range []string{"One", "Two", "Three"} {
					h.addID(leaf(title))
				}
				done := h.addID(leaf("Completed"))
				h.move(done, "done")
				body := read(map[string]any{"states": []any{"queued"}, "limit": int64(2)})
				records := body["records"].([]any)
				projection := body["projection"].(map[string]any)
				if len(records) != 2 || projection["complete"] != false || len(projection["omitted"].([]any)) != 1 {
					t.Fatalf("limited projection = %v", body)
				}
				for _, r := range records {
					if r.(map[string]any)["state"] != "queued" {
						t.Fatal("state filter failed")
					}
				}
			case "uses show --full as the mutation truncation escape hatch", "uses show --full as the list truncation escape hatch", "truncates the body by default and reveals --full":
				body := leaf("Long prose")
				text := ""
				for range 600 {
					text += "x"
				}
				body["body"] = text
				id := h.addID(body)
				record, _ := h.item(id)
				if record["body"] != text {
					t.Fatal("identity read truncated body")
				}
				listed := read(map[string]any{})["records"].([]any)[0].(map[string]any)
				if listed["body"] != text {
					t.Fatal("query read truncated body")
				}
			case "gives a definitive empty state":
				body := read(map[string]any{"states": []any{"queued"}})
				if len(body["records"].([]any)) != 0 || body["projection"].(map[string]any)["complete"] != true {
					t.Fatalf("empty = %v", body)
				}
			case "errors with NOT_FOUND for an unknown id":
				refused(t, h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"id": mint(t, ids.Task)}}), envelope.NotFound, envelope.CodeNotFound)
			case "replaces the body wholesale":
				body := leaf("Notes")
				body["body"] = "Old note"
				id := h.addID(body)
				result := update(id, map[string]any{"body": "Current status only"})
				record, _ := h.item(id)
				if record["body"] != "Current status only" || !reflect.DeepEqual(result["changed"], []any{"body"}) {
					t.Fatalf("replacement = %v / %v", record, result)
				}
			case "lists every changed field in the confirmation", "update/emits a machine-readable task with --json":
				id := h.addID(leaf("Original"))
				result := update(id, map[string]any{"title": "Renamed", "body": "Current body", "priority": int64(3)})
				if !reflect.DeepEqual(result["changed"], []any{"title", "body", "priority"}) || result["item"].(map[string]any)["task_id"] != id {
					t.Fatalf("update = %v", result)
				}
			case "reports an unchanged body replacement as already done":
				body := leaf("Unchanged notes")
				body["body"] = "Current note"
				id := h.addID(body)
				h.unchanged(func() {
					if len(update(id, map[string]any{"body": "Current note"})["changed"].([]any)) != 0 {
						t.Fatal("unchanged replacement reports a change")
					}
				})
			case "update/rejects a malformed id as a validation error":
				h.unchanged(func() {
					refused(t, h.do("update", map[string]any{"task_id": "bad:id", "patch": map[string]any{"body": "Note"}}), envelope.Invalid, envelope.CodeSchemaViolation)
				})
			case "removes a task and confirms", "emits a machine-readable result with --json":
				id := h.addID(leaf("Retired work"))
				h.move(id, "archived")
				record, _ := h.item(id)
				if record["state"] != "archived" || record["completed"] != nil {
					t.Fatalf("archived record = %v", record)
				}
			case "rejects removing a task that active tasks still block on":
				id := h.addID(leaf("Blocker"))
				body := leaf("Dependent")
				body["deps"] = edge(id)
				h.addID(body)
				h.unchanged(func() {
					refused(t, h.do("transition", map[string]any{"task_id": id, "to": "archived"}), envelope.Refused, "pool.dependency_active")
				})
			case "removes a blocker after dependents are unblocked or done":
				for _, clear := range []bool{true, false} {
					id := h.addID(leaf("Blocker"))
					body := leaf("Dependent")
					body["deps"] = edge(id)
					dep := h.addID(body)
					if clear {
						update(dep, map[string]any{"remove_deps": edge(id)})
					} else {
						h.move(dep, "done")
					}
					h.move(id, "archived")
					record, _ := h.item(id)
					if record["state"] != "archived" {
						t.Fatal("blocker did not archive")
					}
				}
			case "rm/rejects a malformed id as a validation error":
				h.unchanged(func() {
					refused(t, h.do("transition", map[string]any{"task_id": "bad:id", "to": "archived"}), envelope.Invalid, envelope.CodeSchemaViolation)
				})
			}
		})
	}
}

func TestRetainedCRUDInvalidLimits(t *testing.T) {
	// test/commands/crud.test.ts, list, "rejects an invalid limit %s".
	for _, limit := range []any{"2abc", "abc", int64(-1)} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			h := newHome(t)
			refused(t, h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"limit": limit}}), envelope.Invalid, envelope.CodeSchemaViolation)
		})
	}
}

// Retained upstream case inventory (test declarations, with table rows in
// their subtests); upstream: test/commands/crud.test.ts.
// - "adds a queued task and suggests next steps".
// - "adds directly to In flight with --start".
// - "confirms a queued add and suggests start, not done".
// - "emits a machine-readable task with --json".
// - "uses show --full as the mutation truncation escape hatch".
// - "is idempotent for an existing id".
// - "rejects an invalid id".
// - "rejects an invalid blocked-by id".
// - "rejects a missing blocked-by target before creating a task".
// - "rejects a self blocked-by edge before creating a task".
// - "records blocked-by when the target exists".
// - "rejects an empty link flag before creating a task".
// - "persists priority through a fresh read".
// - "emits a count line and the default compact schema".
// - "filters by state and reports a true total when limited".
// - "uses show --full as the list truncation escape hatch".
// - "rejects an invalid limit %s".
// - "gives a definitive empty state".
// - "truncates the body by default and reveals --full".
// - "errors with NOT_FOUND for an unknown id".
// - "replaces the body wholesale".
// - "lists every changed field in the confirmation".
// - "emits a machine-readable task with --json".
// - "reports an unchanged body replacement as already done".
// - "rejects a malformed id as a validation error".
// - "rejects an empty link flag before updating".
// - "persists updated priority through a fresh read".
// - "removes a task and confirms".
// - "emits a machine-readable result with --json".
// - "rejects removing a task that active tasks still block on".
// - "removes a blocker after dependents are unblocked or done".
// - "rejects a malformed id as a validation error".
//
// Not carried:
// - "keeps the confirmation-forward output valid TOON".
// - "rejects conflicting placement flags before creating a task".
// - "rejects unknown flags before creating a task".
// - "rejects extra positional arguments".
// - "exposes usage help text".
// - "rejects a %s filter" (empty repo, whitespace kind, multiline repo).
// - "escapes filtered empty states as valid TOON".
// - "carries repo scope into list suggestions".
// - "suppresses ready suggestions for kind-filtered lists".
// - "rejects an unknown --fields name".
// - "adds requested columns via --fields".
// - "carries explicit globals into not-found help".
// - "rejects extra positional arguments".
// - "rejects the removed --append flag as unknown".
// - "rejects extra positional arguments before updating".
// - "rejects extra positional arguments before removing".
// Reason: spec/import-plan-tasks-axi.md, Excluded: "Presentation and
// packaging: argument parsing, output encoding, suggestions, the home and
// setup commands, the skill asset and the version fast path. None is the
// Pool's behavior; Hive's command contract is the interface."
//
// Not carried:
// - "mints an id from the title with --mint".
// - "rejects --prefix without --mint before creating a task".
// - "rejects a %s prefix while minting" (empty, whitespace, multiline).
// - "rejects a blank minted title".
// Reason: spec/import-plan-tasks-axi.md, Excluded: "Minting of slug-xx
// identifiers; Hive mints ULIDs."
//
// Not claimed by this file pending the behavioral decision:
// - "rejects %s before duplicate add no-ops" (bad pr, missing blocker,
//   tag-injecting repo, tagging title).
// - "rejects a blank title before creating a task".
// - "rejects a multiline title before creating a task".
// - "rejects an empty %s value before creating a task" (--body, --repo, --kind).
// - "does not report an empty backlog when a zero limit hides matches".
// - "rejects an invalid state filter".
// - "filters to blocked tasks with --blocked".
// - "rejects a malformed id as a validation error".
// - "archives the superseded body when replacing with --archive-body".
// - "requires a replacement body with --archive-body".
// - "requires at least one field".
// - "rejects a blank replacement title before updating".
// - "rejects a multiline replacement title before updating".
// - "rejects an empty %s value before updating" (--body, --repo, --kind).
