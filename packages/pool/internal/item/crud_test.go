package item_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/pool/internal/legacy"
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

// test/commands/crud.test.ts, add/update: the cases below rejected blank,
// whitespace or multiline text upstream. Hive stores the supplied title/body:
// spec/pool.md, What an item carries, says "Kept exactly as written". Their
// Markdown restrictions apply to legacy export, not native item persistence.
// For the two "rejects an empty %s value" tables, the --body row is retained;
// --repo and --kind rows are not carried under the import plan's presentation
// exclusion ("argument parsing ... None is the Pool's behavior").
func TestRetainedCRUDExactText(t *testing.T) {
	for _, name := range []string{
		"rejects a blank title before creating a task",
		"rejects a multiline title before creating a task",
		"rejects an empty %s value before creating a task",
		"rejects a blank replacement title before updating",
		"rejects a multiline replacement title before updating",
		"rejects an empty %s value before updating",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHome(t)
			member, value := "title", "   "
			if strings.Contains(name, "multiline") {
				value = "First\nsecond"
			}
			if strings.Contains(name, "empty %s") {
				member = "body"
			}
			body := leaf("Original text")
			var id string
			if strings.Contains(name, "before updating") {
				body["body"] = "Prior body"
				id = h.addID(body)
				result := h.ok(h.do("update", map[string]any{"task_id": id, "patch": map[string]any{member: value}}))
				if !reflect.DeepEqual(result["changed"], []any{member}) || result["item"].(map[string]any)[member] != value {
					t.Fatalf("exact-text update = %v", result)
				}
			} else {
				body[member] = value
				added := h.add(body)
				id = added["task_id"].(string)
				if added[member] != value {
					t.Fatalf("exact-text add = %v", added)
				}
			}
			stored, _ := h.item(id)
			if stored[member] != value {
				t.Fatalf("stored %s = %q, want %q", member, stored[member], value)
			}
			if member == "title" {
				// The preserved title is not representable as a legacy one-line tag
				// region: export refuses the format without changing the stored item.
				h.unchanged(func() {
					refused(t, h.do("export-legacy", map[string]any{"zone": "UTC", "selector": map[string]any{"task_id": id}}), envelope.Invalid, "pool.legacy_malformed")
				})
			}
		})
	}
}

// test/commands/crud.test.ts, add: "rejects %s before duplicate add no-ops"
// validated metadata before its existing-id no-op. Hive keys retries by the
// whole request: spec/commands.md, Replay, says a different digest with the
// same request_id is "a conflict with code replay_mismatch". Request-schema
// invalidity is refused before replay. Every upstream table row is named:
// the malformed-link row uses the original issue URL. A changed add payload
// is a replay mismatch; done validates harvest URLs at its own boundary.
func TestRetainedCRUDChangedReplayPayload(t *testing.T) {
	t.Run("rejects %s before duplicate add no-ops", func(t *testing.T) {
		for _, row := range []string{"bad pr", "missing blocker", "tag-injecting repo", "tagging title"} {
			t.Run(row, func(t *testing.T) {
				h := newHome(t)
				request := map[string]any{
					"schema": "hive.request", "schema_version": "1.0", "command": "hive-pool", "operation": "add", "operation_version": "1.0",
					"request_id": mint(t, ids.Request), "correlation_id": mint(t, ids.Correlation), "issued_at": "2026-10-14T09:00:00.000Z",
					"caller": map[string]any{"kind": "test"}, "scope_level": "hive", "home_id": h.homeID, "authority": map[string]any{"kind": "none"}, "body": leaf("Original creation"),
				}
				invoke := func() *envelope.Response {
					encoded, err := canon.Encode(request)
					if err != nil {
						t.Fatal(err)
					}
					var command *cli.Command
					for _, c := range h.exe.Commands {
						if c.Name == "hive-pool" {
							command = c
						}
					}
					out, err := h.exe.Handle(cli.Invocation{Command: command, Operation: "add", Home: h.dir, Request: append(encoded, '\n')})
					if err != nil {
						t.Fatal(err)
					}
					response, err := envelope.DecodeResponse(out.Encoded)
					if err != nil {
						t.Fatal(err)
					}
					return response
				}
				first := invoke()
				h.ok(first)
				before := h.files("")
				changed := leaf("Duplicate title")
				status, code := envelope.Conflict, envelope.CodeReplayMismatch
				switch row {
				case "bad pr":
					changed["links"] = []any{map[string]any{"kind": "harvest", "url": "https://github.com/o/r/issues/9"}}
				case "missing blocker":
					changed["deps"] = []any{map[string]any{"type": "blocked-by", "task_id": mint(t, ids.Task)}}
				case "tag-injecting repo":
					changed["repo"] = "foo(bar)"
					status, code = envelope.Invalid, envelope.CodeSchemaViolation
				case "tagging title":
					changed["title"] = "Duplicate title (repo: app)"
				}
				request["body"] = changed
				refused(t, invoke(), status, code)
				if !same(before, h.files("")) {
					t.Fatal("refused changed-payload retry wrote files")
				}
			})
		}
	})
}

