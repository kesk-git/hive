package calls

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/declare"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// representative is the operation a call's exit states, repetition and size
// are proved on: the first operation of the first command of the callee that
// has one. A callee none of whose commands has an operation (the Shadow, at
// this stage) has none, and its call is proved by the refusal of every
// operation name.
func representative(callee *declare.Package) (command, operation string, ok bool) {
	for _, c := range callee.Commands {
		if len(c.Operations) > 0 {
			return c.Name, c.Operations[0], true
		}
	}
	return "", "", false
}

// TestDeclaredCallsExitStatesRepeatAndSize proves, for each call, on its
// representative operation, every exit state the callee can give at this stage
// to a request to an operation declared and not built, and the two things that
// remain to be proved of the call:
//
//   - usage, exit 2: an operation name the command has not, and no home;
//   - invalid, exit 3: bytes that are no request, with a null request_id;
//   - refused, exit 4: the operation declared and not built;
//   - too_large, exit 7: a request one byte over the limit the command
//     declares, with the limit, the actual size and the request named as the
//     record that was too large;
//   - replay: the same request sent again gets the same refusal, as a first
//     answer and not as a replay, and writes nothing; a refusal is not a
//     recorded outcome;
//   - size: a request carrying a record of 2,097,152 bytes arrives whole,
//     which the refusal after the whole request is parsed shows, or fails with
//     too_large where the limit is below it; either way with no body and no
//     part of the record in the output.
//
// The states a not-built operation cannot reach (failed, conflict, not_found,
// unavailable, unknown, held, ok) are proved by the transport tests against
// the test-only callee.
//
// It catches a callee that reads no further than a buffer, that answers a
// truncated or oversized request with a success or a partial result, that
// answers an unknown operation as a refusal, and a repeated refusal that
// differs from the first.
func TestDeclaredCallsExitStatesRepeatAndSize(t *testing.T) {
	decls := declarations(t)
	for _, c := range declaredCalls {
		caller, callee := decls[c.caller], decls[c.callee]
		command, operation, ok := representative(callee)
		if !ok {
			continue
		}
		t.Run(c.caller+" calls "+c.callee+" ("+command+" "+operation+")", func(t *testing.T) {
			t.Parallel()
			from := asPackage{caller.Name, caller.Executable}
			home := newHome(t)
			caps := capabilitiesOf(t, command)
			state := caps.Operation(operation).State
			limit := caps.Limits.MaxRequestBytes
			before := snapshot(t, home)

			t.Run("usage", func(t *testing.T) {
				req := newRequest(from, command, "no-such-operation", nextRequest(), map[string]any{})
				r := call{program: command, operation: "no-such-operation", home: home, req: req}.run(t)
				wantEarly(t, r, 2, "usage", "unknown_operation")

				req = newRequest(from, command, operation, nextRequest(), map[string]any{})
				r = call{program: command, operation: operation, req: req}.run(t) // no --home
				wantEarly(t, r, 2, "usage", "usage")
			})

			t.Run("invalid", func(t *testing.T) {
				req := request{id: ident(ids.Request, 1), data: []byte(`{"schema":"hive.request",`)}
				r := call{program: command, operation: operation, home: home, req: req}.run(t)
				wantEarly(t, r, 3, "invalid", "malformed_request")
			})

			t.Run("refused", func(t *testing.T) {
				req := newRequest(from, command, operation, nextRequest(), map[string]any{})
				r := call{program: command, operation: operation, home: home, req: req}.run(t)
				refusedByState(t, r, req.id, callee, command, operation, state, false)
			})

			t.Run("too_large", func(t *testing.T) {
				req := request{id: ident(ids.Request, 2)}
				r := call{program: command, operation: operation, home: home, req: req, requestPath: oversizeFile(t, limit+1)}.run(t)
				if got := wantTooLarge(t, r, "request", limit); got != limit+1 {
					t.Errorf("actual_bytes = %d, want %d", got, limit+1)
				}
				if r.resp.RequestID != "" {
					t.Errorf("request_id = %q, want null", r.resp.RequestID)
				}
			})

			t.Run("repeated", func(t *testing.T) {
				req := newRequest(from, command, operation, nextRequest(), map[string]any{})
				first := call{program: command, operation: operation, home: home, req: req}.run(t)
				second := call{program: command, operation: operation, home: home, req: req, stdin: true, responseFile: true}.run(t)
				refusedByState(t, first, req.id, callee, command, operation, state, false)
				refusedByState(t, second, req.id, callee, command, operation, state, true)
				if first.resp.Status != second.resp.Status || errorCode(first.resp) != errorCode(second.resp) {
					t.Errorf("the repeated request was answered %s %q, the first %s %q", second.resp.Status, errorCode(second.resp), first.resp.Status, errorCode(first.resp))
				}
			})

			t.Run("record of 2,097,152 bytes", func(t *testing.T) {
				req := newRequest(from, command, operation, nextRequest(), map[string]any{"record": pattern(recordBytes)})
				r := call{program: command, operation: operation, home: home, req: req, stdin: true, responseFile: true}.run(t)
				if int64(len(req.data)) > limit {
					wantTooLarge(t, r, "request", limit)
					return
				}
				refusedByState(t, r, req.id, callee, command, operation, state, false)
				if bytes.Contains(r.raw, []byte(pattern(64))) {
					t.Error("the refusal echoes the first bytes of the request's record")
				}
			})

			sameButOneDiagnostic(t, "the exit states", before, snapshot(t, home), home, callee)
		})
	}
}

