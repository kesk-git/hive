package admission_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
)

// The acceptance test of hive-admit create and check (spec/pool.md, Admission).
// It drives the executable the default registry describes, in process, over a
// home of its own with an item and its Imperative planted from the contract's
// record fixtures, and holds the two operations to the contract: create records
// the request and checks nothing; check runs the table's checks in order,
// records every finding, appends the check and writes one receipt.

const (
	taskID = "task_01jc7d8e9f0g1h2j3k4m5n6p7f"
	ruleID = "rule_01jc7d8e9f0g1h2j3k4m5n6p7e"
	words  = "  Run the export retry fix now,\n and keep the vendor client as it is.  "
)

// github is the repository binding of a project a Harvest can be published for.
var github = map[string]any{"kind": "github", "host": "github.com", "owner": "example-org", "name": "widget-service"}

// project replaces the project record of the home with one bound to the
// repository.
func (h *home) project(repository map[string]any) {
	h.t.Helper()
	rec := h.stored2("dispatch/registry/projects/" + projectID + ".json")
	rec["repository"] = repository
	h.write("dispatch/registry/projects/"+projectID+".json", rec)
}

// stored2 reads a file of the home by its path.
func (h *home) stored2(rel string) map[string]any {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, filepath.FromSlash(rel)))
	if err != nil {
		h.t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		h.t.Fatal(err)
	}
	return v.(map[string]any)
}

// mod changes the item and the one revision of its Imperative before they are
// planted.
type mod func(item, rev map[string]any)

func tree(v any) map[string]any { m, _ := v.(map[string]any); return m }

// work plants a queued Hunt at a project that needs an accepted specification
// and has one, requested on the operator's machine, with an Imperative that
// agrees with it in everything and a guess in nothing, and the project bound to
// a forge.
func (h *home) work(mods ...mod) {
	h.t.Helper()
	h.project(github)
	item := fixture(h.t, "pool.item", "hunt-in-flight-with-open-generation")
	item["state"], item["generations"], item["deps"] = "queued", []any{}, []any{}
	delete(item, "schedule")
	imp := fixture(h.t, "pool.imperative", "project-swarm-claude-mac")
	rev := imp["revisions"].([]any)[0].(map[string]any)
	tree(rev["setup"])["placement"] = map[string]any{"requested": "mac", "item_revision": item["record_revision"]}
	tree(tree(rev["task"])["standard"])["criteria"] = copyOf(item["specification"])
	for _, m := range mods {
		m(item, rev)
	}
	h.plant("item", taskID, item)
	h.plant("imperative", taskID, imp)
}

// place changes the place the item requests and the Imperative copied.
func place(p string) mod {
	return func(item, rev map[string]any) {
		item["placement_requested"] = p
		tree(tree(rev["setup"])["placement"])["requested"] = p
	}
}

func harness(name string) mod {
	return func(_, rev map[string]any) { tree(rev["setup"])["harness"] = name }
}

func sensitive(boundaries ...string) mod {
	return func(_, rev map[string]any) {
		list := []any{}
		for _, b := range boundaries {
			list = append(list, b)
		}
		tree(tree(rev["rules"])["boundary"])["sensitive"] = list
	}
}

// guess makes the section a guess, naming its question and consequence.
func guess(path ...string) mod {
	return func(_, rev map[string]any) {
		m := rev
		for _, p := range path {
			m = tree(m[p])
		}
		m["confidence"] = map[string]any{"level": "guess", "question": "Which export is meant?", "consequence": "A different worker would be changed."}
	}
}

func all(mods ...mod) mod {
	return func(item, rev map[string]any) {
		for _, m := range mods {
			m(item, rev)
		}
	}
}

// The create and check requests.
func (h *home) create(r request, revision int64) *envelope.Response {
	h.t.Helper()
	return h.send("hive-admit", "create", map[string]any{"task_id": taskID, "imperative_revision": revision, "words": words}, r)
}

func none() request { return request{authority: map[string]any{"kind": "none"}} }

func requested(value string) map[string]any {
	return map[string]any{"kind": "requested", "value": value}
}

func pushed(state string) map[string]any {
	return map[string]any{"branch": "main", "revision": "9b8a7c6d5e4f30211203f4e5d6c7b8a998877665", "pushed": state, "observed_at": "2026-10-14T08:59:00.000Z"}
}

func (h *home) check(id string, body map[string]any) *envelope.Response {
	h.t.Helper()
	if _, ok := body["admission_id"]; !ok {
		body["admission_id"] = id
	}
	if _, ok := body["placement"]; !ok {
		body["placement"] = requested(tree(tree(h.stored("imperative", taskID)["revisions"].([]any)[0])["setup"])["placement"].(map[string]any)["requested"].(string))
	}
	return h.send("hive-admit", "check", body, none())
}

// admission creates an admission against the current revision and returns its
// identity.
func (h *home) admission(r request) string {
	h.t.Helper()
	resp := h.create(r, 1)
	if resp.Status != envelope.Ok {
		h.t.Fatalf("create: status %s, error %+v", resp.Status, resp.Error)
	}
	return resp.Body["admission"].(map[string]any)["admission_id"].(string)
}

// files lists the files under a directory of the home that hold the text.
func (h *home) filesWith(dir, text string) int {
	h.t.Helper()
	n := 0
	filepath.WalkDir(filepath.Join(h.dir, dir), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				h.t.Fatal(rerr)
			}
			n += strings.Count(string(data), text)
		}
		return nil
	})
	return n
}

// lastCheck is the last check of the stored admission.
func lastCheck(rec map[string]any) map[string]any {
	checks := rec["checks"].([]any)
	return checks[len(checks)-1].(map[string]any)
}

func codes(check map[string]any) []string {
	out := []string{}
	for _, f := range check["findings"].([]any) {
		out = append(out, f.(map[string]any)["code"].(string))
	}
	return out
}

// outcome says what a check must have answered and recorded.
type outcome struct {
	status   envelope.Status
	code     string // the error code, for a refused or held response
	result   string // admitted, refused or held
	findings []string
	reason   string // the reason code of the hold a held check places
}

