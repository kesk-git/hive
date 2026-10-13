package calls

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// The transport of the command contract (spec/commands.md), proved end to
// end against the test-only callee of integration/testcmd/ until a real
// operation exists: the request and response envelopes, each exit state, replay
// and the size limits. Every expected value below is read from the tables of
// spec/commands.md, not from the code under test.

// fromDispatch is the caller of the transport tests: a package's command.
var fromDispatch = asPackage{"dispatch", "hive-dispatch"}

// isOneDocument says whether the raw output of a call is what the contract
// fixes a response to be: one document followed by one line feed.
func isOneDocument(raw []byte) bool {
	return len(raw) > 0 && raw[len(raw)-1] == '\n' && bytes.Count(raw, []byte("\n")) == 1
}

// TestTransportEnvelopes proves the request and response envelopes on a call
// that succeeds: the response is one complete document that echoes the
// request, names its responder, carries the body and nothing else, and the
// request is closed, addressed and versioned.
//
// It catches a response that is not one document, that does not echo the
// request's identity, command or operation, that carries an error beside a
// body, and a callee that ignores a field it does not know, a request
// addressed to another operation or a body version it does not accept.
func TestTransportEnvelopes(t *testing.T) {
	home := newHome(t)
	before := snapshot(t, home)

	t.Run("response", func(t *testing.T) {
		req := newRequest(fromDispatch, calleeExe, "echo", 1, map[string]any{"record": "hello"})
		r := call{program: calleeExe, operation: "echo", home: home, req: req}.run(t)
		resp := r.must(t)
		if !isOneDocument(r.raw) {
			t.Errorf("the response is not one document followed by a line feed: %d bytes", len(r.raw))
		}
		if r.result.Code != 0 || string(resp.Status) != "ok" {
			t.Fatalf("exit code %d, status %q; want 0, ok", r.result.Code, resp.Status)
		}
		if resp.RequestID != req.id || resp.Command != calleeExe || resp.Operation != "echo" {
			t.Errorf("response echoes %q %s %s; want %q %s echo", resp.RequestID, resp.Command, resp.Operation, req.id, calleeExe)
		}
		want := envelope.Responder{Package: "shadow", Command: calleeExe, ImplementationVersion: "0.0.0-test", ContractVersion: resp.Responder.ContractVersion}
		if resp.Responder != want || resp.Responder.ContractVersion == "" {
			t.Errorf("responder = %+v, want the callee's package, command and version with a contract version", resp.Responder)
		}
		if resp.CompletedAt == "" {
			t.Error("completed_at is empty")
		}
		if resp.Replayed {
			t.Error("a first request is not a replay")
		}
		if len(resp.Receipts) != 0 {
			t.Errorf("a read-only operation returned %d receipts", len(resp.Receipts))
		}
		if resp.Error != nil || resp.Body["record"] != "hello" || len(resp.Body) != 1 {
			t.Errorf("body = %v, error = %+v; want the record alone", resp.Body, resp.Error)
		}
		sameSnapshot(t, "a read-only call", before, snapshot(t, home))
	})

	t.Run("capabilities", func(t *testing.T) {
		for _, name := range []string{calleeExe, calleeSmallName} {
			r := start(t, name, []string{"capabilities"}, input{})
			resp, err := envelope.AcceptCapabilities(r.stdout, int(r.result.Code))
			if err != nil {
				t.Fatalf("%s capabilities: %v", name, err)
			}
			if r.result.Code != 0 || resp.RequestID != "" || resp.Operation != "capabilities" {
				t.Errorf("%s: exit %d, request_id %q, operation %q; want 0, null, capabilities", name, r.result.Code, resp.RequestID, resp.Operation)
			}
			caps, err := envelope.DecodeCapabilities(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			var ops []string
			for _, op := range caps.Operations {
				ops = append(ops, op.Name)
				if string(op.State) != "supported" {
					t.Errorf("%s %s is %s", name, op.Name, op.State)
				}
			}
			if got := strings.Join(ops, " "); got != "echo expand state put grow" {
				t.Errorf("%s declares %q", name, got)
			}
			limit := int64(67108864)
			if name == calleeSmallName {
				limit = smallLimit
			}
			if caps.Command != name || caps.Package != "shadow" || caps.Limits.MaxRequestBytes != limit || caps.Limits.MaxResponseBytes != limit || caps.Limits.MaxArgumentBytes != 4096 {
				t.Errorf("%s declares %+v with limits %+v; want request and response limits of %d and an argument limit of 4096", name, caps.Command, caps.Limits, limit)
			}
		}
	})

	t.Run("a field the request does not know", func(t *testing.T) {
		req := newRequest(fromDispatch, calleeExe, "echo", 2, map[string]any{"record": "x"})
		tree := parseObject(t, req.data)
		tree["unexpected"] = "member"
		req.data = encodeObject(t, tree)
		r := call{program: calleeExe, operation: "echo", home: home, req: req}.run(t)
		wantFailure(t, r, req.id, 3, "invalid", "schema_violation")
	})

	t.Run("another command or operation", func(t *testing.T) {
		for _, c := range []struct{ command, operation string }{{calleeSmallName, "echo"}, {calleeExe, "state"}} {
			req := newRequest(fromDispatch, c.command, c.operation, 3, map[string]any{"record": "x"})
			r := call{program: calleeExe, operation: "echo", home: home, req: req}.run(t)
			wantFailure(t, r, req.id, 3, "invalid", "envelope_mismatch")
		}
	})

	t.Run("a body version the operation does not accept", func(t *testing.T) {
		req := newRequest(fromDispatch, calleeExe, "echo", 4, map[string]any{"record": "x"})
		tree := parseObject(t, req.data)
		tree["operation_version"] = "9.0"
		req.data = encodeObject(t, tree)
		r := call{program: calleeExe, operation: "echo", home: home, req: req}.run(t)
		wantFailure(t, r, req.id, 3, "invalid", "unsupported_version")
	})

	sameSnapshot(t, "the refused calls", before, snapshot(t, home))
}

// exitStates are the eleven exit states of spec/commands.md, Exit and error
// states, with the code each ends with in this proof.
var exitStates = []struct {
	exit   int
	status string
	code   string
}{
	{0, "ok", ""},
	{1, "failed", "internal"},
	{2, "usage", "unknown_operation"},
	{3, "invalid", "malformed_request"},
	{4, "refused", "authority_missing"},
	{5, "conflict", "revision_conflict"},
	{6, "not_found", "not_found"},
	{7, "too_large", "record_too_large"},
	{8, "unavailable", "dependency_missing"},
	{9, "unknown", "outcome_unknown"},
	{10, "held", "held"},
}

// TestTransportEachExitState proves that each of the eleven exit states
// reaches the caller as a complete response whose status agrees with the
// process's exit code, with no body and no change to the home for a state
// that is not ok. Usage, invalid and too_large are reached the way a caller
// reaches them, by a wrong invocation, by bytes that are no request and by a
// request over the limit; the rest are the callee's own to give.
//
// It catches an exit code that does not agree with the response's status, a
// body or partial result beside an error, a missing field a status requires
// (the limit and actual size of too_large, the hold of held), an unknown
// outcome marked retryable, and a refusal that writes.
func TestTransportEachExitState(t *testing.T) {
	home := newHome(t)
	before := snapshot(t, home)
	for _, want := range exitStates {
		t.Run(want.status, func(t *testing.T) {
			var r reply
			id := ""
			switch want.status {
			case "usage":
				// An operation the command does not have, and no request is read.
				req := newRequest(fromDispatch, calleeExe, "no-such-operation", 10, map[string]any{})
				r = call{program: calleeExe, operation: "no-such-operation", home: home, req: req}.run(t)
			case "invalid":
				// Bytes that are no request: the identity cannot be read.
				req := request{id: ident(ids.Request, 11), data: []byte(`{"schema":"hive.request",`)}
				r = call{program: calleeExe, operation: "echo", home: home, req: req}.run(t)
			case "too_large":
				req := newRequest(fromDispatch, calleeSmallName, "echo", 12, map[string]any{"record": pattern(recordBytes)})
				r = call{program: calleeSmallName, operation: "echo", home: home, req: req}.run(t)
			default:
				// ok and the states the callee gives are asked of its state operation.
				req := newRequest(fromDispatch, calleeExe, "state", 13, map[string]any{"status": want.status})
				id = req.id
				r = call{program: calleeExe, operation: "state", home: home, req: req}.run(t)
			}
			resp := r.must(t)
			if r.result.Code != int64(want.exit) || string(resp.Status) != want.status {
				t.Fatalf("exit code %d, status %q; want %d, %s", r.result.Code, resp.Status, want.exit, want.status)
			}
			if !isOneDocument(r.raw) {
				t.Errorf("the response is not one document followed by a line feed")
			}
			if want.status == "ok" {
				if resp.Error != nil || resp.Body == nil {
					t.Errorf("ok carries error %+v and body %v", resp.Error, resp.Body)
				}
				return
			}
			if resp.Body != nil {
				t.Errorf("%s carries a body: %v", want.status, resp.Body)
			}
			e := resp.Error
			if e == nil || e.Code != want.code || e.Message == "" {
				t.Fatalf("error = %+v, want code %s with a message", e, want.code)
			}
			switch want.status {
			case "usage", "invalid", "too_large":
				// The callee could not read the request's identity.
				if want.status == "too_large" && resp.RequestID != "" {
					t.Errorf("a request over the limit is answered with request_id %q, want null", resp.RequestID)
				}
				if want.status != "too_large" && resp.RequestID != "" {
					t.Errorf("request_id = %q, want null", resp.RequestID)
				}
			default:
				if resp.RequestID != id {
					t.Errorf("request_id = %q, want %q", resp.RequestID, id)
				}
			}
			if want.status == "unknown" && e.Retryable {
				t.Error("an unknown outcome is marked retryable")
			}
			if want.status == "held" && (e.Hold == nil || e.Hold.ID != "hold-1") {
				t.Errorf("held names hold %+v, want hold-1", e.Hold)
			}
			if want.status == "too_large" && (e.LimitBytes == nil || e.ActualBytes == nil || *e.LimitBytes != smallLimit || *e.ActualBytes <= *e.LimitBytes) {
				t.Errorf("too_large limit_bytes %v actual_bytes %v; want %d and a larger size", e.LimitBytes, e.ActualBytes, smallLimit)
			}
		})
	}
	sameSnapshot(t, "the exit states", before, snapshot(t, home))
}

// TestTransportUsage proves the other ways an invocation itself is wrong: no
// home is named, and an operation name that no command has. Each is exit 2,
// usage, with nothing read and nothing written.
//
// It catches a command that falls back to a default home, which the contract
// forbids, or that reads a request for an invocation it already refuses.
func TestTransportUsage(t *testing.T) {
	home := newHome(t)
	before := snapshot(t, home)
	req := newRequest(fromDispatch, calleeExe, "echo", 20, map[string]any{"record": "x"})
	r := call{program: calleeExe, operation: "echo", req: req}.run(t) // no --home
	if r.failure != nil || r.result.Code != 2 || string(r.resp.Status) != "usage" || r.resp.Error.Code != "usage" {
		t.Errorf("no home: exit %v, response %+v, failure %v; want exit 2, usage", r.result, r.resp, r.failure)
	}
	sameSnapshot(t, "an invocation without a home", before, snapshot(t, home))
}

// TestTransportReplay proves replay (spec/commands.md, Replay) of a mutating
// operation: the same request_id with the same request returns the recorded
// response with replayed true and writes nothing; the same request_id with
// another request is a conflict, replay_mismatch; and another request_id for
// a record that exists loses to the state (already_exists), with nothing written.
//
// It catches a replay that runs the operation twice, one that writes again,
// one that answers another body or other receipts than the first, a reuse of
// a request_id that is not refused, and a refusal that changes the home.
func TestTransportReplay(t *testing.T) {
	home := newHome(t)
	empty := snapshot(t, home)
	first := newRequest(fromDispatch, calleeExe, "put", 30, map[string]any{"name": "alpha", "record": "first"})

	r1 := call{program: calleeExe, operation: "put", home: home, req: first}.run(t)
	resp1 := r1.must(t)
	if r1.result.Code != 0 || resp1.Replayed || len(resp1.Receipts) != 1 || resp1.Receipts[0].Kind != "shadow.callee.put" {
		t.Fatalf("first put: exit %d, replayed %v, receipts %d; want 0, false, one shadow.callee.put", r1.result.Code, resp1.Replayed, len(resp1.Receipts))
	}
	written := snapshot(t, home)
	if len(written) <= len(empty) {
		t.Fatal("the first put wrote nothing")
	}

	// The same request again, by standard input and a response file.
	r2 := call{program: calleeExe, operation: "put", home: home, req: first, stdin: true, responseFile: true}.run(t)
	resp2 := r2.must(t)
	if r2.result.Code != 0 || !resp2.Replayed {
		t.Fatalf("replay: exit %d, replayed %v; want 0, true", r2.result.Code, resp2.Replayed)
	}
	if resp2.Body["wrote"] != "alpha" || resp2.Body["record"] != "first" {
		t.Errorf("replay body = %v, want the first answer", resp2.Body)
	}
	if len(resp2.Receipts) != 1 || resp2.Receipts[0].ReceiptID != resp1.Receipts[0].ReceiptID {
		t.Errorf("replay receipts = %+v, want the first call's receipt %s", resp2.Receipts, resp1.Receipts[0].ReceiptID)
	}
	sameSnapshot(t, "a replay", written, snapshot(t, home))

	// The same request_id with another request.
	other := newRequest(fromDispatch, calleeExe, "put", 30, map[string]any{"name": "beta", "record": "second"})
	r3 := call{program: calleeExe, operation: "put", home: home, req: other}.run(t)
	wantFailure(t, r3, other.id, 5, "conflict", "replay_mismatch")
	sameSnapshot(t, "a reused request_id", written, snapshot(t, home))

	// Another request_id for the identifier that exists with other content.
	again := newRequest(fromDispatch, calleeExe, "put", 31, map[string]any{"name": "alpha", "record": "third"})
	r4 := call{program: calleeExe, operation: "put", home: home, req: again}.run(t)
	wantFailure(t, r4, again.id, 5, "conflict", "already_exists")
	sameSnapshot(t, "a write that lost to the state", written, snapshot(t, home))
}
