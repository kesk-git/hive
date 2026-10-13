package registry_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/dispatch/internal/entry"
)

// The bodies of the requests below.

func hiveBody(name string) map[string]any {
	return map[string]any{"home": map[string]any{"role": "hive", "name": name}}
}

func fleetBody(parentID string, projects ...any) map[string]any {
	return map[string]any{
		"home":      map[string]any{"role": "fleet", "name": "Widgets", "parent_home_id": parentID},
		"workspace": map[string]any{"slug": "widgets", "name": "Widgets"},
		"projects":  append([]any{}, projects...),
	}
}

func projectSpec(slug string, clones ...any) map[string]any {
	return map[string]any{
		"slug": slug, "name": strings.ToUpper(slug), "repository": map[string]any{"kind": "local-only"},
		"clones": append([]any{}, clones...),
	}
}

func cloneSpec(path, remote string) map[string]any {
	c := map[string]any{"worktree_root": path}
	if remote != "" {
		c["remote_name"] = remote
	}
	return c
}

// expect checks the status and the code of a response that is not ok.
func expect(t testing.TB, resp *envelope.Response, status envelope.Status, code string) {
	t.Helper()
	if resp.Status != status || resp.Error == nil || resp.Error.Code != code {
		t.Fatalf("status %s (%+v); want %s with %s", resp.Status, resp.Error, status, code)
	}
	if len(resp.Receipts) != 0 {
		t.Errorf("%d receipts with a refusal that leaves none", len(resp.Receipts))
	}
}

// created lists the "created" marks of a response body, in the order home,
// workspace, then each project followed by its clones.
func marks(t testing.TB, body map[string]any) []bool {
	t.Helper()
	var out []bool
	for _, k := range []string{"home", "workspace"} {
		if m, ok := body[k].(map[string]any); ok {
			out = append(out, m["created"].(bool))
		}
	}
	for _, p := range list(t, body, "projects") {
		out = append(out, p["created"].(bool))
		for _, c := range list(t, p, "clones") {
			out = append(out, c["created"].(bool))
		}
	}
	return out
}

// records lists the records of a response body, in the order of marks.
func records(t testing.TB, body map[string]any) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, k := range []string{"home", "workspace"} {
		if m, ok := body[k].(map[string]any); ok {
			out = append(out, m["record"].(map[string]any))
		}
	}
	for _, p := range list(t, body, "projects") {
		out = append(out, p["record"].(map[string]any))
		for _, c := range list(t, p, "clones") {
			out = append(out, c["record"].(map[string]any))
		}
	}
	return out
}

func all(want bool, n int) []bool {
	out := make([]bool, n)
	for i := range out {
		out[i] = want
	}
	return out
}

// withoutOutcomes is the snapshot without the recorded outcomes of requests,
// which a request that finds everything it names writes and nothing else.
func withoutOutcomes(snap map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range snap {
		if !strings.HasPrefix(k, "dispatch/requests/") {
			out[k] = v
		}
	}
	return out
}

