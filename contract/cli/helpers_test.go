package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/store"
)

var epoch = time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)

const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// id invents the n-th identifier of kind p.
func id(p ids.Prefix, n int) string {
	s := string(p) + "_0" + strings.Repeat("0", 23) + string(alphabet[n/32%32]) + string(alphabet[n%32])
	if err := ids.Check(s, p); err != nil {
		panic(err)
	}
	return s
}

// The identities of the test home and its registry.
var (
	homeID      = id(ids.Home, 1)
	workspaceID = id(ids.Workspace, 1)
	projectID   = id(ids.Project, 1)
	cloneID     = id(ids.Clone, 1)
)

// chainPath is the path a lower layer names in the error the fail operation
// returns for "chain".
const chainPath = "/srv/hive-test/records/thing.json"

// The schemas the test operations declare: the provision request body as a
// request body, and the common definitions, which admit any object, as a
// response body.
const (
	testRequestSchema  = "urn:hive:schema:ops:hive-fleet.provision.request:v1"
	testResponseSchema = "urn:hive:schema:common:v1"
)

// record writes a JSON tree as a record file, in canonical form with one
// final line feed, under dir.
func record(t testing.TB, dir, rel string, tree map[string]any) {
	t.Helper()
	out, err := canon.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writer() map[string]any {
	return map[string]any{"package": "dispatch", "command": "hive-fleet", "operation": "provision", "implementation_version": "test"}
}

// homeRecord is a valid home.json for the test home.
func homeRecord(home string) map[string]any {
	return map[string]any{
		"schema": "hive.home", "schema_version": "1.0", "record_revision": int64(1),
		"home_id": home, "role": "hive", "name": "A test home", "parent": map[string]any{"kind": "none"},
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "writer": writer(),
	}
}

// emptyHome makes a home directory with nothing in it.
func emptyHome(t testing.TB) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	secureHome(t, dir)
	return dir
}

// newHome makes a home with its identity record and a registry holding one
// active workspace, project and clone that belong to each other.
func newHome(t testing.TB) string {
	t.Helper()
	dir := emptyHome(t)
	record(t, dir, "home.json", homeRecord(homeID))
	record(t, dir, "dispatch/registry/workspaces/"+workspaceID+".json", workspaceRecord(workspaceID, homeID, "active"))
	record(t, dir, "dispatch/registry/projects/"+projectID+".json", projectRecord(projectID, workspaceID, homeID, "active"))
	record(t, dir, "dispatch/registry/clones/"+cloneID+".json", cloneRecord(cloneID, projectID, workspaceID, homeID, "active"))
	return dir
}

func workspaceRecord(ws, home, state string) map[string]any {
	return map[string]any{
		"schema": "hive.workspace", "schema_version": "1.0", "record_revision": int64(1),
		"workspace_id": ws, "slug": "one", "name": "One", "home_id": home, "parent_home_id": id(ids.Home, 9), "state": state,
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "writer": writer(),
	}
}

func projectRecord(proj, ws, home, state string) map[string]any {
	return map[string]any{
		"schema": "hive.project", "schema_version": "1.0", "record_revision": int64(1),
		"project_id": proj, "workspace_id": ws, "home_id": home, "slug": "p", "name": "P",
		"repository": map[string]any{"kind": "local-only"}, "state": state,
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "writer": writer(),
	}
}

func cloneRecord(clone, proj, ws, home, state string) map[string]any {
	return map[string]any{
		"schema": "hive.clone", "schema_version": "1.0", "record_revision": int64(1),
		"clone_id": clone, "project_id": proj, "workspace_id": ws, "home_id": home,
		"git_common_dir": "/srv/one/.git", "worktree_root": "/srv/one", "object_format": "sha1",
		"origin": map[string]any{"kind": "none"}, "state": state, "verified_at": "2026-10-11T08:00:00.000Z",
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "writer": writer(),
	}
}

// request builds a valid request for the test command: hive-fleet, the
// operation given, body version 1.0, in the hive scope of the test home,
// with a body the provision schema admits. edit may change the tree before
// it is written.
func request(t testing.TB, operation string, n int, edit func(m map[string]any)) []byte {
	t.Helper()
	m := map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": "hive-fleet", "operation": operation, "operation_version": "1.0",
		"request_id": id(ids.Request, n), "correlation_id": id(ids.Correlation, 1),
		"issued_at": "2026-10-11T08:00:00.000Z", "caller": map[string]any{"kind": "test"},
		"scope_level": "hive", "home_id": homeID,
		"authority": map[string]any{"kind": "none"},
		"body":      map[string]any{"home": map[string]any{"role": "hive", "name": "A home"}},
	}
	if edit != nil {
		edit(m)
	}
	out, err := canon.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	return append(out, "\n"...)
}

