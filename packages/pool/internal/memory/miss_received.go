package memory

import (
	"time"

	cstore "github.com/kesk-git/hive/contract/store"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// receivedRule says whether the actor holds evidence of having been given the
// rule at the revision no later than the incident (spec/pool.md, Generated
// knowledge, Misses). Evidence is of two kinds, and both are read whole:
//
//   - a pool.projection.received step in the receipt history of any projection,
//     which names a rendering whose rule inventory carries the rule at exactly
//     the revision, and whose time is no later than the incident. The history
//     keeps every step, even of a consumer that has since advanced, so what the
//     actor actually received is read and not what it holds now;
//   - for a task generation, a delivery of the item's Imperative to that
//     generation, whose rules name the rule at exactly the revision, delivered no
//     later than the incident. The delivery is read from the imperative record
//     directly: this package is imported by imperative and imports nothing of it.
//
// A receipt of another revision, or a later delivery, is no evidence: it does not
// erase the earlier miss.
func receivedRule(st *cstore.Store, actor map[string]any, ruleID string, revision int64, observed time.Time) (bool, error) {
	projections, omitted, err := poolstore.LoadAll(st, kindProj)
	if err != nil {
		return false, poolstore.Failure("the projections", err)
	}
	if len(omitted) > 0 {
		return false, malformed("a projection cannot be read, so what the actor received cannot be known", omitted[0].ID, nil)
	}
	for _, p := range projections {
		for _, v := range list(p.Data["receipt_history"]) {
			entry := tree(v)
			if !sameTree(entry["consumer"], actor) {
				continue
			}
			step := tree(entry["received"])
			if step == nil || !notAfter(str(step["at"]), observed) {
				continue
			}
			if r, ok := renderingOf(p.Data, num(step["rendering"]), str(step["digest"])); ok && carries(r, ruleID, revision) {
				return true, nil
			}
		}
	}
	if str(actor["kind"]) != "generation" {
		return false, nil
	}
	taskID, generation := str(actor["task_id"]), num(actor["generation"])
	l, om, err := poolstore.Load(st, "imperative", taskID)
	switch {
	case isNotFound(err):
		return false, nil
	case err != nil:
		return false, poolstore.Failure("the Imperative", err)
	case om != nil:
		return false, malformed("an Imperative cannot be read, so what its generation was given cannot be known", om.ID, nil)
	}
	for _, v := range list(l.Data["deliveries"]) {
		d := tree(v)
		if num(d["generation"]) != generation || !notAfter(str(d["delivered_at"]), observed) {
			continue
		}
		for _, r := range list(d["rules"]) {
			if e := tree(r); str(e["rule_id"]) == ruleID && num(e["record_revision"]) == revision {
				return true, nil
			}
		}
	}
	return false, nil
}

// renderingOf finds the rendering of the projection numbered and digested as a
// step names it, current or earlier.
func renderingOf(projection map[string]any, number int64, digest string) (Rendering, bool) {
	candidates := append([]any{projection["current"]}, list(projection["history"])...)
	for _, v := range candidates {
		if r, err := parseRendering(v); err == nil && r.Number == number && r.Digest == digest {
			return r, true
		}
	}
	return Rendering{}, false
}

// carries says whether the rendering's rule inventory names the rule at exactly
// the revision.
func carries(r Rendering, ruleID string, revision int64) bool {
	for _, e := range r.Rules {
		if e.RuleID == ruleID && e.Revision == revision {
			return true
		}
	}
	return false
}

// notAfter says whether the instant given is a calendar instant no later than
// the incident. A time that cannot be read is no evidence.
func notAfter(at string, incident time.Time) bool {
	t, err := parseInstant(at)
	return err == nil && !t.After(incident)
}
