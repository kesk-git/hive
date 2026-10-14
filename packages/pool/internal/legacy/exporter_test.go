package legacy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/pool/internal/legacy"
)

// plant writes a record of the kind into the home, as the operations that
// build it write it.
func (h *home) plant(kind, id string, rec map[string]any) {
	h.t.Helper()
	dir := filepath.Join(h.dir, "pool", "records", kind)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		h.t.Fatal(err)
	}
	data, err := canon.Encode(rec)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), append(data, '\n'), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

// leaf plants an imported item with no level in the state, under the legacy
// identifier, and returns its identity. The changes are applied to the record.
func (h *home) leaf(legacyID, title, state string, change ...func(map[string]any)) string {
	h.t.Helper()
	id := mint(h.t, ids.Task)
	rec := map[string]any{
		"schema": "pool.item", "schema_version": "1.0", "record_revision": int64(1),
		"task_id": id, "scope_level": "hive", "home_id": h.homeID, "title": title,
		"state": state, "definition": "not-required", "deliverable": "swarm", "placement_requested": "policy",
		"deps": []any{}, "links": []any{}, "generations": []any{},
		"legacy":     map[string]any{"legacy_id": legacyID, "migration_id": mint(h.t, ids.Migration)},
		"created_at": "2026-10-02T09:00:00.000Z", "updated_at": "2026-10-02T09:00:00.000Z", "writer": writer("import-legacy"),
	}
	for _, f := range change {
		f(rec)
	}
	h.plant("item", id, rec)
	return id
}

// completed marks the record done at the instant.
func completed(at string) func(map[string]any) {
	return func(rec map[string]any) { rec["completed"], rec["closed_at"] = true, at }
}

// blockedBy gives the record a blocked-by edge to the item.
func blockedBy(taskID, reason string) func(map[string]any) {
	return func(rec map[string]any) {
		edge := map[string]any{"type": "blocked-by", "task_id": taskID}
		if reason != "" {
			edge["reason"] = reason
		}
		rec["deps"] = append(rec["deps"].([]any), edge)
	}
}

// plantHold writes a hold on the item.
func (h *home) plantHold(taskID, kind, reason, state string, until map[string]any) string {
	h.t.Helper()
	id := mint(h.t, ids.Hold)
	rec := map[string]any{
		"schema": "pool.hold", "schema_version": "1.0", "record_revision": int64(1),
		"hold_id": id, "task_id": taskID, "scope_level": "hive", "home_id": h.homeID,
		"kind": kind, "reason": reason, "state": state,
		"placed_at":  "2026-10-12T09:00:00.000Z",
		"placed_by":  map[string]any{"request_id": mint(h.t, ids.Request), "caller": map[string]any{"kind": "test"}},
		"created_at": "2026-10-12T09:00:00.000Z", "updated_at": "2026-10-12T09:00:00.000Z", "writer": writer("hold"),
	}
	if state == "released" {
		rec["released_at"] = "2026-10-13T09:00:00.000Z"
		rec["released_by"] = map[string]any{"schema": "hive.receipt", "id": mint(h.t, ids.Receipt)}
	}
	if until != nil {
		rec["until"] = until
	}
	h.plant("hold", id, rec)
	return id
}

// exported is the text of an export, parsed with the grammar.
type exported struct {
	text    string
	backlog *legacy.Backlog
	body    map[string]any
}

func (h *home) export(body map[string]any) exported {
	h.t.Helper()
	before := snapshot(h.t, h.dir)
	got := ok(h.t, h.do("export-legacy", body))
	if after := snapshot(h.t, h.dir); !equal(before, after) {
		h.t.Errorf("an export wrote in the home")
	}
	text, _ := got["text"].(string)
	if got["digest"] != canon.FileDigest([]byte(text)) || got["bytes"] != int64(len(text)) {
		h.t.Errorf("the response names digest %v and %v bytes for a text of %s and %d", got["digest"], got["bytes"], canon.FileDigest([]byte(text)), len(text))
	}
	b, err := legacy.Parse([]byte(text))
	if err != nil {
		h.t.Fatal(err)
	}
	if string(b.Render()) != text {
		h.t.Errorf("the exported text does not render back from its parse")
	}
	return exported{text: text, backlog: b, body: got}
}

