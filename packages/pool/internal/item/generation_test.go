package item_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	_ "github.com/kesk-git/hive/packages/pool/internal/admission"
	_ "github.com/kesk-git/hive/packages/pool/internal/imperative"
)

// The acceptance test of the operations open-generation and close-generation
// of hive-pool (spec/pool.md, Generations). It drives the executable in
// process over a home of its own, with the Imperative and the admission of an
// item planted from the contract's record fixtures as the operations of their
// own packages leave them, and holds the two operations to the contract: one
// open generation at a time, numbered one above the last, with the place
// requested snapshotted at the open and never changed after; a queued item
// moved to in-flight in the same transaction; the five outcomes a close
// records and an item's state a close leaves as it was.

// fixture reads a valid record fixture of the contract's schemas.
func fixture(t testing.TB, schema, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas", "records_"+schema, "valid", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)
}

// atHome moves a fixture record into the home, at the hive scope.
func (h *home) atHome(rec map[string]any) {
	rec["scope_level"], rec["home_id"] = "hive", h.homeID
	delete(rec, "workspace_id")
	delete(rec, "project_id")
	delete(rec, "clone_id")
}

// revisionOf is the revision of the stored record.
func (h *home) revisionOf(kind, id string) int64 {
	h.t.Helper()
	return h.stored(kind, id)["record_revision"].(int64)
}

// imperative plants the Imperative of the item at its revision 1, naming the
// criteria given, none where nil, and the place and item revision the item
// stands at.
func (h *home) imperative(id string, criteria map[string]any) {
	h.t.Helper()
	imp := fixture(h.t, "pool.imperative", "project-swarm-claude-mac")
	h.atHome(imp)
	imp["task_id"] = id
	rev := imp["revisions"].([]any)[0].(map[string]any)
	standard := rev["task"].(map[string]any)["standard"].(map[string]any)
	if criteria == nil {
		delete(standard, "criteria")
	} else {
		standard["criteria"] = criteria
	}
	item := h.stored("item", id)
	place, ok := item["placement_requested"]
	if !ok {
		place = "policy"
	}
	rev["setup"].(map[string]any)["placement"] = map[string]any{"requested": place, "item_revision": item["record_revision"]}
	h.plant("imperative", id, imp)
}

// reviseImperative stands the Imperative at a new current revision.
func (h *home) reviseImperative(id string) {
	h.t.Helper()
	imp := h.stored("imperative", id)
	next := copyJSON(imp["revisions"].([]any)[0]).(map[string]any)
	next["revision"] = imp["current_revision"].(int64) + 1
	imp["revisions"] = append(imp["revisions"].([]any), next)
	imp["current_revision"], imp["record_revision"] = next["revision"], imp["record_revision"].(int64)+1
	h.plant("imperative", id, imp)
}

func copyJSON(v any) any {
	data, err := canon.Encode(map[string]any{"v": v})
	if err != nil {
		panic(err)
	}
	out, err := canon.Parse(data)
	if err != nil {
		panic(err)
	}
	return out.(map[string]any)["v"]
}

// admit plants an admission of the item in state admitted whose last check read
// the item and the Imperative as they stand, and returns its identity. The
// mods change the record before it is planted.
func (h *home) admit(id string, mods ...func(adm, check map[string]any)) string {
	h.t.Helper()
	return h.admitAs("admitted-mac-project", id, mods...)
}

// admitAs is admit for the admission fixture named, whose last check, where it
// has one, read the item and the Imperative as they stand.
func (h *home) admitAs(name, id string, mods ...func(adm, check map[string]any)) string {
	h.t.Helper()
	adm := fixture(h.t, "pool.admission", name)
	h.atHome(adm)
	adm["admission_id"] = mint(h.t, ids.Admission)
	adm["task_id"] = id
	adm["imperative_revision"] = h.stored("imperative", id)["current_revision"]
	var check map[string]any
	if checks := adm["checks"].([]any); len(checks) > 0 {
		check = checks[len(checks)-1].(map[string]any)
		check["item_revision"] = h.revisionOf("item", id)
		check["imperative_revision"] = adm["imperative_revision"]
	}
	for _, m := range mods {
		m(adm, check)
	}
	h.plant("admission", adm["admission_id"].(string), adm)
	return adm["admission_id"].(string)
}

