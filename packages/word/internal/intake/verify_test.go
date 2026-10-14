package intake_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/word/internal/entry"
)

// The acceptance of spec/word.md, verify and The word stands, against a fake
// hive-pool that answers hive-pool read from the records it was given: verify
// reads the record for each subject kind, lists each reason once and none when
// the word stands, returns the holds and questions it read whole, and records
// and sends nothing.

const (
	readRequestSchema  = "urn:hive:schema:ops:hive-pool.read.request:v1"
	readResponseSchema = "urn:hive:schema:ops:hive-pool.read.response:v1"
	questionSchema     = "urn:hive:schema:records:pool.question:v1"
	holdSchema         = "urn:hive:schema:records:pool.hold:v1"
	itemSchema         = "urn:hive:schema:records:pool.item:v1"
)

// The identities the contract's valid examples carry.
const (
	fxHome     = "home_01jc7d8e9f0g1h2j3k4m5n6p7a"
	fxWs       = "ws_01jc7d8e9f0g1h2j3k4m5n6p7b"
	fxQuestion = "q_01jc7d8e9f0g1h2j3k4m5n6p7q"
	fxHold     = "hold_01jc7d8e9f0g1h2j3k4m5n6p7m"
	fxTask     = "task_01jc7d8e9f0g1h2j3k4m5n6p7f"
	fxCard     = "card_01jc7d8e9f0g1h2j3k4m5n6p7s"
	fxRound    = "dr_01jc7d8e9f0g1h2j3k4m5n6p7r"
)

// readingPool is the fake hive-pool of these tests: the fake that answers the
// handoffs, and a hive-pool read of the question, hold and item kinds, which it
// answers from the records it holds, checking each request it is sent against
// the declared schema of the read.
type readingPool struct {
	t      testing.TB
	homeID string

	mu        sync.Mutex
	questions map[string]map[string]any
	holds     map[string]map[string]any
	items     map[string]map[string]any
	derived   map[string]map[string]any
	// kinds are the kinds read, in order; problems are what the fake found
	// wrong with a request it was sent.
	kinds    []string
	problems []string
	// fail, when set, is the Pool's answer to every read.
	fail *envelope.Failure
}

func (p *readingPool) problem(format string, args ...any) {
	p.problems = append(p.problems, fmt.Sprintf(format, args...))
}

