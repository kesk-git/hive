package registry_test

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/schema"
)

// The story: hive-fleet provision, pointed at one fresh home, creates the home,
// workspace, project and clone identity records there and does nothing else
// (spec/identities.md, Creating identities); and the same request sent again
// returns the same response and writes nothing more (spec/commands.md,
// Replay).
func TestProvisionCreatesTheFourIdentityRecordsAndAReplayWritesNothing(t *testing.T) {
	isolateGit(t)
	home := newHome(t)
	clone := newClone(t, "https://github.com/example/widgets.git")
	before := snapshot(t, clone)

	homeID, parentID := mint(t, ids.Home), mint(t, ids.Home)
	req := request(t, homeID, map[string]any{
		"home":      map[string]any{"role": "fleet", "name": "Widgets", "parent_home_id": parentID},
		"workspace": map[string]any{"slug": "widgets", "name": "Widgets"},
		"projects": []any{map[string]any{
			"slug": "widgets-app", "name": "Widgets app",
			"repository": map[string]any{"kind": "github", "host": "github.com", "owner": "example", "name": "widgets"},
			"clones":     []any{map[string]any{"worktree_root": clone, "remote_name": "origin"}},
		}},
	})

	resp := send(t, home, req)
	if resp.Status != envelope.Ok || resp.Replayed {
		t.Fatalf("status %s, replayed %v: %+v", resp.Status, resp.Replayed, resp.Error)
	}

	// The response names the four records, each created.
	homeRec := object(t, resp.Body, "home", "record")
	wsRec := object(t, resp.Body, "workspace", "record")
	projects := list(t, resp.Body, "projects")
	if len(projects) != 1 {
		t.Fatalf("%d projects in the response", len(projects))
	}
	projRec := object(t, projects[0], "record")
	clones := list(t, projects[0], "clones")
	if len(clones) != 1 {
		t.Fatalf("%d clones in the response", len(clones))
	}
	cloneRec := object(t, clones[0], "record")
	for name, created := range map[string]any{
		"home": resp.Body["home"].(map[string]any)["created"], "workspace": resp.Body["workspace"].(map[string]any)["created"],
		"project": projects[0]["created"], "clone": clones[0]["created"],
	} {
		if created != true {
			t.Errorf("the %s is not marked created", name)
		}
	}
	if homeRec["home_id"] != homeID {
		t.Errorf("the home took %v, not the proposed %s", homeRec["home_id"], homeID)
	}
	wsID, projID, cloneID := wsRec["workspace_id"].(string), projRec["project_id"].(string), cloneRec["clone_id"].(string)
	for id, prefix := range map[string]ids.Prefix{wsID: ids.Workspace, projID: ids.Project, cloneID: ids.Clone} {
		if err := ids.Check(id, prefix); err != nil {
			t.Error(err)
		}
	}

	// On disk there are exactly those four identity records, each as the
	// response returned it, valid under its schema.
	records := map[string]map[string]any{
		"home.json": homeRec,
		"dispatch/registry/workspaces/" + wsID + ".json": wsRec,
		"dispatch/registry/projects/" + projID + ".json": projRec,
		"dispatch/registry/clones/" + cloneID + ".json":  cloneRec,
	}
	schemas := map[string]string{
		"home.json": "urn:hive:schema:home:v1", "workspaces": "urn:hive:schema:workspace:v1",
		"projects": "urn:hive:schema:project:v1", "clones": "urn:hive:schema:clone:v1",
	}
	reg, err := schema.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, home)
	var identities []string
	for rel := range after {
		if rel == "home.json" || strings.HasPrefix(rel, "dispatch/registry/") && strings.HasSuffix(rel, ".json") {
			identities = append(identities, rel)
		}
	}
	if want := slices.Sorted(maps.Keys(records)); !slices.Equal(slices.Sorted(slices.Values(identities)), want) {
		t.Fatalf("identity records on disk %v; want %v", identities, want)
	}
	for rel, want := range records {
		kind := "home.json"
		if rel != "home.json" {
			kind = strings.Split(rel, "/")[2]
		}
		if err := reg.Validate(schemas[kind], []byte(after[rel])); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
		got, err := canon.Parse([]byte(after[rel]))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, any(want)) {
			t.Errorf("%s is not the record the response returned:\n disk     %v\n response %v", rel, got, want)
		}
	}

	// The records name each other as the contract fixes.
	if homeRec["role"] != "fleet" || homeRec["workspace_id"] != wsID || object(t, homeRec, "parent")["home_id"] != parentID {
		t.Errorf("the home record %v", homeRec)
	}
	if wsRec["home_id"] != homeID || wsRec["parent_home_id"] != parentID || wsRec["slug"] != "widgets" {
		t.Errorf("the workspace record %v", wsRec)
	}
	if projRec["workspace_id"] != wsID || projRec["home_id"] != homeID || projRec["slug"] != "widgets-app" {
		t.Errorf("the project record %v", projRec)
	}
	common := physical(t, filepath.Join(clone, ".git"))
	if cloneRec["project_id"] != projID || cloneRec["git_common_dir"] != common || cloneRec["worktree_root"] != clone || cloneRec["object_format"] != "sha1" {
		t.Errorf("the clone record %v; the common directory is %s", cloneRec, common)
	}
	if origin := object(t, cloneRec, "origin"); origin["kind"] != "remote" || origin["remote_name"] != "origin" || origin["url"] != "https://github.com/example/widgets.git" {
		t.Errorf("the clone origin %v", origin)
	}

	// One receipt per record created, each naming its record as its subject,
	// and on disk the same four.
	kinds := map[string]string{
		"dispatch.home.created": homeID, "dispatch.workspace.registered": wsID,
		"dispatch.project.registered": projID, "dispatch.clone.registered": cloneID,
	}
	if len(resp.Receipts) != len(kinds) {
		t.Fatalf("%d receipts, want %d", len(resp.Receipts), len(kinds))
	}
	for _, r := range resp.Receipts {
		if want, ok := kinds[r.Kind]; !ok || r.Subject == nil || r.Subject.ID != want || len(r.Body) != 0 {
			t.Errorf("receipt %s names %v with body %v", r.Kind, r.Subject, r.Body)
		}
		delete(kinds, r.Kind)
	}
	var onDisk int
	for rel := range after {
		if strings.HasPrefix(rel, "dispatch/receipts/") && strings.HasSuffix(rel, ".json") {
			onDisk++
		}
	}
	if onDisk != 4 {
		t.Errorf("%d receipt files on disk", onDisk)
	}

	// Nothing else: the home holds only its record, dispatch's own part, the
	// locks and the journal, which holds no event, and the clone and its Git
	// directory are as they were.
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "home.json" && e.Name() != "dispatch" && e.Name() != "locks" && e.Name() != "journal" {
			t.Errorf("the home holds %s", e.Name())
		}
	}
	if lines, err := os.ReadDir(filepath.Join(home, "journal")); err != nil || len(lines) != 0 {
		t.Errorf("the journal holds %d entries (%v)", len(lines), err)
	}
	if !reflect.DeepEqual(before, snapshot(t, clone)) {
		t.Error("the clone changed")
	}

	// The same request again: the same response, marked replayed, and not one
	// byte more in the home.
	again := send(t, home, req)
	if again.Status != envelope.Ok || !again.Replayed {
		t.Fatalf("the replay: status %s, replayed %v: %+v", again.Status, again.Replayed, again.Error)
	}
	if !reflect.DeepEqual(again.Body, resp.Body) {
		t.Errorf("the replay's body differs:\n first  %v\n replay %v", resp.Body, again.Body)
	}
	if !reflect.DeepEqual(again.Receipts, resp.Receipts) {
		t.Error("the replay's receipts differ")
	}
	if !reflect.DeepEqual(after, snapshot(t, home)) {
		t.Error("the replay wrote something")
	}
}
