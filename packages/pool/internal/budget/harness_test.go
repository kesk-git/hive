package budget_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/pool/internal/budget"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
)

// The identities the record fixtures of contract/testdata share, so that the
// fixtures seed a home as they stand.
const (
	homeID      = "home_01jc7d8e9f0g1h2j3k4m5n6p7a"
	workspaceID = "ws_01jc7d8e9f0g1h2j3k4m5n6p7b"
	projectID   = "proj_01jc7d8e9f0g1h2j3k4m5n6p7c"
	questionID  = "q_01jc7d8e9f0g1h2j3k4m5n6p7q"
)

var start = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// home is a home with the Pool's executable over it.
type home struct {
	t   testing.TB
	dir string
	exe *cli.Executable
}

func mint(t testing.TB, p ids.Prefix) string {
	t.Helper()
	id, err := ids.Mint(clock.System{}, p)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func newHome(t testing.TB) *home {
	t.Helper()
	h := &home{t: t, dir: filepath.Join(t.TempDir(), "home")}
	if err := os.Mkdir(h.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	h.write("home.json", map[string]any{
		"schema": "hive.home", "schema_version": "1.0", "record_revision": int64(1),
		"home_id": homeID, "role": "hive", "name": "A test home", "parent": map[string]any{"kind": "none"},
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z",
		"writer": map[string]any{"package": "pool", "command": "hive-pool", "operation": "provision", "implementation_version": "test"},
	})
	registry := map[string]any{"schema_version": "1.0", "record_revision": int64(1), "home_id": homeID, "state": "active",
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "slug": "registered", "name": "Registered",
		"writer": map[string]any{"package": "dispatch", "command": "hive-fleet", "operation": "provision", "implementation_version": "test"}}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range registry {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	h.write("dispatch/registry/workspaces/"+workspaceID+".json", with(map[string]any{
		"schema": "hive.workspace", "workspace_id": workspaceID, "parent_home_id": mint(t, ids.Home)}))
	h.write("dispatch/registry/projects/"+projectID+".json", with(map[string]any{
		"schema": "hive.project", "project_id": projectID, "workspace_id": workspaceID, "repository": map[string]any{"kind": "local-only"}}))
	h.exe = executable()
	h.exe.Clock = clock.NewFake(start)
	return h
}

// write writes a file of the home, as the canonical form of the record.
func (h *home) write(rel string, rec map[string]any) {
	h.t.Helper()
	data, err := canon.Encode(rec)
	if err != nil {
		h.t.Fatal(err)
	}
	path := filepath.Join(h.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

// fixture reads a valid record fixture of the contract's schemas.
func fixture(t testing.TB, schema, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas", "records_"+schema, "valid", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)
}

// plant stands a fixture in the Pool as a record of another lane would.
func (h *home) plant(kind, id string, rec map[string]any) {
	h.t.Helper()
	h.write("pool/records/"+kind+"/"+id+".json", rec)
}

// stored reads a record file as the store holds it.
func (h *home) stored(kind, id string) map[string]any {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, "pool", "records", kind, id+".json"))
	if err != nil {
		h.t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		h.t.Fatal(err)
	}
	return v.(map[string]any)
}

// records is every file under pool/ but the replay records of requests, by
// slash path, with its bytes.
func (h *home) records() map[string]string {
	h.t.Helper()
	out := map[string]string{}
	filepath.WalkDir(filepath.Join(h.dir, "pool"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				h.t.Fatal(rerr)
			}
			rel, _ := filepath.Rel(h.dir, path)
			if rel = filepath.ToSlash(rel); !strings.HasPrefix(rel, "pool/requests/") {
				out[rel] = string(data)
			}
		}
		return nil
	})
	return out
}

// unchanged runs f and fails if it changed anything under pool/.
func (h *home) unchanged(f func()) {
	h.t.Helper()
	before := h.records()
	f()
	after := h.records()
	for k, v := range before {
		if w, ok := after[k]; !ok || w != v {
			h.t.Errorf("the Pool changed at %s", k)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			h.t.Errorf("the Pool gained %s", k)
		}
	}
}

// The scopes a request is made at.
var (
	hiveScope    = map[string]any{"scope_level": "hive", "home_id": homeID}
	projectScope = map[string]any{"scope_level": "project", "home_id": homeID, "workspace_id": workspaceID, "project_id": projectID}
)

// rev is the revision of a record a caller read, as the request names it.
type rev struct {
	schema, id string
	revision   int64
}

// request is how a request is made: the scope it is made at, the authority it
// carries and the revisions it expects.
type request struct {
	scope     map[string]any
	authority map[string]any
	expected  []rev
}

// word is the authority of kind word that names the question at a record
// revision.
func word(revision int64) map[string]any {
	return authority("word", "pool.question", questionID, revision)
}

func authority(kind, schema, id string, revision int64) map[string]any {
	if kind == "none" {
		return map[string]any{"kind": "none"}
	}
	return map[string]any{"kind": kind, "ref": map[string]any{"schema": schema, "id": id, "home_id": homeID, "record_revision": revision}}
}

// budget sends hive-memory budget.
func (h *home) budget(r request, projections []map[string]any, intake map[string]any) *envelope.Response {
	h.t.Helper()
	list := []any{}
	for _, p := range projections {
		list = append(list, p)
	}
	return h.send("hive-memory", "budget", map[string]any{"projections": list, "intake": intake}, r)
}

func (h *home) send(command, operation string, body map[string]any, r request) *envelope.Response {
	h.t.Helper()
	if r.scope == nil {
		r.scope = hiveScope
	}
	req := map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": command, "operation": operation, "operation_version": "1.0",
		"request_id": mint(h.t, ids.Request), "correlation_id": mint(h.t, ids.Correlation),
		"issued_at": "2026-10-14T09:00:00.000Z", "caller": map[string]any{"kind": "test"},
		"authority": r.authority, "body": body,
	}
	for k, v := range r.scope {
		req[k] = v
	}
	if len(r.expected) > 0 {
		list := []any{}
		for _, e := range r.expected {
			list = append(list, map[string]any{"schema": e.schema, "id": e.id, "record_revision": e.revision})
		}
		req["expected_revisions"] = list
	}
	data, err := canon.Encode(req)
	if err != nil {
		h.t.Fatal(err)
	}
	var cmd *cli.Command
	for _, c := range h.exe.Commands {
		if c.Name == command {
			cmd = c
		}
	}
	out, err := h.exe.Handle(cli.Invocation{Command: cmd, Operation: operation, Home: h.dir, Request: append(data, '\n')})
	if err != nil {
		h.t.Fatal(err)
	}
	resp, err := envelope.DecodeResponse(out.Encoded)
	if err != nil {
		h.t.Fatal(err)
	}
	return resp
}

