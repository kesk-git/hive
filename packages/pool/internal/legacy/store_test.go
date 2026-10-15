package legacy_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/journal"
	"github.com/kesk-git/hive/packages/pool/internal/item"
	"github.com/kesk-git/hive/packages/pool/internal/legacy"
)

// Re-expressed from test/backends/markdown.test.ts at tasks-axi revision
// 9401ff899c0d1d8ae6b4fd8727b9025abda2032c; see imports/tasks-axi.json.
// Subtests name their upstream cases. Store mutations use the Pool executable
// and legacy export; the format-specific validation cases use its translated
// normalization interface, because the Pool's native records carry whole
// strings rather than Markdown tags. Fixtures are synthetic Hive text.
func TestRetainedStoreNormalization(t *testing.T) {
	for _, name := range []string{
		"rejects blank titles", "rejects multiline titles",
		"rejects titles that end with canonical trailing tags",
		"rejects repo values that would inject canonical tags",
		"rejects create links that cannot round-trip with their kind",
		"rejects out-of-range priority values",
		"rejects unsafe hold fields before rendering",
		"rejects invalid date tags before rendering",
		"rejects empty added links before updating",
		"rejects added links that cannot round-trip with their kind",
		"rejects blank replacement titles", "rejects multiline replacement titles",
		"rejects replacement titles that end with canonical trailing tags",
		"rejects kind values that would split canonical tags",
		"rejects out-of-range priority updates",
		"rejects dependency ids that cannot round-trip through markdown",
		"rejects multiline dependency reasons before writing",
		"rejects dependency reasons that contain edge markers before writing",
		"rejects self-dependencies before writing",
	} {
		t.Run(name, func(t *testing.T) {
			reject := func(err error) {
				t.Helper()
				if err == nil {
					t.Fatal("legacy normalization accepted an unsafe value")
				}
			}
			switch name {
			case "rejects blank titles", "rejects blank replacement titles":
				_, err := legacy.NormalizeTitle("   ")
				reject(err)
			case "rejects multiline titles", "rejects multiline replacement titles":
				for _, title := range []string{"First\nsecond", "First\r\nsecond"} {
					_, err := legacy.NormalizeTitle(title)
					reject(err)
				}
			case "rejects titles that end with canonical trailing tags", "rejects replacement titles that end with canonical trailing tags":
				for _, title := range []string{"Work (repo: app)", "Work (kind: ship)", "Work blocked-by: blocker-a"} {
					_, err := legacy.NormalizeTitle(title)
					reject(err)
				}
				safe := "report.md (reported 2026-10-02): write summary"
				got, err := legacy.NormalizeTitle(safe)
				if err != nil || got != safe {
					t.Fatalf("safe prose = %q, %v", got, err)
				}
			case "rejects repo values that would inject canonical tags":
				_, err := legacy.NormalizeTagValue("app) (kind: ship", "repo")
				reject(err)
			case "rejects kind values that would split canonical tags":
				_, err := legacy.NormalizeTagValue("ship\nscout", "kind")
				reject(err)
			case "rejects create links that cannot round-trip with their kind":
				_, err := legacy.NormalizeTypedLink(item.Link{Kind: item.LinkPR, URL: "https://github.com/acme/app/issues/9"})
				reject(err)
			case "rejects out-of-range priority values", "rejects out-of-range priority updates":
				_, err := legacy.NormalizePriority(7)
				reject(err)
			case "rejects unsafe hold fields before rendering":
				for _, hold := range []legacy.Hold{{Reason: "wait (blocked)"}, {Reason: "Wait", Until: "tomorrow"}} {
					_, err := legacy.NormalizeHold(hold)
					reject(err)
				}
			case "rejects invalid date tags before rendering":
				for field, value := range map[string]string{"created": "tomorrow", "closed": "06/22"} {
					_, err := legacy.NormalizeDate(value, field)
					reject(err)
				}
			case "rejects empty added links before updating":
				_, err := legacy.NormalizeTypedLink(item.Link{Kind: item.LinkPR, URL: ""})
				reject(err)
			case "rejects added links that cannot round-trip with their kind":
				for _, link := range []item.Link{{Kind: item.LinkPR, URL: "https://github.com/acme/app/issues/9"}, {Kind: item.LinkPR, URL: " https://github.com/acme/app/pull/9 "}, {Kind: item.LinkReport, URL: "reports/item/report.md"}} {
					_, err := legacy.NormalizeTypedLink(link)
					reject(err)
				}
			case "rejects dependency ids that cannot round-trip through markdown":
				_, err := legacy.NormalizeDep("owner-a", legacy.Dep{Type: "blocked-by", ID: "bad:id"})
				reject(err)
			case "rejects multiline dependency reasons before writing":
				for _, reason := range []string{"waits\n- [ ] injected-a - bad", "waits\r- [ ] injected-a - bad"} {
					_, err := legacy.NormalizeDep("owner-a", legacy.Dep{Type: "blocked-by", ID: "other-b", Reason: reason})
					reject(err)
				}
			case "rejects dependency reasons that contain edge markers before writing":
				for _, typ := range []string{"blocked-by", "parent", "discovered-from"} {
					_, err := legacy.NormalizeDep("owner-a", legacy.Dep{Type: "blocked-by", ID: "other-b", Reason: "waits " + typ + ": injected-a - hidden"})
					reject(err)
				}
			case "rejects self-dependencies before writing":
				_, err := legacy.NormalizeDep("owner-a", legacy.Dep{Type: "blocked-by", ID: "owner-a"})
				reject(err)
			}
		})
	}
}

