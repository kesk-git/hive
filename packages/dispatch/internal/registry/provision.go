package registry

import (
	"fmt"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/schema"
	"github.com/kesk-git/hive/contract/store"
)

// provision is hive-fleet provision (spec/identities.md, Creating identities).
// It reads everything it needs, the clones included, and decides what is to be
// created before it writes anything: the records and receipts it returns are
// the one transaction the harness commits with the recorded outcome, so that a
// request which fails leaves nothing and one which succeeds leaves all.
func provision(c *cli.Call) (*cli.Result, error) {
	if c.Request.Scope.Level != envelope.ScopeHive {
		return nil, envelope.Common(envelope.CodeScopeMismatch, "provisioning creates the scope it works in, so its request carries scope_level hive")
	}
	reg, err := schema.Embedded()
	if err != nil {
		return nil, envelope.Common(envelope.CodeInternal, err.Error())
	}
	p := &provisioner{call: c, reg: reg, rd: &reader{c.Home, reg}, homeID: c.Request.HomeID, fresh: map[string]bool{}}
	if p.now, err = recordedAt(c); err != nil {
		return nil, err
	}
	if len(c.Home.Unknown) > 0 {
		fmt.Fprintf(c.Stderr, "hive-fleet provision: the home holds entries this specification does not name, left alone: %v\n", c.Home.Unknown)
	}
	body, err := p.run(c.Request.Body)
	if err != nil {
		return nil, err
	}
	return &cli.Result{Body: body, Records: p.records, Receipts: p.receipts}, nil
}

// provisioner is one provisioning in hand: the records and receipts it will
// write, in order.
type provisioner struct {
	call   *cli.Call
	reg    *schema.Registry
	rd     *reader
	homeID string
	// now is the one time at which every record of the request is written.
	now string

	records  []store.RecordWrite
	receipts []envelope.Receipt
	// fresh are the identifiers of the records this invocation created, whichever
	// number of times the request names them.
	fresh map[string]bool
	// clones are the clones met so far in this request, created or found, with
	// the project each belongs to, so that a clone named twice is one.
	clones []*cloneEntry
}

// A clone met: its common directory, its record and the project it belongs to.
type cloneEntry struct {
	commonDir string
	projectID string
	record    map[string]any
}

// run reads the request's body and returns the response's body.
func (p *provisioner) run(body map[string]any) (map[string]any, error) {
	spec, _ := body["home"].(map[string]any)
	role, _ := spec["role"].(string)
	name, _ := spec["name"].(string)
	parentID, _ := spec["parent_home_id"].(string)

	out := map[string]any{}
	homeRec, homeNew, err := p.home(role, name, parentID)
	if err != nil {
		return nil, err
	}
	out["home"] = created(homeNew, homeRec)
	if role == "fleet" {
		wsID, _ := homeRec["workspace_id"].(string)
		wsSpec, _ := body["workspace"].(map[string]any)
		wsRec, wsCreated, err := p.workspace(wsID, wsSpec, parentID)
		if err != nil {
			return nil, err
		}
		out["workspace"] = created(wsCreated, wsRec)
	}
	projects := []any{}
	if list, _ := body["projects"].([]any); len(list) > 0 {
		wsID, _ := homeRec["workspace_id"].(string)
		existing, err := p.rd.all(projectKind)
		if err != nil {
			return nil, err
		}
		for i, item := range list {
			spec, _ := item.(map[string]any)
			entry, newer, err := p.project(wsID, spec, existing, i)
			if err != nil {
				return nil, err
			}
			existing = newer
			projects = append(projects, entry)
		}
	}
	out["projects"] = projects
	return out, nil
}

// created is an entry of the response: a record and whether this invocation
// created it.
func created(isNew bool, record map[string]any) map[string]any {
	return map[string]any{"created": isNew, "record": record}
}

