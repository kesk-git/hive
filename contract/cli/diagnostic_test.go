package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/internal/testsupport/access"
	"github.com/kesk-git/hive/contract/journal"
	"github.com/kesk-git/hive/contract/store"
)

// events reads the events the harness's journal holds, in order.
func (h *harness) events(t testing.TB) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, eid := range h.journal.order {
		v, err := canon.Parse(h.journal.lines[eid])
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, v.(map[string]any))
	}
	return out
}

// A failure with a lower-layer chain answers with one sentence and writes
// one line to standard error; the chain goes to the home's journal as one
// diagnostic event of the answering package (spec/events.md, Kinds).
func TestFailureDetailGoesToTheJournalOnly(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	req := request(t, "fail", 7, func(m map[string]any) {
		m["body"].(map[string]any)["home"].(map[string]any)["name"] = "chain"
	})
	code, out, stderr := h.run("hive-fleet", req, "fail", HomeOption, home)
	resp, err := envelope.AcceptResponse(out, code, id(ids.Request, 7))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, resp, envelope.Failed, "internal")
	for _, leak := range []string{chainPath, "no such file", "reading", "open "} {
		if strings.Contains(resp.Error.Message, leak) || strings.Contains(stderr, leak) {
			t.Errorf("%q is in the message %q or standard error %q", leak, resp.Error.Message, stderr)
		}
	}
	if want := "hive-fleet fail: failed internal " + id(ids.Request, 7) + "\n"; stderr != want {
		t.Errorf("standard error = %q, want %q", stderr, want)
	}

	events := h.events(t)
	if len(events) != 1 {
		t.Fatalf("the journal holds %d events, want 1", len(events))
	}
	e := events[0]
	source, _ := e["source"].(map[string]any)
	if e["kind"] != "dispatch.diagnostic.recorded" || e["kind_version"] != "1.0" || source["package"] != "dispatch" || source["source_id"] != "dispatch" {
		t.Errorf("event %v", e)
	}
	if e["correlation_id"] != id(ids.Correlation, 1) || e["home_id"] != homeID || e["scope_level"] != "hive" {
		t.Errorf("event %v", e)
	}
	body, _ := e["body"].(map[string]any)
	if body["request_id"] != id(ids.Request, 7) || body["command"] != "hive-fleet" || body["operation"] != "fail" || body["status"] != "failed" || body["code"] != "internal" {
		t.Errorf("body %v", body)
	}
	detail, _ := body["detail"].([]any)
	if len(detail) == 0 || !strings.Contains(detail[0].(string), chainPath) || !strings.Contains(detail[len(detail)-1].(string), "file does not exist") {
		t.Errorf("detail %v", detail)
	}
	for k := range body {
		switch k {
		case "command", "operation", "status", "code", "request_id", "detail":
		default:
			t.Errorf("the body holds %q", k)
		}
	}
	basis, _ := e["basis"].(map[string]any)
	ref, _ := basis["ref"].(map[string]any)
	if basis["kind"] != "observation" || ref["schema"] != "hive.request" || ref["id"] != id(ids.Request, 7) {
		t.Errorf("basis %v", basis)
	}
}

// A usage refusal names a position and a length, never the argument, and
// leaves no event: its message already says everything.
func TestUsageRefusalQuotesNothingAndWritesNoEvent(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	const typed = "sk-live-0123456789abcdefSECRET"
	code, out, stderr := h.run("hive-fleet", nil, "fail", typed, HomeOption, home)
	resp, err := envelope.AcceptResponse(out, code, id(ids.Request, 1))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, resp, envelope.Usage, "usage")
	if strings.Contains(resp.Error.Message, typed) || strings.Contains(stderr, typed) || strings.Contains(string(out), typed) {
		t.Errorf("the argument is quoted: %q, %q", resp.Error.Message, stderr)
	}
	if want := "hive-fleet fail: usage usage -\n"; stderr != want {
		t.Errorf("standard error = %q, want %q", stderr, want)
	}
	if n := len(h.events(t)); n != 0 {
		t.Errorf("the journal holds %d events", n)
	}
}

// diagnostics are the diagnostic events the harness's journal holds.
func (h *harness) diagnostics(t testing.TB) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, e := range h.events(t) {
		if e["kind"] == "dispatch.diagnostic.recorded" {
			out = append(out, e)
		}
	}
	return out
}