// A home with role hive: the directory's home.json, adopting the request's
// home_id as the home's identity, with no workspace and no parent; and the
// same request under a new request_id returns the record unchanged.
func TestAHiveHomeAdoptsTheProposedIdentityAndIsIdempotentOnTheDirectory(t *testing.T) {
	home := newHome(t)
	homeID := mint(t, ids.Home)

	resp := send(t, home, request(t, homeID, hiveBody("Hive")))
	if resp.Status != envelope.Ok {
		t.Fatalf("%s: %+v", resp.Status, resp.Error)
	}
	if _, ok := resp.Body["workspace"]; ok {
		t.Error("a Hive home answers with a workspace")
	}
	if got := list(t, resp.Body, "projects"); len(got) != 0 {
		t.Errorf("%d projects", len(got))
	}
	rec := object(t, resp.Body, "home", "record")
	if rec["home_id"] != homeID || rec["role"] != "hive" || rec["name"] != "Hive" || rec["record_revision"] != int64(1) {
		t.Errorf("the record %v", rec)
	}
	if _, ok := rec["workspace_id"]; ok || object(t, rec, "parent")["kind"] != "none" {
		t.Errorf("a Hive home has no workspace and no parent: %v", rec)
	}
	if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "dispatch.home.created" || resp.Receipts[0].Subject.ID != homeID {
		t.Errorf("receipts %v", resp.Receipts)
	}
	after := snapshot(t, home)
	for rel := range after {
		if strings.HasPrefix(rel, "dispatch/registry/") && !strings.HasSuffix(rel, "/") {
			t.Errorf("a registry record %s", rel)
		}
	}

	// Another request for the same directory and identity, under another
	// name: the record is returned unchanged, and nothing is created.
	again := send(t, home, request(t, homeID, hiveBody("Another name")))
	if again.Status != envelope.Ok || again.Replayed {
		t.Fatalf("%s %v: %+v", again.Status, again.Replayed, again.Error)
	}
	if got := marks(t, again.Body); !reflect.DeepEqual(got, []bool{false}) {
		t.Errorf("marks %v", got)
	}
	if !reflect.DeepEqual(object(t, again.Body, "home", "record"), rec) {
		t.Error("the record changed")
	}
	if len(again.Receipts) != 0 {
		t.Errorf("%d receipts for nothing created", len(again.Receipts))
	}
	if !reflect.DeepEqual(withoutOutcomes(snapshot(t, home)), withoutOutcomes(after)) {
		t.Error("the home changed")
	}
}

// A home with role fleet and a workspace: home.json and the workspace record,
// each naming the other, with the parent's home_id as given; idempotent on the
// directory and the workspace slug.
func TestAFleetHomeAndItsWorkspaceNameEachOther(t *testing.T) {
	home := newHome(t)
	homeID, parentID := mint(t, ids.Home), mint(t, ids.Home)

	resp := send(t, home, request(t, homeID, fleetBody(parentID)))
	if resp.Status != envelope.Ok {
		t.Fatalf("%s: %+v", resp.Status, resp.Error)
	}
	homeRec, wsRec := object(t, resp.Body, "home", "record"), object(t, resp.Body, "workspace", "record")
	if err := ids.Check(wsRec["workspace_id"].(string), ids.Workspace); err != nil {
		t.Fatal(err)
	}
	if homeRec["workspace_id"] != wsRec["workspace_id"] || wsRec["home_id"] != homeID || wsRec["parent_home_id"] != parentID {
		t.Errorf("home %v, workspace %v", homeRec, wsRec)
	}
	if parent := object(t, homeRec, "parent"); parent["kind"] != "home" || parent["home_id"] != parentID {
		t.Errorf("the parent %v", parent)
	}
	if wsRec["slug"] != "widgets" || wsRec["name"] != "Widgets" || wsRec["state"] != "active" {
		t.Errorf("the workspace %v", wsRec)
	}
	if len(resp.Receipts) != 2 {
		t.Errorf("%d receipts", len(resp.Receipts))
	}
	after := snapshot(t, home)

	// The same directory and slug under other names: the same two records.
	body := fleetBody(parentID)
	body["home"].(map[string]any)["name"] = "Renamed"
	body["workspace"].(map[string]any)["name"] = "Renamed"
	again := send(t, home, request(t, homeID, body))
	if again.Status != envelope.Ok {
		t.Fatalf("%s: %+v", again.Status, again.Error)
	}
	if !reflect.DeepEqual(marks(t, again.Body), []bool{false, false}) || !reflect.DeepEqual(records(t, again.Body), records(t, resp.Body)) {
		t.Errorf("marks %v; the records differ: %v", marks(t, again.Body), records(t, again.Body))
	}
	if !reflect.DeepEqual(withoutOutcomes(snapshot(t, home)), withoutOutcomes(after)) {
		t.Error("the home changed")
	}
}

