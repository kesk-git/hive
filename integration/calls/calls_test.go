package calls

import (
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/declare"
	"github.com/kesk-git/hive/contract/envelope"
)

// The calls between packages (spec/packages.md, Calls between packages): the
// table below is that table, written out, and every row is one call. Each is
// proved through the real command line of the callee's built executable, as
// the caller's code would make it: a request document naming the caller, in a
// file or on standard input, and a response read from standard output or a
// file, accepted under the rules of spec/commands.md.
//
// Each call to an operation declared and not built is proved up to the
// refusal the contract states for it (unsupported_operation, exit 4;
// operation_parked for a parked one), and the size proof for it arrives with
// the stage that builds the operation. An operation that a callee has built
// and declares supported is proved only up to what no body decides: its
// capability declaration, its addressing, and the refusals that come before a
// request is read. The call from hive-word to hive-pool is proved past that
// point, in word_pool_test.go.

var declaredCalls = []struct{ caller, callee string }{
	{"word", "pool"},
	{"dispatch", "pool"}, {"dispatch", "spawning-pool"}, {"dispatch", "word"},
	{"synapse", "word"}, {"synapse", "dispatch"},
	{"shadow", "pool"}, {"shadow", "dispatch"}, {"shadow", "synapse"}, {"shadow", "word"},
	{"ui", "pool"}, {"ui", "dispatch"}, {"ui", "shadow"}, {"ui", "synapse"}, {"ui", "word"},
}

// TestEveryDeclaredCallIsProved proves that the calls the declarations make
// are the calls this package proves: no package declares a call on another
// that has no row above, and no row is a call no declaration makes.
//
// It catches a call added to a declaration with no proof of it here, and a
// proof that outlives the call it was written for.
func TestEveryDeclaredCallIsProved(t *testing.T) {
	var declared, proved []string
	for name, d := range declarations(t) {
		for _, callee := range d.Calls {
			declared = append(declared, name+" -> "+callee)
		}
	}
	for _, c := range declaredCalls {
		proved = append(proved, c.caller+" -> "+c.callee)
	}
	sort.Strings(declared)
	sort.Strings(proved)
	if !slices.Equal(declared, proved) {
		t.Errorf("the declarations make the calls\n  %s\nand this package proves\n  %s", strings.Join(declared, "\n  "), strings.Join(proved, "\n  "))
	}
}

// requestNumber gives every request of the proof its own identity.
var requestNumber atomic.Int64

func nextRequest() int { return int(requestNumber.Add(1)) }

// TestDeclaredCallsCrossTheCommandLine is the proof of each call, through the
// callee's every command and every operation it declares:
//
//   - the command's capabilities agree with the callee's declaration, and the
//     capabilities invocation answers ok with a null request_id;
//   - a request to each operation gets one complete response that echoes it:
//     for an operation declared and not built, refused with
//     unsupported_operation and exit code 4, no body, no receipt, nothing
//     written to the home;
//   - a request that names another command than the one it is sent to is
//     invalid with envelope_mismatch, for built and not built alike;
//   - a command that declares no operation refuses every operation name as
//     usage, exit 2.
//
// It catches an executable that does not own a command name its declaration
// lists, a capabilities answer that disagrees with the declaration, an
// operation that answers anything but its declared state, a refusal that
// leaves a record, and a response a caller cannot accept.
func TestDeclaredCallsCrossTheCommandLine(t *testing.T) {
	decls := declarations(t)
	for _, c := range declaredCalls {
		caller, callee := decls[c.caller], decls[c.callee]
		t.Run(c.caller+" calls "+c.callee, func(t *testing.T) {
			t.Parallel()
			from := asPackage{caller.Name, caller.Executable}
			home := newHome(t)
			for _, cmd := range callee.Commands {
				t.Run(cmd.Name, func(t *testing.T) {
					proveCommand(t, from, callee, cmd, home)
				})
			}
		})
	}
}

// capabilitiesOf runs the capabilities invocation of the command and decodes
// its answer.
func capabilitiesOf(t testing.TB, command string) *envelope.Capabilities {
	t.Helper()
	r := start(t, command, []string{"capabilities"}, input{})
	resp, err := envelope.AcceptCapabilities(r.stdout, int(r.result.Code))
	if err != nil {
		t.Fatalf("%s capabilities: %v; stderr: %s", command, err, r.stderr)
	}
	if r.result.Code != 0 || string(resp.Status) != "ok" || resp.RequestID != "" {
		t.Fatalf("%s capabilities: exit %d, status %q, request_id %q; want 0, ok, null", command, r.result.Code, resp.Status, resp.RequestID)
	}
	caps, err := envelope.DecodeCapabilities(resp.Body)
	if err != nil {
		t.Fatalf("%s capabilities: %v", command, err)
	}
	return caps
}