// home creates the home record or finds it. The record is the home's identity,
// adopted from the request; a fleet home names its one workspace, minted here,
// and its parent. What is fixed about an existing home, its role and for a
// fleet its parent, is held to: a request that would change it is a conflict
// with already_exists and nothing is written.
func (p *provisioner) home(role, name, parentID string) (map[string]any, bool, error) {
	if p.call.Home.HomeID != "" {
		rec, err := p.rd.read(homeKind, p.homeID)
		if err != nil {
			return nil, false, err
		}
		if rec == nil {
			return nil, false, malformed(store.HomeFilePath, "the home record cannot be read")
		}
		if rec["role"] != role {
			return nil, false, envelope.Common(envelope.CodeAlreadyExists, "the home already exists with another role; a home's role is fixed")
		}
		if role == "fleet" {
			if parent, _ := rec["parent"].(map[string]any); parent["home_id"] != parentID {
				return nil, false, envelope.Common(envelope.CodeAlreadyExists, "the home already exists under another parent home; a home's parent is fixed")
			}
		}
		return rec, false, nil
	}
	rec := map[string]any{
		"home_id": p.homeID, "role": role, "name": name,
		"parent": map[string]any{"kind": "none"},
	}
	if role == "fleet" {
		rec["workspace_id"] = p.mint(ids.Workspace)
		rec["parent"] = map[string]any{"kind": "home", "home_id": parentID}
	}
	rec, err := p.write(homeKind, p.homeID, rec, homeCreated)
	return rec, true, err
}

// workspace creates the workspace record or finds the one the home names. A
// workspace is idempotent on its slug: the one the existing home names must
// have the slug of the request, and a workspace's slug is fixed.
func (p *provisioner) workspace(id string, spec map[string]any, parentID string) (map[string]any, bool, error) {
	if p.call.Home.HomeID != "" {
		rec, err := p.rd.read(workspaceKind, id)
		if err != nil {
			return nil, false, err
		}
		if rec == nil {
			return nil, false, malformed(workspaceKind.path(id), "the home names a workspace the registry does not hold")
		}
		if rec["slug"] != spec["slug"] {
			return nil, false, envelope.Common(envelope.CodeAlreadyExists, "the home already holds a workspace with another slug; a workspace's slug is fixed")
		}
		return rec, false, nil
	}
	rec, err := p.write(workspaceKind, id, map[string]any{
		"workspace_id": id, "slug": spec["slug"], "name": spec["name"],
		"home_id": p.homeID, "parent_home_id": parentID, "state": "active",
	}, workspaceRegistered)
	return rec, true, err
}

// project creates the project record under the workspace or finds the one with
// the slug, then registers the clones it names. existing are the projects of
// the registry and those met in this request; the list with the project is
// returned.
func (p *provisioner) project(wsID string, spec map[string]any, existing []map[string]any, index int) (map[string]any, []map[string]any, error) {
	var rec map[string]any
	for _, e := range existing {
		if e["workspace_id"] == wsID && e["slug"] == spec["slug"] {
			rec = e
			break
		}
	}
	if rec == nil {
		id := p.mint(ids.Project)
		var err error
		rec, err = p.write(projectKind, id, map[string]any{
			"project_id": id, "workspace_id": wsID, "home_id": p.homeID,
			"slug": spec["slug"], "name": spec["name"], "repository": spec["repository"], "state": "active",
		}, projectRegistered)
		if err != nil {
			return nil, nil, err
		}
		existing = append(existing, rec)
	}
	clones := []any{}
	list, _ := spec["clones"].([]any)
	known, err := p.rd.all(cloneKind)
	if err != nil {
		return nil, nil, err
	}
	for j, item := range list {
		cs, _ := item.(map[string]any)
		entry, err := p.clone(rec["project_id"].(string), wsID, cs, known, fmt.Sprintf("projects[%d].clones[%d].worktree_root", index, j))
		if err != nil {
			return nil, nil, err
		}
		clones = append(clones, entry)
	}
	entry := created(p.fresh[rec["project_id"].(string)], rec)
	entry["clones"] = clones
	return entry, existing, nil
}

