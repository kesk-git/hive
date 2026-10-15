package specification_test

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
	"github.com/kesk-git/hive/packages/pool/internal/entry"
	_ "github.com/kesk-git/hive/packages/pool/internal/item"
	_ "github.com/kesk-git/hive/packages/pool/internal/specification"
)

var start = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// home is a home with the Pool's executable over it and a clock that moves
// only when told to.
type home struct {
	t      testing.TB
	dir    string
	homeID string
	clk    *clock.Fake
	exe    *cli.Executable
	// correlation is the one correlation of every request, so that a request
	// sent again is the same request.
	correlation string
}

func mint(t testing.TB, p ids.Prefix) string {
	t.Helper()
	id, err := ids.Mint(clock.System{}, p)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func writer(operation string) map[string]any {
	return map[string]any{"package": "pool", "command": "hive-pool", "operation": operation, "implementation_version": "test"}
}

func newHome(t testing.TB) *home {
	t.Helper()
	h := &home{t: t, dir: filepath.Join(t.TempDir(), "home"), homeID: mint(t, ids.Home), clk: clock.NewFake(start), correlation: mint(t, ids.Correlation)}
	if err := os.Mkdir(h.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := canon.Encode(map[string]any{
		"schema": "hive.home", "schema_version": "1.0", "record_revision": int64(1),
		"home_id": h.homeID, "role": "hive", "name": "A test home", "parent": map[string]any{"kind": "none"},
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "writer": writer("provision"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dir, "home.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	h.exe = entry.Executable()
	h.exe.Clock = h.clk
	return h
}

// rev is the revision of a record a caller read, as the request names it.
type rev struct {
	schema, id string
	revision   int64
}

// operationVersion is the operation version a request to the operation
// carries: define is declared at the contract's 1.2, accept and close at 1.0.
func operationVersion(command, operation string) string {
	if command == "hive-spec" && operation == "define" {
		return "1.2"
	}
	return "1.0"
}

// send sends one request of the command and returns the response as it was
// written. A request_id of "" is minted.
func (h *home) send(requestID, command, operation string, body map[string]any, expected ...rev) *envelope.Response {
	h.t.Helper()
	if requestID == "" {
		requestID = mint(h.t, ids.Request)
	}
	req := map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": command, "operation": operation, "operation_version": operationVersion(command, operation),
		"request_id": requestID, "correlation_id": h.correlation,
		"issued_at": "2026-10-14T09:00:00.000Z", "caller": map[string]any{"kind": "test"},
		"scope_level": "hive", "home_id": h.homeID,
		"authority": map[string]any{"kind": "none"}, "body": body,
	}
	if len(expected) > 0 {
		list := []any{}
		for _, e := range expected {
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

func (h *home) pool(operation string, body map[string]any, expected ...rev) *envelope.Response {
	h.t.Helper()
	return h.send("", "hive-pool", operation, body, expected...)
}

func (h *home) spec(operation string, body map[string]any, expected ...rev) *envelope.Response {
	h.t.Helper()
	return h.send("", "hive-spec", operation, body, expected...)
}

// ok fails the test unless the response is ok.
func (h *home) ok(resp *envelope.Response) map[string]any {
	h.t.Helper()
	if resp.Status != envelope.Ok {
		h.t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
	}
	return resp.Body
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
}

// kinds are the receipt kinds of a response, in order.
func kinds(resp *envelope.Response) []string {
	var out []string
	for _, r := range resp.Receipts {
		out = append(out, r.Kind)
	}
	return out
}

func count(list []string, want string) int {
	n := 0
	for _, k := range list {
		if k == want {
			n++
		}
	}
	return n
}

// files is every file under the directory of the home, by slash path, with its
// bytes.
func (h *home) files(under string) map[string]string {
	h.t.Helper()
	out := map[string]string{}
	filepath.WalkDir(filepath.Join(h.dir, under), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				h.t.Fatal(rerr)
			}
			rel, _ := filepath.Rel(h.dir, path)
			out[filepath.ToSlash(rel)] = string(data)
		}
		return nil
	})
	return out
}

// quiet runs f and fails if it changed anything under pool/ but the replay
// records of requests.
func (h *home) quiet(f func()) {
	h.t.Helper()
	snap := func() map[string]string {
		out := h.files("pool")
		for k := range out {
			if strings.HasPrefix(k, "pool/requests/") {
				delete(out, k)
			}
		}
		return out
	}
	before := snap()
	f()
	after := snap()
	if len(before) != len(after) {
		h.t.Errorf("the Pool changed: %d files before, %d after", len(before), len(after))
		return
	}
	for k, v := range before {
		if after[k] != v {
			h.t.Errorf("the Pool changed: %s", k)
		}
	}
}

// stored reads the record file as the store holds it.
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

// bytes is the record file as the store holds it, for a comparison that
// nothing of it changed.
func (h *home) bytes(kind, id string) string {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, "pool", "records", kind, id+".json"))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(data)
}

// plant writes a record directly, as a record of another lane or an earlier
// build would stand in the store.
func (h *home) plant(kind, id string, rec map[string]any) {
	h.t.Helper()
	data, err := canon.Encode(rec)
	if err != nil {
		h.t.Fatal(err)
	}
	dir := filepath.Join(h.dir, "pool", "records", kind)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), append(data, '\n'), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

// activeHolds are the active holds of the kind on the item.
func (h *home) activeHolds(taskID, kind string) []string {
	h.t.Helper()
	var out []string
	for path, text := range h.files("pool/records/hold") {
		v, err := canon.Parse([]byte(text))
		if err != nil {
			h.t.Fatal(err)
		}
		rec := v.(map[string]any)
		if rec["task_id"] == taskID && rec["kind"] == kind && rec["state"] == "active" {
			out = append(out, path)
		}
	}
	slices.Sort(out)
	return out
}

// The bodies of an add.

func target(title string, needsSpecification bool) map[string]any {
	b := map[string]any{"title": title, "level": "target"}
	if needsSpecification {
		b["specification_required"] = true
	}
	return b
}

func under(level, title, parent string) map[string]any {
	b := map[string]any{"title": title, "level": level, "deps": []any{map[string]any{"type": "parent", "task_id": parent}}}
	if level != "target" {
		b["deliverable"] = "swarm"
	}
	return b
}

func (h *home) add(body map[string]any) map[string]any {
	h.t.Helper()
	return h.ok(h.pool("add", body))["item"].(map[string]any)
}

func (h *home) addID(body map[string]any) string { return h.add(body)["task_id"].(string) }

// move transitions the item to the state.
func (h *home) move(id, to string) {
	h.t.Helper()
	h.ok(h.pool("transition", map[string]any{"task_id": id, "to": to}))
}

// item is the stored item.
func (h *home) item(id string) map[string]any { return h.stored("item", id) }

// The bodies of a define and an accept.

func criterion(id, text, evidence string) map[string]any {
	return map[string]any{"criterion_id": id, "text": text, "evidence": evidence}
}

// criteria are the two criteria the tests start from.
func criteria() []any {
	return []any{
		criterion("resets", "The counter resets after its sample is collected.", "A test reads the counter twice."),
		criterion("kept", "The collected sample is kept.", "A test reads the sample after the reset."),
	}
}

func subject(t testing.TB) string { return "subj_" + strings.TrimPrefix(mint(t, ids.Task), "task_") }

func define(title string, text string, crit []any, extra map[string]any) map[string]any {
	b := map[string]any{"text": text, "criteria": crit, "touched_subjects": []any{}}
	if title != "" {
		b["title"] = title
	}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

// defined defines a new specification and returns its identity.
func (h *home) defined(crit []any, extra map[string]any) string {
	h.t.Helper()
	body := h.ok(h.spec("define", define("A specification", "What the change must satisfy.", crit, extra)))
	return body["specification"].(map[string]any)["specification_id"].(string)
}

// revise defines the next revision of the specification, as a caller that read
// it at its record revision.
func (h *home) revise(id string, text string, crit []any, extra map[string]any) *envelope.Response {
	h.t.Helper()
	b := define("", text, crit, extra)
	b["specification_id"] = id
	r := h.stored("specification", id)["record_revision"].(int64)
	return h.spec("define", b, rev{"pool.specification", id, r})
}

func needTest() map[string]any {
	return map[string]any{"outcome": "The counter resets.", "coverage": "new-definition", "reason": "No accepted specification covered it."}
}

func acceptBody(specID string, revision int64, taskID string) map[string]any {
	return map[string]any{
		"specification_id": specID, "revision": revision, "task_id": taskID,
		"need_test": needTest(), "open_product_questions": []any{},
	}
}

// accepted accepts the revision for the item and returns the response body.
func (h *home) accepted(specID string, revision int64, taskID string) map[string]any {
	h.t.Helper()
	return h.ok(h.spec("accept", acceptBody(specID, revision, taskID)))
}
