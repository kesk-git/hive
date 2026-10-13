package calls

import (
	"bytes"
	"strings"
	"testing"
)

// The size proof of spec/commands.md, Size, and of spec/test-targets.md: a
// record of 2,097,152 bytes, which is above the platform's limit on arguments
// on every supported platform, crosses a call whole or the caller receives
// too_large and nothing else. It is proved against the test-only callee on
// the default limits, which hold the record, and on the limit of 1,048,576
// bytes, which does not. A real operation's proof arrives with the stage
// that builds it.

// wantTooLarge fails the test unless the call ended with a complete too_large
// response about the record named by what, within the limit and above it as
// the response states, with no body and not one byte of the record in the
// output.
func wantTooLarge(t testing.TB, r reply, what string, limit int64) (actual int64) {
	t.Helper()
	resp := r.must(t)
	if r.result.Code != 7 || string(resp.Status) != "too_large" {
		t.Fatalf("exit code %d, status %q; want 7, too_large", r.result.Code, resp.Status)
	}
	e := resp.Error
	if e == nil || e.Code != "record_too_large" || e.LimitBytes == nil || e.ActualBytes == nil {
		t.Fatalf("error = %+v, want record_too_large with limit_bytes and actual_bytes", e)
	}
	if *e.LimitBytes != limit || *e.ActualBytes <= limit {
		t.Errorf("limit_bytes %d, actual_bytes %d; want %d and a larger size", *e.LimitBytes, *e.ActualBytes, limit)
	}
	if e.Detail["record"] != what {
		t.Errorf("detail.record = %v, want %q", e.Detail["record"], what)
	}
	if resp.Body != nil {
		t.Errorf("too_large carries a body of %d members", len(resp.Body))
	}
	if bytes.Contains(r.raw, []byte(pattern(64))) || bytes.Contains(r.stdout, []byte(pattern(64))) {
		t.Error("the output holds the first bytes of the record: a partial result")
	}
	return *e.ActualBytes
}

// TestSizeRecordCrossesWhole proves the record crossing whole where the
// limits hold it: in a request, in a response from a small request, in a
// mutating call, and in the replay of that call; by a request file, by
// standard input, on standard output and in a response file.
//
// It catches a transport that cuts a record at a buffer or pipe size, that
// reads a request only to the first read, that writes a response file before
// all of it is there, and a replay that records less than it returned.
func TestSizeRecordCrossesWhole(t *testing.T) {
	home := newHome(t)
	body := map[string]any{"record": pattern(recordBytes)}

	for _, c := range []struct {
		name         string
		stdin        bool
		responseFile bool
	}{
		{"request file, standard output", false, false},
		{"standard input, response file", true, true},
	} {
		t.Run("echo, "+c.name, func(t *testing.T) {
			req := newRequest(fromDispatch, calleeExe, "echo", 40, body)
			if len(req.data) <= recordBytes {
				t.Fatalf("the request is %d bytes, not above the record", len(req.data))
			}
			r := call{program: calleeExe, operation: "echo", home: home, req: req, stdin: c.stdin, responseFile: c.responseFile}.run(t)
			resp := r.must(t)
			if r.result.Code != 0 || !sameRecord(resp.Body["record"], recordBytes) {
				t.Fatalf("exit code %d; the record did not come back whole (%d bytes)", r.result.Code, len(asString(resp.Body["record"])))
			}
		})
	}

	t.Run("a response from a small request", func(t *testing.T) {
		req := newRequest(fromDispatch, calleeExe, "expand", 41, map[string]any{"bytes": int64(recordBytes)})
		if len(req.data) > 4096 {
			t.Fatalf("the request is %d bytes, not small", len(req.data))
		}
		r := call{program: calleeExe, operation: "expand", home: home, req: req}.run(t)
		resp := r.must(t)
		if r.result.Code != 0 || !sameRecord(resp.Body["record"], recordBytes) {
			t.Fatalf("exit code %d; the record did not come back whole", r.result.Code)
		}
	})

	t.Run("a mutating call and its replay", func(t *testing.T) {
		req := newRequest(fromDispatch, calleeExe, "put", 42, map[string]any{"name": "big", "record": pattern(recordBytes)})
		r1 := call{program: calleeExe, operation: "put", home: home, req: req}.run(t)
		resp1 := r1.must(t)
		if r1.result.Code != 0 || resp1.Replayed || !sameRecord(resp1.Body["record"], recordBytes) {
			t.Fatalf("put: exit %d, replayed %v; the record did not come back whole", r1.result.Code, resp1.Replayed)
		}
		written := snapshot(t, home)
		r2 := call{program: calleeExe, operation: "put", home: home, req: req, stdin: true, responseFile: true}.run(t)
		resp2 := r2.must(t)
		if r2.result.Code != 0 || !resp2.Replayed || !sameRecord(resp2.Body["record"], recordBytes) {
			t.Fatalf("replay: exit %d, replayed %v; the record did not come back whole", r2.result.Code, resp2.Replayed)
		}
		sameSnapshot(t, "the replay of a large record", written, snapshot(t, home))
	})
}

