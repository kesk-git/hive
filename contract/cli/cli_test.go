package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/store"
)

// A test executable describes itself correctly, and Check refuses one that
// does not.
func TestCheck(t *testing.T) {
	h := newHarness(t)
	if err := h.exe.Check(); err != nil {
		t.Fatal(err)
	}
	bad := func(name string, edit func(e *Executable)) {
		t.Run(name, func(t *testing.T) {
			e := *newHarness(t).exe
			e.Commands = append([]*Command(nil), e.Commands...)
			for i, c := range e.Commands {
				cc := *c
				cc.Operations = append([]*Operation(nil), c.Operations...)
				e.Commands[i] = &cc
			}
			edit(&e)
			if err := e.Check(); err == nil {
				t.Error("Check passed")
			}
			// Main answers failed, with nothing on standard output.
			var stdout, stderr bytes.Buffer
			if code := e.Main([]string{"hive-fleet", "capabilities"}, nil, &stdout, &stderr); code != 1 || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Errorf("Main: exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
			}
		})
	}
	bad("not a command name", func(e *Executable) { e.Name = "dispatch" })
	bad("own name not owned", func(e *Executable) { e.Name = "hive-other" })
	bad("empty version", func(e *Executable) { e.Version = "" })
	bad("command twice", func(e *Executable) { e.Commands = append(e.Commands, e.Commands[1]) })
	bad("operation named capabilities", func(e *Executable) {
		e.Commands[1].Operations = append(e.Commands[1].Operations, &Operation{OperationDecl: envelope.OperationDecl{Name: "capabilities"}})
	})
	bad("operation twice", func(e *Executable) {
		e.Commands[1].Operations = append(e.Commands[1].Operations, e.Commands[1].Operations[0])
	})
	bad("supported without handler", func(e *Executable) {
		op := *e.Commands[1].Operations[0]
		op.Name, op.Handle = "other", nil
		e.Commands[1].Operations = append(e.Commands[1].Operations, &op)
	})
	bad("unsupported without reason", func(e *Executable) {
		e.Commands[1].Operations = append(e.Commands[1].Operations, &Operation{OperationDecl: envelope.OperationDecl{Name: "x", State: envelope.Unsupported, Mode: envelope.Unary}})
	})
	bad("package not a package", func(e *Executable) { e.Package = "contract" })
	bad("version over a hundred characters", func(e *Executable) { e.Version = strings.Repeat("é", 101) })
	bad("response limit under the answer of an interrupted transaction", func(e *Executable) { e.Commands[1].Limits.MaxResponseBytes = 100 })
	bad("storeless with a mutating operation", func(e *Executable) { e.Storeless = true })
	// The limits are characters: a hundred two-byte ones are not too many.
	e := *h.exe
	e.Version = strings.Repeat("é", 100)
	if err := e.Check(); err != nil {
		t.Error(err)
	}
}

// --version prints one line and exits 0; capabilities answers with a valid
// declaration and a null request_id; a command whose only operation is
// capabilities declares an empty list.
func TestVersionAndCapabilities(t *testing.T) {
	h := newHarness(t)
	code, out, errs := h.run("hive-fleet", nil, "--version")
	if code != 0 || string(out) != "0.1.0-test\n" || errs != "" {
		t.Fatalf("--version: %d %q %q", code, out, errs)
	}
	for _, command := range []string{"hive-fleet", "hive-park", "hive-dispatch"} {
		code, out, errs := h.run(command, nil, "capabilities")
		resp, err := envelope.AcceptCapabilities(out, code)
		if err != nil || errs != "" {
			t.Fatalf("%s capabilities: %v %q", command, err, errs)
		}
		expect(t, resp, envelope.Ok, "")
		if resp.RequestID != "" || resp.Operation != "capabilities" || resp.Command != command || resp.Replayed || len(resp.Receipts) != 0 {
			t.Errorf("%s: %+v", command, resp)
		}
		caps, err := envelope.DecodeCapabilities(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if caps.Command != command || caps.Package != "dispatch" || caps.Executable != "hive-dispatch" || caps.ImplementationVersion != "0.1.0-test" || caps.ContractVersion != envelope.ContractVersion || caps.SourceRevision != strings.Repeat("ab", 20) {
			t.Errorf("%s: %+v", command, caps)
		}
		switch command {
		case "hive-fleet":
			if caps.Limits != (envelope.Limits{MaxArgumentBytes: 4096, MaxRequestBytes: 4096, MaxResponseBytes: 4096}) || len(caps.Operations) != len(h.exe.command(command).Operations) || caps.Operation("route").State != envelope.Unsupported || caps.Operation("recover").Reason == "" || caps.Operation("serve").Mode != envelope.Resident || !caps.Operation("put").Mutates {
				t.Errorf("hive-fleet: %+v", caps)
			}
		default:
			if caps.Limits != envelope.DefaultLimits() || len(caps.Operations) != 0 {
				t.Errorf("%s: %+v", command, caps)
			}
		}
	}
	// The response is the executable's own Capabilities, exactly.
	if _, err := h.exe.Capabilities(h.exe.command("hive-fleet")).Encode(); err != nil {
		t.Error(err)
	}
}

// Everything wrong with an invocation itself exits 2 with a usage response,
// before anything is read: the command name, the operation, the options
// and the length of an argument.
func TestUsage(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	before := tree(t, home)
	long := strings.Repeat("x", 4097)
	cases := []struct {
		name      string
		command   string
		args      []string
		code      string
		operation string
	}{
		{"unknown command name", "hive-nest", []string{"echo", "--home", home}, "usage", ""},
		{"no arguments", "hive-fleet", nil, "usage", ""},
		{"unknown operation", "hive-fleet", []string{"fly", "--home", home}, "unknown_operation", "fly"},
		{"operation of no form", "hive-fleet", []string{"Fly!", "--home", home}, "unknown_operation", ""},
		{"capabilities of a command without the operation", "hive-park", []string{"echo", "--home", home}, "unknown_operation", "echo"},
		{"missing home", "hive-fleet", []string{"echo"}, "usage", "echo"},
		{"home without a value", "hive-fleet", []string{"echo", "--home"}, "usage", "echo"},
		{"home given twice", "hive-fleet", []string{"echo", "--home", home, "--home", home}, "usage", "echo"},
		{"unknown option", "hive-fleet", []string{"echo", "--home", home, "--verbose"}, "usage", "echo"},
		{"request as an argument", "hive-fleet", []string{"echo", "--home", home, "{}"}, "usage", "echo"},
		{"argument too long", "hive-fleet", []string{"echo", "--home", long}, "argument_too_large", ""},
		{"long argument before an unknown operation", "hive-fleet", []string{long}, "argument_too_large", ""},
		{"version with more", "hive-fleet", []string{"--version", "now"}, "usage", ""},
		{"capabilities with a home", "hive-fleet", []string{"capabilities", "--home", home}, "usage", "capabilities"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stdin := &failingReader{}
			var stdout, stderr bytes.Buffer
			code := h.exe.Main(append([]string{c.command}, c.args...), stdin, &stdout, &stderr)
			resp, err := envelope.AcceptResponse(stdout.Bytes(), code, id(ids.Request, 1))
			if err != nil {
				t.Fatalf("not accepted: %v\n%s", err, stdout.String())
			}
			expect(t, resp, envelope.Usage, c.code)
			if resp.Operation != c.operation {
				t.Errorf("operation %q, want %q", resp.Operation, c.operation)
			}
			if stdin.read {
				t.Error("standard input was read")
			}
			if stderr.Len() == 0 {
				t.Error("no diagnostic on standard error")
			}
			if c.code == "argument_too_large" && (resp.Error.Detail["limit_bytes"] != int64(4096) || resp.Error.Detail["actual_bytes"] != int64(4097)) {
				t.Errorf("detail: %v", resp.Error.Detail)
			}
		})
	}
	same(t, before, tree(t, home))
	if len(h.calls) != 0 {
		t.Errorf("handlers ran: %v", h.calls)
	}
}

