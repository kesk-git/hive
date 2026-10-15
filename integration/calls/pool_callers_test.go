package calls

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/declare"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/schema"
)

// The Pool read by the packages that depend on it (spec/pool.md, Reading the
// Pool, and the table of its readers; spec/packages.md, Calls between
// packages). Each of dispatch, shadow and ui is stood in for by a caller in
// this file that makes only the calls its row of the table gives it, the way
// the package's code would: by the built hive-pool executable, found by name,
// with a request document that names the package and its command. The home is
// built by the tests through the same executables; every record a caller
// receives is held to the response schema, to the schema of its kind, and to
// the record the Pool keeps, so that "whole" is checked against what is stored
// and not against what the caller was told.

// pcSchemas is the validator of the specification's schemas.
func pcSchemas(t testing.TB) *schema.Registry {
	t.Helper()
	reg, err := schema.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// pcKind describes a kind a caller reads: the schema of its record, the member
// that is its identity, and whether the Pool stores it as a file of its own.
type pcKind struct {
	schema, identity string
}

var pcKinds = map[string]pcKind{
	"item":           {"urn:hive:schema:records:pool.item:v1", "task_id"},
	"hold":           {"urn:hive:schema:records:pool.hold:v1", "hold_id"},
	"admission":      {"urn:hive:schema:records:pool.admission:v1", "admission_id"},
	"specification":  {"urn:hive:schema:records:pool.specification:v1", "specification_id"},
	"rule":           {"urn:hive:schema:records:pool.rule:v2", "rule_id"},
	"projection":     {"urn:hive:schema:records:pool.projection:v2", "projection_id"},
	"question":       {"urn:hive:schema:records:pool.question:v1", "question_id"},
	"decision-round": {"urn:hive:schema:records:pool.decision-round:v1", "decision_round_id"},
}

// The stand-in callers, as the Pool's request names them: the package and its
// executable, from the declarations.
func pcFrom(t testing.TB, pkg string) asPackage {
	t.Helper()
	d := declarations(t)[pkg]
	if d == nil {
		t.Fatalf("no package %s is declared", pkg)
	}
	return asPackage{d.Name, d.Executable}
}

// pcWorld is a home with the requests a test makes in it.
type pcWorld struct {
	*wpFixture
}

func newPcWorld(t testing.TB) *pcWorld {
	t.Helper()
	return &pcWorld{newWpFixture(t)}
}

// send makes a request as the test, as the home's builder, to the command and
// operation, and requires it to end ok.
func (w *pcWorld) send(command, operation string, body map[string]any, expected ...envelope.ExpectedRevision) map[string]any {
	w.t.Helper()
	req := wpRequest(w.t, wpCall{command: command, operation: operation, n: w.ids.take(), caller: wpTest, body: body, expected: expected})
	r := call{program: command, operation: operation, home: w.home, req: req}.run(w.t)
	return wpOK(w.t, command+" "+operation, r).Body
}

// pcStamp writes an instant as a request carries it.
func pcStamp(at time.Time) string { return at.UTC().Format("2006-01-02T15:04:05.000Z") }

// plantBudget stands the budget configuration in the Pool, as the provisioning
// of a home leaves it (spec/pool.md, the budget record); the rule store and
// the projections read it, and no built operation writes the first one.
func (w *pcWorld) plantBudget() {
	w.t.Helper()
	dir := filepath.Join(w.home, "pool", "records", "budget")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		w.t.Fatal(err)
	}
	const at = "2026-10-11T08:00:00.000Z"
	rec := map[string]any{
		"schema": "pool.budget", "schema_version": "1.0", "record_revision": int64(1), "budget_id": "memory",
		"home_id":     testHome,
		"defaults":    map[string]any{"hive_always": int64(18432), "fleet_always": int64(4608), "subject": int64(9216)},
		"projections": []any{}, "changes": []any{},
		"created_at": at, "updated_at": at,
		"writer": map[string]any{"package": "dispatch", "command": "hive-dispatch", "operation": "create-home", "implementation_version": "test"},
	}
	if err := os.WriteFile(filepath.Join(dir, "memory.json"), encodeObject(w.t, rec), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

// pcDelivery is a delivery that agrees with the Output of pcImperative.
var pcDelivery = map[string]any{"validation": "direct", "publication": "local-only", "assimilation": "word"}

// add adds an item through hive-pool and returns it.
func (w *pcWorld) add(body map[string]any) map[string]any {
	w.t.Helper()
	item, _ := w.send("hive-pool", "add", body)["item"].(map[string]any)
	if item == nil {
		w.t.Fatal("add returned no item")
	}
	return item
}

// addID adds an item and returns its identity.
func (w *pcWorld) addID(body map[string]any) string {
	w.t.Helper()
	id, _ := w.add(body)["task_id"].(string)
	return id
}

// pcLeaf is the body of a Prey with no Hunt, with the edges given, that asks
// nothing of the Pool.
func pcLeaf(title string, deps ...map[string]any) map[string]any {
	body := map[string]any{"title": title, "level": "prey", "deliverable": "swarm"}
	if len(deps) > 0 {
		list := []any{}
		for _, d := range deps {
			list = append(list, d)
		}
		body["deps"] = list
	}
	return body
}

// pcEdge is a dependency edge of the type to the item.
func pcEdge(kind, task string) map[string]any { return map[string]any{"type": kind, "task_id": task} }

// done moves the leaf to done at the instant.
func (w *pcWorld) done(task, at string) {
	w.t.Helper()
	w.send("hive-pool", "transition", map[string]any{"task_id": task, "to": "done", "at": at})
}

// placeHold places a hold on the item and returns its identity.
func (w *pcWorld) placeHold(task, kind string, until string) string {
	w.t.Helper()
	body := map[string]any{"task_id": task, "kind": kind, "reason": "A reason."}
	if until != "" {
		body["until"] = map[string]any{"at": until, "zone": "UTC"}
	}
	hold, _ := w.send("hive-pool", "hold", body)["hold"].(map[string]any)
	id, _ := hold["hold_id"].(string)
	if id == "" {
		w.t.Fatal("hold returned no hold")
	}
	return id
}

// pcSpec is an accepted specification.
type pcSpec struct {
	id       string
	revision int64
	digest   string
}

func (s pcSpec) ref() map[string]any {
	return map[string]any{"specification_id": s.id, "revision": s.revision, "digest": s.digest}
}

// specify defines a specification for the item and has it accepted for it.
func (w *pcWorld) specify(task string) pcSpec {
	w.t.Helper()
	defined := w.send("hive-spec", "define", map[string]any{
		"title": "A specification", "text": "The counter resets after its sample is collected.",
		"criteria": []any{map[string]any{
			"criterion_id": "counter-resets", "text": "The counter resets.", "evidence": "A test reads the counter twice.",
		}},
		"task_ids": []any{task}, "touched_subjects": []any{},
	})
	rec, _ := defined["specification"].(map[string]any)
	revisions, _ := rec["revisions"].([]any)
	if len(revisions) != 1 {
		w.t.Fatalf("define returned %d revisions", len(revisions))
	}
	rev, _ := revisions[0].(map[string]any)
	s := pcSpec{id: fmt.Sprint(rec["specification_id"]), revision: rev["revision"].(int64), digest: fmt.Sprint(rev["digest"])}
	w.send("hive-spec", "accept", map[string]any{
		"specification_id": s.id, "revision": s.revision, "task_id": task,
		"need_test":              map[string]any{"outcome": "The counter resets.", "coverage": "new-definition", "reason": "No test covers it yet."},
		"open_product_questions": []any{},
	})
	return s
}

// pcImperative is the body of the creation of the Imperative of the item, whose
// Standard names the criteria.
func pcImperative(task string, criteria map[string]any) map[string]any {
	high := func() map[string]any { return map[string]any{"level": "high"} }
	return map[string]any{
		"task_id": task, "class": int64(3),
		"task": map[string]any{
			"intent": "Make the nightly export stop retrying forever when the vendor is down.",
			"object": map[string]any{
				"text":       "The nightly export job of the widget service.",
				"artifact":   "The retry loop in the export worker.",
				"confidence": high(),
			},
			"standard": map[string]any{
				"conditions":   "The retry path gives up after three attempts and leaves the queue unchanged.",
				"evidence":     []any{"The test log of the retry package.", "The diff of the queue worker."},
				"criteria":     criteria,
				"rules_digest": "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				"confidence":   high(),
			},
		},
		"setup": map[string]any{
			"base_branch": "main", "base_revision": "3f5a9c1e7b2d4860a1c3e5f708192a3b4c5d6e7f",
			"source_branch": "main", "output_branch_rule": "A branch named hive/<task> cut from the source branch.",
			"role": "lictor", "harness": "policy",
			"dependencies": []any{"The vendor sandbox is reachable."},
			"tools":        []any{"git", "go-test"},
			"access":       map[string]any{"credentials": []any{}},
			"isolation":    []any{"The working directory is the clone root."},
			"repository":   map[string]any{"kind": "local-only"},
		},
		"rules": map[string]any{
			"boundary": map[string]any{
				"scope":           "The export worker and its tests.",
				"exclusions":      []any{"The vendor client library."},
				"allowed_effects": []any{"Edit files in the clone.", "Run the package tests."},
				"resources":       "One CPU hour of local test time.",
				"permissions":     "Read and write inside the clone only.",
				"stop_conditions": []any{"A test outside the worker fails for an unrelated reason."},
				"repair_scope":    "The worker and its own tests.",
				"retry_limit":     int64(3),
				"sensitive":       []any{},
				"confidence":      high(),
			},
			"validation_commands": []any{map[string]any{"name": "export-tests", "argv": []any{"go", "test", "./export/..."}}},
			"channels": map[string]any{
				"delivery": "Commit to the output branch.", "decisions": "Ask the supervisor through a question.",
				"blocker_owner": "The supervising session.",
			},
		},
		"definition_of_done": map[string]any{
			"output": map[string]any{
				"deliverable": "A branch holding the retry change and its tests.", "recipient": "The supervising session.",
				"validation": "direct", "publication": "local-only",
				"completion_evidence": []any{"The passing test output."}, "confidence": high(),
			},
			"stopping_point": "The branch is pushed and the evidence is written; the worker stops there.",
		},
	}
}

// admit creates the item's Imperative, requests an admission and checks it
// until it is admitted. It returns the admission's identity.
func (w *pcWorld) admit(task string, s pcSpec) string {
	w.t.Helper()
	w.send("hive-imperative", "create", pcImperative(task, s.ref()))
	created, _ := w.send("hive-admit", "create", map[string]any{
		"task_id": task, "imperative_revision": int64(1), "words": "Dispatch the work as written.",
	})["admission"].(map[string]any)
	id, _ := created["admission_id"].(string)
	checked := w.send("hive-admit", "check", map[string]any{
		"admission_id": id, "placement": map[string]any{"kind": "requested", "value": "mac"},
	})
	if adm, _ := checked["admission"].(map[string]any); adm["state"] != "admitted" {
		w.t.Fatalf("the admission is %v, want admitted", adm["state"])
	}
	return id
}

// openGeneration opens the first generation of the admitted item.
func (w *pcWorld) openGeneration(task, admission string) {
	w.t.Helper()
	w.send("hive-pool", "open-generation", map[string]any{"task_id": task, "admission_id": admission})
}

// pcProjection is what a caller received from one read.
type pcProjection struct {
	kind    string
	body    map[string]any
	header  map[string]any
	cursor  int64
	records []map[string]any
	derived []map[string]any
	// omitted names the records the Pool could not read, by identity and reason.
	omitted map[string]string
}

func pcMaps(v any) []map[string]any {
	list, _ := v.([]any)
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		m, _ := e.(map[string]any)
		out = append(out, m)
	}
	return out
}

func pcMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

// ids are the identities of the records, in the order read.
func (p pcProjection) ids() []string {
	out := []string{}
	for _, r := range p.records {
		out = append(out, fmt.Sprint(r[pcKinds[p.kind].identity]))
	}
	return out
}

// sorted is ids in order.
func (p pcProjection) sorted() []string {
	out := p.ids()
	slices.Sort(out)
	return out
}

// record is the record of the identity, and nil where the read has none.
func (p pcProjection) record(id string) map[string]any {
	for _, r := range p.records {
		if fmt.Sprint(r[pcKinds[p.kind].identity]) == id {
			return r
		}
	}
	return nil
}

// fact is the derived facts of the item or the freshness of the projection of
// the identity.
func (p pcProjection) fact(id string) map[string]any {
	for i, r := range p.records {
		if fmt.Sprint(r[pcKinds[p.kind].identity]) == id && i < len(p.derived) {
			return p.derived[i]
		}
	}
	return nil
}

// pcSorted is a sorted copy.
func pcSorted(ids ...string) []string {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

// stored is the record the Pool keeps for the identity, as the file holds it.
func (w *pcWorld) stored(kind, id string) map[string]any {
	w.t.Helper()
	data, err := os.ReadFile(filepath.Join(w.home, "pool", "records", kind, id+".json"))
	if err != nil {
		w.t.Fatalf("the Pool keeps no %s %s: %v", kind, id, err)
	}
	return parseObject(w.t, data)
}

// readAs makes the read as the package and holds what it receives to the
// contract of a projection: the exchange is a complete response of hive-pool
// that echoes the request, its body is the schema's, every record is of the
// kind asked for and whole as the Pool stores it, and the header names the
// home, the time of the read and the journal cursor. It does not require the
// read to be complete.
func (w *pcWorld) readAs(from asPackage, kind string, selector map[string]any) pcProjection {
	w.t.Helper()
	t := w.t
	req := newRequest(t, from, "hive-pool", "read", w.ids.take(), map[string]any{"kind": kind, "selector": selector})
	before := time.Now().Add(-time.Millisecond)
	r := call{program: "hive-pool", operation: "read", home: w.home, req: req}.run(t)
	after := time.Now().Add(time.Millisecond)
	wantAccepted(t, r, req.id, declarations(t)["pool"], "hive-pool", "read", false)
	resp := r.must(t)
	if r.result.Code != 0 || resp.Status != envelope.Ok {
		t.Fatalf("read %s: exit code %d, status %q, error %+v; want 0, ok", kind, r.result.Code, resp.Status, resp.Error)
	}
	if len(resp.Receipts) != 0 {
		t.Errorf("a read carries %d receipts", len(resp.Receipts))
	}
	data, err := canon.Encode(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := pcSchemas(t).Validate("urn:hive:schema:ops:hive-pool.read.response:v1", data); err != nil {
		t.Errorf("the body of the read breaks its response schema: %v", err)
	}
	p := pcProjection{kind: kind, body: resp.Body, header: pcMap(resp.Body["projection"]), omitted: map[string]string{}}
	if resp.Body["kind"] != kind {
		t.Errorf("the response is for kind %v, want %s", resp.Body["kind"], kind)
	}

	// The projection header.
	if p.header["home_id"] != testHome {
		t.Errorf("the header names home %v, want %s", p.header["home_id"], testHome)
	}
	at, err := time.Parse(time.RFC3339Nano, fmt.Sprint(p.header["read_at"]))
	if err != nil || at.Before(before) || at.After(after) {
		t.Errorf("read_at = %v (%v), want an instant between %s and %s", p.header["read_at"], err, before.UTC().Format(time.RFC3339Nano), after.UTC().Format(time.RFC3339Nano))
	}
	cursor, ok := p.header["journal_cursor"].(int64)
	if !ok || cursor < 0 {
		t.Errorf("journal_cursor = %v, want a count of events", p.header["journal_cursor"])
	}
	p.cursor = cursor
	if _, ok := p.header["complete"].(bool); !ok {
		t.Errorf("the header says nothing of whether the read was complete: %v", p.header)
	}
	for _, o := range pcMaps(p.header["omitted"]) {
		p.omitted[fmt.Sprint(o["id"])] = fmt.Sprint(o["reason"])
	}
	if p.header["complete"] == true && len(p.omitted) != 0 {
		t.Errorf("a read that names omitted records says it is complete: %v", p.omitted)
	}
	if p.header["complete"] == false && len(p.omitted) == 0 {
		t.Errorf("an incomplete read does not say what is missing")
	}

	// Every record whole.
	p.records = pcMaps(resp.Body["records"])
	p.derived = pcMaps(resp.Body["derived"])
	k, known := pcKinds[kind]
	if !known {
		t.Fatalf("pool_callers_test.go has no description of the kind %s", kind)
	}
	for _, rec := range p.records {
		id := fmt.Sprint(rec[k.identity])
		enc, err := canon.Encode(rec)
		if err != nil {
			t.Fatal(err)
		}
		if err := pcSchemas(t).Validate(k.schema, enc); err != nil {
			t.Errorf("the %s %s breaks the schema of its kind: %v", kind, id, err)
		}
		if !wpSame(t, rec, w.stored(kind, id)) {
			t.Errorf("the %s %s is not the record the Pool keeps", kind, id)
		}
	}
	switch kind {
	case "item", "projection":
		if len(p.derived) != len(p.records) {
			t.Fatalf("%d derived entries for %d %s records", len(p.derived), len(p.records), kind)
		}
		member := "task_id"
		if kind == "projection" {
			member = "projection_id"
		}
		for i, d := range p.derived {
			if d[member] != p.records[i][k.identity] {
				t.Errorf("derived entry %d is for %v, want %v", i, d[member], p.records[i][k.identity])
			}
		}
	default:
		if len(p.derived) != 0 {
			t.Errorf("a read of %s carries %d derived entries", kind, len(p.derived))
		}
	}
	return p
}

// pcCaller is the stand-in code of one dependent package: it reads the Pool
// only through the calls its row of the Pool's table of readers gives it. A
// call outside them fails the test, and the calls it made are kept, so that a
// test can hold them to the row.
type pcCaller struct {
	w       *pcWorld
	from    asPackage
	allowed []string
	made    map[string]bool
}

func (w *pcWorld) caller(pkg string, allowed ...string) *pcCaller {
	return &pcCaller{w: w, from: pcFrom(w.t, pkg), allowed: allowed, made: map[string]bool{}}
}

// use records a call, and fails the test when the row does not give it.
func (c *pcCaller) use(call string) {
	c.w.t.Helper()
	if !slices.Contains(c.allowed, call) {
		c.w.t.Fatalf("the package %s is not given the call %q by the Pool's table of readers", c.from.pkg, call)
	}
	c.made[call] = true
}

// read reads the kind with the selector and requires the read to be complete.
func (c *pcCaller) read(kind string, selector map[string]any) pcProjection {
	c.w.t.Helper()
	p := c.readAny(kind, selector)
	if p.header["complete"] != true {
		c.w.t.Fatalf("read %s %v is not complete: %v", kind, selector, p.omitted)
	}
	return p
}

// readAny reads the kind with the selector and returns it complete or not.
func (c *pcCaller) readAny(kind string, selector map[string]any) pcProjection {
	c.w.t.Helper()
	c.use("hive-pool read " + kind)
	return c.w.readAs(c.from, kind, selector)
}

// called lists the calls the caller made, sorted.
func (c *pcCaller) called() []string {
	out := []string{}
	for k := range c.made {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// pcCalls fails the test unless the caller made exactly the calls given.
func pcCalls(t testing.TB, c *pcCaller, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := c.called(); !slices.Equal(got, want) {
		t.Errorf("%s made the calls %v, want %v", c.from.pkg, got, want)
	}
}

// state is the state of the home: every file of it, with its digest.
func (w *pcWorld) state() map[string]string { return snapshot(w.t, w.home) }

// TestTheCallersCallOnlyThePackagesTheirDeclarationsList proves that the three
// stand-in callers may call the Pool: the declarations of dispatch, shadow and
// ui list pool, and the calls they make here are to the executable of that
// package (and, for dispatch, to the Imperative's render, a command of the
// same package).
//
// It catches a caller added here for a package that may not call the Pool.
func TestTheCallersCallOnlyThePackagesTheirDeclarationsList(t *testing.T) {
	decls := declarations(t)
	pool := decls["pool"]
	for _, pkg := range []string{"dispatch", "shadow", "ui"} {
		d := decls[pkg]
		if !slices.Contains(d.Calls, "pool") {
			t.Errorf("%s does not declare a call to pool: %v", pkg, d.Calls)
		}
		from := pcFrom(t, pkg)
		if !slices.ContainsFunc(d.Commands, func(c declare.Command) bool { return c.Name == from.command }) {
			t.Errorf("%s does not own the command %s the stand-in caller names", pkg, from.command)
		}
	}
	for _, name := range []string{"hive-pool", "hive-imperative", "hive-memory"} {
		if !slices.ContainsFunc(pool.Commands, func(c declare.Command) bool { return c.Name == name }) {
			t.Errorf("pool does not own the command %s the callers call", name)
		}
	}
}

// pcAdmitted is an item the dispatch caller dispatches: its admission, the
// specification accepted for it and the extra admission requested after.
type pcAdmitted struct {
	task      string
	spec      pcSpec
	admission string
	// unchecked is a second admission of the item that no check has run on.
	unchecked string
}

// admittedItem builds the item dispatch dispatches: a Prey with no Hunt whose
// specification is accepted, with its Imperative and an admission in state
// admitted, and a second admission requested and never checked.
func (w *pcWorld) admittedItem() pcAdmitted {
	w.t.Helper()
	a := pcAdmitted{}
	a.task = w.addID(map[string]any{
		"title": "The item dispatch dispatches", "level": "prey", "deliverable": "swarm",
		"placement_requested": "mac", "specification_required": true, "delivery": pcDelivery,
	})
	a.spec = w.specify(a.task)
	a.admission = w.admit(a.task, a.spec)
	created, _ := w.send("hive-admit", "create", map[string]any{
		"task_id": a.task, "imperative_revision": int64(1), "words": "Dispatch it again.",
	})["admission"].(map[string]any)
	a.unchecked, _ = created["admission_id"].(string)
	return a
}

// TestDispatchCallerReadsTheItemItsAdmissionAndTheAcceptedSpecification is the
// proof of the row of the Pool's table of readers that gives dispatch "the item
// it dispatches, its admission in state `admitted` and the accepted
// specification revision; the Imperative by `render`", by `read` of kinds item,
// admission and specification and by `hive-imperative render`.
//
// The caller reads the item by its identity, the admissions of the item by
// query and takes the one in state admitted, reads the specification that
// admission checked against, and renders the Imperative. Every record comes
// back whole under a projection header, and the four calls change nothing in
// the home.
//
// It catches a read that returns a record cut or changed on the way, a
// projection header that names another home or a cursor that moves without a
// write, an admission the caller cannot tell from the one in state admitted, a
// specification revision other than the one accepted for the item, and a
// rendering that is not the Imperative of the admitted revision.
func TestDispatchCallerReadsTheItemItsAdmissionAndTheAcceptedSpecification(t *testing.T) {
	w := newPcWorld(t)
	a := w.admittedItem()
	w.addID(pcLeaf("An item no one dispatches"))
	d := w.caller("dispatch", "hive-pool read item", "hive-pool read admission", "hive-pool read specification", "hive-imperative render")
	before := w.state()

	// The item it dispatches, by identity, with its derived facts.
	item := d.read("item", map[string]any{"id": a.task})
	if got := item.ids(); !slices.Equal(got, []string{a.task}) {
		t.Fatalf("the item read by identity returned %v, want just %s", got, a.task)
	}
	rec := item.record(a.task)
	if rec["state"] != "queued" || rec["definition"] != "accepted" || rec["placement_requested"] != "mac" || rec["level"] != "prey" {
		t.Errorf("the item as read: state %v, definition %v, place %v, level %v", rec["state"], rec["definition"], rec["placement_requested"], rec["level"])
	}
	bound := pcMap(rec["specification"])
	if bound["specification_id"] != a.spec.id || bound["revision"] != a.spec.revision || bound["digest"] != a.spec.digest {
		t.Errorf("the item is bound to %v, want %+v", bound, a.spec)
	}
	facts := item.fact(a.task)
	if facts["ready"] != true || facts["blocked"] != false || facts["held"] != false || facts["landed"] != false {
		t.Errorf("the derived facts of the item: %v", facts)
	}

	// Its admissions, by query: the one in state admitted is the one checked.
	admissions := d.read("admission", map[string]any{"task_id": a.task})
	if got, want := admissions.sorted(), pcSorted(a.admission, a.unchecked); !slices.Equal(got, want) {
		t.Fatalf("the admissions of the item are %v, want %v", got, want)
	}
	var admitted []map[string]any
	for _, r := range admissions.records {
		if r["state"] == "admitted" {
			admitted = append(admitted, r)
		}
	}
	if len(admitted) != 1 || admitted[0]["admission_id"] != a.admission {
		t.Fatalf("the admissions in state admitted are %v, want just %s", admitted, a.admission)
	}
	if other := admissions.record(a.unchecked); other["state"] != "created" {
		t.Errorf("the admission never checked is in state %v, want created", other["state"])
	}
	adm := admitted[0]
	checks := pcMaps(adm["checks"])
	if len(checks) != 1 || checks[0]["result"] != "admitted" {
		t.Fatalf("the checks of the admitted admission: %v", adm["checks"])
	}
	checked := pcMap(checks[0]["specification"])
	if checked["specification_id"] != a.spec.id || checked["revision"] != a.spec.revision || checked["digest"] != a.spec.digest {
		t.Errorf("the admission checked specification %v, want %+v", checked, a.spec)
	}

	// The specification revision the admission checked, accepted for the item.
	specification := d.read("specification", map[string]any{"id": fmt.Sprint(checked["specification_id"])})
	spec := specification.record(a.spec.id)
	if spec == nil || len(specification.records) != 1 {
		t.Fatalf("the specification read by identity returned %v", specification.ids())
	}
	var accepted bool
	for _, acc := range pcMaps(spec["acceptances"]) {
		if acc["revision"] == a.spec.revision && acc["task_id"] == a.task && acc["state"] == "accepted" && acc["digest"] == a.spec.digest {
			accepted = true
		}
	}
	if !accepted {
		t.Errorf("the specification holds no acceptance of revision %d for %s: %v", a.spec.revision, a.task, spec["acceptances"])
	}
	var revision map[string]any
	for _, r := range pcMaps(spec["revisions"]) {
		if r["revision"] == a.spec.revision {
			revision = r
		}
	}
	if revision == nil || revision["digest"] != a.spec.digest {
		t.Errorf("the specification holds no revision %d at the digest %s: %v", a.spec.revision, a.spec.digest, spec["revisions"])
	}

	// The Imperative, by render.
	d.use("hive-imperative render")
	first := w.render(d.from, a.task)
	second := w.render(d.from, a.task)
	if first["revision"] != adm["imperative_revision"] || first["revision_digest"] != adm["imperative_digest"] {
		t.Errorf("the rendering is of revision %v at %v; the admission checked revision %v at %v", first["revision"], first["revision_digest"], adm["imperative_revision"], adm["imperative_digest"])
	}
	criteria := pcMap(first["criteria"])
	if criteria["specification_id"] != a.spec.id || criteria["revision"] != a.spec.revision || criteria["digest"] != a.spec.digest {
		t.Errorf("the rendering carries the criteria %v, want %+v", criteria, a.spec)
	}
	text, _ := first["text"].(string)
	if sum := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(text))); text == "" || first["text_bytes"] != int64(len(text)) || first["text_digest"] != sum {
		t.Errorf("the rendering's text is %d bytes with digest %s; it declares %v bytes and %v", len(text), sum, first["text_bytes"], first["text_digest"])
	}
	if second["text_digest"] != first["text_digest"] || second["text"] != first["text"] {
		t.Error("the same revision rendered twice gave two renderings")
	}
	launch := pcMap(first["launch_parameters"])
	if launch["placement_requested"] != "mac" || launch["publication"] != "local-only" || launch["validation"] != "direct" {
		t.Errorf("the launch parameters %v do not carry the item's place and delivery", launch)
	}

	// Every projection was read at one cursor, and the calls wrote nothing.
	cursors := []int64{item.cursor, admissions.cursor, specification.cursor}
	if cursors[0] == 0 || cursors[0] != cursors[1] || cursors[1] != cursors[2] {
		t.Errorf("the journal cursors of reads with no write between them are %v", cursors)
	}
	sameSnapshot(t, "the dispatch caller's reads", before, w.state())
	pcCalls(t, d, "hive-pool read item", "hive-pool read admission", "hive-pool read specification", "hive-imperative render")
}

// render sends hive-imperative render for the item as the package and returns
// the rendering.
func (w *pcWorld) render(from asPackage, task string) map[string]any {
	w.t.Helper()
	t := w.t
	req := newRequest(t, from, "hive-imperative", "render", w.ids.take(), map[string]any{"task_id": task})
	r := call{program: "hive-imperative", operation: "render", home: w.home, req: req}.run(t)
	wantAccepted(t, r, req.id, declarations(t)["pool"], "hive-imperative", "render", false)
	resp := wpOK(t, "render", r)
	data, err := canon.Encode(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := pcSchemas(t).Validate("urn:hive:schema:ops:hive-imperative.render.response:v1", data); err != nil {
		t.Errorf("the rendering breaks its response schema: %v", err)
	}
	return resp.Body
}

// pcInstant is the instant the shadow caller asks for what is due at or
// before; the dates of the fixtures lie on both sides of it, and one on it.
const pcInstant = "2098-06-01T00:00:00.000Z"

// pcShadowWorld is the home the shadow caller reads.
type pcShadowWorld struct {
	*pcWorld
	// Dated holds: due before the instant, due on it, due after it, with no
	// date, and released.
	holdEarly, holdOn, holdLate, holdUndated, holdReleased string
	// Rules by expiry: before the instant (one already past), after it, none.
	rulePast, ruleEarly, ruleLate, ruleNone string
	// Deferred questions by revisit date, and one never deferred.
	questionEarly, questionLate, questionOpen string
	// The Hunts of a Prey beneath a Target.
	target, prey, huntDone, huntHeld, huntRunning string
	holdOnHunt                                    string
	subject, miss                                 string
}

const pcSubject = "subj_01000000000000000000000000"

// rule records an operating rule of the subject key, filed under the subject,
// with the dates given, and returns the rule.
func (w *pcWorld) rule(key, line string, extra map[string]any) map[string]any {
	w.t.Helper()
	body := map[string]any{
		"subject": key, "words": line, "origin": map[string]any{"kind": "ruling"},
		"applies": []any{}, "kind": "operating", "tier": "subject", "filed_under": pcSubject, "rule_line": line,
	}
	maps.Copy(body, extra)
	rule, _ := w.send("hive-rule", "record", body)["rule"].(map[string]any)
	if rule == nil {
		w.t.Fatal("record returned no rule")
	}
	return rule
}

// pcCreateSubject is the member of a rule's record that creates the subject
// the rules of a home are filed under, along with the first of them.
func pcCreateSubject() map[string]any {
	return map[string]any{"create_subject": map[string]any{
		"subject_id": pcSubject, "key": "widget.exports", "loads_at": []any{"dispatch"},
	}}
}

// answerLater has the operator answer the question with Later, to revisit at
// the instant.
func (w *pcWorld) answerLater(h wpHeld, revisit string) {
	w.t.Helper()
	wpOK(w.t, "answer", w.operatorAnswer(h, w.ids.take(), map[string]any{
		"later": map[string]any{"revisit_at": revisit},
	}).word(w.t, w.home))
}

// newShadowWorld builds the home of the shadow caller through the real
// executables.
func newShadowWorld(t testing.TB) *pcShadowWorld {
	t.Helper()
	s := &pcShadowWorld{pcWorld: newPcWorld(t)}
	s.plantBudget()

	// Dated holds, each on an item of its own.
	s.holdEarly = s.placeHold(s.addID(pcLeaf("Held early")), "future", "2098-01-01T00:00:00.000Z")
	s.holdOn = s.placeHold(s.addID(pcLeaf("Held on the instant")), "resource", pcInstant)
	s.holdLate = s.placeHold(s.addID(pcLeaf("Held late")), "future", "2099-01-01T00:00:00.000Z")
	s.holdUndated = s.placeHold(s.addID(pcLeaf("Held with no date")), "parked", "")
	released := s.addID(pcLeaf("Held and released"))
	s.holdReleased = s.placeHold(released, "future", "2097-01-01T00:00:00.000Z")
	s.send("hive-pool", "release", map[string]any{"hold_id": s.holdReleased})

	// Rules by expiry, a miss of one of their subject.
	first := s.rule("widget.past", "Expired already.", mergeMap(pcCreateSubject(), map[string]any{
		"effective_at": "2026-09-01T00:00:00.000Z", "expires_at": "2026-09-30T00:00:00.000Z",
	}))
	s.rulePast = fmt.Sprint(first["rule_id"])
	s.ruleEarly = fmt.Sprint(s.rule("widget.early", "Expires early.", map[string]any{"expires_at": "2098-01-01T00:00:00.000Z"})["rule_id"])
	s.ruleLate = fmt.Sprint(s.rule("widget.late", "Expires late.", map[string]any{"expires_at": "2099-06-01T00:00:00.000Z"})["rule_id"])
	s.ruleNone = fmt.Sprint(s.rule("widget.none", "Never expires.", nil)["rule_id"])
	s.subject = pcSubject
	s.miss = s.recordMiss(s.ruleNone)

	// Deferred questions, and one still open.
	s.questionEarly = s.deferred("2098-02-01T00:00:00.000Z")
	s.questionLate = s.deferred("2099-02-01T00:00:00.000Z")
	s.questionOpen = s.hold().question

	// A Hunt's progress: a Target with an accepted specification, its Prey and
	// the Prey's Hunts: one done, one held, one admitted and running.
	s.target = s.addID(map[string]any{"title": "A Target", "level": "target", "specification_required": true})
	spec := s.specify(s.target)
	s.prey = s.addID(pcLeaf("A Prey", pcEdge("parent", s.target)))
	hunt := func(title string) string {
		return s.addID(map[string]any{
			"title": title, "level": "hunt", "deliverable": "swarm", "placement_requested": "mac",
			"delivery": pcDelivery, "deps": []any{pcEdge("parent", s.prey)},
		})
	}
	s.huntDone = hunt("A Hunt that is done")
	s.done(s.huntDone, pcClosed)
	s.huntHeld = hunt("A Hunt that is held")
	s.holdOnHunt = s.placeHold(s.huntHeld, "parked", "")
	s.huntRunning = hunt("A Hunt that is running")
	s.openGeneration(s.huntRunning, s.admit(s.huntRunning, spec))
	return s
}

// pcClosed is the closing time of the Hunt that is done.
const pcClosed = "2026-10-12T10:00:00.000Z"

// deferred raises a question on a new item and has it answered with Later, and
// returns the question.
func (w *pcWorld) deferred(revisit string) string {
	w.t.Helper()
	h := w.hold()
	w.answerLater(h, revisit)
	return h.question
}

// recordMiss records the independent miss of a rule and returns the miss.
func (w *pcWorld) recordMiss(rule string) string {
	w.t.Helper()
	item := w.add(pcLeaf("The incident of a miss"))
	data, err := os.ReadFile(filepath.Join(w.home, "pool", "records", "item", fmt.Sprint(item["task_id"])+".json"))
	if err != nil {
		w.t.Fatal(err)
	}
	ref := map[string]any{
		"schema": "pool.item", "id": item["task_id"], "home_id": testHome,
		"record_revision": item["record_revision"], "digest": canon.FileDigest(data),
	}
	rev := pcMap(w.stored("rule", rule))["record_revision"]
	miss, _ := w.send("hive-memory", "miss", map[string]any{
		"rule":  map[string]any{"rule_id": rule, "record_revision": rev},
		"actor": map[string]any{"kind": "session", "session_id": ident(ids.Session, 1), "session_generation": int64(1)},
		"step":  "dispatch",
		"provenance": map[string]any{
			"incident": ref, "observer": map[string]any{"kind": "test"}, "independence": "independent",
			"derived_from": []any{}, "evidence": []any{ref},
		},
		"observed_at": "2026-10-11T08:00:00.000Z",
	})["miss"].(map[string]any)
	return fmt.Sprint(miss["miss_id"])
}

// shadow is the stand-in caller of the Shadow: the row of the Pool's table of
// readers that gives it what is due, a Hunt's progress and, on its
// reconciliation backstop, the health of the projections.
func (s *pcShadowWorld) shadow() *pcCaller {
	return s.caller("shadow", "hive-pool read hold", "hive-pool read rule", "hive-pool read question", "hive-pool read item", "hive-memory check")
}

// TestShadowCallerReadsWhatIsDueAtOrBeforeAnInstant is the proof of the row of
// the Pool's table of readers that gives the Shadow "active holds with an
// `until`, rules with an expiry time and deferred questions with a revisit date,
// each due at or before an instant", by `read` with `due_before`.
//
// The home holds holds, rules and deferred questions dated before the instant,
// on it and after it, and others that carry no date or are not active. The
// caller reads each kind at the instant and at a later one, and receives, whole
// and in a complete read, the records dated at or before it and no others.
//
// It catches a due time read as a strict bound or as a bound on another date,
// an undated record or a released hold that is called due, a dated record that
// is missed, and a read that answers empty without saying it is complete.
func TestShadowCallerReadsWhatIsDueAtOrBeforeAnInstant(t *testing.T) {
	s := newShadowWorld(t)
	c := s.shadow()
	before := s.state()
	const later = "2100-01-01T00:00:00.000Z"

	holds := c.read("hold", map[string]any{"active": true, "due_before": pcInstant})
	if got, want := holds.sorted(), pcSorted(s.holdEarly, s.holdOn); !slices.Equal(got, want) {
		t.Errorf("the active holds due at or before %s are %v, want %v (the one on the instant included)", pcInstant, got, want)
	}
	for _, h := range holds.records {
		if until := pcMap(h["until"])["at"]; h["state"] != "active" || until == nil || fmt.Sprint(until) > pcInstant {
			t.Errorf("hold %v is %v with until %v: not an active hold due by %s", h["hold_id"], h["state"], h["until"], pcInstant)
		}
	}
	if got, want := c.read("hold", map[string]any{"active": true, "due_before": later}).sorted(), pcSorted(s.holdEarly, s.holdOn, s.holdLate); !slices.Equal(got, want) {
		t.Errorf("the active holds due at or before %s are %v, want %v", later, got, want)
	}
	if got := c.read("hold", map[string]any{"active": true, "due_before": "2097-12-31T23:59:59.999Z"}).ids(); len(got) != 0 {
		t.Errorf("no hold is due before 2098, and the read returned %v", got)
	}

	rules := c.read("rule", map[string]any{"due_before": pcInstant})
	if got, want := rules.sorted(), pcSorted(s.rulePast, s.ruleEarly); !slices.Equal(got, want) {
		t.Errorf("the rules due at or before %s are %v, want %v", pcInstant, got, want)
	}
	if got, want := c.read("rule", map[string]any{"due_before": later}).sorted(), pcSorted(s.rulePast, s.ruleEarly, s.ruleLate); !slices.Equal(got, want) {
		t.Errorf("the rules due at or before %s are %v, want %v (a rule that never expires is never due)", later, got, want)
	}

	questions := c.read("question", map[string]any{"due_before": pcInstant})
	if got, want := questions.sorted(), pcSorted(s.questionEarly); !slices.Equal(got, want) {
		t.Errorf("the questions due for a revisit at or before %s are %v, want %v", pcInstant, got, want)
	}
	if got, want := c.read("question", map[string]any{"due_before": later}).sorted(), pcSorted(s.questionEarly, s.questionLate); !slices.Equal(got, want) {
		t.Errorf("the questions due for a revisit at or before %s are %v, want %v (a question never deferred is never due)", later, got, want)
	}

	sameSnapshot(t, "the shadow caller's reads", before, s.state())
	pcCalls(t, c, "hive-pool read hold", "hive-pool read rule", "hive-pool read question")
}

// TestShadowCallerReadsAHuntsProgress is the proof of the row of the Pool's
// table of readers that gives the Shadow, from stage 4, "for a Hunt's progress,
// a Hunt with no generation and the items beneath a Target or a Prey, with
// their state, derived `landed` fact, closing time, edges and active holds", by
// `read` by query.
//
// The home holds a Target, its Prey and three Hunts of the Prey: one done, one
// held and one admitted and running with an open generation. The caller reads
// the items by query and finds beneath the Target its Prey and Hunts, with
// each one's state, derived landed fact, closing time, edges and active holds,
// and among the Hunts those with no generation.
//
// It catches a derived fact that disagrees with the state it is derived from, a
// closing time that is lost or invented, a hold that does not reach the item it
// holds, and a generation that is missing from the Hunt that has one.
func TestShadowCallerReadsAHuntsProgress(t *testing.T) {
	s := newShadowWorld(t)
	c := s.shadow()
	before := s.state()

	items := c.read("item", map[string]any{})

	// The items beneath the Target, found by their parent edges.
	beneath := map[string]bool{}
	for grew := true; grew; {
		grew = false
		for _, r := range items.records {
			id := fmt.Sprint(r["task_id"])
			for _, d := range pcMaps(r["deps"]) {
				if parent := fmt.Sprint(d["task_id"]); d["type"] == "parent" && (parent == s.target || beneath[parent]) && !beneath[id] {
					beneath[id], grew = true, true
				}
			}
		}
	}
	var got []string
	for id := range beneath {
		got = append(got, id)
	}
	if want := pcSorted(s.prey, s.huntDone, s.huntHeld, s.huntRunning); !slices.Equal(pcSorted(got...), want) {
		t.Fatalf("the items beneath the Target are %v, want %v", pcSorted(got...), want)
	}

	// The Hunts with no generation.
	var ungenerated []string
	for _, r := range items.records {
		if id := fmt.Sprint(r["task_id"]); r["level"] == "hunt" && len(pcMaps(r["generations"])) == 0 {
			ungenerated = append(ungenerated, id)
		}
	}
	if want := pcSorted(s.huntDone, s.huntHeld); !slices.Equal(pcSorted(ungenerated...), want) {
		t.Errorf("the Hunts with no generation are %v, want %v", pcSorted(ungenerated...), want)
	}

	// Each item's state, landed fact, closing time, edges and active holds.
	type progress struct {
		id, state string
		landed    bool
		closed    any
		holds     []string
		edge      string
	}
	for _, want := range []progress{
		{s.prey, "queued", false, nil, nil, s.target},
		{s.huntDone, "done", true, pcClosed, nil, s.prey},
		{s.huntHeld, "queued", false, nil, []string{s.holdOnHunt}, s.prey},
		{s.huntRunning, "in-flight", false, nil, nil, s.prey},
	} {
		rec, facts := items.record(want.id), items.fact(want.id)
		if rec["state"] != want.state || facts["landed"] != want.landed || rec["closed_at"] != want.closed {
			t.Errorf("%s: state %v, landed %v, closed %v; want %s, %v, %v", want.id, rec["state"], facts["landed"], rec["closed_at"], want.state, want.landed, want.closed)
		}
		var edges []string
		for _, d := range pcMaps(rec["deps"]) {
			edges = append(edges, fmt.Sprintf("%v:%v", d["type"], d["task_id"]))
		}
		if !slices.Equal(edges, []string{"parent:" + want.edge}) {
			t.Errorf("%s: edges %v, want a parent edge to %s", want.id, edges, want.edge)
		}
		var holds []string
		for _, h := range facts["active_holds"].([]any) {
			holds = append(holds, fmt.Sprint(h))
		}
		if !slices.Equal(holds, want.holds) {
			t.Errorf("%s: active holds %v, want %v", want.id, holds, want.holds)
		}
	}
	if facts := items.fact(s.huntHeld); facts["held"] != true || facts["ready"] != false {
		t.Errorf("the held Hunt: held %v, ready %v", facts["held"], facts["ready"])
	}
	if g := pcMaps(items.record(s.huntRunning)["generations"]); len(g) != 1 || g[0]["state"] != "open" || g[0]["generation"] != int64(1) {
		t.Errorf("the running Hunt's generations: %v", items.record(s.huntRunning)["generations"])
	}
	if facts := items.fact(s.target); facts["building"] != true || facts["landed"] != false {
		t.Errorf("the Target: building %v, landed %v; an item beneath it is in flight", facts["building"], facts["landed"])
	}

	sameSnapshot(t, "the shadow caller's reads", before, s.state())
	pcCalls(t, c, "hive-pool read item")
}

// check runs hive-memory check as the package and returns the report, held to
// the response schema.
func (w *pcWorld) check(from asPackage) map[string]any {
	w.t.Helper()
	t := w.t
	req := newRequest(t, from, "hive-memory", "check", w.ids.take(), map[string]any{})
	r := call{program: "hive-memory", operation: "check", home: w.home, req: req}.run(t)
	wantAccepted(t, r, req.id, declarations(t)["pool"], "hive-memory", "check", false)
	resp := wpOK(t, "check", r)
	if len(resp.Receipts) != 0 {
		t.Errorf("a check carries %d receipts", len(resp.Receipts))
	}
	data, err := canon.Encode(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := pcSchemas(t).Validate("urn:hive:schema:ops:hive-memory.check.response:v2", data); err != nil {
		t.Errorf("the report breaks its response schema: %v", err)
	}
	return resp.Body
}

// delayedRule records a rule that takes effect a few seconds from now, after
// the projections of its scope were rendered without it, and returns it and
// the instant it takes effect.
func (w *pcWorld) delayedRule() (rule string, effective time.Time) {
	w.t.Helper()
	effective = time.Now().Add(4 * time.Second)
	rule = fmt.Sprint(w.rule("widget.delayed", "Takes effect after the projections were rendered.", map[string]any{"effective_at": pcStamp(effective)})["rule_id"])
	return rule, effective
}

// pcWaitUntil returns once the clock has passed the instant.
func pcWaitUntil(at time.Time) { time.Sleep(time.Until(at) + 300*time.Millisecond) }

// pcStaleReport lists the projections of the report that are stale, by
// identity, with the sources they are stale by.
func pcStaleReport(report map[string]any) map[string][]string {
	out := map[string][]string{}
	for _, p := range pcMaps(report["projections"]) {
		if p["state"] != "stale" {
			continue
		}
		id := fmt.Sprint(p["projection_id"])
		out[id] = []string{}
		for _, ref := range pcMaps(p["stale_sources"]) {
			out[id] = append(out[id], fmt.Sprint(ref["id"]))
		}
	}
	return out
}

// TestShadowCallerReadsTheHealthOfTheProjectionsOnItsBackstop is the proof of
// the row of the Pool's table of readers that gives the Shadow, "on its
// reconciliation backstop, each projection's size against its budget, its
// freshness and each subject's misses", by `hive-memory check`.
//
// The caller runs the check on a home whose projections were rendered by the
// rules recorded in it. It receives every projection with its size, its budget
// and its freshness, and every subject with the misses counted against it; the
// sizes are the ones the Pool stores, the budgets are the home's, the
// projections are fresh, and the check changes nothing. It then runs the check
// after a rule recorded to take effect later has taken effect, and receives
// the projections that rule makes stale, naming it.
//
// It catches a size that is not the stored rendering's, a budget that is not
// one of the home's, a projection called fresh after the clock changed the
// rules in force, a stale source that does not name the rule, a subject whose
// misses are lost, and a check that writes.
func TestShadowCallerReadsTheHealthOfTheProjectionsOnItsBackstop(t *testing.T) {
	s := newShadowWorld(t)
	c := s.shadow()
	before := s.state()

	report := c.check()
	projections := pcMaps(report["projections"])
	if len(projections) == 0 {
		t.Fatal("the check reports no projection of a home whose rules rendered some")
	}
	budgets := []int64{18432, 4608, 9216}
	for _, p := range projections {
		id := fmt.Sprint(p["projection_id"])
		current := pcMap(s.stored("projection", id)["current"])
		bytes, max := p["bytes"].(int64), p["max_bytes"].(int64)
		if bytes != current["bytes"] {
			t.Errorf("projection %s is reported at %d bytes; the Pool stores %v", id, bytes, current["bytes"])
		}
		if !slices.Contains(budgets, max) {
			t.Errorf("projection %s is reported against the budget %d, which is none of the home's %v", id, max, budgets)
		}
		if p["over_budget"] != (bytes > max) {
			t.Errorf("projection %s: %d bytes against %d, over_budget %v", id, bytes, max, p["over_budget"])
		}
		if p["state"] != "fresh" || len(pcMaps(p["stale_sources"])) != 0 {
			t.Errorf("projection %s is %v with stale sources %v, rendered by the rules in force", id, p["state"], p["stale_sources"])
		}
	}
	if want := s.stored("budget", "memory")["record_revision"]; report["budget_revision"] != want {
		t.Errorf("budget_revision = %v, want the stored %v", report["budget_revision"], want)
	}
	var subject map[string]any
	for _, sub := range pcMaps(report["subjects"]) {
		if sub["subject_id"] == s.subject {
			subject = sub
		}
	}
	if subject == nil {
		t.Fatalf("the report names no subject %s: %v", s.subject, report["subjects"])
	}
	if subject["independent_misses"] != int64(1) || !slices.Equal(subject["misses"].([]any), []any{s.miss}) {
		t.Errorf("the subject's misses: %v, want the one miss %s", subject, s.miss)
	}
	sameSnapshot(t, "the check", before, s.state())

	// A rule takes effect by the clock after the projections were rendered.
	rule, effective := s.delayedRule()
	stale := pcStaleReport(c.check())
	if time.Now().Before(effective) && len(stale) != 0 {
		t.Errorf("before the rule takes effect the check reports stale projections %v", stale)
	}
	pcWaitUntil(effective)
	stale = pcStaleReport(c.check())
	if len(stale) == 0 {
		t.Fatalf("the rule %s took effect and the check reports no projection stale", rule)
	}
	for id, sources := range stale {
		if !slices.Contains(sources, rule) {
			t.Errorf("projection %s is stale by %v, which does not name the rule %s that took effect", id, sources, rule)
		}
	}
	pcCalls(t, c, "hive-memory check")
}

// check runs hive-memory check as the caller.
func (c *pcCaller) check() map[string]any {
	c.w.t.Helper()
	c.use("hive-memory check")
	return c.w.check(c.from)
}

// pcUIWorld is the home the ui caller reads.
type pcUIWorld struct {
	*pcWorld
	ready, held, blocked, finished, loose string
	// holdOnHeld is the hold of the item held.
	holdOnHeld string
	// The tree of a Target all of whose items have landed.
	target, prey, hunt string
	// The questions: one open, one answered with Later, and their items.
	open, later wpHeld
	// Rules: in force, expired and expiring.
	inForce, expired string
}

// newUIWorld builds the home of the ui caller through the real executables.
func newUIWorld(t testing.TB) *pcUIWorld {
	t.Helper()
	u := &pcUIWorld{pcWorld: newPcWorld(t)}
	u.plantBudget()

	u.ready = u.addID(pcLeaf("Ready"))
	u.held = u.addID(pcLeaf("Held"))
	u.holdOnHeld = u.placeHold(u.held, "parked", "")
	u.blocked = u.addID(pcLeaf("Blocked", pcEdge("blocked-by", u.ready)))
	u.finished = u.addID(pcLeaf("Finished"))
	u.done(u.finished, pcClosed)

	u.target = u.addID(map[string]any{"title": "A Target", "level": "target"})
	u.prey = u.addID(pcLeaf("A Prey", pcEdge("parent", u.target)))
	u.hunt = u.addID(map[string]any{
		"title": "A Hunt", "level": "hunt", "deliverable": "swarm", "placement_requested": "mac",
		"delivery": pcDelivery, "deps": []any{pcEdge("parent", u.prey)},
	})
	u.done(u.hunt, pcClosed)

	u.open = u.hold()
	u.later = u.hold()
	u.answerLater(u.later, "2099-02-01T00:00:00.000Z")

	first := u.rule("widget.past", "Expired already.", mergeMap(pcCreateSubject(), map[string]any{
		"effective_at": "2026-09-01T00:00:00.000Z", "expires_at": "2026-09-30T00:00:00.000Z",
	}))
	u.expired = fmt.Sprint(first["rule_id"])
	u.inForce = fmt.Sprint(u.rule("widget.none", "Never expires.", nil)["rule_id"])
	u.send("hive-rule", "expire", map[string]any{"rule_id": u.expired, "noticed_at": "2026-10-01T00:00:00.000Z"},
		envelope.ExpectedRevision{Schema: "pool.rule", ID: u.expired, RecordRevision: first["record_revision"].(int64)})
	return u
}

// ui is the stand-in caller of the UI: the row of the Pool's table of readers
// that gives it items with their derived facts, holds, questions and decision
// rounds, rules in force and recently expired, and projections.
func (u *pcUIWorld) ui() *pcCaller {
	return u.caller("ui", "hive-pool read item", "hive-pool read hold", "hive-pool read question", "hive-pool read decision-round", "hive-pool read rule", "hive-pool read projection")
}

// TestUICallerReadsItemsWithTheirDerivedFacts is the proof of the part of the
// row of the Pool's table of readers that gives the UI "items with their derived
// facts", by `read` by query.
//
// The home holds an item ready, one held, one blocked by another, one done and
// a Target whose Prey and Hunt are done. The caller reads the items by query,
// by state and by readiness, and receives each item whole with the derived
// facts that agree with the state it was made in: blocked, held, ready, landed,
// the blockers and the holds that make them so, and building on the Target.
// A read that names a state the Pool does not have is refused, and never
// answered with an empty list.
//
// It catches a derived fact that is stored or stale, a ready selector that
// lets a held or blocked item through, a state selector that drops an item of
// the state or keeps one of another, and a state outside the four answered as
// though nothing were there.
func TestUICallerReadsItemsWithTheirDerivedFacts(t *testing.T) {
	u := newUIWorld(t)
	c := u.ui()
	before := u.state()

	items := c.read("item", map[string]any{})
	type facts struct {
		blocked, held, ready, landed bool
		blockers, holds              []string
	}
	for id, want := range map[string]facts{
		u.ready:    {ready: true},
		u.held:     {held: true, holds: []string{u.holdOnHeld}},
		u.blocked:  {blocked: true, blockers: []string{u.ready}},
		u.finished: {landed: true},
		u.hunt:     {landed: true},
		u.prey:     {landed: true},
		u.target:   {landed: true},
	} {
		got := items.fact(id)
		if got == nil {
			t.Errorf("the read of all items has no derived facts for %s", id)
			continue
		}
		strs := func(v any) []string {
			out := []string{}
			for _, e := range v.([]any) {
				out = append(out, fmt.Sprint(e))
			}
			return out
		}
		if got["blocked"] != want.blocked || got["held"] != want.held || got["ready"] != want.ready || got["landed"] != want.landed ||
			!slices.Equal(strs(got["active_blockers"]), append([]string{}, want.blockers...)) || !slices.Equal(strs(got["active_holds"]), append([]string{}, want.holds...)) {
			t.Errorf("%s: derived facts %v, want %+v", id, got, want)
		}
	}
	if got := items.fact(u.target)["building"]; got != false {
		t.Errorf("building on the Target is %v, want false: nothing beneath it is in flight", got)
	}
	if _, ok := items.fact(u.ready)["building"]; ok {
		t.Error("building is a fact of a Target and is reported on an item that is not one")
	}
	if got := items.record(u.finished)["closed_at"]; got != pcClosed {
		t.Errorf("the finished item's closing time is %v, want %s", got, pcClosed)
	}

	ready := c.read("item", map[string]any{"ready": true})
	if got, want := ready.sorted(), pcSorted(u.ready); !slices.Equal(got, want) {
		t.Errorf("the items ready are %v, want %v", got, want)
	}
	if got, want := c.read("item", map[string]any{"states": []any{"done"}}).sorted(), pcSorted(u.finished, u.hunt); !slices.Equal(got, want) {
		t.Errorf("the items done are %v, want %v", got, want)
	}
	queued := c.read("item", map[string]any{"states": []any{"queued"}})
	for _, id := range []string{u.finished, u.hunt} {
		if queued.record(id) != nil {
			t.Errorf("%s is done and a read of the queued items returned it", id)
		}
	}
	for _, id := range []string{u.ready, u.held, u.blocked, u.prey, u.target} {
		if queued.record(id) == nil {
			t.Errorf("%s is queued and a read of the queued items missed it", id)
		}
	}
	if got := c.read("item", map[string]any{"states": []any{"in-flight", "archived"}}).ids(); len(got) != 0 {
		t.Errorf("nothing is in flight or archived, and the read returned %v", got)
	}

	// A state the Pool does not have is refused as invalid.
	for _, states := range [][]any{{"blocked"}, {"queued", "running"}, {"Done"}} {
		req := newRequest(t, c.from, "hive-pool", "read", u.ids.take(), map[string]any{"kind": "item", "selector": map[string]any{"states": states}})
		c.use("hive-pool read item")
		r := call{program: "hive-pool", operation: "read", home: u.home, req: req}.run(t)
		wantFailure(t, r, req.id, 3, "invalid", "schema_violation")
	}

	sameSnapshot(t, "the ui caller's reads", before, u.state())
	pcCalls(t, c, "hive-pool read item")
}

// TestUICallerReadsHoldsQuestionsAndDecisionRounds is the proof of the part of
// the row of the Pool's table of readers that gives the UI "holds, questions
// and decision rounds", by `read` by query.
//
// The home holds a hold placed by hand, two questions raised through the
// Mind's word on items of their own, one of them answered with Later, and the
// decision round each question was raised in. The caller reads each kind by
// query and receives whole records that agree with one another: each question
// names the hold, the card and the round it was raised with, the round holds
// the card, and the hold holds the item the question holds.
//
// It catches a record that names another record than the one that names it, a
// state selector that keeps a record of another state, and a hold selected by
// its item that is another's.
func TestUICallerReadsHoldsQuestionsAndDecisionRounds(t *testing.T) {
	u := newUIWorld(t)
	c := u.ui()
	before := u.state()

	holds := c.read("hold", map[string]any{})
	for _, want := range []struct {
		hold, kind, item string
	}{
		{u.holdOnHeld, "parked", u.held},
		{u.open.hold, "word", u.open.item},
		{u.later.hold, "word", u.later.item},
	} {
		h := holds.record(want.hold)
		if h == nil {
			t.Errorf("the read of all holds missed %s", want.hold)
			continue
		}
		if h["kind"] != want.kind || h["task_id"] != want.item || h["state"] != "active" {
			t.Errorf("hold %s: kind %v, item %v, state %v; want %s, %s, active", want.hold, h["kind"], h["task_id"], h["state"], want.kind, want.item)
		}
	}
	if got := c.read("hold", map[string]any{"task_id": u.held}).ids(); !slices.Equal(got, []string{u.holdOnHeld}) {
		t.Errorf("the holds of the held item are %v, want %v", got, []string{u.holdOnHeld})
	}
	if got := c.read("hold", map[string]any{"active": true}); len(got.records) != len(holds.records) {
		t.Errorf("%d of %d holds are active, and none was released", len(got.records), len(holds.records))
	}

	questions := c.read("question", map[string]any{})
	rounds := c.read("decision-round", map[string]any{})
	if got, want := questions.sorted(), pcSorted(u.open.question, u.later.question); !slices.Equal(got, want) {
		t.Fatalf("the questions are %v, want %v", got, want)
	}
	if got, want := rounds.sorted(), pcSorted(u.open.round, u.later.round); !slices.Equal(got, want) {
		t.Fatalf("the decision rounds are %v, want %v", got, want)
	}
	for _, h := range []wpHeld{u.open, u.later} {
		q, round := questions.record(h.question), rounds.record(h.round)
		if q["held_task_id"] != h.item || q["hold_id"] != h.hold || q["card_id"] != h.card || q["decision_round_id"] != h.round {
			t.Errorf("question %s names item %v, hold %v, card %v, round %v; raised with %s, %s, %s, %s", h.question, q["held_task_id"], q["hold_id"], q["card_id"], q["decision_round_id"], h.item, h.hold, h.card, h.round)
		}
		var card bool
		for _, cd := range pcMaps(round["cards"]) {
			card = card || (cd["card_id"] == h.card && cd["question_id"] == h.question)
		}
		if !card {
			t.Errorf("round %s holds no card %s for question %s: %v", h.round, h.card, h.question, round["cards"])
		}
	}
	if q := questions.record(u.open.question); q["state"] != "open" {
		t.Errorf("the open question is %v", q["state"])
	}
	deferred := questions.record(u.later.question)
	if deferral := pcMap(deferred["deferral"]); deferral["revisit_at"] != "2099-02-01T00:00:00.000Z" {
		t.Errorf("the question answered with Later is deferred to %v", deferred["deferral"])
	}
	for _, state := range []string{"open", fmt.Sprint(deferred["state"])} {
		got := c.read("question", map[string]any{"states": []any{state}})
		for _, q := range got.records {
			if q["state"] != state {
				t.Errorf("a read of the %s questions returned %v in state %v", state, q["question_id"], q["state"])
			}
		}
		var want []string
		for _, q := range questions.records {
			if q["state"] == state {
				want = append(want, fmt.Sprint(q["question_id"]))
			}
		}
		if !slices.Equal(got.sorted(), pcSorted(want...)) {
			t.Errorf("the %s questions are %v, want %v", state, got.sorted(), want)
		}
	}
	for _, state := range []string{"open", "closed"} {
		for _, rd := range c.read("decision-round", map[string]any{"states": []any{state}}).records {
			if rd["state"] != state {
				t.Errorf("a read of the %s rounds returned a round in state %v", state, rd["state"])
			}
		}
	}

	sameSnapshot(t, "the ui caller's reads", before, u.state())
	pcCalls(t, c, "hive-pool read hold", "hive-pool read question", "hive-pool read decision-round")
}

// TestUICallerReadsRulesInForceAndExpiredAndProjections is the proof of the
// part of the row of the Pool's table of readers that gives the UI "rules in
// force and recently expired, projections", by `read` by query.
//
// The home holds a rule in force and a rule whose expiry was noticed, and the
// projections the rules rendered. The caller reads the rules by state and
// receives each under its own state; it reads the projections and receives each
// whole with its freshness beside it, fresh while the rules in force are the
// ones it was rendered from and stale, naming the rule, once a rule recorded to
// take effect later has taken effect.
//
// It catches a rule in the wrong state, an expired rule that is still called in
// force, a projection that is called fresh after the clock changed the rules in
// force, and a freshness that is not beside the projection it is about.
func TestUICallerReadsRulesInForceAndExpiredAndProjections(t *testing.T) {
	u := newUIWorld(t)
	c := u.ui()
	before := u.state()

	inForce := c.read("rule", map[string]any{"states": []any{"in-force"}})
	expired := c.read("rule", map[string]any{"states": []any{"expired"}})
	if got := inForce.ids(); !slices.Equal(got, []string{u.inForce}) {
		t.Errorf("the rules in force are %v, want %v", got, []string{u.inForce})
	}
	if got := expired.ids(); !slices.Equal(got, []string{u.expired}) {
		t.Errorf("the rules expired are %v, want %v", got, []string{u.expired})
	}
	if rule := expired.record(u.expired); rule["status"] != "expired" || rule["expires_at"] != "2026-09-30T00:00:00.000Z" {
		t.Errorf("the expired rule: status %v, expires_at %v", rule["status"], rule["expires_at"])
	}
	if got, want := c.read("rule", map[string]any{}).sorted(), pcSorted(u.inForce, u.expired); !slices.Equal(got, want) {
		t.Errorf("all the rules are %v, want %v", got, want)
	}

	projections := c.read("projection", map[string]any{})
	if len(projections.records) == 0 {
		t.Fatal("the home's rules rendered no projection")
	}
	for _, p := range projections.records {
		id := fmt.Sprint(p["projection_id"])
		facts := projections.fact(id)
		if facts["state"] != "fresh" || len(facts["stale_sources"].([]any)) != 0 {
			t.Errorf("projection %s is %v with stale sources %v, rendered by the rules in force", id, facts["state"], facts["stale_sources"])
		}
	}

	sameSnapshot(t, "the ui caller's reads", before, u.state())

	// A rule takes effect by the clock after the projections were rendered.
	rule, effective := u.delayedRule()
	pcWaitUntil(effective)
	stale := 0
	read := c.read("projection", map[string]any{})
	for _, p := range read.records {
		id := fmt.Sprint(p["projection_id"])
		facts := read.fact(id)
		if facts["state"] != "stale" {
			continue
		}
		stale++
		var named bool
		for _, ref := range pcMaps(facts["stale_sources"]) {
			named = named || ref["id"] == rule
		}
		if !named {
			t.Errorf("projection %s is stale by %v, which does not name the rule %s that took effect", id, facts["stale_sources"], rule)
		}
	}
	if stale == 0 {
		t.Errorf("the rule %s took effect and no projection is read as stale", rule)
	}
	pcCalls(t, c, "hive-pool read rule", "hive-pool read projection")
}

// spoil replaces the record the Pool keeps with bytes that are not a record,
// or with the record at a version this build does not read.
func (w *pcWorld) spoil(kind, id, how string) {
	w.t.Helper()
	path := "pool/records/" + kind + "/" + id + ".json"
	switch how {
	case "malformed":
		wpWrite(w.t, w.home, path, []byte("this is not a record\n"))
	case "version":
		rec := w.stored(kind, id)
		rec["schema_version"] = "99.0"
		wpWrite(w.t, w.home, path, encodeObject(w.t, rec))
	default:
		w.t.Fatalf("no way to spoil a record called %q", how)
	}
}

// TestAnIncompleteReadSaysSoAndIsNeverAnAuthoritativeEmptyResult proves, for
// the read each of dispatch, shadow and ui makes, the sentence of spec/pool.md,
// Reading the Pool: "A record that cannot be read is omitted and named, never
// guessed; an incomplete read says so and is never an authoritative empty
// result."
//
// Each caller reads the records of a home in which one of the records it
// asks for cannot be read, and receives the others whole, a header that says
// the read was not complete, and the record omitted and named with the reason:
// record_malformed for bytes that are no record, unsupported_version for a
// record of a version the build does not read. With every record it asks for
// unreadable it receives no record and a header that says it was not complete,
// so that the caller cannot take the answer for the Pool's having none.
//
// It catches a read that drops what it cannot read without saying so, one that
// guesses a record, one that calls itself complete with a record missing, and
// one that answers an empty list as the whole truth.
func TestAnIncompleteReadSaysSoAndIsNeverAnAuthoritativeEmptyResult(t *testing.T) {
	// verify holds the read at its two stages: with the first record
	// unreadable and with both.
	verify := func(t *testing.T, w *pcWorld, c *pcCaller, kind string, selector map[string]any, readable, first, second string, how string) {
		t.Helper()
		reason := map[string]string{"malformed": "record_malformed", "version": "unsupported_version"}[how]
		w.spoil(kind, first, how)
		p := c.readAny(kind, selector)
		if p.header["complete"] != false {
			t.Errorf("one %s cannot be read and the read says complete = %v", kind, p.header["complete"])
		}
		if want := map[string]string{first: reason}; !maps.Equal(p.omitted, want) {
			t.Errorf("the omitted records are %v, want %v", p.omitted, want)
		}
		if got := p.ids(); !slices.Equal(got, []string{readable}) {
			t.Errorf("the records read are %v, want just the one that can be read, %s", got, readable)
		}
		w.spoil(kind, second, how)
		p = c.readAny(kind, selector)
		if len(p.records) != 0 {
			t.Errorf("every %s asked for is unreadable and the read returned %v", kind, p.ids())
		}
		if p.header["complete"] != false || !maps.Equal(p.omitted, map[string]string{first: reason, second: reason}) {
			t.Errorf("an empty read of unreadable records says complete = %v, omitted %v; it must say it is not complete and name both", p.header["complete"], p.omitted)
		}
	}

	t.Run("dispatch reads the admissions of an item", func(t *testing.T) {
		w := newPcWorld(t)
		a := w.admittedItem()
		d := w.caller("dispatch", "hive-pool read admission")
		verify(t, w, d, "admission", map[string]any{"task_id": a.task}, a.admission, a.unchecked, a.admission, "malformed")
	})
	t.Run("shadow reads the holds due", func(t *testing.T) {
		w := newPcWorld(t)
		early := w.placeHold(w.addID(pcLeaf("Held early")), "future", "2098-01-01T00:00:00.000Z")
		on := w.placeHold(w.addID(pcLeaf("Held on the instant")), "resource", pcInstant)
		s := w.caller("shadow", "hive-pool read hold")
		verify(t, w, s, "hold", map[string]any{"active": true, "due_before": pcInstant}, on, early, on, "malformed")
	})
	t.Run("ui reads the items", func(t *testing.T) {
		w := newPcWorld(t)
		one, two := w.addID(pcLeaf("One")), w.addID(pcLeaf("Two"))
		u := w.caller("ui", "hive-pool read item")
		verify(t, w, u, "item", map[string]any{}, two, one, two, "version")
	})
}

// TestTheJournalCursorOfAReadMovesWithTheHomeAndOnlyWithIt proves the part of
// the projection header that names "the journal cursor at that moment": reads
// with no write between them are at one cursor, whatever the kind and the
// caller, and a write moves it on.
//
// It catches a cursor that moves on a read, one that is fixed at a value the
// journal has left, and one that differs between callers of one home.
func TestTheJournalCursorOfAReadMovesWithTheHomeAndOnlyWithIt(t *testing.T) {
	w := newPcWorld(t)
	w.addID(pcLeaf("One"))
	ui := w.caller("ui", "hive-pool read item", "hive-pool read hold")
	shadow := w.caller("shadow", "hive-pool read item")

	first := ui.read("item", map[string]any{})
	again := ui.read("hold", map[string]any{})
	other := shadow.read("item", map[string]any{})
	if first.cursor == 0 || again.cursor != first.cursor || other.cursor != first.cursor {
		t.Errorf("the cursors of reads with no write between them: %d, %d, %d", first.cursor, again.cursor, other.cursor)
	}
	w.addID(pcLeaf("Two"))
	if after := ui.read("item", map[string]any{}); after.cursor <= first.cursor {
		t.Errorf("the cursor was %d before a write and %d after it", first.cursor, after.cursor)
	}
}

// TestShadowAndUICallersAgreeOnWhichProjectionsAreStale proves that the
// freshness the UI reads beside a projection and the freshness the Shadow's
// check reports are one fact of the home: "Freshness is computed, never stored,
// here and at `hive-pool read` of a projection, from the rules in force at that
// moment". Neither caller may call the other's operation, so each reads the home
// through its own row of the Pool's table of readers, one after the other with
// nothing written between them, after the clock has put a rule in force.
//
// It catches a freshness that is computed one way for a read and another for a
// check.
func TestShadowAndUICallersAgreeOnWhichProjectionsAreStale(t *testing.T) {
	w := newPcWorld(t)
	w.plantBudget()
	w.rule("widget.exports", "Exports retry three times.", mergeMap(pcCreateSubject(), nil))
	rule, effective := w.delayedRule()
	pcWaitUntil(effective)

	ui := w.caller("ui", "hive-pool read projection")
	shadow := w.caller("shadow", "hive-memory check")
	read := ui.read("projection", map[string]any{})
	report := shadow.check()

	fromRead := map[string][]string{}
	for _, p := range read.records {
		id := fmt.Sprint(p["projection_id"])
		if facts := read.fact(id); facts["state"] == "stale" {
			fromRead[id] = []string{}
			for _, ref := range pcMaps(facts["stale_sources"]) {
				fromRead[id] = append(fromRead[id], fmt.Sprint(ref["id"]))
			}
		}
	}
	fromCheck := pcStaleReport(report)
	if len(fromRead) == 0 {
		t.Fatalf("the rule %s took effect and the UI reads no projection as stale", rule)
	}
	if len(fromRead) != len(fromCheck) {
		t.Errorf("the UI reads %v stale; the check reports %v", fromRead, fromCheck)
	}
	for id, sources := range fromRead {
		if !slices.Equal(pcSorted(sources...), pcSorted(fromCheck[id]...)) {
			t.Errorf("projection %s is stale by %v in the read and by %v in the check", id, sources, fromCheck[id])
		}
	}
	pcCalls(t, ui, "hive-pool read projection")
	pcCalls(t, shadow, "hive-memory check")
}
