package decision_test

import (
	"maps"
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
	"github.com/kesk-git/hive/contract/store"
	_ "github.com/kesk-git/hive/packages/pool/internal/decision"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
	_ "github.com/kesk-git/hive/packages/pool/internal/item"
)

// The acceptance test of hive-pool hold-question and revise-question
// (spec/word.md, hold and revise, and Error codes). It drives the executable
// the default registry describes, in process, over a home of its own, with the
// requests the way hive-word hands them off, and holds the two operations to
// the contract: who may send them, the hold a question binds or places, the
// decision item a question with its own scope adds, the round it joins, each
// refusal the contract names, and a revision that keeps what a question keeps.

var start = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

const asked = "2026-10-14T09:00:00.000Z"

// home is a home with the Pool's executable over it.
type home struct {
	t      testing.TB
	dir    string
	homeID string
	exe    *cli.Executable
	// scope is the scope members requests are made at, the whole home where
	// nil.
	scope map[string]any
}

func (h *home) as(scope map[string]any) *home {
	c := *h
	c.scope = scope
	return &c
}

func (h *home) inWorkspace(workspace string) *home {
	return h.as(map[string]any{"scope_level": "workspace", "workspace_id": workspace})
}

func (h *home) inProject(workspace, project string) *home {
	return h.as(map[string]any{"scope_level": "project", "workspace_id": workspace, "project_id": project})
}

