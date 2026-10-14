package entry

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/read"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The acceptance test of the Pool's store and read foundation (spec/pool.md,
// The store and Reading the Pool). It drives the executable the registry
// describes, with a test writer standing in for the operations other lanes
// build and a test reader for the kind hold, and holds the foundation to the
// contract: what a write checks before it writes a byte, what a stale
// expectation leaves, what a shared read finds after a crash, and what a read
// refuses.

// pool is a home with the Pool's executable over it.
type pool struct {
	t       testing.TB
	dir     string
	homeID  string
	reg     *Registry
	exe     *cli.Executable
	pending poolstore.Transaction
}

func mint(t testing.TB, p ids.Prefix) string {
	t.Helper()
	id, err := ids.Mint(clock.System{}, p)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// newPool makes a home and the executable: the test writer replaces the
// declared-only update, and the hold reader is registered.
func newPool(t testing.TB) *pool {
	t.Helper()
	p := &pool{t: t, dir: filepath.Join(t.TempDir(), "home"), homeID: mint(t, ids.Home), reg: NewRegistry()}
	if err := os.Mkdir(p.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writer := map[string]any{"package": "dispatch", "command": "hive-fleet", "operation": "provision", "implementation_version": "test"}
	home, err := canon.Encode(map[string]any{
		"schema": "hive.home", "schema_version": "1.0", "record_revision": int64(1),
		"home_id": p.homeID, "role": "hive", "name": "A test home", "parent": map[string]any{"kind": "none"},
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "writer": writer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.dir, "home.json"), append(home, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	p.reg.RegisterOperation("hive-pool", &cli.Operation{
		OperationDecl: envelope.OperationDecl{
			Name: "update", State: envelope.Supported, Mode: envelope.Unary, Mutates: true, Versions: []string{"1.0"},
			RequestSchema: "urn:hive:schema:ops:hive-pool.hold.request:v1", ReceiptKinds: []string{}, Executes: []string{}, Calls: []string{},
		},
		Handle: func(c *cli.Call) (*cli.Result, error) {
			staged, err := poolstore.Stage(c, p.pending)
			if err != nil {
				return nil, err
			}
			return staged.Result(map[string]any{}), nil
		},
	})
	p.reg.Reads.Register(read.Kind{
		Name:       "hold",
		Members:    []string{read.MemberID, read.MemberTaskID, read.MemberStates, read.MemberActive, read.MemberLimit},
		Standalone: func(s read.Selector) bool { return s.ID != "" },
		Read: func(c *read.Context) (*read.Result, error) {
			var res read.Result
			if c.Selector.ID != "" {
				l, om, err := poolstore.Load(c.Store, "hold", c.Selector.ID)
				switch {
				case err != nil:
					return nil, err
				case om != nil:
					res.Omitted = append(res.Omitted, *om)
				default:
					res.Records = append(res.Records, read.Record{ID: l.ID, Data: l.Data})
				}
				return &res, nil
			}
			all, omitted, err := poolstore.LoadAll(c.Store, "hold")
			if err != nil {
				return nil, err
			}
			for _, l := range all {
				res.Records = append(res.Records, read.Record{ID: l.ID, Data: l.Data})
			}
			res.Omitted = omitted
			return &res, nil
		},
	})
	p.exe = p.reg.Executable()
	return p
}

// do sends one request to the command and returns the response as the
// command wrote it.
func (p *pool) do(operation string, body map[string]any) *envelope.Response {
	p.t.Helper()
	req, err := canon.Encode(map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": "hive-pool", "operation": operation, "operation_version": "1.0",
		"request_id": mint(p.t, ids.Request), "correlation_id": mint(p.t, ids.Correlation),
		"issued_at": "2026-10-11T08:00:00.000Z", "caller": map[string]any{"kind": "test"},
		"scope_level": "hive", "home_id": p.homeID,
		"authority": map[string]any{"kind": "none"}, "body": body,
	})
	if err != nil {
		p.t.Fatal(err)
	}
	var cmd *cli.Command
	for _, c := range p.exe.Commands {
		if c.Name == "hive-pool" {
			cmd = c
		}
	}
	out, err := p.exe.Handle(cli.Invocation{Command: cmd, Operation: operation, Home: p.dir, Request: append(req, '\n')})
	if err != nil {
		p.t.Fatal(err)
	}
	resp, err := envelope.DecodeResponse(out.Encoded)
	if err != nil {
		p.t.Fatal(err)
	}
	return resp
}

// write stages tx through the test writer.
func (p *pool) write(tx poolstore.Transaction) *envelope.Response {
	p.t.Helper()
	p.pending = tx
	return p.do("update", map[string]any{"task_id": mint(p.t, ids.Task), "kind": "external", "reason": "A test."})
}

// files is every file under the directory of the home, by slash path, with
// its bytes.
func (p *pool) files(under string) map[string]string {
	p.t.Helper()
	out := map[string]string{}
	root := filepath.Join(p.dir, under)
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				p.t.Fatal(rerr)
			}
			rel, _ := filepath.Rel(p.dir, path)
			out[filepath.ToSlash(rel)] = string(data)
		}
		return nil
	})
	return out
}