// failingReader fails every read, and remembers that it was asked.
type failingReader struct{ read bool }

func (r *failingReader) Read([]byte) (int, error) {
	r.read = true
	return 0, errors.New("standard input was read")
}

// A request that is read and rejected exits 3, echoes its request_id where
// one can be read, and changes nothing.
func TestInvalid(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	before := tree(t, home)
	good := request(t, "echo", 1, nil)
	cases := []struct {
		name string
		req  []byte
		code string
		id   bool
	}{
		{"empty", nil, "malformed_request", false},
		{"not json", []byte("hello\n"), "malformed_request", false},
		{"truncated", good[:len(good)-10], "malformed_request", false},
		{"two documents", append(append([]byte{}, good...), good...), "malformed_request", false},
		{"not an object", []byte("[]\n"), "malformed_request", false},
		{"newer envelope", request(t, "echo", 1, func(m map[string]any) { m["schema_version"] = "1.1" }), "unsupported_version", true},
		{"unknown field", request(t, "echo", 1, func(m map[string]any) { m["extra"] = 1 }), "schema_violation", true},
		{"missing field", request(t, "echo", 1, func(m map[string]any) { delete(m, "caller") }), "schema_violation", true},
		{"request_id of another kind", request(t, "echo", 1, func(m map[string]any) { m["request_id"] = workspaceID }), "malformed_request", false},
		{"another command", request(t, "echo", 1, func(m map[string]any) { m["command"] = "hive-pool" }), "envelope_mismatch", true},
		{"another operation", request(t, "put", 1, nil), "envelope_mismatch", true},
		{"body version not accepted", request(t, "echo", 1, func(m map[string]any) { m["operation_version"] = "2.0" }), "unsupported_version", true},
		{"body breaks its schema", request(t, "echo", 1, func(m map[string]any) {
			m["body"] = map[string]any{"home": map[string]any{"role": "nest", "name": "x"}}
		}), "schema_violation", true},
		{"body with an unknown field", request(t, "echo", 1, func(m map[string]any) { m["body"].(map[string]any)["extra"] = 1 }), "schema_violation", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := h.invoke(t, home, "echo", c.req)
			expect(t, resp, envelope.Invalid, c.code)
			if (resp.RequestID != "") != c.id {
				t.Errorf("request_id %q", resp.RequestID)
			}
			if resp.Operation != "echo" || resp.Command != "hive-fleet" {
				t.Errorf("%s %s", resp.Command, resp.Operation)
			}
		})
	}
	// An older body version the operation accepts passes.
	resp := h.invoke(t, home, "echo", request(t, "echo", 1, func(m map[string]any) { m["operation_version"] = "1.1" }))
	expect(t, resp, envelope.Ok, "")
	same(t, before, tree(t, home))
	if h.calls["echo"] != 1 {
		t.Errorf("echo ran %d times", h.calls["echo"])
	}
}

// A well-formed request the contract forbids exits 4: the operation's
// state, the home's identity and place, the scope, and the operation's own
// refusal, which may leave a receipt.
func TestRefused(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	before := tree(t, home)
	inRepo := filepath.Join(t.TempDir(), "repo", "home")
	if err := os.MkdirAll(inRepo, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inRepo, "..", ".git"), []byte("gitdir: elsewhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record(t, inRepo, "home.json", homeRecord(homeID))

	project := func(m map[string]any) {
		m["scope_level"] = "project"
		m["workspace_id"] = workspaceID
		m["project_id"] = projectID
		m["clone_id"] = cloneID
	}
	other := newHome(t)
	record(t, other, "dispatch/registry/workspaces/"+workspaceID+".json", workspaceRecord(workspaceID, id(ids.Home, 5), "retired"))
	crossed := newHome(t)
	record(t, crossed, "dispatch/registry/projects/"+projectID+".json", projectRecord(projectID, id(ids.Workspace, 2), homeID, "active"))
	strayed := newHome(t)
	record(t, strayed, "dispatch/registry/clones/"+cloneID+".json", cloneRecord(cloneID, id(ids.Project, 2), workspaceID, homeID, "active"))
	// A workspace the Hive's home holds as a parent is in scope there.
	parent := newHome(t)
	record(t, parent, "dispatch/registry/workspaces/"+workspaceID+".json", func() map[string]any {
		m := workspaceRecord(workspaceID, id(ids.Home, 5), "active")
		m["parent_home_id"] = homeID
		return m
	}())

	cases := []struct {
		name      string
		home      string
		operation string
		req       []byte
		code      string
	}{
		{"unsupported", home, "route", request(t, "route", 1, nil), "unsupported_operation"},
		{"parked", home, "recover", request(t, "recover", 1, nil), "operation_parked"},
		{"another home", home, "echo", request(t, "echo", 1, func(m map[string]any) { m["home_id"] = id(ids.Home, 2) }), "home_mismatch"},
		{"home inside a repository", inRepo, "echo", request(t, "echo", 1, nil), "home_inside_repository"},
		{"workspace not registered", home, "echo", request(t, "echo", 1, func(m map[string]any) { m["scope_level"] = "workspace"; m["workspace_id"] = id(ids.Workspace, 2) }), "scope_mismatch"},
		{"workspace retired and elsewhere", other, "echo", request(t, "echo", 1, func(m map[string]any) { m["scope_level"] = "workspace"; m["workspace_id"] = workspaceID }), "scope_mismatch"},
		{"project not registered", home, "echo", request(t, "echo", 1, func(m map[string]any) { project(m); m["project_id"] = id(ids.Project, 2); delete(m, "clone_id") }), "scope_mismatch"},
		{"project of another workspace", crossed, "echo", request(t, "echo", 1, func(m map[string]any) { project(m); delete(m, "clone_id") }), "scope_mismatch"},
		{"clone not registered", home, "echo", request(t, "echo", 1, func(m map[string]any) { project(m); m["clone_id"] = id(ids.Clone, 2) }), "scope_mismatch"},
		{"clone of another project", strayed, "echo", request(t, "echo", 1, project), "scope_mismatch"},
		{"refusal of the operation", home, "refuse", request(t, "refuse", 1, nil), "authority_missing"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := h.invoke(t, c.home, c.operation, c.req)
			expect(t, resp, envelope.Refused, c.code)
			if resp.RequestID != id(ids.Request, 1) || len(resp.Receipts) != 0 || resp.Error.Retryable {
				t.Errorf("%+v", resp)
			}
		})
	}
	// Refusals that leave nothing leave nothing but the lock file a
	// mutating operation takes: no outcome, no receipt.
	after := tree(t, home)
	delete(after, "locks/dispatch.lock")
	same(t, before, after)
	for _, p := range []string{"dispatch/requests", "dispatch/receipts"} {
		if _, err := os.Stat(filepath.Join(home, p)); err == nil {
			t.Errorf("%s was created", p)
		}
	}
	// The scopes above are in scope where they belong.
	for _, c := range []struct {
		home string
		edit func(m map[string]any)
	}{
		{home, func(m map[string]any) { m["scope_level"] = "workspace"; m["workspace_id"] = workspaceID }},
		{home, project},
		{home, func(m map[string]any) { project(m); delete(m, "clone_id") }},
		{parent, func(m map[string]any) { m["scope_level"] = "workspace"; m["workspace_id"] = workspaceID }},
	} {
		expect(t, h.invoke(t, c.home, "echo", request(t, "echo", 1, c.edit)), envelope.Ok, "")
	}

	// A refusal that leaves a record returns its receipt, records the
	// outcome, and is replayed from it.
	req := request(t, "refuse", 2, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "record it" })
	resp := h.invoke(t, home, "refuse", req)
	expect(t, resp, envelope.Refused, "authority_missing")
	if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "dispatch.thing.refused" || resp.Receipts[0].RequestID != id(ids.Request, 2) || resp.Replayed {
		t.Fatalf("%+v", resp)
	}
	rc := resp.Receipts[0]
	written, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(store.ReceiptPath("dispatch", rc.ReceiptID))))
	if err != nil {
		t.Fatal(err)
	}
	if enc, _ := rc.Encode(); !bytes.Equal(written, enc) {
		t.Error("the receipt on disk is not the one returned")
	}
	if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(store.OutcomePath("dispatch", id(ids.Request, 2))))); err != nil {
		t.Error("the outcome was not recorded")
	}
	recorded := tree(t, home)
	again := h.invoke(t, home, "refuse", req)
	expect(t, again, envelope.Refused, "authority_missing")
	if !again.Replayed || len(again.Receipts) != 1 || again.Receipts[0].ReceiptID != rc.ReceiptID || h.calls["refuse"] != 2 {
		t.Errorf("replay: %+v, calls %d", again, h.calls["refuse"])
	}
	same(t, recorded, tree(t, home))

	// An unknown outcome returns no receipt and writes nothing: this build
	// makes none durable before an outside effect, so a handler that names one
	// is at fault, and the caller is never handed a receipt that is not on disk.
	bare := h.invoke(t, home, "refuse", request(t, "refuse", 3, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "unknown bare" }))
	expect(t, bare, envelope.Unknown, "outcome_unknown")
	if bare.Replayed || len(bare.Receipts) != 0 {
		t.Errorf("%+v", bare)
	}
	same(t, recorded, tree(t, home))
	for _, name := range []string{"unknown", "conflict"} {
		fault := h.invoke(t, home, "refuse", request(t, "refuse", 3, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = name }))
		expect(t, fault, envelope.Failed, "internal")
		if len(fault.Receipts) != 0 {
			t.Errorf("%s: %+v", name, fault)
		}
		same(t, recorded, tree(t, home))
	}
	// A message is counted in characters, as the schema counts them: one of
	// 1,500 two-byte characters is whole, and a longer one is cut on a
	// character boundary at 2,000.
	if got := clip(strings.Repeat("é", 2500), "x"); got != strings.Repeat("é", 2000) {
		t.Errorf("clip: %d bytes", len(got))
	}
	if got := clip("", "refused"); got != "refused" {
		t.Errorf("clip: %q", got)
	}
	wide := h.invoke(t, home, "refuse", request(t, "refuse", 4, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "wide" }))
	expect(t, wide, envelope.Refused, "authority_missing")
	if wide.Error.Message != strings.Repeat("é", 1500) {
		t.Errorf("a message of %d bytes", len(wide.Error.Message))
	}
	if _, err := h.exe.Handle(Invocation{}); err == nil {
		t.Error("Handle without a command passed")
	}
}