// Read is hive-pool read: the request, the encoded document, answered as the
// document and the exit code the Pool ends with.
func (p *readingPool) Read(c *cli.Call, request []byte) ([]byte, int, error) {
	req, err := envelope.DecodeRequest(request)
	if err != nil {
		return nil, 0, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	answer := func(r *envelope.Response) ([]byte, int, error) {
		r.RequestID, r.Command, r.Operation = req.RequestID, req.Command, req.Operation
		r.Responder = envelope.Responder{Package: "pool", Command: "hive-pool", ImplementationVersion: "0.1.0-fake", ContractVersion: envelope.ContractVersion}
		r.CompletedAt = "2026-10-12T11:00:00.000Z"
		if r.Receipts == nil {
			r.Receipts = []envelope.Receipt{}
		}
		data, err := r.Encode()
		return data, r.Status.ExitCode(), err
	}
	failed := func(f *envelope.Failure) ([]byte, int, error) {
		return answer(&envelope.Response{Status: f.Status, Error: &f.Err})
	}
	if req.Command != "hive-pool" || req.Operation != "read" {
		p.problem("a request to %s %s", req.Command, req.Operation)
		return failed(envelope.Common(envelope.CodeUnknownOperation, "the fake Pool reads only"))
	}
	if req.Caller != (envelope.Caller{Kind: envelope.CallerPackage, Package: "word", Command: "hive-word"}) {
		p.problem("the read is brought by %+v", req.Caller)
	}
	if err := envelope.ValidateBody(req.Body, readRequestSchema); err != nil {
		p.problem("the read is not valid: %v", err)
		return failed(envelope.AsFailure(err))
	}
	kind, _ := req.Body["kind"].(string)
	selector, _ := req.Body["selector"].(map[string]any)
	id, _ := selector["id"].(string)
	task, _ := selector["task_id"].(string)
	p.kinds = append(p.kinds, kind)
	if p.fail != nil {
		return failed(p.fail)
	}
	var records, derived []any
	notFound := func() ([]byte, int, error) {
		return failed(envelope.Common(envelope.CodeNotFound, "the Pool holds no such record"))
	}
	keys := func(m map[string]map[string]any) []string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	switch kind {
	case "question":
		switch {
		case id != "":
			rec, ok := p.questions[id]
			if !ok {
				return notFound()
			}
			records = append(records, rec)
		case task != "":
			for _, k := range keys(p.questions) {
				if p.questions[k]["held_task_id"] == task {
					records = append(records, p.questions[k])
				}
			}
		default:
			p.problem("a read of questions with no selector this fake answers")
			return failed(envelope.Common(envelope.CodeSchemaViolation, "unsupported selector"))
		}
	case "hold":
		switch {
		case id != "":
			rec, ok := p.holds[id]
			if !ok {
				return notFound()
			}
			records = append(records, rec)
		case task != "":
			for _, k := range keys(p.holds) {
				h := p.holds[k]
				if h["task_id"] == task && (selector["active"] != true || h["state"] == "active") {
					records = append(records, h)
				}
			}
		default:
			p.problem("a read of holds with no selector this fake answers")
			return failed(envelope.Common(envelope.CodeSchemaViolation, "unsupported selector"))
		}
	case "item":
		rec, ok := p.items[id]
		if id == "" || !ok {
			return notFound()
		}
		records, derived = append(records, rec), append(derived, p.derived[id])
	default:
		p.problem("a read of kind %s", kind)
		return failed(envelope.Common(envelope.CodeSchemaViolation, "unsupported kind"))
	}
	if records == nil {
		records = []any{}
	}
	body := map[string]any{
		"projection": map[string]any{"home_id": p.homeID, "read_at": "2026-10-12T11:00:00.000Z", "journal_cursor": int64(1), "complete": true},
		"kind":       kind, "records": records,
	}
	if derived != nil {
		body["derived"] = derived
	}
	if err := envelope.ValidateBody(body, readResponseSchema); err != nil {
		p.t.Fatalf("the fake's own answer is not valid: %v", err)
	}
	return answer(&envelope.Response{Status: envelope.Ok, Body: body})
}

// world is the executable over the fake Pool, with the records the Pool holds.
type world struct {
	r *rig
	p *readingPool
}

func newWorld(t testing.TB) *world {
	t.Helper()
	r := newRig(t)
	p := &readingPool{
		t: t, homeID: r.homeID,
		questions: map[string]map[string]any{}, holds: map[string]map[string]any{},
		items: map[string]map[string]any{}, derived: map[string]map[string]any{},
	}
	// The fake of the handoffs is the one every test of the package uses.
	r.exe = entry.ExecutableWith(struct {
		cli.Callee
		*readingPool
	}{r.pool, p})
	return &world{r: r, p: p}
}

// fixture is the valid example of the declared schema, with the identities of
// the home in its place and those given replaced.
func (w *world) fixture(dir, name string, replace map[string]string) map[string]any {
	w.r.t.Helper()
	root, err := os.Getwd()
	if err != nil {
		w.r.t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			w.r.t.Fatal("no go.mod above the working directory")
		}
		root = parent
	}
	data, err := os.ReadFile(filepath.Join(root, "contract", "testdata", "schemas", dir, "valid", name+".json"))
	if err != nil {
		w.r.t.Fatal(err)
	}
	pairs := []string{fxHome, w.r.homeID, fxWs, w.r.workspaceID}
	for from, to := range replace {
		pairs = append(pairs, from, to)
	}
	v, err := canon.Parse([]byte(strings.NewReplacer(pairs...).Replace(string(data))))
	if err != nil {
		w.r.t.Fatal(err)
	}
	return v.(map[string]any)
}

// inHome puts the record in the scope of the home itself, which contains the
// scope of every request.
func inHome(rec map[string]any) {
	rec["scope_level"] = "hive"
	delete(rec, "workspace_id")
	delete(rec, "project_id")
}

// pair is a question and the word hold it answers for.
type pair struct{ q, h map[string]any }