// retainedStoreImport writes only the synthetic source in a test directory;
// importPath checks that import-legacy does not change its bytes or mtime.
func retainedStoreImport(t *testing.T, h *home, text string) *imported {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backlog.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return h.importPath(path, []byte(text), info.ModTime(), "UTC")
}

func TestRetainedStoreOperations(t *testing.T) {
	for _, name := range []string{
		"creates a queued task and reads it back",
		"stamps created for non-done tasks via the injected clock",
		"rejects a duplicate id with CONFLICT",
		"creates and reads structured holds",
		"a single mutation only alters the targeted line",
		"adding a body leaves all original lines intact",
		"a no-holds backlog still renders byte-exactly after parsing",
		"rejects writes when the backlog changed after load",
		"replaces the body as continuation lines",
		"adds body lines without duplicating existing lines",
		"does not rewrite when added body lines already exist",
		"does not archive an unchanged body replacement",
		"does not archive the same body twice after replacement",
		"folds an added link into the prose and re-derives links",
		"dedupes added links by exact parsed url, not substring",
		"throws NOT_FOUND for an unknown id",
		"moves queued -> in_flight and stamps since",
		"carries a multi-paragraph body through start (queued -> in_flight)",
		"carries a multi-paragraph body through done (in_flight -> done)",
		"moves to done, records the pr link and a merged stamp",
		"records a shorter transition link when a longer one already exists",
		"rejects empty transition links before moving",
		"rejects invalid transition dates before moving",
		"addDep is idempotent (false on an existing edge)",
		"rejects addDep when the dependency target is missing",
		"rejects create when a dependency target is missing",
		"removeDep returns false when there is no such edge",
		"normalizes every id'd task and returns the count",
		"creates sections on first write to an empty file",
		"surfaces an unknown id as null from get",
		"uses AxiError for structured failures",
		"sees the `- [ ]` checkbox in-flight item that firstmate writes",
		"keeps a `blocked-by: <id> - <reason>` item out of `ready`",
		"keeps an item with a later active reason-bearing blocker out of `ready`",
		"surfaces the item as ready once its blocker is done",
		"preserves firstmate's lines byte-for-byte on a read-only load",
		"preserves the blocked-by reason when the item is later mutated",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHome(t)
			text := "# Backlog\n\n## In flight\n- [ ] blocker-a - Unfinished blocker (repo: app)\n\n## Queued\n- [ ] work-b - Work (repo: app) (kind: ship)\n  First paragraph.\n\n  Second paragraph.\n  ## Intent\n\n  Final line.\n- [ ] neighbor-c - Keep this line  (repo: app)\n\n## Done\n- [x] complete-d - Completed blocker (merged 2026-10-01)\n"
			im := retainedStoreImport(t, h, text)
			id := im.taskID("work-b")
			blocker := im.taskID("blocker-a")
			done := im.taskID("complete-d")
			migrationID := im.migration["migration_id"].(string)
			export := func() string { return h.exportMigration(migrationID, nil)["text"].(string) }
			patch := func(p map[string]any) map[string]any {
				return ok(t, h.do("update", map[string]any{"task_id": id, "patch": p}))
			}
			transition := func(to string, extra map[string]any) map[string]any {
				body := map[string]any{"task_id": id, "to": to}
				for k, v := range extra {
					body[k] = v
				}
				return ok(t, h.do("transition", body))["item"].(map[string]any)
			}
			edge := func(other, reason string) []any {
				return []any{map[string]any{"type": "blocked-by", "task_id": other, "reason": reason}}
			}
			readItem := func() (map[string]any, map[string]any) {
				body := ok(t, h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"id": id}}))
				return body["records"].([]any)[0].(map[string]any), body["derived"].([]any)[0].(map[string]any)
			}
			samePool := func(f func()) {
				before := snapshot(t, h.dir)
				f()
				if !equal(before, snapshot(t, h.dir)) {
					t.Fatal("no-op/refusal changed the Pool")
				}
			}
			switch name {
			case "creates a queued task and reads it back", "stamps created for non-done tasks via the injected clock":
				added := ok(t, h.do("add", map[string]any{"title": "Brand new work", "level": "prey"}))["item"].(map[string]any)
				body := ok(t, h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"id": added["task_id"]}}))
				record := body["records"].([]any)[0].(map[string]any)
				if record["title"] != "Brand new work" || record["state"] != "queued" || record["created_at"] != "2026-10-14T09:00:00.000Z" {
					t.Fatalf("created = %v", record)
				}
				rendered := ok(t, h.do("export-legacy", map[string]any{"zone": "UTC", "selector": map[string]any{"task_id": added["task_id"]}}))["text"].(string)
				if !strings.Contains(rendered, "Brand new work") || !strings.Contains(rendered, "(since 2026-10-14)") {
					t.Fatalf("export = %q", rendered)
				}
			case "rejects a duplicate id with CONFLICT":
				samePool(func() { refused(t, h.liveImport(im.path, "UTC", nil), envelope.Conflict, envelope.CodeAlreadyExists) })
			case "creates and reads structured holds":
				hold := ok(t, h.do("hold", map[string]any{"task_id": id, "kind": "resource", "reason": "Capacity clears", "until": map[string]any{"at": "2999-01-01T00:00:00.000Z", "zone": "UTC", "date": "2999-01-01"}}))["hold"].(map[string]any)
				got := ok(t, h.do("read", map[string]any{"kind": "hold", "selector": map[string]any{"id": hold["hold_id"]}}))["records"].([]any)[0].(map[string]any)
				if !reflect.DeepEqual(hold, got) || !strings.Contains(export(), "(hold: Capacity clears) (hold-kind: load) (hold-until: 2999-01-01)") {
					t.Fatalf("hold = %v, export = %q", got, export())
				}
			case "a single mutation only alters the targeted line":
				patch(map[string]any{"add_deps": edge(blocker, "Wait for the blocker")})
				after := export()
				var removed []string
				for _, line := range strings.Split(text, "\n") {
					if !slices.Contains(strings.Split(after, "\n"), line) {
						removed = append(removed, line)
					}
				}
				if len(removed) != 1 || !strings.Contains(removed[0], "work-b") {
					t.Fatalf("removed lines = %q", removed)
				}
			case "adding a body leaves all original lines intact":
				patch(map[string]any{"add_body_lines": []any{"Added note"}})
				after := export()
				for _, line := range strings.Split(text, "\n") {
					if !strings.Contains(after, line) {
						t.Errorf("original line lost: %q", line)
					}
				}
				if !strings.Contains(after, "\n  Added note") {
					t.Fatal("added body missing")
				}
			case "a no-holds backlog still renders byte-exactly after parsing", "preserves firstmate's lines byte-for-byte on a read-only load":
				before := snapshot(t, h.dir)
				readItem()
				if got := export(); got != text {
					t.Fatalf("read-only export differs: %q", got)
				}
				if !equal(before, snapshot(t, h.dir)) {
					t.Fatal("read/export wrote records")
				}
			case "rejects writes when the backlog changed after load":
				before := snapshot(t, h.dir)
				edited := text + "\nManual edit\n"
				if err := os.WriteFile(im.path, []byte(edited), 0o600); err != nil {
					t.Fatal(err)
				}
				refused(t, h.liveImport(im.path, "UTC", map[string]any{"expected_digest": canon.FileDigest([]byte(text))}), envelope.Conflict, envelope.CodeRevisionConflict)
				if !equal(before, snapshot(t, h.dir)) {
					t.Fatal("digest conflict wrote records")
				}
				data, err := os.ReadFile(im.path)
				if err != nil || string(data) != edited {
					t.Fatal("manual edit changed")
				}
			case "replaces the body as continuation lines":
				patch(map[string]any{"body": "Started 2026-10-14\nCurrent only"})
				got := export()
				if !strings.Contains(got, "\n  Started 2026-10-14\n  Current only") || strings.Contains(got, "First paragraph.") {
					t.Fatalf("replacement export = %q", got)
				}
			case "adds body lines without duplicating existing lines":
				patch(map[string]any{"add_body_lines": []any{"First paragraph.", "New note"}})
				got := export()
				if strings.Count(got, "First paragraph.") != 1 || !strings.Contains(got, "\n  New note") {
					t.Fatalf("added body = %q", got)
				}
			case "does not rewrite when added body lines already exist":
				samePool(func() {
					result := patch(map[string]any{"add_body_lines": []any{"First paragraph."}})
					if len(result["changed"].([]any)) != 0 {
						t.Fatal("existing line changed")
					}
				})
				if export() != text {
					t.Fatal("no-op changed exact export")
				}
			case "does not archive an unchanged body replacement":
				samePool(func() {
					record, _ := readItem()
					result := patch(map[string]any{"body": record["body"]})
					if len(result["changed"].([]any)) != 0 {
						t.Fatal("unchanged body changed")
					}
				})
			case "does not archive the same body twice after replacement":
				patch(map[string]any{"body": "Current note"})
				samePool(func() { patch(map[string]any{"body": "Current note"}) })
			case "folds an added link into the prose and re-derives links", "dedupes added links by exact parsed url, not substring":
				longer := map[string]any{"kind": "harvest", "url": "https://github.com/acme/app/pull/10"}
				shorter := map[string]any{"kind": "harvest", "url": "https://github.com/acme/app/pull/1"}
				patch(map[string]any{"add_links": []any{longer}})
				patch(map[string]any{"add_links": []any{shorter}})
				samePool(func() { patch(map[string]any{"add_links": []any{shorter}}) })
				record, _ := readItem()
				if !reflect.DeepEqual(record["links"], []any{longer, shorter}) {
					t.Fatalf("links = %v", record["links"])
				}
				got := export()
				parsed, err := legacy.Parse([]byte(got))
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(parsed.Find("work-b").Links, []item.Link{{Kind: item.LinkPR, URL: longer["url"].(string)}, {Kind: item.LinkPR, URL: shorter["url"].(string)}}) {
					t.Fatalf("re-derived links = %v", parsed.Find("work-b").Links)
				}
			case "throws NOT_FOUND for an unknown id":
				samePool(func() {
					refused(t, h.do("update", map[string]any{"task_id": mint(t, ids.Task), "patch": map[string]any{"title": "Changed"}}), envelope.NotFound, envelope.CodeNotFound)
				})
			case "moves queued -> in_flight and stamps since", "carries a multi-paragraph body through start (queued -> in_flight)", "carries a multi-paragraph body through done (in_flight -> done)":
				before, _ := readItem()
				to := "in-flight"
				if name == "carries a multi-paragraph body through done (in_flight -> done)" {
					transition("in-flight", nil)
					to = "done"
				}
				record := transition(to, nil)
				if record["body"] != before["body"] {
					t.Fatal("transition lost body")
				}
				got := export()
				b, err := legacy.Parse([]byte(got))
				if err != nil {
					t.Fatal(err)
				}
				task := b.Find("work-b")
				if string(task.State) != map[string]string{"in-flight": "in_flight", "done": "done"}[to] || task.Body != before["body"] {
					t.Fatalf("transition export task = %+v", task)
				}
			case "moves to done, records the pr link and a merged stamp", "records a shorter transition link when a longer one already exists":
				longer := map[string]any{"kind": "harvest", "url": "https://github.com/acme/app/pull/10"}
				shorter := map[string]any{"kind": "harvest", "url": "https://github.com/acme/app/pull/1"}
				patch(map[string]any{"add_links": []any{longer}})
				record := transition("done", map[string]any{"evidence": []any{shorter}})
				if record["closed_at"] != "2026-10-14T09:00:00.000Z" || !reflect.DeepEqual(record["links"], []any{longer, shorter}) {
					t.Fatalf("completion = %v", record)
				}
				got := export()
				if !strings.Contains(got, "(merged 2026-10-14)") || !strings.Contains(got, shorter["url"].(string)) {
					t.Fatalf("completion export = %q", got)
				}
			case "rejects empty transition links before moving", "rejects invalid transition dates before moving":
				body := map[string]any{"task_id": id, "to": "done"}
				if name == "rejects empty transition links before moving" {
					body["evidence"] = []any{map[string]any{"kind": "harvest", "url": ""}}
				} else {
					body["at"] = "06/22"
				}
				samePool(func() { refused(t, h.do("transition", body), envelope.Invalid, envelope.CodeSchemaViolation) })
			case "addDep is idempotent (false on an existing edge)":
				result := patch(map[string]any{"add_deps": edge(done, "Done blocker")})
				if !reflect.DeepEqual(result["changed"], []any{"deps"}) {
					t.Fatalf("changed = %v", result)
				}
				samePool(func() {
					if len(patch(map[string]any{"add_deps": edge(done, "Done blocker")})["changed"].([]any)) != 0 {
						t.Fatal("existing edge changed")
					}
				})
			case "rejects addDep when the dependency target is missing":
				samePool(func() {
					refused(t, h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"add_deps": edge(mint(t, ids.Task), "Missing")}}), envelope.Refused, "pool.dependency_missing")
				})
			case "rejects create when a dependency target is missing":
				samePool(func() {
					refused(t, h.do("add", map[string]any{"title": "Missing blocker", "level": "prey", "deps": edge(mint(t, ids.Task), "Missing")}), envelope.Refused, "pool.dependency_missing")
				})
			case "removeDep returns false when there is no such edge":
				samePool(func() {
					if len(patch(map[string]any{"remove_deps": edge(mint(t, ids.Task), "")})["changed"].([]any)) != 0 {
						t.Fatal("absent edge changed")
					}
				})
			case "normalizes every id'd task and returns the count":
				got := h.exportMigration(migrationID, map[string]any{"normalize": true})
				if len(got["items"].([]any)) != 4 {
					t.Fatalf("normalized items = %v", got["items"])
				}
			case "creates sections on first write to an empty file":
				empty := newHome(t)
				added := ok(t, empty.do("add", map[string]any{"title": "First task", "level": "prey"}))["item"].(map[string]any)
				got := ok(t, empty.do("export-legacy", map[string]any{"zone": "UTC"}))["text"].(string)
				for _, piece := range []string{"## In flight", "## Queued", "## Done", added["task_id"].(string)} {
					if !strings.Contains(got, piece) {
						t.Errorf("export lacks %q", piece)
					}
				}
			case "surfaces an unknown id as null from get", "uses AxiError for structured failures":
				// Hive substitutes a structured not_found envelope for null/AxiError.
				refused(t, h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"id": mint(t, ids.Task)}}), envelope.NotFound, envelope.CodeNotFound)
			case "sees the `- [ ]` checkbox in-flight item that firstmate writes":
				record := h.records("item")[blocker]
				if record["state"] != "in-flight" || export() != text {
					t.Fatalf("checkbox import = %v", record)
				}
			case "keeps a `blocked-by: <id> - <reason>` item out of `ready`", "keeps an item with a later active reason-bearing blocker out of `ready`", "surfaces the item as ready once its blocker is done", "preserves the blocked-by reason when the item is later mutated":
				reason := "Wait for unfinished work"
				deps := edge(blocker, reason)
				if name == "keeps an item with a later active reason-bearing blocker out of `ready`" {
					deps = append(edge(done, "First blocker done"), deps...)
				}
				patch(map[string]any{"add_deps": deps})
				record, facts := readItem()
				if !facts["blocked"].(bool) || facts["ready"].(bool) {
					t.Fatalf("blocked facts = %v", facts)
				}
				if name == "surfaces the item as ready once its blocker is done" {
					ok(t, h.do("transition", map[string]any{"task_id": blocker, "to": "done"}))
					_, facts = readItem()
					if !facts["ready"].(bool) {
						t.Fatal("completed blocker did not make item ready")
					}
				}
				if name == "preserves the blocked-by reason when the item is later mutated" {
					patch(map[string]any{"title": "Renamed work"})
					record, _ = readItem()
					if !reflect.DeepEqual(record["deps"], deps) || !strings.Contains(export(), "blocked-by: blocker-a - "+reason) {
						t.Fatalf("reason lost: %v", record)
					}
				}
			}
		})
	}
}

