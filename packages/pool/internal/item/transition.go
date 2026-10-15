package item

import (
	"fmt"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/read"
)

// transition moves one item between queued, in-flight, done and archived under
// the table of spec/pool.md, State. Every refusal names the code the table
// gives, and a refusal leaves the Pool as it was.
func transition(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	taskID, _ := body["task_id"].(string)
	to, _ := body["to"].(string)
	if at, ok := body["at"].(string); ok && !clock.Valid(at) {
		return nil, envelope.Common(envelope.CodeSchemaViolation, "at is not a calendar instant")
	}
	if to == "done" {
		links, _ := body["evidence"].([]any)
		for i, value := range links {
			link, _ := value.(map[string]any)
			url, _ := link["url"].(string)
			if link["kind"] == "harvest" && !IsPRURL(url) {
				return nil, envelope.Common(envelope.CodeSchemaViolation, fmt.Sprintf("evidence[%d].url must be %s", i, PRURLExpected))
			}
		}
	}
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
	from := n.State
	if from == "archived" || from == to {
		return nil, refuse(codeStateTransition, "the transition is not in the table of states")
	}

	now, err := c.Stamp()
	if err != nil {
		return nil, internal("a transition cannot be given a time", err)
	}
	next := copyMap(n.Data)
	next["state"] = to
	links, _ := body["evidence"].([]any)
	switch {
	case to == "done":
		closed := now
		if at, ok := body["at"].(string); ok {
			closed = at
		}
		next["completed"], next["closed_at"] = true, closed
		delete(next, "closed_at_unknown")
		have, _ := next["links"].([]any)
		for _, l := range links {
			if !contains(have, l) {
				have = append(have, copyValue(l))
			}
		}
		next["links"] = have
	case from == "done" && to != "archived":
		// A reopen clears the marker and the time; archiving keeps both.
		delete(next, "completed")
		delete(next, "closed_at")
		delete(next, "closed_at_unknown")
	}
	if note, ok := body["note"].(string); ok {
		text, _ := next["body"].(string)
		next["body"] = appendLine(text, note)
	}

	if f := guardTransition(c, g.Replace(taskID, next), taskID, to); f != nil {
		return nil, f
	}

	b := &builder{c: c}
	final, ref, err := b.put("item", taskID, next, n.Revision)
	if err != nil {
		return nil, err
	}
	if _, err := b.receipt("pool.item.transitioned", ref, scopeOf(final), taskID, evidenceOf(links)); err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"item": final, "from": from})
}

// guardTransition applies the guards of the state entered, in the order of the
// table.
func guardTransition(c *cli.Call, g *read.Graph, id, to string) *envelope.Failure {
	n := g.Items[id]
	generationOpen := func() *envelope.Failure {
		if read.OpenGeneration(n.Data) {
			return envelope.Fail(envelope.Conflict, codeGenerationOpen, "a generation of the item is open")
		}
		return nil
	}
	switch to {
	case "in-flight":
		f := g.Facts(n.ID)
		if f.Held {
			return envelope.Hold(holdRef(c, f.Holds[0]), "an active hold stands on the item or on an ancestor")
		}
		if f.Blocked {
			return refuse(codeBlocked, "a blocked-by edge of the item or of an ancestor names an item that has not landed").With("blockers", toAny(f.Blockers))
		}
		if g.Needs(n.ID) && g.Effective(n.ID) == nil {
			return refuse(codeSpecificationUnaccepted, "the item needs an accepted specification and has no effective specification")
		}
		if n.Data["deliverable"] == "decision" {
			return refuse(codeNotDispatchable, "a decision item is never dispatched")
		}
	case "queued":
		return generationOpen()
	case "done":
		if f := generationOpen(); f != nil {
			return f
		}
		if g.Derived(n.ID) {
			return refuse(codeDerivedState, "the landing of a Target, and of a Prey that has a Hunt beneath it, is derived from the items beneath it")
		}
	case "archived":
		if !g.Landed(n.ID) {
			for _, d := range g.Dependents(n.ID) {
				if !g.Landed(d) && g.Items[d].State != "archived" {
					return refuse(codeDependencyActive, "an item that has not landed is named by the blocked-by edge of a dependent that has not landed and is not archived")
				}
			}
		}
		return generationOpen()
	}
	return nil
}

func toAny(ids []string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}
