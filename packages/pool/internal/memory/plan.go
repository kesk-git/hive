package memory

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
)

// Write is what a write to the rule store, or to the budget, changes: the rules
// and subjects as they will be, added or replacing the one of their identity,
// and the entries of the budget it sets, by projection identity.
type Write struct {
	Rules    []*rule.Rule
	Subjects []*rule.Subject
	Budget   map[string]int64
}

// Rendered is one reachable rendering the planner made: of the projection of the
// key, at the instant, with the budget it was measured against.
type Rendered struct {
	// ProjectionID is the identity of the projection where one stands, and empty
	// for one that would be created.
	ProjectionID string
	Key          Key
	At           time.Time
	Bytes, Max   int64
}

// Plan renders, before a write commits, every projection the write affects
// (spec/pool.md, Generated knowledge): in every scope beneath the writes' and in
// every role audience, the projections that stand and those the rules and
// subjects would bring, as the live set stands at the clock and at each future
// instant a rule takes effect or expires at which it changes. It refuses the
// whole write with pool.projection_over_budget when any of them would exceed its
// budget, naming the first by scope, tier, audience, subject and instant. The
// budget of a projection that does not stand is the default a new entry is
// initialized from. A rendering that conflicts is measured like any other: it
// is the write that records a rule that refuses a conflict.
//
// The planner is called by every writer of the rule store with the records as
// they will be, and by the change of the budget with the entries it sets.
func (s *Snapshot) Plan(now time.Time, w Write) ([]Rendered, error) {
	after := s.after(w)
	var out []Rendered
	instants := after.instants(now)
	for _, k := range after.reachable() {
		id := ""
		if e, ok := after.projection(k); ok {
			id = e.ID
		}
		max := after.Budget.Max(id, k)
		for _, at := range instants {
			r, err := after.render(k, at, after.Budget.Revision)
			if err != nil {
				return nil, err
			}
			out = append(out, Rendered{ProjectionID: id, Key: k, At: at, Bytes: r.Bytes, Max: max})
			if r.Bytes > max {
				return nil, overBudget(id, k, at, r.Bytes, max)
			}
		}
	}
	return out, nil
}

// overBudget is the refusal of a rendering that exceeds its budget: it names
// the projection, its size and budget, and the recorder's three moves.
func overBudget(id string, k Key, at time.Time, bytes, max int64) *envelope.Failure {
	instant, _ := clock.Format(at)
	f := refuse(CodeOverBudget, fmt.Sprintf(
		"the projection %s would be %d bytes at %s, over its budget of %d; the recorder's three moves are to shorten the line to one sentence and file its mechanics as a fact of its subject in the same request, to file the rule into a new, narrower subject created in the same request with its loads_at, or to supersede a rule in force with it",
		k, bytes, instant, max))
	if id != "" {
		f = f.With("projection_id", id)
	}
	return f.With("projection", k.String()).With("bytes", bytes).With("max_bytes", max).With("at", instant).With("moves", slices.Clone(moves))
}

// after is the snapshot the write leaves: its rules and subjects in place of
// those of their identities, and its budget entries.
func (s *Snapshot) after(w Write) *Snapshot {
	out := &Snapshot{Projections: s.Projections, Budget: s.Budget, Scopes: s.Scopes, budgetData: s.budgetData}
	out.Budget.Entries = map[string]int64{}
	for id, max := range s.Budget.Entries {
		out.Budget.Entries[id] = max
	}
	for id, max := range w.Budget {
		out.Budget.Entries[id] = max
	}
	replaced := map[string]bool{}
	for _, r := range w.Rules {
		replaced[r.RuleID] = true
	}
	out.Rules = append(out.Rules, w.Rules...)
	for _, r := range s.Rules {
		if !replaced[r.RuleID] {
			out.Rules = append(out.Rules, r)
		}
	}
	replaced = map[string]bool{}
	for _, sub := range w.Subjects {
		replaced[sub.SubjectID] = true
	}
	out.Subjects = append(out.Subjects, w.Subjects...)
	for _, sub := range s.Subjects {
		if !replaced[sub.SubjectID] {
			out.Subjects = append(out.Subjects, sub)
		}
	}
	return out
}

// instants are the instants whose renderings are reachable: the clock and each
// later instant at which a rule takes effect or its expiry passes, which are the
// instants at which the live set changes. A lift is by its receipt and is no
// instant: it is checked at its own write.
func (s *Snapshot) instants(now time.Time) []time.Time {
	set := []time.Time{now}
	add := func(t time.Time) {
		if t.After(now) && !slices.ContainsFunc(set, t.Equal) {
			set = append(set, t)
		}
	}
	for _, r := range s.Rules {
		if r.Kind != rule.KindOperating || r.TaskID != "" {
			continue
		}
		if t, err := clock.Parse(r.EffectiveAt); err == nil {
			add(t)
		}
		if t, ok, err := r.EffectiveExpiry(); err == nil && ok {
			add(t)
		}
	}
	slices.SortFunc(set, func(a, b time.Time) int { return a.Compare(b) })
	return set
}

// reachable are the keys of every projection that can be rendered from the
// records, in every scope the records name or the home registers: those that
// stand, the always projections of the Hive and of each workspace, and, for each subject, a projection in every scope the subject's
// own contains and in every role audience: the subject's facts render for each
// of them whether or not a rule applies to it.
func (s *Snapshot) reachable() []Key {
	home := s.home()
	scopes := []rule.Scope{{Level: envelope.ScopeHive, HomeID: home}}
	addScope := func(sc rule.Scope) {
		if sc.Level == envelope.ScopeHive || sc.Level == "" {
			return
		}
		if sc.Level == envelope.ScopeProject {
			ws := rule.Scope{Level: envelope.ScopeWorkspace, HomeID: sc.HomeID, WorkspaceID: sc.WorkspaceID}
			if !slices.Contains(scopes, ws) {
				scopes = append(scopes, ws)
			}
		}
		if (sc.Level == envelope.ScopeWorkspace || sc.Level == envelope.ScopeProject) && !slices.Contains(scopes, sc) {
			scopes = append(scopes, sc)
		}
	}
	for _, r := range s.Rules {
		if r.Kind != rule.KindOperating || r.TaskID != "" {
			continue
		}
		addScope(r.Scope)
	}
	for _, sub := range s.Subjects {
		addScope(sub.Scope)
	}
	for _, sc := range s.Scopes {
		addScope(sc)
	}
	for _, e := range s.Projections {
		addScope(e.Scope)
	}
	seen := map[Key]bool{}
	var keys []Key
	add := func(k Key) {
		if !seen[k] && k.Valid() == nil {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for _, e := range s.Projections {
		add(e.Key())
	}
	hive := scopes[0]
	add(Key{Scope: hive, Tier: TierAlways, Audience: "hive"})
	add(Key{Scope: hive, Tier: TierAlways, Audience: "fleet"})
	for _, sc := range scopes {
		if sc.Level == envelope.ScopeWorkspace {
			add(Key{Scope: sc, Tier: TierAlways, Audience: "fleet"})
		}
	}
	for _, sub := range s.Subjects {
		for _, sc := range scopes {
			if !within(sub.Scope, sc) {
				continue
			}
			for _, a := range roles {
				add(Key{Scope: sc, Tier: TierSubject, Audience: a, SubjectID: sub.SubjectID})
			}
		}
	}
	slices.SortFunc(keys, func(a, b Key) int { return cmp.Compare(a.String(), b.String()) })
	return keys
}