// wants holds the response of a check and the record it appended to to the
// outcome, and returns the check.
func wants(t *testing.T, h *home, id string, resp *envelope.Response, want outcome) map[string]any {
	t.Helper()
	if resp.Status != want.status {
		t.Fatalf("status %s, error %+v; want %s", resp.Status, resp.Error, want.status)
	}
	if want.status != envelope.Ok && (resp.Error == nil || resp.Error.Code != want.code) {
		t.Fatalf("error %+v; want code %s", resp.Error, want.code)
	}
	if want.status == envelope.Ok && resp.Error != nil {
		t.Fatalf("an admitted check carries an error %+v", resp.Error)
	}
	rec := h.stored("admission", id)
	check := lastCheck(rec)
	if check["result"] != want.result || rec["state"] != want.result {
		t.Errorf("the check's result %v and the state %v; want %s", check["result"], rec["state"], want.result)
	}
	if got := codes(check); !slices.Equal(got, want.findings) {
		t.Errorf("findings %v; want %v", got, want.findings)
	}
	if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "pool.admission."+want.result {
		t.Fatalf("receipts %+v; want exactly one pool.admission.%s", resp.Receipts, want.result)
	}
	r := resp.Receipts[0]
	if check["receipt_id"] != r.ReceiptID || check["request_id"] != resp.RequestID {
		t.Errorf("the check names receipt %v and request %v; the response %s and %s", check["receipt_id"], check["request_id"], r.ReceiptID, resp.RequestID)
	}
	if s := r.Subject; s == nil || s.Schema != "pool.admission" || s.ID != id || s.RecordRevision != rec["record_revision"] {
		t.Errorf("the receipt's subject %+v; the record is at %v", s, rec["record_revision"])
	}
	if r.Scope.TaskID != taskID {
		t.Errorf("the receipt names task %q", r.Scope.TaskID)
	}
	switch want.result {
	case "admitted":
		if got, _ := resp.Body["admission"].(map[string]any); !equalTree(got, rec) {
			t.Errorf("the response's admission is not the record: %v", got)
		}
		if got, _ := resp.Body["check"].(map[string]any); !equalTree(got, check) {
			t.Errorf("the response's check is not the last check: %v", got)
		}
		if _, ok := check["hold_id"]; ok {
			t.Errorf("an admitted check names a hold")
		}
	default:
		if resp.Body != nil {
			t.Errorf("a %s check carries a body: %v", want.result, resp.Body)
		}
	}
	if want.result == "held" {
		hold := resp.Error.Hold
		if hold == nil || hold.Schema != "pool.hold" || hold.ID != check["hold_id"] {
			t.Fatalf("error.hold %+v; the check names %v", hold, check["hold_id"])
		}
		stored := h.stored("hold", hold.ID)
		if stored["kind"] != "admission" || stored["state"] != "active" || stored["reason_code"] != want.reason ||
			stored["admission_id"] != id || stored["task_id"] != taskID || tree(stored["placed_by"])["request_id"] != resp.RequestID {
			t.Errorf("the hold %v", stored)
		}
	} else if _, ok := check["hold_id"]; ok {
		t.Errorf("a %s check names a hold", want.result)
	}
	return check
}

// create records the request as given, with who gave it, the authority it
// carried and the Imperative revision with its digest, and checks nothing.
func TestCreateRecordsTheRequestAndChecksNothing(t *testing.T) {
	h := newHome(t)
	// Everything check would refuse or hold: create looks at none of it.
	h.work(place("codex-cloud"), harness("claude"), guess("task", "object"), sensitive("spend"))
	h.plant("question", questionID, fixture(t, "pool.question", "answered-superseding"))
	imperative, item := h.stored("imperative", taskID), h.stored("item", taskID)
	before := h.records()

	req := request{authority: word(3)}
	source := map[string]any{"schema": "word.intake", "id": "rcpt_01jc7d8e9f0g1h2j3k4m5n6p7v"}
	resp := h.send("hive-admit", "create", map[string]any{"task_id": taskID, "imperative_revision": int64(1), "words": words, "source": source}, req)
	if resp.Status != envelope.Ok {
		t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
	}
	rec := resp.Body["admission"].(map[string]any)
	id := rec["admission_id"].(string)
	if !strings.HasPrefix(id, "adm_") {
		t.Errorf("admission_id %q", id)
	}
	if !equalTree(rec, h.stored("admission", id)) {
		t.Errorf("the response is not the record: %v", rec)
	}
	request := tree(rec["request"])
	auth := tree(request["authority"])
	if request["words"] != words {
		t.Errorf("the words were not kept as given: %q", request["words"])
	}
	if tree(request["caller"])["kind"] != "test" || auth["kind"] != "word" || tree(auth["ref"])["id"] != questionID || tree(auth["ref"])["record_revision"] != int64(3) {
		t.Errorf("caller %v, authority %v", request["caller"], auth)
	}
	if request["received_at"] == nil || !equalTree(request["source"], source) {
		t.Errorf("received_at %v, source %v", request["received_at"], request["source"])
	}
	revision := imperative["revisions"].([]any)[0].(map[string]any)
	if rec["imperative_revision"] != int64(1) || rec["imperative_digest"] != revision["digest"] {
		t.Errorf("bound to revision %v digest %v", rec["imperative_revision"], rec["imperative_digest"])
	}
	if rec["state"] != "created" || len(rec["checks"].([]any)) != 0 || rec["approval"] != nil || rec["record_revision"] != int64(1) {
		t.Errorf("state %v, checks %v, revision %v", rec["state"], rec["checks"], rec["record_revision"])
	}
	// The record is at the scope of the item.
	for _, k := range []string{"task_id", "scope_level", "home_id", "workspace_id", "project_id", "clone_id"} {
		if rec[k] != item[k] {
			t.Errorf("%s is %v; the item's is %v", k, rec[k], item[k])
		}
	}
	if w := tree(rec["writer"]); w["command"] != "hive-admit" || w["operation"] != "create" {
		t.Errorf("writer %v", w)
	}
	// One receipt, about the record; one new record; no event and no hold.
	if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "pool.admission.created" {
		t.Fatalf("receipts %+v", resp.Receipts)
	}
	if s := resp.Receipts[0].Subject; s == nil || s.Schema != "pool.admission" || s.ID != id || s.RecordRevision != 1 || resp.Receipts[0].Scope.TaskID != taskID {
		t.Errorf("the receipt's subject %+v, scope %+v", s, resp.Receipts[0].Scope)
	}
	var receipts, records int
	for k, v := range h.records() {
		old, ok := before[k]
		switch {
		case ok && old != v:
			t.Errorf("%s changed", k)
		case ok:
		case strings.HasPrefix(k, "pool/receipts/"):
			receipts++
		case strings.HasPrefix(k, "pool/records/admission/"):
			records++
		default:
			t.Errorf("the request added %s", k)
		}
	}
	if receipts != 1 || records != 1 {
		t.Errorf("the request added %d receipts and %d records", receipts, records)
	}
	if n := h.filesWith("journal", "pool.admission"); n != 0 {
		t.Errorf("create appended %d events", n)
	}
	// A second request is its own admission; the first is not changed.
	second := h.create(req, 1)
	if second.Status != envelope.Ok || second.Body["admission"].(map[string]any)["admission_id"] == id {
		t.Errorf("a second create: %s, %v", second.Status, second.Body)
	}
	if !equalTree(h.stored("admission", id), rec) {
		t.Errorf("a second create changed the first admission")
	}
}

