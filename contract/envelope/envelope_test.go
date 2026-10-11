package envelope

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/schema"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// tree decodes a document into its JSON tree.
func tree(t *testing.T, data []byte) map[string]any {
	t.Helper()
	v, err := schema.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)
}

// encode writes a tree as a document.
func encode(t *testing.T, v any) []byte {
	t.Helper()
	out, err := canon.Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	return append(out, '\n')
}

// failure asserts that err is a Failure of the status and code.
func failure(t *testing.T, err error, status Status, code string) {
	t.Helper()
	f := AsFailure(err)
	if f == nil {
		t.Fatalf("got %v, want a failure %s/%s", err, status, code)
	}
	if f.Status != status || f.Err.Code != code {
		t.Fatalf("got %s/%s (%s), want %s/%s", f.Status, f.Err.Code, f.Err.Message, status, code)
	}
	if got, _ := StatusOfCode(code); got != f.Status {
		t.Fatalf("code %s belongs to %s, the failure says %s", code, got, f.Status)
	}
}

// The eleven exit states: each status one exit code, each exit code one
// status, in the order of the table of spec/commands.md.
func TestElevenExitStates(t *testing.T) {
	want := []Status{"ok", "failed", "usage", "invalid", "refused", "conflict", "not_found", "too_large", "unavailable", "unknown", "held"}
	got := Statuses()
	if len(got) != len(want) || len(got) != MaxExitCode+1 {
		t.Fatalf("%d statuses, want %d", len(got), len(want))
	}
	for i, s := range want {
		if got[i] != s || s.ExitCode() != i || !s.Valid() {
			t.Errorf("%s: exit code %d, want %d", s, got[i].ExitCode(), i)
		}
		back, ok := StatusOf(i)
		if !ok || back != s {
			t.Errorf("exit code %d maps to %s, want %s", i, back, s)
		}
	}
	for _, code := range []int{-1, 11, 63, 64, 255} {
		if _, ok := StatusOf(code); ok {
			t.Errorf("exit code %d names a status", code)
		}
	}
	if Status("done").Valid() {
		t.Error("an unknown word is a status")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("ExitCode of a non-status did not panic")
			}
		}()
		Status("done").ExitCode()
	}()
	changed := map[Status]bool{Ok: true, Refused: true, Held: true, Unknown: true}
	for _, s := range got {
		if s.Changed() != changed[s] {
			t.Errorf("%s: Changed %v", s, s.Changed())
		}
	}
}

// Every common code belongs to the status its table row gives, and the
// table holds every code of spec/commands.md once.
func TestCommonCodesMapToTheirStatus(t *testing.T) {
	table := map[Status][]string{
		Failed:      {"internal", "io_failure"},
		Usage:       {"usage", "unknown_operation", "argument_too_large"},
		Invalid:     {"malformed_request", "schema_violation", "unsupported_version", "envelope_mismatch"},
		Refused:     {"unsupported_operation", "operation_parked", "authority_missing", "authority_insufficient", "home_mismatch", "home_inside_repository", "scope_mismatch", "clone_identity_mismatch"},
		Conflict:    {"revision_conflict", "lock_held", "already_exists", "replay_mismatch"},
		NotFound:    {"not_found", "home_not_found"},
		TooLarge:    {"record_too_large"},
		Unavailable: {"dependency_missing", "dependency_unusable", "authentication_refused", "source_unreadable", "record_malformed"},
		Unknown:     {"outcome_unknown"},
		Held:        {"held"},
	}
	count := 0
	for status, codes := range table {
		for _, code := range codes {
			count++
			got, ok := StatusOfCode(code)
			if !ok || got != status {
				t.Errorf("%s: status %s, want %s", code, got, status)
			}
			if !ValidCode(code) {
				t.Errorf("%s: not a valid code", code)
			}
			if f := Common(code, "x"); f.Status != status || f.Err.Code != code {
				t.Errorf("Common(%s): %s/%s", code, f.Status, f.Err.Code)
			}
		}
	}
	all := CommonCodes()
	if len(all) != count {
		t.Errorf("%d common codes, the table has %d", len(all), count)
	}
	seen := map[string]bool{}
	for _, c := range all {
		if seen[c] {
			t.Errorf("%s is listed twice", c)
		}
		seen[c] = true
	}
	if _, ok := StatusOfCode("pool.full"); ok {
		t.Error("a package code is a common code")
	}
	for _, code := range []string{"pool.full", "internal", "a.b_c"} {
		if !ValidCode(code) {
			t.Errorf("%q should be a valid code", code)
		}
	}
	for _, code := range []string{"", "Internal", "pool.", ".x", "a.b.c", "a-b"} {
		if ValidCode(code) {
			t.Errorf("%q should not be a valid code", code)
		}
	}
}