func mint(t testing.TB, p ids.Prefix) string {
	t.Helper()
	id, err := ids.Mint(clock.System{}, p)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func writer(operation string) map[string]any {
	return map[string]any{"package": "pool", "command": "hive-pool", "operation": operation, "implementation_version": "test"}
}

func newHome(t testing.TB) *home {
	t.Helper()
	h := &home{t: t, dir: filepath.Join(t.TempDir(), "home"), homeID: mint(t, ids.Home)}
	// The root is made and secured first, as Hive makes a home, so that every
	// entry made in it is owner-only and owned by the user.
	if err := store.MakeDir(h.dir); err != nil {
		t.Fatal(err)
	}
	if err := store.SecureDir(h.dir); err != nil {
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
	h.put(filepath.Join(h.dir, "home.json"), append(data, '\n'))
	h.exe = entry.Executable()
	h.exe.Clock = clock.NewFake(start)
	return h
}

// register writes a record of the home's identity registry, as fleet
// provisioning would.
func (h *home) register(dir, id string, rec map[string]any) {
	h.t.Helper()
	rec["schema_version"], rec["record_revision"], rec["home_id"], rec["state"] = "1.0", int64(1), h.homeID, "active"
	rec["created_at"], rec["updated_at"] = "2026-10-11T08:00:00.000Z", "2026-10-11T08:00:00.000Z"
	rec["slug"], rec["name"] = "registered", "Registered"
	rec["writer"] = map[string]any{"package": "dispatch", "command": "hive-fleet", "operation": "provision", "implementation_version": "test"}
	data, err := canon.Encode(rec)
	if err != nil {
		h.t.Fatal(err)
	}
	path := filepath.Join(h.dir, "dispatch", "registry", dir, id+".json")
	h.put(path, append(data, '\n'))
}

// put puts the file at path in the home with the content given, through the
// store's own primitives, so that the directories above it and the file are
// owner-only and owned by the user, as Hive makes them. A file that is there
// is emptied first.
func (h *home) put(path string, data []byte) {
	h.t.Helper()
	if err := store.MakeDir(filepath.Dir(path)); err != nil {
		h.t.Fatal(err)
	}
	f, err := store.CreateFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		h.t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		h.t.Fatal(err)
	}
}

func (h *home) workspace() string {
	h.t.Helper()
	id := mint(h.t, ids.Workspace)
	h.register("workspaces", id, map[string]any{"schema": "hive.workspace", "workspace_id": id, "parent_home_id": mint(h.t, ids.Home)})
	return id
}

func (h *home) project(workspace string) string {
	h.t.Helper()
	id := mint(h.t, ids.Project)
	h.register("projects", id, map[string]any{"schema": "hive.project", "project_id": id, "workspace_id": workspace, "repository": map[string]any{"kind": "local-only"}})
	return id
}

// rev is the revision of a record a caller read, as the request names it.
type rev struct {
	schema, id string
	revision   int64
}

// sent is how a request is made: by default the way hive-word hands one off,
// under a request_id of its own.
type sent struct {
	requestID string
	// caller replaces the handoff's caller; noCausation leaves the causation
	// out, and causationOf names another request than the request's own.
	caller       map[string]any
	noCausation  bool
	causationOf  string
	digest       string
	expectedRevs []rev
}

var wordCaller = map[string]any{"kind": "package", "package": "word", "command": "hive-word"}

// send sends one request to hive-pool and returns the response as it was
// written.
func (h *home) send(operation string, body map[string]any, o sent) *envelope.Response {
	h.t.Helper()
	if o.requestID == "" {
		o.requestID = mint(h.t, ids.Request)
	}
	if o.caller == nil {
		o.caller = wordCaller
	}
	if o.digest == "" {
		o.digest = canon.FileDigest([]byte(o.requestID))
	}
	req := map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": "hive-pool", "operation": operation, "operation_version": "1.0",
		"request_id": o.requestID, "correlation_id": mint(h.t, ids.Correlation),
		"issued_at": asked, "caller": o.caller,
		"scope_level": "hive", "home_id": h.homeID,
		"authority": map[string]any{"kind": "none"}, "body": body,
	}
	if !o.noCausation {
		id := o.requestID
		if o.causationOf != "" {
			id = o.causationOf
		}
		req["causation"] = map[string]any{"schema": "hive.request", "id": id, "digest": o.digest}
	}
	for k, v := range h.scope {
		req[k] = v
	}
	if len(o.expectedRevs) > 0 {
		list := []any{}
		for _, e := range o.expectedRevs {
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

// do sends a request that is not hive-word's, as the items' operations are
// sent: by a test, with no causation.
func (h *home) do(operation string, body map[string]any) *envelope.Response {
	h.t.Helper()
	return h.send(operation, body, sent{caller: map[string]any{"kind": "test"}, noCausation: true})
}

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

// kinds are the kinds of the receipts a response carries, sorted.
func kinds(resp *envelope.Response) []string {
	out := []string{}
	for _, r := range resp.Receipts {
		out = append(out, r.Kind)
	}
	slices.Sort(out)
	return out
}

func sameKinds(t testing.TB, resp *envelope.Response, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := kinds(resp); !slices.Equal(got, want) {
		t.Errorf("receipts %v, want %v", got, want)
	}
}

// files is every file under pool/ but the replay records of requests, by
// slash path, with its bytes.
func (h *home) records() map[string]string {
	h.t.Helper()
	out := map[string]string{}
	filepath.WalkDir(filepath.Join(h.dir, "pool"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				h.t.Fatal(rerr)
			}
			rel, _ := filepath.Rel(h.dir, path)
			if rel = filepath.ToSlash(rel); !strings.HasPrefix(rel, "pool/requests/") {
				out[rel] = string(data)
			}
		}
		return nil
	})
	return out
}

// unchanged runs f and fails if it changed anything under pool/.
func (h *home) unchanged(f func()) {
	h.t.Helper()
	before := h.records()
	f()
	after := h.records()
	if len(before) != len(after) {
		h.t.Errorf("the Pool changed: %d files before, %d after", len(before), len(after))
		return
	}
	for k, v := range before {
		if w, ok := after[k]; !ok || w != v {
			h.t.Errorf("the Pool changed at %s", k)
		}
	}
}

// stored reads a record file as the store holds it.
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

// plant writes a record directly, as a record of another lane or an earlier
// import would stand in the store.
func (h *home) plant(kind, id string, rec map[string]any) {
	h.t.Helper()
	data, err := canon.Encode(rec)
	if err != nil {
		h.t.Fatal(err)
	}
	h.put(filepath.Join(h.dir, "pool", "records", kind, id+".json"), append(data, '\n'))
}

// item adds a leaf item at the scope the home is seen from and returns its
// identity.
func (h *home) item() string {
	h.t.Helper()
	body := h.ok(h.do("add", map[string]any{"title": "A leaf", "level": "prey", "deliverable": "swarm"}))
	return body["item"].(map[string]any)["task_id"].(string)
}

func (h *home) archive(id string) {
	h.t.Helper()
	h.ok(h.do("transition", map[string]any{"task_id": id, "to": "archived"}))
}

// wordHold plants an active word hold, placed at the time, that no question
// names, as an import of a legacy backlog leaves one.
func (h *home) wordHold(taskID, placed string) string {
	h.t.Helper()
	id := mint(h.t, ids.Hold)
	h.plant("hold", id, map[string]any{
		"schema": "pool.hold", "schema_version": "1.0", "record_revision": int64(1),
		"hold_id": id, "task_id": taskID, "scope_level": "hive", "home_id": h.homeID,
		"kind": "word", "reason": "Held for the Hive Mind.", "state": "active",
		"placed_at":  placed,
		"placed_by":  map[string]any{"request_id": mint(h.t, ids.Request), "caller": map[string]any{"kind": "test"}},
		"created_at": placed, "updated_at": placed, "writer": writer("import-legacy"),
	})
	return id
}

// The bodies of a hold-question.

func option(id string) map[string]any {
	return map[string]any{"option_id": id, "label": "Option " + id, "consequence": "Choosing " + id + " goes ahead."}
}

// ask is the question of a hold-question for the item held.
func ask(t testing.TB, held string) map[string]any {
	return map[string]any{
		"question_id": mint(t, ids.Question), "held_task_id": held, "origin": map[string]any{"kind": "supervisor"},
		"text": "Which vendor should the integration use?", "consequence": "Without an answer the work stays held.",
		"recommendation": "keep", "options": []any{option("keep"), option("switch")}, "asked_at": asked,
	}
}

func newRound(t testing.TB) map[string]any {
	return map[string]any{
		"decision_round_id": mint(t, ids.DecisionRound), "new": true,
		"origin": map[string]any{"kind": "supervisor"}, "attachments": []any{}, "complete": true,
	}
}

func joined(round string) map[string]any {
	return map[string]any{"decision_round_id": round, "new": false}
}

func holdBody(t testing.TB, question, round map[string]any) map[string]any {
	return map[string]any{
		"question": question, "round": round, "card_id": mint(t, ids.Card),
		"requested_by": map[string]any{"kind": "supervisor", "session_id": mint(t, ids.Session), "session_generation": int64(1)},
	}
}

// raise raises a question on the item in a round of its own and returns the
// body of the response.
func (h *home) raise(held string) map[string]any {
	h.t.Helper()
	return h.ok(h.send("hold-question", holdBody(h.t, ask(h.t, held), newRound(h.t)), sent{}))
}

func (h *home) raiseIn(held, round string) map[string]any {
	h.t.Helper()
	return h.ok(h.send("hold-question", holdBody(h.t, ask(h.t, held), joined(round)), sent{}))
}

func rec(body map[string]any, member string) map[string]any { return body[member].(map[string]any) }

func card(round map[string]any, id string) map[string]any {
	for _, c := range round["cards"].([]any) {
		if c.(map[string]any)["card_id"] == id {
			return c.(map[string]any)
		}
	}
	return nil
}

// Only a handoff from hive-word is accepted by the two operations, and any
// other request is refused with authority_insufficient before the Pool reads
// anything.
func TestOnlyAHandoffFromWordIsAccepted(t *testing.T) {
	h := newHome(t)
	item := h.item()
	raised := h.raise(item)
	q := rec(raised, "question")
	revise := func() map[string]any {
		return map[string]any{
			"question_id": q["question_id"], "text": "Which vendor?", "consequence": "Held.", "options": []any{}, "asked_at": asked,
			"requested_by": map[string]any{"kind": "operator"},
		}
	}
	requests := map[string]sent{
		"a caller that is a test with no causation":         {caller: map[string]any{"kind": "test"}, noCausation: true},
		"hive-word with no causation":                       {noCausation: true},
		"a causation of another request":                    {causationOf: mint(t, ids.Request)},
		"a package other than word":                         {caller: map[string]any{"kind": "package", "package": "dispatch", "command": "hive-fleet"}},
		"another command of the package word":               {caller: map[string]any{"kind": "package", "package": "word", "command": "hive-other"}},
		"a supervisor that carries the handoff's causation": {caller: map[string]any{"kind": "supervisor", "session_id": mint(t, ids.Session), "session_generation": int64(1)}},
	}
	for name, o := range requests {
		t.Run(name, func(t *testing.T) {
			h.unchanged(func() {
				refused(t, h.send("hold-question", holdBody(t, ask(t, item), newRound(t)), o), envelope.Refused, envelope.CodeAuthorityInsufficient)
				refused(t, h.send("revise-question", revise(), o), envelope.Refused, envelope.CodeAuthorityInsufficient)
			})
		})
	}
}

// A question raised on existing work binds the earliest active word hold that
// no question names, and places a new one where there is none.
func TestAQuestionBindsTheEarliestWordHoldOrPlacesOne(t *testing.T) {
	t.Run("a new word hold is placed where there is none", func(t *testing.T) {
		h := newHome(t)
		item := h.item()
		before := h.stored("item", item)
		body := holdBody(t, ask(t, item), newRound(t))
		body["hold_reason"] = "The vendor is not settled."
		resp := h.send("hold-question", body, sent{})
		got := h.ok(resp)
		sameKinds(t, resp, "pool.hold.placed", "pool.question.held")
		hold, q, r := rec(got, "hold"), rec(got, "question"), rec(got, "round")
		if hold["kind"] != "word" || hold["state"] != "active" || hold["task_id"] != item || hold["reason"] != "The vendor is not settled." {
			t.Errorf("hold %v", hold)
		}
		if q["hold_id"] != hold["hold_id"] || q["held_task_id"] != item || q["held_item"] != "work" || q["state"] != "open" {
			t.Errorf("question %v", q)
		}
		if q["record_revision"] != int64(1) || q["current_revision"] != int64(1) || len(q["answers"].([]any)) != 0 {
			t.Errorf("question revision %v, current %v, answers %v", q["record_revision"], q["current_revision"], q["answers"])
		}
		rv := q["revisions"].([]any)[0].(map[string]any)
		if rv["revision"] != int64(1) || rv["asked_at"] != asked || rv["request_id"] != resp.RequestID || rv["recommendation"] != "keep" {
			t.Errorf("revision %v", rv)
		}
		if by := rv["asked_by"].(map[string]any); by["kind"] != "supervisor" {
			t.Errorf("asked_by %v", by)
		}
		c := card(r, body["card_id"].(string))
		if c == nil || c["card_revision"] != int64(1) || c["question_revision"] != int64(1) || c["state"] != "open" || c["question_id"] != q["question_id"] {
			t.Fatalf("card %v", c)
		}
		if c["held_task_revision"] != before["record_revision"] || rec(got, "item")["record_revision"] != before["record_revision"] {
			t.Errorf("the card shows the item at revision %v, the item is at %v", c["held_task_revision"], before["record_revision"])
		}
		if r["state"] != "open" || r["complete"] != true || r["record_revision"] != int64(1) || q["decision_round_id"] != r["decision_round_id"] {
			t.Errorf("round %v", r)
		}
		// What is returned is what is stored.
		for kind, id := range map[string]string{"question": q["question_id"].(string), "decision-round": r["decision_round_id"].(string), "hold": hold["hold_id"].(string)} {
			stored := h.stored(kind, id)
			if stored["record_revision"] != int64(1) || stored["schema"] != "pool."+kind {
				t.Errorf("stored %s %v", kind, stored)
			}
		}
		// The item is untouched: a hold is a record of its own.
		if after := h.stored("item", item); after["record_revision"] != before["record_revision"] {
			t.Errorf("the item changed to revision %v", after["record_revision"])
		}
	})
	t.Run("the hold's reason is the question's text where none is given", func(t *testing.T) {
		h := newHome(t)
		got := h.raise(h.item())
		if rec(got, "hold")["reason"] != rec(got, "question")["revisions"].([]any)[0].(map[string]any)["text"] {
			t.Errorf("reason %v", rec(got, "hold")["reason"])
		}
	})
	t.Run("the earliest unnamed word hold is bound, one question after another", func(t *testing.T) {
		h := newHome(t)
		item := h.item()
		late := h.wordHold(item, "2026-10-13T09:00:00.000Z")
		early := h.wordHold(item, "2026-10-12T09:00:00.000Z")
		lateBefore, earlyBefore := h.stored("hold", late), h.stored("hold", early)

		body := holdBody(t, ask(t, item), newRound(t))
		resp := h.send("hold-question", body, sent{})
		first := h.ok(resp)
		sameKinds(t, resp, "pool.question.held")
		if rec(first, "question")["hold_id"] != early || rec(first, "hold")["hold_id"] != early {
			t.Errorf("bound %v, want %s", rec(first, "question")["hold_id"], early)
		}
		if !equal(h.stored("hold", early), earlyBefore) || !equal(h.stored("hold", late), lateBefore) {
			t.Errorf("a hold that is bound is not written")
		}
		round := rec(first, "round")["decision_round_id"].(string)

		resp = h.send("hold-question", holdBody(t, ask(t, item), joined(round)), sent{})
		second := h.ok(resp)
		sameKinds(t, resp, "pool.question.held")
		if rec(second, "question")["hold_id"] != late {
			t.Errorf("the second question bound %v, want %s", rec(second, "question")["hold_id"], late)
		}

		resp = h.send("hold-question", holdBody(t, ask(t, item), joined(round)), sent{})
		third := h.ok(resp)
		sameKinds(t, resp, "pool.hold.placed", "pool.question.held")
		if id := rec(third, "question")["hold_id"]; id == early || id == late {
			t.Errorf("the third question bound %v, a hold a question names", id)
		}
	})
}

// equal reports whether two decoded values are the same document.
func equal(a, b any) bool {
	x, err1 := canon.Encode(map[string]any{"v": a})
	y, err2 := canon.Encode(map[string]any{"v": b})
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// A question with its own scope adds its decision item, in the request's
// scope, and holds it.
func TestANewItemQuestionAddsItsDecisionItem(t *testing.T) {
	h := newHome(t)
	q := ask(t, "")
	delete(q, "held_task_id")
	q["new_item"] = map[string]any{"title": "Choose the vendor", "body": "The integration needs a vendor."}
	resp := h.send("hold-question", holdBody(t, q, newRound(t)), sent{})
	got := h.ok(resp)
	sameKinds(t, resp, "pool.hold.placed", "pool.item.added", "pool.question.held")
	item, question, hold, round := rec(got, "item"), rec(got, "question"), rec(got, "hold"), rec(got, "round")
	if item["title"] != "Choose the vendor" || item["body"] != "The integration needs a vendor." || item["deliverable"] != "decision" ||
		item["state"] != "queued" || item["definition"] != "not-required" || item["record_revision"] != int64(1) {
		t.Errorf("item %v", item)
	}
	if _, has := item["level"]; has {
		t.Errorf("a decision item has no level: %v", item["level"])
	}
	if question["held_item"] != "decision-item" || question["held_task_id"] != item["task_id"] || hold["task_id"] != item["task_id"] || question["hold_id"] != hold["hold_id"] {
		t.Errorf("question %v, hold %v", question, hold)
	}
	if hold["kind"] != "word" || hold["state"] != "active" {
		t.Errorf("hold %v", hold)
	}
	if c := round["cards"].([]any)[0].(map[string]any); c["held_task_id"] != item["task_id"] || c["held_task_revision"] != int64(1) {
		t.Errorf("card %v", c)
	}
	if stored := h.stored("item", item["task_id"].(string)); stored["deliverable"] != "decision" {
		t.Errorf("stored item %v", stored)
	}

	t.Run("in the request's scope", func(t *testing.T) {
		w := h.workspace()
		q := ask(t, "")
		delete(q, "held_task_id")
		q["new_item"] = map[string]any{"title": "Choose the workspace's vendor"}
		got := h.ok(h.inWorkspace(w).send("hold-question", holdBody(t, q, newRound(t)), sent{}))
		for _, r := range []map[string]any{rec(got, "item"), rec(got, "question"), rec(got, "hold"), rec(got, "round")} {
			if r["scope_level"] != "workspace" || r["workspace_id"] != w {
				t.Errorf("scope of %v", r)
			}
		}
	})
}

// A card joins an existing round that is open and complete, and no other.
func TestACardJoinsAnOpenCompleteRound(t *testing.T) {
	h := newHome(t)
	first := h.raise(h.item())
	round := rec(first, "round")["decision_round_id"].(string)

	t.Run("an open round takes the card", func(t *testing.T) {
		item := h.item()
		resp := h.send("hold-question", holdBody(t, ask(t, item), joined(round)), sent{})
		got := h.ok(resp)
		sameKinds(t, resp, "pool.hold.placed", "pool.question.held")
		r := rec(got, "round")
		cards := r["cards"].([]any)
		if r["decision_round_id"] != round || len(cards) != 2 || r["record_revision"] != int64(2) || r["state"] != "open" {
			t.Fatalf("round %v", r)
		}
		c := cards[1].(map[string]any)
		if c["question_id"] != rec(got, "question")["question_id"] || c["card_revision"] != int64(1) || c["held_task_id"] != item || c["state"] != "open" {
			t.Errorf("card %v", c)
		}
		if rec(got, "question")["decision_round_id"] != round {
			t.Errorf("question round %v", rec(got, "question")["decision_round_id"])
		}
	})
	t.Run("a closed round is refused", func(t *testing.T) {
		closed := h.stored("decision-round", round)
		closed["state"], closed["closed_at"] = "closed", "2026-10-14T08:30:00.000Z"
		for _, c := range closed["cards"].([]any) {
			c.(map[string]any)["state"] = "moot"
		}
		id := mint(t, ids.DecisionRound)
		closed["decision_round_id"] = id
		h.plant("decision-round", id, closed)
		item := h.item()
		h.unchanged(func() {
			refused(t, h.send("hold-question", holdBody(t, ask(t, item), joined(id)), sent{}), envelope.Refused, "pool.round_closed")
		})
	})
	t.Run("a round that was not collected whole is refused", func(t *testing.T) {
		item := h.item()
		whole := newRound(t)
		whole["complete"], whole["incomplete_reason"] = false, "An attachment could not be read."
		whole["attachments"] = []any{map[string]any{
			"file":    map[string]any{"path": "word/rounds/notes.md", "digest": canon.FileDigest([]byte("notes")), "bytes": int64(5)},
			"fetched": "unreadable",
		}}
		opened := h.ok(h.send("hold-question", holdBody(t, ask(t, h.item()), whole), sent{}))
		r := rec(opened, "round")
		if r["complete"] != false || r["incomplete_reason"] != "An attachment could not be read." || len(r["attachments"].([]any)) != 1 {
			t.Errorf("round %v", r)
		}
		h.unchanged(func() {
			refused(t, h.send("hold-question", holdBody(t, ask(t, item), joined(r["decision_round_id"].(string))), sent{}), envelope.Refused, "pool.round_incomplete")
		})
	})
}

// Each refusal of hold-question the contract names, with nothing written.
func TestEachRefusalOfHoldQuestion(t *testing.T) {
	h := newHome(t)
	item := h.item()
	with := func(f func(q map[string]any)) map[string]any {
		q := ask(t, item)
		f(q)
		return holdBody(t, q, newRound(t))
	}
	refuses := func(name string, h *home, body map[string]any, status envelope.Status, code string) {
		t.Run(name, func(t *testing.T) {
			h.unchanged(func() { refused(t, h.send("hold-question", body, sent{}), status, code) })
		})
	}
	refuses("a held item that does not exist", h, holdBody(t, ask(t, mint(t, ids.Task)), newRound(t)), envelope.NotFound, envelope.CodeNotFound)
	refuses("options whose identities repeat", h, with(func(q map[string]any) {
		q["options"] = []any{option("keep"), option("keep")}
	}), envelope.Invalid, "pool.option_unknown")
	refuses("a recommendation that is not an option", h, with(func(q map[string]any) { q["recommendation"] = "elsewhere" }), envelope.Invalid, "pool.option_unknown")

	archived := h.item()
	h.archive(archived)
	refuses("an archived held item", h, holdBody(t, ask(t, archived), newRound(t)), envelope.Refused, "pool.held_item_archived")

	known := h.raise(item)
	taken := rec(known, "question")
	refuses("a question identity that exists", h, with(func(q map[string]any) { q["question_id"] = taken["question_id"] }), envelope.Conflict, envelope.CodeAlreadyExists)
	existing := newRound(t)
	existing["decision_round_id"] = rec(known, "round")["decision_round_id"]
	refuses("a round identity that exists", h, holdBody(t, ask(t, item), existing), envelope.Conflict, envelope.CodeAlreadyExists)
	reused := holdBody(t, ask(t, item), newRound(t))
	reused["card_id"] = rec(known, "round")["cards"].([]any)[0].(map[string]any)["card_id"]
	refuses("a card identity that exists", h, reused, envelope.Conflict, envelope.CodeAlreadyExists)

	t.Run("a round that does not exist", func(t *testing.T) {
		h.unchanged(func() {
			refused(t, h.send("hold-question", holdBody(t, ask(t, item), joined(mint(t, ids.DecisionRound))), sent{}), envelope.NotFound, envelope.CodeNotFound)
		})
	})

	// A request's scope contains the records of its home at hive scope, those
	// of its workspace and its projects at workspace scope, and those of its
	// project at project scope.
	ws1, ws2 := h.workspace(), h.workspace()
	p1, p2 := h.project(ws1), h.project(ws2)
	inWS1, inP1, inP2 := h.inWorkspace(ws1), h.inProject(ws1, p1), h.inProject(ws2, p2)
	atWS1, atP1, atP2 := inWS1.item(), inP1.item(), inP2.item()
	for _, tc := range []struct {
		name string
		by   *home
		held string
		ok   bool
	}{
		{"a hive-scope request holds an item of a project", h, atP2, true},
		{"a workspace-scope request holds an item of its workspace", inWS1, atWS1, true},
		{"a workspace-scope request holds an item of its project", inWS1, atP1, true},
		{"a project-scope request holds an item of its project", inP1, atP1, true},
		{"a workspace-scope request does not hold an item of another workspace's project", inWS1, atP2, false},
		{"a project-scope request does not hold an item of another project", inP1, atP2, false},
		{"a project-scope request does not hold an item of its workspace", inP1, atWS1, false},
	} {
		if tc.ok {
			t.Run(tc.name, func(t *testing.T) {
				got := tc.by.ok(tc.by.send("hold-question", holdBody(t, ask(t, tc.held), newRound(t)), sent{}))
				if rec(got, "question")["held_task_id"] != tc.held {
					t.Errorf("question %v", rec(got, "question"))
				}
			})
			continue
		}
		refuses(tc.name, tc.by, holdBody(t, ask(t, tc.held), newRound(t)), envelope.Refused, envelope.CodeScopeMismatch)
	}
	t.Run("a round outside the request's scope", func(t *testing.T) {
		inP1.unchanged(func() {
			refused(t, inP1.send("hold-question", holdBody(t, ask(t, atP1), joined(rec(known, "round")["decision_round_id"].(string))), sent{}), envelope.Refused, envelope.CodeScopeMismatch)
		})
	})
}

// A handoff sent again, with the identities minted anew, returns the outcome
// recorded of the first, replayed, and writes nothing more.
func TestAHandoffSentAgainIsReplayed(t *testing.T) {
	h := newHome(t)
	item := h.item()
	id := mint(t, ids.Request)
	first := h.send("hold-question", holdBody(t, ask(t, item), newRound(t)), sent{requestID: id})
	got := h.ok(first)
	h.unchanged(func() {
		again := h.send("hold-question", holdBody(t, ask(t, item), newRound(t)), sent{requestID: id})
		if again.Status != envelope.Ok || !again.Replayed {
			t.Fatalf("status %s, replayed %v, error %+v", again.Status, again.Replayed, again.Error)
		}
		if rec(again.Body, "question")["question_id"] != rec(got, "question")["question_id"] {
			t.Errorf("the replay gave the identities of the second request")
		}
	})
	refused(t, h.send("hold-question", holdBody(t, ask(t, item), newRound(t)), sent{requestID: id, digest: canon.FileDigest([]byte("another request"))}), envelope.Conflict, envelope.CodeReplayMismatch)
}

// requestFiles are the outcomes the Pool has recorded of requests, by name,
// with their bytes.
func (h *home) requestFiles() map[string]string {
	h.t.Helper()
	out := map[string]string{}
	entries, err := os.ReadDir(filepath.Join(h.dir, "pool", "requests"))
	if err != nil && !os.IsNotExist(err) {
		h.t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(h.dir, "pool", "requests", e.Name()))
		if err != nil {
			h.t.Fatal(err)
		}
		out[e.Name()] = string(data)
	}
	return out
}

// untouched runs f and fails if it changed any record or any recorded outcome
// of a request under pool/.
func (h *home) untouched(f func()) {
	h.t.Helper()
	before := h.requestFiles()
	h.unchanged(f)
	if after := h.requestFiles(); !maps.Equal(before, after) {
		h.t.Errorf("the Pool changed its recorded outcomes: %d before, %d after", len(before), len(after))
	}
}

var otherCommandCaller = map[string]any{"kind": "package", "package": "word", "command": "hive-other"}

// A request that names the package word and another command than hive-word is
// not a handoff, though its causation is its own request: where no outcome is
// recorded of its request_id it is refused with authority_insufficient, and
// nothing is written.
func TestAnotherCommandOfWordWithAFreshRequestIsRefused(t *testing.T) {
	h := newHome(t)
	item := h.item()
	h.untouched(func() {
		resp := h.send("hold-question", holdBody(t, ask(t, item), newRound(t)), sent{caller: otherCommandCaller})
		refused(t, resp, envelope.Refused, envelope.CodeAuthorityInsufficient)
		if resp.Replayed || len(resp.Receipts) != 0 {
			t.Errorf("replayed %v, receipts %v", resp.Replayed, kinds(resp))
		}
	})
}

// A resend of a recorded handoff's exact request_id and causation under
// another command of the package is not that handoff: it is keyed on its own
// digest, which is not the recorded one, so it is a replay_mismatch and is not
// answered from the outcome hive-word's request recorded, and nothing is
// written.
func TestAResendUnderAnotherCommandIsAReplayMismatch(t *testing.T) {
	h := newHome(t)
	item := h.item()
	id := mint(t, ids.Request)
	h.ok(h.send("hold-question", holdBody(t, ask(t, item), newRound(t)), sent{requestID: id}))
	h.untouched(func() {
		resp := h.send("hold-question", holdBody(t, ask(t, item), newRound(t)), sent{requestID: id, caller: otherCommandCaller})
		refused(t, resp, envelope.Conflict, envelope.CodeReplayMismatch)
		if resp.Replayed || len(resp.Receipts) != 0 {
			t.Errorf("replayed %v, receipts %v", resp.Replayed, kinds(resp))
		}
	})
}

// The handoff itself, sent again with the identities minted anew, is still
// answered from the outcome recorded of the first, replayed.
func TestTheGenuineHandoffResentIsAnsweredFromItsRecordedOutcome(t *testing.T) {
	h := newHome(t)
	item := h.item()
	id := mint(t, ids.Request)
	first := h.ok(h.send("hold-question", holdBody(t, ask(t, item), newRound(t)), sent{requestID: id}))
	h.untouched(func() {
		again := h.send("hold-question", holdBody(t, ask(t, item), newRound(t)), sent{requestID: id})
		if again.Status != envelope.Ok || !again.Replayed {
			t.Fatalf("status %s, replayed %v, error %+v", again.Status, again.Replayed, again.Error)
		}
		if rec(again.Body, "question")["question_id"] != rec(first, "question")["question_id"] {
			t.Errorf("the replay gave the identities of the second request")
		}
	})
}

// revision is the body of a revise-question.
func revision(question string) map[string]any {
	return map[string]any{
		"question_id": question, "text": "Which vendor should the integration use after the price rise?",
		"consequence":    "Without an answer the work stays held and the price rise lands.",
		"recommendation": "switch", "options": []any{option("switch"), option("keep"), option("both")},
		"amount": "12000 USD", "asked_at": "2026-10-14T10:00:00.000Z",
		"requested_by": map[string]any{"kind": "operator"},
	}
}

// settled rewrites the stored question into a later state, with the members
// that state carries, taken from a valid record of the contract's fixtures.
func (h *home) settled(question, fixture string) {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas", "records_pool.question", "valid", fixture))
	if err != nil {
		h.t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		h.t.Fatal(err)
	}
	from, rec := v.(map[string]any), h.stored("question", question)
	for _, m := range []string{"state", "answers", "applied", "deferral", "disposition", "closure"} {
		delete(rec, m)
		if x, ok := from[m]; ok {
			rec[m] = x
		}
	}
	h.plant("question", question, rec)
}

// A revision is the next whole one of an open or a deferred question, and
// keeps the question's identity, round, hold and state.
func TestARevisionKeepsWhatTheQuestionKeeps(t *testing.T) {
	for _, tc := range []struct{ name, fixture, state string }{
		{"an open question", "", "open"},
		{"a deferred question", "deferred-by-later.json", "deferred"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHome(t)
			item := h.item()
			raised := h.raise(item)
			q, r := rec(raised, "question"), rec(raised, "round")
			id, roundID := q["question_id"].(string), r["decision_round_id"].(string)
			if tc.fixture != "" {
				h.settled(id, tc.fixture)
			}
			before := h.stored("question", id)
			resp := h.send("revise-question", revision(id), sent{expectedRevs: []rev{{"pool.question", id, 1}}})
			got := h.ok(resp)
			sameKinds(t, resp, "pool.question.revised")
			nq, nr := rec(got, "question"), rec(got, "round")
			if nq["record_revision"] != int64(2) || nq["current_revision"] != int64(2) || len(nq["revisions"].([]any)) != 2 {
				t.Fatalf("question %v", nq)
			}
			second := nq["revisions"].([]any)[1].(map[string]any)
			if second["revision"] != int64(2) || second["text"] != "Which vendor should the integration use after the price rise?" ||
				second["recommendation"] != "switch" || second["amount"] != "12000 USD" || len(second["options"].([]any)) != 3 ||
				second["asked_at"] != "2026-10-14T10:00:00.000Z" || second["request_id"] != resp.RequestID || second["asked_by"].(map[string]any)["kind"] != "operator" {
				t.Errorf("second revision %v", second)
			}
			if !equal(nq["revisions"].([]any)[0], before["revisions"].([]any)[0]) {
				t.Errorf("the first revision changed: %v", nq["revisions"].([]any)[0])
			}
			for _, m := range []string{"question_id", "held_task_id", "held_item", "hold_id", "decision_round_id", "card_id", "state", "answers", "deferral", "origin"} {
				if !equal(nq[m], before[m]) {
					t.Errorf("%s changed from %v to %v", m, before[m], nq[m])
				}
			}
			if nq["state"] != tc.state {
				t.Errorf("state %v", nq["state"])
			}
			c := card(nr, q["card_id"].(string))
			if nr["decision_round_id"] != roundID || c == nil || c["card_revision"] != int64(2) || c["question_revision"] != int64(2) || c["state"] != "open" || nr["record_revision"] != int64(2) {
				t.Errorf("round %v", nr)
			}
			if again := h.stored("question", id); !equal(again, nq) {
				t.Errorf("what is returned is not what is stored")
			}
			if stored := h.stored("hold", rec(raised, "hold")["hold_id"].(string)); stored["state"] != "active" || stored["record_revision"] != int64(1) {
				t.Errorf("the hold changed: %v", stored)
			}

			// Another revision raises the card's presentation revision again.
			next := h.ok(h.send("revise-question", revision(id), sent{expectedRevs: []rev{{"pool.question", id, 2}}}))
			if c := card(rec(next, "round"), q["card_id"].(string)); c["card_revision"] != int64(3) || c["question_revision"] != int64(3) {
				t.Errorf("card %v", c)
			}
		})
	}
}