// A panic answers with a sentence that holds no value; its stack goes to the
// event, and nothing of it to standard error.
func TestPanicDetailGoesToTheJournalOnly(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	req := request(t, "fail", 3, func(m map[string]any) {
		m["body"].(map[string]any)["home"].(map[string]any)["name"] = "panic"
	})
	code, out, stderr := h.run("hive-fleet", req, "fail", HomeOption, home)
	resp, err := envelope.AcceptResponse(out, code, id(ids.Request, 3))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, resp, envelope.Failed, "internal")
	if strings.Contains(resp.Error.Message, "boom") || strings.Contains(stderr, "boom") || strings.Contains(stderr, "goroutine") {
		t.Errorf("the panic is in the message %q or standard error %q", resp.Error.Message, stderr)
	}
	if want := "hive-fleet fail: failed internal " + id(ids.Request, 3) + "\n"; stderr != want {
		t.Errorf("standard error = %q, want %q", stderr, want)
	}
	events := h.diagnostics(t)
	if len(events) != 1 {
		t.Fatalf("%d diagnostic events", len(events))
	}
	body, _ := events[0]["body"].(map[string]any)
	if !strings.Contains(body["stack"].(string), "goroutine") || body["detail"] != nil || body["panic_type"] != nil {
		t.Errorf("body %v", body)
	}
	if line := string(h.journal.lines[events[0]["event_id"].(string)]); strings.Contains(line, "boom") {
		t.Errorf("the panic's value is in the event: %s", line)
	}
}

// A request that could not be read has no request_id, no correlation_id and no
// scope; its detail is kept under a correlation_id minted for the event, in the
// hive scope of the home, based on the invocation.
func TestUnreadRequestKeepsItsDetail(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	missing := filepath.Join(t.TempDir(), "no-such-request.json")
	code, out, stderr := h.run("hive-fleet", nil, "echo", HomeOption, home, RequestFileOption, missing)
	resp, err := envelope.DecodeResponse(out)
	if err != nil || resp.ExitCode() != code {
		t.Fatalf("exit %d: %v", code, err)
	}
	expect(t, resp, envelope.Unavailable, "source_unreadable")
	if strings.Contains(resp.Error.Message, missing) || strings.Contains(stderr, missing) {
		t.Errorf("the path is in the message %q or standard error %q", resp.Error.Message, stderr)
	}
	if want := "hive-fleet echo: unavailable source_unreadable -\n"; stderr != want {
		t.Errorf("standard error = %q, want %q", stderr, want)
	}
	events := h.diagnostics(t)
	if len(events) != 1 {
		t.Fatalf("%d diagnostic events", len(events))
	}
	e := events[0]
	corr, _ := e["correlation_id"].(string)
	if ids.Check(corr, ids.Correlation) != nil || corr == id(ids.Correlation, 1) {
		t.Errorf("correlation_id %q", corr)
	}
	basis, _ := e["basis"].(map[string]any)
	ref, _ := basis["ref"].(map[string]any)
	if basis["kind"] != "observation" || ref["schema"] != "hive.invocation" || ref["id"] != corr {
		t.Errorf("basis %v", basis)
	}
	body, _ := e["body"].(map[string]any)
	if _, has := body["request_id"]; has || e["causation"] != nil || e["scope_level"] != "hive" || e["home_id"] != homeID {
		t.Errorf("event %v", e)
	}
	if detail, _ := body["detail"].([]any); len(detail) == 0 || !strings.Contains(detail[0].(string), missing) {
		t.Errorf("detail %v", detail)
	}
}

