package item_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
	_ "github.com/kesk-git/hive/packages/pool/internal/item"
)

// The acceptance test of the Pool's items and non-decision holds (spec/pool.md,
// Items and Holds, and the operations add, update, transition, hold and release
// with read of the kinds item and hold). It drives the executable the default
// registry describes, in process, over a home of its own, and holds the
// operations to the contract: every transition of the state table with the
// code it names, the atomic revision guard, the derived facts a container's
// holds and blockers give its items, the landing of a Target and of a Prey
// from what is beneath them, the holds the decision intake alone places, and
// a body holding a credential shape.

var start = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// home is a home with the Pool's executable over it and a clock that moves
// only when told to.
type home struct {
	t      testing.TB
	dir    string
	homeID string
	clk    *clock.Fake
	exe    *cli.Executable
}

func mint(t testing.TB, p ids.Prefix) string {
	t.Helper()
	id, err := ids.Mint(clock.System{}, p)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func newHome(t testing.TB) *home {
	t.Helper()
	h := &home{t: t, dir: filepath.Join(t.TempDir(), "home"), homeID: mint(t, ids.Home), clk: clock.NewFake(start)}
	if err := os.Mkdir(h.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := canon.Encode(map[string]any{
		"schema": "hive.home", "schema_version": "1.0", "record_revision": int64(1),
		"home_id": h.homeID, "role": "hive", "name": "A test home", "parent": map[string]any{"kind": "none"},
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "writer": writer("provision"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dir, "home.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	h.exe = entry.Executable()
	h.exe.Clock = h.clk
	return h
}

func writer(operation string) map[string]any {
	return map[string]any{"package": "pool", "command": "hive-pool", "operation": operation, "implementation_version": "test"}
}

// rev is the revision of a record a caller read, as the request names it.
type rev struct {
	schema, id string
	revision   int64
}

// do sends one request to hive-pool and returns the response as it was
// written.
func (h *home) do(operation string, body map[string]any, expected ...rev) *envelope.Response {
	h.t.Helper()
	req := map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": "hive-pool", "operation": operation, "operation_version": "1.0",
		"request_id": mint(h.t, ids.Request), "correlation_id": mint(h.t, ids.Correlation),
		"issued_at": "2026-10-14T09:00:00.000Z", "caller": map[string]any{"kind": "test"},
		"scope_level": "hive", "home_id": h.homeID,
		"authority": map[string]any{"kind": "none"}, "body": body,
	}
	if len(expected) > 0 {
		list := []any{}
		for _, e := range expected {
			list = append(list, map[string]any{"schema": e.schema, "id": e.id, "record_revision": e.revision})
		}
		req["expected_revisions"] = list
	}
	data, err := canon.Encode(req)
	if err != nil {
		h.t.Fatal(err)
	}
	var cmd *cli.Command
	for _, c := range h.exe.Commands {
		if c.Name == "hive-pool" {
			cmd = c
		}
	}
	out, err := h.exe.Handle(cli.Invocation{Command: cmd, Operation: operation, Home: h.dir, Request: append(data, '\n')})
	if err != nil {
		h.t.Fatal(err)
	}
	resp, err := envelope.DecodeResponse(out.Encoded)
	if err != nil {
		h.t.Fatal(err)
	}
	return resp
}

// ok fails the test unless the response is ok.
func (h *home) ok(resp *envelope.Response) map[string]any {
	h.t.Helper()
	if resp.Status != envelope.Ok {
		h.t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
	}
	return resp.Body
}

// refused checks the status and the code of a response that must not be ok.
func refused(t testing.TB, resp *envelope.Response, status envelope.Status, code string) {
	t.Helper()
	if resp.Status != status || resp.Error == nil || resp.Error.Code != code {
		t.Fatalf("want %s %s; got status %s, error %+v", status, code, resp.Status, resp.Error)
	}
	if resp.Body != nil {
		t.Errorf("a refusal carries a body: %v", resp.Body)
	}
}

// files is every file under the directory of the home, by slash path, with its
// bytes.
func (h *home) files(under string) map[string]string {
	h.t.Helper()
	out := map[string]string{}
	filepath.WalkDir(filepath.Join(h.dir, under), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				h.t.Fatal(rerr)
			}
			rel, _ := filepath.Rel(h.dir, path)
			out[filepath.ToSlash(rel)] = string(data)
		}
		return nil
	})
	return out
}

func same(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// unchanged runs f and fails if it changed anything under pool/.
func (h *home) unchanged(f func()) {
	h.t.Helper()
	before := h.files("pool")
	f()
	if after := h.files("pool"); !same(before, after) {
		h.t.Errorf("the Pool changed: before %v, after %v", sortedKeys(before), sortedKeys(after))
	}
}

func sortedKeys(m map[string]string) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// The bodies of an add.

func target(title string) map[string]any { return map[string]any{"title": title, "level": "target"} }

func under(level, title, parent string) map[string]any {
	return map[string]any{"title": title, "level": level, "deps": []any{map[string]any{"type": "parent", "task_id": parent}}}
}

func leaf(title string) map[string]any {
	return map[string]any{"title": title, "level": "prey", "deliverable": "swarm"}
}

// add adds the item and returns its record.
func (h *home) add(body map[string]any) map[string]any {
	h.t.Helper()
	return h.ok(h.do("add", body))["item"].(map[string]any)
}

func (h *home) addID(body map[string]any) string { return h.add(body)["task_id"].(string) }

// move transitions the item to the state and returns the record.
func (h *home) move(id, to string, extra ...map[string]any) map[string]any {
	h.t.Helper()
	body := map[string]any{"task_id": id, "to": to}
	for _, e := range extra {
		for k, v := range e {
			body[k] = v
		}
	}
	return h.ok(h.do("transition", body))["item"].(map[string]any)
}

func (h *home) hold(id, kind string) string {
	h.t.Helper()
	body := h.ok(h.do("hold", map[string]any{"task_id": id, "kind": kind, "reason": "A test reason."}))
	return body["hold"].(map[string]any)["hold_id"].(string)
}

// item reads one item with its derived facts.
func (h *home) item(id string) (map[string]any, map[string]any) {
	h.t.Helper()
	body := h.ok(h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"id": id}}))
	records, derived := body["records"].([]any), body["derived"].([]any)
	if len(records) != 1 || len(derived) != 1 {
		h.t.Fatalf("a read of one item gave %d records and %d derived", len(records), len(derived))
	}
	return records[0].(map[string]any), derived[0].(map[string]any)
}

