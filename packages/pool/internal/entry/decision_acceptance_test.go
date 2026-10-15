package entry

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/store"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The acceptance test of the Pool's decision records and of the recovering
// reads (spec/pool.md, Reading the Pool; spec/word.md, A question's life;
// spec/commands.md, Replay): the kinds question, decision-round and
// request-outcome of hive-pool read, over records written through the
// transaction boundary the way the operations that write them will write them.

const (
	early  = "2026-10-12T09:00:00.000Z"
	middle = "2026-10-20T09:00:00.000Z"
	late   = "2026-11-02T09:00:00.000Z"
)

// decisionPool is a home with the readers of the decision kinds registered.
func decisionPool(t testing.TB) *pool {
	t.Helper()
	p := newPool(t)
	RegisterDecisionReads(p.reg)
	p.exe = p.reg.Executable()
	return p
}

// fixture is a valid record of the contract's fixtures, which the schemas are
// proved against.
func fixture(t testing.TB, kind, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas", "records_pool."+kind, "valid", name))
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)
}

// question is a record of the fixture, made a question of its own: its
// identities, its project, the item it holds and, for a deferred question, the
// date it is revisited.
func question(t testing.TB, base, project, task, revisit string) (string, map[string]any) {
	t.Helper()
	body := fixture(t, "question", base)
	id := mint(t, ids.Question)
	body["question_id"], body["project_id"], body["held_task_id"] = id, project, task
	if revisit != "" {
		body["deferral"].(map[string]any)["revisit_at"] = revisit
		body["answers"].([]any)[0].(map[string]any)["later"].(map[string]any)["revisit_at"] = revisit
	}
	return id, body
}

func round(t testing.TB, base, project string) (string, map[string]any) {
	t.Helper()
	body := fixture(t, "decision-round", base)
	id := mint(t, ids.DecisionRound)
	body["decision_round_id"], body["project_id"] = id, project
	return id, body
}

// read sends hive-pool read for the kind and selector.
func (p *pool) read(kind string, selector map[string]any) *envelope.Response {
	p.t.Helper()
	return p.do("read", map[string]any{"kind": kind, "selector": selector})
}

// found is the identities of the records a read returned, in order, by the
// member that names them.
func (p *pool) found(kind string, selector map[string]any, member string) []string {
	p.t.Helper()
	resp := p.read(kind, selector)
	if resp.Status != envelope.Ok {
		p.t.Fatalf("read %s %v: %s %+v", kind, selector, resp.Status, resp.Error)
	}
	out := []string{}
	for _, r := range resp.Body["records"].([]any) {
		out = append(out, r.(map[string]any)[member].(string))
	}
	return out
}

