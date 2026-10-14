package intake_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/word/internal/entry"
	"github.com/kesk-git/hive/packages/word/internal/poolfake"
)

// The acceptance of spec/word.md, How word persists through the Pool and
// What test-word proves, for the five operations that are one handoff each:
// hold, revise, release, complete and close, against the fake hive-pool.

// poolOperation is the hive-pool operation each hive-word operation hands off
// to.
var poolOperation = map[string]string{
	"hold": "hold-question", "revise": "revise-question", "release": "release-question",
	"complete": "complete-question", "close": "close-question",
}

var operations = []string{"hold", "revise", "release", "complete", "close"}

// The words of every request: kept exactly, spacing and punctuation included.
const words = "  Keep   this\ttext exactly — 'quoted', \"escaped\"  "

// rig is the executable over the fake Pool, in a home of its own.
type rig struct {
	t    testing.TB
	home string
	exe  *cli.Executable
	pool *poolfake.Pool

	homeID, workspaceID string
}

func mint(t testing.TB, p ids.Prefix) string {
	t.Helper()
	id, err := ids.Mint(clock.System{}, p)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func newRig(t testing.TB) *rig {
	t.Helper()
	r := &rig{t: t, home: filepath.Join(t.TempDir(), "home"), pool: &poolfake.Pool{}, homeID: mint(t, ids.Home), workspaceID: mint(t, ids.Workspace)}
	if err := os.Mkdir(r.home, 0o700); err != nil {
		t.Fatal(err)
	}
	r.exe = entry.ExecutableWith(r.pool)
	stamp := "2026-10-11T08:00:00.000Z"
	writer := map[string]any{"package": "dispatch", "command": "hive-fleet", "operation": "provision", "implementation_version": "test"}
	r.write("home.json", map[string]any{
		"schema": "hive.home", "schema_version": "1.0", "record_revision": int64(1),
		"home_id": r.homeID, "role": "hive", "name": "A test home", "parent": map[string]any{"kind": "none"},
		"created_at": stamp, "updated_at": stamp, "writer": writer,
	})
	r.write("dispatch/registry/workspaces/"+r.workspaceID+".json", map[string]any{
		"schema": "hive.workspace", "schema_version": "1.0", "record_revision": int64(1),
		"workspace_id": r.workspaceID, "slug": "one", "name": "One", "home_id": r.homeID, "parent_home_id": mint(t, ids.Home), "state": "active",
		"created_at": stamp, "updated_at": stamp, "writer": writer,
	})
	return r
}

func (r *rig) write(rel string, tree map[string]any) {
	r.t.Helper()
	out, err := canon.Encode(tree)
	if err != nil {
		r.t.Fatal(err)
	}
	path := filepath.Join(r.home, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// Callers, as a request names them.
func (r *rig) supervisor() map[string]any {
	return map[string]any{"kind": "supervisor", "session_id": mint(r.t, ids.Session), "session_generation": 1}
}
func (r *rig) operator() map[string]any { return map[string]any{"kind": "operator"} }
func (r *rig) worker() map[string]any {
	return map[string]any{"kind": "worker", "task_id": mint(r.t, ids.Task), "generation": 1}
}
func pkg(name string) map[string]any {
	return map[string]any{"kind": "package", "package": name, "command": "hive-" + name}
}

// body is a valid body of the operation, carrying the words.
func (r *rig) body(operation, question string) map[string]any {
	switch operation {
	case "hold":
		return map[string]any{
			"held_task_id": mint(r.t, ids.Task), "origin": map[string]any{"kind": "supervisor"},
			"text": words, "consequence": words,
			"options":        []any{map[string]any{"option_id": "yes", "label": "Yes", "consequence": words}},
			"recommendation": "yes", "round": map[string]any{"complete": true},
		}
	case "revise":
		return map[string]any{"question_id": question, "text": words, "consequence": words, "options": []any{}}
	case "close":
		return map[string]any{"question_id": question, "reason": words, "evidence": []any{
			map[string]any{"kind": "item-archived", "ref": map[string]any{"schema": "pool.item", "id": mint(r.t, ids.Task)}},
		}}
	}
	return map[string]any{"question_id": question, "note": words}
}

// request is the encoded request of the operation by the caller, with the id
// and the body given.
func (r *rig) request(operation, requestID string, caller, body map[string]any) []byte {
	r.t.Helper()
	m := map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": "hive-word", "operation": operation, "operation_version": "1.0",
		"request_id": requestID, "correlation_id": mint(r.t, ids.Correlation),
		"issued_at": "2026-10-11T08:00:00.123Z", "caller": caller,
		"scope_level": "workspace", "home_id": r.homeID, "workspace_id": r.workspaceID,
		"authority": map[string]any{"kind": "none"}, "body": body,
	}
	if operation != "hold" {
		m["expected_revisions"] = []any{map[string]any{"schema": "pool.question", "id": body["question_id"], "record_revision": 3}}
	}
	out, err := canon.Encode(m)
	if err != nil {
		r.t.Fatal(err)
	}
	return append(out, '\n')
}

// send runs the executable as hive-word on the request and accepts the
// response by the caller's rule.
func (r *rig) send(operation string, req []byte) (*envelope.Response, int) {
	r.t.Helper()
	var stdout, stderr bytes.Buffer
	exit := r.exe.Main([]string{"/opt/hive/bin/hive-word", operation, "--home", r.home}, bytes.NewReader(req), &stdout, &stderr)
	id, _ := envelope.LooseRequestID(req)
	resp, err := envelope.AcceptResponse(stdout.Bytes(), exit, id)
	if err != nil {
		r.t.Fatalf("%s: the response is not accepted: %v\nexit %d\n%s", operation, err, exit, stdout.String())
	}
	return resp, exit
}

// same reports whether two trees are the same document.
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
	return bytes.Equal(x, y)
}

// decoded is the request as the Pool's caller reads it.
func decoded(t testing.TB, req []byte) *envelope.Request {
	t.Helper()
	d, err := envelope.DecodeRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Each operation sends exactly one handoff to the matching hive-pool
// operation, with the request's own request_id, correlation, scope and
// caller, and as the causation the request itself by identity and digest, and
// the question's words exactly; it returns the Pool's body and receipts.
func TestEachOperationSendsExactlyOneHandoff(t *testing.T) {
	for _, operation := range operations {
		t.Run(operation, func(t *testing.T) {
			r := newRig(t)
			caller := r.supervisor()
			req := r.request(operation, mint(t, ids.Request), caller, r.body(operation, mint(t, ids.Question)))
			resp, exit := r.send(operation, req)
			if exit != 0 || resp.Status != envelope.Ok || resp.Replayed {
				t.Fatalf("exit %d, %s, replayed %v, error %+v", exit, resp.Status, resp.Replayed, resp.Error)
			}
			if r.pool.Count() != 1 {
				t.Fatalf("%d handoffs were sent", r.pool.Count())
			}
			own, sent := decoded(t, req), r.pool.Sent[0]
			digest, err := envelope.Digest(req)
			if err != nil {
				t.Fatal(err)
			}
			if sent.Command != "hive-pool" || sent.Operation != poolOperation[operation] {
				t.Errorf("handed to %s %s", sent.Command, sent.Operation)
			}
			if sent.RequestID != own.RequestID || sent.CorrelationID != own.CorrelationID || sent.Scope != own.Scope {
				t.Errorf("identity %s %s %+v, want %s %s %+v", sent.RequestID, sent.CorrelationID, sent.Scope, own.RequestID, own.CorrelationID, own.Scope)
			}
			if sent.Caller != (envelope.Caller{Kind: envelope.CallerPackage, Package: "word", Command: "hive-word"}) {
				t.Errorf("caller %+v", sent.Caller)
			}
			if c := sent.Causation; c == nil || c.Schema != envelope.RequestSchema || c.ID != own.RequestID || c.Digest != digest {
				t.Errorf("causation %+v, want the request %s with digest %s", c, own.RequestID, digest)
			}
			if len(sent.ExpectedRevisions) != len(own.ExpectedRevisions) {
				t.Errorf("expected revisions %+v, want %+v", sent.ExpectedRevisions, own.ExpectedRevisions)
			}
			if operation != "hold" {
				if !same(t, sent.Body["requested_by"], own.Tree()["caller"]) {
					t.Errorf("requested_by %v", sent.Body["requested_by"])
				}
			}
			if len(resp.Receipts) != 1 || resp.Receipts[0].Kind == "" || resp.Body == nil {
				t.Errorf("the Pool's receipts and body were not returned: %d receipts", len(resp.Receipts))
			}
			// The words reach the Pool exactly.
			switch operation {
			case "hold":
				q := sent.Body["question"].(map[string]any)
				if q["text"] != words || q["consequence"] != words || q["asked_at"] != own.IssuedAt {
					t.Errorf("question %v", q)
				}
				for _, key := range []string{"question_id", "card_id"} {
					if resp.Body[key] == nil {
						t.Errorf("the response names no %s", key)
					}
				}
			case "revise":
				if sent.Body["text"] != words || sent.Body["consequence"] != words {
					t.Errorf("revision %v", sent.Body)
				}
			case "release", "complete":
				if sent.Body["note"] != words {
					t.Errorf("note %v", sent.Body["note"])
				}
			case "close":
				if sent.Body["reason"] != words {
					t.Errorf("reason %v", sent.Body["reason"])
				}
			}
		})
	}
}

// A retry returns the recorded outcome with replayed true, no second set of
// identities and nothing sent, though the records have changed since: it is
// not checked against them.
func TestARetryReturnsTheRecordedOutcome(t *testing.T) {
	for _, operation := range operations {
		t.Run(operation, func(t *testing.T) {
			r := newRig(t)
			req := r.request(operation, mint(t, ids.Request), r.supervisor(), r.body(operation, mint(t, ids.Question)))
			first, _ := r.send(operation, req)
			if first.Status != envelope.Ok {
				t.Fatalf("%s %+v", first.Status, first.Error)
			}
			r.pool.Refuse = func(*envelope.Request) *envelope.Failure {
				return envelope.Fail(envelope.Refused, "pool.question_closed", "the question changed")
			}
			again, exit := r.send(operation, req)
			if exit != 0 || again.Status != envelope.Ok || !again.Replayed {
				t.Fatalf("exit %d, %s, replayed %v, error %+v", exit, again.Status, again.Replayed, again.Error)
			}
			if r.pool.Count() != 1 {
				t.Errorf("the retry sent: %d handoffs", r.pool.Count())
			}
			if !same(t, first.Body, again.Body) {
				t.Errorf("the retry's body differs from the recorded one")
			}
			if len(again.Receipts) != 1 || again.Receipts[0].ReceiptID != first.Receipts[0].ReceiptID {
				t.Errorf("the retry's receipts are not the recorded ones")
			}
		})
	}
}

// A request_id used before with other words is a conflict, replay_mismatch,
// and nothing is sent.
func TestAReusedRequestIDWithOtherWordsIsRefused(t *testing.T) {
	for _, operation := range operations {
		t.Run(operation, func(t *testing.T) {
			r := newRig(t)
			id, question := mint(t, ids.Request), mint(t, ids.Question)
			caller := r.supervisor()
			first := r.body(operation, question)
			if resp, _ := r.send(operation, r.request(operation, id, caller, first)); resp.Status != envelope.Ok {
				t.Fatalf("%s %+v", resp.Status, resp.Error)
			}
			other := r.body(operation, question)
			for _, key := range []string{"text", "note", "reason"} {
				if _, ok := other[key]; ok {
					other[key] = words + " and more"
				}
			}
			if operation == "hold" {
				other["held_task_id"] = first["held_task_id"]
				other["text"] = words + " and more"
			}
			resp, exit := r.send(operation, r.request(operation, id, caller, other))
			if exit != 5 || resp.Status != envelope.Conflict || resp.Error == nil || resp.Error.Code != envelope.CodeReplayMismatch {
				t.Fatalf("exit %d, %s, %+v", exit, resp.Status, resp.Error)
			}
			if r.pool.Count() != 1 {
				t.Errorf("%d handoffs were sent", r.pool.Count())
			}
		})
	}
}

// A worker and the package ui are refused with word.not_authorized for every
// operation, and nothing is sent.
func TestAWorkerAndTheUIAreRefused(t *testing.T) {
	for _, operation := range operations {
		r := newRig(t)
		for name, caller := range map[string]map[string]any{"worker": r.worker(), "ui": pkg("ui")} {
			t.Run(operation+" "+name, func(t *testing.T) {
				req := r.request(operation, mint(t, ids.Request), caller, r.body(operation, mint(t, ids.Question)))
				resp, exit := r.send(operation, req)
				if exit != 4 || resp.Status != envelope.Refused || resp.Error == nil || resp.Error.Code != "word.not_authorized" {
					t.Fatalf("exit %d, %s, %+v", exit, resp.Status, resp.Error)
				}
				if r.pool.Count() != 0 || len(r.pool.Reads) != 0 {
					t.Errorf("%d handoffs and %d reads were made", r.pool.Count(), len(r.pool.Reads))
				}
			})
		}
	}
}