func TestFailureRules(t *testing.T) {
	if f := Oversize("request", 10, 11); f.Status != TooLarge || *f.Err.LimitBytes != 10 || *f.Err.ActualBytes != 11 || f.Err.Detail["record"] != "request" {
		t.Errorf("Oversize: %+v", f)
	}
	if err := (&Failure{Status: TooLarge, Err: Error{Code: CodeRecordTooLarge, Message: "x"}}).Check(); err == nil {
		t.Error("too_large without sizes passed")
	}
	if err := (&Failure{Status: Held, Err: Error{Code: CodeHeld, Message: "x"}}).Check(); err == nil {
		t.Error("held without a hold passed")
	}
	if err := Hold(RecordRef{Schema: "word.question", ID: "q_1"}, "waiting").Check(); err != nil {
		t.Error(err)
	}
	if f := Common(CodeOutcomeUnknown, "x").Retry(); f.Err.Retryable {
		t.Error("an unknown outcome became retryable")
	}
	if f := Common(CodeLockHeld, "x").Retry(); !f.Err.Retryable {
		t.Error("lock_held did not become retryable")
	}
	if err := (&Failure{Status: Unknown, Err: Error{Code: CodeOutcomeUnknown, Message: "x", Retryable: true}}).Check(); err == nil {
		t.Error("a retryable unknown passed")
	}
	if err := (&Failure{Status: Ok, Err: Error{Code: "internal", Message: "x"}}).Check(); err == nil {
		t.Error("ok as a failure passed")
	}
	if err := (&Failure{Status: Failed, Err: Error{Code: "internal"}}).Check(); err == nil {
		t.Error("an empty message passed")
	}
	// The message limit counts characters, as the schema validator does.
	for _, c := range []struct {
		chars int
		ok    bool
	}{{MaxMessageChars, true}, {MaxMessageChars + 1, false}} {
		f := &Failure{Status: Failed, Err: Error{Code: CodeInternal, Message: strings.Repeat("é", c.chars)}}
		if err := f.Check(); (err == nil) != c.ok {
			t.Errorf("%d characters: Check %v", c.chars, err)
		}
		r := &Response{RequestID: "req_01arz3ndektsv4rrffq69g5fav", Command: "hive-pool", Status: Failed, Responder: Responder{Package: "pool", Command: "hive-pool", ImplementationVersion: "0.1.0", ContractVersion: ContractVersion},
			CompletedAt: "2026-10-11T08:00:00.000Z", Error: &f.Err}
		out, err := r.Encode()
		if c.ok {
			if err != nil {
				t.Fatalf("%d characters: %v", c.chars, err)
			}
			if _, err := AcceptResponse(out, 1, "req_01arz3ndektsv4rrffq69g5fav"); err != nil {
				t.Errorf("%d characters: %v", c.chars, err)
			}
		} else if err == nil {
			t.Errorf("%d characters encoded", c.chars)
		}
	}
	for _, bad := range []func(){
		func() { Fail(Ok, "internal", "x") },
		func() { Fail("done", "internal", "x") },
		func() { Fail(Failed, "Internal", "x") },
		func() { Common("pool.full", "x") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("no panic")
				}
			}()
			bad()
		}()
	}
	f := Common(CodeInternal, "boom").With("where", "here")
	if f.Err.Detail["where"] != "here" || f.Error() != "failed (internal): boom" {
		t.Errorf("With or Error: %v", f)
	}
	var err error = f
	if AsFailure(errors.Join(errors.New("outer"), err)) != f {
		t.Error("AsFailure did not unwrap")
	}
	if AsFailure(errors.New("plain")) != nil {
		t.Error("AsFailure found a failure in a plain error")
	}
}

func TestVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"1.0", "1.0", 0}, {"1.1", "1.0", 1}, {"1.0", "1.1", -1}, {"2.0", "1.9", 1}, {"1.10", "1.9", 1}, {"10.0", "9.0", 1}} {
		got, err := CompareVersions(c.a, c.b)
		if err != nil || (got > 0) != (c.want > 0) || (got < 0) != (c.want < 0) {
			t.Errorf("CompareVersions(%s, %s) = %d, %v; want %d", c.a, c.b, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "1", "01.0", "1.01", "v1.0", "1.0.0", "1.0 "} {
		if _, err := CompareVersions(bad, "1.0"); !errors.Is(err, ErrVersionForm) {
			t.Errorf("%q: %v", bad, err)
		}
		if Newer(bad, "1.0") {
			t.Errorf("%q counts as newer", bad)
		}
	}
	if !Newer("1.1", "1.0") || !Newer("2.0", "1.0") || Newer("1.0", "1.0") || Newer("0.9", "1.0") {
		t.Error("Newer is wrong")
	}
}

// A request round-trips: decode, encode canonically, decode again to the
// same value; the body keeps its text and its whole numbers as written.
func TestRequestRoundTrip(t *testing.T) {
	data := fixture(t, "request.json")
	r, err := DecodeRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	if r.Command != "hive-fleet" || r.Operation != "provision" || r.OperationVersion != "1.0" ||
		r.RequestID != "req_01arz3ndektsv4rrffq69g5fav" || r.Causation == nil || r.Causation.ID != "req_01arz3ndektsv4rrffq69g5fa0" ||
		r.Caller.Kind != CallerTest || r.Level != ScopeProject || r.CloneID != "clone_01arz3ndektsv4rrffq69g5fav" ||
		r.Generation != 2 || r.SourceRevision == "" || len(r.ExpectedRevisions) != 1 || r.ExpectedRevisions[0].RecordRevision != 3 ||
		r.Authority.Kind != AuthorityWord || r.Authority.Ref == nil || r.Authority.Ref.RecordRevision != 1 {
		t.Fatalf("decoded wrongly: %+v", r)
	}
	if r.Body["note"] != "kept é as written" || r.Body["count"] != int64(9007199254740991) {
		t.Errorf("body not kept as written: %v", r.Body)
	}
	out, err := r.Encode()
	if err != nil {
		t.Fatal(err)
	}
	want, err := canon.Line(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, want) {
		t.Errorf("Encode is not the canonical form:\n%s\n%s", out, want)
	}
	d1, _ := r.Digest()
	d2, _ := Digest(data)
	if d1 != d2 || !canon.ValidDigest(d1) {
		t.Errorf("digests differ: %s %s", d1, d2)
	}
	again, err := DecodeRequest(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encode(t, again.Tree()), out) {
		t.Error("a second round changed the document")
	}
	if err := r.Addressed("hive-fleet", "provision"); err != nil {
		t.Error(err)
	}
	failure(t, r.Addressed("hive-fleet", "route"), Invalid, CodeEnvelopeMismatch)
	failure(t, r.Addressed("hive-pool", "provision"), Invalid, CodeEnvelopeMismatch)

	// A minimal request: every optional field absent.
	min := &Request{Command: "hive-pool", Operation: "list", OperationVersion: "1.0", RequestID: r.RequestID, CorrelationID: r.CorrelationID,
		IssuedAt: r.IssuedAt, Caller: Caller{Kind: CallerOperator}, Scope: Scope{Level: ScopeHive, HomeID: r.HomeID}, Authority: Authority{Kind: AuthorityNone}}
	out, err = min.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeRequest(out)
	if err != nil {
		t.Fatal(err)
	}
	if back.ExpectedRevisions != nil || back.Causation != nil || len(back.Body) != 0 || back.Body == nil {
		t.Errorf("minimal request decoded wrongly: %+v", back)
	}
	// An empty expected_revisions is present and empty, not absent.
	min.ExpectedRevisions = []ExpectedRevision{}
	out, _ = min.Encode()
	if !strings.Contains(string(out), `"expected_revisions":[]`) {
		t.Error("an empty list was dropped")
	}
	back, _ = DecodeRequest(out)
	if back.ExpectedRevisions == nil {
		t.Error("an empty list was read as absent")
	}
}

// Every way a request is rejected carries the code of spec/commands.md.
func TestRequestRejections(t *testing.T) {
	data := fixture(t, "request.json")
	mutate := func(f func(m map[string]any)) []byte {
		m := tree(t, data)
		f(m)
		return encode(t, m)
	}
	cases := []struct {
		name string
		data []byte
		code string
	}{
		{"empty", nil, CodeMalformedRequest},
		{"not json", []byte("hello\n"), CodeMalformedRequest},
		{"byte-order mark", append([]byte("\xef\xbb\xbf"), data...), CodeMalformedRequest},
		{"truncated", data[:len(data)/2], CodeMalformedRequest},
		{"fractional number", bytes.Replace(data, []byte(`"generation": 2`), []byte(`"generation": 2.0`), 1), CodeMalformedRequest},
		{"duplicate member", bytes.Replace(data, []byte(`"schema": "hive.request",`), []byte(`"schema": "hive.request", "schema": "hive.request",`), 1), CodeMalformedRequest},
		{"an array", []byte("[]\n"), CodeSchemaViolation},
		{"newer minor", mutate(func(m map[string]any) { m["schema_version"] = "1.1" }), CodeUnsupportedVersion},
		{"newer major", mutate(func(m map[string]any) { m["schema_version"] = "2.0" }), CodeUnsupportedVersion},
		{"wrong schema name", mutate(func(m map[string]any) { m["schema"] = "hive.response" }), CodeSchemaViolation},
		{"unknown field", mutate(func(m map[string]any) { m["extra"] = true }), CodeSchemaViolation},
		{"missing scope", mutate(func(m map[string]any) { delete(m, "scope_level") }), CodeSchemaViolation},
		{"hive scope with workspace", mutate(func(m map[string]any) { m["scope_level"] = "hive" }), CodeSchemaViolation},
		{"generation without task", mutate(func(m map[string]any) { delete(m, "task_id") }), CodeSchemaViolation},
		{"authority ref missing", mutate(func(m map[string]any) { m["authority"] = map[string]any{"kind": "word"} }), CodeSchemaViolation},
		{"uppercase id", mutate(func(m map[string]any) { m["request_id"] = "req_01ARZ3NDEKTSV4RRFFQ69G5FAV" }), CodeSchemaViolation},
		{"timestamp with offset", mutate(func(m map[string]any) { m["issued_at"] = "2026-10-11T08:00:00.000+00:00" }), CodeSchemaViolation},
		{"body not an object", mutate(func(m map[string]any) { m["body"] = []any{} }), CodeSchemaViolation},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DecodeRequest(c.data)
			failure(t, err, Invalid, c.code)
		})
	}
	// An older minor version is not refused as newer; the schema decides.
	_, err := DecodeRequest(mutate(func(m map[string]any) { m["schema_version"] = "0.9" }))
	failure(t, err, Invalid, CodeSchemaViolation)
}