// A read-only operation answers from the home and records nothing; every
// invocation runs it.
func TestReadOnly(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	if err := os.Mkdir(filepath.Join(home, "stuff"), 0o700); err != nil {
		t.Fatal(err)
	}
	before := tree(t, home)
	req := request(t, "echo", 1, nil)
	for i := 1; i <= 2; i++ {
		resp := h.invoke(t, home, "echo", req)
		expect(t, resp, envelope.Ok, "")
		if resp.Replayed || resp.RequestID != id(ids.Request, 1) || resp.Command != "hive-fleet" || resp.Operation != "echo" || resp.CompletedAt != "2026-10-11T08:00:00.000Z" ||
			resp.Responder != (envelope.Responder{Package: "dispatch", Command: "hive-fleet", ImplementationVersion: "0.1.0-test", ContractVersion: envelope.ContractVersion}) || len(resp.Receipts) != 0 {
			t.Errorf("%+v", resp)
		}
		if resp.Body["scope"] != "hive" || resp.Body["home"] != homeID || resp.Body["echo"].(map[string]any)["home"].(map[string]any)["name"] != "A home" {
			t.Errorf("body: %v", resp.Body)
		}
		// The entry the home holds that the specification does not name is
		// reported, not touched.
		if u, _ := resp.Body["unknown"].([]any); len(u) != 1 || u[0] != "stuff" {
			t.Errorf("unknown entries: %v", resp.Body["unknown"])
		}
		if h.calls["echo"] != i {
			t.Errorf("echo ran %d times, want %d", h.calls["echo"], i)
		}
	}
	same(t, before, tree(t, home))
}

// A mutating operation writes its records, receipts and events with the
// recorded outcome in one transaction, and is replayed from that outcome:
// the same request again returns the recorded response and writes nothing;
// the same request_id with other content is a conflict.
func TestMutatingAndReplay(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	req := request(t, "put", 1, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "first" })
	resp := h.invoke(t, home, "put", req)
	expect(t, resp, envelope.Ok, "")
	if resp.Body["wrote"] != "first" || len(resp.Receipts) != 1 || resp.Replayed {
		t.Fatalf("%+v", resp)
	}
	rc := resp.Receipts[0]
	if rc.Kind != "dispatch.thing.put" || rc.Writer != (envelope.Writer{Package: "dispatch", Command: "hive-fleet", Operation: "put", ImplementationVersion: "0.1.0-test"}) || rc.RequestID != id(ids.Request, 1) ||
		rc.CorrelationID != id(ids.Correlation, 1) || rc.Level != "hive" || rc.HomeID != homeID || rc.Authority.Kind != "none" || rc.Subject.ID != "first" || rc.WrittenAt != "2026-10-11T08:00:00.000Z" {
		t.Errorf("receipt: %+v", rc)
	}
	for _, p := range []string{store.RecordPath("dispatch", "thing", "first"), store.ReceiptPath("dispatch", rc.ReceiptID), store.OutcomePath("dispatch", id(ids.Request, 1))} {
		if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(p))); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "dispatch", "tx", id(ids.Request, 1)+".json")); err == nil {
		t.Error("the intent was left behind")
	}
	if len(h.journal.order) != 1 {
		t.Errorf("events appended: %v", h.journal.order)
	}
	// The outcome holds the request's digest and the response as returned.
	outcome, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(store.OutcomePath("dispatch", id(ids.Request, 1)))))
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := envelope.Digest(req)
	if !strings.Contains(string(outcome), `"request_digest":"`+digest+`"`) || !strings.Contains(string(outcome), `"schema":"hive.request-outcome"`) {
		t.Errorf("outcome: %s", outcome)
	}
	after := tree(t, home)

	// The same request, spaced differently, is the same request.
	spaced := append([]byte("  \n"), bytes.ReplaceAll(req, []byte(","), []byte(",\n"))...)
	h.clk.Advance(1000)
	again := h.invoke(t, home, "put", spaced)
	expect(t, again, envelope.Ok, "")
	if !again.Replayed || again.Body["wrote"] != "first" || len(again.Receipts) != 1 || again.Receipts[0].ReceiptID != rc.ReceiptID || again.CompletedAt != resp.CompletedAt {
		t.Errorf("replay: %+v", again)
	}
	if h.calls["put"] != 1 {
		t.Errorf("put ran %d times", h.calls["put"])
	}
	same(t, after, tree(t, home))

	// The same request_id with other content conflicts, and nothing is
	// written.
	other := request(t, "put", 1, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "second" })
	conflict := h.invoke(t, home, "put", other)
	expect(t, conflict, envelope.Conflict, "replay_mismatch")
	if conflict.Replayed || conflict.Error.Retryable || h.calls["put"] != 1 {
		t.Errorf("%+v", conflict)
	}
	same(t, after, tree(t, home))

	// A write at the wrong revision conflicts inside the transaction, and
	// nothing of it is written: no record, no receipt, no outcome.
	wrong := request(t, "put", 2, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "first@3" })
	rev := h.invoke(t, home, "put", wrong)
	expect(t, rev, envelope.Conflict, "revision_conflict")
	if h.calls["put"] != 2 || len(rev.Receipts) != 0 {
		t.Errorf("%+v", rev)
	}
	same(t, after, tree(t, home))
	// At the right revision it replaces the record.
	right := request(t, "put", 3, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "first@1" })
	expect(t, h.invoke(t, home, "put", right), envelope.Ok, "")
	st, err := store.Open(home, "dispatch", store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := st.ReadRecord(store.RecordPath("dispatch", "thing", "first"))
	if err != nil || rec.Revision != 2 {
		t.Errorf("record after replace: %+v %v", rec, err)
	}
	// A name the store refuses is a failure of the command, not a crash.
	bad := request(t, "put", 4, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "Bad Name" })
	expect(t, h.invoke(t, home, "put", bad), envelope.Failed, "io_failure")
}