// fakeJournal keeps each event once, by event_id.
type fakeJournal struct {
	order []string
	lines map[string][]byte
	// fail, when set, is the error Append and Recover return without
	// appending.
	fail error
	// checks is what the journal says of whether its own validation applies
	// the no-value check, and dislikes, when set, the text such a validation
	// refuses a body for: the event holding it is refused whole.
	checks   bool
	dislikes string
}

// ChecksValues says whether the journal applies the no-value check.
func (j *fakeJournal) ChecksValues() bool { return j.checks }

func (j *fakeJournal) Append(events []store.Event) error {
	if j.fail != nil {
		return j.fail
	}
	for _, e := range events {
		if j.dislikes != "" && bytes.Contains(e.Content, []byte(j.dislikes)) {
			return errors.New("the body holds a value")
		}
	}
	for _, e := range events {
		if _, ok := j.lines[e.ID]; ok {
			continue
		}
		j.order = append(j.order, e.ID)
		j.lines[e.ID] = append([]byte(nil), e.Content...)
	}
	return nil
}

// Recover is Append: the fake never repeats an event it holds.
func (j *fakeJournal) Recover(events []store.Event) error { return j.Append(events) }

// harness is a test executable with its fake clock and journal.
type harness struct {
	exe     *Executable
	clk     *clock.Fake
	journal *fakeJournal
	// calls counts the handler runs of each operation.
	calls map[string]int
	// stderr gathers the diagnostics of every run.
	stderr bytes.Buffer
}