// Where no event can be written the detail is dropped, and the response and
// its one line of standard error are what they would have been.
func TestDetailIsDroppedWhereNothingCanReceiveIt(t *testing.T) {
	req := request(t, "fail", 7, func(m map[string]any) {
		m["body"].(map[string]any)["home"].(map[string]any)["name"] = "chain"
	})
	want := "hive-fleet fail: failed internal " + id(ids.Request, 7) + "\n"
	for _, c := range []struct {
		name string
		edit func(h *harness)
	}{
		{"no journal is configured", func(h *harness) { h.exe.Journal = nil }},
		{"the journal does not open", func(h *harness) {
			h.exe.Journal = func(string) (store.Journal, error) { return nil, errors.New("the journal is gone") }
		}},
		{"the append fails", func(h *harness) { h.journal.fail = errors.New("the journal is full") }},
		{"the journal does not apply the no-value check", func(h *harness) { h.journal.checks = false }},
		{"the check refuses the body", func(h *harness) { h.journal.dislikes = chainPath }},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			c.edit(h)
			code, out, stderr := h.run("hive-fleet", req, "fail", HomeOption, newHome(t))
			resp, err := envelope.AcceptResponse(out, code, id(ids.Request, 7))
			if err != nil {
				t.Fatal(err)
			}
			expect(t, resp, envelope.Failed, "internal")
			if code != envelope.Failed.ExitCode() || stderr != want {
				t.Errorf("exit %d, standard error %q", code, stderr)
			}
			if n := len(h.events(t)); n != 0 {
				t.Errorf("%d events", n)
			}
		})
	}

	// A directory with no home record, or none at all, has no home_id to
	// write the event under.
	for name, home := range map[string]string{"no home record": access.EmptyHome(t), "no directory": filepath.Join(t.TempDir(), "absent")} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			missing := filepath.Join(t.TempDir(), "no-such-request.json")
			code, out, stderr := h.run("hive-fleet", nil, "echo", HomeOption, home, RequestFileOption, missing)
			if resp, err := envelope.DecodeResponse(out); err != nil || resp.ExitCode() != code {
				t.Fatalf("exit %d: %v", code, err)
			}
			if stderr != "hive-fleet echo: unavailable source_unreadable -\n" || len(h.events(t)) != 0 {
				t.Errorf("standard error %q, %d events", stderr, len(h.events(t)))
			}
		})
	}
}

// An ok response and a replayed one write no diagnostic event, and a failure
// whose message says everything writes none either.
func TestNoEventWithoutDetail(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	put := request(t, "put", 1, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "first" })
	expect(t, h.invoke(t, home, "put", put), envelope.Ok, "")
	again := h.invoke(t, home, "put", put)
	expect(t, again, envelope.Ok, "")
	if !again.Replayed {
		t.Fatal("not a replay")
	}
	refused := request(t, "refuse", 2, nil)
	expect(t, h.invoke(t, home, "refuse", refused), envelope.Refused, "authority_missing")
	expect(t, h.invoke(t, home, "fail", request(t, "fail", 3, func(m map[string]any) {
		m["body"].(map[string]any)["home"].(map[string]any)["name"] = "package code"
	})), envelope.Unavailable, "dispatch.adapter_down")
	if n := len(h.diagnostics(t)); n != 0 {
		t.Errorf("%d diagnostic events", n)
	}
}

// shapeFor is a string of a listed credential shape, built at run time from
// its prefix and a repeated character so that no file holds one.
func shapeFor(prefix string, n int) string { return prefix + strings.Repeat("a", n) }

// The journal as built applies the no-value check, so through it the detail of
// a failure is kept: one diagnostic event of the answering package for each
// failure, with the response and its one line of standard error as they were.
func TestLandedJournalKeepsDetail(t *testing.T) {
	h := newHarness(t)
	h.exe.Journal = journal.Opener("dispatch")
	home := newHome(t)
	for n, name := range []string{"chain", "panic"} {
		req := request(t, "fail", n+1, func(m map[string]any) {
			m["body"].(map[string]any)["home"].(map[string]any)["name"] = name
		})
		code, out, stderr := h.run("hive-fleet", req, "fail", HomeOption, home)
		resp, err := envelope.AcceptResponse(out, code, id(ids.Request, n+1))
		if err != nil {
			t.Fatal(err)
		}
		expect(t, resp, envelope.Failed, "internal")
		if want := "hive-fleet fail: failed internal " + id(ids.Request, n+1) + "\n"; stderr != want {
			t.Errorf("standard error = %q, want %q", stderr, want)
		}
	}
	entries := realEntries(t, home)
	if len(entries) != 2 {
		t.Fatalf("the journal holds %d events, want 2", len(entries))
	}
	for n, e := range entries {
		body, _ := e.Event["body"].(map[string]any)
		if e.Kind != "dispatch.diagnostic.recorded" || body["request_id"] != id(ids.Request, n+1) || e.Event["correlation_id"] != id(ids.Correlation, 1) {
			t.Errorf("event %d: %v", n+1, e.Event)
		}
	}
}