// task is the exported entry for the legacy identifier and the state of the
// section it stands in.
func (e exported) task(t testing.TB, legacyID string) (*legacy.Task, legacy.State) {
	t.Helper()
	for _, s := range e.backlog.Sections {
		for _, en := range s.Entries {
			if en.Task != nil && en.Task.ID == legacyID {
				return en.Task, s.State
			}
		}
	}
	t.Fatalf("the export holds no entry %s:\n%s", legacyID, e.text)
	return nil, ""
}

// Each item is exported in the section its current state maps to, a landed one
// as done, an archived one in the section its state gives, with the hold tags
// of the active holds only, the legacy words from the legacy member and no
// field of Hive's own.
func TestExportPlacesEachItemInTheSectionOfItsState(t *testing.T) {
	h := newHome(t)
	queued := h.leaf("queued-a1", "SHIP wire the gauge", "queued", func(rec map[string]any) {
		rec["legacy"].(map[string]any)["repo"] = "app"
		rec["legacy"].(map[string]any)["kind"] = "SHIP"
		rec["priority"] = int64(2)
		rec["body"] = "Body line one.\n\nBody line after a blank."
	})
	h.leaf("flying-b2", "fly the gauge", "in-flight", blockedBy(queued, "needs the wire"))
	h.leaf("done-c3", "ship the gauge https://github.com/acme/app/pull/77", "done", completed("2026-10-04T00:00:00.000Z"))
	// A Target stores no completion of its own: it has landed when the Prey
	// beneath it has, and is read as done.
	target := h.leaf("outcome-d4", "the gauge outcome", "queued", func(rec map[string]any) {
		rec["level"] = "target"
		delete(rec, "deliverable")
		delete(rec, "placement_requested")
	})
	h.leaf("work-e5", "the gauge work", "done", completed("2026-10-04T00:00:00.000Z"), func(rec map[string]any) {
		rec["level"] = "prey"
		rec["deps"] = []any{map[string]any{"type": "parent", "task_id": target}}
	})
	h.leaf("archived-done-f6", "retired after it landed", "archived", completed("2026-10-03T00:00:00.000Z"))
	h.leaf("archived-open-g7", "retired before it landed", "archived")
	held := h.leaf("held-h8", "wait for the slot", "queued")
	h.plantHold(held, "parked", "waiting for the review slot", "active", nil)
	h.plantHold(held, "external", "an earlier wait", "released", nil)
	word := h.leaf("word-i9", "wait for the word", "queued")
	h.plantHold(word, "word", "awaiting the word", "active", map[string]any{"at": "2026-11-01T00:00:00.000Z", "zone": "UTC", "date": "2026-11-01"})
	released := h.leaf("released-j1", "was held, is not", "queued")
	h.plantHold(released, "future", "a held past", "released", nil)

	e := h.export(map[string]any{"zone": "UTC"})
	if e.body["exact"] != false {
		t.Errorf("an export with no migration is not exact: %v", e.body["exact"])
	}
	for id, want := range map[string]legacy.State{
		"queued-a1": legacy.StateQueued, "flying-b2": legacy.StateInFlight, "done-c3": legacy.StateDone,
		"outcome-d4": legacy.StateDone, "work-e5": legacy.StateDone,
		"archived-done-f6": legacy.StateDone, "archived-open-g7": legacy.StateQueued,
		"held-h8": legacy.StateQueued, "word-i9": legacy.StateQueued, "released-j1": legacy.StateQueued,
	} {
		if _, got := e.task(t, id); got != want {
			t.Errorf("%s stands in the section of %s; want %s", id, got, want)
		}
	}
	t.Run("the legacy words come from the legacy member", func(t *testing.T) {
		a, _ := e.task(t, "queued-a1")
		if a.Repo != "app" || a.Kind != "ship" || a.Priority == nil || *a.Priority != 2 || a.Body != "Body line one.\n\nBody line after a blank." || a.Created != "2026-10-02" {
			t.Errorf("%+v", a)
		}
		b, _ := e.task(t, "flying-b2")
		if len(b.Deps) != 1 || b.Deps[0] != (legacy.Dep{Type: "blocked-by", ID: "queued-a1", Reason: "needs the wire"}) {
			t.Errorf("%+v", b.Deps)
		}
		c, _ := e.task(t, "done-c3")
		if c.Closed != "2026-10-04" {
			t.Errorf("%+v", c)
		}
	})
	t.Run("hold tags are written for the active holds only", func(t *testing.T) {
		if task, _ := e.task(t, "held-h8"); task.Hold == nil || task.Hold.Reason != "waiting for the review slot" || task.Hold.Kind != "parked" || task.Hold.Until != "" {
			t.Errorf("%+v", task.Hold)
		}
		if task, _ := e.task(t, "word-i9"); task.Hold == nil || task.Hold.Kind != "captain" || task.Hold.Until != "2026-11-01" {
			t.Errorf("%+v", task.Hold)
		}
		if task, _ := e.task(t, "released-j1"); task.Hold != nil {
			t.Errorf("a released hold is written back: %+v", task.Hold)
		}
		for _, word := range []string{"an earlier wait", "a held past"} {
			if strings.Contains(e.text, word) {
				t.Errorf("the text holds the reason %q of a released hold", word)
			}
		}
	})
	t.Run("no field of Hive's own is written", func(t *testing.T) {
		for _, word := range []string{"swarm", "policy", "target", "prey", "level", "deliverable", "task_", "hold_", "migr_"} {
			if strings.Contains(strings.ToLower(e.text), word) {
				t.Errorf("the text holds %q:\n%s", word, e.text)
			}
		}
	})
	t.Run("the items are listed with the form they were rendered in", func(t *testing.T) {
		list, _ := e.body["items"].([]any)
		if len(list) != 11 {
			t.Fatalf("%d items listed", len(list))
		}
		for _, v := range list {
			if v.(map[string]any)["form"] != "rendered" {
				t.Errorf("%v", v)
			}
		}
	})
}