// Prune itself is not carried: spec/import-plan-tasks-axi.md, Local changes,
// says "prune is not carried for Hive's records and survives only as archive
// lookup for import". These cases retain the lookup and byte preservation of
// the surplus, using a synthetic archive rather than pruning Pool records.
func TestRetainedStoreArchiveLookup(t *testing.T) {
	for _, name := range []string{"keeps N recent done tasks and archives the rest", "preserves free-form done lines (does not count or archive them)", "is a no-op when under the keep count"} {
		t.Run(name, func(t *testing.T) {
			liveText := "## Done\n- [x] recent-a - Recent completion (merged 2026-10-14)\n- [x] PR #41 (contributor) - Keep this free line\n"
			archiveText := "\n## Archived 2026-10-14\n- [x] older-b - Older completion (merged 2026-10-01)\n  Earlier report note.\n"
			live, err := legacy.Parse([]byte(liveText))
			if err != nil {
				t.Fatal(err)
			}
			archive, err := legacy.ParseArchive([]byte(archiveText))
			if err != nil {
				t.Fatal(err)
			}
			found, exists := legacy.Lookup("older-b", live, archive)
			if !exists || !found.Archived || found.Task.State != legacy.StateDone || found.Task.Body != "Earlier report note." || found.Stamp != "2026-10-14" {
				t.Fatalf("archive lookup = %+v, %v", found, exists)
			}
			if string(live.Render()) != liveText || string(archive.Render()) != archiveText {
				t.Fatal("lookup changed bytes")
			}
			if name == "preserves free-form done lines (does not count or archive them)" {
				if len(live.Tasks()) != 1 {
					t.Fatalf("free line counted as task: %v", live.Tasks())
				}
				live.Normalize()
				if !strings.Contains(string(live.Render()), "- [x] PR #41 (contributor) - Keep this free line") {
					t.Fatal("normalization changed free-form done line")
				}
			}
			if name == "is a no-op when under the keep count" {
				found, exists := legacy.Lookup("recent-a", live, archive)
				if !exists || found.Archived {
					t.Fatal("live entry lost to archive")
				}
			}
		})
	}
}