// realEntries reads what the home's own journal holds.
func realEntries(t testing.TB, home string) []*journal.Entry {
	t.Helper()
	j, err := journal.Open(home, journal.Options{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := j.Reader(0)
	if err != nil {
		t.Fatal(err)
	}
	var out []*journal.Entry
	for {
		e, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		if e == nil {
			return out
		}
		out = append(out, e)
	}
}

// shaping adds to hive-fleet an operation of the name given that fails with an
// error chain holding the text given.
func (h *harness) shaping(name, text string) {
	fleet := h.exe.command("hive-fleet")
	decl := fleet.Operations[0].OperationDecl
	decl.Name, decl.Mutates = name, false
	fleet.Operations = append(fleet.Operations, &Operation{OperationDecl: decl, Handle: func(c *Call) (*Result, error) {
		return nil, fmt.Errorf("the vendor refused %s: %w", text, fs.ErrNotExist)
	}})
}

// A failure whose detail holds a string of a listed shape keeps no diagnostic
// at all: the whole event is dropped, with the response, its status and its
// line of standard error as they would have been, and a failure beside it
// that only mentions a prefix is kept.
func TestDetailHoldingACredentialShapeIsDroppedWhole(t *testing.T) {
	text := shapeFor("ghp_", 36)
	h := newHarness(t)
	h.exe.Journal = journal.Opener("dispatch")
	h.shaping("shaped", text)
	h.shaping("mention", "a token that begins ghp_ and goes on")
	home := newHome(t)

	code, out, stderr := h.run("hive-fleet", request(t, "shaped", 1, nil), "shaped", HomeOption, home)
	resp, err := envelope.AcceptResponse(out, code, id(ids.Request, 1))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, resp, envelope.Failed, "internal")
	if want := "hive-fleet shaped: failed internal " + id(ids.Request, 1) + "\n"; stderr != want {
		t.Errorf("standard error = %q, want %q", stderr, want)
	}
	if n := len(realEntries(t, home)); n != 0 {
		t.Fatalf("the journal holds %d events", n)
	}

	expect(t, h.invoke(t, home, "mention", request(t, "mention", 2, nil)), envelope.Failed, "internal")
	entries := realEntries(t, home)
	if len(entries) != 1 || strings.Contains(string(entries[0].Line), text) {
		t.Fatalf("the journal holds %d events", len(entries))
	}
}

// A request whose body holds a string of a listed shape is refused whole as
// invalid, before anything is read or written: the refusal names the field and
// the shape and never the text, which reaches neither the response, standard
// error nor the journal. A sentence that mentions the prefix passes.
func TestRequestHoldingACredentialShapeIsRefusedWhole(t *testing.T) {
	text := shapeFor("ghp_", 36)
	h := newHarness(t)
	home := newHome(t)
	before := tree(t, home)

	req := request(t, "put", 1, func(m map[string]any) {
		m["body"].(map[string]any)["home"].(map[string]any)["name"] = text
	})
	code, out, stderr := h.run("hive-fleet", req, "put", HomeOption, home)
	resp, err := envelope.AcceptResponse(out, code, id(ids.Request, 1))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, resp, envelope.Invalid, "secret_shape_recognized")
	if !strings.Contains(resp.Error.Message, "/home/name") || !strings.Contains(resp.Error.Message, "github.pat-classic") {
		t.Errorf("the message %q names neither the field nor the shape", resp.Error.Message)
	}
	saw := string(out) + stderr
	for _, e := range h.events(t) {
		saw += string(h.journal.lines[e["event_id"].(string)])
	}
	if strings.Contains(saw, text) {
		t.Error("the text of the credential is in the response, standard error or the journal")
	}
	if h.calls["put"] != 0 || !maps.Equal(before, tree(t, home)) {
		t.Errorf("the operation ran %d times or the home changed", h.calls["put"])
	}

	mention := request(t, "put", 2, func(m map[string]any) {
		m["body"].(map[string]any)["home"].(map[string]any)["name"] = "ghp_"
	})
	expect(t, h.invoke(t, home, "put", mention), envelope.Ok, "")
}