// A lock another command holds is a conflict with lock_held, retryable, and
// the wait is bounded by the clock.
func TestLockHeld(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	before := tree(t, home)
	held, err := store.Acquire(home, "dispatch", false, store.Waiting{})
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	resp := h.invoke(t, home, "put", request(t, "put", 1, nil))
	expect(t, resp, envelope.Conflict, "lock_held")
	if !resp.Error.Retryable || h.calls["put"] != 0 {
		t.Errorf("%+v", resp)
	}
	if h.clk.Now().Sub(epoch) < 100*time.Millisecond {
		t.Error("the wait did not run to its bound")
	}
	after := tree(t, home)
	delete(after, "locks/dispatch.lock")
	same(t, before, after)
	// A read-only operation takes no lock and is not held up.
	expect(t, h.invoke(t, home, "echo", request(t, "echo", 2, nil)), envelope.Ok, "")
}

// A held operation exits 10 with the hold it waits on, leaves its receipt
// and is replayed from the recorded outcome.
func TestHeld(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	req := request(t, "hold", 1, nil)
	resp := h.invoke(t, home, "hold", req)
	expect(t, resp, envelope.Held, "held")
	if resp.Error.Hold == nil || resp.Error.Hold.ID != id(ids.Question, 1) || len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "dispatch.thing.held" || resp.Error.Retryable {
		t.Fatalf("%+v", resp)
	}
	after := tree(t, home)
	again := h.invoke(t, home, "hold", req)
	expect(t, again, envelope.Held, "held")
	if !again.Replayed || again.Receipts[0].ReceiptID != resp.Receipts[0].ReceiptID || h.calls["hold"] != 1 {
		t.Errorf("%+v", again)
	}
	same(t, after, tree(t, home))
}

// A command that fails for a reason of its own exits 1 and changes nothing;
// an unknown outcome exits 9 and is never retryable; a package's own code
// keeps its status.
func TestFailedAndUnknown(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	before := tree(t, home)
	cases := []struct {
		name   string
		status envelope.Status
		code   string
	}{
		{"plain", envelope.Failed, "internal"},
		{"panic", envelope.Failed, "internal"},
		{"bad failure", envelope.Failed, "internal"},
		{"no body", envelope.Failed, "internal"},
		{"records", envelope.Failed, "internal"},
		{"unknown", envelope.Unknown, "outcome_unknown"},
		{"package code", envelope.Unavailable, "dispatch.adapter_down"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := h.invoke(t, home, "fail", request(t, "fail", 1, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = c.name }))
			expect(t, resp, c.status, c.code)
			if resp.Error.Retryable != (c.name == "package code") {
				t.Errorf("retryable %v", resp.Error.Retryable)
			}
		})
	}
	if !strings.Contains(h.stderr.String(), "panic: boom") {
		t.Error("the panic was not reported on standard error")
	}
	same(t, before, tree(t, home))
	// A response body that breaks the operation's schema is never written.
	e := *h.exe
	op := *e.command("hive-fleet").operation("echo")
	op.ResponseSchema = "urn:hive:schema:home:v1"
	inv := Invocation{Command: &Command{Name: "hive-fleet", Operations: []*Operation{&op}}, Operation: "echo", Home: home, Request: request(t, "echo", 1, nil)}
	outc, err := e.Handle(inv)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, outc.Response, envelope.Failed, "internal")
}

// The thing named does not exist: the home directory, or its record.
func TestNotFound(t *testing.T) {
	h := newHarness(t)
	missing := filepath.Join(t.TempDir(), "nowhere")
	resp := h.invoke(t, missing, "echo", request(t, "echo", 1, nil))
	expect(t, resp, envelope.NotFound, "home_not_found")
	empty := emptyHome(t)
	resp = h.invoke(t, empty, "echo", request(t, "echo", 1, nil))
	expect(t, resp, envelope.NotFound, "home_not_found")
	resp = h.invoke(t, empty, "put", request(t, "put", 1, nil))
	expect(t, resp, envelope.NotFound, "home_not_found")
	if entries, _ := os.ReadDir(empty); len(entries) != 0 {
		t.Errorf("the empty directory was written to: %v", entries)
	}
	// The operation that provisions a home is admitted and gives it its
	// identity; then the home is found.
	resp = h.invoke(t, empty, "provision", request(t, "provision", 2, nil))
	expect(t, resp, envelope.Ok, "")
	if resp.Body["created"] != true {
		t.Errorf("%+v", resp.Body)
	}
	resp = h.invoke(t, empty, "echo", request(t, "echo", 3, nil))
	expect(t, resp, envelope.Ok, "")
	if resp.Body["home"] != homeID {
		t.Errorf("%+v", resp.Body)
	}
	// Provisioning again with another identity is a mismatch.
	resp = h.invoke(t, empty, "provision", request(t, "provision", 4, func(m map[string]any) { m["home_id"] = id(ids.Home, 2) }))
	expect(t, resp, envelope.Refused, "home_mismatch")
}

// Size: a request over the limit is too_large before it is parsed, with its
// whole size counted; one at the limit passes; a response over the limit is
// too_large in its place, and a mutating one leaves nothing behind.
func TestTooLarge(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	before := tree(t, home)
	req := request(t, "echo", 1, nil)
	pad := func(n int) []byte {
		return append(append(bytes.TrimSuffix(req, []byte("\n")), bytes.Repeat([]byte(" "), n)...), '\n')
	}
	exact := pad(4096 - len(req))
	if len(exact) != 4096 {
		t.Fatal("padding")
	}
	expect(t, h.invoke(t, home, "echo", exact), envelope.Ok, "")
	over := pad(10000 - len(req))
	// Nothing of the request was parsed, so the response echoes no
	// request_id.
	code, out, _ := h.run("hive-fleet", over, "echo", "--home", home)
	resp, err := envelope.AcceptResponse(out, code, id(ids.Request, 1))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, resp, envelope.TooLarge, "record_too_large")
	if resp.RequestID != "" || *resp.Error.LimitBytes != 4096 || *resp.Error.ActualBytes != 10000 || resp.Error.Detail["record"] != "request" {
		t.Errorf("%+v %+v", resp, resp.Error)
	}
	if h.calls["echo"] != 1 {
		t.Error("the oversize request was handled")
	}
	// The same by file.
	file := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(file, over, 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ = h.run("hive-fleet", nil, "echo", "--home", home, "--request-file", file)
	resp, err = envelope.AcceptResponse(out, code, id(ids.Request, 1))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, resp, envelope.TooLarge, "record_too_large")
	if *resp.Error.ActualBytes != 10000 {
		t.Errorf("%+v", resp.Error)
	}
	// Handle checks the size too, for a request a convenience form built.
	outc, err := h.exe.Handle(Invocation{Command: h.exe.command("hive-fleet"), Operation: "echo", Home: home, Request: over})
	if err != nil {
		t.Fatal(err)
	}
	expect(t, outc.Response, envelope.TooLarge, "record_too_large")

	// A response too large: in place of the response, nothing written.
	resp = h.invoke(t, home, "big", request(t, "big", 2, nil))
	expect(t, resp, envelope.TooLarge, "record_too_large")
	if resp.RequestID != id(ids.Request, 2) || *resp.Error.LimitBytes != 4096 || *resp.Error.ActualBytes <= 4096 || resp.Error.Detail["record"] != "response" {
		t.Errorf("%+v", resp.Error)
	}
	resp = h.invoke(t, home, "put", request(t, "put", 3, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "huge" }))
	expect(t, resp, envelope.TooLarge, "record_too_large")
	after := tree(t, home)
	delete(after, "locks/dispatch.lock")
	same(t, before, after)
	if _, err := os.Stat(filepath.Join(home, "dispatch", "requests")); err == nil {
		t.Error("an outcome was recorded for a response that was not returned")
	}
	// The too_large response is itself under the limit, whatever the
	// limit; a command with a tiny limit still answers.
	tiny := *h.exe
	tiny.Commands = []*Command{{Name: "hive-dispatch"}, {Name: "hive-fleet", Limits: envelope.Limits{MaxResponseBytes: 1}, Operations: []*Operation{h.exe.command("hive-fleet").operation("echo")}}}
	var stdout, stderr bytes.Buffer
	code = tiny.Main([]string{"hive-fleet", "echo", "--home", home}, bytes.NewReader(req), &stdout, &stderr)
	resp, err = envelope.AcceptResponse(stdout.Bytes(), code, id(ids.Request, 1))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, resp, envelope.TooLarge, "record_too_large")
}