// hold is a record of the kind hold that already has a fixture, made for the
// home: the same shape, with identities of its own.
func (p *pool) hold() (id string, body map[string]any) {
	p.t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas", "records_pool.hold", "valid", "active-parked.json"))
	if err != nil {
		p.t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		p.t.Fatal(err)
	}
	body = v.(map[string]any)
	id = mint(p.t, ids.Hold)
	body["hold_id"], body["task_id"], body["home_id"] = id, mint(p.t, ids.Task), p.homeID
	return id, body
}

func holdPath(id string) string { return "pool/records/hold/" + id + ".json" }

// A transaction holding one record that breaks its schema is refused whole,
// and no temporary file and no transaction file was written for it.
func TestARecordThatBreaksItsSchemaWritesNoByte(t *testing.T) {
	p := newPool(t)
	id, body := p.hold()
	delete(body, "reason")
	resp := p.write(poolstore.Transaction{Records: []poolstore.Write{{Kind: "hold", ID: id, Body: body}}})
	if resp.Status != envelope.Invalid || resp.Error == nil || resp.Error.Code != envelope.CodeSchemaViolation {
		t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
	}
	if files := p.files("pool"); len(files) != 0 {
		t.Errorf("a refused transaction left %v", slices.Sorted(mapKeys(files)))
	}
}