// A journal that applies the no-value check keeps the event; one that refuses
// the body drops it whole, with the response standing.
func TestRefusedDetailIsDroppedWhole(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	h.journal.dislikes = chainPath
	req := request(t, "fail", 7, func(m map[string]any) {
		m["body"].(map[string]any)["home"].(map[string]any)["name"] = "chain"
	})
	expect(t, h.invoke(t, home, "fail", req), envelope.Failed, "internal")
	if n := len(h.diagnostics(t)); n != 0 {
		t.Fatalf("%d diagnostic events", n)
	}
	h.journal.dislikes = ""
	expect(t, h.invoke(t, home, "fail", req), envelope.Failed, "internal")
	if n := len(h.diagnostics(t)); n != 1 {
		t.Errorf("%d diagnostic events", n)
	}
}

// A failure that replaces another keeps the reason of the one it replaces: the
// effect that ends unavailable after its intent is answered unknown, and the
// event holds its code and message.
func TestReplacedFailureKeepsItsReason(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	expect(t, h.invoke(t, home, "effect", effectRequest(t, 1, "unavailable")), envelope.Unknown, "outcome_unknown")
	events := h.diagnostics(t)
	if len(events) != 1 {
		t.Fatalf("%d diagnostic events", len(events))
	}
	body, _ := events[0]["body"].(map[string]any)
	detail, _ := body["detail"].([]any)
	if len(detail) != 1 || detail[0] != "dependency_missing: the program is gone" || body["code"] != "outcome_unknown" {
		t.Errorf("body %v", body)
	}
}

// A request that breaks its schema is answered with one sentence naming where,
// never with a member's name or a value of the request, and its detail goes to
// the journal.
func TestSchemaViolationQuotesNothingOfTheRequest(t *testing.T) {
	const typed = "sk-live-0123456789abcdefSECRET"
	for name, edit := range map[string]func(m map[string]any){
		"envelope member": func(m map[string]any) { m[typed] = "x" },
		"body member":     func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)[typed] = "x" },
		"body value":      func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["role"] = typed },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			home := newHome(t)
			code, out, stderr := h.run("hive-fleet", request(t, "fail", 5, edit), "fail", HomeOption, home)
			resp, err := envelope.AcceptResponse(out, code, id(ids.Request, 5))
			if err != nil {
				t.Fatal(err)
			}
			expect(t, resp, envelope.Invalid, "schema_violation")
			if strings.Contains(string(out), typed) || strings.Contains(stderr, typed) || strings.Contains(resp.Error.Message, "\n") {
				t.Errorf("message %q, standard error %q", resp.Error.Message, stderr)
			}
		})
	}
}

// No message and no line of standard error quotes what the caller typed: not
// the command name, the operation name, an argument or an option.
func TestMessagesQuoteNothingTyped(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	const typed = "sk-live-0123456789abcdefSECRET"
	cases := []struct {
		name    string
		command string
		args    []string
		message string
	}{
		{"command name", "hive-" + typed, []string{"echo", HomeOption, home}, "does not own the command name it was started under, of 35 bytes"},
		{"operation of no form", "hive-fleet", []string{"Fly" + typed, HomeOption, home}, "argument 1, of 33 bytes, is not an operation name"},
		{"operation not declared", "hive-fleet", []string{"fly-" + strings.ToLower(typed), HomeOption, home}, "has no operation of the name in argument 1, of 34 bytes"},
		{"unexpected argument", "hive-fleet", []string{"echo", HomeOption, home, typed}, "argument 4 is not an option; the request is never an argument"},
		{"unknown option", "hive-fleet", []string{"echo", HomeOption, home, "--" + typed, "x"}, "argument 4, of 32 bytes, is not an option this command takes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, stderr := h.run(c.command, nil, c.args...)
			resp, err := envelope.DecodeResponse(out)
			if err != nil || resp.ExitCode() != code || resp.Error == nil {
				t.Fatalf("exit %d: %v", code, err)
			}
			if !strings.Contains(resp.Error.Message, c.message) {
				t.Errorf("message %q lacks %q", resp.Error.Message, c.message)
			}
			if strings.Contains(strings.ToLower(resp.Error.Message), strings.ToLower(typed)) || strings.Contains(strings.ToLower(string(out)+stderr), strings.ToLower(typed)) {
				t.Errorf("the typed text is in the response %s or standard error %q", out, stderr)
			}
			if c.name == "operation not declared" && resp.Operation != "" {
				t.Errorf("the response names the operation %q", resp.Operation)
			}
			if strings.Count(stderr, "\n") != 1 || strings.Contains(stderr, resp.Error.Message) {
				t.Errorf("standard error %q is not one line without the message", stderr)
			}
		})
	}
}