// test/backends/markdown.test.ts: "filters list by state, repo, and kind
// with a true total" used the Markdown repo/kind filters. Hive item read uses
// state and ready selectors; repo/kind/blocked selector forms are not carried.
// spec/pool.md, Reading the Pool, says read returns every record whole with a
// projection header and "an incomplete read says so and is never an
// authoritative empty result". Derived facts says blocked is computed at read.
func TestRetainedStoreSelectors(t *testing.T) {
	t.Run("filters list by state, repo, and kind with a true total", func(t *testing.T) {
		h := newHome(t)
		im := retainedStoreImport(t, h, "## In flight\n- [ ] blocker-a - Unfinished\n## Queued\n- [ ] blocked-b - Wait blocked-by: blocker-a - Needs the blocker\n- [ ] ready-c - Ready\n## Done\n- [x] done-d - Completed (merged 2026-10-01)\n")
		read := func(selector map[string]any) map[string]any {
			return ok(t, h.do("read", map[string]any{"kind": "item", "selector": selector}))
		}
		limited := read(map[string]any{"states": []any{"queued"}, "limit": int64(1)})
		rows := limited["records"].([]any)
		projection := limited["projection"].(map[string]any)
		omissions := projection["omitted"].([]any)
		if len(rows) != 1 || len(omissions) != 1 || projection["complete"] != false {
			t.Fatalf("limited view = %v", limited)
		}
		seen := map[string]bool{rows[0].(map[string]any)["task_id"].(string): true}
		for _, v := range omissions {
			row := v.(map[string]any)
			id := row["id"].(string)
			if row["reason"] != "limit" || seen[id] {
				t.Fatalf("omission = %v", row)
			}
			seen[id] = true
		}
		if !seen[im.taskID("blocked-b")] || !seen[im.taskID("ready-c")] || len(seen) != 2 {
			t.Fatalf("records/omissions = %v", seen)
		}
		ready := read(map[string]any{"ready": true})
		readyRows := ready["records"].([]any)
		if len(readyRows) != 1 || readyRows[0].(map[string]any)["task_id"] != im.taskID("ready-c") || ready["projection"].(map[string]any)["complete"] != true {
			t.Fatalf("ready view = %v", ready)
		}
		all := read(map[string]any{})
		var blocked []string
		for i, row := range all["records"].([]any) {
			if all["derived"].([]any)[i].(map[string]any)["blocked"].(bool) {
				blocked = append(blocked, row.(map[string]any)["task_id"].(string))
			}
		}
		if !reflect.DeepEqual(blocked, []string{im.taskID("blocked-b")}) {
			t.Fatalf("derived blocked view = %v", blocked)
		}
	})
}

