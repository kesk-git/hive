package memory

import (
	"time"

	"github.com/kesk-git/hive/packages/pool/internal/read"
)

// The states a projection's freshness is computed as.
const (
	StateFresh = "fresh"
	StateStale = "stale"
)

// Freshness is a projection's freshness at an instant, computed and never
// stored (spec/pool.md, Generated knowledge, check).
type Freshness struct {
	State string
	// Stale are the sources whose revision differs from the one rendered, the
	// rules now in force that the rendering did not read, and the rules it read
	// that are no longer in force, at the instant.
	Stale []Source
}

// Fresh computes the freshness of the projection at the instant: it resolves
// the rules for the projection's scope and audience as render did and finds the
// projection fresh only when they resolve to the rendering's rules digest with
// no conflict and every non-rule source revision and the budget revision agree.
// The comparison names the rules digest and never the digest of the text, so an
// unchanged text of changed rules is stale; a changed fact or corrected
// provenance, which is a changed revision of its subject, makes it stale with
// the rules digest unchanged.
func (s *Snapshot) Fresh(e Existing, now time.Time) (Freshness, error) {
	res, err := s.resolve(e.Scope, e.Audience, now)
	if err != nil {
		return Freshness{}, err
	}
	rendered := map[string]int64{}
	for _, src := range e.Current.Sources {
		if src.Schema == schemaRule {
			rendered[src.ID] = src.Revision
		}
	}
	var stale []Source
	inForce := map[string]bool{}
	for _, r := range res.Rules {
		inForce[r.RuleID] = true
		if rev, ok := rendered[r.RuleID]; !ok || rev != r.RecordRevision {
			stale = append(stale, Source{Schema: schemaRule, ID: r.RuleID, Revision: r.RecordRevision})
		}
	}
	for _, src := range e.Current.Sources {
		if src.Schema == schemaRule && !inForce[src.ID] {
			stale = append(stale, src)
		}
	}
	for _, src := range e.Current.Sources {
		if src.Schema != schemaSubject {
			continue
		}
		if sub := s.subject(src.ID); sub == nil || sub.RecordRevision != src.Revision {
			if sub != nil {
				src.Revision = sub.RecordRevision
			}
			stale = append(stale, src)
		}
	}
	budgetStale := e.BudgetRevision != s.Budget.Revision
	for _, src := range e.Current.Sources {
		if src.Schema == schemaBudget && src.Revision != s.Budget.Revision {
			budgetStale = true
		}
	}
	if budgetStale {
		stale = append(stale, Source{Schema: schemaBudget, ID: budgetID, Revision: s.Budget.Revision})
	}
	f := Freshness{State: StateFresh, Stale: stale}
	if len(res.Conflicts) > 0 || res.Digest != e.Current.RulesDigest || len(stale) > 0 {
		f.State = StateStale
	}
	return f, nil
}

// staleTrees are the stale sources as the responses carry them.
func staleTrees(homeID string, stale []Source) []any {
	out := make([]any, len(stale))
	for i, s := range stale {
		out[i] = s.ref(homeID)
	}
	return out
}

// Derive is the freshness of the projections hive-pool read returns, computed
// at the clock from the rules in force.
func Derive(c *read.Context, projections []map[string]any) ([]map[string]any, error) {
	snap, err := loadSources(c.Store, c.Call.Home.HomeID)
	if err != nil {
		return nil, err
	}
	now := c.Call.Clock.Now()
	out := make([]map[string]any, len(projections))
	for i, data := range projections {
		e, err := ParseProjection(data)
		if err != nil {
			return nil, err
		}
		f, err := snap.Fresh(e, now)
		if err != nil {
			return nil, err
		}
		out[i] = map[string]any{"projection_id": e.ID, "state": f.State, "stale_sources": staleTrees(c.Call.Home.HomeID, f.Stale)}
	}
	return out, nil
}