// prepared is an item with the Imperative and the admission it can open
// under.
func (h *home) prepared(id string) string {
	h.t.Helper()
	h.imperative(id, nil)
	return h.admit(id)
}

// open sends open-generation under the admission.
func (h *home) open(id, admission string) *envelope.Response {
	h.t.Helper()
	return h.do("open-generation", map[string]any{"task_id": id, "admission_id": admission})
}

// opened opens the next generation under an admission that has just read the
// item and the Imperative as they stand, and returns the generation.
func (h *home) opened(id string) map[string]any {
	h.t.Helper()
	return h.ok(h.open(id, h.admit(id)))["generation"].(map[string]any)
}

// close sends close-generation with a receipt as the evidence.
func (h *home) closeGeneration(id string, generation int64, outcome string) *envelope.Response {
	h.t.Helper()
	return h.do("close-generation", map[string]any{
		"task_id": id, "generation": generation, "outcome": outcome,
		"closed_by": map[string]any{"schema": "hive.receipt", "id": mint(h.t, ids.Receipt)},
	})
}

func (h *home) generations(id string) []any {
	h.t.Helper()
	return h.stored("item", id)["generations"].([]any)
}

func hunting(title string, extra map[string]any) map[string]any {
	body := leaf(title)
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func blockedBy(ids ...string) map[string]any {
	deps := []any{}
	for _, id := range ids {
		deps = append(deps, map[string]any{"type": "blocked-by", "task_id": id})
	}
	return map[string]any{"deps": deps}
}

// A generation is opened before anything is launched: the first is numbered 1,
// moves a queued item to in-flight in the same transaction, snapshots the place
// the item requested and names the Imperative revision and the admission, with
// the receipt and the response.
func TestOpenGenerationNumbersTheFirstAndMovesAQueuedItemToInFlight(t *testing.T) {
	h := newHome(t)
	id := h.addID(hunting("A leaf", map[string]any{"placement_requested": "mac"}))
	admission := h.prepared(id)
	before := h.revisionOf("item", id)

	resp := h.open(id, admission)
	body := h.ok(resp)
	generation, record := body["generation"].(map[string]any), body["item"].(map[string]any)
	if generation["generation"] != int64(1) || generation["state"] != "open" || generation["placement_requested"] != "mac" ||
		generation["imperative_revision"] != int64(1) || generation["admission_id"] != admission || generation["opened_at"] == nil {
		t.Errorf("the generation opened is %v", generation)
	}
	for _, closed := range []string{"closed_at", "closed_by", "outcome"} {
		if _, ok := generation[closed]; ok {
			t.Errorf("an open generation carries %s", closed)
		}
	}
	if record["state"] != "in-flight" || record["record_revision"] != before+1 {
		t.Errorf("the item is %v at revision %v", record["state"], record["record_revision"])
	}
	if stored := h.stored("item", id); !equalJSON(t, stored["generations"], []any{generation}) || !equalJSON(t, stored, record) {
		t.Errorf("the stored item is %v", stored)
	}
	if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "pool.generation.opened" || resp.Receipts[0].Subject == nil ||
		resp.Receipts[0].Subject.ID != id || resp.Receipts[0].Subject.RecordRevision != before+1 || resp.Receipts[0].TaskID != id ||
		resp.Receipts[0].Scope.Generation != generation["generation"] {
		t.Errorf("the receipts are %+v", resp.Receipts)
	}
	if !strings.Contains(joinValues(h.files("journal")), `"pool.generation.opened"`) {
		t.Error("no event of kind pool.generation.opened was written")
	}
	opened, _ := h.item(id)
	if !equalJSON(t, opened["generations"], []any{generation}) {
		t.Errorf("the item reads %v", opened["generations"])
	}
}

func joinValues(m map[string]string) string {
	var sb strings.Builder
	for _, v := range m {
		sb.WriteString(v)
	}
	return sb.String()
}

// Generations are numbered one above the last, an in-flight item opens another
// after a close that left it in flight, and the place requested is read at each
// open: a later update of it touches neither an open nor a closed generation.
func TestGenerationsRiseByOneAndTheSnapshotOfThePlaceIsNeverChanged(t *testing.T) {
	h := newHome(t)
	id := h.addID(hunting("A leaf", map[string]any{"placement_requested": "mac"}))
	h.imperative(id, nil)

	first := h.opened(id)
	h.ok(h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"placement_requested": "claude-cloud-session"}}))
	if got := h.generations(id)[0].(map[string]any); got["placement_requested"] != "mac" || got["state"] != "open" {
		t.Errorf("the update of the place changed the open generation: %v", got)
	}
	if resp := h.closeGeneration(id, 1, "failed"); resp.Status != envelope.Ok {
		t.Fatalf("close: %s %+v", resp.Status, resp.Error)
	}
	if record, _ := h.item(id); record["state"] != "in-flight" {
		t.Fatalf("a failed close left the item %v", record["state"])
	}

	second := h.opened(id)
	if second["generation"] != int64(2) || second["placement_requested"] != "claude-cloud-session" {
		t.Errorf("the second generation is %v", second)
	}
	h.ok(h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"placement_requested": "policy"}}))
	h.ok(h.closeGeneration(id, 2, "stopped"))
	third := h.opened(id)
	if third["generation"] != int64(3) || third["placement_requested"] != "policy" {
		t.Errorf("the third generation is %v", third)
	}

	list := h.generations(id)
	if len(list) != 3 {
		t.Fatalf("%d generations", len(list))
	}
	for i, place := range []string{"mac", "claude-cloud-session", "policy"} {
		if g := list[i].(map[string]any); g["generation"] != int64(i+1) || g["placement_requested"] != place {
			t.Errorf("generation %d is %v", i+1, g)
		}
	}
	// The snapshot of a closed generation is what the open recorded.
	if g := list[0].(map[string]any); g["state"] != "closed" || g["opened_at"] != first["opened_at"] || g["admission_id"] != first["admission_id"] {
		t.Errorf("the first generation became %v", g)
	}
}