// test/backends/markdown.test.ts: "archives the superseded body before
// replacing it" and "restores the note archive when the active backlog write
// fails" are re-expressed as lookup of preserved archive bytes. The native
// archive_body writer and its rollback are not carried: the import plan's
// Portions keeps "Archive lookup for the export", and Local changes says
// "prune is not carried for Hive's records and survives only as archive
// lookup for import". No Pool record or archive is pruned or appended here.
func TestRetainedStoreNoteArchiveLookup(t *testing.T) {
	for _, name := range []string{"archives the superseded body before replacing it", "restores the note archive when the active backlog write fails"} {
		t.Run(name, func(t *testing.T) {
			text := "\n## Archived 2026-10-14\n- [ ] note-a - Work (repo: app)\n  Old line one.\n  Old line two.\n"
			path := filepath.Join(t.TempDir(), legacy.NoteArchiveName)
			if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			archive, err := legacy.ParseArchive(data)
			if err != nil {
				t.Fatal(err)
			}
			found, exists := legacy.Lookup("note-a", nil, archive)
			if !exists || !found.Archived || found.Task.Body != "Old line one.\nOld line two." || found.Stamp != "2026-10-14" {
				t.Fatalf("prior body lookup = %+v / %v", found, exists)
			}
			live, err := legacy.Parse([]byte("## Queued\n- [ ] note-a - Work (repo: app)\n  Current note.\n"))
			if err != nil {
				t.Fatal(err)
			}
			current, exists := legacy.Lookup("note-a", live, archive)
			if !exists || current.Archived || current.Task.Body != "Current note." {
				t.Fatalf("live lookup = %+v / %v", current, exists)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(archive.Render()) != text || string(after) != text {
				t.Fatal("lookup changed archive bytes")
			}
		})
	}
}

// test/backends/markdown.test.ts: "sets repo and kind as canonical tags"
// changed them through update upstream. Native mutation is not carried;
// spec/pool.md calls legacy identity, kind, repo and free-form fields
// "verbatim", and its export renders "Legacy words ... from the legacy
// member". Import preserves that metadata and canonical export emits it.
func TestRetainedStoreLegacyTags(t *testing.T) {
	t.Run("sets repo and kind as canonical tags", func(t *testing.T) {
		h := newHome(t)
		im := retainedStoreImport(t, h, "## Queued\n- [ ] tagged-a - Original prose (repo: app) (kind: docs)\n")
		id := im.taskID("tagged-a")
		ok(t, h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"title": "Changed prose"}}))
		record := h.records("item")[id]
		if legacyMember(record, "repo") != "app" || legacyMember(record, "kind") != "docs" {
			t.Fatalf("legacy metadata = %v", record["legacy"])
		}
		exported := h.exportMigration(im.migration["migration_id"].(string), nil)["text"].(string)
		parsed, err := legacy.Parse([]byte(exported))
		if err != nil {
			t.Fatal(err)
		}
		task := parsed.Find("tagged-a")
		if task == nil || task.Title != "Changed prose" || task.Repo != "app" || task.Kind != "docs" || !strings.Contains(exported, "(repo: app) (kind: docs)") {
			t.Fatalf("canonical tags = %q / %+v", exported, task)
		}
	})
}