func (p pair) qid() string  { return p.q["question_id"].(string) }
func (p pair) hid() string  { return p.h["hold_id"].(string) }
func (p pair) task() string { return p.q["held_task_id"].(string) }

// release marks the hold released, as a disposition or a close does.
func release(h map[string]any, w *world) {
	h["state"] = "released"
	h["released_at"] = "2026-10-12T10:00:00.000Z"
	h["released_by"] = map[string]any{"schema": "hive.receipt", "id": mint(w.r.t, ids.Receipt)}
}

// activate marks the hold active.
func activate(h map[string]any) {
	h["state"] = "active"
	delete(h, "released_at")
	delete(h, "released_by")
}

// pair is a question in the state, with fresh identities and the word hold it
// answers for in the state that follows from it, both in the scope of the home
// and not yet held by the Pool. A state is open, deferred, answered, released,
// completed, moot, unreconciled (open, with an answer given against an earlier
// revision) or superseding (answered on a second answer that replaced the first).
func (w *world) pair(state string) pair {
	w.r.t.Helper()
	replace := map[string]string{
		fxQuestion: mint(w.r.t, ids.Question), fxHold: mint(w.r.t, ids.Hold), fxTask: mint(w.r.t, ids.Task),
		fxCard: mint(w.r.t, ids.Card), fxRound: mint(w.r.t, ids.DecisionRound),
	}
	file := map[string]string{
		"open": "open-with-options", "deferred": "deferred-by-later", "answered": "released", "released": "released",
		"completed": "released", "moot": "moot-question-replaced", "unreconciled": "unreconciled-late-answer",
		"superseding": "answered-superseding",
	}[state]
	p := pair{
		q: w.fixture("records_pool.question", file, replace),
		h: w.fixture("records_pool.hold", "active-word-workspace-scope", replace),
	}
	inHome(p.q)
	inHome(p.h)
	switch state {
	case "answered":
		p.q["state"] = "answered"
		delete(p.q, "disposition")
	case "completed":
		p.q["state"] = "completed"
		p.q["disposition"].(map[string]any)["kind"] = "completed"
	}
	if slices.Contains([]string{"released", "completed", "moot"}, state) {
		release(p.h, w)
	}
	return p
}

// put gives the Pool the question and the hold, each checked against its
// declared schema.
func (w *world) put(ps ...pair) {
	w.r.t.Helper()
	for _, p := range ps {
		if p.q != nil {
			w.check(p.q, questionSchema)
			w.p.questions[p.qid()] = p.q
		}
		w.putHold(p.h)
	}
}

func (w *world) putHold(h map[string]any) {
	w.r.t.Helper()
	w.check(h, holdSchema)
	w.p.holds[h["hold_id"].(string)] = h
}

func (w *world) check(rec map[string]any, schema string) {
	w.r.t.Helper()
	if err := envelope.ValidateBody(rec, schema); err != nil {
		w.r.t.Fatalf("a record of the test is not valid against %s: %v", schema, err)
	}
}

// importedHold is a word hold no question names, as a legacy import leaves it,
// on a fresh item.
func (w *world) importedHold() map[string]any {
	h := w.fixture("records_pool.hold", "active-word-workspace-scope", map[string]string{
		fxHold: mint(w.r.t, ids.Hold), fxTask: mint(w.r.t, ids.Task),
	})
	inHome(h)
	w.putHold(h)
	return h
}

// otherHold is a hold that is not a word hold, on a fresh item.
func (w *world) otherHold() map[string]any {
	h := w.fixture("records_pool.hold", "active-parked", map[string]string{
		fxHold: mint(w.r.t, ids.Hold), fxTask: mint(w.r.t, ids.Task),
	})
	inHome(h)
	w.putHold(h)
	return h
}

// item is the item, with the active holds the Pool derives for it: its own and
// its ancestors'.
func (w *world) item(task string, active ...string) {
	w.r.t.Helper()
	read := w.fixture("ops_hive-pool.read.response", "items-with-derived-facts", map[string]string{fxTask: task})
	rec := read["records"].([]any)[0].(map[string]any)
	derived := read["derived"].([]any)[0].(map[string]any)
	inHome(rec)
	w.check(rec, itemSchema)
	list := []any{}
	for _, h := range active {
		list = append(list, h)
	}
	derived["active_holds"], derived["held"] = list, len(list) > 0
	w.p.items[task], w.p.derived[task] = rec, derived
}