// test/commands/crud.test.ts, list: "does not report an empty backlog when
// a zero limit hides matches" accepted --limit 0 upstream. Hive's request
// contract requires a positive limit; its replacement is named omissions.
// spec/pool.md, Reading the Pool, says "an incomplete read says so and is
// never an authoritative empty result". Every matching identity is accounted
// for by a returned record or an omission of reason limit, once each.
func TestRetainedCRUDLimitedProjection(t *testing.T) {
	t.Run("does not report an empty backlog when a zero limit hides matches", func(t *testing.T) {
		h := newHome(t)
		want := map[string]bool{}
		for _, title := range []string{"One match", "Another match", "A third match"} {
			want[h.addID(leaf(title))] = true
		}
		body := h.ok(h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"states": []any{"queued"}, "limit": int64(1)}}))
		records := body["records"].([]any)
		projection := body["projection"].(map[string]any)
		omitted := projection["omitted"].([]any)
		if len(records) != 1 || len(omitted) != 2 || projection["complete"] != false {
			t.Fatalf("limited projection = %v", body)
		}
		seen := map[string]bool{}
		for _, v := range records {
			seen[v.(map[string]any)["task_id"].(string)] = true
		}
		for _, v := range omitted {
			omission := v.(map[string]any)
			id := omission["id"].(string)
			if seen[id] || omission["reason"] != "limit" {
				t.Fatalf("omission = %v", omission)
			}
			seen[id] = true
		}
		if !reflect.DeepEqual(seen, want) {
			t.Fatalf("accounted matches = %v, want %v", seen, want)
		}
	})
}

// test/commands/crud.test.ts, list: "filters to blocked tasks with --blocked"
// supplied a blocked selector upstream. Hive's item read supports state and
// ready selectors; blocked is a returned derived fact. spec/pool.md, Derived
// facts, says "blocked, held, ready and landed are never stored" and read
// "computes them at the moment of the read". The view selects those facts.
func TestRetainedCRUDDerivedBlockedView(t *testing.T) {
	t.Run("filters to blocked tasks with --blocked", func(t *testing.T) {
		h := newHome(t)
		blocker := h.addID(leaf("Blocker"))
		body := leaf("Dependent")
		body["deps"] = []any{map[string]any{"type": "blocked-by", "task_id": blocker}}
		dep := h.addID(body)
		free := h.addID(leaf("Independent"))
		read := func(selector map[string]any) map[string]any {
			return h.ok(h.do("read", map[string]any{"kind": "item", "selector": selector}))
		}
		queued := read(map[string]any{"states": []any{"queued"}})
		if len(queued["records"].([]any)) != 3 || queued["projection"].(map[string]any)["complete"] != true {
			t.Fatalf("queued view = %v", queued)
		}
		var blocked []string
		for i, v := range queued["records"].([]any) {
			if queued["derived"].([]any)[i].(map[string]any)["blocked"].(bool) {
				blocked = append(blocked, v.(map[string]any)["task_id"].(string))
			}
		}
		if !reflect.DeepEqual(blocked, []string{dep}) {
			t.Fatalf("blocked view = %v", blocked)
		}
		ready := read(map[string]any{"ready": true})
		seen := map[string]bool{}
		for _, v := range ready["records"].([]any) {
			seen[v.(map[string]any)["task_id"].(string)] = true
		}
		if !reflect.DeepEqual(seen, map[string]bool{blocker: true, free: true}) {
			t.Fatalf("ready view = %v", seen)
		}
	})
}

// test/commands/crud.test.ts, show: "rejects a malformed id as a validation
// error" validated its slug before lookup. Hive's read-by-identity contract
// answers not_found for a nonmatching selector: spec/pool.md, Reading the
// Pool, defines reading "one kind by identity or by query". The malformed
// read selector is kept apart from add/update/remove identity validation.
func TestRetainedCRUDMalformedReadIdentity(t *testing.T) {
	t.Run("rejects a malformed id as a validation error", func(t *testing.T) {
		h := newHome(t)
		h.addID(leaf("Existing item"))
		h.unchanged(func() {
			refused(t, h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"id": "bad:id"}}), envelope.NotFound, envelope.CodeNotFound)
		})
	})
}