// create refuses a revision that is not the Imperative's current one, and
// writes nothing.
func TestCreateRefusesARevisionThatIsNotCurrent(t *testing.T) {
	h := newHome(t)
	h.work()
	h.unchanged(func() {
		resp := h.create(none(), 2)
		if resp.Status != envelope.Conflict || resp.Error == nil || resp.Error.Code != envelope.CodeRevisionConflict || len(resp.Receipts) != 0 {
			t.Errorf("a revision after the current: status %s, error %+v, receipts %v", resp.Status, resp.Error, resp.Receipts)
		}
	})
	// The Imperative moves on: revision 1 is no longer current.
	imp := h.stored("imperative", taskID)
	second := copyRevision(imp["revisions"].([]any)[0].(map[string]any), 2)
	imp["revisions"], imp["current_revision"] = append(imp["revisions"].([]any), second), int64(2)
	h.plant("imperative", taskID, imp)
	h.unchanged(func() {
		if resp := h.create(none(), 1); resp.Status != envelope.Conflict || resp.Error.Code != envelope.CodeRevisionConflict {
			t.Errorf("revision 1 after revision 2: status %s, error %+v", resp.Status, resp.Error)
		}
	})
	if resp := h.create(none(), 2); resp.Status != envelope.Ok || resp.Body["admission"].(map[string]any)["imperative_revision"] != int64(2) {
		t.Errorf("revision 2: status %s, error %+v", resp.Status, resp.Error)
	}
}

func copyOf(v any) any {
	data, _ := canon.Encode(v)
	out, _ := canon.Parse(data)
	return out
}

func copyRevision(rev map[string]any, n int64) map[string]any {
	data, _ := canon.Encode(rev)
	v, _ := canon.Parse(data)
	out := v.(map[string]any)
	out["revision"] = n
	out["digest"] = "sha256:" + strings.Repeat("f", 64)
	return out
}

// create answers not_found for an item or an Imperative the Pool does not hold,
// and scope_mismatch outside the scope of the request.
func TestCreateNeedsTheItemAndItsImperative(t *testing.T) {
	h := newHome(t)
	h.unchanged(func() {
		if resp := h.create(none(), 1); resp.Status != envelope.NotFound {
			t.Errorf("no item: status %s, error %+v", resp.Status, resp.Error)
		}
	})
	h.work()
	if err := os.Remove(filepath.Join(h.dir, "pool", "records", "imperative", taskID+".json")); err != nil {
		t.Fatal(err)
	}
	h.unchanged(func() {
		if resp := h.create(none(), 1); resp.Status != envelope.NotFound {
			t.Errorf("no Imperative: status %s, error %+v", resp.Status, resp.Error)
		}
	})
	h.work()
	other := request{authority: map[string]any{"kind": "none"}, scope: map[string]any{"scope_level": "project", "home_id": homeID, "workspace_id": workspaceID, "project_id": "proj_01jc7d8e9f0g1h2j3k4m5n6p7z"}}
	h.unchanged(func() {
		if resp := h.create(other, 1); resp.Status == envelope.Ok {
			t.Errorf("a request at another project created an admission")
		}
	})
}

// A check that finds nothing wrong admits: it appends the check, writes the
// pool.admission.admitted receipt, answers ok with the check, and lets the
// admission be read in state admitted.
func TestCheckAdmitsAndRecordsTheCheck(t *testing.T) {
	h := newHome(t)
	h.work()
	id := h.admission(none())
	created := h.stored("admission", id)
	receipts := h.filesWith("pool/receipts", `"kind":"pool.admission.`)

	resp := h.check(id, map[string]any{"rules_digest": "sha256:" + strings.Repeat("d", 64)})
	check := wants(t, h, id, resp, outcome{status: envelope.Ok, result: "admitted", findings: []string{}})
	item := h.stored("item", taskID)
	if check["sequence"] != int64(1) || check["imperative_revision"] != int64(1) || check["item_revision"] != item["record_revision"] {
		t.Errorf("sequence %v, Imperative revision %v, item revision %v", check["sequence"], check["imperative_revision"], check["item_revision"])
	}
	if !equalTree(check["placement"], requested("mac")) || !equalTree(check["specification"], item["specification"]) || check["rules_digest"] != "sha256:"+strings.Repeat("d", 64) {
		t.Errorf("placement %v, specification %v, rules_digest %v", check["placement"], check["specification"], check["rules_digest"])
	}
	if check["checked_at"] == nil {
		t.Errorf("no checked_at")
	}
	rec := h.stored("admission", id)
	if rec["record_revision"] != int64(2) {
		t.Errorf("record_revision %v", rec["record_revision"])
	}
	// Nothing but the check, the state and the write's own members changed.
	for k, v := range created {
		switch k {
		case "record_revision", "updated_at", "writer", "checks", "state":
		default:
			if !equalTree(v, rec[k]) {
				t.Errorf("%s changed: %v became %v", k, v, rec[k])
			}
		}
	}
	// One new receipt of the check, one event for it, no hold.
	if n := h.filesWith("pool/receipts", `"kind":"pool.admission.`); n != receipts+1 {
		t.Errorf("%d admission receipts after %d", n, receipts)
	}
	if n := h.filesWith("journal", `"pool.admission.checked"`); n != 1 {
		t.Errorf("%d check events", n)
	}
	if holds, _ := os.ReadDir(filepath.Join(h.dir, "pool", "records", "hold")); len(holds) != 0 {
		t.Errorf("an admitting check placed %d holds", len(holds))
	}

	// A check may be run as often as a launch path needs; the earlier check
	// is never changed, and each appends its own.
	again := h.check(id, map[string]any{})
	wants(t, h, id, again, outcome{status: envelope.Ok, result: "admitted", findings: []string{}})
	checks := h.stored("admission", id)["checks"].([]any)
	if len(checks) != 2 || !equalTree(checks[0], check) || checks[1].(map[string]any)["sequence"] != int64(2) {
		t.Errorf("checks %v", checks)
	}
}