// verify runs hive-word verify for the subject, as the caller, and checks the
// response against what the Pool and the home must be left as.
func (w *world) verify(subject, caller map[string]any) (*envelope.Response, int) {
	w.r.t.Helper()
	req, err := canon.Encode(map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": "hive-word", "operation": "verify", "operation_version": "1.0",
		"request_id": mint(w.r.t, ids.Request), "correlation_id": mint(w.r.t, ids.Correlation),
		"issued_at": "2026-10-12T11:00:00.123Z", "caller": caller,
		"scope_level": "workspace", "home_id": w.r.homeID, "workspace_id": w.r.workspaceID,
		"authority": map[string]any{"kind": "none"}, "body": map[string]any{"subject": subject},
	})
	if err != nil {
		w.r.t.Fatal(err)
	}
	before := w.r.listing()
	resp, exit := w.r.send("verify", append(req, '\n'))
	if after := w.r.listing(); !slices.Equal(before, after) {
		w.r.t.Errorf("verify changed the home")
	}
	if n := w.r.pool.Count(); n != 0 {
		w.r.t.Errorf("verify sent %d requests that mutate", n)
	}
	for _, kind := range w.p.kinds {
		if !slices.Contains([]string{"question", "hold", "item"}, kind) {
			w.r.t.Errorf("verify read a record of kind %s", kind)
		}
	}
	if len(w.p.problems) > 0 {
		w.r.t.Errorf("verify's reads: %s", strings.Join(w.p.problems, "; "))
	}
	if len(w.r.pool.Reads) != 0 {
		w.r.t.Errorf("verify read the recorded outcome of a request: %v", w.r.pool.Reads)
	}
	return resp, exit
}

// records are the holds and the questions a response must return whole.
type records struct{ holds, questions []map[string]any }

// expect checks the verdict: the word stands or does not, with each of the
// reasons once and no other; none when it stands. A response that lists the
// records it read carries them whole.
func (w *world) expect(resp *envelope.Response, exit int, stands bool, reasons []string, read *records) {
	w.r.t.Helper()
	t := w.r.t
	if exit != 0 || resp.Status != envelope.Ok || resp.Replayed || len(resp.Receipts) != 0 {
		t.Fatalf("exit %d, %s, replayed %v, %d receipts, error %+v", exit, resp.Status, resp.Replayed, len(resp.Receipts), resp.Error)
	}
	if got, _ := resp.Body["stands"].(bool); got != stands {
		t.Errorf("stands %v, want %v (reasons %v)", resp.Body["stands"], stands, resp.Body["reasons"])
	}
	if at, _ := resp.Body["verified_at"].(string); at == "" {
		t.Errorf("no verified_at")
	}
	var got []string
	for _, r := range resp.Body["reasons"].([]any) {
		got = append(got, r.(string))
	}
	if stands && len(got) != 0 {
		t.Errorf("the word stands and lists %v", got)
	}
	if !stands && len(got) == 0 {
		t.Errorf("the word does not stand and lists no reason")
	}
	sorted := slices.Clone(got)
	slices.Sort(sorted)
	if len(slices.Compact(slices.Clone(sorted))) != len(sorted) {
		t.Errorf("a reason is listed twice: %v", got)
	}
	want := slices.Clone(reasons)
	slices.Sort(want)
	if !slices.Equal(sorted, want) {
		t.Errorf("reasons %v, want %v", got, reasons)
	}
	if read == nil {
		return
	}
	for _, c := range []struct {
		key  string
		want []map[string]any
		id   string
	}{{"holds", read.holds, "hold_id"}, {"questions", read.questions, "question_id"}} {
		list, _ := resp.Body[c.key].([]any)
		if len(list) != len(c.want) {
			t.Errorf("%s: %d returned, want %d", c.key, len(list), len(c.want))
			continue
		}
		for _, rec := range c.want {
			found := false
			for _, g := range list {
				if g.(map[string]any)[c.id] == rec[c.id] {
					found = true
					if !same(t, g, rec) {
						t.Errorf("%s %v is not returned whole", c.key, rec[c.id])
					}
				}
			}
			if !found {
				t.Errorf("%s %v is not returned", c.key, rec[c.id])
			}
		}
	}
}