func TestLooseRequestID(t *testing.T) {
	if id, err := LooseRequestID([]byte(`{"request_id": "req_01arz3ndektsv4rrffq69g5fav", "junk": 1}`)); err != nil || id != "req_01arz3ndektsv4rrffq69g5fav" {
		t.Errorf("got %q, %v", id, err)
	}
	for _, bad := range []string{``, `[]`, `{}`, `{"request_id": 1}`, `{"request_id": "REQ_X"}`, `{"request_id": "ws_01arz3ndektsv4rrffq69g5fav"}`, `{"request_id": "req_01arz3ndektsv4rrffq69g5fav"`} {
		if id, err := LooseRequestID([]byte(bad)); err == nil || id != "" {
			t.Errorf("%s: got %q, %v", bad, id, err)
		}
	}
}

func TestResponseRoundTrip(t *testing.T) {
	for _, name := range []string{"response-ok.json", "response-held.json", "response-too-large.json"} {
		t.Run(name, func(t *testing.T) {
			data := fixture(t, name)
			r, err := DecodeResponse(data)
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.Encode()
			if err != nil {
				t.Fatal(err)
			}
			want, _ := canon.Line(data)
			if !bytes.Equal(out, want) {
				t.Errorf("Encode is not the canonical form:\n%s\n%s", out, want)
			}
			caller := r.RequestID
			if caller == "" {
				caller = "req_01arz3ndektsv4rrffq69g5fav"
			}
			if _, err := AcceptResponse(out, r.ExitCode(), caller); err != nil {
				t.Errorf("the command's own response is not accepted: %v", err)
			}
		})
	}
	ok, _ := DecodeResponse(fixture(t, "response-ok.json"))
	if ok.Status != Ok || ok.ExitCode() != 0 || len(ok.Receipts) != 1 || ok.Receipts[0].Kind != "dispatch.home.created" || ok.Receipts[0].Subject == nil || ok.Body["home"] == nil || ok.Error != nil {
		t.Errorf("ok decoded wrongly: %+v", ok)
	}
	held, _ := DecodeResponse(fixture(t, "response-held.json"))
	if held.Status != Held || held.ExitCode() != 10 || !held.Replayed || held.Error == nil || held.Error.Hold == nil || held.Error.Hold.ID != "q_01arz3ndektsv4rrffq69g5fav" || held.Body != nil {
		t.Errorf("held decoded wrongly: %+v", held)
	}
	large, _ := DecodeResponse(fixture(t, "response-too-large.json"))
	if large.RequestID != "" || large.Operation != "" || large.ExitCode() != 7 || *large.Error.LimitBytes != 67108864 || *large.Error.ActualBytes != 67108865 || large.Error.Detail["record"] != "request" {
		t.Errorf("too_large decoded wrongly: %+v", large)
	}
	if out, _ := large.Encode(); !strings.Contains(string(out), `"request_id":null`) || !strings.Contains(string(out), `"operation":null`) {
		t.Errorf("null fields not written: %s", out)
	}
}