func proveCommand(t *testing.T, from asPackage, callee *declare.Package, cmd declare.Command, home string) {
	caps := capabilitiesOf(t, cmd.Name)
	if caps.Command != cmd.Name || caps.Package != callee.Name || caps.Executable != callee.Executable {
		t.Fatalf("capabilities name command %s of package %s in %s; want %s of %s in %s", caps.Command, caps.Package, caps.Executable, cmd.Name, callee.Name, callee.Executable)
	}
	var declared []string
	states := map[string]envelope.OperationState{}
	for _, op := range caps.Operations {
		declared = append(declared, op.Name)
		states[op.Name] = op.State
	}
	want := slices.Clone(cmd.Operations)
	sort.Strings(declared)
	sort.Strings(want)
	if !slices.Equal(declared, want) {
		t.Fatalf("capabilities declare the operations %v; the package's declaration lists %v", declared, want)
	}

	if len(cmd.Operations) == 0 {
		// Nothing to call: the command refuses every operation name as usage.
		req := newRequest(t, from, cmd.Name, "no-such-operation", nextRequest(), map[string]any{})
		r := call{program: cmd.Name, operation: "no-such-operation", home: home, req: req}.run(t)
		wantEarly(t, r, 2, "usage", "unknown_operation")
		return
	}

	for _, op := range cmd.Operations {
		t.Run(op, func(t *testing.T) { proveOperation(t, from, callee, cmd.Name, op, states[op], home) })
	}
}

// proveOperation proves a request to one operation of a callee: its answer by
// the state the callee declares for it, and the addressing every state shares.
func proveOperation(t *testing.T, from asPackage, callee *declare.Package, command, operation string, state envelope.OperationState, home string) {
	before := snapshot(t, home)

	req := newRequest(t, from, command, operation, nextRequest(), map[string]any{})
	r := call{program: command, operation: operation, home: home, req: req}.run(t)
	switch state {
	case envelope.Unsupported:
		wantRefusal(t, r, req.id, callee, command, operation, "unsupported_operation")
	case envelope.Parked:
		wantRefusal(t, r, req.id, callee, command, operation, "operation_parked")
	default:
		wantAccepted(t, r, req.id, callee, command, operation, false)
	}

	other := newMisaddressed(t, from, command, "hive-other", operation, nextRequest(), map[string]any{})
	r = call{program: command, operation: operation, home: home, req: other}.run(t)
	wantFailure(t, r, other.id, 3, "invalid", "envelope_mismatch")

	sameSnapshot(t, "the calls", before, snapshot(t, home))
}

// wantRefusal fails the test unless the call ended with the complete refusal
// the contract states for an operation that is declared and not built (or
// parked): refused, exit 4, the code given, echoing the request, answered by
// the callee's package and command, with no body, no receipt and no replay.
func wantRefusal(t testing.TB, r reply, requestID string, callee *declare.Package, command, operation, code string) {
	t.Helper()
	wantFailure(t, r, requestID, 4, "refused", code)
	resp := r.resp
	if resp.Command != command || resp.Operation != operation {
		t.Errorf("the response is for %s %s, want %s %s", resp.Command, resp.Operation, command, operation)
	}
	if resp.Responder.Package != callee.Name || resp.Responder.Command != command {
		t.Errorf("responder = %+v, want package %s, command %s", resp.Responder, callee.Name, command)
	}
	if len(resp.Receipts) != 0 || resp.Replayed {
		t.Errorf("a refusal that leaves nothing carries %d receipts, replayed %v", len(resp.Receipts), resp.Replayed)
	}
	if !isOneDocument(r.raw) {
		t.Error("the response is not one document followed by a line feed")
	}
}

// wantAccepted fails the test unless the call ended with a complete response
// a caller accepts for an operation the callee has built, whatever its
// status: it answers the request's identity, command and operation, is written
// by the callee's package and command at a time, is a replay only where one is
// allowed, and carries receipts only where its status may leave them. What the
// operation's body says is its own stage's to prove.
func wantAccepted(t testing.TB, r reply, requestID string, callee *declare.Package, command, operation string, mayReplay bool) {
	t.Helper()
	resp := r.must(t)
	if resp.RequestID != requestID {
		t.Errorf("request_id = %q, want %q", resp.RequestID, requestID)
	}
	if resp.Command != command || resp.Operation != operation {
		t.Errorf("the response is for %s %s, want %s %s", resp.Command, resp.Operation, command, operation)
	}
	if resp.Responder.Package != callee.Name || resp.Responder.Command != command {
		t.Errorf("responder = %+v, want package %s, command %s", resp.Responder, callee.Name, command)
	}
	if _, err := time.Parse(time.RFC3339Nano, resp.CompletedAt); err != nil {
		t.Errorf("completed_at = %q: %v", resp.CompletedAt, err)
	}
	if resp.Replayed && !mayReplay {
		t.Error("a first answer is marked as a replay")
	}
	switch resp.Status {
	case envelope.Ok, envelope.Refused, envelope.Held, envelope.Unknown:
	default:
		if len(resp.Receipts) != 0 {
			t.Errorf("a %s response carries %d receipts", resp.Status, len(resp.Receipts))
		}
	}
	if !isOneDocument(r.raw) {
		t.Error("the response is not one document followed by a line feed")
	}
}

// wantEarly fails the test unless the call ended with a complete response of
// a status a callee gives before it can read the request's identity: the exit
// code, status and code given, a null request_id and no body.
func wantEarly(t testing.TB, r reply, exit int, status, code string) {
	t.Helper()
	wantFailure(t, r, "", exit, status, code)
}
