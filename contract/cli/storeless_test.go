package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// The acceptance of spec/commands.md, A command with no store: a mutating
// request of a command that keeps no store leaves no store, no lock and no
// requests directory; it reads the callee's recorded outcome of the request
// first and replays it before it mints an identity or checks the operation;
// otherwise it sends the callee one handoff, which the callee keys on the
// digest of the original request.

// wordy is a command with no store, as word is: hive-word, whose operations
// hand off to hive-fleet, the command of the callee in the harness.
type wordy struct {
	exe *Executable
	// runs counts the handler runs of each operation, and minted the
	// identities its handlers made anew.
	runs   map[string]int
	minted int
}

// newWordy makes the command with no store over the callee adapter.
func newWordy(t testing.TB, h *harness, adapter Callee) *wordy {
	t.Helper()
	w := &wordy{runs: map[string]int{}}
	decl := func(name string) envelope.OperationDecl {
		return envelope.OperationDecl{Name: name, State: envelope.Supported, Mode: envelope.Unary, Mutates: true, Versions: []string{"1.0"},
			RequestSchema: testRequestSchema, ResponseSchema: testResponseSchema, ReceiptKinds: []string{}, Executes: []string{}, Calls: []string{"hive-fleet"}}
	}
	handoff := func(c *Call, command, operation, name string) (*envelope.Response, error) {
		return c.SendHandoff(Handoff{Command: command, Operation: operation, OperationVersion: "1.0",
			Body: map[string]any{"home": map[string]any{"role": "hive", "name": name}}})
	}
	say := func(c *Call) (*Result, error) {
		w.runs[c.Operation.Name]++
		name, _ := c.Request.Body["home"].(map[string]any)["name"].(string)
		// An identity made anew by each run, as a handler that mints one is.
		w.minted++
		operation, callee := "put", fmt.Sprintf("said-%d", w.minted)
		if strings.HasPrefix(name, "refuse") {
			operation, callee = "refuse", "record it"
		}
		command := "hive-fleet"
		switch c.Operation.Name {
		case "stray":
			command = "hive-other"
		case "twice":
			if _, err := handoff(c, command, operation, callee); err != nil {
				return nil, err
			}
		}
		resp, err := handoff(c, command, operation, callee)
		if err != nil {
			return nil, err
		}
		body, err := c.Operation.Adapt(c, resp)
		if err != nil {
			return nil, err
		}
		return &Result{Body: body}, nil
	}
	adapt := func(c *Call, callee *envelope.Response) (map[string]any, error) {
		return map[string]any{"adapted": true, "callee": callee.Body}, nil
	}
	op := func(name string) *Operation { return &Operation{OperationDecl: decl(name), Handle: say, Adapt: adapt} }
	w.exe = &Executable{
		Package: "word", Name: "hive-word", Version: "0.1.0-test", Storeless: true, Clock: h.clk, Callee: adapter,
		Commands: []*Command{{Name: "hive-word", Operations: []*Operation{op("say"), op("twice"), op("stray")}}},
	}
	if err := w.exe.Check(); err != nil {
		t.Fatal(err)
	}
	return w
}

// sayRequest is the request of an operation of hive-word with the name given
// in its body.
func sayRequest(t testing.TB, operation string, n int, name string) []byte {
	t.Helper()
	return request(t, operation, n, func(m map[string]any) {
		m["command"] = "hive-word"
		m["body"] = map[string]any{"home": map[string]any{"role": "hive", "name": name}}
	})
}

// send runs one invocation through Main and accepts the response by the
// caller's rule.
func (w *wordy) send(t testing.TB, home, operation string, req []byte) *envelope.Response {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := w.exe.Main([]string{"/opt/hive/bin/hive-word", operation, HomeOption, home}, bytes.NewReader(req), &stdout, &stderr)
	want, _ := envelope.LooseRequestID(req)
	resp, err := envelope.AcceptResponse(stdout.Bytes(), code, want)
	if err != nil {
		t.Fatalf("the response is not accepted: %v\nexit %d\n%s", err, code, stdout.String())
	}
	return resp
}