func TestResponseRules(t *testing.T) {
	data := fixture(t, "response-ok.json")
	mutate := func(f func(m map[string]any)) []byte {
		m := tree(t, data)
		f(m)
		return encode(t, m)
	}
	cases := []struct {
		name string
		data []byte
		code string
	}{
		{"status disagrees with exit code", mutate(func(m map[string]any) { m["exit_code"] = int64(1) }), CodeSchemaViolation},
		{"exit code beyond the table", mutate(func(m map[string]any) { m["status"] = "failed"; m["exit_code"] = int64(11); delete(m, "body") }), CodeSchemaViolation},
		{"ok without body", mutate(func(m map[string]any) { delete(m, "body") }), CodeSchemaViolation},
		{"ok with error", mutate(func(m map[string]any) { m["error"] = map[string]any{"code": "x", "message": "y", "retryable": false} }), CodeSchemaViolation},
		{"failed with body", mutate(func(m map[string]any) {
			m["status"] = "failed"
			m["exit_code"] = int64(1)
			m["error"] = map[string]any{"code": "internal", "message": "y", "retryable": false}
		}), CodeSchemaViolation},
		{"failed without error", mutate(func(m map[string]any) { m["status"] = "failed"; m["exit_code"] = int64(1); delete(m, "body") }), CodeSchemaViolation},
		{"too_large without sizes", mutate(func(m map[string]any) {
			m["status"] = "too_large"
			m["exit_code"] = int64(7)
			delete(m, "body")
			m["error"] = map[string]any{"code": "record_too_large", "message": "y", "retryable": false}
		}), CodeSchemaViolation},
		{"held without hold", mutate(func(m map[string]any) {
			m["status"] = "held"
			m["exit_code"] = int64(10)
			delete(m, "body")
			m["error"] = map[string]any{"code": "held", "message": "y", "retryable": false}
		}), CodeSchemaViolation},
		{"receipt breaks its envelope", mutate(func(m map[string]any) { m["receipts"].([]any)[0].(map[string]any)["kind"] = "Home" }), CodeSchemaViolation},
		{"newer version", mutate(func(m map[string]any) { m["schema_version"] = "1.2" }), CodeUnsupportedVersion},
		{"not a record", []byte("{\n"), CodeMalformedRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DecodeResponse(c.data)
			failure(t, err, Invalid, c.code)
		})
	}
	// Encode refuses what the schema would: a response is complete or it
	// is not a response.
	r := &Response{RequestID: "req_01arz3ndektsv4rrffq69g5fav", Command: "hive-pool", Operation: "list", Status: Ok, Responder: Responder{"pool", "hive-pool", "test", "1.0"}, CompletedAt: "2026-10-11T08:00:00.000Z"}
	if _, err := r.Encode(); err == nil {
		t.Error("ok without a body was encoded")
	}
	r.Body = map[string]any{}
	r.Error = &Error{Code: "x", Message: "y"}
	if _, err := r.Encode(); err == nil {
		t.Error("ok with an error was encoded")
	}
	r.Error = nil
	if _, err := r.Encode(); err != nil {
		t.Error(err)
	}
	r.Status, r.Body = Failed, nil
	if _, err := r.Encode(); err == nil {
		t.Error("failed without an error was encoded")
	}
	r.Error = &Error{Code: "internal", Message: "y"}
	if _, err := r.Encode(); err != nil {
		t.Error(err)
	}
	r.Status = "done"
	if _, err := r.Encode(); err == nil {
		t.Error("a non-status was encoded")
	}
	r.Status, r.Responder.Package = Failed, "nobody"
	if _, err := r.Encode(); err == nil {
		t.Error("a responder of no package was encoded")
	}
}

