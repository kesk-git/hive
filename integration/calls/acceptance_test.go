package calls

import (
	"bytes"
	"testing"
)

// The acceptance test of test-integration (spec/test-targets.md): one
// declared call crosses the real command line with its envelopes, and a
// record of 2,097,152 bytes crosses a call whole or fails with too_large and
// no partial result. The expected exit codes and codes are those the tables
// of spec/commands.md fix.

// TestAcceptanceDeclaredCallCrossesTheCommandLine sends the call that word
// declares on pool (spec/packages.md, Calls between packages) to the built
// hive-pool, as a package's code would: a request document in a file, a
// response read from standard output. hive-pool's open-generation is declared
// and not built, so the call is proved up to its refusal: a complete response that
// echoes the request, refused with unsupported_operation, exit code 4, no
// body, and nothing written in the home.
//
// It catches a call that does not cross the real command line: an
// executable that is missing from dist/, that answers in a form a caller does
// not accept, that exits with a code its response does not state, or that
// writes into a home for a refused request.
func TestAcceptanceDeclaredCallCrossesTheCommandLine(t *testing.T) {
	home := newHome(t)
	before := snapshot(t, home)
	req := newRequest(t, asPackage{"word", "hive-word"}, "hive-pool", "open-generation", 1, map[string]any{})

	r := call{program: "hive-pool", operation: "open-generation", home: home, req: req}.run(t)
	resp := r.must(t)

	if got := r.result.Code; got != 4 {
		t.Errorf("exit code = %d, want 4 (refused)", got)
	}
	if string(resp.Status) != "refused" {
		t.Errorf("status = %q, want refused", resp.Status)
	}
	if resp.Error == nil || resp.Error.Code != "unsupported_operation" {
		t.Errorf("error = %+v, want code unsupported_operation", resp.Error)
	}
	if resp.RequestID != req.id {
		t.Errorf("request_id = %q, want the request's %q", resp.RequestID, req.id)
	}
	if resp.Command != "hive-pool" || resp.Operation != "open-generation" {
		t.Errorf("response is for %s %s, want hive-pool open-generation", resp.Command, resp.Operation)
	}
	if resp.Responder.Package != "pool" || resp.Responder.Command != "hive-pool" {
		t.Errorf("responder = %+v, want package pool, command hive-pool", resp.Responder)
	}
	if resp.Body != nil {
		t.Errorf("a refusal carries a body: %v", resp.Body)
	}
	if resp.Replayed {
		t.Error("a first request is not a replay")
	}
	sameSnapshot(t, "a refused call", before, snapshot(t, home))
}

// TestAcceptanceRecordCrossesWhole sends a record of 2,097,152 bytes through
// the real command line of the test-only callee, which answers the record it
// was sent. Where the callee's limit holds the record it comes back whole, in
// the request and in the response, byte for byte; where the limit is below
// it, the caller receives too_large and nothing of the record: no body, and
// not one of its bytes in the output.
//
// It catches a transport that cuts a large request or response at a pipe
// or buffer size, one that returns part of a record with a success, and one
// that answers too_large with part of the result beside it.
func TestAcceptanceRecordCrossesWhole(t *testing.T) {
	home := newHome(t)
	caller := asPackage{"dispatch", "hive-dispatch"}
	body := map[string]any{"record": pattern(recordBytes)}

	t.Run("whole", func(t *testing.T) {
		req := newRequest(t, caller, calleeExe, "echo", 1, body)
		r := call{program: calleeExe, operation: "echo", home: home, req: req}.run(t)
		resp := r.must(t)
		if r.result.Code != 0 || string(resp.Status) != "ok" {
			t.Fatalf("exit code %d, status %q; want 0, ok", r.result.Code, resp.Status)
		}
		if !sameRecord(resp.Body["record"], recordBytes) {
			t.Fatalf("the record did not come back whole: %d bytes", len(asString(resp.Body["record"])))
		}
	})

	t.Run("too_large", func(t *testing.T) {
		req := newRequest(t, caller, calleeSmallName, "echo", 2, body)
		r := call{program: calleeSmallName, operation: "echo", home: home, req: req}.run(t)
		resp := r.must(t)
		if r.result.Code != 7 || string(resp.Status) != "too_large" {
			t.Fatalf("exit code %d, status %q; want 7, too_large", r.result.Code, resp.Status)
		}
		e := resp.Error
		if e == nil || e.Code != "record_too_large" || e.LimitBytes == nil || e.ActualBytes == nil {
			t.Fatalf("error = %+v, want record_too_large with limit_bytes and actual_bytes", e)
		}
		if *e.LimitBytes != smallLimit || *e.ActualBytes != int64(len(req.data)) {
			t.Errorf("limit_bytes %d, actual_bytes %d; want %d and the request's %d", *e.LimitBytes, *e.ActualBytes, smallLimit, len(req.data))
		}
		if resp.Body != nil {
			t.Errorf("too_large carries a body: %d members", len(resp.Body))
		}
		if bytes.Contains(r.raw, []byte(pattern(64))) {
			t.Error("the output holds the first bytes of the record: a partial result")
		}
	})
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