func (h *home) fact(id, name string) bool {
	h.t.Helper()
	_, d := h.item(id)
	return d[name].(bool)
}

// plant writes a record directly, as a record of another lane or an earlier
// import would stand in the store.
func (h *home) plant(kind, id string, rec map[string]any) {
	h.t.Helper()
	data, err := canon.Encode(rec)
	if err != nil {
		h.t.Fatal(err)
	}
	dir := filepath.Join(h.dir, "pool", "records", kind)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), append(data, '\n'), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

// stored reads the record file of the item as the store holds it.
func (h *home) stored(kind, id string) map[string]any {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, "pool", "records", kind, id+".json"))
	if err != nil {
		h.t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		h.t.Fatal(err)
	}
	return v.(map[string]any)
}

// withOpenGeneration rewrites the stored item with one generation open, as
// open-generation, a later lane's operation, leaves it.
func (h *home) withOpenGeneration(id string) {
	h.t.Helper()
	rec := h.stored("item", id)
	rec["generations"] = []any{map[string]any{
		"generation": int64(1), "state": "open", "opened_at": "2026-10-14T09:00:00.000Z",
		"placement_requested": "mac", "imperative_revision": int64(1), "admission_id": mint(h.t, ids.Admission),
	}}
	h.plant("item", id, rec)
}

// legacyItem plants an imported item that has no level yet.
func (h *home) legacyItem(state string) string {
	h.t.Helper()
	id := mint(h.t, ids.Task)
	h.plant("item", id, map[string]any{
		"schema": "pool.item", "schema_version": "1.0", "record_revision": int64(1),
		"task_id": id, "scope_level": "hive", "home_id": h.homeID, "title": "An imported item",
		"state": state, "definition": "not-required", "deliverable": "swarm", "placement_requested": "policy",
		"deps": []any{}, "links": []any{}, "generations": []any{},
		"legacy":     map[string]any{"legacy_id": "T-1", "migration_id": mint(h.t, ids.Migration)},
		"created_at": "2026-10-12T09:00:00.000Z", "updated_at": "2026-10-12T09:00:00.000Z", "writer": writer("import-legacy"),
	})
	return id
}

// plantHold writes a hold of a kind other operations place.
func (h *home) plantHold(taskID, kind string, until map[string]any) string {
	h.t.Helper()
	id := mint(h.t, ids.Hold)
	rec := map[string]any{
		"schema": "pool.hold", "schema_version": "1.0", "record_revision": int64(1),
		"hold_id": id, "task_id": taskID, "scope_level": "hive", "home_id": h.homeID,
		"kind": kind, "reason": "Held for the Hive Mind.", "state": "active",
		"placed_at":  "2026-10-12T09:00:00.000Z",
		"placed_by":  map[string]any{"request_id": mint(h.t, ids.Request), "caller": map[string]any{"kind": "test"}},
		"created_at": "2026-10-12T09:00:00.000Z", "updated_at": "2026-10-12T09:00:00.000Z", "writer": writer("hold-question"),
	}
	if kind == "admission" {
		rec["admission_id"], rec["reason_code"] = mint(h.t, ids.Admission), "authority_required"
	}
	if until != nil {
		rec["until"] = until
	}
	h.plant("hold", id, rec)
	return id
}