// A mutating request of a command with no store writes no store, no lock and
// no requests directory, and sends the callee exactly one handoff.
func TestAStorelessRequestLeavesNoStoreOfItsOwn(t *testing.T) {
	h := newHarness(t)
	adapter := &scripted{}
	w := newWordy(t, h, adapter)
	home := newHome(t)
	before := layout(t, home)

	resp := w.send(t, home, "say", sayRequest(t, "say", 1, "first"))
	expect(t, resp, envelope.Ok, "")
	if resp.Replayed || resp.Responder.Package != "word" || resp.Command != "hive-word" || resp.Operation != "say" {
		t.Errorf("%+v", resp)
	}
	if resp.Body["adapted"] != true {
		t.Errorf("the body is not the operation's own: %v", resp.Body)
	}
	if adapter.reads != 1 || len(adapter.sent) != 1 || w.runs["say"] != 1 {
		t.Errorf("reads %d, handoffs %d, runs %d; want one of each", adapter.reads, len(adapter.sent), w.runs["say"])
	}
	same(t, before, layout(t, home))
	absent(t, filepath.Join(home, "word"))
}

// The handoff is the same request_id, correlation_id and scope, from the
// package and its command, and its causation is the original request by
// identity and digest. A second handoff of one request is refused by the
// harness, whatever the operation does after the first.
func TestTheHandoffCarriesTheOriginalDigest(t *testing.T) {
	h := newHarness(t)
	adapter := &scripted{}
	w := newWordy(t, h, adapter)
	home := newHome(t)
	original := sayRequest(t, "say", 1, "first")
	want, err := envelope.Digest(original)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, w.send(t, home, "say", original), envelope.Ok, "")
	if len(adapter.sent) != 1 {
		t.Fatalf("%d handoffs", len(adapter.sent))
	}
	got, err := envelope.DecodeRequest(adapter.sent[0])
	if err != nil {
		t.Fatal(err)
	}
	orig, _ := envelope.DecodeRequest(original)
	if got.RequestID != orig.RequestID || got.CorrelationID != orig.CorrelationID || got.Scope != orig.Scope {
		t.Errorf("identity or scope differs: %+v", got)
	}
	if got.Caller.Kind != envelope.CallerPackage || got.Caller.Package != "word" || got.Caller.Command != "hive-word" {
		t.Errorf("caller %+v", got.Caller)
	}
	if got.Command != "hive-fleet" || got.Operation != "put" {
		t.Errorf("addressed to %s %s", got.Command, got.Operation)
	}
	c := got.Causation
	if c == nil || c.Schema != "hive.request" || c.ID != orig.RequestID || c.Digest != want {
		t.Errorf("causation %+v, want the original %s at %s", c, orig.RequestID, want)
	}

	// The second handoff of one request is not sent; the first one's answer
	// is durable, so the answer is unknown and the retry replays it.
	adapter.sent = nil
	resp := w.send(t, home, "twice", sayRequest(t, "twice", 2, "second"))
	expect(t, resp, envelope.Unknown, "outcome_unknown")
	if len(adapter.sent) != 1 {
		t.Errorf("%d handoffs of one request", len(adapter.sent))
	}
	// A handoff to a command the operation does not declare it calls is not
	// sent either.
	adapter.sent = nil
	expect(t, w.send(t, home, "stray", sayRequest(t, "stray", 3, "third")), envelope.Failed, "internal")
	if len(adapter.sent) != 0 {
		t.Errorf("%d handoffs to a command not declared", len(adapter.sent))
	}
}

