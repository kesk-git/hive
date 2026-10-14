package entry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/declare"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/read"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

func operation(e *cli.Executable, command, name string) *cli.Operation {
	for _, c := range e.Commands {
		for _, op := range c.Operations {
			if c.Name == command && op.Name == name {
				return op
			}
		}
	}
	return nil
}

func panics(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: registered", name)
		}
	}()
	f()
}

// hive-pool read is declared supported when a kind has a reader and not
// before, the executable stays well formed and agrees with the declaration,
// and a registration that cannot be right stops the program.
func TestReadIsBuiltOnceAKindHasAReader(t *testing.T) {
	r := NewRegistry()
	if op := operation(r.Executable(), "hive-pool", "read"); op.State != envelope.Unsupported {
		t.Errorf("read with no reader is %s", op.State)
	}
	r.Reads.Register(read.Kind{Name: "hold", Members: []string{read.MemberID}, Read: func(*read.Context) (*read.Result, error) { return &read.Result{}, nil }})
	e := r.Executable()
	if op := operation(e, "hive-pool", "read"); op.State != envelope.Supported || op.Mutates {
		t.Errorf("read with a reader is %s, mutates %v", op.State, op.Mutates)
	}
	if err := e.Check(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "hive-package.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := declare.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if ps := (&declare.Set{Packages: []*declare.Package{p, wordDeclaration(t)}}).Agree(e); len(ps) != 0 {
		t.Fatal(ps)
	}

	built := &cli.Operation{OperationDecl: envelope.OperationDecl{Name: "update", State: envelope.Supported}, Handle: func(*cli.Call) (*cli.Result, error) { return nil, nil }}
	r.RegisterOperation("hive-pool", built)
	if operation(r.Executable(), "hive-pool", "update") != built {
		t.Error("the registered operation is not the executable's")
	}
	panics(t, "an operation built twice", func() { r.RegisterOperation("hive-pool", built) })
	panics(t, "an operation not declared", func() {
		r.RegisterOperation("hive-pool", &cli.Operation{OperationDecl: envelope.OperationDecl{Name: "nothing", State: envelope.Supported}, Handle: built.Handle})
	})
	panics(t, "a command not declared", func() { r.RegisterOperation("hive-nothing", built) })
	panics(t, "an operation not supported", func() {
		r.RegisterOperation("hive-pool", &cli.Operation{OperationDecl: envelope.OperationDecl{Name: "add", State: envelope.Unsupported}})
	})
	panics(t, "read, which the readers build", func() {
		r.RegisterOperation("hive-pool", &cli.Operation{OperationDecl: envelope.OperationDecl{Name: "read", State: envelope.Supported}, Handle: built.Handle})
	})
}

// A write whose transaction is not one the Pool can write leaves the store as
// it found it: a credential shape in a record, an event of another package, an
// event with a body its kind does not register, a record created over one that
// is there.
func TestAWriteThatCannotBeWrittenLeavesNothing(t *testing.T) {
	p := newPool(t)
	id, body := p.hold()
	event := func(kind, source string, eventBody map[string]any) store.Event {
		eventID := mint(t, ids.Event)
		content, err := canon.Encode(map[string]any{
			"schema": "hive.event", "schema_version": "1.0", "event_id": eventID,
			"kind": kind, "kind_version": "1.0", "source": map[string]any{"package": source, "source_id": source},
			"scope_level": "hive", "home_id": p.homeID, "correlation_id": mint(t, ids.Correlation),
			"occurred_at": "2026-10-11T08:00:00.000Z", "observed_at": "2026-10-11T08:00:00.000Z",
			"basis": map[string]any{"kind": "record", "ref": map[string]any{"schema": "pool.hold", "id": id}},
			"body":  eventBody,
		})
		if err != nil {
			t.Fatal(err)
		}
		return store.Event{ID: eventID, Content: content}
	}
	shaped := map[string]any{}
	for k, v := range body {
		shaped[k] = v
	}
	shaped["reason"] = "the token ghp_" + strings.Repeat("a", 36)
	for _, tc := range []struct {
		name   string
		tx     poolstore.Transaction
		status envelope.Status
		code   string
	}{
		{"a credential shape", poolstore.Transaction{Records: []poolstore.Write{{Kind: "hold", ID: id, Body: shaped}}}, envelope.Invalid, envelope.CodeSecretShapeRecognized},
		{"an event of another package", poolstore.Transaction{Records: []poolstore.Write{{Kind: "hold", ID: id, Body: body}}, Events: []store.Event{event("dispatch.session.opened", "dispatch", map[string]any{})}}, envelope.Failed, envelope.CodeInternal},
		{"an event with a body its kind does not register", poolstore.Transaction{Records: []poolstore.Write{{Kind: "hold", ID: id, Body: body}}, Events: []store.Event{event("pool.hold.placed", "pool", map[string]any{"extra": "x"})}}, envelope.Failed, envelope.CodeInternal},
		{"a kind the Pool does not store", poolstore.Transaction{Records: []poolstore.Write{{Kind: "session", ID: id, Body: body}}}, envelope.Failed, envelope.CodeInternal},
		{"a record named twice", poolstore.Transaction{Records: []poolstore.Write{{Kind: "hold", ID: id, Body: body}, {Kind: "hold", ID: id, Body: body}}}, envelope.Failed, envelope.CodeInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := p.write(tc.tx)
			if resp.Status != tc.status || resp.Error == nil || resp.Error.Code != tc.code {
				t.Errorf("status %s, error %+v", resp.Status, resp.Error)
			}
			if files := p.files("pool"); len(files) != 0 {
				t.Errorf("left %d files", len(files))
			}
		})
	}
	if resp := p.write(poolstore.Transaction{Records: []poolstore.Write{{Kind: "hold", ID: id, Body: body}}}); resp.Status != envelope.Ok {
		t.Fatalf("%s %+v", resp.Status, resp.Error)
	}
	before := p.files("pool")
	resp := p.write(poolstore.Transaction{Records: []poolstore.Write{{Kind: "hold", ID: id, Body: body}}})
	if resp.Status != envelope.Conflict || resp.Error == nil || resp.Error.Code != envelope.CodeAlreadyExists || !equal(before, p.files("pool")) {
		t.Errorf("a record created over one that is there: %s %+v", resp.Status, resp.Error)
	}
}