// Projects are created under the home's workspace, one receipt each with its
// own subject, and are idempotent on their slug within the workspace; a
// request that adds a project to an existing home sends the home's identity.
func TestProjectsAreIdempotentOnTheirSlugWithinTheWorkspace(t *testing.T) {
	home := newHome(t)
	homeID, parentID := mint(t, ids.Home), mint(t, ids.Home)

	first := send(t, home, request(t, homeID, fleetBody(parentID, projectSpec("one"), projectSpec("two"))))
	if first.Status != envelope.Ok {
		t.Fatalf("%s: %+v", first.Status, first.Error)
	}
	if got := marks(t, first.Body); !reflect.DeepEqual(got, all(true, 4)) {
		t.Fatalf("marks %v", got)
	}
	subjects := map[string]bool{}
	var kinds []string
	for _, r := range first.Receipts {
		kinds = append(kinds, r.Kind)
		if r.Kind == "dispatch.project.registered" {
			subjects[r.Subject.ID] = true
		}
	}
	if len(subjects) != 2 || len(first.Receipts) != 4 {
		t.Errorf("receipts %v name %d projects", kinds, len(subjects))
	}
	projects := list(t, first.Body, "projects")
	wsID := object(t, first.Body, "workspace", "record")["workspace_id"]
	for i, p := range projects {
		rec := object(t, p, "record")
		if rec["workspace_id"] != wsID || rec["home_id"] != homeID || !subjects[rec["project_id"].(string)] {
			t.Errorf("project %d: %v", i, rec)
		}
	}

	// Adding a third and naming the first again, with another name: the first
	// is found as it was, the third is new, and one receipt is written.
	renamed := projectSpec("one")
	renamed["name"] = "Another name"
	second := send(t, home, request(t, homeID, fleetBody(parentID, renamed, projectSpec("three"))))
	if second.Status != envelope.Ok {
		t.Fatalf("%s: %+v", second.Status, second.Error)
	}
	if got := marks(t, second.Body); !reflect.DeepEqual(got, []bool{false, false, false, true}) {
		t.Errorf("marks %v", got)
	}
	got := list(t, second.Body, "projects")
	if !reflect.DeepEqual(got[0]["record"], projects[0]["record"]) {
		t.Errorf("the first project changed: %v", got[0]["record"])
	}
	if len(second.Receipts) != 1 || second.Receipts[0].Kind != "dispatch.project.registered" || second.Receipts[0].Subject.ID != object(t, got[1], "record")["project_id"] {
		t.Errorf("receipts %v", second.Receipts)
	}
}

// A clone is created from what Git reads of it: the physical common directory
// and the top level, the object format and the named remote.
func TestACloneIsReadFromGit(t *testing.T) {
	isolateGit(t)
	home := newHome(t)
	clone := newClone(t, "git@example.test:acme/tool.git")
	bare := newClone(t, "")

	resp := send(t, home, request(t, mint(t, ids.Home), fleetBody(mint(t, ids.Home),
		projectSpec("tool", cloneSpec(clone, "origin"), cloneSpec(bare, "")))))
	if resp.Status != envelope.Ok {
		t.Fatalf("%s: %+v", resp.Status, resp.Error)
	}
	clones := list(t, list(t, resp.Body, "projects")[0], "clones")
	if len(clones) != 2 {
		t.Fatalf("%d clones", len(clones))
	}
	named, unnamed := object(t, clones[0], "record"), object(t, clones[1], "record")
	if named["git_common_dir"] != physical(t, filepath.Join(clone, ".git")) || named["worktree_root"] != clone {
		t.Errorf("paths %v %v", named["git_common_dir"], named["worktree_root"])
	}
	if origin := object(t, named, "origin"); origin["kind"] != "remote" || origin["remote_name"] != "origin" || origin["url"] != "git@example.test:acme/tool.git" {
		t.Errorf("origin %v", origin)
	}
	if origin := object(t, unnamed, "origin"); origin["kind"] != "none" || len(origin) != 1 {
		t.Errorf("a clone registered with no remote named: %v", origin)
	}
	for _, rec := range []map[string]any{named, unnamed} {
		if rec["state"] != "active" || rec["verified_at"] != rec["created_at"] {
			t.Errorf("clone %v", rec)
		}
	}
}

