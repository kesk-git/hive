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
	build   func(*cli.Call) (poolstore.Transaction, error)
}

func mint(t testing.TB, p ids.Prefix) string {
	t.Helper()
	id, err := ids.Mint(clock.System{}, p)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// put puts the file at path in the home with the content given, through the
// store's own primitives, so that the directories above it and the file are
// owner-only and owned by the user, as Hive makes them. A file that is there
// is emptied first.
func (p *pool) put(path string, data []byte) {
	p.t.Helper()
	if err := store.MakeDir(filepath.Dir(path)); err != nil {
		p.t.Fatal(err)
	}
	f, err := store.CreateFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		p.t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		p.t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		p.t.Fatal(err)
	}
}

// newPool makes a home and the executable: the test writer replaces the
// declared-only update, and the hold reader is registered.
func newPool(t testing.TB) *pool {
	t.Helper()
	p := &pool{t: t, dir: filepath.Join(t.TempDir(), "home"), homeID: mint(t, ids.Home), reg: NewRegistry()}
	// The root is made and secured first, as Hive makes a home, so that every
	// entry made in it is owner-only and owned by the user.
	if err := store.MakeDir(p.dir); err != nil {
		t.Fatal(err)
	}
	if err := store.SecureDir(p.dir); err != nil {
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
	p.put(filepath.Join(p.dir, "home.json"), append(home, '\n'))
	p.reg.RegisterOperation("hive-pool", &cli.Operation{
		OperationDecl: envelope.OperationDecl{
			Name: "update", State: envelope.Supported, Mode: envelope.Unary, Mutates: true, Versions: []string{"1.0"},
			RequestSchema: "urn:hive:schema:ops:hive-pool.hold.request:v1", ReceiptKinds: []string{}, Executes: []string{}, Calls: []string{},
		},
		Handle: func(c *cli.Call) (*cli.Result, error) {
			tx := p.pending
			if p.build != nil {
				var err error
				if tx, err = p.build(c); err != nil {
					return nil, err
				}
			}
			staged, err := poolstore.Stage(c, tx)
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
			rel, _ := filepath.Rel(p.dir, path)
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, "locks/") {
				// A lock file is listed, never opened: the store may hold it,
				// and on Windows a plain open of a held lock file is refused.
				out[rel] = ""
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				p.t.Fatal(rerr)
			}
			out[rel] = string(data)
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

// holdPlaced is a pool.hold.placed event of the home for the hold, as the
// journal will take it.
func (p *pool) holdPlaced(holdID string) (string, map[string]any) {
	p.t.Helper()
	eventID := mint(p.t, ids.Event)
	return eventID, map[string]any{
		"schema": "hive.event", "schema_version": "1.0", "event_id": eventID,
		"kind": "pool.hold.placed", "kind_version": "1.0",
		"source":      map[string]any{"package": "pool", "source_id": "pool"},
		"scope_level": "hive", "home_id": p.homeID, "correlation_id": mint(p.t, ids.Correlation),
		"occurred_at": "2026-10-11T08:00:00.000Z", "observed_at": "2026-10-11T08:00:00.000Z",
		"basis": map[string]any{"kind": "record", "ref": map[string]any{"schema": "pool.hold", "id": holdID}},
		"body":  map[string]any{},
	}
}

// writeEvent stages a transaction of the event alone and returns the response.
func (p *pool) writeEvent(id string, event map[string]any) *envelope.Response {
	p.t.Helper()
	content, err := canon.Encode(event)
	if err != nil {
		p.t.Fatal(err)
	}
	return p.write(poolstore.Transaction{Events: []store.Event{{ID: id, Content: content}}})
}

// writeReceipt stages a transaction of one receipt of the kind alone, made
// for the call, and returns the response.
func (p *pool) writeReceipt(kind string, body map[string]any) *envelope.Response {
	p.t.Helper()
	p.build = func(c *cli.Call) (poolstore.Transaction, error) {
		r, err := c.NewReceipt(kind, "1.0", nil, nil, body)
		return poolstore.Transaction{Receipts: []envelope.Receipt{r}}, err
	}
	defer func() { p.build = nil }()
	return p.do("update", map[string]any{"task_id": mint(p.t, ids.Task), "kind": "external", "reason": "A test."})
}

// A receipt is held to the body schema its kind registers, and to an empty
// body where it registers none, before any byte is written.
func TestAReceiptBodyIsHeldToItsKind(t *testing.T) {
	folded := func() map[string]any {
		return map[string]any{
			"queue_revision_before": int64(1), "queue_revision_after": int64(2),
			"notes": []any{}, "facts": []any{}, "items": []any{}, "remaining": int64(0),
		}
	}
	tests := []struct {
		name string
		kind string
		body map[string]any
		code string
	}{
		{"a folded receipt with its body", "pool.memory.folded", folded(), ""},
		{"a folded receipt with an empty body", "pool.memory.folded", map[string]any{}, envelope.CodeSchemaViolation},
		{"a folded receipt missing a member", "pool.memory.folded", func() map[string]any { b := folded(); delete(b, "remaining"); return b }(), envelope.CodeSchemaViolation},
		{"a receipt of an empty-body kind with a body", "pool.item.added", map[string]any{"x": "y"}, envelope.CodeInternal},
		{"a receipt of an empty-body kind with none", "pool.item.added", map[string]any{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newPool(t)
			resp := p.writeReceipt(tc.kind, tc.body)
			if tc.code == "" {
				if resp.Status != envelope.Ok {
					t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
				}
				return
			}
			if resp.Status == envelope.Ok || resp.Error == nil || resp.Error.Code != tc.code {
				t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
			}
			if files := p.files("pool"); len(files) != 0 {
				t.Errorf("a refused transaction left %v", slices.Sorted(mapKeys(files)))
			}
		})
	}
}

// An event the journal would refuse is refused when it is staged, before the
// intent exists: another home's home_id, a timestamp that is no instant, and
// an unknown occurred_at that differs from observed_at.
func TestAnEventTheJournalWouldRefuseIsRefusedBeforeTheIntent(t *testing.T) {
	tests := []struct {
		name   string
		change func(p *pool, e map[string]any)
		ok     bool
	}{
		{"as the journal takes it", func(*pool, map[string]any) {}, true},
		{"another home's home_id", func(_ *pool, e map[string]any) { e["home_id"] = mint(t, ids.Home) }, false},
		{"an occurred_at that is no instant", func(_ *pool, e map[string]any) { e["occurred_at"] = "2026-02-30T08:00:00.000Z" }, false},
		{"an observed_at that is no instant", func(_ *pool, e map[string]any) { e["observed_at"] = "2026-13-11T08:00:00.000Z" }, false},
		{"an unknown occurred_at that differs", func(_ *pool, e map[string]any) {
			e["occurred_at_known"], e["observed_at"] = false, "2026-10-11T08:00:01.000Z"
		}, false},
		{"an unknown occurred_at that repeats observed_at", func(_ *pool, e map[string]any) { e["occurred_at_known"] = false }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newPool(t)
			id, event := p.holdPlaced(mint(t, ids.Hold))
			tc.change(p, event)
			resp := p.writeEvent(id, event)
			if tc.ok {
				if resp.Status != envelope.Ok {
					t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
				}
				return
			}
			if resp.Status == envelope.Ok {
				t.Fatal("the event was staged")
			}
			if files := p.files("pool"); len(files) != 0 {
				t.Errorf("a refused transaction left %v", slices.Sorted(mapKeys(files)))
			}
		})
	}
}

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
	eventID, tree := p.holdPlaced(id)
	event, err := canon.Encode(tree)
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
		Events:  []store.Event{{ID: eventID, Content: event}},
	})
	p.exe.Fault = nil
	if resp.Status != envelope.Unknown {
		t.Fatalf("an interrupted write is %s, not unknown: %+v", resp.Status, resp.Error)
	}
	if _, err := os.Stat(filepath.Join(p.dir, filepath.FromSlash(holdPath(id)))); err == nil {
		t.Fatal("the record was written before the crash")
	}
	resp = p.do("read", map[string]any{"kind": "hold", "selector": map[string]any{"active": true}})
	if resp.Status != envelope.Ok {
		t.Fatalf("read: %s %+v", resp.Status, resp.Error)
	}
	header, _ := resp.Body["projection"].(map[string]any)
	lines, placed := 0, false
	for _, text := range p.files("journal") {
		lines += strings.Count(text, "\n")
		placed = placed || strings.Contains(text, `"kind":"pool.hold.placed"`)
	}
	if !placed {
		t.Error("the recovery did not append the event of the transaction")
	}
	if header["home_id"] != p.homeID || header["complete"] != true || header["journal_cursor"] != int64(lines) || lines == 0 || !clock.Valid(header["read_at"].(string)) {
		t.Errorf("projection header %v over a journal of %d lines", header, lines)
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