// test/commands/crud.test.ts, update: "archives the superseded body when
// replacing with --archive-body" and "requires a replacement body with
// --archive-body" wrote an archive and imposed a writer flag rule upstream.
// Hive keeps archive lookup and byte preservation; the writer and its
// required-body rule are not carried. spec/import-plan-tasks-axi.md, Portions,
// keeps "Archive lookup for the export", and Local changes says prune
// "survives only as archive lookup for import".
func TestRetainedCRUDNoteArchiveLookup(t *testing.T) {
	for _, name := range []string{"archives the superseded body when replacing with --archive-body", "requires a replacement body with --archive-body"} {
		t.Run(name, func(t *testing.T) {
			text := "\n## Archived 2026-10-14\n- [ ] notes-a - Work\n  Old line one.\n  Old line two.\n"
			path := filepath.Join(t.TempDir(), legacy.NoteArchiveName)
			if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			archive, err := legacy.ParseArchive(source)
			if err != nil {
				t.Fatal(err)
			}
			found, exists := legacy.Lookup("notes-a", nil, archive)
			if !exists || !found.Archived || found.Task.Body != "Old line one.\nOld line two." {
				t.Fatalf("prior body = %+v / %v", found, exists)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(archive.Render()) != text || string(after) != text {
				t.Fatal("archive lookup changed its bytes")
			}
		})
	}
}

// test/commands/crud.test.ts, update: "requires at least one field" refused
// an empty patch upstream. Hive treats it as a no-op: spec/pool.md says a
// patch that changes nothing "writes nothing, raises no revision and returns
// no receipt". The response still carries the unchanged item and changed=[].
func TestRetainedCRUDEmptyPatch(t *testing.T) {
	t.Run("requires at least one field", func(t *testing.T) {
		h := newHome(t)
		id := h.addID(leaf("Empty patch"))
		before, _ := h.item(id)
		h.unchanged(func() {
			response := h.do("update", map[string]any{"task_id": id, "patch": map[string]any{}})
			result := h.ok(response)
			if len(result["changed"].([]any)) != 0 || len(response.Receipts) != 0 || !reflect.DeepEqual(result["item"], before) {
				t.Fatalf("empty patch = %+v", response)
			}
		})
		after, _ := h.item(id)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("empty patch changed the item/revision")
		}
	})
}

// Retained upstream case inventory (test declarations, with table rows in
// their subtests); upstream: test/commands/crud.test.ts.
// - "adds a queued task and suggests next steps".
// - "adds directly to In flight with --start".
// - "confirms a queued add and suggests start, not done".
// - "emits a machine-readable task with --json".
// - "uses show --full as the mutation truncation escape hatch".
// - "is idempotent for an existing id".
// - "rejects %s before duplicate add no-ops" (bad pr, missing blocker, tag-injecting repo, tagging title).
// - "rejects an invalid id".
// - "rejects a blank title before creating a task".
// - "rejects a multiline title before creating a task".
// - "rejects an invalid blocked-by id".
// - "rejects a missing blocked-by target before creating a task".
// - "rejects a self blocked-by edge before creating a task".
// - "records blocked-by when the target exists".
// - "rejects an empty link flag before creating a task".
// - "rejects an empty %s value before creating a task" (--body row; --repo/--kind are excluded below).
// - "persists priority through a fresh read".
// - "emits a count line and the default compact schema".
// - "filters by state and reports a true total when limited".
// - "does not report an empty backlog when a zero limit hides matches".
// - "uses show --full as the list truncation escape hatch".
// - "rejects an invalid limit %s" (2abc, abc, -1).
// - "filters to blocked tasks with --blocked".
// - "gives a definitive empty state".
// - "truncates the body by default and reveals --full".
// - "rejects a malformed id as a validation error".
// - "errors with NOT_FOUND for an unknown id".
// - "replaces the body wholesale".
// - "archives the superseded body when replacing with --archive-body".
// - "requires a replacement body with --archive-body".
// - "lists every changed field in the confirmation".
// - "emits a machine-readable task with --json".
// - "reports an unchanged body replacement as already done".
// - "requires at least one field".
// - "rejects a malformed id as a validation error".
// - "rejects a blank replacement title before updating".
// - "rejects a multiline replacement title before updating".
// - "rejects an empty link flag before updating".
// - "rejects an empty %s value before updating" (--body row; --repo/--kind are excluded below).
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
// The --repo and --kind rows of these two tables are not carried:
// - "rejects an empty %s value before creating a task".
// - "rejects an empty %s value before updating".
// Reason: spec/import-plan-tasks-axi.md, Excluded, names argument parsing
// as presentation and says "None is the Pool's behavior; Hive's command
// contract is the interface". Native title/body rows are retained above.
//

// test/commands/crud.test.ts: "rejects an invalid state filter". Hive's
// in-flight spelling is valid; bogus is outside the item state vocabulary.
func TestRetainedCRUDInvalidStateFilter(t *testing.T) {
	t.Run("rejects an invalid state filter", func(t *testing.T) {
		for _, states := range [][]any{{"bogus"}, {"queued", "bogus"}} {
			h := newHome(t)
			h.addID(leaf("Queued work"))
			retainedNoWrites(h, func() {
				resp := h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"states": states}})
				refused(t, resp, envelope.Invalid, envelope.CodeSchemaViolation)
				if !strings.Contains(resp.Error.Message, "states") {
					t.Fatalf("field absent from error: %+v", resp.Error)
				}
			})
		}
	})
}