// A failure that replaces the one an operation returned with receipts its
// status leaves none of keeps that failure's reason.
func TestReceiptStatusReplacementKeepsTheReason(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	expect(t, h.invoke(t, home, "effect", effectRequest(t, 1, "unknown with a receipt")), envelope.Unknown, "outcome_unknown")
	events := h.diagnostics(t)
	if len(events) != 1 {
		t.Fatalf("%d diagnostic events", len(events))
	}
	detail, _ := events[0]["body"].(map[string]any)["detail"].([]any)
	if len(detail) != 1 || detail[0] != "outcome_unknown: the adapter did not answer" {
		t.Errorf("detail %v", detail)
	}
}

// A replay writes nothing: a recorded refusal answered unknown because it no
// longer fits leaves no diagnostic event, however often it is sent.
func TestReplayThatNoLongerFitsWritesNoEvent(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	fleet := h.exe.command("hive-fleet")
	req := request(t, "refuse", 1, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "record it" })
	expect(t, h.invoke(t, home, "refuse", req), envelope.Refused, "authority_missing")
	fleet.Limits.MaxResponseBytes = floorOf(t, h.exe, fleet)
	for range 2 {
		expectUnknown(t, h.invoke(t, home, "refuse", req))
	}
	if n := len(h.diagnostics(t)); n != 0 {
		t.Errorf("%d diagnostic events", n)
	}
}

// A request that cannot be read whole but whose request_id can is the event's
// request: its request_id, its causation and its basis, under a correlation_id
// minted for the event in the hive scope, since neither was read.
func TestUnreadRequestWithARequestIDIsTheEventsRequest(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	req := request(t, "fail", 5, func(m map[string]any) {
		m["extra"] = "x"
		m["scope_level"] = "workspace"
		m["workspace_id"] = id(ids.Workspace, 1)
	})
	expect(t, h.invoke(t, home, "fail", req), envelope.Invalid, "schema_violation")
	events := h.diagnostics(t)
	if len(events) != 1 {
		t.Fatalf("%d diagnostic events", len(events))
	}
	e := events[0]
	body, _ := e["body"].(map[string]any)
	if body["request_id"] != id(ids.Request, 5) {
		t.Errorf("body %v", body)
	}
	ref := map[string]any{"schema": "hive.request", "id": id(ids.Request, 5)}
	causation, _ := e["causation"].(map[string]any)
	basis, _ := e["basis"].(map[string]any)
	if !maps.Equal(causation, ref) || basis["kind"] != "observation" || !maps.Equal(basis["ref"].(map[string]any), ref) {
		t.Errorf("causation %v, basis %v", causation, basis)
	}
	corr, _ := e["correlation_id"].(string)
	if ids.Check(corr, ids.Correlation) != nil || corr == id(ids.Correlation, 1) || e["scope_level"] != "hive" || e["workspace_id"] != nil {
		t.Errorf("event %v", e)
	}
}

// A response that cannot be delivered gives no response and exactly one line
// of standard error, written once that is known: failed with io_failure, and
// no text of the file system.
func TestUndeliveredResponseGivesOneLine(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	nowhere := filepath.Join(t.TempDir(), "absent", "response.json")
	missing := filepath.Join(t.TempDir(), "no-such-request.json")
	for name, c := range map[string]struct {
		stdin []byte
		args  []string
		want  string
	}{
		"a failure": {nil, []string{"echo", HomeOption, home, RequestFileOption, missing, ResponseFileOption, nowhere}, "hive-fleet echo: failed io_failure -\n"},
		"an answer": {request(t, "echo", 4, nil), []string{"echo", HomeOption, home, ResponseFileOption, nowhere}, "hive-fleet echo: failed io_failure " + id(ids.Request, 4) + "\n"},
	} {
		t.Run(name, func(t *testing.T) {
			code, out, stderr := h.run("hive-fleet", c.stdin, c.args...)
			if code != envelope.Failed.ExitCode() || len(out) != 0 || stderr != c.want {
				t.Errorf("exit %d, stdout %q, standard error %q, want %q", code, out, stderr, c.want)
			}
		})
	}
}