// A date is written in the zone the request names.
func TestExportWritesDatesInTheZone(t *testing.T) {
	h := newHome(t)
	h.leaf("zoned-a1", "closed late in the evening", "done", completed("2026-10-04T20:00:00.000Z"))
	h.leaf("zoned-b2", "opened late in the evening", "queued", func(rec map[string]any) { rec["created_at"] = "2026-10-04T20:00:00.000Z" })
	e := h.export(map[string]any{"zone": "Pacific/Auckland"})
	if task, _ := e.task(t, "zoned-a1"); task.Closed != "2026-10-05" {
		t.Errorf("%+v", task)
	}
	if task, _ := e.task(t, "zoned-b2"); task.Created != "2026-10-05" {
		t.Errorf("%+v", task)
	}
}

// A selector narrows the items, and an identity that does not exist is not
// found.
func TestExportSelector(t *testing.T) {
	h := newHome(t)
	a := h.leaf("sel-a1", "one", "queued")
	h.leaf("sel-b2", "two", "in-flight")
	e := h.export(map[string]any{"zone": "UTC", "selector": map[string]any{"states": []any{"in-flight"}}})
	if list := e.body["items"].([]any); len(list) != 1 || list[0].(map[string]any)["legacy_id"] != "sel-b2" {
		t.Errorf("%v", list)
	}
	e = h.export(map[string]any{"zone": "UTC", "selector": map[string]any{"task_id": a}})
	if list := e.body["items"].([]any); len(list) != 1 || list[0].(map[string]any)["task_id"] != a {
		t.Errorf("%v", list)
	}
	resp := h.do("export-legacy", map[string]any{"zone": "UTC", "selector": map[string]any{"task_id": mint(t, ids.Task)}})
	refused(t, resp, envelope.NotFound, envelope.CodeNotFound)
}

// An item with no legacy identity is written under its own identity.
func TestExportOfAnItemThatWasNeverImported(t *testing.T) {
	h := newHome(t)
	id := h.leaf("x", "a native item", "queued", func(rec map[string]any) { delete(rec, "legacy") })
	e := h.export(map[string]any{"zone": "UTC"})
	if task, state := e.task(t, id); state != legacy.StateQueued || task.Title != "a native item" {
		t.Errorf("%+v", task)
	}
}