func questionSubject(id string) map[string]any {
	return map[string]any{"kind": "question", "question_id": id}
}
func holdSubject(id string) map[string]any { return map[string]any{"kind": "hold", "hold_id": id} }
func taskSubject(id string) map[string]any { return map[string]any{"kind": "task", "task_id": id} }

// wordRef is the authority of kind word naming the question at the record
// revision.
func wordRef(question string, revision int64) map[string]any {
	return map[string]any{"schema": "pool.question", "id": question, "record_revision": revision}
}

func authoritySubject(kind string, ref map[string]any) map[string]any {
	return map[string]any{"kind": "authority", "authority": map[string]any{"kind": kind, "ref": ref}}
}

// A question subject stands when it has an applied answer: answered, released
// or completed. An open, deferred or moot one never does, and an answer that
// awaits reconciliation is a reason beside the state's.
func TestVerifyAQuestion(t *testing.T) {
	for _, c := range []struct {
		state   string
		stands  bool
		reasons []string
	}{
		{"answered", true, nil},
		{"released", true, nil},
		{"completed", true, nil},
		{"superseding", true, nil},
		{"open", false, []string{"unanswered"}},
		{"deferred", false, []string{"deferred"}},
		{"moot", false, []string{"moot"}},
		{"unreconciled", false, []string{"unanswered", "unreconciled"}},
	} {
		t.Run(c.state, func(t *testing.T) {
			w := newWorld(t)
			p := w.pair(c.state)
			w.put(p)
			resp, exit := w.verify(questionSubject(p.qid()), w.r.supervisor())
			w.expect(resp, exit, c.stands, c.reasons, &records{questions: []map[string]any{p.q}})
		})
	}
	t.Run("an answer that awaits reconciliation beside an applied one", func(t *testing.T) {
		w := newWorld(t)
		p := w.pair("released")
		late := map[string]any{}
		for k, v := range p.q["answers"].([]any)[0].(map[string]any) {
			late[k] = v
		}
		late["sequence"], late["disposition"] = int64(2), "unreconciled"
		late["request_id"], late["receipt_id"] = mint(t, ids.Request), mint(t, ids.Receipt)
		late["record_revision"] = p.q["record_revision"]
		p.q["answers"] = append(p.q["answers"].([]any), late)
		w.put(p)
		resp, exit := w.verify(questionSubject(p.qid()), w.r.supervisor())
		w.expect(resp, exit, true, nil, nil)
	})
	t.Run("a question the Pool does not hold", func(t *testing.T) {
		w := newWorld(t)
		resp, exit := w.verify(questionSubject(mint(t, ids.Question)), w.r.supervisor())
		w.expect(resp, exit, false, []string{"not-found"}, &records{})
	})
	t.Run("the scope of the envelope is not asked of a question subject", func(t *testing.T) {
		w := newWorld(t)
		p := w.pair("released")
		p.q["scope_level"], p.q["workspace_id"], p.q["project_id"] = "project", w.r.workspaceID, mint(t, ids.Project)
		w.put(p)
		resp, exit := w.verify(questionSubject(p.qid()), w.r.supervisor())
		w.expect(resp, exit, true, nil, nil)
	})
}