// newHarness makes the test executable: hive-dispatch, owning hive-fleet
// with the operations the tests drive, and hive-park with none.
func newHarness(t testing.TB) *harness {
	t.Helper()
	h := &harness{clk: clock.NewFake(epoch), journal: &fakeJournal{lines: map[string][]byte{}, checks: true}, calls: map[string]int{}}
	count := func(name string, f func(*Call) (*Result, error)) func(*Call) (*Result, error) {
		return func(c *Call) (*Result, error) {
			h.calls[name]++
			return f(c)
		}
	}
	decl := func(name string, mutates bool) envelope.OperationDecl {
		return envelope.OperationDecl{Name: name, State: envelope.Supported, Mode: envelope.Unary, Mutates: mutates, Versions: []string{"1.0", "1.1"},
			RequestSchema: testRequestSchema, ResponseSchema: testResponseSchema, ReceiptKinds: []string{}, Executes: []string{}, Calls: []string{}}
	}
	fleet := &Command{Name: "hive-fleet", Limits: envelope.Limits{MaxRequestBytes: 4096, MaxResponseBytes: 4096}, Operations: []*Operation{
		// echo answers with the request's body and scope; read-only.
		{OperationDecl: decl("echo", false), Handle: count("echo", func(c *Call) (*Result, error) {
			if c.Store != nil {
				return nil, errors.New("a read-only operation was given the store")
			}
			unknown := make([]any, 0, len(c.Home.Unknown))
			for _, u := range c.Home.Unknown {
				unknown = append(unknown, u)
			}
			return &Result{Body: map[string]any{"echo": c.Request.Body, "scope": c.Request.Level, "home": c.Home.HomeID, "unknown": unknown}}, nil
		})},
		// put writes one record with the name the body gives, one receipt
		// and one event, and answers with what it wrote.
		{OperationDecl: decl("put", true), Handle: count("put", func(c *Call) (*Result, error) {
			label, _ := c.Request.Body["home"].(map[string]any)["name"].(string)
			if label == "huge" {
				return &Result{Body: map[string]any{"filler": strings.Repeat("x", 4096)}}, nil
			}
			// The name is the record's name, and after @ the revision the
			// write expects.
			name, rev, _ := strings.Cut(label, "@")
			var expected int64
			for _, d := range rev {
				expected = expected*10 + int64(d-'0')
			}
			subject := &envelope.RecordRef{Schema: "dispatch.thing", ID: name}
			rc, err := c.NewReceipt("dispatch.thing.put", "1.0", subject, nil, nil)
			if err != nil {
				return nil, err
			}
			return &Result{
				Body:     map[string]any{"wrote": name},
				Records:  []store.RecordWrite{{Path: store.RecordPath("dispatch", "thing", name), Content: []byte(`{"schema":"dispatch.thing","schema_version":"1.0","name":"` + name + `"}`), Expected: expected}},
				Receipts: []envelope.Receipt{rc},
				Events:   []store.Event{{ID: id(ids.Event, h.calls["put"]), Content: []byte(`{"schema":"dispatch.thing.put"}`)}},
			}, nil
		})},
		// refuse refuses, and leaves a receipt of the refusal when the body
		// asks for one with a name.
		{OperationDecl: decl("refuse", true), Handle: count("refuse", func(c *Call) (*Result, error) {
			f := envelope.Common(envelope.CodeAuthorityMissing, "no word was given")
			name, _ := c.Request.Body["home"].(map[string]any)["name"].(string)
			switch name {
			case "unknown":
				f = envelope.Common(envelope.CodeOutcomeUnknown, "the adapter did not answer")
			case "unknown bare":
				f = envelope.Common(envelope.CodeOutcomeUnknown, "the adapter did not answer")
			case "wide":
				f = envelope.Common(envelope.CodeAuthorityMissing, strings.Repeat("é", 1500))
			case "record big":
				f = envelope.Common(envelope.CodeAuthorityMissing, strings.Repeat("é", 2000))
			case "conflict":
				f = envelope.Common(envelope.CodeAlreadyExists, "it exists")
			}
			if name == "record it" || name == "unknown" || name == "record big" || name == "conflict" {
				rc, err := c.NewReceipt("dispatch.thing.refused", "1.0", nil, nil, nil)
				if err != nil {
					return nil, err
				}
				f.Receipts = []envelope.Receipt{rc}
			}
			return nil, f
		})},
		// hold answers held with a receipt of the hold.
		{OperationDecl: decl("hold", true), Handle: count("hold", func(c *Call) (*Result, error) {
			rc, err := c.NewReceipt("dispatch.thing.held", "1.0", nil, nil, nil)
			if err != nil {
				return nil, err
			}
			f := envelope.Hold(envelope.RecordRef{Schema: "word.question", ID: id(ids.Question, 1)}, "waiting on a decision")
			switch c.Request.Body["home"].(map[string]any)["name"] {
			case "oversize":
				f.Err.Message = strings.Repeat("é", 2000)
			case "bad hold":
				f.Err.Hold.Digest = "nope"
			}
			f.Receipts = []envelope.Receipt{rc}
			return nil, f
		})},
		// effect commits its intent, a record and a receipt, then counts an
		// effect outside Hive in h.calls["effected"], then ends as the body's
		// name says.
		{OperationDecl: decl("effect", true), Handle: count("effect", func(c *Call) (*Result, error) {
			mode, _ := c.Request.Body["home"].(map[string]any)["name"].(string)
			path := store.RecordPath("dispatch", "thing", "intended")
			var body map[string]any
			if mode == "large receipt" {
				body = map[string]any{"filler": strings.Repeat("x", 5000)}
			}
			intended, err := c.NewReceipt("dispatch.thing.intended", "1.0", nil, nil, body)
			if err != nil {
				return nil, err
			}
			if mode == "record only" {
				if _, err := c.CommitIntent(Intent{Records: []store.RecordWrite{{Path: path, Content: []byte(`{"schema":"dispatch.thing","schema_version":"1.0","name":"intended"}`)}}}); err != nil {
					return nil, err
				}
				h.calls["effected"]++
				return nil, errors.New("something of its own")
			}
			expected := int64(0)
			if mode == "conflict" {
				expected = 4
			}
			written, err := c.CommitIntent(Intent{
				Records:  []store.RecordWrite{{Path: path, Content: []byte(`{"schema":"dispatch.thing","schema_version":"1.0","name":"intended"}`), Expected: expected}},
				Receipts: []envelope.Receipt{intended},
			})
			if err != nil {
				return nil, err
			}
			h.calls["effected"]++
			switch mode {
			case "unknown":
				return nil, envelope.Common(envelope.CodeOutcomeUnknown, "the adapter did not answer")
			case "changed after the commit":
				intended.Body["filler"] = strings.Repeat("x", 5000)
				held, err := c.Committed()
				if err != nil {
					return nil, err
				}
				held[0].Body["other"] = "changed"
				return nil, envelope.Common(envelope.CodeOutcomeUnknown, "the adapter did not answer")
			case "unknown with a receipt":
				rc, err := c.NewReceipt("dispatch.thing.refused", "1.0", nil, nil, nil)
				if err != nil {
					return nil, err
				}
				f := envelope.Common(envelope.CodeOutcomeUnknown, "the adapter did not answer")
				f.Receipts = []envelope.Receipt{rc}
				return nil, f
			case "refused":
				return nil, envelope.Common(envelope.CodeAuthorityMissing, "no word was given")
			case "refused with a receipt":
				rc, err := c.NewReceipt("dispatch.thing.refused", "1.0", nil, nil, nil)
				if err != nil {
					return nil, err
				}
				f := envelope.Common(envelope.CodeAuthorityMissing, "no word was given")
				f.Receipts = []envelope.Receipt{rc}
				return nil, f
			case "unavailable":
				return nil, envelope.Common(envelope.CodeDependencyMissing, "the program is gone")
			case "plain error":
				return nil, errors.New("something of its own")
			case "huge":
				return &Result{Body: map[string]any{"filler": strings.Repeat("x", 4096)}}, nil
			case "wrong revision":
				return &Result{Body: map[string]any{}, Records: []store.RecordWrite{{Path: path, Content: []byte(`{"schema":"dispatch.thing","schema_version":"1.0","name":"intended"}`), Expected: written[0].Revision + 1}}}, nil
			}
			done, err := c.NewReceipt("dispatch.thing.done", "1.0", nil, nil, nil)
			if err != nil {
				return nil, err
			}
			return &Result{
				Body:     map[string]any{"revision": written[0].Revision},
				Records:  []store.RecordWrite{{Path: path, Content: []byte(`{"schema":"dispatch.thing","schema_version":"1.0","name":"done"}`), Expected: written[0].Revision}},
				Receipts: []envelope.Receipt{done},
			}, nil
		})},
		// stamp writes one record stamped with the time Finalize gives it, and
		// answers with the digest Finalize gives, which is the digest of the
		// record as stored though the clock moves between every reading.
		{OperationDecl: decl("stamp", true), Handle: count("stamp", func(c *Call) (*Result, error) {
			rec, digest, err := c.Finalize(store.RecordWrite{Path: store.RecordPath("dispatch", "thing", "stamped"),
				Content: []byte(`{"schema":"dispatch.thing","schema_version":"1.0","name":"stamped","updated_at":"2026-10-11T07:59:59.000Z"}`)})
			if err != nil {
				return nil, err
			}
			subject := &envelope.RecordRef{Schema: "dispatch.thing", ID: "stamped", HomeID: homeID, RecordRevision: 1, Digest: digest}
			rc, err := c.NewReceipt("dispatch.thing.stamped", "1.0", subject, nil, nil)
			if err != nil {
				return nil, err
			}
			return &Result{Body: map[string]any{"digest": digest}, Records: []store.RecordWrite{rec}, Receipts: []envelope.Receipt{rc}}, nil
		})},
		// unfinalized writes one record without finalizing it, with metadata
		// that is not the store's.
		{OperationDecl: decl("raw", true), Handle: count("raw", func(c *Call) (*Result, error) {
			return &Result{Body: map[string]any{}, Records: []store.RecordWrite{{Path: store.RecordPath("dispatch", "thing", "raw"),
				Content: []byte(`{"schema":"dispatch.thing","schema_version":"1.0","name":"raw","record_revision":7,"updated_at":"2020-01-01T00:00:00.000Z"}`)}}}, nil
		})},
		// fail ends as the body's name says: a plain error, a panic, an
		// unknown outcome, a failure that breaks the contract, a body that
		// breaks the schema, no body, or records from a read-only operation.
		{OperationDecl: decl("fail", false), Handle: count("fail", func(c *Call) (*Result, error) {
			switch c.Request.Body["home"].(map[string]any)["name"] {
			case "panic":
				panic("boom")
			case "chain":
				return nil, fmt.Errorf("reading %s: %w", chainPath, fmt.Errorf("open %s: %w", chainPath, fs.ErrNotExist))
			case "unknown":
				return nil, envelope.Common(envelope.CodeOutcomeUnknown, "the adapter did not answer")
			case "bad failure":
				return nil, &envelope.Failure{Status: envelope.TooLarge, Err: envelope.Error{Code: "record_too_large", Message: "x"}}
			case "no body":
				return &Result{}, nil
			case "records":
				return &Result{Body: map[string]any{}, Records: []store.RecordWrite{{Path: "dispatch/records/x/y.json"}}}, nil
			case "package code":
				return nil, envelope.Fail(envelope.Unavailable, "dispatch.adapter_down", "the adapter is down").Retry()
			}
			return nil, errors.New("something of its own")
		})},
		// tick answers with two readings of the call's clock.
		{OperationDecl: decl("tick", false), Handle: count("tick", func(c *Call) (*Result, error) {
			a, _ := c.Stamp()
			b, _ := c.Stamp()
			return &Result{Body: map[string]any{"a": a, "b": b}}, nil
		})},
		// big answers with a body of the size the body's name gives.
		{OperationDecl: decl("big", false), Handle: count("big", func(c *Call) (*Result, error) {
			return &Result{Body: map[string]any{"filler": strings.Repeat("x", 4096)}}, nil
		})},
		// resident answers, then runs on.
		{OperationDecl: envelope.OperationDecl{Name: "serve", State: envelope.Supported, Mode: envelope.Resident, Mutates: false, Versions: []string{"1.0"},
			RequestSchema: testRequestSchema, ResponseSchema: testResponseSchema, ReceiptKinds: []string{}, Executes: []string{}, Calls: []string{}},
			Handle: count("serve", func(c *Call) (*Result, error) {
				return &Result{Body: map[string]any{"started": true}, After: func() error {
					h.calls["after"]++
					if c.Request.Body["home"].(map[string]any)["name"] == "stop badly" {
						return errors.New("the server fell over")
					}
					return nil
				}}, nil
			})},
		// provision is admitted to a home with no record and creates one.
		{OperationDecl: decl("provision", true), Provisions: true, Handle: count("provision", func(c *Call) (*Result, error) {
			return &Result{Body: map[string]any{"created": c.Home.HomeID == ""}, Records: []store.RecordWrite{{Path: store.HomeFilePath, Content: mustEncode(homeRecord(c.Request.HomeID))}}}, nil
		})},
		{OperationDecl: envelope.OperationDecl{Name: "route", State: envelope.Unsupported, Reason: "built at stage 4", Mode: envelope.Unary, Mutates: true, Versions: []string{}, ReceiptKinds: []string{}, Executes: []string{}, Calls: []string{}}},
		{OperationDecl: envelope.OperationDecl{Name: "recover", State: envelope.Parked, Reason: "held closed by decision 7", Mode: envelope.Unary, Mutates: true, Versions: []string{"1.0"}, ReceiptKinds: []string{}, Executes: []string{}, Calls: []string{}}},
	}}
	park := &Command{Name: "hive-park"}
	h.exe = &Executable{
		Package: "dispatch", Name: "hive-dispatch", Version: "0.1.0-test", SourceRevision: strings.Repeat("ab", 20),
		Commands: []*Command{{Name: "hive-dispatch"}, fleet, park},
		Clock:    h.clk,
		Waiting:  store.Waiting{Clock: h.clk, Limit: 100 * time.Millisecond, Pause: h.clk.Advance},
		Journal:  func(string) (store.Journal, error) { return h.journal, nil },
	}
	return h
}