// Every row of the table, with the status, the code and the receipt it ends in.
func TestCheckRunsTheTable(t *testing.T) {
	vmode := func(v string) mod {
		return func(_, rev map[string]any) { tree(tree(rev["definition_of_done"])["output"])["validation"] = v }
	}
	pmode := func(v string) mod {
		return func(_, rev map[string]any) { tree(tree(rev["definition_of_done"])["output"])["publication"] = v }
	}
	cloud := all(place("claude-cloud-session"), harness("claude"))
	for _, tc := range []struct {
		name    string
		mods    mod
		project map[string]any
		body    map[string]any
		want    outcome
	}{
		{name: "nothing wrong on the operator's machine", want: outcome{status: envelope.Ok, result: "admitted", findings: []string{}}},
		{name: "nothing wrong in a cloud session with a pushed branch", mods: cloud, body: map[string]any{"source_branch": pushed("true")},
			want: outcome{status: envelope.Ok, result: "admitted", findings: []string{}}},
		{name: "a guess in the Object", mods: guess("task", "object"),
			want: outcome{status: envelope.Held, code: "held", result: "held", findings: []string{"pool.admission_guess"}, reason: "field_guess"}},
		{name: "a guess in the Standard", mods: guess("task", "standard"),
			want: outcome{status: envelope.Held, code: "held", result: "held", findings: []string{"pool.admission_guess"}, reason: "field_guess"}},
		{name: "a guess in the Output", mods: guess("definition_of_done", "output"),
			want: outcome{status: envelope.Held, code: "held", result: "held", findings: []string{"pool.admission_guess"}, reason: "field_guess"}},
		{name: "a guess in the Boundary", mods: guess("rules", "boundary"),
			want: outcome{status: envelope.Held, code: "held", result: "held", findings: []string{"pool.admission_guess"}, reason: "field_guess"}},
		{name: "an Imperative whose place is not the item's", mods: func(item, _ map[string]any) { item["placement_requested"] = "policy" },
			body: map[string]any{"placement": requested("policy")},
			want: outcome{status: envelope.Refused, code: "pool.imperative_stale", result: "refused", findings: []string{"pool.imperative_stale"}}},
		{name: "a harness that contradicts the place", mods: all(place("claude-cloud-session"), harness("codex")), body: map[string]any{"source_branch": pushed("true")},
			want: outcome{status: envelope.Refused, code: "pool.setup_contradiction", result: "refused", findings: []string{"pool.setup_contradiction"}}},
		{name: "a harness left to the resolver does not contradict", mods: all(place("claude-cloud-session"), harness("policy")), body: map[string]any{"source_branch": pushed("true")},
			want: outcome{status: envelope.Ok, result: "admitted", findings: []string{}}},
		{name: "a specification the item needs and has not got", mods: func(item, rev map[string]any) {
			delete(item, "specification")
			item["definition"] = "needed"
			delete(tree(tree(rev["task"])["standard"]), "criteria")
		}, want: outcome{status: envelope.Refused, code: "pool.specification_unaccepted", result: "refused", findings: []string{"pool.specification_unaccepted"}}},
		{name: "a Standard that names another revision", mods: func(_, rev map[string]any) {
			tree(tree(tree(rev["task"])["standard"])["criteria"])["revision"] = int64(1)
		}, want: outcome{status: envelope.Refused, code: "pool.criteria_mismatch", result: "refused", findings: []string{"pool.criteria_mismatch"}}},
		{name: "a Standard that names none where the item has one", mods: func(_, rev map[string]any) {
			delete(tree(tree(rev["task"])["standard"]), "criteria")
		}, want: outcome{status: envelope.Refused, code: "pool.criteria_mismatch", result: "refused", findings: []string{"pool.criteria_mismatch"}}},
		{name: "an item that needs no specification and has none", mods: func(item, rev map[string]any) {
			delete(item, "specification")
			item["specification_required"] = false
			delete(tree(tree(rev["task"])["standard"]), "criteria")
		}, want: outcome{status: envelope.Ok, result: "admitted", findings: []string{}}},
		{name: "Codex Cloud", mods: all(place("codex-cloud"), harness("codex")), body: map[string]any{"source_branch": pushed("true")},
			want: outcome{status: envelope.Refused, code: "pool.placement_parked", result: "refused", findings: []string{"pool.placement_parked"}}},
		{name: "a cloud session for a project with no forge", mods: cloud, project: map[string]any{"kind": "local-only"}, body: map[string]any{"source_branch": pushed("true")},
			want: outcome{status: envelope.Refused, code: "pool.placement_local_only", result: "refused", findings: []string{"pool.placement_local_only", "pool.publication_needs_forge"}}},
		{name: "a local place for a project with no forge", mods: all(pmode("local-only"), func(item, _ map[string]any) { tree(item["delivery"])["publication"] = "local-only" }),
			project: map[string]any{"kind": "local-only"}, want: outcome{status: envelope.Ok, result: "admitted", findings: []string{}}},
		{name: "a cloud session with no branch evidence", mods: cloud,
			want: outcome{status: envelope.Held, code: "held", result: "held", findings: []string{"source_branch_not_pushed"}, reason: "source_branch_not_pushed"}},
		{name: "a cloud session whose branch is not pushed", mods: cloud, body: map[string]any{"source_branch": pushed("false")},
			want: outcome{status: envelope.Held, code: "held", result: "held", findings: []string{"source_branch_not_pushed"}, reason: "source_branch_not_pushed"}},
		{name: "a cloud session whose branch is not known to be pushed", mods: cloud, body: map[string]any{"source_branch": pushed("unknown")},
			want: outcome{status: envelope.Held, code: "held", result: "held", findings: []string{"source_branch_not_pushed"}, reason: "source_branch_not_pushed"}},
		{name: "a sensitive boundary with no authority", mods: sensitive("spend"),
			want: outcome{status: envelope.Held, code: "held", result: "held", findings: []string{"pool.authority_required"}, reason: "authority_required"}},
		{name: "a validation that differs from the item's", mods: vmode("direct"),
			want: outcome{status: envelope.Refused, code: "pool.delivery_mismatch", result: "refused", findings: []string{"pool.delivery_mismatch"}}},
		{name: "a publication that differs from the item's", mods: pmode("local-only"),
			want: outcome{status: envelope.Refused, code: "pool.delivery_mismatch", result: "refused", findings: []string{"pool.delivery_mismatch"}}},
		{name: "a Harvest asked of a project with no forge", project: map[string]any{"kind": "local-only"},
			want: outcome{status: envelope.Refused, code: "pool.publication_needs_forge", result: "refused", findings: []string{"pool.publication_needs_forge"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHome(t)
			mods := []mod{}
			if tc.mods != nil {
				mods = append(mods, tc.mods)
			}
			h.work(mods...)
			if tc.project != nil {
				h.project(tc.project)
			}
			id := h.admission(none())
			body := map[string]any{}
			for k, v := range tc.body {
				body[k] = v
			}
			wants(t, h, id, h.check(id, body), tc.want)
		})
	}
}

// A finding that is a guess names the field, its question and its consequence.
func TestAGuessIsFoundNamingTheFieldItsQuestionAndItsConsequence(t *testing.T) {
	for field, path := range map[string][]string{
		"task.object": {"task", "object"}, "task.standard": {"task", "standard"},
		"definition_of_done.output": {"definition_of_done", "output"}, "rules.boundary": {"rules", "boundary"},
	} {
		t.Run(field, func(t *testing.T) {
			h := newHome(t)
			h.work(guess(path...))
			id := h.admission(none())
			wants(t, h, id, h.check(id, map[string]any{}), outcome{status: envelope.Held, code: "held", result: "held", findings: []string{"pool.admission_guess"}, reason: "field_guess"})
			f := lastCheck(h.stored("admission", id))["findings"].([]any)[0].(map[string]any)
			if f["field"] != field || !strings.Contains(f["message"].(string), "Which export is meant?") || f["consequence"] != "A different worker would be changed." {
				t.Errorf("finding %v", f)
			}
		})
	}
}

// The first check in the table that does not pass fixes the result, and every
// finding of every check is recorded, in the table's order.
func TestTheFirstFailureFixesTheResultAndEveryFindingIsRecorded(t *testing.T) {
	t.Run("a hold before a refusal", func(t *testing.T) {
		h := newHome(t)
		h.work(all(guess("task", "object"), sensitive("deletion"), func(_, rev map[string]any) {
			tree(tree(tree(rev["task"])["standard"])["criteria"])["revision"] = int64(1)
		}))
		id := h.admission(none())
		wants(t, h, id, h.check(id, map[string]any{}), outcome{
			status: envelope.Held, code: "held", result: "held", reason: "field_guess",
			findings: []string{"pool.admission_guess", "pool.criteria_mismatch", "pool.authority_required"},
		})
	})
	t.Run("a refusal before a hold", func(t *testing.T) {
		h := newHome(t)
		h.work(all(sensitive("security"), func(item, _ map[string]any) { item["placement_requested"] = "policy" }))
		id := h.admission(none())
		wants(t, h, id, h.check(id, map[string]any{"placement": requested("policy")}), outcome{
			status: envelope.Refused, code: "pool.imperative_stale", result: "refused",
			findings: []string{"pool.imperative_stale", "pool.authority_required"},
		})
		if holds, _ := os.ReadDir(filepath.Join(h.dir, "pool", "records", "hold")); len(holds) != 0 {
			t.Errorf("a refusal placed %d holds", len(holds))
		}
	})
	t.Run("an Imperative revision that is no longer current", func(t *testing.T) {
		h := newHome(t)
		h.work()
		id := h.admission(none())
		imp := h.stored("imperative", taskID)
		imp["revisions"], imp["current_revision"] = append(imp["revisions"].([]any), copyRevision(imp["revisions"].([]any)[0].(map[string]any), 2)), int64(2)
		h.plant("imperative", taskID, imp)
		wants(t, h, id, h.check(id, map[string]any{}), outcome{status: envelope.Refused, code: "pool.imperative_stale", result: "refused", findings: []string{"pool.imperative_stale"}})
	})
	t.Run("a place the item no longer requests", func(t *testing.T) {
		h := newHome(t)
		h.work()
		id := h.admission(none())
		item := h.stored("item", taskID)
		item["placement_requested"], item["record_revision"] = "claude-cloud-session", int64(4)
		h.plant("item", taskID, item)
		wants(t, h, id, h.check(id, map[string]any{"placement": requested("claude-cloud-session"), "source_branch": pushed("true")}), outcome{
			status: envelope.Refused, code: "pool.imperative_stale", result: "refused", findings: []string{"pool.imperative_stale"}})
		if c := lastCheck(h.stored("admission", id)); c["item_revision"] != int64(4) {
			t.Errorf("the check read item revision %v", c["item_revision"])
		}
	})
}

// A place the policy resolved is checked and recorded with the receipt that
// resolved it; a resolved place with none is invalid.
func TestAResolvedPlaceIsCheckedAndRecordedWithItsReceipt(t *testing.T) {
	resolved := func(value string) map[string]any {
		return map[string]any{"kind": "resolved", "value": value,
			"resolved_by": map[string]any{"schema": "dispatch.resolution", "id": "rcpt_01jc7d8e9f0g1h2j3k4m5n6p7x"}}
	}
	h := newHome(t)
	h.work(place("policy"))
	id := h.admission(none())
	h.unchanged(func() {
		resp := h.send("hive-admit", "check", map[string]any{"admission_id": id, "placement": map[string]any{"kind": "resolved", "value": "mac"}}, none())
		if resp.Status != envelope.Invalid {
			t.Errorf("a resolved place with no receipt: status %s, error %+v", resp.Status, resp.Error)
		}
	})
	check := wants(t, h, id, h.check(id, map[string]any{"placement": resolved("codex-cloud"), "source_branch": pushed("true")}), outcome{
		status: envelope.Refused, code: "pool.placement_parked", result: "refused", findings: []string{"pool.placement_parked"}})
	if !equalTree(check["placement"], resolved("codex-cloud")) {
		t.Errorf("placement %v", check["placement"])
	}
	check = wants(t, h, id, h.check(id, map[string]any{"placement": resolved("mac")}), outcome{status: envelope.Ok, result: "admitted", findings: []string{}})
	if !equalTree(check["placement"], resolved("mac")) {
		t.Errorf("placement %v", check["placement"])
	}
}

// Nothing admits codex-cloud: not a branch pushed, an approval on the record, a
// rule as the request's authority, nor the place resolved by policy.
func TestNothingAdmitsCodexCloud(t *testing.T) {
	h := newHome(t)
	h.work(all(place("codex-cloud"), harness("codex"), sensitive("spend")))
	h.plant("question", questionID, fixture(t, "pool.question", "answered-superseding"))
	rule := fixture(t, "pool.rule_v2", "minimal")
	h.plant("rule", rule["rule_id"].(string), rule)
	id := h.admission(request{authority: authority("rule", "pool.rule", rule["rule_id"].(string), 1)})
	if resp := h.approve(request{authority: word(3)}, id); resp.Status != envelope.Ok {
		t.Fatalf("approve: %s %+v", resp.Status, resp.Error)
	}
	for _, p := range []map[string]any{requested("codex-cloud"), {"kind": "resolved", "value": "codex-cloud",
		"resolved_by": map[string]any{"schema": "dispatch.resolution", "id": "rcpt_01jc7d8e9f0g1h2j3k4m5n6p7x"}}} {
		wants(t, h, id, h.check(id, map[string]any{"placement": p, "source_branch": pushed("true")}), outcome{
			status: envelope.Refused, code: "pool.placement_parked", result: "refused", findings: []string{"pool.placement_parked"}})
	}
}

// A held check is lifted by a later check with new evidence; the earlier check
// is never changed, and the hold it placed is released by the receipt that
// admitted. A held check for a reason an active hold of the admission already
// holds names that hold and places none.
func TestALaterCheckWithNewEvidenceAdmitsWhatAnEarlierOneHeld(t *testing.T) {
	h := newHome(t)
	h.work(all(place("claude-cloud-session"), harness("claude")))
	id := h.admission(none())

	first := h.check(id, map[string]any{})
	held := wants(t, h, id, first, outcome{status: envelope.Held, code: "held", result: "held", findings: []string{"source_branch_not_pushed"}, reason: "source_branch_not_pushed"})
	holdID := first.Error.Hold.ID
	// Run again: held for the same reason, the same hold named, none placed.
	again := h.check(id, map[string]any{"source_branch": pushed("unknown")})
	wants(t, h, id, again, outcome{status: envelope.Held, code: "held", result: "held", findings: []string{"source_branch_not_pushed"}, reason: "source_branch_not_pushed"})
	if again.Error.Hold.ID != holdID {
		t.Errorf("a second held check for the same reason names %s, not the first hold %s", again.Error.Hold.ID, holdID)
	}
	if n := h.holdCount(); n != 1 {
		t.Errorf("%d holds after two checks held for one reason", n)
	}
	if stored := h.stored("hold", holdID); stored["record_revision"] != int64(1) {
		t.Errorf("the reused hold was rewritten: %v", stored)
	}

	evidence := map[string]any{"source_branch": pushed("true")}
	third := h.check(id, evidence)
	admitted := wants(t, h, id, third, outcome{status: envelope.Ok, result: "admitted", findings: []string{}})
	rec := h.stored("admission", id)
	checks := rec["checks"].([]any)
	if len(checks) != 3 || !equalTree(checks[0], held) || checks[2].(map[string]any)["sequence"] != int64(3) || !equalTree(checks[2], admitted) {
		t.Errorf("checks %v", checks)
	}
	if !equalTree(tree(admitted["source_branch"]), evidence["source_branch"]) {
		t.Errorf("the evidence is not recorded with the check: %v", admitted["source_branch"])
	}
	stored := h.stored("hold", holdID)
	by := tree(stored["released_by"])
	if stored["state"] != "released" || stored["released_at"] == nil || by["schema"] != "hive.receipt" || by["id"] != third.Receipts[0].ReceiptID {
		t.Errorf("hold %s: %v", holdID, stored)
	}
	// One placement and one release, each with the check's receipt as its basis.
	h.holdEvents(t, "pool.hold.placed", first.Receipts[0].ReceiptID, 1)
	h.holdEvents(t, "pool.hold.released", third.Receipts[0].ReceiptID, 1)
	if n := h.filesWith("journal", `"pool.hold.placed"`) + h.filesWith("journal", `"pool.hold.released"`); n != 2 {
		t.Errorf("%d hold events in all", n)
	}
}

// holdCount is the number of hold records of the Pool.
func (h *home) holdCount() int {
	h.t.Helper()
	holds, _ := os.ReadDir(filepath.Join(h.dir, "pool", "records", "hold"))
	return len(holds)
}

// holdEvents checks that the journal holds n events of the kind whose basis is
// the receipt.
func (h *home) holdEvents(t *testing.T, kind, receipt string, n int) {
	t.Helper()
	got := 0
	filepath.WalkDir(filepath.Join(h.dir, "journal"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatal(rerr)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, `"kind":"`+kind+`"`) && strings.Contains(line, `"id":"`+receipt+`"`) && strings.Contains(line, `"task_id":"`+taskID+`"`) {
				got++
			}
		}
		return nil
	})
	if got != n {
		t.Errorf("%d %s events on receipt %s; want %d", got, kind, receipt, n)
	}
}

