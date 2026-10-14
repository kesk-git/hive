package specification

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The operation define of hive-spec (spec/pool.md, Specifications). It writes
// revision 1 of a new specification or the next revision of an existing one,
// whole, and decides invalidation binding by binding: every item bound to the
// specification, at whichever revision, is compared by criteria identity, text
// and evidence and by the subjects added or removed with the revision it is
// bound to, and where they differ its acceptance is marked invalidated, it
// loses its specification, returns to defining and receives a definition hold.
// Every dispatchable item that has not landed and resolves its effective
// specification through it follows, with a hold, and an item with a binding of
// its own is untouched. Nothing else of those items changes, and a generation
// already open keeps the Imperative it was delivered with.

// Define writes a revision of a specification.
func Define(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	subjects := map[string]bool{}
	for _, v := range listOf(body["touched_subjects"]) {
		s, _ := v.(string)
		if subjects[s] {
			return nil, invalid("touched_subjects names a subject twice")
		}
		subjects[s] = true
	}
	specID, revising := body["specification_id"].(string)
	title, _ := body["title"].(string)
	if !revising && title == "" {
		return nil, invalid("a new specification carries its title")
	}

	g, err := loadGraph(c)
	if err != nil {
		return nil, err
	}
	var cur *poolstore.Loaded
	if revising {
		if cur, err = loadSpecification(c, specID); err != nil {
			return nil, err
		}
		if err := checkScope(c, cur.Data); err != nil {
			return nil, err
		}
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	var named []string
	for _, v := range listOf(body["task_ids"]) {
		id, _ := v.(string)
		n := g.Items[id]
		if n == nil {
			return nil, notFound("item")
		}
		if err := checkScope(c, n.Data); err != nil {
			return nil, err
		}
		if !contains(named, id) {
			named = append(named, id)
		}
	}

	now, err := c.Stamp()
	if err != nil {
		return nil, internal("a revision cannot be given a time", err)
	}
	number := int64(1)
	var previous map[string]any
	if revising {
		current, _ := cur.Data["current_revision"].(int64)
		number, previous = current+1, revisionNamed(cur.Data, current)
	}
	rev := map[string]any{
		"revision": number, "text": body["text"], "criteria": copyValue(body["criteria"]),
		"defined_at": now, "defined_by": callerTree(c), "authority": authorityTree(c), "request_id": c.Request.RequestID,
		"touched_subjects": []any{},
	}
	if v, ok := body["touched_subjects"]; ok {
		rev["touched_subjects"] = copyValue(v)
	}
	for _, name := range []string{"product_rules", "plan_stage"} {
		if v, ok := body[name]; ok {
			rev[name] = copyValue(v)
		}
	}
	if previous != nil {
		rev["changed_criteria"] = changedCriteria(previous, rev)
	}
	digest, err := revisionDigest(rev)
	if err != nil {
		return nil, internal("a revision cannot be digested", err)
	}
	rev["digest"] = digest

	// The items the revision changes, each written once.
	edits := map[string]map[string]any{}
	var written []string
	edit := func(id string) map[string]any {
		if m, ok := edits[id]; ok {
			return m
		}
		m := copyMap(g.Items[id].Data)
		edits[id] = m
		written = append(written, id)
		return m
	}
	definition := func(id string) any {
		if m, ok := edits[id]; ok {
			return m["definition"]
		}
		return g.Items[id].Data["definition"]
	}
	holds := map[string]bool{}
	var holdOrder []string
	needHold := func(id string) {
		if !holds[id] {
			holds[id] = true
			holdOrder = append(holdOrder, id)
		}
	}

	var invalidated []string
	survives := map[string]bool{}
	var acceptances []any
	if revising {
		acceptances = listOf(copyValue(cur.Data["acceptances"]))
		for _, id := range g.Order {
			bound, revision, _, ok := bindingOf(g.Items[id].Data)
			if !ok || bound != specID {
				continue
			}
			old := revisionNamed(cur.Data, revision)
			if old == nil {
				return nil, envelope.Common(envelope.CodeRecordMalformed, "an item is bound to a revision the specification does not hold").With("record", id)
			}
			if sameCriteriaAndSubjects(old, rev) {
				survives[id] = true
				continue
			}
			invalidated = append(invalidated, id)
			markInvalidated(acceptances, id, revision, number, now)
			m := edit(id)
			delete(m, "specification")
			m["definition"] = "defining"
			needHold(id)
		}
		for _, id := range invalidated {
			for _, r := range resolvers(g, id) {
				if dispatchable(g, r) {
					if definition(r) != "defining" {
						edit(r)["definition"] = "defining"
					}
					needHold(r)
				}
			}
		}
	}
	for _, id := range named {
		if !survives[id] && definition(id) != "defining" {
			edit(id)["definition"] = "defining"
		}
	}

	b := &builder{c: c}
	refs := map[string]*envelope.RecordRef{}
	for _, id := range written {
		_, ref, err := b.put("item", id, edits[id], g.Items[id].Revision)
		if err != nil {
			return nil, err
		}
		refs[id] = ref
	}
	var rec map[string]any
	var expected int64
	scope := c.Request.Scope
	scope.TaskID, scope.Generation = "", 0
	if revising {
		rec, expected = copyMap(cur.Data), cur.Revision
		rec["revisions"] = append(listOf(rec["revisions"]), rev)
		rec["current_revision"] = number
		if acceptances != nil {
			rec["acceptances"] = acceptances
		}
		scope = scopeOf(rec)
	} else {
		specID, err = ids.Mint(c.Clock, ids.Spec)
		if err != nil {
			return nil, internal("a specification cannot be given an identity", err)
		}
		rec = map[string]any{
			"schema": "pool.specification", "schema_version": schemaVersion, "record_revision": int64(1),
			"specification_id": specID, "title": title, "current_revision": number,
			"revisions": []any{rev}, "closures": []any{}, "created_at": now, "updated_at": now,
		}
		putScope(rec, scope, false)
	}
	final, specRef, err := b.put("specification", specID, rec, expected)
	if err != nil {
		return nil, err
	}
	if err := b.receipt("pool.specification.defined", specRef, scope, "", nil); err != nil {
		return nil, err
	}
	for _, id := range invalidated {
		if err := b.receipt("pool.specification.invalidated", refs[id], scopeOf(g.Items[id].Data), id, []envelope.Evidence{{Role: "revision", Ref: *specRef}}); err != nil {
			return nil, err
		}
	}
	for _, id := range holdOrder {
		if len(activeDefinitionHolds(g, id)) > 0 {
			continue
		}
		if err := b.placeHold(id, scopeOf(g.Items[id].Data)); err != nil {
			return nil, err
		}
	}
	out := make([]any, len(invalidated))
	for i, id := range invalidated {
		out[i] = id
	}
	return b.stage(map[string]any{"specification": final, "invalidated": out})
}

// markInvalidated marks the acceptance of the revision the item is bound to as
// invalidated by the revision that now differs from it: the latest entry of
// that item at that revision that is still accepted.
func markInvalidated(acceptances []any, taskID string, bound, by int64, at string) {
	for i := len(acceptances) - 1; i >= 0; i-- {
		a := mapOf(acceptances[i])
		if a["task_id"] == taskID && a["revision"] == bound && a["state"] == "accepted" {
			a["state"], a["invalidated_by"], a["invalidated_at"] = "invalidated", by, at
			return
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
