package imperative_test

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
	_ "github.com/kesk-git/hive/packages/pool/internal/imperative"
	_ "github.com/kesk-git/hive/packages/pool/internal/memory"
	_ "github.com/kesk-git/hive/packages/pool/internal/rule"
)

// The identities the record fixtures of contract/testdata share.
const (
	homeID      = "home_01jc7d8e9f0g1h2j3k4m5n6p7a"
	workspaceID = "ws_01jc7d8e9f0g1h2j3k4m5n6p7b"
	projectID   = "proj_01jc7d8e9f0g1h2j3k4m5n6p7c"
	questionID  = "q_01jc7d8e9f0g1h2j3k4m5n6p7q"
)

// The clock of the tests stands at start.
var start = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

const (
	d01 = "2026-10-01T00:00:00.000Z"
	d02 = "2026-10-02T00:00:00.000Z"
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

// repository is the binding of the project the tests register.
var repository = map[string]any{"kind": "github", "host": "github.com", "owner": "example-org", "name": "widget-service"}

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
		"schema": "hive.project", "project_id": projectID, "workspace_id": workspaceID, "repository": repository}))
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

// request is how a request is made: the scope it is made at and the revisions
// it expects.
type request struct {
	scope    map[string]any
	expected []rev
	// version is the operation version the request carries, where it is not
	// the one the operation is declared at.
	version string
}

// versions are the operation versions the requests carry.
var versions = map[string]string{
	"create": "1.0", "revise": "1.0", "render": "1.1", "deliver": "1.1", "read": "1.0",
	"propagate": "2.0",
}