// Every transition of the state table is allowed and takes effect as the table
// and the notes under it say, or is refused with the code it names.
func TestEveryTransitionOfTheStateTable(t *testing.T) {
	path := map[string][]string{"queued": nil, "in-flight": {"in-flight"}, "done": {"done"}, "archived": {"archived"}}
	prepared := func(h *home, from string) string {
		id := h.addID(leaf("A leaf"))
		for _, to := range path[from] {
			h.move(id, to)
		}
		return id
	}
	evidence := []any{map[string]any{"kind": "harvest", "url": "https://example.invalid/harvest/7"}}
	closed := "2026-10-14T08:30:00.000Z"
	for _, tc := range []struct{ from, to string }{
		{"queued", "in-flight"}, {"queued", "done"}, {"queued", "archived"},
		{"in-flight", "queued"}, {"in-flight", "done"}, {"in-flight", "archived"},
		{"done", "queued"}, {"done", "in-flight"}, {"done", "archived"},
	} {
		t.Run("allowed "+tc.from+" to "+tc.to, func(t *testing.T) {
			h := newHome(t)
			id := prepared(h, tc.from)
			before := h.stored("item", id)
			body := h.ok(h.do("transition", map[string]any{"task_id": id, "to": tc.to, "at": closed, "evidence": evidence, "note": "Moved."}))
			rec := body["item"].(map[string]any)
			if body["from"] != tc.from || rec["state"] != tc.to {
				t.Fatalf("from %v, state %v", body["from"], rec["state"])
			}
			if rec["record_revision"] != before["record_revision"].(int64)+1 {
				t.Errorf("the revision is %v after %v", rec["record_revision"], before["record_revision"])
			}
			if !strings.Contains(rec["body"].(string), "Moved.") {
				t.Errorf("the note is not in the body: %v", rec["body"])
			}
			switch {
			case tc.to == "done":
				if rec["completed"] != true || rec["closed_at"] != closed || len(rec["links"].([]any)) != 1 {
					t.Errorf("done: completed %v, closed_at %v, links %v", rec["completed"], rec["closed_at"], rec["links"])
				}
			case tc.from == "done" && tc.to == "archived":
				if rec["completed"] != true || rec["closed_at"] == nil {
					t.Errorf("archiving a done item loses its completion: %v", rec)
				}
			default:
				if rec["completed"] != nil || rec["closed_at"] != nil {
					t.Errorf("%s to %s leaves a completion marker: %v", tc.from, tc.to, rec)
				}
			}
			if n := len(h.ok(h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"id": id}}))["records"].([]any)); n != 1 {
				t.Errorf("the item is read back %d times", n)
			}
		})
	}
	for _, tc := range []struct{ from, to string }{
		{"queued", "queued"}, {"in-flight", "in-flight"}, {"done", "done"},
		{"archived", "queued"}, {"archived", "in-flight"}, {"archived", "done"}, {"archived", "archived"},
	} {
		t.Run("refused "+tc.from+" to "+tc.to, func(t *testing.T) {
			h := newHome(t)
			id := prepared(h, tc.from)
			h.unchanged(func() {
				refused(t, h.do("transition", map[string]any{"task_id": id, "to": tc.to}), envelope.Refused, "pool.state_transition")
			})
		})
	}
	// An archived item that was done stays landed; one that never was is
	// archived and never landed.
	t.Run("archiving keeps the completion", func(t *testing.T) {
		h := newHome(t)
		done := prepared(h, "done")
		h.move(done, "archived")
		never := prepared(h, "queued")
		h.move(never, "archived")
		if !h.fact(done, "landed") || h.fact(never, "landed") {
			t.Error("a done item archived is landed, one archived without being done is not")
		}
	})
	t.Run("a reopen clears the completion", func(t *testing.T) {
		h := newHome(t)
		id := prepared(h, "done")
		h.move(id, "queued")
		if rec, d := h.item(id); rec["completed"] != nil || rec["closed_at"] != nil || d["landed"] != false {
			t.Errorf("the reopened item is %v, %v", rec, d)
		}
	})

	guards := []struct {
		name   string
		status envelope.Status
		code   string
		setup  func(h *home) (id string, to string)
	}{
		{"an active hold stands on the item", envelope.Held, "held", func(h *home) (string, string) {
			id := h.addID(leaf("Held"))
			h.hold(id, "parked")
			return id, "in-flight"
		}},
		{"a blocker has not landed", envelope.Refused, "pool.blocked", func(h *home) (string, string) {
			blocker := h.addID(leaf("Blocker"))
			item := leaf("Blocked")
			item["deps"] = []any{map[string]any{"type": "blocked-by", "task_id": blocker}}
			return h.addID(item), "in-flight"
		}},
		{"a blocker archived without having been done", envelope.Refused, "pool.blocked", func(h *home) (string, string) {
			blocker := h.addID(leaf("Blocker"))
			item := leaf("Blocked")
			item["deps"] = []any{map[string]any{"type": "blocked-by", "task_id": blocker}}
			id := h.addID(item)
			h.move(blocker, "archived")
			return id, "in-flight"
		}},
		{"a required specification has none accepted", envelope.Refused, "pool.specification_unaccepted", func(h *home) (string, string) {
			id := h.addID(leaf("Needs a specification"))
			h.ok(h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"specification_required": true}}))
			return id, "in-flight"
		}},
		{"the item is a decision", envelope.Refused, "pool.not_dispatchable", func(h *home) (string, string) {
			return h.addID(map[string]any{"title": "A decision", "deliverable": "decision"}), "in-flight"
		}},
		{"a generation is open on the way to done", envelope.Conflict, "pool.generation_open", func(h *home) (string, string) {
			id := h.addID(leaf("Running"))
			h.move(id, "in-flight")
			h.withOpenGeneration(id)
			return id, "done"
		}},
		{"a generation is open on the way back to queued", envelope.Conflict, "pool.generation_open", func(h *home) (string, string) {
			id := h.addID(leaf("Running"))
			h.move(id, "in-flight")
			h.withOpenGeneration(id)
			return id, "queued"
		}},
		{"a generation is open on the way to archived", envelope.Conflict, "pool.generation_open", func(h *home) (string, string) {
			id := h.addID(leaf("Running"))
			h.move(id, "in-flight")
			h.withOpenGeneration(id)
			return id, "archived"
		}},
		{"a dependent that has not landed names an item that has not", envelope.Refused, "pool.dependency_active", func(h *home) (string, string) {
			blocker := h.addID(leaf("Blocker"))
			dependent := leaf("Dependent")
			dependent["deps"] = []any{map[string]any{"type": "blocked-by", "task_id": blocker}}
			h.addID(dependent)
			return blocker, "archived"
		}},
		{"a Target is done directly", envelope.Refused, "pool.derived_state", func(h *home) (string, string) {
			return h.addID(target("A Target")), "done"
		}},
		{"a Prey with a Hunt is done directly", envelope.Refused, "pool.derived_state", func(h *home) (string, string) {
			tgt := h.addID(target("A Target"))
			prey := h.addID(under("prey", "A Prey", tgt))
			h.addID(under("hunt", "A Hunt", prey))
			return prey, "done"
		}},
	}
	for _, tc := range guards {
		t.Run("refused when "+tc.name, func(t *testing.T) {
			h := newHome(t)
			id, to := tc.setup(h)
			h.unchanged(func() {
				resp := h.do("transition", map[string]any{"task_id": id, "to": to})
				if resp.Status != tc.status || resp.Error == nil || resp.Error.Code != tc.code {
					t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
				}
				if tc.code == "held" && (resp.Error.Hold == nil || resp.Error.Hold.Schema != "pool.hold") {
					t.Errorf("a held transition names the hold: %+v", resp.Error)
				}
			})
		})
	}
	t.Run("allowed once the blocker has landed, done and then archived", func(t *testing.T) {
		h := newHome(t)
		blocker := h.addID(leaf("Blocker"))
		item := leaf("Blocked")
		item["deps"] = []any{map[string]any{"type": "blocked-by", "task_id": blocker}}
		id := h.addID(item)
		h.move(blocker, "done")
		h.move(blocker, "archived")
		h.move(id, "in-flight")
	})
	t.Run("a reopened blocker has not landed again", func(t *testing.T) {
		h := newHome(t)
		blocker := h.addID(leaf("Blocker"))
		item := leaf("Blocked")
		item["deps"] = []any{map[string]any{"type": "blocked-by", "task_id": blocker}}
		id := h.addID(item)
		h.move(blocker, "done")
		if h.fact(id, "blocked") {
			t.Fatal("a blocker that is done satisfies the edge")
		}
		h.move(blocker, "queued")
		if !h.fact(id, "blocked") {
			t.Error("a reopened blocker blocks again")
		}
		refused(t, h.do("transition", map[string]any{"task_id": id, "to": "in-flight"}), envelope.Refused, "pool.blocked")
	})
	t.Run("a dependent archived first frees the item to be archived", func(t *testing.T) {
		h := newHome(t)
		blocker := h.addID(leaf("Blocker"))
		dependent := leaf("Dependent")
		dependent["deps"] = []any{map[string]any{"type": "blocked-by", "task_id": blocker}}
		dep := h.addID(dependent)
		h.move(dep, "archived")
		h.move(blocker, "archived")
	})
	t.Run("a hold survives completion and archival", func(t *testing.T) {
		h := newHome(t)
		id := h.addID(leaf("Held then done"))
		holdID := h.hold(id, "external")
		h.move(id, "done")
		h.move(id, "archived")
		hold := h.stored("hold", holdID)
		if hold["state"] != "active" {
			t.Errorf("the hold is %v", hold["state"])
		}
	})
}