func TestACloneRecordsItsObjectFormat(t *testing.T) {
	isolateGit(t)
	dir := physical(t, t.TempDir())
	if err := tryGit(dir, "init", "--quiet", "--object-format=sha256"); err != nil {
		t.Skipf("this git cannot make a sha256 repository: %v", err)
	}
	resp := send(t, newHome(t), request(t, mint(t, ids.Home), fleetBody(mint(t, ids.Home), projectSpec("tool", cloneSpec(dir, "")))))
	if resp.Status != envelope.Ok {
		t.Fatalf("%s: %+v", resp.Status, resp.Error)
	}
	rec := records(t, resp.Body)[3]
	if rec["object_format"] != "sha256" {
		t.Errorf("the object format %v", rec["object_format"])
	}
}

// A clone is idempotent on its physical common directory: registering the
// same one again returns the same clone, however the path is spelled, and a
// linked worktree of a clone is that clone. Two clones of one origin are two.
func TestACloneIsIdempotentOnItsPhysicalCommonDirectory(t *testing.T) {
	isolateGit(t)
	home := newHome(t)
	homeID, parentID := mint(t, ids.Home), mint(t, ids.Home)
	clone := newClone(t, "https://example.test/acme/tool.git")
	if err := os.Mkdir(filepath.Join(clone, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, clone, "worktree", "add", "--quiet", "-b", "side", linked)
	spellings := map[string]string{
		"a subdirectory":    filepath.ToSlash(filepath.Join(clone, "sub")),
		"the git directory": filepath.ToSlash(filepath.Join(clone, ".git")),
		"a linked worktree": filepath.ToSlash(linked),
	}
	if link := filepath.Join(t.TempDir(), "link"); os.Symlink(clone, link) == nil {
		spellings["a symbolic link"] = filepath.ToSlash(link)
	}

	first := send(t, home, request(t, homeID, fleetBody(parentID, projectSpec("tool", cloneSpec(clone, "origin")))))
	if first.Status != envelope.Ok {
		t.Fatalf("%s: %+v", first.Status, first.Error)
	}
	want := records(t, first.Body)[3]
	before := snapshot(t, home)
	for name, path := range spellings {
		again := send(t, home, request(t, homeID, fleetBody(parentID, projectSpec("tool", cloneSpec(path, "origin")))))
		if again.Status != envelope.Ok {
			t.Fatalf("%s: %s: %+v", name, again.Status, again.Error)
		}
		if got := marks(t, again.Body); !reflect.DeepEqual(got, all(false, 4)) {
			t.Errorf("%s: marks %v", name, got)
		}
		if got := records(t, again.Body)[3]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: the clone is %v, not %v", name, got, want)
		}
		if len(again.Receipts) != 0 {
			t.Errorf("%s: %d receipts", name, len(again.Receipts))
		}
	}
	if !reflect.DeepEqual(withoutOutcomes(snapshot(t, home)), withoutOutcomes(before)) {
		t.Error("the home changed")
	}

	// A second clone of the same origin is a second clone, with its own identity.
	copyOf := physical(t, t.TempDir())
	git(t, copyOf, "clone", "--quiet", clone, copyOf+"/c")
	other := send(t, home, request(t, homeID, fleetBody(parentID, projectSpec("tool", cloneSpec(copyOf+"/c", "origin")))))
	if other.Status != envelope.Ok {
		t.Fatalf("%s: %+v", other.Status, other.Error)
	}
	if got := marks(t, other.Body); !reflect.DeepEqual(got, []bool{false, false, false, true}) {
		t.Errorf("marks %v", got)
	}
	if records(t, other.Body)[3]["clone_id"] == want["clone_id"] {
		t.Error("two clones share an identity")
	}
}