// The recorded outcome of the callee is replayed before the handler mints an
// identity or checks the operation, with the callee's receipts and status.
func TestARecordedOutcomeIsReplayedBeforeAnythingElse(t *testing.T) {
	h := newHarness(t)
	h.exe.Handoffs = []string{"word"}
	adapter := &inProcess{exe: h.exe}
	w := newWordy(t, h, adapter)
	home := newHome(t)

	for i, tc := range []struct {
		name   string
		status envelope.Status
		code   string
	}{{"first", envelope.Ok, ""}, {"refuse it", envelope.Refused, "authority_missing"}} {
		n := i + 1
		req := sayRequest(t, "say", n, tc.name)
		first := w.send(t, home, "say", req)
		expect(t, first, tc.status, tc.code)
		if first.Replayed || len(first.Receipts) != 1 || first.Receipts[0].Writer.Package != "dispatch" {
			t.Fatalf("the callee's receipt is not carried in full: %+v", first)
		}
		runs, minted, sent, reads := w.runs["say"], w.minted, len(adapter.sent), len(adapter.reads)

		again := w.send(t, home, "say", req)
		expect(t, again, tc.status, tc.code)
		if !again.Replayed || again.Command != "hive-word" || again.Operation != "say" || again.Responder.Package != "word" {
			t.Errorf("%s: %+v", tc.name, again)
		}
		if len(again.Receipts) != 1 || again.Receipts[0].ReceiptID != first.Receipts[0].ReceiptID {
			t.Errorf("%s: receipts %+v, want %+v", tc.name, again.Receipts, first.Receipts)
		}
		if tc.status == envelope.Ok && fmt.Sprint(again.Body) != fmt.Sprint(first.Body) {
			t.Errorf("%s: body %v, was %v", tc.name, again.Body, first.Body)
		}
		if w.runs["say"] != runs || w.minted != minted {
			t.Errorf("%s: the replay ran the handler (%d runs, %d identities)", tc.name, w.runs["say"]-runs, w.minted-minted)
		}
		if len(adapter.sent) != sent || len(adapter.reads) != reads+1 {
			t.Errorf("%s: %d handoffs after the replay, %d reads", tc.name, len(adapter.sent)-sent, len(adapter.reads)-reads)
		}
	}
	absent(t, filepath.Join(home, "word"))
}

// The same request_id with another request is replay_mismatch, and nothing is
// sent.
func TestAChangedRequestIsAReplayMismatch(t *testing.T) {
	h := newHarness(t)
	h.exe.Handoffs = []string{"word"}
	adapter := &inProcess{exe: h.exe}
	w := newWordy(t, h, adapter)
	home := newHome(t)
	expect(t, w.send(t, home, "say", sayRequest(t, "say", 1, "first")), envelope.Ok, "")
	runs, sent := w.runs["say"], len(adapter.sent)

	resp := w.send(t, home, "say", sayRequest(t, "say", 1, "changed"))
	expect(t, resp, envelope.Conflict, "replay_mismatch")
	if len(resp.Receipts) != 0 || resp.Replayed || w.runs["say"] != runs || len(adapter.sent) != sent {
		t.Errorf("%+v, %d runs, %d handoffs", resp, w.runs["say"]-runs, len(adapter.sent)-sent)
	}
}

// Two identical requests that both pass the read are answered by whichever
// reached the callee first, though the identities the second minted anew make
// its handoff's body differ; the same request_id with another body is the
// callee's replay_mismatch.
func TestRacingRequestsAreAnsweredByTheFirstToReachTheCallee(t *testing.T) {
	h := newHarness(t)
	h.exe.Handoffs = []string{"word"}
	adapter := &inProcess{exe: h.exe}
	w := newWordy(t, h, adapter)
	home := newHome(t)
	req := sayRequest(t, "say", 1, "first")
	first := w.send(t, home, "say", req)
	expect(t, first, envelope.Ok, "")

	adapter.hide = true
	raced := w.send(t, home, "say", req)
	expect(t, raced, envelope.Ok, "")
	if w.runs["say"] != 2 || len(adapter.sent) != 2 {
		t.Fatalf("the racer did not reach the callee: %d runs, %d handoffs", w.runs["say"], len(adapter.sent))
	}
	if !raced.Replayed || fmt.Sprint(raced.Body) != fmt.Sprint(first.Body) || len(raced.Receipts) != 1 || raced.Receipts[0].ReceiptID != first.Receipts[0].ReceiptID {
		t.Errorf("the racer was not answered with the first's outcome: %+v, first %+v", raced, first)
	}
	absent(t, filepath.Join(home, "dispatch", "records", "thing", "said-2.json"))

	changed := w.send(t, home, "say", sayRequest(t, "say", 1, "changed"))
	expect(t, changed, envelope.Conflict, "replay_mismatch")
	if len(adapter.sent) != 3 {
		t.Errorf("%d handoffs", len(adapter.sent))
	}
	absent(t, filepath.Join(home, "word"))
}

