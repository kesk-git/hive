package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/schema"
	"github.com/kesk-git/hive/contract/store"
)

// The identity registry (spec/identities.md, The four identity records):
// the one store this specification places beyond a package's five places,
// read by every callee for the scope check.
const registryDir = "dispatch/registry"

// A registry record kind: where its records sit, the schema they follow and
// the name and newest version this build reads.
type registryKind struct {
	dir     string
	schema  string
	record  string
	version string
	idField string
}

var (
	workspaceKind = registryKind{"workspaces", "urn:hive:schema:workspace:v1", "hive.workspace", "1.0", "workspace_id"}
	projectKind   = registryKind{"projects", "urn:hive:schema:project:v1", "hive.project", "1.0", "project_id"}
	cloneKind     = registryKind{"clones", "urn:hive:schema:clone:v1", "hive.clone", "1.0", "clone_id"}
)

// checkScope is the callee's scope check (spec/identities.md, Scope): the
// workspace, project and clone the request names exist in the home's
// registry, are active and belong to each other, and the workspace belongs
// to this home. It reads each record through the validated reader, takes no
// lock and calls no package. A record that is missing, retired or bound
// elsewhere is refused with scope_mismatch; one that cannot be read or
// fails its schema is record_malformed, as every read of a malformed record
// reports.
func checkScope(home *store.Home, s envelope.Scope) error {
	if s.Level == envelope.ScopeHive {
		return nil
	}
	mismatch := func(format string, args ...any) error {
		return envelope.Common(envelope.CodeScopeMismatch, fmt.Sprintf(format, args...))
	}
	ws, err := readRegistry(home, workspaceKind, s.WorkspaceID)
	if err != nil {
		return err
	}
	if ws == nil {
		return mismatch("the workspace is not registered in this home")
	}
	if ws["state"] != "active" {
		return mismatch("the workspace is not active")
	}
	if ws["home_id"] != home.HomeID && ws["parent_home_id"] != home.HomeID {
		return mismatch("the workspace does not belong to this home")
	}
	if s.Level == envelope.ScopeWorkspace {
		return nil
	}
	proj, err := readRegistry(home, projectKind, s.ProjectID)
	if err != nil {
		return err
	}
	switch {
	case proj == nil:
		return mismatch("the project is not registered in this home")
	case proj["state"] != "active":
		return mismatch("the project is not active")
	case proj["workspace_id"] != s.WorkspaceID:
		return mismatch("the project does not belong to the workspace")
	case proj["home_id"] != home.HomeID:
		return mismatch("the project does not belong to this home")
	}
	if s.CloneID == "" {
		return nil
	}
	clone, err := readRegistry(home, cloneKind, s.CloneID)
	if err != nil {
		return err
	}
	switch {
	case clone == nil:
		return mismatch("the clone is not registered in this home")
	case clone["state"] != "active":
		return mismatch("the clone is not active")
	case clone["project_id"] != s.ProjectID:
		return mismatch("the clone does not belong to the project")
	case clone["workspace_id"] != s.WorkspaceID:
		return mismatch("the clone does not belong to the workspace")
	case clone["home_id"] != home.HomeID:
		return mismatch("the clone does not belong to this home")
	}
	return nil
}

// readRegistry reads one identity record of the registry through the
// validated reader: a record file, of a version this build reads, valid
// under its schema, naming the identity its file name does. It returns nil
// for a record that is not there.
func readRegistry(home *store.Home, kind registryKind, id string) (map[string]any, error) {
	rel := registryDir + "/" + kind.dir + "/" + id + ".json"
	data, err := store.ReadFile(filepath.Join(home.Dir, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	malformed := func(what string) error {
		return envelope.Common(envelope.CodeRecordMalformed, fmt.Sprintf("the registry record %s is malformed: %s", rel, what))
	}
	if err != nil {
		return nil, malformed(err.Error())
	}
	if err := canon.CheckFile(data); err != nil {
		return nil, malformed(err.Error())
	}
	v, err := canon.Parse(data)
	if err != nil {
		return nil, malformed(err.Error())
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, malformed("a record is a JSON object")
	}
	if obj["schema"] == kind.record {
		if version, _ := obj["schema_version"].(string); envelope.Newer(version, kind.version) {
			return nil, envelope.Common(envelope.CodeUnsupportedVersion, fmt.Sprintf("the registry record %s is at version %s; this build reads up to %s", rel, version, kind.version))
		}
	}
	reg, err := schema.Embedded()
	if err != nil {
		return nil, envelope.Common(envelope.CodeInternal, err.Error())
	}
	if err := reg.Validate(kind.schema, data); err != nil {
		return nil, malformed(err.Error())
	}
	if obj[kind.idField] != id {
		return nil, malformed("it names another identity than its file")
	}
	return obj, nil
}