// A body computed from an older read is a conflict: the first writer commits,
// the second is revision_conflict with nothing changed.
func TestAStaleExpectedRevisionIsAConflictAndChangesNothing(t *testing.T) {
	h := newHome(t)
	id := h.addID(leaf("Contested"))
	first := h.ok(h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"body": "The first writer's words."}}, rev{"pool.item", id, 1}))
	if first["item"].(map[string]any)["record_revision"] != int64(2) {
		t.Fatalf("the first update is at revision %v", first["item"].(map[string]any)["record_revision"])
	}
	for _, tc := range []struct {
		name string
		do   func() *envelope.Response
	}{
		{"update", func() *envelope.Response {
			return h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"body": "The second writer's words."}}, rev{"pool.item", id, 1})
		}},
		{"transition", func() *envelope.Response {
			return h.do("transition", map[string]any{"task_id": id, "to": "in-flight"}, rev{"pool.item", id, 1})
		}},
		{"update that changes nothing", func() *envelope.Response {
			return h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"title": "Contested"}}, rev{"pool.item", id, 1})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h.unchanged(func() { refused(t, tc.do(), envelope.Conflict, envelope.CodeRevisionConflict) })
		})
	}
	if got := h.stored("item", id)["body"]; got != "The first writer's words." {
		t.Errorf("the body is %q", got)
	}
	t.Run("release", func(t *testing.T) {
		holdID := h.hold(id, "parked")
		h.ok(h.do("release", map[string]any{"hold_id": holdID}))
		again := h.hold(id, "resource")
		h.unchanged(func() {
			refused(t, h.do("release", map[string]any{"hold_id": again}, rev{"pool.hold", again, 7}), envelope.Conflict, envelope.CodeRevisionConflict)
		})
	})
	t.Run("hold on an item read earlier", func(t *testing.T) {
		h.unchanged(func() {
			refused(t, h.do("hold", map[string]any{"task_id": id, "kind": "parked", "reason": "Late."}, rev{"pool.item", id, 1}), envelope.Conflict, envelope.CodeRevisionConflict)
		})
	})
}

// An item beneath a held or a blocked container is neither ready nor
// dispatchable: the facts and the guard read the item's ancestors.
func TestAnItemBeneathAHeldOrBlockedContainerIsNeitherReadyNorDispatchable(t *testing.T) {
	h := newHome(t)
	tgt := h.addID(target("A Target"))
	prey := h.addID(under("prey", "A Prey", tgt))
	hunt := h.addID(under("hunt", "A Hunt", prey))
	if !h.fact(hunt, "ready") || h.fact(prey, "ready") || h.fact(tgt, "ready") {
		t.Fatal("only the Hunt is ready: a Target and a Prey with a Hunt never are")
	}
	holdID := h.hold(tgt, "parked")
	_, d := h.item(hunt)
	if d["held"] != true || d["ready"] != false || !slices.Contains(d["active_holds"].([]any), any(holdID)) {
		t.Errorf("a Hunt beneath a held Target: %v", d)
	}
	resp := h.do("transition", map[string]any{"task_id": hunt, "to": "in-flight"})
	if resp.Status != envelope.Held || resp.Error == nil || resp.Error.Code != "held" || resp.Error.Hold == nil || resp.Error.Hold.ID != holdID {
		t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
	}
	h.ok(h.do("release", map[string]any{"hold_id": holdID}))
	if !h.fact(hunt, "ready") {
		t.Error("released, the Hunt is ready")
	}

	// A Target that depends on another does not start its work until that one
	// has landed.
	upstream := h.addID(target("Upstream"))
	up := h.addID(under("prey", "Its Prey", upstream))
	h.addID(under("hunt", "Its Hunt", up))
	dependent := target("Dependent")
	dependent["deps"] = []any{map[string]any{"type": "blocked-by", "task_id": upstream}}
	dep := h.addID(dependent)
	depPrey := h.addID(under("prey", "Dependent Prey", dep))
	depHunt := h.addID(under("hunt", "Dependent Hunt", depPrey))
	for _, id := range []string{dep, depPrey, depHunt} {
		if !h.fact(id, "blocked") || h.fact(id, "ready") {
			t.Errorf("%s is ready beneath a blocked Target", id)
		}
	}
	resp = h.do("transition", map[string]any{"task_id": depHunt, "to": "in-flight"})
	refused(t, resp, envelope.Refused, "pool.blocked")

	// The query's ready selector reads the same facts.
	ready := func() []string {
		body := h.ok(h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"ready": true}}))
		var out []string
		for _, r := range body["records"].([]any) {
			out = append(out, r.(map[string]any)["task_id"].(string))
		}
		return out
	}
	if got := ready(); slices.Contains(got, depHunt) || slices.Contains(got, tgt) || slices.Contains(got, prey) || !slices.Contains(got, hunt) {
		t.Errorf("the ready items are %v", got)
	}
}

