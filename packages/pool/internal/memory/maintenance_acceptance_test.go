package memory_test

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// The tests of hive-memory enqueue and fold (spec/pool.md, Generated knowledge,
// The maintenance queue and the fold), each through the executable.

const (
	queueSchema = "pool.maintenance-queue"
	queueID     = "maintenance"
	queueKind   = "maintenance-queue"
	foldedKind  = "pool.memory.folded"
	queuedKind  = "pool.note.queued"
)

var noteIDForm = regexp.MustCompile(`^mnote_[0-7][0-9a-hjkmnp-tv-z]{25}$`)

// plantQueue stands the maintenance queue in the home, empty, at revision 1.
func (h *home) plantQueue() {
	h.t.Helper()
	rec := fixture(h.t, queueSchema, "minimal")
	rec["home_id"], rec["record_revision"] = homeID, int64(1)
	h.plant(queueKind, queueID, rec)
}

func (h *home) queue() map[string]any { return h.stored(queueKind, queueID) }

func (h *home) pending() []string {
	h.t.Helper()
	var out []string
	for _, v := range h.queue()["pending"].([]any) {
		out = append(out, v.(string))
	}
	return out
}

// rawCall sends the request document as it is, and returns the response.
func (h *home) rawCall(command string, op string, data []byte) *envelope.Response {
	h.t.Helper()
	var cmd *cli.Command
	for _, c := range h.exe.Commands {
		if c.Name == command {
			cmd = c
		}
	}
	out, err := h.exe.Handle(cli.Invocation{Command: cmd, Operation: op, Home: h.dir, Request: append(bytes.Clone(data), '\n')})
	if err != nil {
		h.t.Fatal(err)
	}
	resp, err := envelope.DecodeResponse(out.Encoded)
	if err != nil {
		h.t.Fatal(err)
	}
	return resp
}

// document makes a request of hive-memory at the scope, expecting the queue at
// the revision when it is not zero.
func (h *home) document(op string, body map[string]any, scope map[string]any, queueRevision int64) []byte {
	h.t.Helper()
	req := map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": "hive-memory", "operation": op, "operation_version": "1.0",
		"request_id": mint(h.t, ids.Request), "correlation_id": mint(h.t, ids.Correlation),
		"issued_at": "2026-10-14T09:00:00.000Z", "caller": map[string]any{"kind": "test"},
		"authority": map[string]any{"kind": "none"}, "body": body,
	}
	for k, v := range scope {
		req[k] = v
	}
	if queueRevision != 0 {
		req["expected_revisions"] = []any{map[string]any{"schema": queueSchema, "id": queueID, "record_revision": queueRevision}}
	}
	data, err := canon.Encode(req)
	if err != nil {
		h.t.Fatal(err)
	}
	return data
}

// enqueue sends hive-memory enqueue at the scope against the queue's revision.
func (h *home) enqueue(scope map[string]any, queueRevision int64, notes ...map[string]any) *envelope.Response {
	h.t.Helper()
	list := make([]any, len(notes))
	for i, n := range notes {
		list[i] = n
	}
	return h.rawCall("hive-memory", "enqueue", h.document("enqueue", map[string]any{"notes": list}, scope, queueRevision))
}

// fold sends hive-memory fold against the queue's revision.
func (h *home) fold(queueRevision int64) *envelope.Response {
	h.t.Helper()
	return h.rawCall("hive-memory", "fold", h.document("fold", map[string]any{}, hiveScope, queueRevision))
}

// provenance is the fixture's evidence, observed by a worker of the task where
// one is given.
func provenance(t testing.TB, task string) map[string]any {
	t.Helper()
	p := fixture(t, "pool.note", "minimal")["provenance"].(map[string]any)
	if task != "" {
		p["observer"] = map[string]any{"kind": "worker", "task_id": task, "generation": int64(1)}
	}
	return p
}

// note is a note of the origin with the text, and the members given.
func note(t testing.TB, text string, over map[string]any) map[string]any {
	t.Helper()
	n := map[string]any{"text": text, "provenance": provenance(t, ""), "origin": "stow"}
	for k, v := range over {
		n[k] = v
	}
	return n
}