// A held check for a reason no active hold of the admission holds places a
// hold of its own; a refused check places none and releases none.
func TestHoldsFollowTheReasonOfTheCheck(t *testing.T) {
	h := newHome(t)
	h.work(all(place("claude-cloud-session"), harness("claude"), sensitive("spend")))
	h.plant("question", questionID, fixture(t, "pool.question", "answered-superseding"))
	id := h.admission(none())

	first := h.check(id, map[string]any{})
	wants(t, h, id, first, outcome{status: envelope.Held, code: "held", result: "held",
		findings: []string{"source_branch_not_pushed", "pool.authority_required"}, reason: "source_branch_not_pushed"})
	if n := h.holdCount(); n != 1 {
		t.Fatalf("%d holds", n)
	}
	// The branch is shown pushed: held for another reason, a second hold.
	second := h.check(id, map[string]any{"source_branch": pushed("true")})
	wants(t, h, id, second, outcome{status: envelope.Held, code: "held", result: "held",
		findings: []string{"pool.authority_required"}, reason: "authority_required"})
	if second.Error.Hold.ID == first.Error.Hold.ID || h.holdCount() != 2 {
		t.Errorf("a new reason names %s; %d holds", second.Error.Hold.ID, h.holdCount())
	}
	h.holdEvents(t, "pool.hold.placed", second.Receipts[0].ReceiptID, 1)
	// A refusal places none and releases none.
	item := h.stored("item", taskID)
	item["placement_requested"], item["record_revision"] = "policy", int64(4)
	h.plant("item", taskID, item)
	refusal := h.check(id, map[string]any{"placement": requested("policy")})
	wants(t, h, id, refusal, outcome{status: envelope.Refused, code: "pool.imperative_stale", result: "refused",
		findings: []string{"pool.imperative_stale", "pool.authority_required"}})
	if h.holdCount() != 2 {
		t.Errorf("a refusal changed the holds: %d", h.holdCount())
	}
	for _, hold := range []string{first.Error.Hold.ID, second.Error.Hold.ID} {
		if stored := h.stored("hold", hold); stored["state"] != "active" || stored["record_revision"] != int64(1) {
			t.Errorf("a refusal changed hold %s: %v", hold, stored)
		}
	}
	if n := h.filesWith("journal", `"pool.hold.placed"`) + h.filesWith("journal", `"pool.hold.released"`); n != 2 {
		t.Errorf("%d hold events after a refusal", n)
	}
	// An admitting check releases both.
	item["placement_requested"], item["record_revision"] = "claude-cloud-session", int64(5)
	h.plant("item", taskID, item)
	if resp := h.approve(request{authority: word(3)}, id); resp.Status != envelope.Ok {
		t.Fatalf("approve: %s %+v", resp.Status, resp.Error)
	}
	admit := h.check(id, map[string]any{"placement": requested("claude-cloud-session"), "source_branch": pushed("true")})
	wants(t, h, id, admit, outcome{status: envelope.Ok, result: "admitted", findings: []string{}})
	for _, hold := range []string{first.Error.Hold.ID, second.Error.Hold.ID} {
		if stored := h.stored("hold", hold); stored["state"] != "released" || tree(stored["released_by"])["id"] != admit.Receipts[0].ReceiptID {
			t.Errorf("hold %s after the admitting check: %v", hold, stored)
		}
	}
	h.holdEvents(t, "pool.hold.released", admit.Receipts[0].ReceiptID, 2)
}

