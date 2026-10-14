package cli

import (
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
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