// A Target and a Prey with Hunts land from the items beneath them, and a
// direct done is refused.
func TestATargetAndAPreyWithHuntsLandFromBeneath(t *testing.T) {
	h := newHome(t)
	tgt := h.addID(target("Upstream Target"))
	prey := h.addID(under("prey", "A Prey", tgt))
	first := h.addID(under("hunt", "First Hunt", prey))
	second := h.addID(under("hunt", "Second Hunt", prey))
	dependent := target("Dependent Target")
	dependent["deps"] = []any{map[string]any{"type": "blocked-by", "task_id": tgt}}
	dep := h.addID(dependent)
	depPrey := h.addID(under("prey", "Dependent Prey", dep))
	depHunt := h.addID(under("hunt", "Dependent Hunt", depPrey))

	refused(t, h.do("transition", map[string]any{"task_id": tgt, "to": "done"}), envelope.Refused, "pool.derived_state")
	refused(t, h.do("transition", map[string]any{"task_id": prey, "to": "done"}), envelope.Refused, "pool.derived_state")

	if h.fact(tgt, "landed") || h.fact(prey, "landed") {
		t.Fatal("nothing has landed yet")
	}
	h.move(first, "in-flight")
	if _, d := h.item(tgt); d["building"] != true {
		t.Errorf("a Target with an item in flight beneath it is building: %v", d)
	}
	h.move(first, "done")
	if h.fact(prey, "landed") {
		t.Error("a Prey lands when every Hunt has, not one")
	}
	h.move(second, "done")
	if !h.fact(prey, "landed") || !h.fact(tgt, "landed") {
		t.Error("completing the last Hunt lands the Prey and its Target")
	}
	if h.fact(tgt, "ready") || h.fact(prey, "ready") {
		t.Error("a Target and a Prey are never ready, landed or not")
	}
	if h.fact(dep, "blocked") || !h.fact(depHunt, "ready") {
		t.Error("the Target that depended on it is no longer blocked")
	}
	h.move(depHunt, "in-flight")

	// A Target with no Prey has never landed; a Prey with no Hunt completes
	// directly.
	bare := h.addID(target("A bare Target"))
	if h.fact(bare, "landed") {
		t.Error("a Target with no Prey has never landed")
	}
	lone := h.addID(under("prey", "A lone Prey", bare))
	h.move(lone, "done")
	if !h.fact(lone, "landed") || !h.fact(bare, "landed") {
		t.Error("a Prey with no Hunt completes directly and lands its Target")
	}
	// Reopening one Hunt beneath a landed Prey takes the landing away.
	h.move(second, "queued")
	if h.fact(prey, "landed") || h.fact(tgt, "landed") {
		t.Error("a reopened Hunt unlands the Prey and the Target")
	}
}

// hive-pool hold places parked, external, resource and future holds and refuses
// a word hold, and hive-pool release releases them and refuses the others.
func TestHoldsAndReleases(t *testing.T) {
	h := newHome(t)
	id := h.addID(leaf("A held item"))
	t.Run("a word hold is refused and nothing is written", func(t *testing.T) {
		h.unchanged(func() {
			refused(t, h.do("hold", map[string]any{"task_id": id, "kind": "word", "reason": "The Hive Mind should say."}), envelope.Invalid, envelope.CodeSchemaViolation)
		})
	})
	t.Run("the four kinds are placed", func(t *testing.T) {
		for _, kind := range []string{"parked", "external", "resource"} {
			body := h.ok(h.do("hold", map[string]any{"task_id": id, "kind": kind, "reason": "Because " + kind + "."}))
			hold := body["hold"].(map[string]any)
			if hold["kind"] != kind || hold["state"] != "active" || hold["task_id"] != id || hold["record_revision"] != int64(1) {
				t.Errorf("hold %v", hold)
			}
		}
		refused(t, h.do("hold", map[string]any{"task_id": id, "kind": "future", "reason": "No date."}), envelope.Invalid, envelope.CodeSchemaViolation)
		refused(t, h.do("hold", map[string]any{"task_id": mint(t, ids.Task), "kind": "parked", "reason": "No such item."}), envelope.NotFound, envelope.CodeNotFound)
	})
	t.Run("a future hold is inactive from its date and stays a record", func(t *testing.T) {
		item := h.addID(leaf("A dated item"))
		until := map[string]any{"at": "2026-10-14T10:00:00.000Z", "zone": "Europe/Berlin"}
		holdID := h.ok(h.do("hold", map[string]any{"task_id": item, "kind": "future", "reason": "Later.", "until": until}))["hold"].(map[string]any)["hold_id"].(string)
		if !h.fact(item, "held") || h.fact(item, "ready") {
			t.Error("the item is held before the date")
		}
		h.clk.Advance(2 * time.Hour)
		if _, d := h.item(item); d["held"] != false || d["ready"] != true || len(d["active_holds"].([]any)) != 0 {
			t.Errorf("past its date the hold holds nothing: %v", d)
		}
		body := h.ok(h.do("read", map[string]any{"kind": "hold", "selector": map[string]any{"id": holdID}}))
		if rec := body["records"].([]any)[0].(map[string]any); rec["state"] != "active" {
			t.Errorf("the dated hold is %v as a record", rec["state"])
		}
		due := h.ok(h.do("read", map[string]any{"kind": "hold", "selector": map[string]any{"active": true, "due_before": "2026-10-14T11:00:00.000Z"}}))
		if n := len(due["records"].([]any)); n != 1 {
			t.Errorf("%d holds are due", n)
		}
	})
	t.Run("a dated word hold stays in force after its date", func(t *testing.T) {
		item := h.addID(leaf("An imported hold"))
		h.plantHold(item, "word", map[string]any{"at": "2026-10-14T09:30:00.000Z", "zone": "UTC"})
		h.clk.Advance(48 * time.Hour)
		if _, d := h.item(item); d["held"] != true || d["ready"] != false {
			t.Errorf("a word hold is never made inactive by a date: %v", d)
		}
	})
	t.Run("release", func(t *testing.T) {
		item := h.addID(leaf("To be released"))
		holdID := h.hold(item, "resource")
		released := h.ok(h.do("release", map[string]any{"hold_id": holdID, "reason": "The resource came back."}))["hold"].(map[string]any)
		if released["state"] != "released" || released["released_at"] == nil || released["released_by"] == nil {
			t.Errorf("released hold %v", released)
		}
		// Releasing a released hold returns it and writes nothing.
		h.unchanged(func() {
			again := h.ok(h.do("release", map[string]any{"hold_id": holdID}))["hold"].(map[string]any)
			if again["record_revision"] != released["record_revision"] {
				t.Errorf("a second release moved the revision to %v", again["record_revision"])
			}
		})
		if h.fact(item, "held") {
			t.Error("a released hold holds nothing")
		}
		refused(t, h.do("release", map[string]any{"hold_id": mint(t, ids.Hold)}), envelope.NotFound, envelope.CodeNotFound)
	})
	t.Run("the holds another operation releases are refused", func(t *testing.T) {
		item := h.addID(leaf("Held by others"))
		for _, kind := range []string{"word", "admission"} {
			holdID := h.plantHold(item, kind, nil)
			h.unchanged(func() {
				resp := h.do("release", map[string]any{"hold_id": holdID})
				if resp.Status == envelope.Ok || resp.Error == nil {
					t.Fatalf("a %s hold was released: %s", kind, resp.Status)
				}
			})
			if h.stored("hold", holdID)["state"] != "active" {
				t.Errorf("the %s hold is no longer active", kind)
			}
		}
	})
}

