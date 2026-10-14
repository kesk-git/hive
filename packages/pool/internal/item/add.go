package item

import (
	"slices"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/pool/internal/read"
)

// add adds one item (spec/pool.md, What an item carries). The Pool mints the
// task_id, writes the item and the pool.item.added receipt, refuses a blocked-by
// edge to an item that does not exist and an item that breaks the parent rule,
// and gives the item the definition the specification rule places it at, with
// the definition hold where an accepted specification is required and none is
// there.
func add(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	g, err := loadGraph(c)
	if err != nil {
		return nil, err
	}
	id, err := ids.Mint(c.Clock, ids.Task)
	if err != nil {
		return nil, internal("an item cannot be given an identity", err)
	}
	now, err := c.Stamp()
	if err != nil {
		return nil, internal("an item cannot be given a time", err)
	}
	level, _ := body["level"].(string)
	rec := map[string]any{
		"schema": "pool.item", "schema_version": versionOne, "record_revision": int64(1),
		"task_id": id, "title": body["title"], "state": "queued",
		"links": []any{}, "generations": []any{},
		"created_at": now, "updated_at": now, "writer": writerTree(c),
	}
	scope := c.Request.Scope
	scope.TaskID, scope.Generation = "", 0
	putScope(rec, scope, true)
	for _, name := range []string{"body", "level", "wave", "deliverable", "class", "priority", "placement_requested", "delivery", "specification_required", "schedule"} {
		if v, ok := body[name]; ok {
			rec[name] = copyValue(v)
		}
	}
	if level != "target" {
		if _, ok := rec["placement_requested"]; !ok {
			rec["placement_requested"] = "policy"
		}
	}
	rec["deps"] = []any{}
	for _, v := range listOf(body["deps"]) {
		if !contains(rec["deps"].([]any), v) {
			rec["deps"] = append(rec["deps"].([]any), copyValue(v))
		}
	}
	if v, ok := body["links"]; ok {
		for _, l := range listOf(v) {
			if !contains(rec["links"].([]any), l) {
				rec["links"] = append(rec["links"].([]any), copyValue(l))
			}
		}
	}

	var parents []string
	for _, e := range read.Edges(rec) {
		switch e.Type {
		case read.EdgeBlockedBy:
			if _, ok := g.Items[e.TaskID]; !ok {
				return nil, refuse(codeDependencyMissing, "a blocked-by edge names an item that does not exist")
			}
		case read.EdgeParent:
			parents = append(parents, e.TaskID)
		}
	}
	if f := checkParentRule(g, level, false, parents); f != nil {
		return nil, f
	}
	if level == "hunt" {
		if f := checkAttach(g, parents[0], ""); f != nil {
			return nil, f
		}
	}

	// The definition the item is placed at: where an accepted specification
	// is required, accepted if the item resolves one through its parent and
	// needed if it has none; otherwise as the request gave it.
	parent := ""
	if len(parents) > 0 {
		parent = parents[0]
	}
	needs, _ := rec["specification_required"].(bool)
	effective := false
	if parent != "" {
		needs = needs || g.Needs(parent)
		effective = g.Effective(parent) != nil
	}
	given, hasGiven := body["definition"].(string)
	definition := "not-required"
	switch {
	case needs && effective:
		definition = "accepted"
	case needs && hasGiven && given == "defining":
		definition = "defining"
	case needs:
		definition = "needed"
	case hasGiven && given == "accepted":
		return nil, envelope.Common(envelope.CodeSchemaViolation, "the definition accepted is set by hive-spec accept alone, and this item resolves no accepted specification")
	case hasGiven:
		definition = given
	}
	rec["definition"] = definition

	b := &builder{c: c}
	final, ref, err := b.put("item", id, rec, 0)
	if err != nil {
		return nil, err
	}
	itemScope := scopeOf(final)
	if _, err := b.receipt("pool.item.added", ref, itemScope, id, nil); err != nil {
		return nil, err
	}
	if needs && !effective {
		holdID, hold, err := newHold(c, itemScope, id, "definition", definitionHoldReason, definitionReasonCode, nil)
		if err != nil {
			return nil, err
		}
		_, holdRef, err := b.put("hold", holdID, hold, 0)
		if err != nil {
			return nil, err
		}
		if _, err := b.receipt("pool.hold.placed", holdRef, itemScope, id, nil); err != nil {
			return nil, err
		}
	}
	return b.stage(map[string]any{"item": final})
}

func listOf(v any) []any {
	l, _ := v.([]any)
	return l
}

// checkParentRule holds an item as a request leaves it to the parent rule: a
// Hunt has exactly one parent edge, to a Prey; a Prey at most one, to a Target;
// a Target none. An item with no level is a decision item, which has none,
// unless it was imported, which exempts it until its level is set.
func checkParentRule(g *read.Graph, level string, exempt bool, parents []string) *envelope.Failure {
	if exempt {
		return nil
	}
	if len(parents) > 1 {
		return hierarchy("an item has one parent edge at most")
	}
	kind := func(id string) string {
		if n := g.Items[id]; n != nil {
			return n.Level
		}
		return ""
	}
	switch level {
	case "hunt":
		if len(parents) != 1 || kind(parents[0]) != "prey" {
			return hierarchy("a Hunt has exactly one parent edge, to a Prey")
		}
	case "prey":
		if len(parents) == 1 && kind(parents[0]) != "target" {
			return hierarchy("a Prey has at most one parent edge, to a Target")
		}
	case "target":
		if len(parents) > 0 {
			return hierarchy("a Target has no parent edge")
		}
	default:
		if len(parents) > 0 {
			return hierarchy("a decision item has no parent edge")
		}
	}
	return nil
}

// checkAttach refuses attaching a Hunt to a Prey that carries the completion
// marker, is in flight or has an open generation, and attaching a first Hunt to
// a Prey that carries a schedule. self is the Hunt being attached, where it
// already exists.
func checkAttach(g *read.Graph, prey, self string) *envelope.Failure {
	n := g.Items[prey]
	if n == nil {
		return hierarchy("a Hunt is attached to a Prey")
	}
	switch {
	case n.Completed:
		return hierarchy("a Hunt cannot be attached to a Prey that carries the completion marker")
	case n.State == "in-flight":
		return hierarchy("a Hunt cannot be attached to a Prey that is in flight")
	case read.OpenGeneration(n.Data):
		return hierarchy("a Hunt cannot be attached to a Prey that has an open generation")
	}
	if _, scheduled := n.Data["schedule"]; scheduled {
		hunts := slices.DeleteFunc(slices.Clone(g.Below(prey)), func(id string) bool { return id == self })
		if len(hunts) == 0 {
			return hierarchy("a first Hunt cannot be attached to a Prey that carries a schedule")
		}
	}
	return nil
}