// A hold subject stands when it is a word hold released by the disposition of
// an applied answer, release or complete. Every other hold gives the reason
// the question it answers for gives, or no-question for an imported hold no
// question names.
func TestVerifyAHold(t *testing.T) {
	for _, c := range []struct {
		state   string
		stands  bool
		reasons []string
	}{
		{"released", true, nil},
		{"completed", true, nil},
		{"answered", false, []string{"disposition-pending"}},
		{"superseding", false, []string{"disposition-pending"}},
		{"open", false, []string{"unanswered"}},
		{"deferred", false, []string{"deferred"}},
		{"unreconciled", false, []string{"unanswered", "unreconciled"}},
		{"moot", false, []string{"moot"}},
	} {
		t.Run(c.state, func(t *testing.T) {
			w := newWorld(t)
			p := w.pair(c.state)
			w.put(p)
			resp, exit := w.verify(holdSubject(p.hid()), w.r.supervisor())
			w.expect(resp, exit, c.stands, c.reasons, &records{holds: []map[string]any{p.h}, questions: []map[string]any{p.q}})
		})
	}
	t.Run("an imported hold with no question", func(t *testing.T) {
		w := newWorld(t)
		h := w.importedHold()
		resp, exit := w.verify(holdSubject(h["hold_id"].(string)), w.r.supervisor())
		w.expect(resp, exit, false, []string{"no-question"}, &records{holds: []map[string]any{h}})
	})
	t.Run("a hold that is not a word hold", func(t *testing.T) {
		w := newWorld(t)
		h := w.otherHold()
		resp, exit := w.verify(holdSubject(h["hold_id"].(string)), w.r.supervisor())
		w.expect(resp, exit, false, []string{"not-word"}, nil)
	})
	t.Run("a hold the Pool does not hold", func(t *testing.T) {
		w := newWorld(t)
		resp, exit := w.verify(holdSubject(mint(t, ids.Hold)), w.r.supervisor())
		w.expect(resp, exit, false, []string{"not-found"}, &records{})
	})
	t.Run("a released hold of a question still open", func(t *testing.T) {
		w := newWorld(t)
		p := w.pair("open")
		release(p.h, w)
		w.put(p)
		resp, exit := w.verify(holdSubject(p.hid()), w.r.supervisor())
		w.expect(resp, exit, false, []string{"malformed"}, nil)
	})
}

// A task subject stands when no word hold on the item or on an ancestor is
// active; each active word hold gives the reasons of its question, once in all.
func TestVerifyATask(t *testing.T) {
	t.Run("no hold", func(t *testing.T) {
		w := newWorld(t)
		task := mint(t, ids.Task)
		w.item(task)
		resp, exit := w.verify(taskSubject(task), w.r.supervisor())
		w.expect(resp, exit, true, nil, nil)
	})
	t.Run("a word hold released by its disposition", func(t *testing.T) {
		w := newWorld(t)
		p := w.pair("released")
		w.put(p)
		w.item(p.task())
		resp, exit := w.verify(taskSubject(p.task()), w.r.supervisor())
		w.expect(resp, exit, true, nil, nil)
	})
	t.Run("a hold that is not a word hold", func(t *testing.T) {
		w := newWorld(t)
		h := w.otherHold()
		w.item(h["task_id"].(string), h["hold_id"].(string))
		resp, exit := w.verify(taskSubject(h["task_id"].(string)), w.r.supervisor())
		w.expect(resp, exit, true, nil, nil)
	})
	for _, c := range []struct {
		state   string
		reasons []string
	}{
		{"open", []string{"unanswered"}},
		{"deferred", []string{"deferred"}},
		{"answered", []string{"disposition-pending"}},
		{"unreconciled", []string{"unanswered", "unreconciled"}},
	} {
		t.Run("an active word hold of a question "+c.state, func(t *testing.T) {
			w := newWorld(t)
			p := w.pair(c.state)
			w.put(p)
			w.item(p.task(), p.hid())
			resp, exit := w.verify(taskSubject(p.task()), w.r.supervisor())
			w.expect(resp, exit, false, c.reasons, &records{holds: []map[string]any{p.h}, questions: []map[string]any{p.q}})
		})
		t.Run("an active word hold on an ancestor of a question "+c.state, func(t *testing.T) {
			w := newWorld(t)
			p := w.pair(c.state)
			w.put(p)
			child := mint(t, ids.Task)
			w.item(child, p.hid())
			resp, exit := w.verify(taskSubject(child), w.r.supervisor())
			w.expect(resp, exit, false, c.reasons, &records{holds: []map[string]any{p.h}, questions: []map[string]any{p.q}})
		})
	}
	t.Run("an imported hold with no question", func(t *testing.T) {
		w := newWorld(t)
		h := w.importedHold()
		w.item(h["task_id"].(string), h["hold_id"].(string))
		resp, exit := w.verify(taskSubject(h["task_id"].(string)), w.r.supervisor())
		w.expect(resp, exit, false, []string{"no-question"}, &records{holds: []map[string]any{h}})
	})
	t.Run("a word hold of a question already released", func(t *testing.T) {
		w := newWorld(t)
		p := w.pair("released")
		activate(p.h)
		w.put(p)
		w.item(p.task(), p.hid())
		resp, exit := w.verify(taskSubject(p.task()), w.r.supervisor())
		w.expect(resp, exit, false, []string{"malformed"}, nil)
	})
	t.Run("a task the Pool does not hold", func(t *testing.T) {
		w := newWorld(t)
		resp, exit := w.verify(taskSubject(mint(t, ids.Task)), w.r.supervisor())
		w.expect(resp, exit, false, []string{"not-found"}, &records{})
	})
}

