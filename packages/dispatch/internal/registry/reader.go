package registry

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/schema"
	"github.com/kesk-git/hive/contract/store"
)

// registryDir is where the identity registry sits in a home
// (spec/identities.md, The four identity records).
const registryDir = "dispatch/registry"

// A kind of identity record: where its records sit, the schema they follow, the
// name they carry and the newest version this build reads.
type kind struct {
	dir     string
	schema  string
	record  string
	version string
	idField string
}

var (
	homeKind      = kind{"", "urn:hive:schema:home:v1", "hive.home", "1.0", "home_id"}
	workspaceKind = kind{"workspaces", "urn:hive:schema:workspace:v1", "hive.workspace", "1.0", "workspace_id"}
	projectKind   = kind{"projects", "urn:hive:schema:project:v1", "hive.project", "1.0", "project_id"}
	cloneKind     = kind{"clones", "urn:hive:schema:clone:v1", "hive.clone", "1.0", "clone_id"}
)

// path is the path of the record of the kind with the id, relative to the home.
func (k kind) path(id string) string {
	if k.dir == "" {
		return store.HomeFilePath
	}
	return registryDir + "/" + k.dir + "/" + id + ".json"
}

// reader reads identity records of the home through the validated reader: a
// record file of a version this build reads, valid under its schema, naming
// the identity its file name does. A record that cannot be read or fails its
// schema is record_malformed, never repaired by guessing.
type reader struct {
	home *store.Home
	reg  *schema.Registry
}

func malformed(rel, what string) error {
	return envelope.Common(envelope.CodeRecordMalformed, fmt.Sprintf("the registry record %s is malformed: %s", rel, what))
}

// read returns the record of the kind with the id, or nil when none is there.
func (r *reader) read(k kind, id string) (map[string]any, error) {
	return r.readPath(k, k.path(id), id)
}

func (r *reader) readPath(k kind, rel, id string) (map[string]any, error) {
	path, err := r.home.Path(rel)
	if err != nil {
		return nil, malformed(rel, err.Error())
	}
	data, err := store.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, malformed(rel, err.Error())
	}
	if err := canon.CheckFile(data); err != nil {
		return nil, malformed(rel, err.Error())
	}
	v, err := canon.Parse(data)
	if err != nil {
		return nil, malformed(rel, err.Error())
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, malformed(rel, "a record is a JSON object")
	}
	if obj["schema"] == k.record {
		if version, _ := obj["schema_version"].(string); envelope.Newer(version, k.version) {
			return nil, envelope.Common(envelope.CodeUnsupportedVersion, fmt.Sprintf("the registry record %s is at version %s; this build reads up to %s", rel, version, k.version))
		}
	}
	if err := r.reg.Validate(k.schema, data); err != nil {
		return nil, malformed(rel, err.Error())
	}
	if obj[k.idField] != id {
		return nil, malformed(rel, "it names another identity than its file")
	}
	return obj, nil
}

// all returns every record of the kind in the registry, in the order of their
// file names. An entry that does not end in .json is not a record and is left
// alone.
func (r *reader) all(k kind) ([]map[string]any, error) {
	rel := registryDir + "/" + k.dir
	dir, err := r.home.Path(rel)
	if err != nil {
		return nil, malformed(rel, err.Error())
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, malformed(rel, err.Error())
	}
	var out []map[string]any
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		obj, err := r.readPath(k, rel+"/"+e.Name(), id)
		if err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	return out, nil
}