// TestSizeRecordFailsWithTooLarge proves the record failing where a limit is
// below it: in a request, by file and by standard input; in the response of a
// read-only call; and in the response of a mutating call, whose refusal
// leaves nothing behind, so that the same name can be written afterwards. In
// each the caller receives one complete too_large response, with the limit
// and the actual size, and nothing of the record.
//
// It catches a limit that cuts a record and returns the cut with a success,
// a too_large that carries a body or part of the record, one that is sized
// against the wrong record (the request for the response), and a mutating
// call that writes a record and then reports that it could not answer.
func TestSizeRecordFailsWithTooLarge(t *testing.T) {
	home := newHome(t)
	body := map[string]any{"record": pattern(recordBytes)}

	t.Run("request by file", func(t *testing.T) {
		before := snapshot(t, home)
		req := newRequest(fromDispatch, calleeSmallName, "echo", 50, body)
		r := call{program: calleeSmallName, operation: "echo", home: home, req: req}.run(t)
		if got := wantTooLarge(t, r, "request", smallLimit); got != int64(len(req.data)) {
			t.Errorf("actual_bytes = %d, want the request's %d", got, len(req.data))
		}
		if r.resp.RequestID != "" {
			t.Errorf("request_id = %q, want null: the request was refused before it was read", r.resp.RequestID)
		}
		sameSnapshot(t, "a request over the limit", before, snapshot(t, home))
	})

	t.Run("request by standard input, mutating", func(t *testing.T) {
		before := snapshot(t, home)
		req := newRequest(fromDispatch, calleeSmallName, "put", 51, map[string]any{"name": "toolarge", "record": pattern(recordBytes)})
		r := call{program: calleeSmallName, operation: "put", home: home, req: req, stdin: true, responseFile: true}.run(t)
		if got := wantTooLarge(t, r, "request", smallLimit); got != int64(len(req.data)) {
			t.Errorf("actual_bytes = %d, want the request's %d", got, len(req.data))
		}
		sameSnapshot(t, "a mutating request over the limit", before, snapshot(t, home))
	})

	t.Run("response, read-only", func(t *testing.T) {
		before := snapshot(t, home)
		req := newRequest(fromDispatch, calleeSmallName, "expand", 52, map[string]any{"bytes": int64(recordBytes)})
		r := call{program: calleeSmallName, operation: "expand", home: home, req: req}.run(t)
		if got := wantTooLarge(t, r, "response", smallLimit); got < recordBytes {
			t.Errorf("actual_bytes = %d, below the record the response would have carried", got)
		}
		if r.resp.RequestID != req.id {
			t.Errorf("request_id = %q, want %q: a response too large is still the answer to its request", r.resp.RequestID, req.id)
		}
		sameSnapshot(t, "a response over the limit", before, snapshot(t, home))
	})

	t.Run("response, mutating, leaves nothing behind", func(t *testing.T) {
		before := snapshot(t, home)
		req := newRequest(fromDispatch, calleeSmallName, "grow", 53, map[string]any{"name": "grown", "bytes": int64(recordBytes)})
		r := call{program: calleeSmallName, operation: "grow", home: home, req: req}.run(t)
		wantTooLarge(t, r, "response", smallLimit)
		sameSnapshot(t, "a mutating call whose response was too large", before, snapshot(t, home))

		// Nothing was written and no outcome was recorded, so the name is free
		// and a smaller answer to the same name succeeds.
		again := newRequest(fromDispatch, calleeSmallName, "grow", 54, map[string]any{"name": "grown", "bytes": int64(16)})
		r2 := call{program: calleeSmallName, operation: "grow", home: home, req: again}.run(t)
		if resp := r2.must(t); r2.result.Code != 0 || !sameRecord(resp.Body["record"], 16) {
			t.Fatalf("a smaller answer for the same name: exit %d, status %q; want 0, ok", r2.result.Code, resp.Status)
		}
	})
}

// TestSizeLimitIsExact proves that the limit is the size a request may be and
// not one byte more: a request of exactly 1,048,576 bytes is read and
// answered, and one of 1,048,577 is too_large with its actual size.
//
// It catches a limit applied one byte early or late, and an actual size that
// is the limit and not the size the caller sent.
func TestSizeLimitIsExact(t *testing.T) {
	home := newHome(t)
	padded := func(n int, id int) request {
		req := newRequest(fromDispatch, calleeSmallName, "state", id, map[string]any{"status": "ok", "pad": strings.Repeat("p", 0)})
		extra := n - len(req.data)
		if extra < 0 {
			t.Fatalf("the base request is already %d bytes", len(req.data))
		}
		req = newRequest(fromDispatch, calleeSmallName, "state", id, map[string]any{"status": "ok", "pad": strings.Repeat("p", extra)})
		if len(req.data) != n {
			t.Fatalf("the request is %d bytes, want %d", len(req.data), n)
		}
		return req
	}

	at := padded(smallLimit, 60)
	r := call{program: calleeSmallName, operation: "state", home: home, req: at}.run(t)
	if resp := r.must(t); r.result.Code != 0 || string(resp.Status) != "ok" {
		t.Errorf("a request of exactly the limit: exit %d, status %q; want 0, ok", r.result.Code, resp.Status)
	}

	over := padded(smallLimit+1, 61)
	r = call{program: calleeSmallName, operation: "state", home: home, req: over}.run(t)
	if got := wantTooLarge(t, r, "request", smallLimit); got != smallLimit+1 {
		t.Errorf("actual_bytes = %d, want %d", got, smallLimit+1)
	}
}