// Each refusal of revise-question the contract names, with nothing written.
func TestEachRefusalOfReviseQuestion(t *testing.T) {
	h := newHome(t)
	item := h.item()
	state := func(fixture string) string {
		id := rec(h.raise(h.item()), "question")["question_id"].(string)
		h.settled(id, fixture)
		return id
	}
	id := rec(h.raise(item), "question")["question_id"].(string)
	answered := state("answered-superseding.json")
	released := state("released.json")
	moot := state("moot-question-replaced.json")
	completed := state("released.json")
	rewritten := h.stored("question", completed)
	rewritten["state"] = "completed"
	rewritten["disposition"].(map[string]any)["kind"] = "completed"
	h.plant("question", completed, rewritten)

	refuses := func(name string, o sent, body map[string]any, status envelope.Status, code string) {
		t.Run(name, func(t *testing.T) {
			h.unchanged(func() { refused(t, h.send("revise-question", body, o), status, code) })
		})
	}
	refuses("a question that does not exist", sent{}, revision(mint(t, ids.Question)), envelope.NotFound, envelope.CodeNotFound)
	refuses("a question with an applied answer", sent{}, revision(answered), envelope.Refused, "pool.question_answered")
	refuses("a released question", sent{}, revision(released), envelope.Refused, "pool.question_closed")
	refuses("a completed question", sent{}, revision(completed), envelope.Refused, "pool.question_closed")
	refuses("a moot question", sent{}, revision(moot), envelope.Refused, "pool.question_closed")
	refuses("a stale expected revision", sent{expectedRevs: []rev{{"pool.question", id, 7}}}, revision(id), envelope.Conflict, envelope.CodeRevisionConflict)

	body := revision(id)
	body["options"] = []any{option("keep"), option("keep")}
	body["recommendation"] = "keep"
	refuses("options whose identities repeat", sent{}, body, envelope.Invalid, "pool.option_unknown")
	body = revision(id)
	body["recommendation"] = "elsewhere"
	refuses("a recommendation that is not an option", sent{}, body, envelope.Invalid, "pool.option_unknown")

	t.Run("a question outside the request's scope", func(t *testing.T) {
		ws1, ws2 := h.workspace(), h.workspace()
		p1, p2 := h.project(ws1), h.project(ws2)
		scoped := h.inProject(ws1, p1)
		held := rec(scoped.raise(scoped.item()), "question")["question_id"].(string)
		elsewhere := h.inProject(ws2, p2)
		elsewhere.unchanged(func() {
			refused(t, elsewhere.send("revise-question", revision(held), sent{}), envelope.Refused, envelope.CodeScopeMismatch)
		})
	})
}