// Each reason is listed once however many holds and records give it, and the
// reasons of several holds are listed together.
func TestVerifyListsEachReasonOnce(t *testing.T) {
	t.Run("two holds that give one reason", func(t *testing.T) {
		w := newWorld(t)
		a, b := w.pair("open"), w.pair("open")
		w.put(a, b)
		w.item(a.task(), a.hid(), b.hid())
		// The second question holds the item the first does.
		b.q["held_task_id"], b.h["task_id"] = a.task(), a.task()
		w.put(b)
		resp, exit := w.verify(taskSubject(a.task()), w.r.supervisor())
		w.expect(resp, exit, false, []string{"unanswered"}, &records{holds: []map[string]any{a.h, b.h}, questions: []map[string]any{a.q, b.q}})
	})
	t.Run("two holds that give two reasons", func(t *testing.T) {
		w := newWorld(t)
		a, b := w.pair("open"), w.pair("deferred")
		b.q["held_task_id"], b.h["task_id"] = a.task(), a.task()
		w.put(a, b)
		w.item(a.task(), a.hid(), b.hid())
		resp, exit := w.verify(taskSubject(a.task()), w.r.supervisor())
		w.expect(resp, exit, false, []string{"unanswered", "deferred"}, &records{holds: []map[string]any{a.h, b.h}, questions: []map[string]any{a.q, b.q}})
	})
}