// faultAt makes every store the executable opens fail, as a crash would, at
// the first occurrence of step, and reports whether it did. The executable
// faults until Fault is cleared.
func (h *harness) faultAt(step string) *bool {
	hit := new(bool)
	h.exe.Fault = func(name string) error {
		if name != step || *hit {
			return nil
		}
		*hit = true
		return errors.New("test: the process crashed here")
	}
	return hit
}

// expectUnknown asserts the answer of a durable transaction whose response
// cannot be given: unknown with outcome_unknown, not retryable, with no
// receipts and not a replay.
func expectUnknown(t testing.TB, resp *envelope.Response) {
	t.Helper()
	expect(t, resp, envelope.Unknown, "outcome_unknown")
	if len(resp.Receipts) != 0 || resp.Error.Retryable || resp.Replayed {
		t.Fatalf("%+v", resp)
	}
}

// floorOf is the smallest response limit the command's floor admits: the
// size of the largest outcome_unknown answer of a durable transaction of any
// of its supported mutating operations. The floor admits it for every such
// operation, and refuses one byte less for at least one.
func floorOf(t testing.TB, e *Executable, c *Command) int64 {
	t.Helper()
	var floor int64
	var mutating []*Operation
	for _, op := range c.Operations {
		if op.State != envelope.Supported || !op.Mutates {
			continue
		}
		mutating = append(mutating, op)
		for _, message := range []string{interruptedMessage, oversizeReplayMessage} {
			resp := e.unknownAnswer(c, op.Name, sampleRequestID, message)
			resp.CompletedAt = sampleTimestamp
			data, err := resp.Encode()
			if err != nil {
				t.Fatal(err)
			}
			floor = max(floor, int64(len(data)))
		}
	}
	narrow := *c
	narrow.Limits.MaxResponseBytes = floor
	for _, op := range mutating {
		if err := e.floor(&narrow, op); err != nil {
			t.Fatalf("the floor refuses its own size: %v", err)
		}
	}
	narrow.Limits.MaxResponseBytes = floor - 1
	refused := false
	for _, op := range mutating {
		refused = refused || e.floor(&narrow, op) != nil
	}
	if !refused {
		t.Fatal("the floor admits one byte less than the largest unknown answer")
	}
	return floor
}

