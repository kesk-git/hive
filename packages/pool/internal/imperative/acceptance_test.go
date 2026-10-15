package imperative_test

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

// The acceptance test of the Imperative (spec/pool.md, The Imperative):
// hive-imperative create, revise, render and deliver, and the reader of the
// kind imperative.

// same says whether two decoded values are one value.
func same(t testing.TB, a, b any) bool {
	t.Helper()
	x, err := canon.Encode(a)
	if err != nil {
		t.Fatal(err)
	}
	y, err := canon.Encode(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(x) == string(y)
}

func revisionsOf(rec map[string]any) []any { return list(rec["revisions"]) }

func TestCreateWritesRevisionOneWithTheTypedFieldsAndTheCopiedAxes(t *testing.T) {
	w := newWorld(t)
	body := w.body(w.hunt)
	itemRevision := w.stored("item", w.hunt)["record_revision"]

	resp := w.send("hive-imperative", "create", body, request{})
	rec := ok(t, resp)["imperative"].(map[string]any)
	if !slices.Equal(kinds(resp), []string{"pool.imperative.revised"}) {
		t.Errorf("receipts %v", kinds(resp))
	}
	if sub := resp.Receipts[0].Subject; sub == nil || sub.Schema != "pool.imperative" || sub.ID != w.hunt || sub.RecordRevision != 1 {
		t.Errorf("the receipt is about %+v", sub)
	}
	if rec["record_revision"] != int64(1) || rec["current_revision"] != int64(1) || rec["task_id"] != w.hunt || len(list(rec["deliveries"])) != 0 {
		t.Errorf("record %v", rec)
	}
	if rec["scope_level"] != "project" || rec["workspace_id"] != workspaceID || rec["project_id"] != projectID {
		t.Errorf("the scope of the record is the item's: %v", rec)
	}
	if !same(t, rec, w.stored("imperative", w.hunt)) {
		t.Error("the response is not the record as stored")
	}

	revisions := revisionsOf(rec)
	if len(revisions) != 1 {
		t.Fatalf("revisions %v", revisions)
	}
	first := revisions[0].(map[string]any)
	if first["revision"] != int64(1) || first["class"] != int64(3) || first["supersedes_reason"] != nil {
		t.Errorf("revision %v", first)
	}
	for _, k := range []string{"task", "rules", "definition_of_done"} {
		if !same(t, first[k], body[k]) {
			t.Errorf("%s was not kept whole: %v", k, first[k])
		}
	}
	task := tree(first["task"])
	if tree(tree(task["object"])["confidence"])["level"] != "high" || tree(tree(task["standard"])["confidence"])["level"] != "high" {
		t.Errorf("the confidence of the typed fields: %v", task)
	}
	setup := tree(first["setup"])
	if !same(t, setup["placement"], map[string]any{"requested": "mac", "item_revision": itemRevision}) {
		t.Errorf("the placement axis is the item's: %v", setup["placement"])
	}
	if !same(t, setup["repository"], repository) {
		t.Errorf("the repository is the project record's: %v", setup["repository"])
	}
	for k, v := range tree(body["setup"]) {
		if !same(t, setup[k], v) {
			t.Errorf("setup %s was %v and is %v", k, v, setup[k])
		}
	}
	if first["digest"] != expectedDigest(t, first) {
		t.Errorf("the digest %v is not that of the class and the four sections", first["digest"])
	}
	if !same(t, first["authored_by"], map[string]any{"kind": "test"}) || !same(t, first["authority"], map[string]any{"kind": "none"}) || first["request_id"] == nil {
		t.Errorf("attribution %v", first)
	}
}

func TestCreateKeepsAGuessWithItsQuestionAndConsequence(t *testing.T) {
	w := newWorld(t)
	body := w.body(w.hunt)
	object := tree(tree(body["task"])["object"])
	object["confidence"] = map[string]any{"level": "guess", "question": "Which export is meant?", "consequence": "The nightly one or the weekly one is built."}
	rec := ok(t, w.send("hive-imperative", "create", body, request{}))["imperative"].(map[string]any)
	got := tree(tree(tree(tree(revisionsOf(rec)[0])["task"])["object"])["confidence"])
	if got["level"] != "guess" || got["question"] != "Which export is meant?" || got["consequence"] == nil {
		t.Errorf("confidence %v", got)
	}
}

func TestCreateTakesTheRepositoryGivenOnlyForWorkAtNoProject(t *testing.T) {
	w := newWorld(t)
	id := w.item("hunt", w.prey)
	rec := w.stored("item", id)
	for _, k := range []string{"project_id"} {
		delete(rec, k)
	}
	rec["scope_level"] = "workspace"
	w.plant("item", id, rec)

	body := w.body(id)
	refused(t, w.send("hive-imperative", "create", body, request{scope: workspaceScope}), envelope.Invalid, "schema_violation")
	tree(body["setup"])["repository"] = map[string]any{"kind": "local-only"}
	got := ok(t, w.send("hive-imperative", "create", body, request{scope: workspaceScope}))["imperative"].(map[string]any)
	if !same(t, tree(tree(revisionsOf(got)[0])["setup"])["repository"], map[string]any{"kind": "local-only"}) {
		t.Errorf("setup %v", tree(revisionsOf(got)[0])["setup"])
	}
}

func TestCreateRefusesWhatOthersWork(t *testing.T) {
	w := newWorld(t)
	decision := w.item("hunt", w.prey)
	rec := w.stored("item", decision)
	delete(rec, "level")
	rec["deliverable"] = "decision"
	w.plant("item", decision, rec)

	for name, id := range map[string]string{"a Target": w.target, "a Prey with a Hunt beneath it": w.prey, "a decision item": decision} {
		w.unchanged(func() {
			refused(t, w.create(id), envelope.Refused, "pool.not_dispatchable")
		})
		if w.stored("imperative", id) != nil {
			t.Errorf("%s has an Imperative", name)
		}
	}
	// A Prey with no Hunt beneath it is worked itself.
	lone := w.item("prey", w.target)
	ok(t, w.create(lone))
	// An item that does not exist is not found, and a second revision 1 is refused.
	refused(t, w.create(mint(t, ids.Task)), envelope.NotFound, "not_found")
	ok(t, w.create(w.hunt))
	refused(t, w.create(w.hunt), envelope.Conflict, "already_exists")
}

func TestReviseWritesTheNextWholeRevisionWithItsReasonUnderTheExpectedRevision(t *testing.T) {
	w := newWorld(t)
	created := w.created()
	first := tree(revisionsOf(created)[0])

	// The item has since moved its requested place; the next revision copies it.
	item := w.stored("item", w.hunt)
	item["placement_requested"], item["record_revision"] = "claude-cloud-session", item["record_revision"].(int64)+1
	w.plant("item", w.hunt, item)

	body := w.body(w.hunt)
	body["class"] = int64(4)
	body["supersedes_reason"] = "The steer narrowed the scope to the retry loop."
	tree(tree(body["task"])["object"])["text"] = "The retry loop of the nightly export, and nothing else."
	expect := request{expected: []rev{{"pool.imperative", w.hunt, 1}}}

	w.unchanged(func() {
		refused(t, w.send("hive-imperative", "revise", body, request{expected: []rev{{"pool.imperative", w.hunt, 7}}}), envelope.Conflict, "revision_conflict")
		refused(t, w.send("hive-imperative", "revise", body, request{}), envelope.Invalid, "schema_violation")
	})

	resp := w.send("hive-imperative", "revise", body, expect)
	rec := ok(t, resp)["imperative"].(map[string]any)
	if !slices.Equal(kinds(resp), []string{"pool.imperative.revised"}) {
		t.Errorf("receipts %v", kinds(resp))
	}
	if sub := resp.Receipts[0].Subject; sub == nil || sub.RecordRevision != 2 {
		t.Errorf("the receipt is about %+v", sub)
	}
	revisions := revisionsOf(rec)
	if rec["record_revision"] != int64(2) || rec["current_revision"] != int64(2) || len(revisions) != 2 {
		t.Fatalf("record %v", rec)
	}
	if !same(t, revisions[0], first) {
		t.Error("revision 1 was changed; a revision is never changed")
	}
	next := tree(revisions[1])
	if next["revision"] != int64(2) || next["class"] != int64(4) || next["supersedes_reason"] != "The steer narrowed the scope to the retry loop." {
		t.Errorf("revision %v", next)
	}
	if got := tree(tree(next["setup"])["placement"])["requested"]; got != "claude-cloud-session" {
		t.Errorf("the placement is copied again from the item: %v", got)
	}
	if next["digest"] != expectedDigest(t, next) || next["digest"] == first["digest"] {
		t.Errorf("digest %v, first %v", next["digest"], first["digest"])
	}
	if !same(t, rec, w.stored("imperative", w.hunt)) {
		t.Error("the response is not the record as stored")
	}

	// The first caller's expectation is stale now.
	refused(t, w.send("hive-imperative", "revise", body, expect), envelope.Conflict, "revision_conflict")
}

func TestRenderIsDeterministicAndReadOnly(t *testing.T) {
	w := newWorld(t)
	w.created()
	var a, b map[string]any
	w.unchanged(func() {
		a = ok(t, w.render(w.hunt))
		b = ok(t, w.render(w.hunt))
	})
	for _, k := range []string{"text", "text_digest", "text_bytes", "revision_digest", "renderer_version", "rules", "subject_projections"} {
		if !same(t, a[k], b[k]) {
			t.Errorf("%s differs between renderings", k)
		}
	}
	text := a["text"].(string)
	if got := canon.FileDigest([]byte(text)); a["text_digest"] != got || a["text_bytes"] != int64(len(text)) {
		t.Errorf("digest %v bytes %v of a text that is %s, %d bytes", a["text_digest"], a["text_bytes"], got, len(text))
	}
	if a["revision"] != int64(1) || a["revision_digest"] != tree(revisionsOf(w.stored("imperative", w.hunt))[0])["digest"] || a["renderer_version"] == nil {
		t.Errorf("response %v", a)
	}
	if a["rendered_at"] != "2026-10-14T09:00:00.000Z" {
		t.Errorf("rendered_at %v", a["rendered_at"])
	}
	want := map[string]any{"role": "swarm", "harness": "claude", "placement_requested": "mac", "class": int64(3), "validation": "synapse", "publication": "harvest"}
	if !same(t, a["launch_parameters"], want) {
		t.Errorf("launch parameters %v", a["launch_parameters"])
	}
}

func TestRenderWritesTheCriteriaThePlaybookAndTheSubjectProjectionInOrder(t *testing.T) {
	w := newWorld(t)
	w.created()
	r := ok(t, w.render(w.hunt))
	text := r["text"].(string)

	if !strings.HasPrefix(text, "# Worker role boundary\n") {
		t.Errorf("the text does not open with the worker role boundary: %.60q", text)
	}
	at := func(marker string) int {
		t.Helper()
		i := strings.Index(text, marker)
		if i < 0 {
			t.Fatalf("the text has no %q", marker)
		}
		return i
	}
	order := []int{at("## Task"), at("## Setup"), at("## Rules"), at("### Boundary"), at("### Playbook"), at("### Subject projection "), at("## Definition of done")}
	if !slices.IsSorted(order) {
		t.Errorf("the headings are out of order: %v", order)
	}

	// The criteria are those of the revision the Standard names, in the Standard.
	criteria := at("The collected sample is kept for one day.")
	if criteria < at("## Task") || criteria > at("## Setup") || !strings.Contains(text, "A test reads the sample a day later.") {
		t.Errorf("the criteria are not in the Standard: %d", criteria)
	}
	if strings.Contains(text, "A test reads the sample after reset.") {
		t.Error("a criterion of an earlier revision was rendered")
	}
	if !same(t, r["criteria"], w.criteria) {
		t.Errorf("criteria %v", r["criteria"])
	}
	// The Placement axis is in Setup.
	if p := at("mac"); p < at("## Setup") || p > at("## Rules") {
		t.Errorf("the placement is not in Setup: %d", p)
	}

	// The playbook is the rules in force for the class's role, in the resolver's
	// order, each with its source and revision; the rule of another class is not.
	wantRules := append(slices.Clone(w.playbook), w.touchedRule)
	slices.Sort(wantRules)
	playbook := text[at("### Playbook"):at("### Subject projection ")]
	last := -1
	for _, id := range wantRules {
		i := strings.Index(playbook, "["+id+"@1]")
		if i < 0 || i < last {
			t.Errorf("rule %s is missing from the playbook or out of order:\n%s", id, playbook)
		}
		last = i
	}
	if strings.Contains(text, "Only a class 5 worker reads this.") {
		t.Error("the rule of another class is in the playbook")
	}
	if !strings.Contains(playbook, "Stop at the stated goal and report the evidence. [") {
		t.Errorf("a rule line is not followed by its source:\n%s", playbook)
	}

	// The projection of the subject the specification touches follows it.
	projection := text[at("### Subject projection "):at("## Definition of done")]
	if !strings.Contains(projection, w.projection) || !strings.Contains(projection, "Exports retry at most three times.") {
		t.Errorf("the projection:\n%s", projection)
	}

	// Both inventories, the playbook's rules first and the union without repeats.
	var gotRules []string
	for _, v := range list(r["rules"]) {
		m := tree(v)
		if m["record_revision"] != int64(1) {
			t.Errorf("rule %v", m)
		}
		gotRules = append(gotRules, m["rule_id"].(string))
	}
	if !slices.Equal(gotRules, wantRules) {
		t.Errorf("rules %v, want %v", gotRules, wantRules)
	}
	projections := list(r["subject_projections"])
	if len(projections) != 1 {
		t.Fatalf("subject projections %v", projections)
	}
	p := tree(projections[0])
	stored := w.stored("projection", w.projection)
	current := tree(stored["current"])
	if p["projection_id"] != w.projection || p["rendering"] != current["rendering"] || p["digest"] != current["digest"] {
		t.Errorf("projection %v, stored %v", p, current)
	}
	if !same(t, p["rules"], []any{map[string]any{"rule_id": w.touchedRule, "record_revision": int64(1)}}) {
		t.Errorf("the projection's rules %v", p["rules"])
	}
}

func TestRenderGivesAnotherTextWhenARuleChanges(t *testing.T) {
	w := newWorld(t)
	w.created()
	before := ok(t, w.render(w.hunt))
	w.reword(w.playbook[0], "Work in one commit and run the declared command once.")
	// A rule written through hive-rule re-renders the projections of its scope in
	// its own transaction; the planted one is rendered again here.
	ok(t, w.send("hive-memory", "render", map[string]any{"tier": "subject", "audience": "class-3", "subject_id": w.touched}, request{}))
	after := ok(t, w.render(w.hunt))
	if before["text_digest"] == after["text_digest"] || !strings.Contains(after["text"].(string), "["+w.playbook[0]+"@2]") {
		t.Error("a changed rule left the rendering as it was")
	}
	if before["revision_digest"] != after["revision_digest"] {
		t.Error("the digest of the revision moved with the rules")
	}
}

func TestRenderRefusesAConflictedPlaybook(t *testing.T) {
	w := newWorld(t)
	w.created()
	w.rule(w.classSubject, "class3.steps", "A second rule on a subject the class already rules.", "class-3")
	w.unchanged(func() {
		refused(t, w.render(w.hunt), envelope.Refused, "pool.rule_conflict")
	})
}

func TestRenderRefusesAStaleSubjectProjectionAndNamesIt(t *testing.T) {
	w := newWorld(t)
	w.created()
	ok(t, w.render(w.hunt))
	// A rule now in force that the projection was not rendered from makes it
	// stale, with no writer having marked it.
	w.reword(w.touchedRule, "Exports retry at most five times.")
	w.unchanged(func() {
		resp := refused(t, w.render(w.hunt), envelope.Refused, "pool.projection_stale")
		d := resp.Error.Detail
		sources := list(d["stale_sources"])
		if d["projection"] != w.projection || len(sources) != 1 || tree(sources[0])["id"] != w.touchedRule || tree(sources[0])["schema"] != "pool.rule" {
			t.Errorf("detail %v", d)
		}
	})
	// Rendered again, it is fresh and the Imperative renders.
	ok(t, w.send("hive-memory", "render", map[string]any{"tier": "subject", "audience": "class-3", "subject_id": w.touched}, request{}))
	ok(t, w.render(w.hunt))

	// A conflicted playbook stays pool.rule_conflict, and a bound subject with no
	// projection stays not found.
	w.rule(w.classSubject, "class3.steps", "A second rule on a subject the class already rules.", "class-3")
	refused(t, w.render(w.hunt), envelope.Refused, "pool.rule_conflict")
	other := newWorld(t)
	other.created()
	if err := os.Remove(filepath.Join(other.dir, "pool", "records", "projection", other.projection+".json")); err != nil {
		t.Fatal(err)
	}
	refused(t, other.render(other.hunt), envelope.NotFound, "not_found")
}

func TestRenderWithoutASpecificationHasNoCriteriaAndNoSubjects(t *testing.T) {
	w := newWorld(t)
	body := w.body(w.hunt)
	delete(tree(tree(body["task"])["standard"]), "criteria")
	ok(t, w.send("hive-imperative", "create", body, request{}))
	r := ok(t, w.render(w.hunt))
	if r["criteria"] != nil || len(list(r["subject_projections"])) != 0 || strings.Contains(r["text"].(string), "### Subject projection") {
		t.Errorf("response %v", r)
	}
}

// delivery builds the deliver request from a rendering, as the dispatcher's test
// caller does.
func (w *world) delivery(r map[string]any, generation int64, transport string) map[string]any {
	return map[string]any{
		"task_id": w.hunt, "generation": generation, "revision": r["revision"], "renderer_version": r["renderer_version"],
		"text_digest": r["text_digest"], "text_bytes": r["text_bytes"], "transport": transport,
		"evidence": map[string]any{"schema": "dispatch.launch", "id": mint(w.t, ids.Receipt), "home_id": homeID, "record_revision": int64(1)},
		"rules":    r["rules"], "subject_projections": r["subject_projections"], "rendered_at": r["rendered_at"], "rendered_text": r["text"],
	}
}

func TestDeliverRecordsWhatTheGenerationWasGivenAndWhatItReceived(t *testing.T) {
	w := newWorld(t)
	w.created()
	w.open(w.hunt)
	r := ok(t, w.render(w.hunt))
	body := w.delivery(r, 1, "launch-prompt")

	resp := w.send("hive-imperative", "deliver", body, request{})
	rec := ok(t, resp)["imperative"].(map[string]any)
	if want := []string{"pool.imperative.delivered", "pool.projection.received"}; !slices.Equal(kinds(resp), want) {
		t.Errorf("receipts %v, want %v", kinds(resp), want)
	}
	if rec["record_revision"] != int64(2) || len(list(rec["deliveries"])) != 1 {
		t.Fatalf("record %v", rec)
	}
	d := tree(list(rec["deliveries"])[0])
	for _, k := range []string{"generation", "revision", "renderer_version", "text_digest", "text_bytes", "transport", "evidence", "rules", "subject_projections", "rendered_at"} {
		if !same(t, d[k], body[k]) {
			t.Errorf("delivery %s was %v and is %v", k, body[k], d[k])
		}
	}
	if d["delivered_at"] != "2026-10-14T09:00:00.000Z" {
		t.Errorf("delivered_at %v", d["delivered_at"])
	}
	if !same(t, rec, w.stored("imperative", w.hunt)) {
		t.Error("the response is not the record as stored")
	}
	for path, content := range w.records() {
		if strings.Contains(content, "# Worker role boundary") {
			t.Errorf("the rendered text was kept in %s", path)
		}
	}

	// The embedded projection was received for the worker's generation, at the
	// digest rendered, in the same transaction.
	projection := w.stored("projection", w.projection)
	consumers := list(projection["consumers"])
	if len(consumers) != 1 || len(list(projection["receipt_history"])) != 1 {
		t.Fatalf("consumers %v", consumers)
	}
	c := tree(consumers[0])
	received := tree(c["received"])
	embedded := tree(list(r["subject_projections"])[0])
	if !same(t, c["consumer"], map[string]any{"kind": "generation", "task_id": w.hunt, "generation": int64(1)}) ||
		received["rendering"] != embedded["rendering"] || received["digest"] != embedded["digest"] {
		t.Errorf("consumer %v", c)
	}

	// The same step through propagate is the one recorded, unchanged.
	again := w.send("hive-memory", "propagate", map[string]any{
		"projection_id": w.projection, "consumer": c["consumer"], "step": "received",
		"rendering": embedded["rendering"], "digest": embedded["digest"], "at": "2026-10-14T09:00:00.000Z",
	}, request{})
	ok(t, again)
	if len(again.Receipts) != 0 {
		t.Errorf("a step already recorded wrote %v", kinds(again))
	}

	// A steer to the same generation is a second delivery; the step is not repeated.
	steer := w.send("hive-imperative", "deliver", w.delivery(r, 1, "steer"), request{})
	rec = ok(t, steer)["imperative"].(map[string]any)
	if len(list(rec["deliveries"])) != 2 || !slices.Equal(kinds(steer), []string{"pool.imperative.delivered"}) {
		t.Errorf("deliveries %d, receipts %v", len(list(rec["deliveries"])), kinds(steer))
	}
}

func TestDeliverRefusesWhatDoesNotAgreeWithTheRenderingSupplied(t *testing.T) {
	w := newWorld(t)
	w.created()
	w.open(w.hunt)
	r := ok(t, w.render(w.hunt))

	for name, edit := range map[string]func(map[string]any){
		"a text that is not the digest": func(b map[string]any) { b["rendered_text"] = b["rendered_text"].(string) + " " },
		"a size that is not the text's": func(b map[string]any) { b["text_bytes"] = b["text_bytes"].(int64) + 1 },
		"a rule the text does not carry": func(b map[string]any) {
			b["rules"] = append(list(b["rules"]), map[string]any{"rule_id": mint(t, ids.Rule), "record_revision": int64(1)})
		},
		"a rule the inventory leaves out":     func(b map[string]any) { b["rules"] = list(b["rules"])[1:] },
		"a revision of a rule the text names": func(b map[string]any) { tree(list(b["rules"])[0])["record_revision"] = int64(9) },
		"a projection digest the text does not": func(b map[string]any) {
			tree(list(b["subject_projections"])[0])["digest"] = "sha256:" + strings.Repeat("a", 64)
		},
		"a projection rendering the text does not": func(b map[string]any) {
			tree(list(b["subject_projections"])[0])["rendering"] = int64(5)
		},
		"a projection the text does not embed": func(b map[string]any) {
			b["subject_projections"] = append(list(b["subject_projections"]), map[string]any{"projection_id": mint(t, ids.Projection), "rendering": int64(1), "digest": "sha256:" + strings.Repeat("b", 64), "rules": []any{}})
		},
		"a projection's rule the text does not": func(b map[string]any) {
			tree(list(b["subject_projections"])[0])["rules"] = []any{}
		},
	} {
		body := w.delivery(r, 1, "launch-prompt")
		edit(body)
		w.unchanged(func() {
			resp := w.send("hive-imperative", "deliver", body, request{})
			if resp.Status != envelope.Refused || resp.Error == nil || resp.Error.Code != "pool.delivery_inventory" {
				t.Errorf("%s: status %s, error %+v", name, resp.Status, resp.Error)
			}
		})
	}

	// A new delivery carries the inventories and the exact text.
	for _, k := range []string{"rendered_text", "rules", "subject_projections", "rendered_at"} {
		body := w.delivery(r, 1, "launch-prompt")
		delete(body, k)
		w.unchanged(func() {
			refused(t, w.send("hive-imperative", "deliver", body, request{}), envelope.Invalid, "schema_violation")
		})
	}
	// A revision the Imperative does not hold is not found.
	body := w.delivery(r, 1, "launch-prompt")
	body["revision"] = int64(4)
	refused(t, w.send("hive-imperative", "deliver", body, request{}), envelope.NotFound, "not_found")
}

func TestDeliverRefusesAGenerationThatIsNotOpen(t *testing.T) {
	w := newWorld(t)
	w.created()
	r := ok(t, w.render(w.hunt))
	check := func(generation int64) {
		t.Helper()
		w.unchanged(func() {
			resp := refused(t, w.send("hive-imperative", "deliver", w.delivery(r, generation, "launch-prompt"), request{}), envelope.Refused, "pool.generation_not_open")
			if d := resp.Error.Detail; d["task_id"] != w.hunt || d["generation"] != generation {
				t.Errorf("detail %v", d)
			}
		})
	}
	// The item has no generation yet, then a generation that is not the one named,
	// then the one named once it is closed.
	check(1)
	w.open(w.hunt)
	check(2)
	w.closeGeneration(w.hunt)
	check(1)
}

func TestCreateAndReviseAreDeclaredAtOneZeroOnly(t *testing.T) {
	w := newWorld(t)
	w.unchanged(func() {
		refused(t, w.send("hive-imperative", "create", w.body(w.hunt), request{version: "1.1"}), envelope.Invalid, "unsupported_version")
	})
	w.created()
	body := w.body(w.hunt)
	body["supersedes_reason"] = "A steer."
	w.unchanged(func() {
		refused(t, w.send("hive-imperative", "revise", body, request{version: "1.1", expected: []rev{{"pool.imperative", w.hunt, 1}}}), envelope.Invalid, "unsupported_version")
	})
	for _, op := range []string{"render", "deliver"} {
		refused(t, w.send("hive-imperative", op, map[string]any{"task_id": w.hunt}, request{version: "1.0"}), envelope.Invalid, "unsupported_version")
	}
}

func TestTheReaderReturnsTheImperativeWhole(t *testing.T) {
	w := newWorld(t)
	w.created()
	w.open(w.hunt)
	r := ok(t, w.render(w.hunt))
	ok(t, w.send("hive-imperative", "deliver", w.delivery(r, 1, "launch-prompt"), request{}))

	read := func(selector map[string]any) *envelope.Response {
		return w.send("hive-pool", "read", map[string]any{"kind": "imperative", "selector": selector}, request{})
	}
	body := ok(t, read(map[string]any{"id": w.hunt}))
	records := list(body["records"])
	if len(records) != 1 || !same(t, records[0], w.stored("imperative", w.hunt)) {
		t.Fatalf("records %v", records)
	}
	if h := tree(body["projection"]); h["complete"] != true || h["home_id"] != homeID {
		t.Errorf("header %v", h)
	}
	if len(list(tree(records[0])["deliveries"])) != 1 {
		t.Errorf("the deliveries were not returned whole: %v", records[0])
	}
	// The query selects by project, and a member that is not the kind's is refused.
	if got := list(ok(t, read(map[string]any{"project_id": projectID}))["records"]); len(got) != 1 {
		t.Errorf("by project: %v", got)
	}
	refused(t, read(map[string]any{"id": mint(t, ids.Task)}), envelope.NotFound, "not_found")
	refused(t, read(map[string]any{"states": []any{"queued"}}), envelope.Invalid, "schema_violation")
}