// A body holding a credential shape is refused whole before anything is
// written, by add, update, transition and hold.
func TestABodyHoldingACredentialShapeWritesNothing(t *testing.T) {
	h := newHome(t)
	secret := "ghp_" + strings.Repeat("a", 36)
	id := h.addID(leaf("An item"))
	before := h.files("pool")
	for _, tc := range []struct {
		name string
		do   func() *envelope.Response
	}{
		{"add", func() *envelope.Response {
			b := leaf("Another")
			b["body"] = "The token is " + secret
			return h.do("add", b)
		}},
		{"update", func() *envelope.Response {
			return h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"add_body_lines": []any{"token: " + secret}}})
		}},
		{"transition", func() *envelope.Response {
			return h.do("transition", map[string]any{"task_id": id, "to": "done", "note": secret})
		}},
		{"hold", func() *envelope.Response {
			return h.do("hold", map[string]any{"task_id": id, "kind": "parked", "reason": secret})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refused(t, tc.do(), envelope.Invalid, envelope.CodeSecretShapeRecognized)
			if after := h.files("pool"); !same(before, after) {
				t.Errorf("a refused request left %v", sortedKeys(after))
			}
		})
	}
	// Free text that merely resembles one passes unchanged.
	h.ok(h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"body": "ghp_ is the prefix of a classic token"}}))
}

// add and update hold the parent rule and the other rules of the item.
func TestAddAndUpdateHoldTheHierarchy(t *testing.T) {
	h := newHome(t)
	tgt := h.addID(target("A Target"))
	prey := h.addID(under("prey", "A Prey", tgt))
	hunt := h.addID(under("hunt", "A Hunt", prey))
	otherPrey := h.addID(under("prey", "Another Prey", tgt))
	followUp := h.addID(leaf("A follow-up with no Target"))

	refusals := []struct {
		name string
		body map[string]any
		code string
	}{
		{"a Hunt with no parent", map[string]any{"title": "x", "level": "hunt"}, "pool.hierarchy"},
		{"a Hunt beneath a Target", under("hunt", "x", tgt), "pool.hierarchy"},
		{"a Prey beneath a Hunt", under("prey", "x", hunt), "pool.hierarchy"},
		{"a Target with a parent", under("target", "x", tgt), "pool.hierarchy"},
		{"a Prey beneath a Prey", under("prey", "x", prey), "pool.hierarchy"},
		{"a second parent edge", map[string]any{"title": "x", "level": "hunt", "deps": []any{
			map[string]any{"type": "parent", "task_id": prey}, map[string]any{"type": "parent", "task_id": otherPrey}}}, "pool.hierarchy"},
		{"a blocker that does not exist", map[string]any{"title": "x", "level": "prey", "deps": []any{
			map[string]any{"type": "blocked-by", "task_id": mint(t, ids.Task)}}}, "pool.dependency_missing"},
	}
	for _, tc := range refusals {
		t.Run("add refuses "+tc.name, func(t *testing.T) {
			h.unchanged(func() { refused(t, h.do("add", tc.body), envelope.Refused, tc.code) })
		})
	}
	// The ones the request schema refuses.
	refused(t, h.do("add", map[string]any{"title": "x"}), envelope.Invalid, envelope.CodeSchemaViolation)
	refused(t, h.do("add", map[string]any{"title": "x", "level": "prey", "wave": map[string]any{"slug": "w", "title": "W", "ordinal": int64(1)}}), envelope.Invalid, envelope.CodeSchemaViolation)
	refused(t, h.do("add", map[string]any{"title": "x", "level": "target", "schedule": map[string]any{"kind": "gate", "not_before": map[string]any{"at": "2026-11-01T00:00:00.000Z", "zone": "UTC"}}}), envelope.Invalid, envelope.CodeSchemaViolation)

	t.Run("a Hunt cannot be attached to a Prey that is done, in flight or scheduled", func(t *testing.T) {
		done := h.addID(under("prey", "Done Prey", tgt))
		h.move(done, "done")
		refused(t, h.do("add", under("hunt", "x", done)), envelope.Refused, "pool.hierarchy")
		h.move(done, "queued")
		h.addID(under("hunt", "Accepted once reopened", done))
		flying := h.addID(under("prey", "Flying Prey", tgt))
		h.move(flying, "in-flight")
		refused(t, h.do("add", under("hunt", "x", flying)), envelope.Refused, "pool.hierarchy")
		scheduled := leaf("Scheduled Prey")
		scheduled["deps"] = []any{map[string]any{"type": "parent", "task_id": tgt}}
		scheduled["schedule"] = map[string]any{"kind": "gate", "not_before": map[string]any{"at": "2026-11-01T00:00:00.000Z", "zone": "UTC"}}
		sched := h.addID(scheduled)
		refused(t, h.do("add", under("hunt", "x", sched)), envelope.Refused, "pool.hierarchy")
		h.ok(h.do("update", map[string]any{"task_id": sched, "patch": map[string]any{"clear_schedule": true}}))
		h.addID(under("hunt", "Accepted once the schedule is cleared", sched))
	})
	t.Run("update refuses a schedule on a Prey that has a Hunt, and a move of an item that has begun", func(t *testing.T) {
		refused(t, h.do("update", map[string]any{"task_id": prey, "patch": map[string]any{"schedule": map[string]any{"kind": "gate", "not_before": map[string]any{"at": "2026-11-01T00:00:00.000Z", "zone": "UTC"}}}}), envelope.Refused, "pool.hierarchy")
		refused(t, h.do("update", map[string]any{"task_id": tgt, "patch": map[string]any{"add_deps": []any{map[string]any{"type": "parent", "task_id": tgt}}}}), envelope.Refused, "pool.hierarchy")
		refused(t, h.do("update", map[string]any{"task_id": hunt, "patch": map[string]any{"remove_deps": []any{map[string]any{"type": "parent", "task_id": prey}}}}), envelope.Refused, "pool.hierarchy")
		// A queued Hunt moves between Prey in one patch.
		moved := h.ok(h.do("update", map[string]any{"task_id": hunt, "patch": map[string]any{
			"remove_deps": []any{map[string]any{"type": "parent", "task_id": prey}},
			"add_deps":    []any{map[string]any{"type": "parent", "task_id": otherPrey}}}}))
		if moved["changed"].([]any)[0] != "deps" {
			t.Errorf("changed %v", moved["changed"])
		}
		h.move(hunt, "in-flight")
		refused(t, h.do("update", map[string]any{"task_id": hunt, "patch": map[string]any{
			"remove_deps": []any{map[string]any{"type": "parent", "task_id": otherPrey}},
			"add_deps":    []any{map[string]any{"type": "parent", "task_id": prey}}}}), envelope.Refused, "pool.hierarchy")
		refused(t, h.do("update", map[string]any{"task_id": followUp, "patch": map[string]any{"level": "hunt"}}), envelope.Refused, "pool.hierarchy")
	})
	t.Run("a Prey added beneath a Target takes the definition from it", func(t *testing.T) {
		needing := target("A Target that needs a specification")
		needing["specification_required"] = true
		spec := h.add(needing)
		if spec["definition"] != "needed" {
			t.Errorf("a Target that needs a specification and has none is %v", spec["definition"])
		}
		held := h.ok(h.do("read", map[string]any{"kind": "hold", "selector": map[string]any{"task_id": spec["task_id"], "active": true}}))["records"].([]any)
		if len(held) != 1 || held[0].(map[string]any)["kind"] != "definition" {
			t.Fatalf("the Target takes a definition hold: %v", held)
		}
		beneath := h.add(under("prey", "Beneath it", spec["task_id"].(string)))
		if beneath["definition"] != "needed" {
			t.Errorf("a Prey beneath an unaccepted Target is %v", beneath["definition"])
		}
		if h.add(leaf("Needs nothing"))["definition"] != "not-required" {
			t.Error("an item that needs no specification is not-required")
		}
	})
}