// At most one generation is open at a time: a second open is pool.generation_open
// and changes nothing, whatever the admission it names has read.
func TestASecondOpenGenerationIsAConflict(t *testing.T) {
	h := newHome(t)
	id := h.addID(leaf("A leaf"))
	h.imperative(id, nil)
	h.opened(id)

	// The admission has read the item as the first open left it, so that the
	// only thing standing in the way of the second open is the first.
	admission := h.admit(id)
	h.unchanged(func() {
		refused(t, h.open(id, admission), envelope.Conflict, "pool.generation_open")
	})
	if n := len(h.generations(id)); n != 1 {
		t.Errorf("%d generations", n)
	}
}

// close-generation records each of the five outcomes the caller reports from
// its own receipts, with the receipt as closed_by and the time of the close,
// writes pool.generation.closed, and changes the item's state not at all: an
// unknown outcome is an outcome and never a success, and a delivered one marks
// no completion.
func TestCloseGenerationRecordsEachOutcomeAndChangesNoState(t *testing.T) {
	for _, outcome := range []string{"delivered", "failed", "stopped", "superseded", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			h := newHome(t)
			id := h.addID(leaf("A leaf"))
			h.imperative(id, nil)
			h.opened(id)
			before := h.stored("item", id)
			closedBy := map[string]any{"schema": "hive.receipt", "id": mint(t, ids.Receipt)}

			h.clk.Advance(5 * time.Minute)
			resp := h.do("close-generation", map[string]any{"task_id": id, "generation": int64(1), "outcome": outcome, "closed_by": closedBy})
			body := h.ok(resp)
			generation, record := body["generation"].(map[string]any), body["item"].(map[string]any)
			if generation["state"] != "closed" || generation["outcome"] != outcome || !equalJSON(t, generation["closed_by"], closedBy) || generation["closed_at"] == nil {
				t.Errorf("the generation closed is %v", generation)
			}
			open := before["generations"].([]any)[0].(map[string]any)
			for _, kept := range []string{"generation", "opened_at", "placement_requested", "imperative_revision", "admission_id"} {
				if generation[kept] != open[kept] {
					t.Errorf("the close changed %s from %v to %v", kept, open[kept], generation[kept])
				}
			}
			if generation["closed_at"] == open["opened_at"] {
				t.Errorf("the generation closed at the instant it opened: %v", generation["closed_at"])
			}
			if record["state"] != "in-flight" || record["definition"] != before["definition"] {
				t.Errorf("the close left the item %v", record["state"])
			}
			for _, member := range []string{"completed", "closed_at"} {
				if _, ok := record[member]; ok {
					t.Errorf("the close of a generation as %s marked the item with %s", outcome, member)
				}
			}
			if stored := h.stored("item", id); !equalJSON(t, stored, record) || stored["record_revision"] != before["record_revision"].(int64)+1 {
				t.Errorf("the stored item is %v", stored)
			}
			if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "pool.generation.closed" || resp.Receipts[0].Subject == nil ||
				resp.Receipts[0].Subject.ID != id || resp.Receipts[0].TaskID != id || resp.Receipts[0].Scope.Generation != 1 {
				t.Errorf("the receipts are %+v", resp.Receipts)
			}
			if !strings.Contains(joinValues(h.files("journal")), `"pool.generation.closed"`) {
				t.Error("no event of kind pool.generation.closed was written")
			}
			if h.fact(id, "landed") {
				t.Errorf("a generation closed as %s landed the item", outcome)
			}
		})
	}
}