// A request that would change a fixed attribute of an existing identity is a
// conflict with already_exists, and nothing is written.
func TestAFixedAttributeIsNeverChanged(t *testing.T) {
	isolateGit(t)
	home := newHome(t)
	homeID, parentID := mint(t, ids.Home), mint(t, ids.Home)
	clone := newClone(t, "")
	first := send(t, home, request(t, homeID, fleetBody(parentID, projectSpec("one", cloneSpec(clone, "")), projectSpec("two"))))
	if first.Status != envelope.Ok {
		t.Fatalf("%s: %+v", first.Status, first.Error)
	}
	before := snapshot(t, home)

	otherSlug := fleetBody(parentID)
	otherSlug["workspace"].(map[string]any)["slug"] = "another"
	otherParent := fleetBody(mint(t, ids.Home))
	for name, body := range map[string]map[string]any{
		"the home's role":      hiveBody("Hive"),
		"the workspace's slug": otherSlug,
		"the home's parent":    otherParent,
		"a clone's project":    fleetBody(parentID, projectSpec("two", cloneSpec(clone, ""))),
	} {
		resp := send(t, home, request(t, homeID, body))
		expect(t, resp, envelope.Conflict, envelope.CodeAlreadyExists)
		if !reflect.DeepEqual(snapshot(t, home), before) {
			t.Errorf("%s: the refusal wrote something", name)
		}
	}
}

// A directory that holds a home.json with another home_id is refused with
// home_mismatch.
func TestADirectoryWithAnotherHomeIdentityIsRefused(t *testing.T) {
	home := newHome(t)
	if resp := send(t, home, request(t, mint(t, ids.Home), hiveBody("Hive"))); resp.Status != envelope.Ok {
		t.Fatalf("%s: %+v", resp.Status, resp.Error)
	}
	before := snapshot(t, home)
	resp := send(t, home, request(t, mint(t, ids.Home), hiveBody("Hive")))
	expect(t, resp, envelope.Refused, envelope.CodeHomeMismatch)
	if !reflect.DeepEqual(snapshot(t, home), before) {
		t.Error("the refusal wrote something")
	}
}