// The caller's rule: a response is accepted only if the whole document
// parses, follows the schema, echoes the request_id and agrees with the exit
// code. Anything else is unknown.
func TestAcceptResponse(t *testing.T) {
	data := fixture(t, "response-ok.json")
	good, _ := canon.Line(data)
	if r, err := AcceptResponse(good, 0, "req_01arz3ndektsv4rrffq69g5fav"); err != nil || r.Status != Ok {
		t.Fatalf("not accepted: %v", err)
	}
	cases := []struct {
		name string
		out  []byte
		exit int
		id   string
	}{
		{"empty output", nil, 0, "req_01arz3ndektsv4rrffq69g5fav"},
		{"empty output with failing exit", nil, 1, "req_01arz3ndektsv4rrffq69g5fav"},
		{"no final line feed", good[:len(good)-1], 0, "req_01arz3ndektsv4rrffq69g5fav"},
		{"truncated", good[:len(good)/2], 0, "req_01arz3ndektsv4rrffq69g5fav"},
		{"two documents", append(append([]byte{}, good...), good...), 0, "req_01arz3ndektsv4rrffq69g5fav"},
		{"exit code disagrees", good, 1, "req_01arz3ndektsv4rrffq69g5fav"},
		{"signal exit", good, 137, "req_01arz3ndektsv4rrffq69g5fav"},
		{"another request", good, 0, "req_01arz3ndektsv4rrffq69g5fa0"},
		{"null with a status that needs the identity", nullID(t, "failed", 1, "internal"), 1, "req_01arz3ndektsv4rrffq69g5fav"},
		{"null with a refusal", nullID(t, "refused", 4, "home_mismatch"), 4, "req_01arz3ndektsv4rrffq69g5fav"},
		{"null with another invalid code", nullID(t, "invalid", 3, "schema_violation"), 3, "req_01arz3ndektsv4rrffq69g5fav"},
		{"an identity where the caller has none", good, 0, ""},
		{"null failed where the caller has none", nullID(t, "failed", 1, "internal"), 1, ""},
		{"null with a hold", nullHeld(t), 10, "req_01arz3ndektsv4rrffq69g5fav"},
		{"too_large about the response", tooLarge(t, func(e map[string]any) { e["detail"] = map[string]any{"record": "response"} }), 7, "req_01arz3ndektsv4rrffq69g5fav"},
		{"too_large without the detail", tooLarge(t, func(e map[string]any) { delete(e, "detail") }), 7, "req_01arz3ndektsv4rrffq69g5fav"},
		{"too_large within the limit", tooLarge(t, func(e map[string]any) { e["actual_bytes"] = e["limit_bytes"] }), 7, "req_01arz3ndektsv4rrffq69g5fav"},
		{"a capabilities response for a request", capabilitiesResponse(t), 0, "req_01arz3ndektsv4rrffq69g5fav"},
		{"not a response", []byte("ok\n"), 0, "req_01arz3ndektsv4rrffq69g5fav"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := AcceptResponse(c.out, c.exit, c.id)
			if r != nil {
				t.Fatal("a response was returned")
			}
			failure(t, err, Unknown, CodeOutcomeUnknown)
			if AsFailure(err).Err.Retryable {
				t.Error("unknown is retryable")
			}
		})
	}
	// A null request_id is accepted, against the caller's real identity, only
	// with a status the callee reaches before it can read the identity.
	id := "req_01arz3ndektsv4rrffq69g5fav"
	for _, c := range []struct {
		name   string
		out    []byte
		exit   int
		status Status
	}{
		{"a request over the limit", fixture(t, "response-too-large.json"), 7, TooLarge},
		{"usage", nullID(t, "usage", 2, "usage"), 2, Usage},
		{"a request that is no record", nullID(t, "invalid", 3, "malformed_request"), 3, Invalid},
	} {
		r, err := AcceptResponse(c.out, c.exit, id)
		if err != nil || r.Status != c.status || r.RequestID != "" {
			t.Errorf("%s: %v %+v", c.name, err, r)
		}
	}
}

// The capabilities invocation takes no request: its response is accepted by
// its own rule, only with a null request_id and the capabilities operation.
func TestAcceptCapabilities(t *testing.T) {
	good := capabilitiesResponse(t)
	if r, err := AcceptCapabilities(good, 0); err != nil || r.Status != Ok {
		t.Fatalf("not accepted: %v", err)
	}
	other := tree(t, fixture(t, "response-ok.json"))
	other["request_id"] = nil
	withID := tree(t, fixture(t, "response-ok.json"))
	withID["operation"] = CapabilitiesOperation
	for name, c := range map[string]struct {
		out  []byte
		exit int
	}{
		"empty":                    {nil, 0},
		"exit code disagrees":      {good, 1},
		"another operation":        {encode(t, other), 0},
		"an identity":              {encode(t, withID), 0},
		"a response to a request":  {fixture(t, "response-ok.json"), 0},
		"usage that names no call": {nullID(t, "usage", 2, "usage"), 2},
	} {
		r, err := AcceptCapabilities(c.out, c.exit)
		if r != nil {
			t.Errorf("%s: a response was returned", name)
		}
		failure(t, err, Unknown, CodeOutcomeUnknown)
	}
}