func mapKeys(m map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// Two writers read one revision and prepare different bodies: the first
// commits, and the second, whose expectation is stale, receives
// revision_conflict with nothing changed.
func TestAStaleRevisionIsAConflictAndChangesNothing(t *testing.T) {
	p := newPool(t)
	id, body := p.hold()
	if resp := p.write(poolstore.Transaction{Records: []poolstore.Write{{Kind: "hold", ID: id, Body: body}}}); resp.Status != envelope.Ok {
		t.Fatalf("create: %s %+v", resp.Status, resp.Error)
	}
	first := maps(body)
	first["reason"] = "The first writer's words."
	second := maps(body)
	second["reason"] = "The second writer's words."
	if resp := p.write(poolstore.Transaction{Records: []poolstore.Write{{Kind: "hold", ID: id, Body: first, Expected: 1}}}); resp.Status != envelope.Ok {
		t.Fatalf("first: %s %+v", resp.Status, resp.Error)
	}
	before := p.files("")
	resp := p.write(poolstore.Transaction{Records: []poolstore.Write{{Kind: "hold", ID: id, Body: second, Expected: 1}}})
	if resp.Status != envelope.Conflict || resp.Error == nil || resp.Error.Code != envelope.CodeRevisionConflict {
		t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
	}
	if after := p.files(""); !equal(before, after) {
		t.Errorf("a conflict changed the home: before %v, after %v", slices.Sorted(mapKeys(before)), slices.Sorted(mapKeys(after)))
	}
	if !strings.Contains(before[holdPath(id)], "The first writer's words.") {
		t.Errorf("the record does not hold the first writer's words: %s", before[holdPath(id)])
	}
}

func maps(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func equal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// A shared read after an interrupted transaction sees it completed, and
// answers with a complete projection header whose journal cursor counts the
// event the recovery appended.
func TestASharedReadRecoversAnInterruptedTransaction(t *testing.T) {
	p := newPool(t)
	id, body := p.hold()
	event, err := canon.Encode(map[string]any{
		"schema": "hive.event", "schema_version": "1.0", "event_id": mint(t, ids.Event),
		"kind": "pool.hold.placed", "kind_version": "1.0",
		"source":      map[string]any{"package": "pool", "source_id": "pool"},
		"scope_level": "hive", "home_id": p.homeID, "correlation_id": mint(t, ids.Correlation),
		"occurred_at": "2026-10-11T08:00:00.000Z", "observed_at": "2026-10-11T08:00:00.000Z",
		"basis": map[string]any{"kind": "record", "ref": map[string]any{"schema": "pool.hold", "id": id}},
		"body":  map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	p.exe.Fault = func(step string) error {
		if strings.HasPrefix(step, "rename pool/records/") {
			return os.ErrClosed
		}
		return nil
	}
	resp := p.write(poolstore.Transaction{
		Records: []poolstore.Write{{Kind: "hold", ID: id, Body: body}},
		Events:  []store.Event{{ID: mint(t, ids.Event), Content: event}},
	})
	p.exe.Fault = nil
	if resp.Status != envelope.Unknown {
		t.Fatalf("an interrupted write is %s, not unknown", resp.Status)
	}
	if _, err := os.Stat(filepath.Join(p.dir, filepath.FromSlash(holdPath(id)))); err == nil {
		t.Fatal("the record was written before the crash")
	}
	resp = p.do("read", map[string]any{"kind": "hold", "selector": map[string]any{"active": true}})
	if resp.Status != envelope.Ok {
		t.Fatalf("read: %s %+v", resp.Status, resp.Error)
	}
	header, _ := resp.Body["projection"].(map[string]any)
	if header["home_id"] != p.homeID || header["complete"] != true || header["journal_cursor"] != int64(1) || !clock.Valid(header["read_at"].(string)) {
		t.Errorf("projection header %v", header)
	}
	records, _ := resp.Body["records"].([]any)
	if len(records) != 1 || records[0].(map[string]any)["hold_id"] != id || resp.Body["kind"] != "hold" {
		t.Errorf("records %v", resp.Body["records"])
	}
	if files := p.files("pool/tx"); len(files) != 0 {
		t.Errorf("the intent is still there: %v", slices.Sorted(mapKeys(files)))
	}
}

// A kind with no built reader is refused and never answered as empty, and a
// selector member that does not apply to the kind is refused by the read.
func TestAReadRefusesWhatItCannotAnswer(t *testing.T) {
	p := newPool(t)
	for _, tc := range []struct {
		name     string
		kind     string
		selector map[string]any
		status   envelope.Status
		code     string
	}{
		{"a kind with no reader", "item", map[string]any{"ready": true}, envelope.Refused, envelope.CodeUnsupportedOperation},
		{"a kind with no reader, by identity", "question", map[string]any{"id": mint(t, ids.Question)}, envelope.Refused, envelope.CodeUnsupportedOperation},
		{"a task identity on request-outcome", "request-outcome", map[string]any{"task_id": mint(t, ids.Task)}, envelope.Invalid, envelope.CodeSchemaViolation},
		{"ready on holds", "hold", map[string]any{"ready": true}, envelope.Invalid, envelope.CodeSchemaViolation},
		{"due_before on a kind that did not take it", "hold", map[string]any{"due_before": "2026-10-12T11:00:00.000Z"}, envelope.Invalid, envelope.CodeSchemaViolation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := p.do("read", map[string]any{"kind": tc.kind, "selector": tc.selector})
			if resp.Status != tc.status || resp.Error == nil || resp.Error.Code != tc.code || resp.Body != nil {
				t.Errorf("status %s, error %+v, body %v", resp.Status, resp.Error, resp.Body)
			}
		})
	}
	if files := p.files("pool"); len(files) != 0 {
		t.Errorf("a refused read left %v", slices.Sorted(mapKeys(files)))
	}
}