// The evidence shows the source branch pushed only when it is Setup's branch,
// pushed, and at Setup's revision where Setup gives one; anything else holds,
// and the evidence is recorded as given.
func TestBranchEvidenceMustBeSetupsBranchAndRevision(t *testing.T) {
	held := outcome{status: envelope.Held, code: "held", result: "held", findings: []string{"source_branch_not_pushed"}, reason: "source_branch_not_pushed"}
	for name, tc := range map[string]struct {
		evidence func(map[string]any)
		want     outcome
	}{
		"Setup's branch and revision, pushed": {func(map[string]any) {}, outcome{status: envelope.Ok, result: "admitted", findings: []string{}}},
		"another branch":                      {func(e map[string]any) { e["branch"] = "hive/another" }, held},
		"another revision":                    {func(e map[string]any) { e["revision"] = "3f5a9c1e7b2d4860a1c3e5f708192a3b4c5d6e7f" }, held},
		"no revision where Setup gives one":   {func(e map[string]any) { delete(e, "revision") }, held},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHome(t)
			h.work(all(place("claude-cloud-session"), harness("claude")))
			id := h.admission(none())
			evidence := pushed("true")
			tc.evidence(evidence)
			check := wants(t, h, id, h.check(id, map[string]any{"source_branch": evidence}), tc.want)
			if !equalTree(check["source_branch"], evidence) {
				t.Errorf("the evidence is not recorded as given: %v", check["source_branch"])
			}
		})
	}
	t.Run("Setup gives no revision", func(t *testing.T) {
		h := newHome(t)
		h.work(all(place("claude-cloud-session"), harness("claude"), func(_, rev map[string]any) { delete(tree(rev["setup"]), "source_revision") }))
		id := h.admission(none())
		evidence := pushed("true")
		evidence["revision"] = "3f5a9c1e7b2d4860a1c3e5f708192a3b4c5d6e7f"
		wants(t, h, id, h.check(id, map[string]any{"source_branch": evidence}), outcome{status: envelope.Ok, result: "admitted", findings: []string{}})
	})
}