// A close names a generation of the item that is open: any other, one the item
// does not have or one closed already, is pool.generation_not_open and changes
// nothing, and an item that does not exist is not found.
func TestCloseGenerationRefusesWhatIsNotAnOpenGeneration(t *testing.T) {
	h := newHome(t)
	id := h.addID(leaf("A leaf"))
	h.imperative(id, nil)
	h.opened(id)
	h.ok(h.closeGeneration(id, 1, "failed"))
	h.opened(id)
	for name, generation := range map[string]int64{"an earlier one closed already": 1, "one never opened": 3} {
		t.Run(name, func(t *testing.T) {
			h.unchanged(func() {
				refused(t, h.closeGeneration(id, generation, "failed"), envelope.Refused, "pool.generation_not_open")
			})
		})
	}
	refused(t, h.closeGeneration(mint(t, ids.Task), 1, "failed"), envelope.NotFound, envelope.CodeNotFound)
	if g := h.generations(id); len(g) != 2 || g[1].(map[string]any)["state"] != "open" {
		t.Errorf("the generations are %v", g)
	}
}

// An open is a queued or an in-flight item's: a done or an archived item is
// refused as a transition the table does not list, with nothing written.
func TestOpenGenerationOnADoneOrArchivedItemIsNotInTheTable(t *testing.T) {
	for _, to := range []string{"done", "archived"} {
		t.Run(to, func(t *testing.T) {
			h := newHome(t)
			id := h.addID(leaf("A leaf"))
			h.imperative(id, nil)
			h.move(id, to)
			admission := h.admit(id)
			h.unchanged(func() {
				refused(t, h.open(id, admission), envelope.Refused, "pool.state_transition")
			})
			if record, _ := h.item(id); record["state"] != to || len(h.generations(id)) != 0 {
				t.Errorf("the refusal left the item %v with %d generations", record["state"], len(h.generations(id)))
			}
		})
	}
}

// Criteria the Imperative names for an item that has no effective specification
// differ from it: absent on one side and a binding on the other is
// pool.imperative_stale, and absent on both sides agrees.
func TestCriteriaNamedForAnItemWithNoEffectiveSpecificationDiffer(t *testing.T) {
	h := newHome(t)
	id := h.addID(leaf("A leaf"))
	binding := map[string]any{"specification_id": mint(t, ids.Spec), "revision": int64(1), "digest": "sha256:" + strings.Repeat("a", 64)}
	h.imperative(id, binding)
	admission := h.admit(id)
	h.unchanged(func() {
		refused(t, h.open(id, admission), envelope.Refused, "pool.imperative_stale")
	})
	h.imperative(id, nil)
	h.opened(id)
}