// refused checks the status and the code of a response that must not be ok.
func refused(t testing.TB, resp *envelope.Response, status envelope.Status, code string) {
	t.Helper()
	if resp.Status != status || resp.Error == nil || resp.Error.Code != code {
		t.Fatalf("want %s %s; got status %s, error %+v", status, code, resp.Status, resp.Error)
	}
	if resp.Body != nil {
		t.Errorf("a refusal carries a body: %v", resp.Body)
	}
	if len(resp.Receipts) != 0 {
		t.Errorf("a refusal carries receipts: %v", resp.Receipts)
	}
}

// reasons are the reasons a refusal names for the word not standing.
func reasons(resp *envelope.Response) []string {
	out := []string{}
	if resp.Error != nil {
		list, _ := resp.Error.Detail["reasons"].([]any)
		for _, v := range list {
			s, _ := v.(string)
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}

// executable is the Pool's executable with hive-memory budget built on the
// budget writer, as the later lane that builds the renderer registers it. The
// registry is the test's own: the delivered executable does not build it.
func executable() *cli.Executable {
	r := entry.NewRegistry()
	r.RegisterOperation("hive-memory", &cli.Operation{
		OperationDecl: envelope.OperationDecl{
			Name: "budget", State: envelope.Supported, Mode: envelope.Unary, Mutates: true, Versions: []string{"1.0"},
			RequestSchema:  "urn:hive:schema:ops:hive-memory.budget.request:v1",
			ResponseSchema: "urn:hive:schema:ops:hive-memory.budget.response:v1",
			ReceiptKinds:   []string{"pool.budget.changed"}, Executes: []string{}, Calls: []string{},
		},
		Handle: budget.Apply,
	})
	return r.Executable()
}

func equal(a, b any) bool {
	switch a := a.(type) {
	case map[string]any:
		b, ok := b.(map[string]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for k, v := range a {
			if !equal(v, b[k]) {
				return false
			}
		}
		return true
	case []any:
		b, ok := b.([]any)
		return ok && slices.EqualFunc(a, b, equal)
	}
	return a == b
}
