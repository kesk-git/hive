package rulewrite_test

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
	_ "github.com/kesk-git/hive/packages/pool/internal/memory"
	_ "github.com/kesk-git/hive/packages/pool/internal/rule"
	_ "github.com/kesk-git/hive/packages/pool/internal/rulewrite"
)

// The identities the record fixtures of contract/testdata share.
const (
	homeID      = "home_01jc7d8e9f0g1h2j3k4m5n6p7a"
	workspaceID = "ws_01jc7d8e9f0g1h2j3k4m5n6p7b"
	projectID   = "proj_01jc7d8e9f0g1h2j3k4m5n6p7c"
	questionID  = "q_01jc7d8e9f0g1h2j3k4m5n6p7q"
)

// The clock of the tests stands at start; the instants below date the rules.
var start = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

const (
	d01 = "2026-10-01T00:00:00.000Z"
	d15 = "2026-10-15T00:00:00.000Z"
	d20 = "2026-10-20T00:00:00.000Z"
	d25 = "2026-10-25T00:00:00.000Z"
)

// home is a home with the Pool's executable over it.
type home struct {
	t   testing.TB
	dir string
	exe *cli.Executable
	clk *clock.Fake
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
	h := &home{t: t, dir: filepath.Join(t.TempDir(), "home"), clk: clock.NewFake(start)}
	if err := os.Mkdir(h.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	h.write("home.json", map[string]any{
		"schema": "hive.home", "schema_version": "1.0", "record_revision": int64(1),
		"home_id": homeID, "role": "hive", "name": "A test home", "parent": map[string]any{"kind": "none"},
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "writer": writer("provision"),
	})
	registry := func(extra map[string]any) map[string]any {
		out := map[string]any{"schema_version": "1.0", "record_revision": int64(1), "home_id": homeID, "state": "active",
			"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "slug": "registered", "name": "Registered",
			"writer": map[string]any{"package": "dispatch", "command": "hive-fleet", "operation": "provision", "implementation_version": "test"}}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	h.write("dispatch/registry/workspaces/"+workspaceID+".json", registry(map[string]any{
		"schema": "hive.workspace", "workspace_id": workspaceID, "parent_home_id": mint(t, ids.Home)}))
	h.write("dispatch/registry/projects/"+projectID+".json", registry(map[string]any{
		"schema": "hive.project", "project_id": projectID, "workspace_id": workspaceID, "repository": map[string]any{"kind": "local-only"}}))
	h.exe = entry.Executable()
	h.exe.Clock = h.clk
	return h
}

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

// plant stands a record in the Pool as the writers of other lanes would.
func (h *home) plant(kind, id string, rec map[string]any) {
	h.t.Helper()
	h.write("pool/records/"+kind+"/"+id+".json", rec)
}

// stored reads a record file as the store holds it, and nil where there is none.
func (h *home) stored(kind, id string) map[string]any {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, "pool", "records", kind, id+".json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		h.t.Fatal(err)
	}
	return v.(map[string]any)
}

// records is every file under pool/ but the replay records of requests.
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
	hiveScope      = map[string]any{"scope_level": "hive", "home_id": homeID}
	workspaceScope = map[string]any{"scope_level": "workspace", "home_id": homeID, "workspace_id": workspaceID}
	projectScope   = map[string]any{"scope_level": "project", "home_id": homeID, "workspace_id": workspaceID, "project_id": projectID}
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

func authority(kind, schema, id string, revision int64) map[string]any {
	if kind == "none" {
		return map[string]any{"kind": "none"}
	}
	return map[string]any{"kind": kind, "ref": map[string]any{"schema": schema, "id": id, "home_id": homeID, "record_revision": revision}}
}

// word is the authority of kind word that names the question at a revision.
func word(revision int64) map[string]any {
	return authority("word", "pool.question", questionID, revision)
}

// versions are the operation versions the requests carry.
var versions = map[string]string{"record": "2.0", "amend": "1.0", "expire": "1.0", "subject": "1.0", "render": "2.0", "resolve": "1.0", "read": "1.0"}

func (h *home) send(command, operation string, body map[string]any, r request) *envelope.Response {
	h.t.Helper()
	if r.scope == nil {
		r.scope = hiveScope
	}
	if r.authority == nil {
		r.authority = map[string]any{"kind": "none"}
	}
	req := map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": command, "operation": operation, "operation_version": versions[operation],
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

func ok(t testing.TB, resp *envelope.Response) map[string]any {
	t.Helper()
	if resp.Status != envelope.Ok {
		t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
	}
	return resp.Body
}

// refused checks the status and the code of a response that must not be ok, and
// that it carries no body and no receipt.
func refused(t testing.TB, resp *envelope.Response, status envelope.Status, code string) *envelope.Response {
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
	return resp
}

// plantBudget stands the budget configuration in the home, at the provisioned
// defaults with no entry, and the question that is the Hive Mind's word.
func (h *home) plantBudget() {
	h.t.Helper()
	rec := fixture(h.t, "pool.budget", "changed")
	rec["home_id"], rec["record_revision"] = homeID, int64(1)
	rec["projections"] = []any{}
	h.plant("budget", "memory", rec)
	q := fixture(h.t, "pool.question", "answered-superseding")
	q["scope_level"] = "hive"
	delete(q, "workspace_id")
	delete(q, "project_id")
	h.plant("question", questionID, q)
}

// subject plants a subject at the scope and returns its identity.
func (h *home) subject(scope map[string]any, key string) string {
	h.t.Helper()
	id := "subj_" + strings.TrimPrefix(mint(h.t, ids.Task), "task_")
	rec := map[string]any{
		"schema": "pool.subject", "schema_version": "1.0", "record_revision": int64(1),
		"subject_id": id, "home_id": homeID, "key": key,
		"loads_at": []any{"dispatch"}, "facts": []any{},
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "writer": writer("record"),
	}
	for k, v := range scope {
		rec[k] = v
	}
	h.plant("subject", id, rec)
	return id
}

// seed is a home with the budget configuration at the provisioned defaults and
// the subject the rules of the Hive are filed under.
func seed(t testing.TB) (*home, string) {
	h := newHome(t)
	h.plantBudget()
	return h, h.subject(hiveScope, "filing")
}

// opRule is the body of an operating rule of the tier, filed under the subject.
func opRule(subject, tier, filedUnder, line string) map[string]any {
	return map[string]any{
		"subject": subject, "words": "The Hive Mind's words, kept whole.", "origin": map[string]any{"kind": "ruling"},
		"applies": []any{}, "kind": "operating", "tier": tier, "filed_under": filedUnder, "rule_line": line,
	}
}

// record sends hive-rule record.
func (h *home) record(body map[string]any, r request) *envelope.Response {
	h.t.Helper()
	return h.send("hive-rule", "record", body, r)
}

// recorded records the rule and returns the rule record of an ok response.
func (h *home) recorded(body map[string]any, r request) map[string]any {
	h.t.Helper()
	return ok(h.t, h.record(body, r))["rule"].(map[string]any)
}

func (h *home) amend(rule map[string]any, body map[string]any) *envelope.Response {
	h.t.Helper()
	body["rule_id"] = rule["rule_id"]
	return h.send("hive-rule", "amend", body, request{expected: []rev{{"pool.rule", rule["rule_id"].(string), rule["record_revision"].(int64)}}})
}

func (h *home) expire(rule map[string]any, noticed string) *envelope.Response {
	h.t.Helper()
	return h.send("hive-rule", "expire", map[string]any{"rule_id": rule["rule_id"], "noticed_at": noticed}, request{})
}

// fresh says whether rendering the projection writes nothing, which is so only
// where a write already rendered it.
func (h *home) fresh(scope map[string]any, tier, audience, subject string) bool {
	h.t.Helper()
	body := map[string]any{"tier": tier, "audience": audience}
	if subject != "" {
		body["subject_id"] = subject
	}
	return ok(h.t, h.send("hive-memory", "render", body, request{scope: scope}))["unchanged"].(bool)
}

func kinds(resp *envelope.Response) []string {
	var out []string
	for _, r := range resp.Receipts {
		out = append(out, r.Kind)
	}
	slices.Sort(out)
	return out
}

func detail(t testing.TB, resp *envelope.Response) map[string]any {
	t.Helper()
	if resp.Error == nil {
		t.Fatal("no error")
	}
	return resp.Error.Detail
}

func moves(t testing.TB, resp *envelope.Response) {
	t.Helper()
	got, _ := detail(t, resp)["moves"].([]any)
	want := []any{"shorten-line-and-file-facts", "narrow-subject", "supersede-rule"}
	if !slices.Equal(got, want) {
		t.Errorf("moves %v, want %v", got, want)
	}
}

// tighten renders the projection, which stands it with its entry in the budget
// configuration, and lowers that entry to max bytes.
func (h *home) tighten(scope map[string]any, tier, audience, subject string, max int64) {
	h.t.Helper()
	body := map[string]any{"tier": tier, "audience": audience}
	if subject != "" {
		body["subject_id"] = subject
	}
	id := ok(h.t, h.send("hive-memory", "render", body, request{scope: scope}))["projection"].(map[string]any)["projection_id"]
	rec := h.stored("budget", "memory")
	for _, e := range rec["projections"].([]any) {
		if m := e.(map[string]any); m["projection_id"] == id {
			m["max_bytes"] = max
		}
	}
	h.plant("budget", "memory", rec)
}