// An authority of kind word stands only on the answer applied at the record
// revision it names, in a scope that contains the envelope's: the record is
// read and the reference is never trusted.
func TestVerifyAnAuthority(t *testing.T) {
	for _, c := range []struct {
		name     string
		state    string
		revision int64
		stands   bool
		reasons  []string
	}{
		// released: answered at record revision 2, released at 3.
		{"the revision of the question", "released", 3, true, nil},
		{"the revision the answer was applied at", "released", 2, true, nil},
		{"a revision before any answer was applied", "released", 1, false, []string{"unanswered"}},
		{"a revision the question has not reached", "released", 4, false, []string{"malformed"}},
		{"answered and its disposition pending", "answered", 3, true, nil},
		{"completed", "completed", 3, true, nil},
		// superseding: answered at revision 2, replaced by another at 3.
		{"the superseding answer", "superseding", 3, true, nil},
		{"the answer since superseded", "superseding", 2, false, []string{"superseded"}},
		{"a revision before the first answer", "superseding", 1, false, []string{"unanswered"}},
		{"a deferred question", "deferred", 2, false, []string{"deferred"}},
		{"an open question", "open", 1, false, []string{"unanswered"}},
		{"a moot question", "moot", 2, false, []string{"moot"}},
		{"an open question with an answer awaiting reconciliation", "unreconciled", 3, false, []string{"unanswered", "unreconciled"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			p := w.pair(c.state)
			w.put(p)
			resp, exit := w.verify(authoritySubject("word", wordRef(p.qid(), c.revision)), w.r.supervisor())
			w.expect(resp, exit, c.stands, c.reasons, &records{questions: []map[string]any{p.q}})
		})
	}
	t.Run("a question the Pool does not hold", func(t *testing.T) {
		w := newWorld(t)
		resp, exit := w.verify(authoritySubject("word", wordRef(mint(t, ids.Question), 1)), w.r.supervisor())
		w.expect(resp, exit, false, []string{"not-found"}, &records{})
	})
	t.Run("an authority that is not of kind word", func(t *testing.T) {
		w := newWorld(t)
		p := w.pair("released")
		w.put(p)
		resp, exit := w.verify(authoritySubject("posture", wordRef(p.qid(), 3)), w.r.supervisor())
		w.expect(resp, exit, false, []string{"not-word"}, nil)
	})
	t.Run("a reference that names another schema", func(t *testing.T) {
		w := newWorld(t)
		p := w.pair("released")
		w.put(p)
		ref := wordRef(p.qid(), 3)
		ref["schema"] = "pool.hold"
		resp, exit := w.verify(authoritySubject("word", ref), w.r.supervisor())
		w.expect(resp, exit, false, []string{"malformed"}, nil)
	})
	t.Run("a reference that names no revision", func(t *testing.T) {
		w := newWorld(t)
		p := w.pair("released")
		w.put(p)
		ref := wordRef(p.qid(), 3)
		delete(ref, "record_revision")
		resp, exit := w.verify(authoritySubject("word", ref), w.r.supervisor())
		w.expect(resp, exit, false, []string{"malformed"}, nil)
	})
	for _, c := range []struct {
		name   string
		scope  func(q map[string]any, w *world)
		stands bool
	}{
		{"a question of the home", func(q map[string]any, w *world) {}, true},
		{"a question of the workspace", func(q map[string]any, w *world) {
			q["scope_level"], q["workspace_id"] = "workspace", w.r.workspaceID
		}, true},
		{"a question of another workspace", func(q map[string]any, w *world) {
			q["scope_level"], q["workspace_id"] = "workspace", mint(w.r.t, ids.Workspace)
		}, false},
		{"a question of a project, for a request of its workspace", func(q map[string]any, w *world) {
			q["scope_level"], q["workspace_id"], q["project_id"] = "project", w.r.workspaceID, mint(w.r.t, ids.Project)
		}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			p := w.pair("released")
			c.scope(p.q, w)
			w.put(p)
			resp, exit := w.verify(authoritySubject("word", wordRef(p.qid(), 3)), w.r.supervisor())
			var reasons []string
			if !c.stands {
				reasons = []string{"out-of-scope"}
			}
			w.expect(resp, exit, c.stands, reasons, nil)
		})
	}
}

// Verify is read-only and may be repeated freely: it records nothing, sends
// nothing that mutates, replays nothing, and answers the same again.
func TestVerifyIsRepeatable(t *testing.T) {
	w := newWorld(t)
	p := w.pair("answered")
	w.put(p)
	subject := authoritySubject("word", wordRef(p.qid(), 3))
	first, exit := w.verify(subject, w.r.operator())
	w.expect(first, exit, true, nil, &records{questions: []map[string]any{p.q}})
	again, exit := w.verify(subject, w.r.operator())
	w.expect(again, exit, true, nil, &records{questions: []map[string]any{p.q}})
	a, b := first.Body, again.Body
	delete(a, "verified_at")
	delete(b, "verified_at")
	if !same(t, a, b) {
		t.Errorf("a second verify answered another verdict")
	}
}

// A Pool that gives no answer to a read is no verdict: the Pool's own failure
// is returned as it gave it, never as a word that does not stand.
func TestVerifyReturnsThePoolsFailureAsGiven(t *testing.T) {
	w := newWorld(t)
	p := w.pair("released")
	w.put(p)
	w.p.fail = envelope.Common(envelope.CodeDependencyUnusable, "the Pool cannot be read")
	for _, subject := range []map[string]any{
		questionSubject(p.qid()), holdSubject(p.hid()), authoritySubject("word", wordRef(p.qid(), 3)),
	} {
		resp, exit := w.verify(subject, w.r.supervisor())
		if exit != envelope.Unavailable.ExitCode() || resp.Status != envelope.Unavailable || resp.Error == nil || resp.Error.Code != envelope.CodeDependencyUnusable || resp.Body != nil {
			t.Errorf("%v: exit %d, %s, %+v", subject["kind"], exit, resp.Status, resp.Error)
		}
	}
}