// Something the operation needs cannot be used: the home record, a registry
// record, a recorded outcome or the request file is unreadable or malformed.
func TestUnavailable(t *testing.T) {
	h := newHarness(t)
	broken := newHome(t)
	if err := os.WriteFile(filepath.Join(broken, "home.json"), []byte("{\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	expect(t, h.invoke(t, broken, "echo", request(t, "echo", 1, nil)), envelope.Unavailable, "record_malformed")
	record(t, broken, "home.json", func() map[string]any { m := homeRecord(homeID); m["role"] = "nest"; return m }())
	expect(t, h.invoke(t, broken, "echo", request(t, "echo", 1, nil)), envelope.Unavailable, "record_malformed")
	record(t, broken, "home.json", func() map[string]any { m := homeRecord(homeID); m["schema_version"] = "1.1"; return m }())
	expect(t, h.invoke(t, broken, "echo", request(t, "echo", 1, nil)), envelope.Invalid, "unsupported_version")
	record(t, broken, "home.json", homeRecord(homeID))
	grantOthers(t, filepath.Join(broken, "home.json"), 0o644)
	expect(t, h.invoke(t, broken, "echo", request(t, "echo", 1, nil)), envelope.Unavailable, "record_malformed")

	home := newHome(t)
	record(t, home, "dispatch/registry/workspaces/"+workspaceID+".json", map[string]any{"schema": "hive.workspace", "schema_version": "1.0"})
	ws := func(m map[string]any) { m["scope_level"] = "workspace"; m["workspace_id"] = workspaceID }
	expect(t, h.invoke(t, home, "echo", request(t, "echo", 1, ws)), envelope.Unavailable, "record_malformed")
	record(t, home, "dispatch/registry/workspaces/"+workspaceID+".json", func() map[string]any {
		m := workspaceRecord(workspaceID, homeID, "active")
		m["schema_version"] = "3.0"
		return m
	}())
	expect(t, h.invoke(t, home, "echo", request(t, "echo", 1, ws)), envelope.Invalid, "unsupported_version")
	record(t, home, "dispatch/registry/workspaces/"+workspaceID+".json", workspaceRecord(id(ids.Workspace, 2), homeID, "active"))
	expect(t, h.invoke(t, home, "echo", request(t, "echo", 1, ws)), envelope.Unavailable, "record_malformed")

	// A recorded outcome that cannot be read stops the mutating operation.
	record(t, home, store.OutcomePath("dispatch", id(ids.Request, 7)), map[string]any{"schema": "hive.request-outcome", "schema_version": "1.0"})
	expect(t, h.invoke(t, home, "put", request(t, "put", 7, nil)), envelope.Unavailable, "record_malformed")
	record(t, home, store.OutcomePath("dispatch", id(ids.Request, 8)), map[string]any{"schema": "hive.request-outcome", "schema_version": "2.0", "request_id": id(ids.Request, 8)})
	expect(t, h.invoke(t, home, "put", request(t, "put", 8, nil)), envelope.Invalid, "unsupported_version")

	// An outcome that does not follow its schema is never replayed, whatever
	// else it holds: it is malformed, and the request runs nothing.
	good := newHarness(t)
	source := newHome(t)
	put := request(t, "put", 9, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "thing" })
	expect(t, good.invoke(t, source, "put", put), envelope.Ok, "")
	data, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(store.OutcomePath("dispatch", id(ids.Request, 9)))))
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(m map[string]any){
		"no schema version":      func(m map[string]any) { delete(m, "schema_version") },
		"null schema version":    func(m map[string]any) { m["schema_version"] = nil },
		"malformed version":      func(m map[string]any) { m["schema_version"] = "one" },
		"an unknown member":      func(m map[string]any) { m["extra"] = true },
		"no digest":              func(m map[string]any) { delete(m, "request_digest") },
		"a response that is not": func(m map[string]any) { m["response"].(map[string]any)["exit_code"] = int64(4) },
	} {
		t.Run(name, func(t *testing.T) {
			copied, _ := canon.Parse(data)
			m := copied.(map[string]any)
			edit(m)
			target := newHome(t)
			record(t, target, store.OutcomePath("dispatch", id(ids.Request, 9)), m)
			again := newHarness(t)
			expect(t, again.invoke(t, target, "put", put), envelope.Unavailable, "record_malformed")
			if again.calls["put"] != 0 {
				t.Error("the request ran")
			}
		})
	}
	// The outcome as written is replayed.
	replay := newHome(t)
	record(t, replay, store.OutcomePath("dispatch", id(ids.Request, 9)), v.(map[string]any))
	if r := good.invoke(t, replay, "put", put); !r.Replayed {
		t.Errorf("%+v", r)
	}

	// A request file that cannot be read: no request was read, so the
	// response names none and a caller does not accept it as an answer.
	code, out, _ := h.run("hive-fleet", nil, "echo", "--home", home, "--request-file", filepath.Join(home, "nothing.json"))
	resp, err := envelope.DecodeResponse(out)
	if err != nil || resp.ExitCode() != code {
		t.Fatalf("exit %d: %v", code, err)
	}
	expect(t, resp, envelope.Unavailable, "source_unreadable")
	if _, err := envelope.AcceptResponse(out, code, id(ids.Request, 1)); envelope.AsFailure(err) == nil {
		t.Error("a response that answers no request was accepted for one")
	}
}