func (h *home) send(command, operation string, body map[string]any, r request) *envelope.Response {
	h.t.Helper()
	if r.scope == nil {
		r.scope = projectScope
	}
	version := versions[operation]
	if command == "hive-memory" && operation == "render" {
		version = "2.0"
	}
	if r.version != "" {
		version = r.version
	}
	req := map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": command, "operation": operation, "operation_version": version,
		"request_id": mint(h.t, ids.Request), "correlation_id": mint(h.t, ids.Correlation),
		"issued_at": "2026-10-14T09:00:00.000Z", "caller": map[string]any{"kind": "test"},
		"authority": map[string]any{"kind": "none"}, "body": body,
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

func kinds(resp *envelope.Response) []string {
	var out []string
	for _, r := range resp.Receipts {
		out = append(out, r.Kind)
	}
	slices.Sort(out)
	return out
}

// plantBudget stands the budget configuration in the home, at the provisioned
// defaults with no entry.
func (h *home) plantBudget() {
	h.t.Helper()
	rec := fixture(h.t, "pool.budget", "changed")
	rec["home_id"], rec["record_revision"] = homeID, int64(1)
	rec["projections"] = []any{}
	h.plant("budget", "memory", rec)
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

// rule plants an operating rule of the Hive's scope, filed under the subject, in
// force since the first, that applies to the roles given, and returns its
// identity.
func (h *home) rule(filedUnder, key, line string, roles ...string) string {
	h.t.Helper()
	id := mint(h.t, ids.Rule)
	applies := []any{}
	for _, r := range roles {
		applies = append(applies, r)
	}
	h.plant("rule", id, map[string]any{
		"schema": "pool.rule", "schema_version": "2.0", "record_revision": int64(1),
		"rule_id": id, "home_id": homeID, "scope_level": "hive", "subject": key,
		"words": "The Hive Mind's words, kept whole.", "origin": map[string]any{"kind": "ruling"},
		"applies": applies, "status": "in-force", "effective_at": d01,
		"authority": map[string]any{"kind": "none"}, "amendments": []any{},
		"created_at": d01, "updated_at": d01, "writer": writer("record"),
		"kind": "operating", "tier": "subject", "filed_under": filedUnder, "rule_line": line,
	})
	return id
}

// reword replaces the line of a planted rule and raises its revision, as an
// amendment would.
func (h *home) reword(id, line string) {
	h.t.Helper()
	rec := h.stored("rule", id)
	rec["rule_line"], rec["record_revision"] = line, rec["record_revision"].(int64)+1
	h.plant("rule", id, rec)
}

// item plants an item of the project at the level, beneath the parent where one
// is given, and returns its identity. A Target carries none of the work axes.
func (h *home) item(level, parent string) string {
	h.t.Helper()
	id := mint(h.t, ids.Task)
	deps := []any{}
	if parent != "" {
		deps = append(deps, map[string]any{"type": "parent", "task_id": parent})
	}
	rec := map[string]any{
		"schema": "pool.item", "schema_version": "1.0", "record_revision": int64(3),
		"task_id": id, "scope_level": "project", "home_id": homeID, "workspace_id": workspaceID, "project_id": projectID,
		"title": "An item", "level": level, "state": "queued", "definition": "accepted",
		"deps": deps, "links": []any{}, "generations": []any{},
		"created_at": d01, "updated_at": d02, "writer": writer("add"),
	}
	if level != "target" {
		rec["deliverable"], rec["class"], rec["placement_requested"] = "swarm", int64(3), "mac"
		rec["delivery"] = map[string]any{"validation": "synapse", "publication": "harvest", "assimilation": "word"}
	}
	h.plant("item", id, rec)
	return id
}

// open puts the item in flight with generation 1 open, as open-generation would.
func (h *home) open(id string) {
	h.t.Helper()
	rec := h.stored("item", id)
	rec["state"], rec["record_revision"] = "in-flight", rec["record_revision"].(int64)+1
	rec["generations"] = []any{map[string]any{
		"generation": int64(1), "state": "open", "opened_at": d02, "placement_requested": "mac",
		"imperative_revision": int64(1), "admission_id": mint(h.t, ids.Admission),
	}}
	h.plant("item", id, rec)
}

// world is a home with a Hunt beneath a Prey beneath a Target, the playbook of
// class 3 as two rules, a subject the Hunt's specification touches with a rule
// of its own, that subject's projection rendered for class 3, and the
// specification revision the Standard names.
type world struct {
	*home
	hunt, prey, target string
	classSubject       string
	touched            string
	playbook           []string
	touchedRule        string
	projection         string
	criteria           map[string]any
	specification      map[string]any
}

func newWorld(t testing.TB) *world {
	t.Helper()
	h := newHome(t)
	h.plantBudget()
	w := &world{home: h}
	w.target = h.item("target", "")
	w.prey = h.item("prey", w.target)
	w.hunt = h.item("hunt", w.prey)
	w.classSubject = h.subject(hiveScope, "class.3")
	w.touched = h.subject(hiveScope, "widget.exports")
	w.playbook = []string{
		h.rule(w.classSubject, "class3.steps", "Work in small commits and run the declared command after each.", "class-3"),
		h.rule(w.classSubject, "class3.stop", "Stop at the stated goal and report the evidence.", "class-3"),
	}
	w.touchedRule = h.rule(w.touched, "exports.retry", "Exports retry at most three times.", "class-3")
	// A rule of another class belongs to that class's playbook and to no other.
	h.rule(w.classSubject, "class5.steps", "Only a class 5 worker reads this.", "class-5")

	spec := fixture(t, "pool.specification", "v1-2-touched-subjects-and-evidence")
	specID := mint(t, ids.Spec)
	spec["specification_id"], spec["home_id"], spec["workspace_id"], spec["project_id"] = specID, homeID, workspaceID, projectID
	revisions := spec["revisions"].([]any)
	revisions[0].(map[string]any)["touched_subjects"] = []any{}
	second := revisions[1].(map[string]any)
	second["touched_subjects"] = []any{w.touched}
	delete(second, "product_rules")
	delete(second, "plan_stage")
	delete(spec, "acceptances")
	h.plant("specification", specID, spec)
	w.specification = spec
	w.criteria = map[string]any{"specification_id": specID, "revision": int64(2), "digest": second["digest"]}

	rendered := ok(t, h.send("hive-memory", "render", map[string]any{"tier": "subject", "audience": "class-3", "subject_id": w.touched}, request{}))
	w.projection = rendered["projection"].(map[string]any)["projection_id"].(string)
	return w
}

// body is the create request for the item: the revision of the fixture with
// what the command supplies taken out, and the Standard naming the criteria.
func (w *world) body(task string) map[string]any {
	rec := fixture(w.t, "pool.imperative", "project-swarm-claude-mac")
	r := rec["revisions"].([]any)[0].(map[string]any)
	for _, k := range []string{"revision", "digest", "authored_at", "authored_by", "authority", "request_id", "supersedes_reason"} {
		delete(r, k)
	}
	setup := r["setup"].(map[string]any)
	delete(setup, "placement")
	delete(setup, "repository")
	r["task"].(map[string]any)["standard"].(map[string]any)["criteria"] = w.criteria
	body := map[string]any{"task_id": task, "class": int64(3)}
	for _, k := range []string{"task", "setup", "rules", "definition_of_done"} {
		body[k] = r[k]
	}
	return body
}

// create sends hive-imperative create for the item.
func (w *world) create(task string) *envelope.Response {
	w.t.Helper()
	return w.send("hive-imperative", "create", w.body(task), request{})
}

// created creates the Imperative of the Hunt and returns its record.
func (w *world) created() map[string]any {
	w.t.Helper()
	return ok(w.t, w.create(w.hunt))["imperative"].(map[string]any)
}

// render sends hive-imperative render for the item.
func (w *world) render(task string) *envelope.Response {
	w.t.Helper()
	return w.send("hive-imperative", "render", map[string]any{"task_id": task}, request{})
}

func list(v any) []any { l, _ := v.([]any); return l }

func tree(v any) map[string]any { m, _ := v.(map[string]any); return m }

// expectedDigest is the digest of the canonical form of a revision's class and
// four sections, computed here from the record.
func expectedDigest(t testing.TB, revision map[string]any) string {
	t.Helper()
	form := map[string]any{}
	for _, k := range []string{"class", "task", "setup", "rules", "definition_of_done"} {
		form[k] = revision[k]
	}
	data, err := canon.Encode(form)
	if err != nil {
		t.Fatal(err)
	}
	return canon.FileDigest(data)
}

// closeGeneration marks generation 1 of the item closed, as close-generation would.
func (h *home) closeGeneration(id string) {
	h.t.Helper()
	rec := h.stored("item", id)
	rec["record_revision"] = rec["record_revision"].(int64) + 1
	g := rec["generations"].([]any)[0].(map[string]any)
	g["state"], g["closed_at"], g["outcome"] = "closed", d02, "delivered"
	g["closed_by"] = map[string]any{"schema": "dispatch.session.ended", "id": mint(h.t, ids.Receipt)}
	h.plant("item", id, rec)
}