// Retained upstream case inventory (test declarations, with table rows in
// their subtests); upstream: test/backends/markdown.test.ts.
// - "creates a queued task and reads it back".
// - "stamps created for non-done tasks via the injected clock".
// - "rejects a duplicate id with CONFLICT".
// - "rejects blank titles".
// - "rejects multiline titles".
// - "rejects titles that end with canonical trailing tags".
// - "rejects repo values that would inject canonical tags".
// - "rejects create links that cannot round-trip with their kind".
// - "rejects out-of-range priority values".
// - "creates and reads structured holds".
// - "rejects unsafe hold fields before rendering".
// - "rejects invalid date tags before rendering".
// - "filters list by state, repo, and kind with a true total".
// - "a single mutation only alters the targeted line".
// - "adding a body leaves all original lines intact".
// - "a no-holds backlog still renders byte-exactly after parsing".
// - "rejects writes when the backlog changed after load".
// - "replaces the body as continuation lines".
// - "adds body lines without duplicating existing lines".
// - "does not rewrite when added body lines already exist".
// - "archives the superseded body before replacing it".
// - "does not archive an unchanged body replacement".
// - "does not archive the same body twice after replacement".
// - "restores the note archive when the active backlog write fails".
// - "sets repo and kind as canonical tags".
// - "folds an added link into the prose and re-derives links".
// - "dedupes added links by exact parsed url, not substring".
// - "rejects empty added links before updating".
// - "rejects added links that cannot round-trip with their kind".
// - "throws NOT_FOUND for an unknown id".
// - "rejects blank replacement titles".
// - "rejects multiline replacement titles".
// - "rejects replacement titles that end with canonical trailing tags".
// - "rejects kind values that would split canonical tags".
// - "rejects out-of-range priority updates".
// - "moves queued -> in_flight and stamps since".
// - "carries a multi-paragraph body through start (queued -> in_flight)".
// - "carries a multi-paragraph body through done (in_flight -> done)".
// - "moves to done, records the pr link and a merged stamp".
// - "records a shorter transition link when a longer one already exists".
// - "rejects empty transition links before moving".
// - "rejects invalid transition dates before moving".
// - "addDep is idempotent (false on an existing edge)".
// - "rejects addDep when the dependency target is missing".
// - "rejects create when a dependency target is missing".
// - "removeDep returns false when there is no such edge".
// - "rejects dependency ids that cannot round-trip through markdown".
// - "rejects multiline dependency reasons before writing".
// - "rejects dependency reasons that contain edge markers before writing".
// - "rejects self-dependencies before writing".
// - "keeps N recent done tasks and archives the rest".
// - "preserves free-form done lines (does not count or archive them)".
// - "is a no-op when under the keep count".
// - "normalizes every id'd task and returns the count".
// - "creates sections on first write to an empty file".
// - "surfaces an unknown id as null from get".
// - "uses AxiError for structured failures".
// - "sees the `- [ ]` checkbox in-flight item that firstmate writes".
// - "keeps a `blocked-by: <id> - <reason>` item out of `ready`".
// - "keeps an item with a later active reason-bearing blocker out of `ready`".
// - "surfaces the item as ready once its blocker is done".
// - "preserves firstmate's lines byte-for-byte on a read-only load".
// - "preserves the blocked-by reason when the item is later mutated".
//
// Not carried:
// - "places a done task at the top of the Done section".
// Reason: spec/pool.md, Legacy import and export: an item whose state has
// changed "moves to the end of the first section of the file that maps to
// its present state".
//
// Not carried:
// - "moves a multi-paragraph body with internal blanks as one block".
// - "rolls back the destination when source removal fails".
// - "moves a linked set into an empty destination byte-exact".
// - "preserves the intra-set reason on the moved dependent".
// - "refuses when a moved item's blocker stays behind".
// - "refuses when an active dependent is left behind".
// - "aborts the set when one member id is missing (no partial move)".
// - "rolls back every created destination task when source removal fails".
// - "round-trips a whole linked set back to its origin byte-exact".
// Reason: spec/import-plan-tasks-axi.md, Excluded: "The multi-item move is
// left for a later stage" and "Nothing of it is translated at stage 2."
//
// Not carried:
// - "rejects an archive path that resolves to the live backlog".
// - "restores the archive when the active backlog write fails".
// Reason: spec/import-plan-tasks-axi.md, Local changes: "prune is not
// carried for Hive's records and survives only as archive lookup for
// import". spec/pool.md, Legacy import and export: "export-legacy ... writes
// no file."
//
// Not carried:
// - "advertises the markdown capability set".
// Reason: spec/import-plan-tasks-axi.md, Excluded: "The backend seam and the
// TypeScript data model: Hive's records are the schemas under
// spec/schemas/records/, and there is no second backend."