// Transport: the request from a file or standard input, the response to a
// file or standard output, one document and a line feed, and diagnostics
// on standard error alone.
func TestTransport(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	dir := t.TempDir()
	reqFile := filepath.Join(dir, "request.json")
	respFile := filepath.Join(dir, "response.json")
	req := request(t, "echo", 1, nil)
	if err := os.WriteFile(reqFile, req, 0o600); err != nil {
		t.Fatal(err)
	}
	// A response file that was there before is private once it is written.
	if err := os.WriteFile(respFile, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(respFile, 0o644); err != nil {
		t.Fatal(err)
	}
	// An option is followed by its value; the name=value spelling is not one.
	code, out, errs := h.run("hive-fleet", nil, "echo", "--home="+home, "--request-file", reqFile, "--response-file", respFile)
	if code != 2 || !strings.Contains(errs, "unknown option") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errs)
	}
	code, out, errs = h.run("hive-fleet", nil, "echo", "--home", home, "--request-file", reqFile, "--response-file", respFile)
	if code != 0 || len(out) != 0 || errs != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errs)
	}
	written, err := os.ReadFile(respFile)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := envelope.AcceptResponse(written, code, id(ids.Request, 1))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, resp, envelope.Ok, "")
	if info, _ := os.Stat(respFile); !hasMode(info, respFile, 0o600) {
		t.Errorf("response file mode %o", info.Mode().Perm())
	}
	if bytes.Count(written, []byte("\n")) != 1 || written[len(written)-1] != '\n' {
		t.Error("the response is not one document followed by a line feed")
	}
	// The response file is replaced whole on a second invocation.
	code, _, _ = h.run("hive-fleet", nil, "fail", "--home", home, "--request-file", reqFile, "--response-file", respFile)
	if code != 3 {
		t.Errorf("exit %d", code)
	}
	written2, _ := os.ReadFile(respFile)
	resp, err = envelope.AcceptResponse(written2, code, id(ids.Request, 1))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, resp, envelope.Invalid, "envelope_mismatch")
	// A usage error after the options were read still goes to the file.
	code, out, _ = h.run("hive-fleet", nil, "echo", "--home", home, "--response-file", respFile, "--extra")
	written3, _ := os.ReadFile(respFile)
	if code != 2 || len(out) != 0 || bytes.Equal(written3, written2) {
		t.Errorf("exit %d, stdout %q", code, out)
	}
	// Diagnostics for a person on standard error, never parsed.
	_, _, errs = h.run("hive-fleet", req, "echo", "--home", home)
	if errs != "" {
		t.Errorf("stderr on ok: %q", errs)
	}
	_, _, errs = h.run("hive-fleet", req, "put", "--home", home)
	if !strings.Contains(errs, "envelope_mismatch") {
		t.Errorf("stderr: %q", errs)
	}
	// A response file that cannot be written: no response, exit 1.
	code, out, errs = h.run("hive-fleet", req, "echo", "--home", home, "--response-file", filepath.Join(dir, "no", "such", "dir"))
	if code != 1 || len(out) != 0 || errs == "" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errs)
	}
}

// A resident operation writes its response once it has started, then runs
// on; what it does after is not part of the response.
func TestResident(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	resp := h.invoke(t, home, "serve", request(t, "serve", 1, nil))
	expect(t, resp, envelope.Ok, "")
	if h.calls["after"] != 1 {
		t.Errorf("after ran %d times", h.calls["after"])
	}
	code, out, errs := h.run("hive-fleet", request(t, "serve", 2, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "stop badly" }), "serve", "--home", home)
	if code != 1 || !strings.Contains(errs, "fell over") {
		t.Errorf("exit %d, stderr %q", code, errs)
	}
	if r, err := envelope.DecodeResponse(out); err != nil || r.Status != envelope.Ok {
		t.Errorf("the response was not written before the server ran: %v", err)
	}
	// A resident operation that fails to start runs nothing after.
	resp = h.invoke(t, home, "serve", request(t, "serve", 3, func(m map[string]any) { m["operation_version"] = "9.0" }))
	expect(t, resp, envelope.Invalid, "unsupported_version")
	if h.calls["after"] != 2 {
		t.Errorf("after ran %d times", h.calls["after"])
	}
}

// Every response the harness writes is canonical, one line, and accepted
// by the caller's rule with its exit code; the outcome recorded for a
// request is the response as returned.
func TestRecordedOutcomeIsTheResponse(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	req := request(t, "put", 1, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "thing" })
	code, out, _ := h.run("hive-fleet", req, "put", "--home", home)
	resp, err := envelope.AcceptResponse(out, code, id(ids.Request, 1))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(store.OutcomePath("dispatch", id(ids.Request, 1)))))
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	recorded, _ := canon.Encode(v.(map[string]any)["response"])
	want, _ := resp.Encode()
	if !bytes.Equal(append(recorded, '\n'), want) {
		t.Errorf("the recorded response differs:\n%s\n%s", recorded, want)
	}
	if err := canon.CheckFile(data); err != nil {
		t.Error(err)
	}
}

// Nothing is written for a response that cannot be returned whole: a hold or
// a refusal too large for the limit, or one the response schema refuses, is
// answered as that and leaves no receipt and no outcome behind.
func TestResponseIsWholeBeforeAnythingIsWritten(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	before := tree(t, home)
	named := func(operation string, n int, name string) []byte {
		return request(t, operation, n, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = name })
	}
	for n, c := range []struct {
		operation, name string
		status          envelope.Status
		code            string
	}{
		{"hold", "oversize", envelope.TooLarge, "record_too_large"},
		{"hold", "bad hold", envelope.Failed, "internal"},
		{"refuse", "record big", envelope.TooLarge, "record_too_large"},
	} {
		t.Run(c.operation+" "+c.name, func(t *testing.T) {
			resp := h.invoke(t, home, c.operation, named(c.operation, n+1, c.name))
			expect(t, resp, c.status, c.code)
			if len(resp.Receipts) != 0 || resp.Replayed {
				t.Errorf("%+v", resp)
			}
			after := tree(t, home)
			delete(after, "locks/dispatch.lock")
			same(t, before, after)
		})
	}
}

// A transaction whose intent is durable is the store's to complete, and from
// then on the only answers are ok and outcome_unknown: never one that promises
// nothing changed, whatever stopped it, however long the diagnostic, and
// whether it was this commit or the recovery a retry runs that stopped. The
// same request, sent again once the store works, completes it and replays.
func TestInterruptedCommitIsUnknownUntilCompleted(t *testing.T) {
	intentOf := func(home string, n int) string {
		return filepath.Join(home, filepath.FromSlash(store.IntentPath("dispatch", id(ids.Request, n))))
	}
	put := func(n int, name string) []byte {
		return request(t, "put", n, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = name })
	}
	unknown := func(t *testing.T, resp *envelope.Response) {
		t.Helper()
		expect(t, resp, envelope.Unknown, "outcome_unknown")
		if len(resp.Receipts) != 0 || resp.Error.Retryable || resp.Replayed {
			t.Errorf("%+v", resp)
		}
	}
	for _, c := range []struct {
		name  string
		limit int64
		cause string
	}{
		{"a diagnostic", 0, "the journal is full"},
		{"a diagnostic longer than the response limit", 2048, strings.Repeat("x", 2000)},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			if c.limit != 0 {
				h.exe.command("hive-fleet").Limits.MaxResponseBytes = c.limit
			}
			home := newHome(t)
			req := put(1, "first")
			h.journal.fail = errors.New(c.cause)
			unknown(t, h.invoke(t, home, "put", req))
			if _, err := os.Stat(intentOf(home, 1)); err != nil {
				t.Fatalf("the intent is not there: %v", err)
			}

			// Recovery that fails again is the same answer, and nothing of the
			// handler runs a second time.
			unknown(t, h.invoke(t, home, "put", req))
			if h.calls["put"] != 1 {
				t.Errorf("put ran %d times", h.calls["put"])
			}

			h.journal.fail = nil
			again := h.invoke(t, home, "put", req)
			expect(t, again, envelope.Ok, "")
			if !again.Replayed || len(again.Receipts) != 1 || again.Body["wrote"] != "first" || h.calls["put"] != 1 {
				t.Errorf("%+v, calls %d", again, h.calls["put"])
			}
			if _, err := os.Stat(intentOf(home, 1)); err == nil {
				t.Error("the intent was left behind")
			}
			for _, p := range []string{store.RecordPath("dispatch", "thing", "first"), store.ReceiptPath("dispatch", again.Receipts[0].ReceiptID), store.OutcomePath("dispatch", id(ids.Request, 1))} {
				if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(p))); err != nil {
					t.Errorf("%s: %v", p, err)
				}
			}
			if len(h.journal.order) != 1 {
				t.Errorf("events appended: %v", h.journal.order)
			}
		})
	}

	// A recovery that stops on the transaction of an earlier request answers
	// the present request unavailable: the present request wrote nothing.
	t.Run("an earlier request's recovery", func(t *testing.T) {
		h := newHarness(t)
		home := newHome(t)
		h.journal.fail = errors.New("the journal is full")
		unknown(t, h.invoke(t, home, "put", put(1, "first")))
		before := tree(t, home)
		resp := h.invoke(t, home, "put", put(2, "second"))
		expect(t, resp, envelope.Unavailable, "record_malformed")
		if h.calls["put"] != 1 || len(resp.Receipts) != 0 {
			t.Errorf("%+v, calls %d", resp, h.calls["put"])
		}
		after := tree(t, home)
		same(t, before, after)
		h.journal.fail = nil
		expect(t, h.invoke(t, home, "put", put(2, "second")), envelope.Ok, "")
	})
}

