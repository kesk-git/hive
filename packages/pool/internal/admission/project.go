package admission

import (
	"errors"
	"io/fs"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/schema"
	cstore "github.com/kesk-git/hive/contract/store"
)

// repositoryKind is the kind of the repository binding the project record
// holds, github or local-only, read through the contract library's validated
// reader of a record file from the home's identity registry.
func repositoryKind(c *cli.Call, projectID string) (string, error) {
	path, err := c.Home.Path("dispatch/registry/projects/" + projectID + ".json")
	if err != nil {
		return "", malformed("the project record cannot be read", projectID)
	}
	data, err := cstore.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", notFound("project")
	}
	if err != nil {
		return "", malformed("the project record cannot be read", projectID)
	}
	v, err := canon.Parse(data)
	if err == nil {
		err = canon.CheckFile(data)
	}
	if err != nil {
		return "", malformed("the project record cannot be read", projectID)
	}
	reg, err := schema.Embedded()
	if err != nil {
		return "", envelope.Common(envelope.CodeInternal, "the embedded schemas do not load").Because(err)
	}
	if err := reg.Validate("urn:hive:schema:project:v1", data); err != nil {
		return "", malformed("the project record cannot be read", projectID)
	}
	project := tree(v)
	if project["project_id"] != projectID {
		return "", malformed("the project record cannot be read", projectID)
	}
	return str(tree(project["repository"])["kind"]), nil
}
