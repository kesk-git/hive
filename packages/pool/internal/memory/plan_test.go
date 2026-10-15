package memory_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/pool/internal/memory"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
)

// The planner renders, before a write commits, every projection the write
// affects, and refuses the whole write when any reachable rendering would exceed
// its budget (spec/pool.md, Generated knowledge). It is proved directly, over a
// snapshot made in memory.

func parseRule(t testing.TB, rec map[string]any) *rule.Rule {
	t.Helper()
	r, err := rule.ParseRule(rec)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type plan struct {
	t    testing.TB
	snap *memory.Snapshot
	s    *rule.Subject
}

// newPlan is a snapshot with one Hive subject and a budget whose default for a
// subject projection is 400 bytes.
func newPlan(t testing.TB) *plan {
	t.Helper()
	_, rec := subjectRecord(t, hiveScope, "class.3")
	s, err := rule.ParseSubject(rec)
	if err != nil {
		t.Fatal(err)
	}
	return &plan{t: t, s: s, snap: &memory.Snapshot{
		Subjects: []*rule.Subject{s},
		Budget:   memory.Budget{Revision: 2, Entries: map[string]int64{}, Defaults: memory.Defaults{HiveAlways: 1000, FleetAlways: 1000, Subject: 400}},
	}}
}

// rule makes an operating subject rule of a line of n characters.
func (p *plan) rule(scope map[string]any, n int, over R) *rule.Rule {
	over = merge(R{"rule_line": strings.Repeat("x", n)}, over)
	_, rec := ruleRecord(p.t, "subject", p.s.SubjectID, scope, over)
	return parseRule(p.t, rec)
}

func merge(a, b R) R {
	out := R{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func overBudget(t testing.TB, err error) *envelope.Failure {
	t.Helper()
	var f *envelope.Failure
	if !errors.As(err, &f) || f.Err.Code != "pool.projection_over_budget" || f.Status != envelope.Refused {
		t.Fatalf("want pool.projection_over_budget, got %v", err)
	}
	if moves, _ := f.Err.Detail["moves"].([]any); len(moves) != 3 {
		t.Errorf("moves %v", f.Err.Detail)
	}
	return f
}

// A write whose rule takes effect later is counted at its write: the projection
// fits now and not at the instant the rule becomes effective.
func TestThePlannerRefusesAWriteThatOverflowsAtAFutureBoundary(t *testing.T) {
	p := newPlan(t)
	p.snap.Rules = []*rule.Rule{p.rule(hiveScope, 100, nil)}
	if _, err := p.snap.Plan(start, memory.Write{Rules: []*rule.Rule{p.rule(hiveScope, 10, R{"effective_at": d20})}}); err != nil {
		t.Fatalf("a small later rule: %v", err)
	}
	if _, err := p.snap.Plan(start, memory.Write{Rules: []*rule.Rule{p.rule(hiveScope, 100, R{"effective_at": d20})}}); err == nil {
		t.Fatal("a later rule that overflows at its effective instant was planned")
	} else {
		f := overBudget(t, err)
		if f.Err.Detail["at"] != d20 {
			t.Errorf("detail %v", f.Err.Detail)
		}
	}
	// An expiry is a boundary too: the rule that ends at d15 leaves, and a rule
	// that begins at d15 does not overflow in its place.
	p.snap.Rules = []*rule.Rule{p.rule(hiveScope, 100, R{"expires_at": d15})}
	if _, err := p.snap.Plan(start, memory.Write{Rules: []*rule.Rule{p.rule(hiveScope, 100, R{"effective_at": d15})}}); err != nil {
		t.Fatalf("a rule that replaces an expiring one at its boundary: %v", err)
	}
}

// A write in one scope is checked in the projections of every descendant scope,
// each against its own budget.
func TestThePlannerChecksDescendantScopes(t *testing.T) {
	p := newPlan(t)
	p.snap.Rules = []*rule.Rule{p.rule(hiveScope, 100, nil)}
	existing := mint(t, ids.Projection)
	p.snap.Projections = []memory.Existing{{ID: existing, Scope: rule.Scope{Level: "workspace", HomeID: homeID, WorkspaceID: workspaceID}, Tier: "subject", Audience: "class-3", SubjectID: p.s.SubjectID}}
	p.snap.Budget.Entries[existing] = 160
	if _, err := p.snap.Plan(start, memory.Write{}); err != nil {
		t.Fatalf("what stands fits: %v", err)
	}
	// The Hive's own projection has room; the workspace's has not.
	f := overBudget(t, errOf(p.snap.Plan(start, memory.Write{Rules: []*rule.Rule{p.rule(hiveScope, 20, nil)}})))
	if f.Err.Detail["projection_id"] != existing {
		t.Errorf("detail %v", f.Err.Detail)
	}
}

// A write is checked in every role audience it applies to, and in no other.
func TestThePlannerChecksTheRoleAudiencesAWriteReaches(t *testing.T) {
	p := newPlan(t)
	p.snap.Rules = []*rule.Rule{p.rule(hiveScope, 100, nil)}
	existing := mint(t, ids.Projection)
	p.snap.Projections = []memory.Existing{{ID: existing, Scope: rule.Scope{Level: "hive", HomeID: homeID}, Tier: "subject", Audience: "reviewer", SubjectID: p.s.SubjectID}}
	p.snap.Budget.Entries[existing] = 160
	if _, err := p.snap.Plan(start, memory.Write{Rules: []*rule.Rule{p.rule(hiveScope, 20, R{"applies": []any{"class-5"}})}}); err != nil {
		t.Fatalf("a rule for another role: %v", err)
	}
	f := overBudget(t, errOf(p.snap.Plan(start, memory.Write{Rules: []*rule.Rule{p.rule(hiveScope, 20, nil)}})))
	if f.Err.Detail["projection_id"] != existing {
		t.Errorf("detail %v", f.Err.Detail)
	}
}

func errOf(_ []memory.Rendered, err error) error { return err }
