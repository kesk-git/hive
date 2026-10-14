package item

import (
	"slices"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/read"
)

// changedMembers are the members an update reports, in the order of the
// response schema.
var changedMembers = []string{"title", "body", "level", "wave", "deliverable", "class", "priority", "placement_requested", "delivery", "specification_required", "definition", "links", "deps", "schedule"}

// update applies a patch to one item and reports the fields that changed. The
// item is read and rewritten under the lock with the expected revision it was
// computed from; a patch that changes nothing writes nothing.
func update(c *cli.Call) (*cli.Result, error) {
	taskID, _ := c.Request.Body["task_id"].(string)
	patch, _ := c.Request.Body["patch"].(map[string]any)
	g, err := loadGraph(c)
	if err != nil {
		return nil, err
	}
	n := g.Items[taskID]
	if n == nil {
		return nil, notFound("item")
	}
	if err := checkScope(c, n.Data); err != nil {
		return nil, err
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	if _, set := patch["schedule"]; set && patch["clear_schedule"] == true {
		return nil, envelope.Common(envelope.CodeSchemaViolation, "a patch sets the schedule and clears it")
	}

	orig := n.Data
	next := copyMap(orig)
	level, _ := orig["level"].(string)
	_, imported := orig["legacy"]

	for _, name := range []string{"title", "body", "wave", "class", "priority", "placement_requested", "delivery", "specification_required", "definition"} {
		if v, ok := patch[name]; ok {
			next[name] = copyValue(v)
		}
	}
	if lines, ok := patch["add_body_lines"].([]any); ok {
		body, _ := next["body"].(string)
		next["body"] = addLines(body, lines)
	}
	if v, ok := patch["deliverable"].(string); ok {
		if v != "decision" && orig["deliverable"] == "decision" && !imported {
			if _, withLevel := patch["level"]; !withLevel {
				return nil, hierarchy("a decision item becomes work only in an update that sets its level")
			}
		}
		next["deliverable"] = v
	}
	if links, ok := patch["add_links"].([]any); ok {
		have, _ := next["links"].([]any)
		for _, l := range links {
			if !contains(have, l) {
				have = append(have, copyValue(l))
			}
		}
		next["links"] = have
	}
	if patch["clear_schedule"] == true {
		delete(next, "schedule")
	}
	if v, ok := patch["schedule"]; ok {
		if err := checkInstant("schedule", v); err != nil {
			return nil, err
		}
		next["schedule"] = copyValue(v)
	}

	// The level is set once, on an item that has none.
	newLevel := level
	if v, ok := patch["level"].(string); ok && v != level {
		if level != "" {
			return nil, hierarchy("the level of an item that has one is not changed")
		}
		if orig["deliverable"] == "decision" && next["deliverable"] == "decision" {
			return nil, hierarchy("a decision item carries no level")
		}
		newLevel = v
		next["level"] = v
		if v == "target" {
			if err := checkConversion(c, g, n); err != nil {
				return nil, err
			}
			for _, gone := range []string{"deliverable", "placement_requested", "delivery"} {
				delete(next, gone)
			}
		}
	}

	// The edges.
	deps, _ := next["deps"].([]any)
	before := parentsOf(orig)
	if removed, ok := patch["remove_deps"].([]any); ok {
		deps = slices.DeleteFunc(deps, func(have any) bool {
			return slices.ContainsFunc(removed, func(r any) bool { return sameEdge(have, r) })
		})
	}
	if added, ok := patch["add_deps"].([]any); ok {
		for _, e := range added {
			if !slices.ContainsFunc(deps, func(have any) bool { return sameEdge(have, e) }) {
				deps = append(deps, copyValue(e))
			}
			if m, _ := e.(map[string]any); m["type"] == read.EdgeBlockedBy {
				if _, ok := g.Items[m["task_id"].(string)]; !ok {
					return nil, refuse(codeDependencyMissing, "a blocked-by edge names an item that does not exist")
				}
			}
		}
	}
	next["deps"] = deps
	after := parentsOf(next)
	moved := !slices.Equal(before, after)

	// The parent rule holds for the item as the patch leaves it. An imported
	// item with no level is exempt until its owner sets one.
	_, levelSet := patch["level"]
	if levelSet || moved || patch["add_deps"] != nil || patch["remove_deps"] != nil {
		if f := checkParentRule(g, newLevel, newLevel == "" && imported, after); f != nil {
			return nil, f
		}
		if f := checkTargetBlockers(g, newLevel, deps); f != nil {
			return nil, f
		}
	}
	next2 := g.Replace(taskID, next)
	if moved {
		if err := checkMove(c, g, taskID); err != nil {
			return nil, err
		}
	}
	if newLevel == "hunt" && (moved || newLevel != level) {
		if f := checkAttach(g, after[0], taskID); f != nil {
			return nil, f
		}
	}
	if _, scheduled := next["schedule"]; scheduled && next2.Derived(taskID) {
		return nil, hierarchy("a schedule belongs to a Hunt or to a Prey that has no Hunt beneath it")
	}

	var changed []any
	for _, name := range changedMembers {
		if !equalMember(orig, next, name) {
			changed = append(changed, name)
		}
	}
	if len(changed) == 0 {
		return &cli.Result{Body: map[string]any{"item": orig, "changed": []any{}}}, nil
	}

	b := &builder{c: c}
	final, _, err := b.put("item", taskID, next, n.Revision)
	if err != nil {
		return nil, err
	}
	if moved {
		if err := b.recomputeDefinitionHolds(next2, taskID); err != nil {
			return nil, err
		}
	}
	return b.stage(map[string]any{"item": final, "changed": changed})
}

func equalMember(a, b map[string]any, name string) bool {
	x, okA := a[name]
	y, okB := b[name]
	if okA != okB {
		return false
	}
	return !okA || equalValues(x, y)
}

// parentsOf are the items the record's parent edges name.
func parentsOf(rec map[string]any) []string {
	var out []string
	for _, e := range read.Edges(rec) {
		if e.Type == read.EdgeParent {
			out = append(out, e.TaskID)
		}
	}
	return out
}

// sameEdge reports whether two edges are the same by their type and the item
// they name: the reason is free text and is never read.
func sameEdge(a, b any) bool {
	x, _ := a.(map[string]any)
	y, _ := b.(map[string]any)
	return x["type"] == y["type"] && x["task_id"] == y["task_id"]
}

// checkConversion refuses setting the level target on an item that has a
// generation, an Imperative, a parent edge or a schedule or is not queued.
func checkConversion(c *cli.Call, g *read.Graph, n *read.Node) error {
	_, scheduled := n.Data["schedule"]
	switch {
	case n.State != "queued":
		return hierarchy("an item becomes a Target only while it is queued")
	case read.Generations(n.Data) > 0:
		return hierarchy("an item that has a generation does not become a Target")
	case n.Parent != "":
		return hierarchy("an item that has a parent edge does not become a Target")
	case scheduled:
		return hierarchy("an item that has a schedule does not become a Target until its owner clears the schedule")
	}
	has, err := imperativeExists(c, n.ID)
	if err != nil {
		return err
	}
	if has {
		return hierarchy("an item that has an Imperative does not become a Target")
	}
	return nil
}

// checkMove refuses setting, replacing or removing the parent edge of an item
// unless it and every item beneath it is queued, has no Imperative and has
// never opened a generation: no admitted work changes definition by a move.
func checkMove(c *cli.Call, g *read.Graph, taskID string) error {
	for _, id := range append([]string{taskID}, g.Beneath(taskID)...) {
		n := g.Items[id]
		if n.State != "queued" || read.Generations(n.Data) > 0 {
			return hierarchy("a parent edge is set, replaced or removed only while the item and every item beneath it is queued and has never opened a generation")
		}
		has, err := imperativeExists(c, id)
		if err != nil {
			return err
		}
		if has {
			return hierarchy("a parent edge is set, replaced or removed only while the item and every item beneath it has no Imperative")
		}
	}
	return nil
}

// recomputeDefinitionHolds gives the moved item and each item beneath it the
// definition hold, and not the definition state, that the rule which places an
// item at creation gives it: the hold is present exactly when an accepted
// specification is required and the item has no effective specification.
func (b *builder) recomputeDefinitionHolds(g *read.Graph, taskID string) error {
	for _, id := range append([]string{taskID}, g.Beneath(taskID)...) {
		should := g.Needs(id) && g.Effective(id) == nil
		var active []*read.Hold
		for _, h := range g.Holds[id] {
			if h.Kind == "definition" && h.State == "active" {
				active = append(active, h)
			}
		}
		scope := scopeOf(g.Items[id].Data)
		switch {
		case should && len(active) == 0:
			holdID, hold, err := newHold(b.c, scope, id, "definition", definitionHoldReason, definitionReasonCode, nil)
			if err != nil {
				return err
			}
			_, ref, err := b.put("hold", holdID, hold, 0)
			if err != nil {
				return err
			}
			if _, err := b.receipt("pool.hold.placed", ref, scope, id, nil); err != nil {
				return err
			}
		case !should:
			for _, h := range active {
				if _, err := b.release(h.ID, h.Data, h.Revision, nil); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