// The callee keys a request on the digest of the causation only when it is a
// handoff: its caller a package that declares no home path, and its causation
// a hive.request of its own request_id with a digest.
func TestACalleeKeysOnTheOriginalOnlyForAHandoff(t *testing.T) {
	h := newHarness(t)
	h.exe.Handoffs = []string{"word"}
	home := newHome(t)
	const original = "sha256:00000000000000000000000000000000000000000000000000000000000000aa"
	cases := []struct {
		name     string
		edit     func(m map[string]any)
		original bool
	}{
		{"a handoff", func(m map[string]any) {}, true},
		{"another request's causation", func(m map[string]any) { m["causation"].(map[string]any)["id"] = id(ids.Request, 99) }, false},
		{"a causation without a digest", func(m map[string]any) { delete(m["causation"].(map[string]any), "digest") }, false},
		{"a causation of another schema", func(m map[string]any) { m["causation"].(map[string]any)["schema"] = "word.question" }, false},
		{"a caller that is not a package", func(m map[string]any) { m["caller"] = map[string]any{"kind": "test"} }, false},
		{"a package with a home path", func(m map[string]any) { m["caller"].(map[string]any)["package"] = "dispatch" }, false},
	}
	for i, tc := range cases {
		n := i + 1
		req := request(t, "put", n, func(m map[string]any) {
			m["caller"] = map[string]any{"kind": "package", "package": "word", "command": "hive-word"}
			m["causation"] = map[string]any{"schema": "hive.request", "id": id(ids.Request, n), "digest": original}
			tc.edit(m)
		})
		expect(t, h.invoke(t, home, "put", req), envelope.Ok, "")
		outcome, err := os.ReadFile(filepath.Join(home, "dispatch", "requests", id(ids.Request, n)+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(string(outcome), `"request_digest":"`+original+`"`); got != tc.original {
			t.Errorf("%s: recorded the original's digest: %v", tc.name, got)
		}
	}

	// A handoff repeated: the same causation digest is the recorded outcome
	// though the body differs, and another digest is replay_mismatch.
	handoff := func(n int, digest, name string) *envelope.Response {
		return h.invoke(t, home, "put", request(t, "put", n, func(m map[string]any) {
			m["caller"] = map[string]any{"kind": "package", "package": "word", "command": "hive-word"}
			m["causation"] = map[string]any{"schema": "hive.request", "id": id(ids.Request, n), "digest": digest}
			m["body"] = map[string]any{"home": map[string]any{"role": "hive", "name": name}}
		}))
	}
	again := handoff(1, original, "another body")
	expect(t, again, envelope.Ok, "")
	if !again.Replayed || again.Body["wrote"] == "another body" {
		t.Errorf("%+v", again)
	}
	expect(t, handoff(1, strings.TrimSuffix(original, "aa")+"bb", "another body"), envelope.Conflict, "replay_mismatch")
}

// A callee that gives no response, an unusable one or an unknown one leaves
// the request unknown with nothing recorded, and the retry reads first again
// and sends again. The callee's status and receipts are otherwise preserved.
func TestACalleeThatDoesNotAnswerLeavesTheRequestUnknown(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	before := layout(t, home)
	receipt := func(r *envelope.Response, req *envelope.Request) {
		rc := envelope.Receipt{ReceiptID: id(ids.Receipt, 1), Kind: "dispatch.thing.intended", KindVersion: "1.0", WrittenAt: "2026-10-11T08:00:00.000Z",
			Writer:    envelope.Writer{Package: "dispatch", Command: "hive-fleet", Operation: "put", ImplementationVersion: "0.1.0-test"},
			RequestID: req.RequestID, CorrelationID: req.CorrelationID, Scope: req.Scope, Authority: req.Authority,
			Evidence: []envelope.Evidence{}, Body: map[string]any{}}
		r.Receipts = []envelope.Receipt{rc}
	}
	cases := []struct {
		name     string
		reply    func(request []byte) ([]byte, int, error)
		receipts int
	}{
		{"no response", func([]byte) ([]byte, int, error) { return nil, 0, errors.New("the callee did not start") }, 0},
		{"empty output", func([]byte) ([]byte, int, error) { return nil, 0, nil }, 0},
		{"output that is not a response", func([]byte) ([]byte, int, error) { return []byte("{}\n"), 0, nil }, 0},
		{"a status that disagrees with the exit code", func(r []byte) ([]byte, int, error) {
			out, _, err := calleeAnswer(r, func(*envelope.Response, *envelope.Request) {})
			return out, 4, err
		}, 0},
		{"a response to another request", func(r []byte) ([]byte, int, error) {
			return calleeAnswer(r, func(resp *envelope.Response, _ *envelope.Request) {
				resp.RequestID = id(ids.Request, 77)
				resp.Body = map[string]any{}
			})
		}, 0},
		{"an unknown outcome with the receipts of its intent", func(r []byte) ([]byte, int, error) {
			return calleeAnswer(r, func(resp *envelope.Response, req *envelope.Request) {
				receipt(resp, req)
				f := envelope.Common(envelope.CodeOutcomeUnknown, "the adapter did not answer")
				resp.Status, resp.Body, resp.Error = f.Status, nil, &f.Err
			})
		}, 1},
	}
	for i, tc := range cases {
		adapter := &scripted{reply: tc.reply}
		w := newWordy(t, h, adapter)
		req := sayRequest(t, "say", i+1, "first")
		for try := 1; try <= 2; try++ {
			resp := w.send(t, home, "say", req)
			expect(t, resp, envelope.Unknown, "outcome_unknown")
			if len(resp.Receipts) != tc.receipts || resp.Replayed || resp.Error.Retryable {
				t.Errorf("%s: %+v", tc.name, resp)
			}
			if adapter.reads != try || len(adapter.sent) != try {
				t.Errorf("%s: %d reads and %d handoffs after %d tries", tc.name, adapter.reads, len(adapter.sent), try)
			}
		}
		same(t, before, layout(t, home))
	}
}

// The read is after the request, the home, the scope and the operation are
// checked: a request refused there reads nothing of the callee and runs
// nothing.
func TestTheCalleeIsReadOnlyAfterTheRequestIsChecked(t *testing.T) {
	h := newHarness(t)
	adapter := &scripted{}
	w := newWordy(t, h, adapter)
	home := newHome(t)
	otherHome := id(ids.Home, 5)
	cases := []struct {
		name string
		req  []byte
		op   string
		want envelope.Status
		code string
	}{
		{"a request that breaks its schema", request(t, "say", 1, func(m map[string]any) { m["command"] = "hive-word"; m["body"] = map[string]any{"home": 1} }), "say", envelope.Invalid, "schema_violation"},
		{"a version the operation does not accept", request(t, "say", 2, func(m map[string]any) { m["command"] = "hive-word"; m["operation_version"] = "2.0" }), "say", envelope.Invalid, "unsupported_version"},
		{"another home", request(t, "say", 3, func(m map[string]any) { m["command"] = "hive-word"; m["home_id"] = otherHome }), "say", envelope.Refused, "home_mismatch"},
		{"a scope the registry does not hold", request(t, "say", 4, func(m map[string]any) {
			m["command"] = "hive-word"
			m["scope_level"], m["workspace_id"] = "workspace", id(ids.Workspace, 9)
		}), "say", envelope.Refused, "scope_mismatch"},
	}
	for _, tc := range cases {
		expect(t, w.send(t, home, tc.op, tc.req), tc.want, tc.code)
	}
	if adapter.reads != 0 || len(adapter.sent) != 0 || w.runs["say"] != 0 {
		t.Errorf("%d reads, %d handoffs, %d runs of requests that were refused", adapter.reads, len(adapter.sent), w.runs["say"])
	}
}

// fixtureOutcome reads a recorded outcome fixture of the request-outcome
// schema for the request and digest given.
func fixtureOutcome(t testing.TB, name, requestID, digest string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "schemas", "request-outcome", "valid", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte("req_01j9xk3m2nq4r5s6t7v8w9xyzf"), []byte(requestID))
	data = bytes.ReplaceAll(data, []byte("sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"), []byte(digest))
	return data
}

// The recorded outcome is the record of the existing schema: read as the
// store's own is, an outcome of the recorded kinds is replayed as the
// command's, and one that cannot be read is never replayed and nothing is
// sent.
func TestARecordedOutcomeOfTheExistingSchema(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	req := sayRequest(t, "say", 1, "first")
	digest, err := envelope.Digest(req)
	if err != nil {
		t.Fatal(err)
	}
	requestID := id(ids.Request, 1)
	replays := []struct {
		fixture string
		status  envelope.Status
		code    string
		body    bool
		hold    bool
		receipt int
	}{{"ok", envelope.Ok, "", true, false, 0}, {"held", envelope.Held, "held", false, true, 0}, {"refused-with-receipt", envelope.Refused, "authority_missing", false, false, 1}}
	for _, tc := range replays {
		adapter := &scripted{outcome: fixtureOutcome(t, tc.fixture, requestID, digest), found: true}
		w := newWordy(t, h, adapter)
		resp := w.send(t, home, "say", req)
		expect(t, resp, tc.status, tc.code)
		if !resp.Replayed || resp.Command != "hive-word" || resp.Operation != "say" || resp.Responder.Package != "word" || len(resp.Receipts) != tc.receipt {
			t.Errorf("%s: %+v", tc.fixture, resp)
		}
		if tc.body && resp.Body["adapted"] != true {
			t.Errorf("%s: the body is not the operation's own: %v", tc.fixture, resp.Body)
		}
		if tc.hold && (resp.Error.Hold == nil || resp.Error.Hold.Schema != "word.question") {
			t.Errorf("%s: the hold is lost: %+v", tc.fixture, resp.Error)
		}
		if len(adapter.sent) != 0 || w.runs["say"] != 0 {
			t.Errorf("%s: %d handoffs and %d runs after a replay", tc.fixture, len(adapter.sent), w.runs["say"])
		}
	}

	good := string(fixtureOutcome(t, "ok", requestID, digest))
	unreadable := []struct {
		name    string
		outcome string
		code    string
	}{
		{"a record that breaks its schema", strings.Replace(good, `"request_digest"`, `"digest"`, 1), "record_malformed"},
		{"another request's record", strings.ReplaceAll(good, requestID, id(ids.Request, 2)), "record_malformed"},
		{"a newer version", strings.Replace(good, `"schema_version": "1.0"`, `"schema_version": "2.0"`, 1), "unsupported_version"},
		{"bytes that are not a record", "not a record", "record_malformed"},
	}
	for _, tc := range unreadable {
		adapter := &scripted{outcome: []byte(tc.outcome), found: true}
		w := newWordy(t, h, adapter)
		resp := w.send(t, home, "say", req)
		if resp.Status != envelope.Unavailable && resp.Status != envelope.Invalid {
			t.Errorf("%s: %+v", tc.name, resp)
		}
		if resp.Error == nil || resp.Error.Code != tc.code || resp.Replayed || len(adapter.sent) != 0 || w.runs["say"] != 0 {
			t.Errorf("%s: %+v, %d handoffs", tc.name, resp, len(adapter.sent))
		}
	}

	// A different digest in a good record is the mismatch, and nothing is sent.
	other := &scripted{outcome: fixtureOutcome(t, "ok", requestID, "sha256:"+strings.Repeat("ab", 32)), found: true}
	w := newWordy(t, h, other)
	expect(t, w.send(t, home, "say", req), envelope.Conflict, "replay_mismatch")
	if len(other.sent) != 0 {
		t.Errorf("%d handoffs after a mismatch", len(other.sent))
	}

	// A read that fails: a failure of the contract's is the answer, any other
	// error is the harness's own, and nothing is sent.
	for _, tc := range []struct {
		err    error
		status envelope.Status
		code   string
	}{
		{envelope.Common(envelope.CodeDependencyUnusable, "the callee cannot be read"), envelope.Unavailable, "dependency_unusable"},
		{errors.New("something of its own"), envelope.Failed, "internal"},
	} {
		adapter := &scripted{readErr: tc.err}
		w := newWordy(t, h, adapter)
		expect(t, w.send(t, home, "say", req), tc.status, tc.code)
		if len(adapter.sent) != 0 || w.runs["say"] != 0 {
			t.Errorf("%v: %d handoffs, %d runs", tc.err, len(adapter.sent), w.runs["say"])
		}
	}
}

// Check admits a supported mutating operation of a command with no store only
// when the executable has the callee it hands off to, the operation says how
// to adapt a recorded outcome, and it declares the command it calls.
func TestAStorelessMutatingOperationNeedsItsCallee(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		name   string
		change func(e *Executable)
		want   string
	}{
		{"no callee", func(e *Executable) { e.Callee = nil }, "callee"},
		{"no adaptation", func(e *Executable) { e.Commands[0].Operations[0].Adapt = nil }, "adapt"},
		{"no call declared", func(e *Executable) { e.Commands[0].Operations[0].Calls = []string{} }, "calls"},
	} {
		e := newWordy(t, h, &scripted{}).exe
		tc.change(e)
		if err := e.Check(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}

	// A command with a store hands nothing off.
	e := newWordy(t, h, &scripted{}).exe
	e.Storeless = false
	if err := e.Check(); err == nil || !strings.Contains(err.Error(), "handoff") {
		t.Errorf("a command with a store: %v", err)
	}
}
