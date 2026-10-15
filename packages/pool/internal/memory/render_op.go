package memory

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// RenderOp is hive-memory render (spec/pool.md, Generated knowledge): it
// resolves the rules for the projection of the envelope's scope, the tier, the
// audience and, for a subject projection, its subject, at the instant given or
// the clock's, renders the text, and records the sources at the revisions read,
// the renderer version, the resolved-rule digest and the digest of the text,
// with the receipt and event of the rendering. A rendering equal to the current
// one in text, exact source revisions, rule inventory and budget revision
// writes nothing.
//
// A projection that is new takes its entry in the budget record, initialized
// from the defaults, in the same transaction; that is a change of the budget
// record's revision, which every projection records and is stale against, so
// the projections that stand are rendered again in the same transaction at the
// new revision and none is left stale by the creation of another.
func RenderOp(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	sc := c.Request.Scope
	switch sc.Level {
	case envelope.ScopeHive, envelope.ScopeWorkspace, envelope.ScopeProject:
	default:
		return nil, envelope.Common(envelope.CodeScopeMismatch, "a projection is of the Hive's, a workspace's or a project's scope")
	}
	key := Key{
		Scope: rule.Scope{Level: sc.Level, HomeID: sc.HomeID, WorkspaceID: sc.WorkspaceID, ProjectID: sc.ProjectID},
		Tier:  str(body["tier"]), Audience: str(body["audience"]), SubjectID: str(body["subject_id"]),
	}
	if err := key.Valid(); err != nil {
		return nil, invalid(err.Error())
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	snap, err := Load(c.Store, c.Home.HomeID)
	if err != nil {
		return nil, err
	}
	_, exists := snap.projection(key)
	budgetRevision := snap.Budget.Revision
	targets := []target{{key: key, at: str(body["at"])}}
	if !exists {
		budgetRevision++
		for _, e := range snap.Projections {
			targets = append(targets, target{key: e.Key()})
		}
	}
	b := &builder{c: c}
	projections, written, err := b.renderAll(snap, targets, budgetRevision)
	if err != nil {
		return nil, err
	}
	if !exists {
		entries := list(snap.budgetData["projections"])
		next := copyValue(snap.budgetData).(map[string]any)
		next["projections"] = append(entries, map[string]any{
			"projection_id": str(projections[0]["projection_id"]), "max_bytes": snap.Budget.Max("", key),
		})
		if _, _, err := b.put("budget", budgetID, next, snap.Budget.Revision); err != nil {
			return nil, err
		}
	}
	result := map[string]any{"projection": projections[0], "unchanged": !written[0] && len(b.tx.Records) == 0}
	if len(b.tx.Records) == 0 {
		return &cli.Result{Body: result}, nil
	}
	staged, err := poolstore.Stage(c, b.tx)
	if err != nil {
		return nil, err
	}
	return staged.Result(result), nil
}