// sameButOneDiagnostic fails the test unless the home is as it was but for the
// one diagnostic event the invalid subtest's request makes. That request is
// bytes that are no request, so no request_id and no correlation_id can be
// read: spec/events.md (Diagnostic events) keeps the lower-layer detail of the
// failure in the home's journal as one event of the answering package, with a
// correlation_id minted for it, the hive scope of the home and the invocation
// as its basis. Every other change, a second event, an event of another kind
// and any file outside the journal included, fails as sameSnapshot does.
func sameButOneDiagnostic(t testing.TB, what string, before, after map[string]string, home string, callee *declare.Package) {
	t.Helper()
	rest := func(m map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range m {
			if !strings.HasPrefix(k, "journal/") {
				out[k] = v
			}
		}
		return out
	}
	sameSnapshot(t, what, rest(before), rest(after))
	for k := range before {
		if strings.HasPrefix(k, "journal/") {
			t.Fatalf("%s found the journal holding %s before it began", what, k)
		}
	}
	var lines [][]byte
	for k := range after {
		if !strings.HasPrefix(k, "journal/") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(k)))
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
			lines = append(lines, l)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("%s left %d events in the journal, want exactly the one diagnostic of the invalid request", what, len(lines))
	}
	event := parseObject(t, lines[0])
	source, _ := event["source"].(map[string]any)
	body, _ := event["body"].(map[string]any)
	basis, _ := event["basis"].(map[string]any)
	ref, _ := basis["ref"].(map[string]any)
	corr, _ := event["correlation_id"].(string)
	if event["kind"] != callee.Name+".diagnostic.recorded" || source["package"] != callee.Name {
		t.Errorf("%s: the event is %v of %v, want %s.diagnostic.recorded", what, event["kind"], source, callee.Name)
	}
	if body["status"] != "invalid" || body["code"] != "malformed_request" || body["request_id"] != nil || event["causation"] != nil {
		t.Errorf("%s: the event's body is %v, causation %v; want invalid malformed_request with no request", what, body, event["causation"])
	}
	if corr == "" || basis["kind"] != "observation" || ref["schema"] != "hive.invocation" || ref["id"] != corr || event["home_id"] != testHome || event["scope_level"] != "hive" {
		t.Errorf("%s: the event is not the invocation's, observed, in the hive scope of the home: %v", what, event)
	}
}

// refusedByState fails the test unless the call was answered as the state the
// callee declares for the operation says: unsupported_operation for an
// operation declared and not built, operation_parked for one parked. An
// operation the callee has built is not refused: its answer must be a complete
// response a caller accepts, and what the response says is its own stage's to
// prove. A repeated request (repeat) may be answered as a replay.
func refusedByState(t testing.TB, r reply, requestID string, callee *declare.Package, command, operation string, state envelope.OperationState, repeat bool) {
	t.Helper()
	switch state {
	case envelope.Unsupported:
		wantRefusal(t, r, requestID, callee, command, operation, "unsupported_operation")
	case envelope.Parked:
		wantRefusal(t, r, requestID, callee, command, operation, "operation_parked")
	default:
		wantAccepted(t, r, requestID, callee, command, operation, repeat)
	}
}

// errorCode is the code of the response's error, empty when it has none.
func errorCode(resp *envelope.Response) string {
	if resp.Error == nil {
		return ""
	}
	return resp.Error.Code
}
