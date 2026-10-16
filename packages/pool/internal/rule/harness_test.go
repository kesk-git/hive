package rule_test

import (
	"os"
	"path/filepath"
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
	_ "github.com/kesk-git/hive/packages/pool/internal/rule"
)

// The clock of the tests stands at the first instant below; a request that
// names no instant resolves for it.
var start = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// The instants the rules of the tests are dated by.
const (
	d05 = "2026-10-05T00:00:00.000Z"
	d10 = "2026-10-10T00:00:00.000Z"
	d12 = "2026-10-12T00:00:00.000Z"
	d15 = "2026-10-15T00:00:00.000Z"
	d20 = "2026-10-20T00:00:00.000Z"
	d25 = "2026-10-25T00:00:00.000Z"
	d30 = "2026-10-30T00:00:00.000Z"
	d31 = "2026-10-31T00:00:00.000Z"
	d01 = "2026-10-01T00:00:00.000Z"
)

// home is a home with the Pool's executable over it, at one scope.
type home struct {
	t      testing.TB
	dir    string
	homeID string
	exe    *cli.Executable
	clk    *clock.Fake
	// scope is the scope members requests are made at.
	scope map[string]any
	// workspace and project are the registered identities rules are made for.
	workspace, project string
	// filed is the one subject record the operating rules are filed under.
	filed string
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
	h := &home{t: t, dir: filepath.Join(t.TempDir(), "home"), homeID: mint(t, ids.Home), clk: clock.NewFake(start)}
	// The root is made and secured first, as Hive makes a home, so that every
	// entry made in it is owner-only and owned by the user.
	if err := store.MakeDir(h.dir); err != nil {
		t.Fatal(err)
	}
	if err := store.SecureDir(h.dir); err != nil {
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
	h.put(filepath.Join(h.dir, "home.json"), append(data, '\n'))
	h.exe = entry.Executable()
	h.exe.Clock = h.clk
	h.workspace = h.register("workspaces", mint(t, ids.Workspace), map[string]any{"schema": "hive.workspace", "parent_home_id": mint(t, ids.Home)}, "workspace_id")
	h.project = h.register("projects", mint(t, ids.Project), map[string]any{"schema": "hive.project", "workspace_id": h.workspace, "repository": map[string]any{"kind": "local-only"}}, "project_id")
	h.filed = h.subject(map[string]any{})
	return h
}

// register writes a record of the home's identity registry, as fleet
// provisioning would, and returns its identity.
func (h *home) register(dir, id string, rec map[string]any, idField string) string {
	h.t.Helper()
	rec[idField] = id
	rec["schema_version"], rec["record_revision"], rec["home_id"], rec["state"] = "1.0", int64(1), h.homeID, "active"
	rec["created_at"], rec["updated_at"] = "2026-10-11T08:00:00.000Z", "2026-10-11T08:00:00.000Z"
	rec["slug"], rec["name"] = "registered", "Registered"
	rec["writer"] = map[string]any{"package": "dispatch", "command": "hive-fleet", "operation": "provision", "implementation_version": "test"}
	h.write(filepath.Join("dispatch", "registry", dir, id+".json"), rec)
	return id
}

func (h *home) write(rel string, rec map[string]any) {
	h.t.Helper()
	data, err := canon.Encode(rec)
	if err != nil {
		h.t.Fatal(err)
	}
	path := filepath.Join(h.dir, rel)
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

// at returns the home with requests made at the scope.
func (h *home) at(scope map[string]any) *home {
	c := *h
	c.scope = scope
	return &c
}

func (h *home) inWorkspace() *home {
	return h.at(map[string]any{"scope_level": "workspace", "workspace_id": h.workspace})
}

func (h *home) inProject() *home {
	return h.at(map[string]any{"scope_level": "project", "workspace_id": h.workspace, "project_id": h.project})
}

// The scope members of a rule, by the level it is made at.
func (h *home) scopeOf(level string) map[string]any {
	switch level {
	case "workspace":
		return map[string]any{"scope_level": level, "workspace_id": h.workspace}
	case "project":
		return map[string]any{"scope_level": level, "workspace_id": h.workspace, "project_id": h.project}
	}
	return map[string]any{"scope_level": "hive"}
}

// plant writes a record directly, as the rule recorder, a later lane, would
// leave it in the store.
func (h *home) plant(kind, id string, rec map[string]any) {
	h.t.Helper()
	h.write(filepath.Join("pool", "records", kind, id+".json"), rec)
}

func (h *home) subject(over map[string]any) string {
	h.t.Helper()
	id := "subj_" + strings.TrimPrefix(mint(h.t, ids.Task), "task_")
	rec := map[string]any{
		"schema": "pool.subject", "schema_version": "1.0", "record_revision": int64(1),
		"subject_id": id, "home_id": h.homeID, "scope_level": "hive", "key": "class.3",
		"loads_at": []any{"dispatch"}, "facts": []any{},
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "writer": writer("record"),
	}
	for k, v := range over {
		rec[k] = v
	}
	h.plant("subject", id, rec)
	return id
}

// R describes a rule beside the defaults: an operating rule of the Hive's
// scope, in force and effective since the first, on the subject s, that applies
// to every role. Its line names the rule.
type R map[string]any

// rule plants the rule and returns its identity.
func (h *home) rule(subject, level string, over R) string {
	h.t.Helper()
	id := mint(h.t, ids.Rule)
	rec := map[string]any{
		"schema": "pool.rule", "schema_version": "2.0", "record_revision": int64(1),
		"rule_id": id, "home_id": h.homeID, "subject": subject,
		"words": "The Hive Mind's words.", "origin": map[string]any{"kind": "ruling"},
		"applies": []any{}, "status": "in-force", "effective_at": d01,
		"authority": map[string]any{"kind": "none"}, "amendments": []any{},
		"created_at": d01, "updated_at": d01, "writer": writer("record"),
		"kind": "operating", "tier": "always", "filed_under": h.filed, "rule_line": "Line of " + id,
	}
	for k, v := range h.scopeOf(level) {
		rec[k] = v
	}
	for k, v := range over {
		rec[k] = v
	}
	h.plant("rule", id, rec)
	return id
}

func changes(id, relation string) R {
	return R{"changes": map[string]any{"rule_id": id, "record_revision": int64(1), "relation": relation}}
}

// amendment is an amendment of the rule's operation, as the amender, a later
// lane, would append it.
func amendment(seq int64, kind string, over map[string]any) map[string]any {
	a := map[string]any{
		"sequence": seq, "kind": kind, "words": "The Hive Mind's amending words.",
		"authority": map[string]any{"kind": "none"}, "recorded_at": d10,
		"recorded_by": map[string]any{"kind": "test"},
		"request_id":  "req_01j9xk3m2nq4r5s6t7v8w9xyzf", "receipt_id": "rcpt_01j9xk3m2nq4r5s6t7v8w9xyzh",
	}
	for k, v := range over {
		a[k] = v
	}
	return a
}

// The lift condition of a rule and the receipt that meets it.
const (
	liftKind    = "dispatch.project.registered"
	liftSubject = "proj_01j9xk3m2nq4r5s6t7v8w9xyzc"
)

func liftCondition() map[string]any {
	return map[string]any{"receipt_kind": liftKind, "subject": map[string]any{"schema": "dispatch.project", "id": liftSubject}}
}

// receipt is a whole receipt of the kind about the project.
func receipt(kind, subject string) map[string]any {
	return map[string]any{
		"schema": "hive.receipt", "schema_version": "1.0",
		"receipt_id": "rcpt_01j9xk3m2nq4r5s6t7v8w9xyzh", "kind": kind, "kind_version": "1.0",
		"written_at": "2026-10-11T08:00:01.250Z", "writer": map[string]any{"package": "dispatch", "command": "hive-dispatch", "operation": "register-project", "implementation_version": "0.1.0"},
		"request_id": "req_01j9xk3m2nq4r5s6t7v8w9xyzf", "correlation_id": "corr_01j9xk3m2nq4r5s6t7v8w9xyzg",
		"scope_level": "hive", "home_id": "home_01j9xk3m2nq4r5s6t7v8w9xyza", "authority": map[string]any{"kind": "none"},
		"evidence": []any{}, "body": map[string]any{},
		"subject": map[string]any{"schema": "dispatch.project", "id": subject, "record_revision": int64(1)},
	}
}

// lift is the amendment that lifts a rule on the receipt.
func lift(seq int64, rcpt map[string]any) map[string]any {
	over := map[string]any{"evidence": map[string]any{"schema": "hive.receipt", "id": "rcpt_01j9xk3m2nq4r5s6t7v8w9xyzh"}}
	if rcpt != nil {
		over["receipt"] = rcpt
	}
	return amendment(seq, "lift", over)
}

// send sends one request of the command and returns the response as it was
// written.
func (h *home) send(command, operation string, body map[string]any) *envelope.Response {
	h.t.Helper()
	req := map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": command, "operation": operation, "operation_version": "1.0",
		"request_id": mint(h.t, ids.Request), "correlation_id": mint(h.t, ids.Correlation),
		"issued_at": "2026-10-14T09:00:00.000Z", "caller": map[string]any{"kind": "test"},
		"scope_level": "hive", "home_id": h.homeID,
		"authority": map[string]any{"kind": "none"}, "body": body,
	}
	for k, v := range h.scope {
		req[k] = v
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

// resolved is the body of a resolve for the role at the instant, "" for the
// clock's own.
func (h *home) resolved(role, at string, extra map[string]any) map[string]any {
	h.t.Helper()
	body := map[string]any{"role": role}
	if at != "" {
		body["at"] = at
	}
	for k, v := range extra {
		body[k] = v
	}
	return h.ok(h.send("hive-rule", "resolve", body))
}

// ids are the rule identities of the rules the resolution applies, in order.
func applied(body map[string]any) []string {
	out := []string{}
	for _, r := range body["rules"].([]any) {
		out = append(out, r.(map[string]any)["rule_id"].(string))
	}
	return out
}

// excluded are the rules left out, by rule and reason.
func excluded(body map[string]any) map[string]string {
	out := map[string]string{}
	for _, e := range body["excluded"].([]any) {
		m := e.(map[string]any)
		out[m["rule_id"].(string)] = m["reason"].(string)
	}
	return out
}

func conflicts(body map[string]any) []any { return body["conflicts"].([]any) }
