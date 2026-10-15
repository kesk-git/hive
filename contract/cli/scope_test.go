package cli

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/internal/testsupport/access"
	"github.com/kesk-git/hive/contract/store"
)

// RegisteredScopes lists the workspace and project scopes the scope check
// admits, and no other: a retired workspace or project, a workspace of another
// home, and a project of a workspace not admitted are left out; an entry that
// is not a record is left alone; a record that cannot be read ends the list.
func TestRegisteredScopes(t *testing.T) {
	dir := newHome(t)
	retiredWS, otherWS, parentWS := id(ids.Workspace, 2), id(ids.Workspace, 3), id(ids.Workspace, 4)
	record(t, dir, "dispatch/registry/workspaces/"+retiredWS+".json", workspaceRecord(retiredWS, homeID, "retired"))
	record(t, dir, "dispatch/registry/workspaces/"+otherWS+".json", workspaceRecord(otherWS, id(ids.Home, 5), "active"))
	parent := workspaceRecord(parentWS, id(ids.Home, 5), "active")
	parent["parent_home_id"] = homeID
	record(t, dir, "dispatch/registry/workspaces/"+parentWS+".json", parent)
	inRetired, retired, inParent := id(ids.Project, 2), id(ids.Project, 3), id(ids.Project, 4)
	record(t, dir, "dispatch/registry/projects/"+inRetired+".json", projectRecord(inRetired, retiredWS, homeID, "active"))
	record(t, dir, "dispatch/registry/projects/"+retired+".json", projectRecord(retired, workspaceID, homeID, "retired"))
	record(t, dir, "dispatch/registry/projects/"+inParent+".json", projectRecord(inParent, parentWS, homeID, "active"))
	access.WriteFile(t, filepath.Join(dir, "dispatch", "registry", "workspaces", "notes.txt"), []byte("x"))
	home, err := store.CheckHome(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := RegisteredScopes(home)
	if err != nil {
		t.Fatal(err)
	}
	want := map[envelope.Scope]bool{
		{Level: envelope.ScopeWorkspace, HomeID: homeID, WorkspaceID: workspaceID}:                     true,
		{Level: envelope.ScopeWorkspace, HomeID: homeID, WorkspaceID: parentWS}:                        true,
		{Level: envelope.ScopeProject, HomeID: homeID, WorkspaceID: workspaceID, ProjectID: projectID}: true,
		{Level: envelope.ScopeProject, HomeID: homeID, WorkspaceID: parentWS, ProjectID: inParent}:     true,
	}
	if len(got) != len(want) {
		t.Fatalf("scopes %+v", got)
	}
	for _, s := range got {
		if !want[s] {
			t.Errorf("scope %+v is not admitted; scopes %+v", s, got)
		}
	}

	record(t, dir, "dispatch/registry/workspaces/"+retiredWS+".json", map[string]any{"schema": "hive.workspace", "schema_version": "1.0"})
	var f *envelope.Failure
	if _, err := RegisteredScopes(home); !errors.As(err, &f) || f.Err.Code != envelope.CodeRecordMalformed {
		t.Errorf("a malformed record: %v", err)
	}
}