// A read of an identity that has no record is not_found, a record that cannot
// be read is named and the read says it is not whole whether it was asked for
// by identity or among a list, and a limit that cuts a list says so.
func TestAReadNamesWhatItCouldNotReturn(t *testing.T) {
	p := newPool(t)
	id, body := p.hold()
	if resp := p.write(poolstore.Transaction{Records: []poolstore.Write{{Kind: "hold", ID: id, Body: body}}}); resp.Status != envelope.Ok {
		t.Fatalf("%s %+v", resp.Status, resp.Error)
	}
	broken := filepath.Join(p.dir, "pool", "records", "hold", "hold_broken.json")
	if err := os.WriteFile(broken, []byte("{\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if resp := p.do("read", map[string]any{"kind": "hold", "selector": map[string]any{"id": "hold_absent"}}); resp.Status != envelope.NotFound || resp.Error.Code != envelope.CodeNotFound {
		t.Errorf("an absent record: %s %+v", resp.Status, resp.Error)
	}
	expect := func(selector map[string]any, records int, omitted ...string) {
		t.Helper()
		resp := p.do("read", map[string]any{"kind": "hold", "selector": selector})
		if resp.Status != envelope.Ok {
			t.Fatalf("%v: %s %+v", selector, resp.Status, resp.Error)
		}
		header := resp.Body["projection"].(map[string]any)
		list, _ := header["omitted"].([]any)
		var got []string
		for _, o := range list {
			got = append(got, o.(map[string]any)["id"].(string)+" "+o.(map[string]any)["reason"].(string))
		}
		if n := len(resp.Body["records"].([]any)); n != records || header["complete"] != (len(omitted) == 0) || strings.Join(got, ",") != strings.Join(omitted, ",") {
			t.Errorf("%v: %d records, header %v", selector, n, header)
		}
	}
	expect(map[string]any{"active": true}, 1, "hold_broken record_malformed")
	expect(map[string]any{"id": "hold_broken"}, 0, "hold_broken record_malformed")
	expect(map[string]any{"id": id}, 1)
	expect(map[string]any{"limit": 1}, 1, "hold_broken record_malformed")
}

// A record that stands alone is read with no lock and any other read takes the
// lock shared: with the lock held by a writer, the first still answers and the
// second waits its bounded time and is lock_held.
func TestOnlyARecordThatStandsAloneIsReadWithoutTheLock(t *testing.T) {
	p := newPool(t)
	id, body := p.hold()
	if resp := p.write(poolstore.Transaction{Records: []poolstore.Write{{Kind: "hold", ID: id, Body: body}}}); resp.Status != envelope.Ok {
		t.Fatalf("%s %+v", resp.Status, resp.Error)
	}
	p.exe.Waiting = store.Waiting{Limit: 20 * time.Millisecond}
	lock, err := store.Acquire(p.dir, "pool", false, store.Waiting{})
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if resp := p.do("read", map[string]any{"kind": "hold", "selector": map[string]any{"id": id}}); resp.Status != envelope.Ok {
		t.Errorf("a record that stands alone: %s %+v", resp.Status, resp.Error)
	}
	resp := p.do("read", map[string]any{"kind": "hold", "selector": map[string]any{"active": true}})
	if resp.Status != envelope.Conflict || resp.Error == nil || resp.Error.Code != envelope.CodeLockHeld {
		t.Errorf("a list: %s %+v", resp.Status, resp.Error)
	}
}