// enqueued enqueues the notes at the Hive's scope against the queue as it
// stands and returns the identities the notes were given, in order.
func (h *home) enqueued(notes ...map[string]any) []string {
	h.t.Helper()
	return h.enqueuedAt(hiveScope, notes...)
}

func (h *home) enqueuedAt(scope map[string]any, notes ...map[string]any) []string {
	h.t.Helper()
	body := h.ok(h.enqueue(scope, h.queue()["record_revision"].(int64), notes...))
	var out []string
	for _, v := range body["notes"].([]any) {
		out = append(out, v.(map[string]any)["note_id"].(string))
	}
	return out
}

func receiptOf(t testing.TB, resp *envelope.Response, kind string) envelope.Receipt {
	t.Helper()
	var found []envelope.Receipt
	for _, r := range resp.Receipts {
		if r.Kind == kind {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want one %s receipt, got %d", kind, len(found))
	}
	return found[0]
}

func kinds(resp *envelope.Response) map[string]int {
	out := map[string]int{}
	for _, r := range resp.Receipts {
		out[r.Kind]++
	}
	return out
}

func strs(v any) []string {
	var out []string
	for _, e := range v.([]any) {
		out = append(out, e.(string))
	}
	return out
}

// factsOf is the facts of a stored subject, by identity.
func (h *home) factsOf(subject string) map[string]string {
	h.t.Helper()
	out := map[string]string{}
	for _, v := range h.stored("subject", subject)["facts"].([]any) {
		f := v.(map[string]any)
		out[f["fact_id"].(string)] = f["text"].(string)
	}
	return out
}

func (h *home) items() []map[string]any {
	h.t.Helper()
	var out []map[string]any
	for rel := range h.records() {
		if strings.HasPrefix(rel, "pool/records/item/") {
			out = append(out, h.stored("item", strings.TrimSuffix(filepath.Base(rel), ".json")))
		}
	}
	return out
}

// foldSeed is a home with the budget, the queue and a subject at the Hive's
// scope that holds one fact.
func foldSeed(t testing.TB) (h *home, subject string) {
	h, _ = seed(t)
	h.plantQueue()
	return h, h.subject(hiveScope, "class.0", "The first fact.")
}

func TestEnqueueKeepsEachNoteWhole(t *testing.T) {
	h, subject := foldSeed(t)
	text := "  A note, kept exactly\n\twith its spacing — and “quotes”.  "
	first := note(t, text, map[string]any{"subject_id": subject, "fact_id": "kept.exactly", "origin": "swarm-report"})
	first["provenance"] = provenance(t, "task_01jc7d8e9f0g1h2j3k4m5n6p7t")
	second := note(t, "A note with no subject.", map[string]any{"origin": "lictor-report"})

	resp := h.enqueue(projectScope, 1, first, second)
	body := h.ok(resp)
	notes := body["notes"].([]any)
	if len(notes) != 2 {
		t.Fatalf("notes %v", notes)
	}
	ids := h.pending()
	queue := body["queue"].(map[string]any)
	if queue["record_revision"] != int64(2) || !slices.Equal(strs(queue["pending"]), ids) || len(ids) != 2 {
		t.Fatalf("queue %v, pending %v", queue, ids)
	}
	for i, want := range []map[string]any{first, second} {
		got := notes[i].(map[string]any)
		id := got["note_id"].(string)
		if !noteIDForm.MatchString(id) || ids[i] != id {
			t.Errorf("note %d has the identity %q, pending %q", i, id, ids[i])
		}
		stored := h.stored("note", id)
		if !equal(stored, got) {
			t.Errorf("note %d: the response is not the stored record", i)
		}
		for _, m := range []string{"text", "provenance", "origin"} {
			if !equal(stored[m], want[m]) {
				t.Errorf("note %d: %s is %v, want %v", i, m, stored[m], want[m])
			}
		}
		for _, m := range []string{"subject_id", "fact_id"} {
			if v, ok := want[m]; ok != (stored[m] != nil) || (ok && stored[m] != v) {
				t.Errorf("note %d: %s is %v, want %v", i, m, stored[m], v)
			}
		}
		if stored["state"] != "pending" || stored["record_revision"] != int64(1) || stored["disposition"] != nil {
			t.Errorf("note %d is %v", i, stored)
		}
		if stored["scope_level"] != "project" || stored["workspace_id"] != workspaceID || stored["project_id"] != projectID || stored["home_id"] != homeID {
			t.Errorf("note %d is not at the scope of the request: %v", i, stored)
		}
	}
	if stored := h.stored("note", ids[0]); stored["text"] != text {
		t.Errorf("the text is %q, want %q", stored["text"], text)
	}
	if k := kinds(resp); k[queuedKind] != 2 || len(resp.Receipts) != 2 {
		t.Errorf("receipts %v", k)
	}
	for i, r := range resp.Receipts {
		if r.Kind != queuedKind || r.Subject == nil || r.Subject.Schema != "pool.note" || r.Subject.ID != ids[i] || r.Subject.RecordRevision != 1 {
			t.Errorf("receipt %d: %+v", i, r)
		}
	}
}

func TestEnqueueReplayAddsNoNote(t *testing.T) {
	h, _ := foldSeed(t)
	data := h.document("enqueue", map[string]any{"notes": []any{note(t, "Once only.", nil)}}, hiveScope, 1)
	first := h.rawCall("hive-memory", "enqueue", data)
	h.ok(first)
	pending, files := h.pending(), len(h.records())

	again := h.rawCall("hive-memory", "enqueue", data)
	if !again.Replayed || again.Status != envelope.Ok || !equal(again.Body, first.Body) {
		t.Fatalf("replayed %v, status %s, body %v", again.Replayed, again.Status, again.Body)
	}
	if got := h.pending(); !slices.Equal(got, pending) || len(h.records()) != files || h.queue()["record_revision"] != int64(2) {
		t.Errorf("a replay changed the Pool: pending %v, %d files", got, len(h.records()))
	}
}

func TestEnqueueNeedsTheQueueAtItsRevision(t *testing.T) {
	h, _ := foldSeed(t)
	one := note(t, "Never written.", nil)
	h.unchanged(func() {
		refused(t, h.enqueue(hiveScope, 2, one), envelope.Conflict, envelope.CodeRevisionConflict)
		refused(t, h.enqueue(hiveScope, 0, one), envelope.Invalid, envelope.CodeSchemaViolation)
	})
	h.enqueued(note(t, "First.", nil))
	h.unchanged(func() {
		refused(t, h.enqueue(hiveScope, 1, one), envelope.Conflict, envelope.CodeRevisionConflict)
	})
}

func TestEnqueueRefusesWhatTheSchemaRefuses(t *testing.T) {
	h, _ := foldSeed(t)
	h.unchanged(func() {
		bad := note(t, "Unknown origin.", map[string]any{"origin": "guess"})
		refused(t, h.enqueue(hiveScope, 1, bad), envelope.Invalid, envelope.CodeSchemaViolation)
		refused(t, h.enqueue(hiveScope, 1), envelope.Invalid, envelope.CodeSchemaViolation)
	})
}

func TestEnqueueAppendsInOrderAfterWhatIsPending(t *testing.T) {
	h, _ := foldSeed(t)
	a := h.enqueued(note(t, "One.", nil), note(t, "Two.", nil))
	b := h.enqueued(note(t, "Three.", nil))
	if got := h.pending(); !slices.Equal(got, append(a, b...)) {
		t.Fatalf("pending %v, want %v", got, append(a, b...))
	}
	if h.queue()["record_revision"] != int64(3) {
		t.Errorf("queue %v", h.queue())
	}
}

func TestFoldDrainsTheWholeQueueInOrderInOneTransaction(t *testing.T) {
	h, subject := foldSeed(t)
	other := h.subject(hiveScope, "class.1")
	order := h.enqueued(
		note(t, "Fact for the first.", map[string]any{"subject_id": subject, "fact_id": "one"}),
		note(t, "Fact for the second.", map[string]any{"subject_id": other, "fact_id": "two"}),
	)
	order = append(order, h.enqueued(note(t, "Another fact for the first.", map[string]any{"subject_id": subject, "fact_id": "three"}))...)

	resp := h.fold(h.queue()["record_revision"].(int64))
	body := h.ok(resp)
	if !slices.Equal(strs(body["notes"]), order) || body["remaining"] != int64(0) {
		t.Fatalf("drained %v, want %v; remaining %v", body["notes"], order, body["remaining"])
	}
	if q := h.queue(); len(q["pending"].([]any)) != 0 || q["record_revision"] != int64(4) {
		t.Errorf("queue after the fold %v", q)
	}
	if body["queue_revision_before"] != int64(3) || body["queue_revision_after"] != int64(4) {
		t.Errorf("queue revisions %v to %v", body["queue_revision_before"], body["queue_revision_after"])
	}
	facts := h.factsOf(subject)
	want := map[string]string{"fact-a": "The first fact.", "one": "Fact for the first.", "three": "Another fact for the first."}
	if !equal(factsToAny(facts), factsToAny(want)) {
		t.Errorf("facts %v, want %v", facts, want)
	}
	var order3 []string
	for _, v := range h.stored("subject", subject)["facts"].([]any) {
		order3 = append(order3, v.(map[string]any)["fact_id"].(string))
	}
	if !slices.Equal(order3, []string{"fact-a", "one", "three"}) {
		t.Errorf("facts are filed in queue order: %v", order3)
	}
	if got := h.factsOf(other); got["two"] != "Fact for the second." {
		t.Errorf("facts of the second subject %v", got)
	}
	for _, id := range order {
		if s := h.stored("note", id)["state"]; s != "folded" {
			t.Errorf("note %s is %v", id, s)
		}
	}

	// A note enqueued after the transaction belongs to the next fold.
	later := h.enqueued(note(t, "Later.", map[string]any{"subject_id": subject, "fact_id": "later"}))
	next := h.ok(h.fold(h.queue()["record_revision"].(int64)))
	if !slices.Equal(strs(next["notes"]), later) {
		t.Errorf("the next fold drained %v, want %v", next["notes"], later)
	}
}

func factsToAny(m map[string]string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestFoldFilesAKnownSubjectsNoteAsItsFactAndRerendersItsProjection(t *testing.T) {
	h, subject := foldSeed(t)
	p, _ := h.rendered(hiveScope, "subject", "class-3", subject)
	pid := p["projection_id"].(string)
	before := current(p)["rendering"].(int64)

	ids := h.enqueued(note(t, "The counter resets after collection.", map[string]any{"subject_id": subject, "fact_id": "counter-reset"}))
	resp := h.fold(h.queue()["record_revision"].(int64))
	body := h.ok(resp)

	facts := body["facts"].([]any)
	if len(facts) != 1 || len(body["items"].([]any)) != 0 {
		t.Fatalf("receipt %v", body)
	}
	f := facts[0].(map[string]any)
	if f["note_id"] != ids[0] || f["subject_id"] != subject || f["fact_id"] != "counter-reset" {
		t.Errorf("fact %v", f)
	}
	stored := h.stored("subject", subject)
	filed := stored["facts"].([]any)
	got := filed[len(filed)-1].(map[string]any)
	if got["text"] != "The counter resets after collection." || got["fact_id"] != "counter-reset" || !equal(got["provenance"], h.stored("note", ids[0])["provenance"]) {
		t.Errorf("the fact filed is %v", got)
	}
	if stored["record_revision"] != int64(2) {
		t.Errorf("the subject is at revision %v", stored["record_revision"])
	}

	after := h.stored("projection", pid)
	cur := current(after)
	if cur["rendering"].(int64) <= before || !strings.Contains(cur["text"].(string), "- counter-reset: The counter resets after collection.\n") {
		t.Errorf("the projection was not rendered again with the fact: %v", cur)
	}
	if max := maxOf(h.budgetRecord(), pid); max == 0 || cur["bytes"].(int64) > max {
		t.Errorf("the projection is %v bytes under a budget of %d", cur["bytes"], max)
	}
	if state := h.freshness(pid)["state"]; state != "fresh" {
		t.Errorf("the projection is %v", state)
	}
	if k := kinds(resp); k[foldedKind] != 1 || k["pool.projection.rendered"] == 0 || k["pool.item.added"] != 0 {
		t.Errorf("receipts %v", k)
	}
	if n := h.stored("note", ids[0]); n["state"] != "folded" || n["fact_id"] != "counter-reset" || n["subject_id"] != subject {
		t.Errorf("the note is %v", n)
	}
}

func TestFoldMintsAMissingFactIdentityFromTheNote(t *testing.T) {
	h, subject := foldSeed(t)
	ids := h.enqueued(note(t, "A fact with no name of its own.", map[string]any{"subject_id": subject}))
	body := h.ok(h.fold(h.queue()["record_revision"].(int64)))
	want := "note-" + strings.TrimPrefix(ids[0], "mnote_")
	if f := body["facts"].([]any)[0].(map[string]any); f["fact_id"] != want {
		t.Errorf("fact identity %v, want %s", f["fact_id"], want)
	}
	if got := h.factsOf(subject)[want]; got != "A fact with no name of its own." {
		t.Errorf("facts %v", h.factsOf(subject))
	}
	if n := h.stored("note", ids[0]); n["fact_id"] != want {
		t.Errorf("the note carries the fact identity %v", n["fact_id"])
	}
}

func TestFoldRepeatedIdenticalFactIsIdempotent(t *testing.T) {
	h, subject := foldSeed(t)
	ids := h.enqueued(note(t, "The first fact.", map[string]any{"subject_id": subject, "fact_id": "fact-a"}))
	body := h.ok(h.fold(2))
	if len(body["facts"].([]any)) != 1 || len(body["items"].([]any)) != 0 {
		t.Fatalf("receipt %v", body)
	}
	if got := h.factsOf(subject); len(got) != 1 {
		t.Errorf("facts %v", got)
	}
	if n := h.stored("note", ids[0]); n["state"] != "folded" {
		t.Errorf("the note is %v", n)
	}
}

func TestFoldRoutesWhatCannotBeFiledToTheSupervisorAsPrey(t *testing.T) {
	h, subject := foldSeed(t)
	task := "task_01jc7d8e9f0g1h2j3k4m5n6p7t"
	big := strings.Repeat("A fact too long for its subject's budget. ", 300)
	missing := "subj_" + strings.TrimPrefix(mint(t, ids.Task), "task_")
	filing := h.subject(hiveScope, "filing.always")
	h.rule("always", filing, hiveScope, R{"rule_line": "Always do the same."})
	always, _ := h.rendered(hiveScope, "always", "hive", "")

	lone := note(t, "A note about nothing in particular.\nKept as it was.", map[string]any{"origin": "lictor-report"})
	lone["provenance"] = provenance(t, task)
	order := h.enqueuedAt(projectScope,
		lone,
		note(t, "A note for a subject nobody knows.", map[string]any{"subject_id": missing, "fact_id": "unknown"}),
		note(t, "Different words under a name that is taken.", map[string]any{"subject_id": subject, "fact_id": "fact-a"}),
		note(t, big, map[string]any{"subject_id": subject, "fact_id": "huge"}),
		note(t, "A fact that fits.", map[string]any{"subject_id": subject, "fact_id": "fits"}),
	)

	resp := h.fold(h.queue()["record_revision"].(int64))
	body := h.ok(resp)
	items := body["items"].([]any)
	reasons := map[string]string{}
	tasks := map[string]string{}
	for _, v := range items {
		m := v.(map[string]any)
		reasons[m["note_id"].(string)], tasks[m["note_id"].(string)] = m["reason"].(string), m["task_id"].(string)
	}
	want := map[string]string{order[0]: "subject-unknown", order[1]: "subject-unknown", order[2]: "fact-conflict", order[3]: "over-budget"}
	if !equal(strMap(reasons), strMap(want)) {
		t.Fatalf("reasons %v, want %v", reasons, want)
	}
	if facts := body["facts"].([]any); len(facts) != 1 || facts[0].(map[string]any)["note_id"] != order[4] {
		t.Fatalf("facts %v", facts)
	}
	if got := h.factsOf(subject); got["fits"] != "A fact that fits." || got["fact-a"] != "The first fact." || got["huge"] != "" || got["unknown"] != "" {
		t.Errorf("facts %v", got)
	}

	// Each routed note is a queued Prey for the supervisor, a Lictor's, at the
	// note's scope, with its exact text and evidence, and nothing invented.
	texts := map[string]string{
		order[0]: "A note about nothing in particular.\nKept as it was.",
		order[1]: "A note for a subject nobody knows.",
		order[2]: "Different words under a name that is taken.",
		order[3]: big,
	}
	for id, reason := range reasons {
		item := h.stored("item", tasks[id])
		n := h.stored("note", id)
		if item["state"] != "queued" || item["level"] != "prey" || item["deliverable"] != "lictor" || item["body"] != texts[id] {
			t.Errorf("%s: item %v", reason, item)
		}
		if item["scope_level"] != "project" || item["project_id"] != projectID || item["workspace_id"] != workspaceID {
			t.Errorf("%s: the item is not at the note's scope: %v", reason, item)
		}
		for _, m := range []string{"class", "priority", "delivery", "specification", "handoff", "legacy", "clone_id", "completed"} {
			if _, ok := item[m]; ok {
				t.Errorf("%s: the item carries a %s nobody gave it", reason, m)
			}
		}
		if item["placement_requested"] != "policy" || len(item["generations"].([]any)) != 0 || item["definition"] != "not-required" {
			t.Errorf("%s: placement, generations or definition invented: %v", reason, item)
		}
		links := item["links"].([]any)
		evidence := n["provenance"].(map[string]any)["evidence"].([]any)
		if len(links) != len(evidence) {
			t.Fatalf("%s: links %v, evidence %v", reason, links, evidence)
		}
		for i, l := range links {
			if m := l.(map[string]any); m["kind"] != "record" || !equal(m["ref"], evidence[i]) {
				t.Errorf("%s: link %v does not carry evidence %v", reason, m, evidence[i])
			}
		}
		deps := item["deps"].([]any)
		if id == order[0] {
			if len(deps) != 1 || deps[0].(map[string]any)["type"] != "discovered-from" || deps[0].(map[string]any)["task_id"] != task {
				t.Errorf("deps %v", deps)
			}
		} else if len(deps) != 0 {
			t.Errorf("%s: deps %v for a note that names no task", reason, deps)
		}
		if n["state"] != "routed" || n["disposition"].(map[string]any)["task_id"] != tasks[id] {
			t.Errorf("%s: note %v", reason, n)
		}
	}
	if k := kinds(resp); k["pool.item.added"] != 4 || k[foldedKind] != 1 {
		t.Errorf("receipts %v", k)
	}
	for _, r := range resp.Receipts {
		if r.Kind == "pool.item.added" && (r.Subject == nil || r.Subject.Schema != "pool.item" || r.Subject.RecordRevision != 1) {
			t.Errorf("item receipt %+v", r)
		}
	}

	// No note, whatever became of it, is a line of an always projection.
	a := h.stored("projection", always["projection_id"].(string))
	for _, text := range []string{texts[order[0]], texts[order[1]], texts[order[2]], "A fact that fits."} {
		if strings.Contains(current(a)["text"].(string), text) {
			t.Errorf("an always projection holds a note: %q", text)
		}
	}
	for rel := range h.records() {
		if !strings.HasPrefix(rel, "pool/records/projection/") {
			continue
		}
		p := h.stored("projection", strings.TrimSuffix(filepath.Base(rel), ".json"))
		if p["tier"] == "always" && (strings.Contains(current(p)["text"].(string), "fits") || strings.Contains(current(p)["text"].(string), "Kept as it was")) {
			t.Errorf("the always projection %v holds a note", p["projection_id"])
		}
	}
}

func strMap(m map[string]string) map[string]any { return factsToAny(m) }

func TestFoldAccountsForEveryDrainedNoteExactlyOnce(t *testing.T) {
	h, subject := foldSeed(t)
	order := h.enqueued(
		note(t, "Filed.", map[string]any{"subject_id": subject, "fact_id": "filed"}),
		note(t, "Routed.", nil),
		note(t, "Filed too.", map[string]any{"subject_id": subject}),
	)
	resp := h.fold(h.queue()["record_revision"].(int64))
	body := h.ok(resp)
	r := receiptOf(t, resp, foldedKind)
	if !equal(r.Body, body) {
		t.Errorf("the response body is not the receipt's body")
	}
	var seen []string
	for _, f := range body["facts"].([]any) {
		seen = append(seen, f.(map[string]any)["note_id"].(string))
	}
	for _, i := range body["items"].([]any) {
		seen = append(seen, i.(map[string]any)["note_id"].(string))
	}
	slices.Sort(seen)
	if want := slices.Sorted(slices.Values(order)); !slices.Equal(seen, want) || !slices.Equal(strs(body["notes"]), order) {
		t.Errorf("accounted for %v of %v; notes %v", seen, order, body["notes"])
	}
	if body["remaining"] != int64(0) {
		t.Errorf("remaining %v", body["remaining"])
	}
	if r.Subject == nil || r.Subject.Schema != queueSchema || r.Subject.ID != queueID || r.Subject.RecordRevision != body["queue_revision_after"] || r.Subject.RecordRevision != h.queue()["record_revision"] {
		t.Errorf("the receipt's subject is %+v", r.Subject)
	}
	// Each note names the receipt in the transaction that removed it from the queue.
	for _, id := range order {
		n := h.stored("note", id)
		fold := n["disposition"].(map[string]any)["fold"].(map[string]any)
		if fold["schema"] != "hive.receipt" || fold["id"] != r.ReceiptID {
			t.Errorf("note %s names %v, want the receipt %s", id, fold, r.ReceiptID)
		}
		if n["state"] == "folded" && n["disposition"].(map[string]any)["task_id"] != nil {
			t.Errorf("a folded note names an item: %v", n)
		}
	}
	if len(h.pending()) != 0 {
		t.Errorf("pending %v", h.pending())
	}
}

func TestFoldOfAMalformedNoteFailsTheWholeTransaction(t *testing.T) {
	h, subject := foldSeed(t)
	ids := h.enqueued(
		note(t, "Would be filed.", map[string]any{"subject_id": subject, "fact_id": "would"}),
		note(t, "Is malformed.", map[string]any{"subject_id": subject, "fact_id": "broken"}),
	)
	broken := h.stored("note", ids[1])
	broken["state"] = "folded" // a folded note carries its disposition, and this does not
	h.plant("note", ids[1], broken)

	h.unchanged(func() {
		refused(t, h.fold(2), envelope.Unavailable, envelope.CodeRecordMalformed)
	})
	if got := h.pending(); !slices.Equal(got, ids) || h.queue()["record_revision"] != int64(2) {
		t.Errorf("queue %v", h.queue())
	}
	if got := h.factsOf(subject); len(got) != 1 {
		t.Errorf("facts %v", got)
	}

	// A pending identity with no note behind it is as malformed.
	if err := os.Remove(filepath.Join(h.dir, "pool", "records", "note", ids[1]+".json")); err != nil {
		t.Fatal(err)
	}
	h.unchanged(func() {
		refused(t, h.fold(2), envelope.Unavailable, envelope.CodeRecordMalformed)
	})
}

func TestFoldOfAnEmptyQueueGivesAnEmptyReceiptAndRaisesNothing(t *testing.T) {
	h, _ := foldSeed(t)
	before := h.queue()
	data := h.document("fold", map[string]any{}, hiveScope, 1)
	resp := h.rawCall("hive-memory", "fold", data)
	body := h.ok(resp)
	for _, m := range []string{"notes", "facts", "items"} {
		if l := body[m].([]any); len(l) != 0 {
			t.Errorf("%s %v", m, l)
		}
	}
	if body["queue_revision_before"] != int64(1) || body["queue_revision_after"] != int64(1) || body["remaining"] != int64(0) {
		t.Errorf("receipt %v", body)
	}
	if !equal(h.queue(), before) {
		t.Errorf("the queue changed: %v", h.queue())
	}
	r := receiptOf(t, resp, foldedKind)
	if len(resp.Receipts) != 1 || r.Subject == nil || r.Subject.ID != queueID || r.Subject.RecordRevision != 1 {
		t.Errorf("receipts %v", resp.Receipts)
	}
	again := h.rawCall("hive-memory", "fold", data)
	if !again.Replayed || receiptOf(t, again, foldedKind).ReceiptID != r.ReceiptID {
		t.Errorf("a replay returns the receipt: replayed %v", again.Replayed)
	}
}

func TestFoldNeedsTheQueueAtItsRevision(t *testing.T) {
	h, subject := foldSeed(t)
	h.enqueued(note(t, "Waiting.", map[string]any{"subject_id": subject}))
	h.unchanged(func() {
		refused(t, h.fold(1), envelope.Conflict, envelope.CodeRevisionConflict)
		refused(t, h.fold(0), envelope.Invalid, envelope.CodeSchemaViolation)
	})
}

func TestFoldAndEnqueueNeedAQueue(t *testing.T) {
	h, _ := seed(t)
	refused(t, h.fold(1), envelope.Conflict, envelope.CodeRevisionConflict)
	refused(t, h.enqueue(hiveScope, 1, note(t, "Nowhere to go.", nil)), envelope.Conflict, envelope.CodeRevisionConflict)
}

func TestHivePoolReadsNotesAndTheQueue(t *testing.T) {
	h, _ := foldSeed(t)
	ids := h.enqueued(note(t, "One.", nil), note(t, "Two.", nil))
	read := func(kind string, selector map[string]any) *envelope.Response {
		return h.send("hive-pool", "read", map[string]any{"kind": kind, "selector": selector}, request{})
	}
	body := h.ok(read("note", map[string]any{"id": ids[1]}))
	if recs := body["records"].([]any); len(recs) != 1 || recs[0].(map[string]any)["note_id"] != ids[1] {
		t.Errorf("records %v", body["records"])
	}
	if recs := h.ok(read("note", map[string]any{}))["records"].([]any); len(recs) != 2 {
		t.Errorf("records %v", recs)
	}
	if recs := h.ok(read("note", map[string]any{"limit": int64(1)}))["records"].([]any); len(recs) != 1 {
		t.Errorf("records %v", recs)
	}
	q := h.ok(read(queueKind, map[string]any{"id": queueID}))["records"].([]any)
	if len(q) != 1 || !slices.Equal(strs(q[0].(map[string]any)["pending"]), ids) {
		t.Errorf("queue %v", q)
	}
	refused(t, read("note", map[string]any{"id": "mnote_01000000000000000000000000"}), envelope.NotFound, envelope.CodeNotFound)
	for _, m := range []map[string]any{{"states": []any{"pending"}}, {"ready": true}, {"active": true}, {"task_id": "task_01jc7d8e9f0g1h2j3k4m5n6p7t"}} {
		refused(t, read("note", m), envelope.Invalid, envelope.CodeSchemaViolation)
		refused(t, read(queueKind, m), envelope.Invalid, envelope.CodeSchemaViolation)
	}
}

// enqueue and fold are new operations, declared at 1.0 with v1 schemas: a
// request at another version is refused before its body is read.
func TestEnqueueAndFoldAcceptOnlyVersionOne(t *testing.T) {
	h, _ := foldSeed(t)
	for _, operation := range []string{"enqueue", "fold"} {
		resp := h.send("hive-memory", operation, map[string]any{}, request{version: "2.0"})
		if resp.Status != envelope.Invalid || resp.Error == nil || resp.Error.Code != envelope.CodeUnsupportedVersion {
			t.Errorf("%s at 2.0: status %s, error %+v; want unsupported_version", operation, resp.Status, resp.Error)
		}
	}
}