// A retry of a request whose transaction is durable that fails in the
// recovery before it reaches the intent, at the enumeration of the intents or
// at the first flush of the transaction directory, or after it completed the
// intent, at the cleanup, answers unknown and never failed: the store reports
// the request's transaction as durable, and the harness answers nothing else
// for it. The handler runs once, and the request sent again once the store
// works is the response as recorded.
func TestRetryThatFailsRecoveryIsUnknown(t *testing.T) {
	for _, step := range []string{"readdir dispatch/tx/", "syncdir dispatch/tx/", "clear dispatch/tmp/"} {
		t.Run(step, func(t *testing.T) {
			h := newHarness(t)
			home := newHome(t)
			req := request(t, "put", 1, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "first" })
			h.journal.fail = errors.New("the journal is full")
			expectUnknown(t, h.invoke(t, home, "put", req))
			intent := filepath.Join(home, filepath.FromSlash(store.IntentPath("dispatch", id(ids.Request, 1))))
			if _, err := os.Stat(intent); err != nil {
				t.Fatalf("the intent is not there: %v", err)
			}

			h.journal.fail = nil
			hit := h.faultAt(step)
			resp := h.invoke(t, home, "put", req)
			if !*hit {
				t.Fatal("the interruption did not happen")
			}
			expectUnknown(t, resp)
			if h.calls["put"] != 1 {
				t.Errorf("put ran %d times", h.calls["put"])
			}
			if !strings.Contains(h.stderr.String(), "crashed here") {
				t.Error("the cause was not reported on standard error")
			}

			h.exe.Fault = nil
			again := h.invoke(t, home, "put", req)
			expect(t, again, envelope.Ok, "")
			if !again.Replayed || len(again.Receipts) != 1 || again.Body["wrote"] != "first" || h.calls["put"] != 1 {
				t.Errorf("%+v, calls %d", again, h.calls["put"])
			}
			if _, err := os.Stat(intent); err == nil {
				t.Error("the intent was left behind")
			}
			if len(h.journal.order) != 1 {
				t.Errorf("events appended: %v", h.journal.order)
			}
		})
	}
}

// Handle, which a convenience form calls without Check, holds the floor
// itself: a command whose response limit admits a small success response but
// not the outcome_unknown answer is refused before anything is written, and
// an interrupted commit under it is never answered too_large. At the floor
// exactly, an interrupted commit is answered unknown.
func TestHandleHoldsTheFloor(t *testing.T) {
	h := newHarness(t)
	fleet := h.exe.command("hive-fleet")
	home := newHome(t)
	out, err := h.exe.Handle(Invocation{Command: fleet, Operation: "raw", Home: home, Request: request(t, "raw", 1, nil)})
	if err != nil {
		t.Fatal(err)
	}
	expect(t, out.Response, envelope.Ok, "")
	success := int64(len(out.Encoded))
	floor := floorOf(t, h.exe, fleet)
	if success >= floor {
		t.Fatalf("the success response is %d bytes and the floor %d: the control needs a success below the floor", success, floor)
	}
	narrow := func(limit int64) *Command {
		return &Command{Name: fleet.Name, Limits: envelope.Limits{MaxRequestBytes: 4096, MaxResponseBytes: limit}, Operations: fleet.Operations}
	}

	// Main refuses the whole executable.
	below := *h.exe
	below.Commands = []*Command{{Name: "hive-dispatch"}, narrow(success)}
	if err := below.Check(); err == nil || !strings.Contains(err.Error(), "interrupted transaction") {
		t.Errorf("Check = %v", err)
	}
	var stdout, stderr bytes.Buffer
	if code := below.Main([]string{"hive-fleet", "raw", HomeOption, home}, bytes.NewReader(request(t, "raw", 2, nil)), &stdout, &stderr); code != 1 || stdout.Len() != 0 {
		t.Errorf("Main: exit %d, stdout %q", code, stdout.String())
	}

	// Handle refuses the operation at the floor, before anything is written,
	// with no response, as Main refuses the executable: the store is not
	// opened, the handler does not run, and the commit the test would
	// interrupt is never reached, so no response, too_large least of all,
	// can say anything about it.
	home = newHome(t)
	before := tree(t, home)
	hit := h.faultAt("create " + store.OutcomePath("dispatch", id(ids.Request, 2)))
	out, err = h.exe.Handle(Invocation{Command: narrow(success), Operation: "raw", Home: home, Request: request(t, "raw", 2, nil)})
	if err == nil || out != nil || !strings.Contains(err.Error(), "interrupted transaction") || !strings.Contains(err.Error(), "before anything is written") {
		t.Fatalf("Handle under a limit below the floor = %+v, %v", out, err)
	}
	if *hit || h.calls["raw"] != 1 {
		t.Errorf("hit %v, calls %d", *hit, h.calls["raw"])
	}
	same(t, before, tree(t, home))
	// A read-only operation of the same command is not held to the floor,
	// and an invalid request to the mutating one is still answered invalid:
	// the floor refuses only what would reach the store.
	out, err = h.exe.Handle(Invocation{Command: narrow(success), Operation: "echo", Home: home, Request: request(t, "echo", 3, nil)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Response.Status != envelope.Ok && out.Response.Status != envelope.TooLarge {
		t.Errorf("echo under the narrow limit: %+v", out.Response)
	}
	out, err = h.exe.Handle(Invocation{Command: narrow(success), Operation: "raw", Home: home, Request: []byte("{\n")})
	if err != nil {
		t.Fatal(err)
	}
	if out.Response.Status != envelope.Invalid && out.Response.Status != envelope.TooLarge {
		t.Errorf("a malformed request under the narrow limit: %+v", out.Response)
	}
	same(t, before, tree(t, home))

	// At the floor, the interrupted commit is answered unknown, and the
	// request sent again completes it and replays the success, which fits.
	hit = h.faultAt("create " + store.OutcomePath("dispatch", id(ids.Request, 4)))
	out, err = h.exe.Handle(Invocation{Command: narrow(floor), Operation: "raw", Home: home, Request: request(t, "raw", 4, nil)})
	if err != nil {
		t.Fatal(err)
	}
	if !*hit {
		t.Fatal("the interruption did not happen")
	}
	expectUnknown(t, out.Response)
	if int64(len(out.Encoded)) > floor {
		t.Errorf("the unknown answer is %d bytes over its floor %d", len(out.Encoded), floor)
	}
	h.exe.Fault = nil
	out, err = h.exe.Handle(Invocation{Command: narrow(floor), Operation: "raw", Home: home, Request: request(t, "raw", 4, nil)})
	if err != nil {
		t.Fatal(err)
	}
	expect(t, out.Response, envelope.Ok, "")
	if !out.Response.Replayed || h.calls["raw"] != 2 {
		t.Errorf("%+v, calls %d", out.Response, h.calls["raw"])
	}
}

// A put interrupted at the journal append, then recovered by its retry while
// the response limit is lowered below the recorded response but not below
// the floor, is answered unknown and never too_large: the transaction is
// complete, and only the response as recorded or unknown can say so. With the
// limit restored, the replay is the response as recorded.
func TestReplayThatNoLongerFitsIsUnknown(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	fleet := h.exe.command("hive-fleet")
	req := request(t, "put", 1, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = "first" })
	h.journal.fail = errors.New("the journal is full")
	expectUnknown(t, h.invoke(t, home, "put", req))
	intent := filepath.Join(home, filepath.FromSlash(store.IntentPath("dispatch", id(ids.Request, 1))))
	if _, err := os.Stat(intent); err != nil {
		t.Fatalf("the intent is not there: %v", err)
	}

	h.journal.fail = nil
	floor := floorOf(t, h.exe, fleet)
	fleet.Limits.MaxResponseBytes = floor
	resp := h.invoke(t, home, "put", req)
	if resp.Status == envelope.TooLarge {
		t.Fatalf("a completed transaction was answered too_large: %+v", resp)
	}
	expectUnknown(t, resp)
	if h.calls["put"] != 1 {
		t.Errorf("put ran %d times", h.calls["put"])
	}
	// The retry completed the transaction: the intent is gone, the outcome
	// is recorded, the event is in the journal once, and the recorded
	// response is indeed over the limit.
	if _, err := os.Stat(intent); err == nil {
		t.Error("the intent was left behind")
	}
	if len(h.journal.order) != 1 {
		t.Errorf("events appended: %v", h.journal.order)
	}
	data, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(store.OutcomePath("dispatch", id(ids.Request, 1)))))
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	recorded, _ := canon.Encode(v.(map[string]any)["response"])
	if int64(len(recorded)+1) <= floor {
		t.Fatalf("the recorded response is %d bytes, within the limit %d: the control needs one over it", len(recorded)+1, floor)
	}
	// Lowered again, the same answer; nothing of the handler runs.
	expectUnknown(t, h.invoke(t, home, "put", req))
	if !strings.Contains(h.stderr.String(), "recorded response") {
		t.Error("the cause was not reported on standard error")
	}

	fleet.Limits.MaxResponseBytes = 4096
	again := h.invoke(t, home, "put", req)
	expect(t, again, envelope.Ok, "")
	if !again.Replayed || len(again.Receipts) != 1 || again.Body["wrote"] != "first" || h.calls["put"] != 1 {
		t.Errorf("%+v, calls %d", again, h.calls["put"])
	}
}