// A sensitive boundary needs explicit authority: an approval on the record, a
// word that stands as the request's authority; any other kind of reference is
// insufficient at stage 2. A request that touches none passes with none.
func TestSensitiveBoundariesNeedAuthority(t *testing.T) {
	held := outcome{status: envelope.Held, code: "held", result: "held", findings: []string{"pool.authority_required"}, reason: "authority_required"}
	admitted := outcome{status: envelope.Ok, result: "admitted", findings: []string{}}
	for _, boundary := range []string{"spend", "deletion", "security", "publication"} {
		t.Run(boundary+" with no authority, then approved", func(t *testing.T) {
			h := newHome(t)
			h.work(sensitive(boundary))
			h.plant("question", questionID, fixture(t, "pool.question", "answered-superseding"))
			id := h.admission(none())
			wants(t, h, id, h.check(id, map[string]any{}), held)
			if resp := h.approve(request{authority: word(3)}, id); resp.Status != envelope.Ok {
				t.Fatalf("approve: %s %+v", resp.Status, resp.Error)
			}
			wants(t, h, id, h.check(id, map[string]any{}), admitted)
		})
	}
	t.Run("every sensitive boundary of one Imperative is found", func(t *testing.T) {
		h := newHome(t)
		h.work(sensitive("spend", "deletion"))
		id := h.admission(none())
		wants(t, h, id, h.check(id, map[string]any{}), outcome{status: envelope.Held, code: "held", result: "held",
			findings: []string{"pool.authority_required", "pool.authority_required"}, reason: "authority_required"})
	})
	t.Run("a word that stands as the request's authority", func(t *testing.T) {
		h := newHome(t)
		h.work(sensitive("spend", "publication"))
		h.plant("question", questionID, fixture(t, "pool.question", "answered-superseding"))
		id := h.admission(request{authority: word(3)})
		wants(t, h, id, h.check(id, map[string]any{}), admitted)
	})
	t.Run("a word that does not stand", func(t *testing.T) {
		h := newHome(t)
		h.work(sensitive("spend"))
		h.plant("question", questionID, fixture(t, "pool.question", "answered-superseding"))
		id := h.admission(request{authority: word(1)})
		wants(t, h, id, h.check(id, map[string]any{}), held)
	})
	for _, kind := range []string{"posture", "charter", "instruction"} {
		t.Run("a "+kind+" reference is held, and an approval lifts it", func(t *testing.T) {
			h := newHome(t)
			h.work(sensitive("spend"))
			h.plant("question", questionID, fixture(t, "pool.question", "answered-superseding"))
			schema := map[string]string{"posture": "pool.rule", "charter": "dispatch.charter", "instruction": "dispatch.instruction"}[kind]
			id := h.admission(request{authority: authority(kind, schema, ruleID, 1)})
			check := wants(t, h, id, h.check(id, map[string]any{}), held)
			message := check["findings"].([]any)[0].(map[string]any)["message"].(string)
			if !strings.Contains(message, kind) || !strings.Contains(message, "authority_insufficient") || !strings.Contains(message, "stage 4") {
				t.Errorf("the finding's message %q", message)
			}
			if resp := h.approve(request{authority: word(3)}, id); resp.Status != envelope.Ok {
				t.Fatalf("approve: %s %+v", resp.Status, resp.Error)
			}
			wants(t, h, id, h.check(id, map[string]any{}), admitted)
		})
	}
	t.Run("none passes every check but the sensitive ones", func(t *testing.T) {
		h := newHome(t)
		h.work()
		id := h.admission(none())
		wants(t, h, id, h.check(id, map[string]any{}), admitted)
	})
}