// A home is never inside a Git working tree: given one, the command refuses
// with home_inside_repository.
func TestAHomeInsideAWorkingTreeIsRefused(t *testing.T) {
	isolateGit(t)
	repo := newClone(t, "")
	home := filepath.Join(repo, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	expect(t, send(t, home, request(t, mint(t, ids.Home), hiveBody("Hive"))), envelope.Refused, envelope.CodeHomeInsideRepository)
	if _, err := os.Stat(filepath.Join(home, "home.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a home record was written inside a working tree: %v", err)
	}
}

// A clone that cannot be read fails the whole request and writes nothing.
func TestARequestWhoseCloneCannotBeReadWritesNothing(t *testing.T) {
	isolateGit(t)
	home := newHome(t)
	notRepo := t.TempDir()
	resp := send(t, home, request(t, mint(t, ids.Home), fleetBody(mint(t, ids.Home), projectSpec("tool", cloneSpec(filepath.ToSlash(notRepo), "")))))
	expect(t, resp, envelope.Unavailable, envelope.CodeSourceUnreadable)
	if got := snapshot(t, home); len(withoutBookkeeping(got)) != 0 {
		t.Errorf("the home holds %v", got)
	}
}

// A request breaking the schema is invalid and writes nothing: a Fleet home
// names its workspace and its parent, and a Hive home has neither.
func TestARequestBreakingTheSchemaWritesNothing(t *testing.T) {
	home := newHome(t)
	noWorkspace := fleetBody(mint(t, ids.Home))
	delete(noWorkspace, "workspace")
	noParent := fleetBody("")
	delete(noParent["home"].(map[string]any), "parent_home_id")
	hiveWithWorkspace := hiveBody("Hive")
	hiveWithWorkspace["workspace"] = map[string]any{"slug": "w", "name": "W"}
	for name, body := range map[string]map[string]any{"no workspace": noWorkspace, "no parent": noParent, "a Hive home with a workspace": hiveWithWorkspace} {
		resp := send(t, home, request(t, mint(t, ids.Home), body))
		expect(t, resp, envelope.Invalid, envelope.CodeSchemaViolation)
		if got := snapshot(t, home); len(got) != 0 {
			t.Errorf("%s: the home holds %v", name, got)
		}
	}
}

// The same request_id with another request is a conflict with replay_mismatch.
func TestTheSameRequestIDWithAnotherRequestIsRefused(t *testing.T) {
	home := newHome(t)
	homeID := mint(t, ids.Home)
	requestID := mint(t, ids.Request)
	if resp := send(t, home, requestAs(t, requestID, homeID, hiveBody("Hive"))); resp.Status != envelope.Ok {
		t.Fatalf("%s: %+v", resp.Status, resp.Error)
	}
	before := snapshot(t, home)
	expect(t, send(t, home, requestAs(t, requestID, homeID, hiveBody("Another name"))), envelope.Conflict, envelope.CodeReplayMismatch)
	if !reflect.DeepEqual(snapshot(t, home), before) {
		t.Error("the refusal wrote something")
	}
}

// All records of a request are one transaction whose intent states that each
// is expected absent: a crash after the intent and before any record reached
// disk rolls forward when the next command takes the store's lock, and the
// request sent again finds the whole as the first attempt would have left it.
func TestACrashBeforeAnyRecordReachedDiskRollsForward(t *testing.T) {
	isolateGit(t)
	home := newHome(t)
	clone := newClone(t, "")
	req := request(t, mint(t, ids.Home), fleetBody(mint(t, ids.Home), projectSpec("tool", cloneSpec(clone, ""))))

	e := entry.Executable()
	crashed := false
	e.Fault = func(step string) error {
		// The first file after the intent is the first record.
		if crashed || !strings.HasPrefix(step, "create ") || strings.Contains(step, "/tx/") {
			return nil
		}
		crashed = true
		return errors.New("test: the process crashed here")
	}
	resp := sendWith(t, e, home, req)
	expect(t, resp, envelope.Unknown, envelope.CodeOutcomeUnknown)
	if !crashed {
		t.Fatal("the fault was never reached")
	}
	if _, err := os.Stat(filepath.Join(home, "home.json")); err == nil {
		t.Fatal("the crash came after a record reached disk")
	}

	again := send(t, home, req)
	if again.Status != envelope.Ok {
		t.Fatalf("%s: %+v", again.Status, again.Error)
	}
	if got := marks(t, again.Body); !reflect.DeepEqual(got, all(true, 4)) {
		t.Errorf("marks %v", got)
	}
	var identities, receipts int
	for rel := range snapshot(t, home) {
		switch {
		case rel == "home.json" || strings.HasPrefix(rel, "dispatch/registry/") && strings.HasSuffix(rel, ".json"):
			identities++
		case strings.HasPrefix(rel, "dispatch/receipts/") && strings.HasSuffix(rel, ".json"):
			receipts++
		case strings.HasPrefix(rel, "dispatch/tx/") && strings.HasSuffix(rel, ".json"):
			t.Errorf("the intent %s is left", rel)
		}
	}
	if identities != 4 || receipts != 4 || len(again.Receipts) != 4 {
		t.Errorf("%d identity records, %d receipts on disk, %d returned", identities, receipts, len(again.Receipts))
	}
}

// withoutBookkeeping is the snapshot without what the harness keeps for every
// command: the lock and the directories of the store.
func withoutBookkeeping(snap map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range snap {
		if strings.HasSuffix(k, "/") || strings.HasPrefix(k, "locks/") {
			continue
		}
		out[k] = v
	}
	return out
}

// The top level recorded for a clone is its working tree, never the directory
// Git keeps apart from it: a repository made with a separate Git directory and
// a submodule are named by the path of either their working tree or, for a
// linked worktree, another tree of theirs.
func TestAClonesRecordedTopLevelIsItsWorkingTree(t *testing.T) {
	isolateGit(t)
	base := physical(t, t.TempDir())
	git(t, base, "init", "--quiet", "--separate-git-dir="+filepath.Join(base, "meta"), "work")
	separate := filepath.Join(base, "work")
	git(t, separate, "commit", "--quiet", "--allow-empty", "-m", "start")

	super := newClone(t, "")
	git(t, super, "-c", "protocol.file.allow=always", "submodule", "--quiet", "add", separate, "sub")
	submodule := filepath.Join(super, "sub")

	for name, c := range map[string]struct{ path, want string }{
		"a separate git directory": {separate, separate},
		"a submodule":              {submodule, submodule},
	} {
		resp := send(t, newHome(t), request(t, mint(t, ids.Home), fleetBody(mint(t, ids.Home), projectSpec("tool", cloneSpec(filepath.ToSlash(c.path), "")))))
		if resp.Status != envelope.Ok {
			t.Fatalf("%s: %s: %+v", name, resp.Status, resp.Error)
		}
		clone := object(t, list(t, list(t, resp.Body, "projects")[0], "clones")[0], "record")
		if clone["worktree_root"] != filepath.ToSlash(c.want) {
			t.Errorf("%s: the top level is %v, not %s", name, clone["worktree_root"], c.want)
		}
	}
}

// What Git reports of a clone is recorded as it is: a remote url that ends in a
// newline keeps it.
func TestACloneRecordsItsRemoteUrlWholeToTheByte(t *testing.T) {
	isolateGit(t)
	url := "https://example.test/acme/tool.git\n"
	clone := newClone(t, "")
	git(t, clone, "config", "remote.origin.url", url)
	resp := send(t, newHome(t), request(t, mint(t, ids.Home), fleetBody(mint(t, ids.Home), projectSpec("tool", cloneSpec(clone, "origin")))))
	if resp.Status != envelope.Ok {
		t.Fatalf("%s: %+v", resp.Status, resp.Error)
	}
	rec := object(t, list(t, list(t, resp.Body, "projects")[0], "clones")[0], "record")
	if got := object(t, rec, "origin")["url"]; got != url {
		t.Errorf("origin url %q, not %q", got, url)
	}
}

// Every entry for a record the invocation created reads created, however many
// times the request names it.
func TestARecordCreatedHereIsMarkedCreatedWhereverTheRequestNamesIt(t *testing.T) {
	isolateGit(t)
	clone := newClone(t, "")
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, clone, "worktree", "add", "--quiet", "-b", "side", linked)
	resp := send(t, newHome(t), request(t, mint(t, ids.Home), fleetBody(mint(t, ids.Home),
		projectSpec("tool", cloneSpec(clone, ""), cloneSpec(filepath.ToSlash(linked), "")),
		projectSpec("tool", cloneSpec(clone, "")))))
	if resp.Status != envelope.Ok {
		t.Fatalf("%s: %+v", resp.Status, resp.Error)
	}
	if got := marks(t, resp.Body); !reflect.DeepEqual(got, all(true, len(got))) || len(got) != 7 {
		t.Errorf("marks %v", got)
	}
	if len(resp.Receipts) != 4 {
		t.Errorf("%d receipts for one home, workspace, project and clone", len(resp.Receipts))
	}
}