// A record is final before anything is sealed: the digest the response and
// the receipt carry, taken from Finalize, is the digest of the record on disk
// though the clock moves between every reading, and a record returned without
// Finalize is written as Finalize would make it.
func TestRecordIsFinalBeforeItIsSealed(t *testing.T) {
	h := newHarness(t)
	h.exe.Clock = &ticking{at: epoch}
	home := newHome(t)
	resp := h.invoke(t, home, "stamp", request(t, "stamp", 1, nil))
	expect(t, resp, envelope.Ok, "")
	st, err := store.Open(home, "dispatch", store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := st.ReadRecord(store.RecordPath("dispatch", "thing", "stamped"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Body["digest"] != rec.Digest || resp.Receipts[0].Subject.Digest != rec.Digest || rec.Revision != 1 {
		t.Errorf("the response says %v, the receipt %s, the record on disk %s", resp.Body["digest"], resp.Receipts[0].Subject.Digest, rec.Digest)
	}
	// The handler's own clock is live.
	tick := h.invoke(t, home, "tick", request(t, "tick", 3, nil))
	if tick.Body["a"] == tick.Body["b"] {
		t.Errorf("the clock of a read-only call held still: %v", tick.Body)
	}
	// What the outcome recorded is what was returned and what was stored.
	again := h.invoke(t, home, "stamp", request(t, "stamp", 1, nil))
	if !again.Replayed || again.Body["digest"] != rec.Digest {
		t.Errorf("%+v", again)
	}

	expect(t, h.invoke(t, home, "raw", request(t, "raw", 2, nil)), envelope.Ok, "")
	raw, err := st.ReadRecord(store.RecordPath("dispatch", "thing", "raw"))
	if err != nil {
		t.Fatal(err)
	}
	v, _ := canon.Parse(raw.Data)
	if raw.Revision != 1 || v.(map[string]any)["updated_at"] == "2020-01-01T00:00:00.000Z" {
		t.Errorf("a record returned unfinalized was written as it was: %s", raw.Data)
	}
}

// An executable that owns no part of the home keeps no store, and Check holds
// it to that: it may run read-only operations and declare mutating ones
// unsupported or parked, and a supported mutating operation is refused.
func TestStorelessExecutable(t *testing.T) {
	h := newHarness(t)
	read := &Operation{OperationDecl: envelope.OperationDecl{Name: "look", State: envelope.Supported, Mode: envelope.Unary, Versions: []string{"1.0"},
		RequestSchema: testRequestSchema, ResponseSchema: testResponseSchema, ReceiptKinds: []string{}, Executes: []string{}, Calls: []string{}},
		Handle: func(c *Call) (*Result, error) { return &Result{Body: map[string]any{"seen": true}}, nil }}
	closed := &Operation{OperationDecl: envelope.OperationDecl{Name: "say", State: envelope.Unsupported, Reason: "built at stage 4", Mode: envelope.Unary, Mutates: true, Versions: []string{}, ReceiptKinds: []string{}, Executes: []string{}, Calls: []string{}}}
	parked := &Operation{OperationDecl: envelope.OperationDecl{Name: "mute", State: envelope.Parked, Reason: "held closed by decision 7", Mode: envelope.Unary, Mutates: true, Versions: []string{"1.0"}, ReceiptKinds: []string{}, Executes: []string{}, Calls: []string{}}}
	word := &Executable{Package: "word", Name: "hive-word", Version: "0.1.0-test", Storeless: true,
		Commands: []*Command{{Name: "hive-word", Operations: []*Operation{read, closed, parked}}}, Clock: h.clk}
	if err := word.Check(); err != nil {
		t.Fatal(err)
	}
	home := newHome(t)
	before := tree(t, home)
	send := func(operation string) *envelope.Response {
		t.Helper()
		req := request(t, operation, 1, func(m map[string]any) { m["command"] = "hive-word" })
		var stdout, stderr bytes.Buffer
		code := word.Main([]string{"hive-word", operation, HomeOption, home}, bytes.NewReader(req), &stdout, &stderr)
		resp, err := envelope.AcceptResponse(stdout.Bytes(), code, id(ids.Request, 1))
		if err != nil {
			t.Fatalf("%v\n%s", err, stdout.String())
		}
		return resp
	}
	if resp := send("look"); resp.Status != envelope.Ok || resp.Body["seen"] != true {
		t.Errorf("%+v", resp)
	}
	expect(t, send("say"), envelope.Refused, "unsupported_operation")
	expect(t, send("mute"), envelope.Refused, "operation_parked")
	same(t, before, tree(t, home))

	say := *closed
	say.State, say.Versions, say.RequestSchema, say.ResponseSchema = envelope.Supported, []string{"1.0"}, testRequestSchema, testResponseSchema
	say.Handle = func(c *Call) (*Result, error) { return &Result{Body: map[string]any{}}, nil }
	word.Commands = []*Command{{Name: "hive-word", Operations: []*Operation{read, &say}}}
	err := word.Check()
	if err == nil || !strings.Contains(err.Error(), "delegating handoff") {
		t.Errorf("Check: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if code := word.Main([]string{"hive-word", "look", HomeOption, home}, bytes.NewReader(request(t, "look", 1, nil)), &stdout, &stderr); code != 1 || stdout.Len() != 0 {
		t.Errorf("Main: exit %d, stdout %q", code, stdout.String())
	}
}

// A program started under a Windows name acts as the command of that name
// without its .exe suffix and without regard to case; elsewhere the base name
// is the name.
func TestCommandName(t *testing.T) {
	for _, c := range []struct {
		argv0   string
		windows bool
		want    string
	}{
		{"bin/hive-pool", false, "hive-pool"},
		{"bin/hive-pool.exe", false, "hive-pool.exe"},
		{"bin/Hive-Pool.EXE", false, "Hive-Pool.EXE"},
		{"C:/Hive/hive-pool.exe", true, "hive-pool"},
		{"C:/Hive/HIVE-Pool.EXE", true, "hive-pool"},
		{"hive-pool", true, "hive-pool"},
	} {
		if got := commandName(c.argv0, c.windows); got != c.want {
			t.Errorf("commandName(%q, %v) = %q, want %q", c.argv0, c.windows, got, c.want)
		}
	}
}