// An identity-less call is not a request: AcceptResponse is for requests that
// have one, and never accepts for none.
func TestAcceptResponseNeedsAnIdentity(t *testing.T) {
	for _, out := range [][]byte{capabilitiesResponse(t), nullID(t, "usage", 2, "usage")} {
		if r, err := AcceptResponse(out, 0, ""); r != nil || AsFailure(err) == nil {
			t.Errorf("accepted for no identity: %v", err)
		}
	}
}

// capabilitiesResponse is a response with a null request_id to the
// capabilities operation.
func capabilitiesResponse(t *testing.T) []byte {
	t.Helper()
	m := tree(t, fixture(t, "response-ok.json"))
	m["request_id"], m["operation"] = nil, CapabilitiesOperation
	return encode(t, m)
}

// tooLarge is the too_large response of a request over the limit, edited.
func tooLarge(t *testing.T, edit func(e map[string]any)) []byte {
	t.Helper()
	m := tree(t, fixture(t, "response-too-large.json"))
	edit(m["error"].(map[string]any))
	return encode(t, m)
}

// nullHeld is a held response whose request_id is null.
func nullHeld(t *testing.T) []byte {
	t.Helper()
	m := tree(t, fixture(t, "response-held.json"))
	m["request_id"] = nil
	return encode(t, m)
}

// nullID is a failure response of the status, code and exit code whose
// request_id is null.
func nullID(t *testing.T, status string, exit int64, code string) []byte {
	t.Helper()
	m := tree(t, fixture(t, "response-ok.json"))
	m["request_id"] = nil
	m["status"] = status
	m["exit_code"] = exit
	delete(m, "body")
	m["error"] = map[string]any{"code": code, "message": "the request could not be read", "retryable": false}
	return encode(t, m)
}

