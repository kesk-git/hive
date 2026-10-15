package memory_test

import (
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	_ "github.com/kesk-git/hive/packages/pool/internal/imperative"
)

// The refusal of hive-imperative render for a stale subject projection names the
// rules that make it stale, so it cites rules; but render is a read-only
// operation, which writes no miss and no receipt, so the Pool cannot report it as
// one. The test holds the refusal as it is and the Pool as it was, and stands as
// the case of that path until render is declared as a writer.
func TestAStaleProjectionRefusalNamesTheRuleAndWritesNoMiss(t *testing.T) {
	h, _ := seed(t)
	d02 := "2026-10-02T00:00:00.000Z"
	item := func(level, parent string) string {
		id := mint(t, ids.Task)
		deps := []any{}
		if parent != "" {
			deps = append(deps, map[string]any{"type": "parent", "task_id": parent})
		}
		rec := map[string]any{
			"schema": "pool.item", "schema_version": "1.0", "record_revision": int64(3),
			"task_id": id, "scope_level": "project", "home_id": homeID, "workspace_id": workspaceID, "project_id": projectID,
			"title": "An item", "level": level, "state": "queued", "definition": "accepted",
			"deps": deps, "links": []any{}, "generations": []any{},
			"created_at": d01, "updated_at": d02, "writer": writer("add"),
		}
		if level != "target" {
			rec["deliverable"], rec["class"], rec["placement_requested"] = "swarm", int64(3), "mac"
			rec["delivery"] = map[string]any{"validation": "synapse", "publication": "harvest", "assimilation": "word"}
		}
		h.plant("item", id, rec)
		return id
	}
	hunt := item("hunt", item("prey", item("target", "")))

	classSubject := h.subject(hiveScope, "class.3")
	touched := h.subject(hiveScope, "widget.exports")
	h.rule("subject", classSubject, hiveScope, R{"subject": "class3.steps", "applies": []any{"class-3"}})
	touchedRule := h.rule("subject", touched, hiveScope, R{"subject": "exports.retry", "applies": []any{"class-3"}})

	spec := fixture(t, "pool.specification", "v1-2-touched-subjects-and-evidence")
	specID := mint(t, ids.Spec)
	spec["specification_id"], spec["home_id"], spec["workspace_id"], spec["project_id"] = specID, homeID, workspaceID, projectID
	revisions := spec["revisions"].([]any)
	revisions[0].(map[string]any)["touched_subjects"] = []any{}
	second := revisions[1].(map[string]any)
	second["touched_subjects"] = []any{touched}
	delete(second, "product_rules")
	delete(second, "plan_stage")
	delete(spec, "acceptances")
	h.plant("specification", specID, spec)

	h.ok(h.send("hive-memory", "render", map[string]any{"tier": "subject", "audience": "class-3", "subject_id": touched}, request{scope: projectScope}))

	imperative := fixture(t, "pool.imperative", "project-swarm-claude-mac")
	revision := imperative["revisions"].([]any)[0].(map[string]any)
	for _, k := range []string{"revision", "digest", "authored_at", "authored_by", "authority", "request_id", "supersedes_reason"} {
		delete(revision, k)
	}
	setup := revision["setup"].(map[string]any)
	delete(setup, "placement")
	delete(setup, "repository")
	revision["task"].(map[string]any)["standard"].(map[string]any)["criteria"] = map[string]any{
		"specification_id": specID, "revision": int64(2), "digest": second["digest"],
	}
	create := map[string]any{"task_id": hunt, "class": int64(3)}
	for _, k := range []string{"task", "setup", "rules", "definition_of_done"} {
		create[k] = revision[k]
	}
	h.ok(h.send("hive-imperative", "create", create, request{scope: projectScope, version: "1.0"}))
	h.ok(h.send("hive-imperative", "render", map[string]any{"task_id": hunt}, request{scope: projectScope, version: "1.1"}))

	// A rule now in force that the projection was not rendered from makes it stale.
	h.amend(touchedRule, R{"rule_line": "Exports retry at most five times."})
	caller, _ := sessionActor(t)
	h.unchanged(func() {
		resp := h.sendAs(caller, "hive-imperative", "render", map[string]any{"task_id": hunt}, request{scope: projectScope, version: "1.1"})
		if resp.Status != envelope.Refused || resp.Error == nil || resp.Error.Code != "pool.projection_stale" || len(resp.Receipts) != 0 {
			t.Fatalf("status %s, error %+v, receipts %v", resp.Status, resp.Error, resp.Receipts)
		}
		named := false
		for _, v := range resp.Error.Detail["stale_sources"].([]any) {
			if m := v.(map[string]any); m["schema"] == "pool.rule" && m["id"] == touchedRule {
				named = true
			}
		}
		if !named {
			t.Errorf("the refusal does not name the rule: %v", resp.Error.Detail)
		}
	})
	if len(h.misses()) != 0 {
		t.Errorf("a read-only render wrote misses: %v", h.misses())
	}
}