// The parenthesis reason row of test/commands/state.test.ts's "rejects
// missing or unsafe hold fields before mutating" belongs at the legacy
// rendering boundary: Pool holds keep exact text, and export must refuse a
// tag it cannot represent instead of substituting different text.
func TestRetainedStoreHoldParenthesesExport(t *testing.T) {
	h := newHome(t)
	im := h.importText("# Backlog\n\n## Queued\n- [ ] held-a - Hold text\n", "UTC")
	id := im.taskID("held-a")
	ok(t, h.do("hold", map[string]any{"task_id": id, "kind": "parked", "reason": "wait (blocked)"}))
	poolFiles := func() map[string]string {
		t.Helper()
		files := map[string]string{}
		err := filepath.Walk(filepath.Join(h.dir, "pool"), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = string(data)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	j, err := journal.Open(h.dir, journal.Options{Package: "pool", Clock: h.exe.Clock})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []map[string]any{
		{"zone": "UTC", "selector": map[string]any{"task_id": id}},
		{"zone": "UTC", "migration_id": im.migration["migration_id"]},
		{"zone": "UTC", "migration_id": im.migration["migration_id"], "normalize": true},
	} {
		// A refusal leaves records, request outcomes and receipts unchanged.
		// Its one diagnostic event is the journal's failure evidence.
		before := poolFiles()
		cursor, err := j.Head()
		if err != nil {
			t.Fatal(err)
		}
		resp := h.do("export-legacy", body)
		refused(t, resp, envelope.Invalid, "pool.legacy_malformed")
		if resp.Error.Detail["task_id"] != id {
			t.Fatalf("item absent from error: %+v", resp.Error)
		}
		if after := poolFiles(); !reflect.DeepEqual(before, after) {
			t.Fatal("refused export changed Pool files")
		}
		reader, err := j.Reader(cursor)
		if err != nil {
			t.Fatal(err)
		}
		event, err := reader.Next()
		if err != nil {
			t.Fatal(err)
		}
		if event == nil || event.Kind != "pool.diagnostic.recorded" {
			t.Fatalf("diagnostic event = %+v", event)
		}
		diagnostic, _ := event.Event["body"].(map[string]any)
		if diagnostic["command"] != "hive-pool" || diagnostic["status"] != "invalid" || diagnostic["code"] != "pool.legacy_malformed" || diagnostic["request_id"] != resp.RequestID {
			t.Fatalf("diagnostic names a different failure: %v", diagnostic)
		}
		if extra, err := reader.Next(); err != nil || extra != nil {
			t.Fatalf("extra journal event = %+v, %v", extra, err)
		}
	}
}

// A parenthesized reason is refused whichever of the item's holds the legacy
// form would have selected.
func TestRetainedStoreHoldParenthesesExportSecondHold(t *testing.T) {
	h := newHome(t)
	im := h.importText("# Backlog\n\n## Queued\n- [ ] held-a - Hold text\n", "UTC")
	id := im.taskID("held-a")
	ok(t, h.do("hold", map[string]any{"task_id": id, "kind": "parked", "reason": "wait"}))
	h.exe.Clock.(*clock.Fake).Advance(time.Millisecond)
	ok(t, h.do("hold", map[string]any{"task_id": id, "kind": "external", "reason": "wait (blocked)"}))
	for _, body := range []map[string]any{
		{"zone": "UTC", "selector": map[string]any{"task_id": id}},
		{"zone": "UTC", "migration_id": im.migration["migration_id"]},
		{"zone": "UTC", "migration_id": im.migration["migration_id"], "normalize": true},
	} {
		refused(t, h.do("export-legacy", body), envelope.Invalid, "pool.legacy_malformed")
	}
}