// ticking is a clock that moves a millisecond each time it is read.
type ticking struct{ at time.Time }

func (c *ticking) Now() time.Time {
	t := c.at
	c.at = c.at.Add(time.Millisecond)
	return t
}

func mustEncode(tree map[string]any) []byte {
	out, err := canon.Encode(tree)
	if err != nil {
		panic(err)
	}
	return out
}

// run is one invocation through Main: the arguments after the command
// name, and standard input. It returns the exit code, standard output and
// standard error.
func (h *harness) run(command string, stdin []byte, args ...string) (int, []byte, string) {
	var stdout, stderr bytes.Buffer
	code := h.exe.Main(append([]string{"/opt/hive/bin/" + command}, args...), bytes.NewReader(stdin), &stdout, &stderr)
	h.stderr.Write(stderr.Bytes())
	return code, stdout.Bytes(), stderr.String()
}

// invoke runs an operation of hive-fleet on the home with the request on
// standard input and accepts the response by the caller's rule: whole,
// valid, echoing the request_id and agreeing with the exit code. It fails
// the test otherwise, so every test proves the response is one the
// contract accepts.
func (h *harness) invoke(t testing.TB, home, operation string, req []byte) *envelope.Response {
	t.Helper()
	code, out, _ := h.run("hive-fleet", req, operation, HomeOption, home)
	want, _ := envelope.LooseRequestID(req)
	if want == "" {
		want = id(ids.Request, 1)
	}
	resp, err := envelope.AcceptResponse(out, code, want)
	if err != nil {
		t.Fatalf("the response is not accepted: %v\nexit %d\n%s", err, code, out)
	}
	if !canon.IsCanonical(bytes.TrimSuffix(out, []byte("\n"))) {
		t.Fatalf("the response is not canonical: %s", out)
	}
	return resp
}

// expect asserts the response's status and code.
func expect(t testing.TB, resp *envelope.Response, status envelope.Status, code string) {
	t.Helper()
	if resp.Status != status {
		t.Fatalf("status %s (%v), want %s", resp.Status, resp.Error, status)
	}
	if resp.ExitCode() != status.ExitCode() {
		t.Fatalf("exit code %d, want %d", resp.ExitCode(), status.ExitCode())
	}
	if status == envelope.Ok {
		if resp.Error != nil || resp.Body == nil {
			t.Fatalf("ok with error %v or no body", resp.Error)
		}
		return
	}
	if resp.Body != nil {
		t.Fatalf("%s with a body", status)
	}
	if resp.Error == nil || resp.Error.Code != code {
		t.Fatalf("error %+v, want code %s", resp.Error, code)
	}
}

// tree is the state of a directory: every file by slash path below it, with
// the digest of its bytes.
func tree(t testing.TB, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		sum := sha256.Sum256(data)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// same asserts two directory states are the same.
func same(t testing.TB, a, b map[string]string) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("the home changed: %d files, then %d", len(a), len(b))
	}
	for k, v := range a {
		if b[k] != v {
			t.Fatalf("the home changed: %s", k)
		}
	}
}