// A second question whose origin names the subject of an earlier one is
// refused with already_exists naming the earlier question, whether it holds
// the same item, a decision item of its own or joins the earlier round, and
// writes nothing.
func TestASecondQuestionOnOneSubjectOriginIsRefused(t *testing.T) {
	h := newHome(t)
	subject := map[string]any{"kind": "rule", "ref": map[string]any{"schema": "pool.subject", "id": "subject-one"}}
	about := func(held string) map[string]any {
		q := ask(t, held)
		q["origin"] = subject
		return q
	}
	item := h.item()
	first := h.ok(h.send("hold-question", holdBody(t, about(item), newRound(t)), sent{}))
	earlier := rec(first, "question")["question_id"]

	newItem := about("")
	delete(newItem, "held_task_id")
	newItem["new_item"] = map[string]any{"title": "A decision"}
	for name, body := range map[string]map[string]any{
		"on the same held item":  holdBody(t, about(item), newRound(t)),
		"on another held item":   holdBody(t, about(h.item()), newRound(t)),
		"on a new decision item": holdBody(t, newItem, newRound(t)),
		"in the earlier round":   holdBody(t, about(h.item()), joined(rec(first, "round")["decision_round_id"].(string))),
	} {
		t.Run(name, func(t *testing.T) {
			h.unchanged(func() {
				resp := h.send("hold-question", body, sent{})
				refused(t, resp, envelope.Conflict, envelope.CodeAlreadyExists)
				if resp.Error.Detail["question_id"] != earlier {
					t.Errorf("the refusal names %v, not the earlier question %v", resp.Error.Detail["question_id"], earlier)
				}
			})
		})
	}

	other := ask(t, h.item())
	other["origin"] = map[string]any{"kind": "rule", "ref": map[string]any{"schema": "pool.subject", "id": "subject-two"}}
	h.ok(h.send("hold-question", holdBody(t, other, newRound(t)), sent{}))
}