func TestReceiptRoundTrip(t *testing.T) {
	m := tree(t, fixture(t, "response-ok.json"))["receipts"].([]any)[0].(map[string]any)
	data := encode(t, m)
	r, err := DecodeReceipt(data)
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, data) {
		t.Errorf("Encode is not the canonical form:\n%s\n%s", out, data)
	}
	if r.Writer.Operation != "provision" || len(r.Evidence) != 1 || r.Evidence[0].Role != "request" || r.Subject.RecordRevision != 1 || r.Level != ScopeHive {
		t.Errorf("decoded wrongly: %+v", r)
	}
	// A receipt with nothing optional and nil lists.
	min := Receipt{ReceiptID: r.ReceiptID, Kind: "pool.item.filed", KindVersion: "1.0", WrittenAt: r.WrittenAt, Writer: r.Writer, RequestID: r.RequestID, CorrelationID: r.CorrelationID,
		Scope: Scope{Level: ScopeHive, HomeID: r.HomeID}, Authority: Authority{Kind: AuthorityNone}}
	out, err = min.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"evidence":[]`) || !strings.Contains(string(out), `"body":{}`) {
		t.Errorf("nil lists not written empty: %s", out)
	}
	m["evidence"] = []any{map[string]any{"role": "Bad", "ref": map[string]any{"schema": "x.y", "id": "1"}}}
	_, err = DecodeReceipt(encode(t, m))
	failure(t, err, Invalid, CodeSchemaViolation)
	_, err = DecodeReceipt([]byte("nope"))
	failure(t, err, Invalid, CodeMalformedRequest)
}

func TestCapabilitiesRoundTrip(t *testing.T) {
	data := fixture(t, "capabilities.json")
	c, err := DecodeCapabilities(tree(t, data))
	if err != nil {
		t.Fatal(err)
	}
	if c.Command != "hive-fleet" || c.Executable != "hive-dispatch" || c.Limits != DefaultLimits() || len(c.Operations) != 2 ||
		c.Operation("provision") == nil || c.Operation("provision").State != Supported || c.Operation("route").Reason != "built at stage 4" || c.Operation("nothing") != nil {
		t.Fatalf("decoded wrongly: %+v", c)
	}
	out, err := c.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if m := tree(t, data); m["schema_version"] != "1.0" {
		t.Fatalf("the fixture is at %v, not the version an older build wrote", m["schema_version"])
	}
	current := tree(t, data)
	current["schema_version"] = CapabilitiesSchemaVersion
	want, err := canon.Encode(current)
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if !bytes.Equal(out, want) {
		t.Errorf("Encode is not the canonical form at %s:\n%s\n%s", CapabilitiesSchemaVersion, out, want)
	}
	for version, ok := range map[string]bool{"1.0": true, "1.1": true, "1.2": false, "2.0": false} {
		m := tree(t, data)
		m["schema_version"] = version
		if _, err := DecodeCapabilities(m); (err == nil) != ok {
			t.Errorf("capabilities at schema_version %s: %v", version, err)
		}
	}
	older := tree(t, data)
	older["operations"].([]any)[0].(map[string]any)["platform"] = []any{"security"}
	if _, err := DecodeCapabilities(older); err == nil {
		t.Error("a platform member was read from a declaration at 1.0")
	}
	older["schema_version"] = CapabilitiesSchemaVersion
	if _, err := DecodeCapabilities(older); err != nil {
		t.Errorf("a platform member was refused at %s: %v", CapabilitiesSchemaVersion, err)
	}
	// The specification version a command reports is not the schema version of
	// the envelope: a declaration of this build's specification, or an older
	// one, is read, a newer one is not, and every envelope carries 1.0.
	for version, ok := range map[string]bool{ContractVersion: true, "1.0": true, "1.4": false, "2.0": false} {
		m := tree(t, data)
		m["contract_version"] = version
		_, err := DecodeCapabilities(m)
		if (err == nil) != ok {
			t.Errorf("contract_version %s: %v", version, err)
		}
	}
	if m := c.Tree(); m["schema_version"] != CapabilitiesSchemaVersion || m["contract_version"] != c.ContractVersion {
		t.Errorf("%v %v", m["schema_version"], m["contract_version"])
	}
	// A declaration with no operations is valid; one with a supported
	// operation that names no schemas or versions is not.
	none := &Capabilities{Command: "hive-park", Package: "dispatch", Executable: "hive-dispatch", ContractVersion: "1.0", ImplementationVersion: "test"}
	if err := none.Check(); err != nil {
		t.Error(err)
	}
	if out, _ := none.Encode(); !strings.Contains(string(out), `"operations":[]`) || !strings.Contains(string(out), `"max_argument_bytes":4096`) {
		t.Errorf("defaults not written: %s", out)
	}
	bad := *none
	bad.Operations = []OperationDecl{{Name: "x", State: Supported, Mode: Unary}}
	if err := bad.Check(); err == nil {
		t.Error("a supported operation without schemas passed")
	}
	bad.Operations = []OperationDecl{{Name: "x", State: Unsupported, Mode: Unary}}
	if err := bad.Check(); err == nil {
		t.Error("an unsupported operation without a reason passed")
	}
	bad.Operations = []OperationDecl{{Name: "x", State: Parked, Reason: "r", Mode: Unary, Executes: []string{"make"}}}
	if err := bad.Check(); err == nil {
		t.Error("an undeclared executable passed")
	}
	bad.Limits = Limits{MaxArgumentBytes: 8192}
	bad.Operations = nil
	if err := bad.Check(); err == nil {
		t.Error("an argument limit above 4096 passed")
	}
	m := tree(t, data)
	m["contract_version"] = "2.0"
	if _, err := DecodeCapabilities(m); err == nil {
		t.Error("a newer contract version was read")
	}
	m["contract_version"] = "1.0"
	m["extra"] = 1
	if _, err := DecodeCapabilities(m); err == nil {
		t.Error("an unknown field was read")
	}
}

func TestValidateBody(t *testing.T) {
	body := map[string]any{"home": map[string]any{"role": "hive", "name": "A home"}}
	if err := ValidateBody(body, "urn:hive:schema:ops:hive-fleet.provision.request:v1"); err != nil {
		t.Error(err)
	}
	body["home"].(map[string]any)["role"] = "nest"
	failure(t, ValidateBody(body, "urn:hive:schema:ops:hive-fleet.provision.request:v1"), Invalid, CodeSchemaViolation)
	failure(t, ValidateBody(body, "urn:hive:schema:nothing:v1"), Failed, CodeInternal)
	if err := Validate(RequestSchemaID, fixture(t, "request.json")); err != nil {
		t.Error(err)
	}
}

func TestLimits(t *testing.T) {
	if DefaultLimits() != (Limits{4096, 67108864, 67108864}) {
		t.Errorf("defaults: %+v", DefaultLimits())
	}
	if got := (Limits{MaxRequestBytes: 10}).Resolved(); got != (Limits{4096, 10, 67108864}) {
		t.Errorf("Resolved: %+v", got)
	}
	if got := (Limits{-1, -1, -1}).Resolved(); got != (Limits{-1, -1, -1}) {
		t.Errorf("Resolved negative: %+v", got)
	}
	caps := (&Capabilities{Command: "hive-pool", Package: "pool", Executable: "hive-pool", ContractVersion: ContractVersion,
		ImplementationVersion: "0.1.0", Limits: Limits{-1, 0, 0}.Resolved(), Operations: []OperationDecl{}})
	if err := caps.Check(); err == nil {
		t.Error("a negative limit reached no validation")
	}
}
