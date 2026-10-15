package imperative

import (
	"errors"
	"io/fs"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/schema"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/read"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The operations create and revise of hive-imperative (spec/pool.md, The
// Imperative). create writes revision 1 of an item's canonical Imperative and
// revise the next, whole, with the reason, under the record's expected
// revision. Both copy the Placement axis from the item at the revision they read
// it and the repository binding from the project record, compute the revision's
// digest over the canonical form of its class and four sections, and write
// pool.imperative.revised. create refuses a Target, and a Prey that has a Hunt
// beneath it, with pool.not_dispatchable: they describe work that others do. A
// revision is never changed; steering writes the next one.

// Create writes revision 1 of the item's Imperative.
func Create(c *cli.Call) (*cli.Result, error) { return author(c, false) }

// Revise writes the next revision of the item's Imperative.
func Revise(c *cli.Call) (*cli.Result, error) { return author(c, true) }

func author(c *cli.Call, revising bool) (*cli.Result, error) {
	body := c.Request.Body
	taskID := strOf(body["task_id"])
	if revising && !expects(c, "pool."+kindImp, taskID) {
		return nil, invalid("a revise request names the Imperative's expected revision")
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}

	g, omitted, err := read.LoadGraph(c.Store, c.Clock.Now())
	if err != nil {
		return nil, poolstore.Failure("the Pool", err)
	}
	if len(omitted) > 0 {
		return nil, malformed("a record of the Pool cannot be read, so the item cannot be known to be dispatchable", omitted[0].ID)
	}
	item := g.Items[taskID]
	if item == nil {
		return nil, notFound("item")
	}
	if err := checkScope(c, item.Data); err != nil {
		return nil, err
	}
	if !revising && (item.Data["deliverable"] == "decision" || g.Derived(taskID)) {
		return nil, refuse(codeNotDispatchable, "a decision item, a Target and a Prey that has a Hunt beneath it describe work that others do and have no Imperative")
	}

	scope := scopeOf(item.Data)
	setup, err := setupOf(c, item, scope)
	if err != nil {
		return nil, err
	}
	now, err := c.Stamp()
	if err != nil {
		return nil, internal("a revision cannot be given a time", err)
	}

	var current *poolstore.Loaded
	number := int64(1)
	if revising {
		if current, err = loadImperative(c.Store, taskID); err != nil {
			return nil, err
		}
		if err := checkScope(c, current.Data); err != nil {
			return nil, err
		}
		number = intOf(current.Data["current_revision"]) + 1
	}
	rev := map[string]any{
		"revision": number, "class": body["class"], "task": copyValue(body["task"]), "setup": setup,
		"rules": copyValue(body["rules"]), "definition_of_done": copyValue(body["definition_of_done"]),
		"authored_at": now, "authored_by": callerTree(c), "authority": authorityTree(c), "request_id": c.Request.RequestID,
	}
	if revising {
		rev["supersedes_reason"] = body["supersedes_reason"]
	}
	if rev["digest"], err = revisionDigest(rev); err != nil {
		return nil, internal("a revision cannot be digested", err)
	}

	var rec map[string]any
	var expected int64
	if revising {
		rec, expected = copyMap(current.Data), current.Revision
		rec["revisions"] = append(listOf(rec["revisions"]), rev)
		rec["current_revision"], rec["updated_at"] = number, now
	} else {
		rec = map[string]any{
			"schema": "pool.imperative", "task_id": taskID, "current_revision": number,
			"revisions": []any{rev}, "deliveries": []any{}, "created_at": now, "updated_at": now,
		}
		putScope(rec, scope)
	}
	rec["schema_version"] = schemaVersion

	b := &builder{c: c}
	final, ref, err := b.put(kindImp, taskID, rec, expected)
	if err != nil {
		return nil, err
	}
	scope.TaskID = taskID
	if err := b.receipt(receiptRevised, ref, scope, nil); err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"imperative": final})
}

// setupOf is the Setup of the revision: the request's, with the Placement axis
// the item carries at the revision read and the repository binding. The
// repository is the project record's where the item's scope names a project,
// and is then not the request's to give; for the Hive's own work at no project it
// is given.
func setupOf(c *cli.Call, item *read.Node, scope envelope.Scope) (map[string]any, error) {
	setup := copyMap(mapOf(c.Request.Body["setup"]))
	given, hasGiven := setup["repository"]
	switch {
	case scope.ProjectID == "":
		if !hasGiven {
			return nil, invalid("the repository is given for work at no project")
		}
	default:
		repository, err := projectRepository(c, scope.ProjectID)
		if err != nil {
			return nil, err
		}
		if hasGiven && !sameTree(given, repository) {
			return nil, invalid("the repository is read from the project record, and the request gives another")
		}
		setup["repository"] = repository
	}
	setup["placement"] = map[string]any{"requested": item.Data["placement_requested"], "item_revision": item.Revision}
	return setup, nil
}

// projectRepository reads the repository binding of the project from its record
// in the home's identity registry, through the contract library's validated
// reader of a record file.
func projectRepository(c *cli.Call, projectID string) (any, error) {
	path, err := c.Home.Path("dispatch/registry/projects/" + projectID + ".json")
	if err != nil {
		return nil, malformed("the project record cannot be read", projectID)
	}
	data, err := cstore.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, notFound("project")
	}
	if err != nil {
		return nil, malformed("the project record cannot be read", projectID)
	}
	v, err := canon.Parse(data)
	if err == nil {
		err = canon.CheckFile(data)
	}
	if err != nil {
		return nil, malformed("the project record cannot be read", projectID)
	}
	reg, err := schema.Embedded()
	if err != nil {
		return nil, internal("the embedded schemas do not load", err)
	}
	if err := reg.Validate("urn:hive:schema:project:v1", data); err != nil {
		return nil, malformed("the project record cannot be read", projectID)
	}
	project := mapOf(v)
	if project["project_id"] != projectID || project["repository"] == nil {
		return nil, malformed("the project record cannot be read", projectID)
	}
	return project["repository"], nil
}

// sameTree says whether two decoded values are one value.
func sameTree(a, b any) bool {
	x, err := canon.Encode(a)
	if err != nil {
		return false
	}
	y, err := canon.Encode(b)
	return err == nil && string(x) == string(y)
}