func sorted(ids ...string) []string {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

func sameIDs(t testing.TB, what string, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(sorted(got...), sorted(want...)) {
		t.Errorf("%s: got %v, want %v", what, sorted(got...), sorted(want...))
	}
}

// onDisk is the record as the store wrote it.
func (p *pool) onDisk(path string) map[string]any {
	p.t.Helper()
	data, err := os.ReadFile(filepath.Join(p.dir, filepath.FromSlash(path)))
	if err != nil {
		p.t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		p.t.Fatal(err)
	}
	return v.(map[string]any)
}

func sameDocument(t testing.TB, what string, got, want any) {
	t.Helper()
	a, err1 := canon.Encode(map[string]any{"v": got})
	b, err2 := canon.Encode(map[string]any{"v": want})
	if err1 != nil || err2 != nil || string(a) != string(b) {
		t.Errorf("%s:\n got %s\nwant %s", what, a, b)
	}
}

// Questions and decision rounds come back whole by identity and by query, and
// an identity with no record is not_found.
func TestQuestionsAndRoundsAreReadByIdentityAndQuery(t *testing.T) {
	p := decisionPool(t)
	p1, p2 := mint(t, ids.Project), mint(t, ids.Project)
	t1, t2 := mint(t, ids.Task), mint(t, ids.Task)
	open, openBody := question(t, "open-with-options.json", p1, t1, "")
	deferred, deferredBody := question(t, "deferred-by-later.json", p1, t2, middle)
	other, otherBody := question(t, "deferred-by-later.json", p2, t1, early)
	released, releasedBody := question(t, "released.json", p1, t1, "")
	r1, r1Body := round(t, "open-two-cards.json", p1)
	r2, r2Body := round(t, "closed-answered-and-moot.json", p2)
	write := func(kind, id string, body map[string]any) {
		t.Helper()
		if resp := p.write(poolstore.Transaction{Records: []poolstore.Write{{Kind: kind, ID: id, Body: body}}}); resp.Status != envelope.Ok {
			t.Fatalf("write %s %s: %s %+v", kind, id, resp.Status, resp.Error)
		}
	}
	write("question", open, openBody)
	write("question", deferred, deferredBody)
	write("question", other, otherBody)
	write("question", released, releasedBody)
	write("decision-round", r1, r1Body)
	write("decision-round", r2, r2Body)

	t.Run("a question by identity is whole", func(t *testing.T) {
		resp := p.read("question", map[string]any{"id": deferred})
		if resp.Status != envelope.Ok || resp.Body["kind"] != "question" {
			t.Fatalf("%s %+v", resp.Status, resp.Error)
		}
		records := resp.Body["records"].([]any)
		if len(records) != 1 {
			t.Fatalf("records %v", records)
		}
		sameDocument(t, "the record", records[0], p.onDisk("pool/records/question/"+deferred+".json"))
		if h := resp.Body["projection"].(map[string]any); h["complete"] != true || h["home_id"] != p.homeID {
			t.Errorf("projection %v", h)
		}
	})
	t.Run("a decision round by identity is whole", func(t *testing.T) {
		resp := p.read("decision-round", map[string]any{"id": r1})
		if resp.Status != envelope.Ok || resp.Body["kind"] != "decision-round" {
			t.Fatalf("%s %+v", resp.Status, resp.Error)
		}
		sameDocument(t, "the record", resp.Body["records"].([]any)[0], p.onDisk("pool/records/decision-round/"+r1+".json"))
	})
	t.Run("an identity with no record is not found", func(t *testing.T) {
		for _, kind := range []string{"question", "decision-round"} {
			id := mint(t, ids.Question)
			resp := p.read(kind, map[string]any{"id": id})
			if resp.Status != envelope.NotFound || resp.Error == nil || resp.Error.Code != envelope.CodeNotFound {
				t.Errorf("%s: %s %+v", kind, resp.Status, resp.Error)
			}
		}
	})
	t.Run("questions by query", func(t *testing.T) {
		const key = "question_id"
		sameIDs(t, "all", p.found("question", map[string]any{}, key), open, deferred, other, released)
		sameIDs(t, "task_id", p.found("question", map[string]any{"task_id": t1}, key), open, other, released)
		sameIDs(t, "project_id", p.found("question", map[string]any{"project_id": p1}, key), open, deferred, released)
		sameIDs(t, "states", p.found("question", map[string]any{"states": []any{"deferred"}}, key), deferred, other)
		sameIDs(t, "states, project and task", p.found("question", map[string]any{"states": []any{"open", "released"}, "project_id": p1, "task_id": t1}, key), open, released)
		sameIDs(t, "no match", p.found("question", map[string]any{"states": []any{"moot"}}, key))
		resp := p.read("question", map[string]any{"limit": int64(2)})
		header := resp.Body["projection"].(map[string]any)
		if n := len(resp.Body["records"].([]any)); n != 2 || header["complete"] != false || len(header["omitted"].([]any)) != 2 {
			t.Errorf("a limit of 2: %d records, header %v", n, header)
		}
	})
	t.Run("a deferred question is due at or before its revisit date", func(t *testing.T) {
		const key = "question_id"
		sameIDs(t, "before every date", p.found("question", map[string]any{"due_before": "2026-10-01T00:00:00.000Z"}, key))
		sameIDs(t, "after one", p.found("question", map[string]any{"due_before": "2026-10-15T00:00:00.000Z"}, key), other)
		sameIDs(t, "at the revisit date", p.found("question", map[string]any{"due_before": middle}, key), other, deferred)
		sameIDs(t, "after both; open and released questions are never due", p.found("question", map[string]any{"due_before": late}, key), other, deferred)
		sameIDs(t, "with a project", p.found("question", map[string]any{"due_before": late, "project_id": p2}, key), other)
		resp := p.read("question", map[string]any{"due_before": "yesterday"})
		if resp.Status != envelope.Invalid || resp.Error == nil || resp.Error.Code != envelope.CodeSchemaViolation {
			t.Errorf("a due_before that is no instant: %s %+v", resp.Status, resp.Error)
		}
	})
	t.Run("decision rounds by query", func(t *testing.T) {
		const key = "decision_round_id"
		sameIDs(t, "all", p.found("decision-round", map[string]any{}, key), r1, r2)
		sameIDs(t, "project_id", p.found("decision-round", map[string]any{"project_id": p2}, key), r2)
		sameIDs(t, "states", p.found("decision-round", map[string]any{"states": []any{"open"}}, key), r1)
		sameIDs(t, "states and project", p.found("decision-round", map[string]any{"states": []any{"open"}, "project_id": p2}, key))
		resp := p.read("decision-round", map[string]any{"limit": int64(1)})
		if n := len(resp.Body["records"].([]any)); n != 1 || resp.Body["projection"].(map[string]any)["complete"] != false {
			t.Errorf("a limit of 1: %d records, header %v", n, resp.Body["projection"])
		}
	})
}

// A kind takes the selector members it declares and no other.
func TestEachDecisionKindDeclaresItsSelectorMembers(t *testing.T) {
	p := decisionPool(t)
	for _, tc := range []struct {
		kind     string
		selector map[string]any
		ok       bool
	}{
		{"question", map[string]any{"id": "x", "task_id": mint(t, ids.Task), "project_id": mint(t, ids.Project), "states": []any{"open"}, "due_before": early, "limit": int64(1)}, true},
		{"question", map[string]any{"ready": true}, false},
		{"question", map[string]any{"active": true}, false},
		{"decision-round", map[string]any{"id": "x", "project_id": mint(t, ids.Project), "states": []any{"open"}, "limit": int64(1)}, true},
		{"decision-round", map[string]any{"task_id": mint(t, ids.Task)}, false},
		{"decision-round", map[string]any{"due_before": early}, false},
		{"request-outcome", map[string]any{"id": mint(t, ids.Request)}, true},
		{"request-outcome", map[string]any{"id": mint(t, ids.Request), "limit": int64(1)}, false},
		{"request-outcome", map[string]any{"id": mint(t, ids.Request), "states": []any{"ok"}}, false},
		{"request-outcome", map[string]any{"task_id": mint(t, ids.Task)}, false},
	} {
		resp := p.read(tc.kind, tc.selector)
		refused := resp.Status == envelope.Invalid && resp.Error != nil && resp.Error.Code == envelope.CodeSchemaViolation
		switch {
		case tc.ok && refused, !tc.ok && !refused:
			t.Errorf("%s %v: %s %+v", tc.kind, tc.selector, resp.Status, resp.Error)
		}
		if !tc.ok && p.files("pool") != nil && len(p.files("pool")) != 0 {
			t.Errorf("a refused read left files")
		}
	}
}

// outcomeFile is the path of the recorded outcome of a request.
func outcomeFile(requestID string) string { return store.OutcomePath("pool", requestID) }

// A recorded outcome is read by its request_id: the digest it is keyed on and
// the response as it was returned. A request that is not recorded is
// not_found.
func TestARecordedOutcomeIsReadByItsRequest(t *testing.T) {
	p := decisionPool(t)
	id, body := question(t, "open-with-options.json", mint(t, ids.Project), mint(t, ids.Task), "")
	written := p.write(poolstore.Transaction{Records: []poolstore.Write{{Kind: "question", ID: id, Body: body}}})
	if written.Status != envelope.Ok {
		t.Fatalf("write: %s %+v", written.Status, written.Error)
	}
	resp := p.read("request-outcome", map[string]any{"id": written.RequestID})
	if resp.Status != envelope.Ok || resp.Body["kind"] != "request-outcome" {
		t.Fatalf("%s %+v", resp.Status, resp.Error)
	}
	records := resp.Body["records"].([]any)
	if len(records) != 1 {
		t.Fatalf("records %v", records)
	}
	outcome := records[0].(map[string]any)
	sameDocument(t, "the record", outcome, p.onDisk(outcomeFile(written.RequestID)))
	response, _ := outcome["response"].(map[string]any)
	digest, _ := outcome["request_digest"].(string)
	if outcome["schema"] != "hive.request-outcome" || outcome["request_id"] != written.RequestID || !strings.HasPrefix(digest, "sha256:") ||
		response["request_id"] != written.RequestID || response["status"] != "ok" {
		t.Errorf("outcome %v", outcome)
	}
	if h := resp.Body["projection"].(map[string]any); h["complete"] != true {
		t.Errorf("projection %v", h)
	}

	for name, requestID := range map[string]string{"a request never made": mint(t, ids.Request), "an identity that is no request": "not-a-request"} {
		resp := p.read("request-outcome", map[string]any{"id": requestID})
		if resp.Status != envelope.NotFound || resp.Error == nil || resp.Error.Code != envelope.CodeNotFound || resp.Body != nil {
			t.Errorf("%s: %s %+v", name, resp.Status, resp.Error)
		}
	}
}

// An outcome whose intent is durable is never read as absent: the read takes
// the lock shared, which completes the interrupted transaction, and the
// outcome is read whole.
func TestAnOutcomeWhoseIntentIsDurableIsNotReadAsAbsent(t *testing.T) {
	p := decisionPool(t)
	id, body := question(t, "open-with-options.json", mint(t, ids.Project), mint(t, ids.Task), "")
	p.exe.Fault = func(step string) error {
		if strings.HasPrefix(step, "rename pool/records/") {
			return os.ErrClosed
		}
		return nil
	}
	interrupted := p.write(poolstore.Transaction{Records: []poolstore.Write{{Kind: "question", ID: id, Body: body}}})
	p.exe.Fault = nil
	if interrupted.Status != envelope.Unknown {
		t.Fatalf("an interrupted write is %s, not unknown: %+v", interrupted.Status, interrupted.Error)
	}
	if _, err := os.Stat(filepath.Join(p.dir, filepath.FromSlash(outcomeFile(interrupted.RequestID)))); err == nil {
		t.Fatal("the outcome was written before the crash")
	}
	if files := p.files("pool/tx"); len(files) != 1 {
		t.Fatalf("the intent is not durable: %v", files)
	}

	resp := p.read("request-outcome", map[string]any{"id": interrupted.RequestID})
	if resp.Status != envelope.Ok {
		t.Fatalf("the outcome of an interrupted transaction: %s %+v", resp.Status, resp.Error)
	}
	records := resp.Body["records"].([]any)
	if len(records) != 1 || records[0].(map[string]any)["request_id"] != interrupted.RequestID {
		t.Fatalf("records %v", records)
	}
	sameDocument(t, "the record", records[0], p.onDisk(outcomeFile(interrupted.RequestID)))
	if files := p.files("pool/tx"); len(files) != 0 {
		t.Errorf("the intent is still there: %v", slices.Sorted(mapKeys(files)))
	}
	sameIDs(t, "the question the transaction wrote", p.found("question", map[string]any{"id": id}, "question_id"), id)
}

// A recorded outcome that fails its schema is unavailable with
// record_malformed, and is never read as absent or guessed.
func TestAMalformedOutcomeIsUnavailable(t *testing.T) {
	p := decisionPool(t)
	id, body := question(t, "open-with-options.json", mint(t, ids.Project), mint(t, ids.Task), "")
	written := p.write(poolstore.Transaction{Records: []poolstore.Write{{Kind: "question", ID: id, Body: body}}})
	if written.Status != envelope.Ok {
		t.Fatalf("write: %s %+v", written.Status, written.Error)
	}
	good := p.onDisk(outcomeFile(written.RequestID))
	for name, damage := range map[string]func(map[string]any){
		"no digest":             func(m map[string]any) { delete(m, "request_digest") },
		"another request":       func(m map[string]any) { m["request_id"] = mint(t, ids.Request) },
		"a response that isn't": func(m map[string]any) { m["response"] = map[string]any{"status": "ok"} },
		"another schema":        func(m map[string]any) { m["schema"] = "hive.response" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := map[string]any{}
			for k, v := range good {
				bad[k] = v
			}
			damage(bad)
			data, err := canon.Encode(bad)
			if err != nil {
				t.Fatal(err)
			}
			p.put(filepath.Join(p.dir, filepath.FromSlash(outcomeFile(written.RequestID))), append(data, '\n'))
			resp := p.read("request-outcome", map[string]any{"id": written.RequestID})
			if resp.Status != envelope.Unavailable || resp.Error == nil || resp.Error.Code != envelope.CodeRecordMalformed || resp.Body != nil {
				t.Errorf("%s %+v", resp.Status, resp.Error)
			}
		})
	}
	t.Run("a file that is not a document", func(t *testing.T) {
		p.put(filepath.Join(p.dir, filepath.FromSlash(outcomeFile(written.RequestID))), []byte("not json\n"))
		resp := p.read("request-outcome", map[string]any{"id": written.RequestID})
		if resp.Status != envelope.Unavailable || resp.Error == nil || resp.Error.Code != envelope.CodeRecordMalformed {
			t.Errorf("%s %+v", resp.Status, resp.Error)
		}
	})
}

// A deferred question whose revisit date is no real instant is omitted as
// malformed from a read by date, and the read does not claim to be complete.
func TestAQueryByDateNamesTheQuestionWhoseRevisitDateIsNoInstant(t *testing.T) {
	p := decisionPool(t)
	id, body := question(t, "deferred-by-later.json", mint(t, ids.Project), mint(t, ids.Task), "2026-02-30T09:00:00.000Z")
	if written := p.write(poolstore.Transaction{Records: []poolstore.Write{{Kind: "question", ID: id, Body: body}}}); written.Status != envelope.Ok {
		t.Fatalf("write: %s %+v", written.Status, written.Error)
	}
	resp := p.read("question", map[string]any{"due_before": "2027-01-01T00:00:00.000Z"})
	if resp.Status != envelope.Ok {
		t.Fatalf("read: %s %+v", resp.Status, resp.Error)
	}
	header := resp.Body["projection"].(map[string]any)
	omitted, _ := header["omitted"].([]any)
	if n := len(resp.Body["records"].([]any)); n != 0 || header["complete"] != false || len(omitted) != 1 {
		t.Errorf("%d records, header %v", n, header)
	}
}