// The request always carries scope_level hive: a request at any other scope is
// refused whole, however it names things, and writes nothing.
func TestAProvisioningRequestAtAnotherScopeIsRefused(t *testing.T) {
	isolateGit(t)
	home := newHome(t)
	homeID := mint(t, ids.Home)
	first := send(t, home, request(t, homeID, fleetBody(mint(t, ids.Home), projectSpec("tool"))))
	if first.Status != envelope.Ok {
		t.Fatalf("%s: %+v", first.Status, first.Error)
	}
	wsID := object(t, first.Body, "workspace", "record")["workspace_id"].(string)
	projectID := list(t, first.Body, "projects")[0]["record"].(map[string]any)["project_id"].(string)
	before := withoutOutcomes(snapshot(t, home))

	for name, scope := range map[string]map[string]any{
		"workspace": {"scope_level": "workspace", "workspace_id": wsID},
		"project":   {"scope_level": "project", "workspace_id": wsID, "project_id": projectID},
	} {
		p := request(t, homeID, fleetBody(mint(t, ids.Home), projectSpec("sibling")))
		parsed, err := canon.Parse(p.data)
		if err != nil {
			t.Fatal(err)
		}
		m := parsed.(map[string]any)
		for k, v := range scope {
			m[k] = v
		}
		if p.data, err = canon.Encode(m); err != nil {
			t.Fatal(err)
		}
		expect(t, send(t, home, p), envelope.Refused, envelope.CodeScopeMismatch)
		if !reflect.DeepEqual(withoutOutcomes(snapshot(t, home)), before) {
			t.Errorf("%s: the home changed", name)
		}
	}
}