// update reports the fields that changed, writes nothing for a patch that
// changes nothing, and converts an imported item to a Target as Levels says.
func TestUpdateReportsChangesAndConvertsAnImportedItem(t *testing.T) {
	h := newHome(t)
	id := h.addID(leaf("An item"))
	body := h.ok(h.do("update", map[string]any{"task_id": id, "patch": map[string]any{
		"title": "Renamed", "priority": int64(1), "add_body_lines": []any{"a line"}, "add_links": []any{map[string]any{"kind": "doc", "url": "https://example.invalid/doc"}},
	}}))
	got := body["changed"].([]any)
	for _, want := range []string{"title", "priority", "body", "links"} {
		if !slices.Contains(got, any(want)) {
			t.Errorf("changed %v lacks %s", got, want)
		}
	}
	if body["item"].(map[string]any)["record_revision"] != int64(2) {
		t.Errorf("revision %v", body["item"].(map[string]any)["record_revision"])
	}
	t.Run("a patch that changes nothing writes nothing", func(t *testing.T) {
		h.unchanged(func() {
			again := h.ok(h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"title": "Renamed", "add_body_lines": []any{"a line"}, "priority": int64(1)}}))
			if len(again["changed"].([]any)) != 0 {
				t.Errorf("changed %v", again["changed"])
			}
		})
	})
	t.Run("an imported item with no level becomes a Target", func(t *testing.T) {
		legacy := h.legacyItem("queued")
		resp := h.ok(h.do("update", map[string]any{"task_id": legacy, "patch": map[string]any{"level": "target", "wave": map[string]any{"slug": "first", "title": "First", "ordinal": int64(1)}}}))
		rec := resp["item"].(map[string]any)
		for _, gone := range []string{"deliverable", "placement_requested", "delivery"} {
			if _, ok := rec[gone]; ok {
				t.Errorf("a Target keeps %s", gone)
			}
		}
		for _, want := range []string{"level", "wave", "deliverable", "placement_requested"} {
			if !slices.Contains(resp["changed"].([]any), any(want)) {
				t.Errorf("changed %v lacks %s", resp["changed"], want)
			}
		}
		// The level of an item that has one does not change.
		h.unchanged(func() {
			refused(t, h.do("update", map[string]any{"task_id": legacy, "patch": map[string]any{"level": "prey"}}), envelope.Refused, "pool.hierarchy")
		})
	})
	t.Run("the conversion is refused unless the item is queued and has begun nothing", func(t *testing.T) {
		started := h.legacyItem("in-flight")
		h.unchanged(func() {
			refused(t, h.do("update", map[string]any{"task_id": started, "patch": map[string]any{"level": "target"}}), envelope.Refused, "pool.hierarchy")
		})
		withGeneration := h.legacyItem("queued")
		rec := h.stored("item", withGeneration)
		rec["generations"] = []any{map[string]any{
			"generation": int64(1), "state": "closed", "opened_at": "2026-10-13T09:00:00.000Z", "closed_at": "2026-10-13T10:00:00.000Z",
			"placement_requested": "mac", "imperative_revision": int64(1), "admission_id": mint(t, ids.Admission),
			"closed_by": map[string]any{"schema": "hive.receipt", "id": mint(t, ids.Receipt)}, "outcome": "failed",
		}}
		h.plant("item", withGeneration, rec)
		h.unchanged(func() {
			refused(t, h.do("update", map[string]any{"task_id": withGeneration, "patch": map[string]any{"level": "target"}}), envelope.Refused, "pool.hierarchy")
		})
	})
	t.Run("an imported item is exempt from the parent rule until its level is set", func(t *testing.T) {
		legacy := h.legacyItem("queued")
		h.ok(h.do("update", map[string]any{"task_id": legacy, "patch": map[string]any{"title": "Still unlevelled"}}))
		refused(t, h.do("update", map[string]any{"task_id": legacy, "patch": map[string]any{"level": "hunt"}}), envelope.Refused, "pool.hierarchy")
	})
}