// hive-pool read answers the kind admission with the record whole.
func TestTheReaderReturnsTheAdmissionWhole(t *testing.T) {
	h := newHome(t)
	h.work()
	id := h.admission(none())
	wants(t, h, id, h.check(id, map[string]any{}), outcome{status: envelope.Ok, result: "admitted", findings: []string{}})
	stored := h.stored("admission", id)

	read := func(selector map[string]any) *envelope.Response {
		return h.send("hive-pool", "read", map[string]any{"kind": "admission", "selector": selector}, none())
	}
	for name, selector := range map[string]map[string]any{
		"by identity": {"id": id}, "by task": {"task_id": taskID}, "by project": {"project_id": projectID},
		"with a limit": {"limit": int64(1)},
	} {
		resp := read(selector)
		if resp.Status != envelope.Ok {
			t.Fatalf("%s: status %s, error %+v", name, resp.Status, resp.Error)
		}
		records, _ := resp.Body["records"].([]any)
		if len(records) != 1 || !equalTree(records[0], stored) || resp.Body["kind"] != "admission" {
			t.Errorf("%s: records %v", name, records)
		}
		if header := tree(resp.Body["projection"]); header["complete"] != true || header["home_id"] != homeID {
			t.Errorf("%s: header %v", name, header)
		}
	}
	for name, selector := range map[string]map[string]any{
		"another task": {"task_id": "task_01jc7d8e9f0g1h2j3k4m5n6p7z"}, "another project": {"project_id": "proj_01jc7d8e9f0g1h2j3k4m5n6p7z"},
	} {
		if resp := read(selector); resp.Status != envelope.Ok || len(resp.Body["records"].([]any)) != 0 {
			t.Errorf("%s: status %s, records %v", name, resp.Status, resp.Body["records"])
		}
	}
	if resp := read(map[string]any{"id": "adm_01jc7d8e9f0g1h2j3k4m5n6p7z"}); resp.Status != envelope.NotFound {
		t.Errorf("an admission that does not exist: status %s", resp.Status)
	}
	// A member that does not apply to the kind is a violation of the request.
	for _, selector := range []map[string]any{{"ready": true}, {"active": true}, {"due_before": "2026-10-14T09:00:00.000Z"}, {"states": []any{"admitted"}}} {
		if resp := read(selector); resp.Status != envelope.Invalid {
			t.Errorf("%v: status %s", selector, resp.Status)
		}
	}
}

// A check of the Hive's own work at no project reads the repository binding
// from the Imperative's Setup.
func TestCheckOfWorkAtNoProjectReadsTheImperativesRepository(t *testing.T) {
	h := newHome(t)
	h.work(func(item, rev map[string]any) {
		for _, k := range []string{"workspace_id", "project_id", "clone_id"} {
			delete(item, k)
		}
		item["scope_level"] = "hive"
		tree(rev["setup"])["repository"] = map[string]any{"kind": "local-only"}
	})
	imp := h.stored("imperative", taskID)
	imp["scope_level"] = "hive"
	for _, k := range []string{"workspace_id", "project_id", "clone_id"} {
		delete(imp, k)
	}
	h.plant("imperative", taskID, imp)
	r := request{scope: hiveScope, authority: map[string]any{"kind": "none"}}
	resp := h.create(r, 1)
	if resp.Status != envelope.Ok {
		t.Fatalf("create: %s %+v", resp.Status, resp.Error)
	}
	id := resp.Body["admission"].(map[string]any)["admission_id"].(string)
	if rec := h.stored("admission", id); rec["scope_level"] != "hive" || rec["project_id"] != nil {
		t.Errorf("the admission is at scope %v project %v", rec["scope_level"], rec["project_id"])
	}
	check := h.send("hive-admit", "check", map[string]any{"admission_id": id, "placement": requested("mac")}, r)
	wants(t, h, id, check, outcome{status: envelope.Refused, code: "pool.publication_needs_forge", result: "refused", findings: []string{"pool.publication_needs_forge"}})
}

// The place a check is about is the item's when it is of kind requested; the
// request names no other, and a check outside the admission's scope is refused.
func TestCheckRefusesWhatItCannotRecord(t *testing.T) {
	h := newHome(t)
	h.work()
	id := h.admission(none())
	h.unchanged(func() {
		resp := h.send("hive-admit", "check", map[string]any{"admission_id": id, "placement": requested("claude-cloud-session")}, none())
		if resp.Status != envelope.Invalid || len(resp.Receipts) != 0 {
			t.Errorf("another place of kind requested: status %s, error %+v", resp.Status, resp.Error)
		}
		other := request{authority: map[string]any{"kind": "none"}, scope: map[string]any{"scope_level": "project", "home_id": homeID, "workspace_id": workspaceID, "project_id": "proj_01jc7d8e9f0g1h2j3k4m5n6p7z"}}
		resp = h.send("hive-admit", "check", map[string]any{"admission_id": id, "placement": requested("mac")}, other)
		if resp.Status == envelope.Ok || len(resp.Receipts) != 0 {
			t.Errorf("a check at another project: status %s", resp.Status)
		}
		resp = h.send("hive-admit", "check", map[string]any{"admission_id": "adm_01jc7d8e9f0g1h2j3k4m5n6p7z", "placement": requested("mac")}, none())
		if resp.Status != envelope.NotFound {
			t.Errorf("an admission that does not exist: status %s", resp.Status)
		}
	})
}

// Rule verification is not built: a request whose only authority is a rule
// reference gives a sensitive boundary nothing and is held on authority_required,
// and an approval on the record lets a later check admit it.
func TestARuleReferenceAloneIsHeldUntilApproved(t *testing.T) {
	h := newHome(t)
	h.work(sensitive("deletion"))
	h.plant("question", questionID, fixture(t, "pool.question", "answered-superseding"))
	rule := fixture(t, "pool.rule_v2", "minimal")
	h.plant("rule", rule["rule_id"].(string), rule)
	id := h.admission(request{authority: authority("rule", "pool.rule", rule["rule_id"].(string), 1)})
	wants(t, h, id, h.check(id, map[string]any{}), outcome{status: envelope.Held, code: "held", result: "held",
		findings: []string{"pool.authority_required"}, reason: "authority_required"})
	if resp := h.approve(request{authority: word(3)}, id); resp.Status != envelope.Ok {
		t.Fatalf("approve: %s %+v", resp.Status, resp.Error)
	}
	wants(t, h, id, h.check(id, map[string]any{}), outcome{status: envelope.Ok, result: "admitted", findings: []string{}})
}
