package memory_test

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
	"github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
	_ "github.com/kesk-git/hive/packages/pool/internal/memory"
	_ "github.com/kesk-git/hive/packages/pool/internal/rule"
)

// The identities the record fixtures of contract/testdata share, so that the
// fixtures seed a home as they stand.
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

func at(s string) time.Time {
	t, err := time.Parse("2006-01-02T15:04:05.000Z", s)
	if err != nil {
		panic(err)
	}
	return t
}

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
	// The root is made and secured first, as Hive makes a home, so that every
	// entry made in it is owner-only and owned by the user.
	if err := store.MakeDir(h.dir); err != nil {
		t.Fatal(err)
	}
	if err := store.SecureDir(h.dir); err != nil {
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
	h.put(path, append(data, '\n'))
}

// put puts the file at path in the home with the content given, through the
// store's own primitives, so that the directories above it and the file are
// owner-only and owned by the user, as Hive makes them. A file that is there
// is emptied first.
func (h *home) put(path string, data []byte) {
	h.t.Helper()
	if err := store.MakeDir(filepath.Dir(path)); err != nil {
		h.t.Fatal(err)
	}
	f, err := store.CreateFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		h.t.Fatal(err)
	}
	if err := f.Close(); err != nil {
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
	// version is the operation version the request carries; empty is the
	// version the operation is declared at.
	version   string
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

// versions are the operation versions the requests carry: memory render,
// check and propagate are 2.0, budget 1.0.
var versions = map[string]string{"render": "2.0", "check": "2.0", "propagate": "2.0", "budget": "1.0", "resolve": "1.0", "read": "1.0"}

func (h *home) send(command, operation string, body map[string]any, r request) *envelope.Response {
	h.t.Helper()
	if r.scope == nil {
		r.scope = hiveScope
	}
	if r.authority == nil {
		r.authority = map[string]any{"kind": "none"}
	}
	if r.version == "" {
		r.version = versions[operation]
	}
	req := map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": command, "operation": operation, "operation_version": r.version,
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
	if len(resp.Receipts) != 0 {
		t.Errorf("a refusal carries receipts: %v", resp.Receipts)
	}
}

// render sends hive-memory render at the scope.
func (h *home) render(scope map[string]any, tier, audience, subject, instant string) *envelope.Response {
	h.t.Helper()
	body := map[string]any{"tier": tier, "audience": audience}
	if subject != "" {
		body["subject_id"] = subject
	}
	if instant != "" {
		body["at"] = instant
	}
	return h.send("hive-memory", "render", body, request{scope: scope})
}

// rendered renders and returns the projection of an ok response.
func (h *home) rendered(scope map[string]any, tier, audience, subject string) (projection map[string]any, unchanged bool) {
	h.t.Helper()
	body := h.ok(h.render(scope, tier, audience, subject, ""))
	return body["projection"].(map[string]any), body["unchanged"].(bool)
}

func current(p map[string]any) map[string]any { return p["current"].(map[string]any) }

// check sends hive-memory check, for one projection or for all.
func (h *home) check(projection string) map[string]any {
	h.t.Helper()
	body := map[string]any{}
	if projection != "" {
		body["projection_id"] = projection
	}
	return h.ok(h.send("hive-memory", "check", body, request{}))
}

// checked is the entry of the check's report for the projection.
func checked(t testing.TB, report map[string]any, projection string) map[string]any {
	t.Helper()
	for _, v := range report["projections"].([]any) {
		if m := v.(map[string]any); m["projection_id"] == projection {
			return m
		}
	}
	t.Fatalf("the check reports no projection %s: %v", projection, report["projections"])
	return nil
}

// freshness is the freshness hive-pool read computes for the projection.
func (h *home) freshness(projection string) map[string]any {
	h.t.Helper()
	body := h.ok(h.send("hive-pool", "read", map[string]any{"kind": "projection", "selector": map[string]any{"id": projection}}, request{}))
	derived := body["derived"].([]any)
	if len(derived) != 1 {
		h.t.Fatalf("derived %v", derived)
	}
	return derived[0].(map[string]any)
}

// plantBudget stands the budget configuration in the home, at revision 1 with
// the entries given, and the question that is the Hive Mind's word.
func (h *home) plantBudget(entries ...map[string]any) {
	h.t.Helper()
	rec := fixture(h.t, "pool.budget", "changed")
	rec["home_id"], rec["record_revision"] = homeID, int64(1)
	list := []any{}
	for _, e := range entries {
		list = append(list, e)
	}
	rec["projections"] = list
	h.plant("budget", "memory", rec)
	q := fixture(h.t, "pool.question", "answered-superseding")
	q["scope_level"] = "hive"
	delete(q, "workspace_id")
	delete(q, "project_id")
	h.plant("question", questionID, q)
}

func entryOf(id string, max int64) map[string]any {
	return map[string]any{"projection_id": id, "max_bytes": max}
}

// budgetOf is the budget record and maxOf the budget of one projection in it.
func (h *home) budgetRecord() map[string]any { return h.stored("budget", "memory") }

func maxOf(rec map[string]any, projection string) int64 {
	for _, v := range rec["projections"].([]any) {
		if m := v.(map[string]any); m["projection_id"] == projection {
			return m["max_bytes"].(int64)
		}
	}
	return 0
}

// subjectRecord is a subject at the scope, with the facts given.
func subjectRecord(t testing.TB, scope map[string]any, key string, facts ...string) (string, map[string]any) {
	t.Helper()
	id := "subj_" + strings.TrimPrefix(mint(t, ids.Task), "task_")
	rec := map[string]any{
		"schema": "pool.subject", "schema_version": "1.0", "record_revision": int64(1),
		"subject_id": id, "home_id": homeID, "key": key,
		"loads_at": []any{"dispatch"}, "facts": factList(t, facts),
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "writer": writer("record"),
	}
	for k, v := range scope {
		rec[k] = v
	}
	return id, rec
}

func factList(t testing.TB, texts []string) []any {
	t.Helper()
	provenance := fixture(t, "pool.subject", "class-with-fact")["facts"].([]any)[0].(map[string]any)["provenance"]
	list := []any{}
	for i, text := range texts {
		list = append(list, map[string]any{"fact_id": "fact-" + string(rune('a'+i)), "text": text, "provenance": provenance})
	}
	return list
}

// subject plants a subject at the scope and returns its identity.
func (h *home) subject(scope map[string]any, key string, facts ...string) string {
	h.t.Helper()
	id, rec := subjectRecord(h.t, scope, key, facts...)
	h.plant("subject", id, rec)
	return id
}

// setFacts replaces the facts of a planted subject and raises its revision, as
// the fold would.
func (h *home) setFacts(subject string, facts ...string) {
	h.t.Helper()
	rec := h.stored("subject", subject)
	rec["facts"], rec["record_revision"] = factList(h.t, facts), rec["record_revision"].(int64)+1
	h.plant("subject", subject, rec)
}

// R describes a rule beside the defaults: an operating rule in force since the
// first that applies to every role.
type R map[string]any

// ruleRecord is an operating rule of the tier, filed under the subject, at the
// scope.
func ruleRecord(t testing.TB, tier, filedUnder string, scope map[string]any, over R) (string, map[string]any) {
	t.Helper()
	id := mint(t, ids.Rule)
	rec := map[string]any{
		"schema": "pool.rule", "schema_version": "2.0", "record_revision": int64(1),
		"rule_id": id, "home_id": homeID, "subject": "key." + strings.ToLower(id[len(id)-6:]),
		"words": "The Hive Mind's words.", "origin": map[string]any{"kind": "ruling"},
		"applies": []any{}, "status": "in-force", "effective_at": d01,
		"authority": map[string]any{"kind": "none"}, "amendments": []any{},
		"created_at": d01, "updated_at": d01, "writer": writer("record"),
		"kind": "operating", "tier": tier, "filed_under": filedUnder, "rule_line": "Line of " + id,
	}
	for k, v := range scope {
		rec[k] = v
	}
	for k, v := range over {
		rec[k] = v
	}
	return id, rec
}

// rule plants an operating rule and returns its identity.
func (h *home) rule(tier, filedUnder string, scope map[string]any, over R) string {
	h.t.Helper()
	id, rec := ruleRecord(h.t, tier, filedUnder, scope, over)
	h.plant("rule", id, rec)
	return id
}

// amend raises a planted rule's revision, as an amendment would, with the
// members given changed.
func (h *home) amend(rule string, over R) {
	h.t.Helper()
	rec := h.stored("rule", rule)
	rec["record_revision"] = rec["record_revision"].(int64) + 1
	for k, v := range over {
		rec[k] = v
	}
	h.plant("rule", rule, rec)
}

// seed is a home with the budget configuration, no entry in it, and the
// subject every always rule is filed under.
func seed(t testing.TB) (*home, string) {
	h := newHome(t)
	h.plantBudget()
	return h, h.subject(hiveScope, "filing")
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