// An item's generation history is read and kept whole through every write, and
// the effective-specification binding is read from item records alone.
func TestGenerationHistoryIsKeptWholeAndTheBindingIsReadFromItems(t *testing.T) {
	h := newHome(t)
	tgt := h.addID(target("A bound Target"))
	prey := h.addID(under("prey", "A Prey", tgt))
	hunt := h.addID(under("hunt", "A Hunt", prey))
	// The Target is bound as hive-spec accept leaves it, with no specification
	// record in the store.
	rec := h.stored("item", tgt)
	rec["specification_required"], rec["definition"] = true, "accepted"
	rec["specification"] = map[string]any{"specification_id": mint(t, ids.Spec), "revision": int64(1), "digest": "sha256:" + strings.Repeat("a", 64)}
	h.plant("item", tgt, rec)
	// The Hunt had one closed generation and one open.
	hrec := h.stored("item", hunt)
	hrec["generations"] = []any{
		map[string]any{
			"generation": int64(1), "state": "closed", "opened_at": "2026-10-13T09:00:00.000Z", "closed_at": "2026-10-13T10:00:00.000Z",
			"placement_requested": "mac", "imperative_revision": int64(1), "admission_id": mint(t, ids.Admission),
			"closed_by": map[string]any{"schema": "hive.receipt", "id": mint(t, ids.Receipt)}, "outcome": "failed",
		},
		map[string]any{
			"generation": int64(2), "state": "open", "opened_at": "2026-10-14T08:00:00.000Z",
			"placement_requested": "policy", "imperative_revision": int64(2), "admission_id": mint(t, ids.Admission),
		},
	}
	hrec["state"] = "in-flight"
	h.plant("item", hunt, hrec)

	h.ok(h.do("update", map[string]any{"task_id": hunt, "patch": map[string]any{"title": "Retitled", "placement_requested": "mac"}}))
	after := h.stored("item", hunt)
	if !equalJSON(t, after["generations"], hrec["generations"]) {
		t.Errorf("the generations changed: %v", after["generations"])
	}
	read, _ := h.item(hunt)
	if !equalJSON(t, read["generations"], hrec["generations"]) {
		t.Errorf("the read generations are %v", read["generations"])
	}
	// The Hunt needs a specification through its Target and has one, so it is
	// dispatchable: no specification record is read to know.
	h.ok(h.do("update", map[string]any{"task_id": hunt, "patch": map[string]any{"priority": int64(2)}}))
	other := h.addID(under("hunt", "Another Hunt", prey))
	if rec, _ := h.item(other); rec["definition"] != "accepted" {
		t.Errorf("a Hunt added beneath an accepted Target is %v", rec["definition"])
	}
	if len(h.files("pool/records/specification")) != 0 {
		t.Error("a specification record was written")
	}
	h.move(other, "in-flight")
}

func equalJSON(t testing.TB, a, b any) bool {
	t.Helper()
	x, err := canon.Encode(map[string]any{"v": a})
	if err != nil {
		t.Fatal(err)
	}
	y, err := canon.Encode(map[string]any{"v": b})
	if err != nil {
		t.Fatal(err)
	}
	return string(x) == string(y)
}

// Each reader answers the selector members its kind has and refuses the rest.
func TestEachReaderRefusesTheMembersThatDoNotApply(t *testing.T) {
	h := newHome(t)
	id := h.addID(leaf("An item"))
	holdID := h.hold(id, "parked")
	for _, tc := range []struct {
		name     string
		kind     string
		selector map[string]any
		ok       bool
	}{
		{"item by identity", "item", map[string]any{"id": id}, true},
		{"item by query", "item", map[string]any{"project_id": mint(t, ids.Project), "states": []any{"queued"}, "ready": true, "limit": int64(5)}, true},
		{"item with active", "item", map[string]any{"active": true}, false},
		{"item with due_before", "item", map[string]any{"due_before": "2026-10-14T11:00:00.000Z"}, false},
		{"item with task_id", "item", map[string]any{"task_id": id}, false},
		{"hold by identity", "hold", map[string]any{"id": holdID}, true},
		{"hold by query", "hold", map[string]any{"task_id": id, "states": []any{"active"}, "active": true, "due_before": "2026-10-14T11:00:00.000Z", "limit": int64(5)}, true},
		{"hold with ready", "hold", map[string]any{"ready": true}, false},
		{"hold with project_id", "hold", map[string]any{"project_id": mint(t, ids.Project)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do("read", map[string]any{"kind": tc.kind, "selector": tc.selector})
			if tc.ok {
				if resp.Status != envelope.Ok {
					t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
				}
				return
			}
			refused(t, resp, envelope.Invalid, envelope.CodeSchemaViolation)
		})
	}
	t.Run("an identity that is not there is not_found and a query that matches nothing is empty", func(t *testing.T) {
		refused(t, h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"id": mint(t, ids.Task)}}), envelope.NotFound, envelope.CodeNotFound)
		body := h.ok(h.do("read", map[string]any{"kind": "item", "selector": map[string]any{"states": []any{"archived"}}}))
		if len(body["records"].([]any)) != 0 {
			t.Errorf("records %v", body["records"])
		}
		if header := body["projection"].(map[string]any); header["complete"] != true {
			t.Errorf("projection %v", header)
		}
	})
}