// clone resolves the clone the path lies in and creates its record under the
// project, or finds it. The clone is idempotent on its physical Git common
// directory, compared by file identity and never by the spelling of the
// path; registered under another project it is a conflict with already_exists.
func (p *provisioner) clone(projectID, wsID string, spec map[string]any, known []map[string]any, at string) (map[string]any, error) {
	path, _ := spec["worktree_root"].(string)
	remote, _ := spec["remote_name"].(string)
	facts, err := readClone(path, remote, at)
	if err != nil {
		return nil, err
	}
	var met *cloneEntry
	for _, e := range p.clones {
		if sameDirectory(e.commonDir, facts.commonDir) {
			met = e
		}
	}
	if met == nil {
		for _, rec := range known {
			if dir, _ := rec["git_common_dir"].(string); sameDirectory(dir, facts.commonDir) {
				met = &cloneEntry{commonDir: dir, projectID: rec["project_id"].(string), record: rec}
				break
			}
		}
	}
	if met != nil {
		if met.projectID != projectID {
			return nil, envelope.Common(envelope.CodeAlreadyExists, at+" is a clone already registered under another project; a clone's common directory stays with its project")
		}
		p.clones = append(p.clones, met)
		return created(p.fresh[met.record["clone_id"].(string)], met.record), nil
	}
	id := p.mint(ids.Clone)
	origin := map[string]any{"kind": "none"}
	if facts.hasRemote {
		origin = map[string]any{"kind": "remote", "remote_name": remote, "url": facts.remoteURL}
	}
	rec, err := p.write(cloneKind, id, map[string]any{
		"clone_id": id, "project_id": projectID, "workspace_id": wsID, "home_id": p.homeID,
		"git_common_dir": facts.commonDir, "worktree_root": facts.worktreeRoot,
		"object_format": facts.objectFormat, "origin": origin, "state": "active", "verified_at": p.now,
	}, cloneRegistered)
	if err != nil {
		return nil, err
	}
	p.clones = append(p.clones, &cloneEntry{commonDir: facts.commonDir, projectID: projectID, record: rec})
	return created(true, rec), nil
}

// mint makes an identifier of the prefix, which the command alone does for a
// workspace, a project and a clone.
func (p *provisioner) mint(prefix ids.Prefix) string {
	id, err := ids.Mint(p.call.Clock, prefix)
	if err != nil {
		panic(err) // the harness answers a panic as failed with internal
	}
	return id
}

// write makes the record of the kind whole, checks it against its schema, and
// adds it to the transaction as a record expected to be absent, with the
// receipt of the kind that says it was created. It returns the record as it
// will be stored.
func (p *provisioner) write(k kind, id string, rec map[string]any, receiptKind string) (map[string]any, error) {
	rec["schema"], rec["schema_version"] = k.record, k.version
	rec["record_revision"] = int64(1)
	rec["created_at"], rec["updated_at"] = p.now, p.now
	rec["writer"] = map[string]any{
		"package": p.call.Executable.Package, "command": p.call.Command.Name,
		"operation": p.call.Operation.Name, "implementation_version": p.call.Executable.Version,
	}
	content, err := canon.Encode(rec)
	if err != nil {
		return nil, envelope.Common(envelope.CodeInternal, "a record cannot be written: "+err.Error())
	}
	w, digest, err := p.call.Finalize(store.RecordWrite{Path: k.path(id), Content: content, Expected: 0})
	if err != nil {
		return nil, envelope.Common(envelope.CodeInternal, err.Error())
	}
	if err := p.reg.Validate(k.schema, w.Content); err != nil {
		return nil, envelope.Common(envelope.CodeSchemaViolation, "the "+k.record+" record cannot be kept whole: "+err.Error())
	}
	stored, err := canon.Parse(w.Content)
	if err != nil {
		return nil, envelope.Common(envelope.CodeInternal, err.Error())
	}
	subject := &envelope.RecordRef{Schema: k.record, ID: id, HomeID: p.homeID, RecordRevision: 1, Digest: digest}
	receipt, err := p.call.NewReceipt(receiptKind, "1.0", subject, nil, nil)
	if err != nil {
		return nil, envelope.Common(envelope.CodeInternal, err.Error())
	}
	p.fresh[id] = true
	p.records = append(p.records, w)
	p.receipts = append(p.receipts, receipt)
	return stored.(map[string]any), nil
}

// recordedAt is the one time at which the records of the invocation are
// written, as the harness stamps them.
func recordedAt(c *cli.Call) (string, error) {
	w, _, err := c.Finalize(store.RecordWrite{Path: store.HomeFilePath, Content: []byte("{}")})
	if err != nil {
		return "", envelope.Common(envelope.CodeInternal, err.Error())
	}
	v, err := canon.Parse(w.Content)
	if err != nil {
		return "", envelope.Common(envelope.CodeInternal, err.Error())
	}
	at, _ := v.(map[string]any)["updated_at"].(string)
	return at, nil
}
