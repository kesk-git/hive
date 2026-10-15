package rule

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
)

// The reasons a rule in scope is left out, as the response schema names them.
const (
	ReasonExpired    = "expired-by-clock"
	ReasonNotYet     = "not-yet-effective"
	ReasonLifted     = "lifted"
	ReasonSuperseded = "superseded"
	ReasonEnded      = "ended"
	ReasonOverridden = "overridden"
	ReasonRole       = "role"
)

// The layers a rule is collected into, from the widest scope to the narrowest.
const (
	layerHive = iota
	layerWorkspace
	layerProject
	layerTask
)

// Request is what a resolution is asked for: the scope of the envelope, a
// role and an instant, and the task generation where the request names one.
type Request struct {
	Scope      envelope.Scope
	Role       string
	At         time.Time
	TaskID     string
	Generation int64
}

// Resolved is one rule as the resolver applies it.
type Resolved struct {
	RuleID         string
	RecordRevision int64
	ScopeLevel     string
	Subject        string
	Line           string
	EffectiveAt    string
	ExpiresAt      string
	Temporary      bool
	Tier           string
	FiledUnder     string
}

// Conflict is a subject two rules govern at one scope, neither naming the
// other in changes.
type Conflict struct {
	Subject    string
	ScopeLevel string
	Rules      []string
}

// Excluded is a rule in scope that was not applied.
type Excluded struct{ RuleID, Reason string }

// Resolution is what resolving gives: the rules in the order they apply, the
// conflicts, what was left out and why, and the resolved-rule digest, empty
// when there is a conflict.
type Resolution struct {
	Rules     []Resolved
	Conflicts []Conflict
	Excluded  []Excluded
	Digest    string
}

// placed is a rule in the resolution, with the layer it was collected into.
type placed struct {
	rule  *Rule
	layer int
}

func (e placed) before(o placed) int {
	return cmp.Or(cmp.Compare(e.layer, o.layer), cmp.Compare(e.rule.EffectiveAt, o.rule.EffectiveAt), cmp.Compare(e.rule.RuleID, o.rule.RuleID))
}

// layerOf is the layer the rule is collected into for the request, and false
// for a rule that is not of its scope, which is neither applied nor excluded.
// An operating rule is collected at the Hive, at the workspace and at the
// project the scope names, and a rule narrowed to a task generation only
// where the request names that task and generation and the rule is of the
// request's own scope.
func layerOf(req Request, r *Rule) (int, bool) {
	if r.Kind != KindOperating {
		return 0, false
	}
	s, want := r.Scope, req.Scope
	if r.TaskID != "" {
		own := s.Level == want.Level && s.WorkspaceID == want.WorkspaceID && s.ProjectID == want.ProjectID
		return layerTask, own && req.TaskID != "" && req.Generation != 0 && r.TaskID == req.TaskID && r.Generation == req.Generation
	}
	switch s.Level {
	case envelope.ScopeHive:
		return layerHive, true
	case envelope.ScopeWorkspace:
		return layerWorkspace, want.Level != envelope.ScopeHive && s.WorkspaceID == want.WorkspaceID
	case envelope.ScopeProject:
		return layerProject, want.Level == envelope.ScopeProject && s.WorkspaceID == want.WorkspaceID && s.ProjectID == want.ProjectID
	}
	return 0, false
}

// exclusion is the reason the rule is not in the live set at the instant, or
// "". The reasons are tried in the order the contract lists the drops: the
// role, the effective time, the lift, the end and the expiry.
func exclusion(req Request, r *Rule) (string, error) {
	if len(r.Applies) > 0 && !slices.Contains(r.Applies, req.Role) {
		return ReasonRole, nil
	}
	effective, err := clock.Parse(r.EffectiveAt)
	if err != nil {
		return "", fmt.Errorf("rule %s: effective_at: %w", r.RuleID, err)
	}
	switch {
	case req.At.Before(effective):
		return ReasonNotYet, nil
	case r.Lifted():
		return ReasonLifted, nil
	case r.Status == StatusEnded:
		return ReasonEnded, nil
	}
	past, err := r.ExpiredAt(req.At)
	if err != nil {
		return "", fmt.Errorf("rule %s: expiry: %w", r.RuleID, err)
	}
	if past {
		return ReasonExpired, nil
	}
	return "", nil
}

// Compute computes the rules effective for the request among the rules given
// (spec/pool.md, Rules, resolve). It reads nothing but the rules and decides
// nothing: a conflict is reported and the action that depends on it is the
// caller's to hold. A time of a rule that is not a calendar instant is an
// error, since the rule cannot be placed in time.
func Compute(req Request, rules []*Rule) (*Resolution, error) {
	out := &Resolution{Rules: []Resolved{}, Conflicts: []Conflict{}, Excluded: []Excluded{}}
	var excluded []placed
	reasons := map[string]string{}
	drop := func(e placed, reason string) {
		excluded = append(excluded, e)
		reasons[e.rule.RuleID] = reason
	}

	// The live set: collected, applicable to the role, begun, not lifted, not
	// ended, not past its expiry time by the clock.
	var live []placed
	for _, r := range rules {
		layer, ok := layerOf(req, r)
		if !ok {
			continue
		}
		reason, err := exclusion(req, r)
		if err != nil {
			return nil, err
		}
		if reason != "" {
			drop(placed{r, layer}, reason)
			continue
		}
		live = append(live, placed{r, layer})
	}
	byID := map[string]placed{}
	for _, e := range live {
		byID[e.rule.RuleID] = e
	}

	// A rule of the live set that a rule of the live set names with supersedes
	// is retired; a qualifying rule keeps the rule it names beside it
	// whatever the scopes, against a narrower rule.
	superseded, qualified := map[string]bool{}, map[string]bool{}
	for _, e := range live {
		c := e.rule.Changes
		if c == nil || c.RuleID == e.rule.RuleID {
			continue
		}
		target, ok := byID[c.RuleID]
		switch {
		case !ok:
		case c.Relation == RelationSupersedes:
			superseded[c.RuleID] = true
		case c.Relation == RelationQualifies && e.layer > target.layer:
			qualified[c.RuleID] = true
		}
	}
	var standing []placed
	for _, e := range live {
		if superseded[e.rule.RuleID] {
			drop(e, ReasonSuperseded)
			continue
		}
		standing = append(standing, e)
	}

	// The narrower scope governs a subject the wider also governs.
	narrowest := map[string]int{}
	for _, e := range standing {
		narrowest[e.rule.Subject] = max(narrowest[e.rule.Subject], e.layer)
	}
	var applied []placed
	for _, e := range standing {
		if e.layer < narrowest[e.rule.Subject] && !qualified[e.rule.RuleID] {
			drop(e, ReasonOverridden)
			continue
		}
		applied = append(applied, e)
	}
	slices.SortFunc(applied, placed.before)
	slices.SortFunc(excluded, placed.before)

	for _, e := range excluded {
		out.Excluded = append(out.Excluded, Excluded{RuleID: e.rule.RuleID, Reason: reasons[e.rule.RuleID]})
	}
	for _, e := range applied {
		r, err := resolved(e.rule)
		if err != nil {
			return nil, err
		}
		out.Rules = append(out.Rules, r)
	}
	out.Conflicts = conflicts(applied)
	if len(out.Conflicts) == 0 {
		digest, err := Digest(out.Rules)
		if err != nil {
			return nil, err
		}
		out.Digest = digest
	}
	return out, nil
}

// conflicts finds, among the rules that apply, the subjects that two rules
// govern at one scope without either naming the other in changes. A conflict
// names every rule of the subject at the scope that has a partner it is
// unrelated to.
func conflicts(applied []placed) []Conflict {
	type key struct {
		subject string
		layer   int
	}
	groups := map[key][]placed{}
	var order []key
	for _, e := range applied {
		k := key{e.rule.Subject, e.layer}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], e)
	}
	related := func(a, b *Rule) bool {
		return a.Changes != nil && a.Changes.RuleID == b.RuleID || b.Changes != nil && b.Changes.RuleID == a.RuleID
	}
	out := []Conflict{}
	for _, k := range order {
		group := groups[k]
		var involved []string
		for i, a := range group {
			for j, b := range group {
				if i != j && !related(a.rule, b.rule) {
					involved = append(involved, a.rule.RuleID)
					break
				}
			}
		}
		if len(involved) > 0 {
			slices.Sort(involved)
			out = append(out, Conflict{Subject: k.subject, ScopeLevel: group[0].rule.Scope.Level, Rules: involved})
		}
	}
	return out
}

// SourceSuffix is the source the resolver appends to a rule line: the rule
// identity and the record revision, in brackets.
func SourceSuffix(r *Rule) string {
	return " [" + r.RuleID + "@" + strconv.FormatInt(r.RecordRevision, 10) + "]"
}

func resolved(r *Rule) (Resolved, error) {
	out := Resolved{
		RuleID: r.RuleID, RecordRevision: r.RecordRevision, ScopeLevel: r.Scope.Level, Subject: r.Subject,
		Line: r.RuleLine + SourceSuffix(r), EffectiveAt: r.EffectiveAt, Tier: r.Tier, FiledUnder: r.FiledUnder,
	}
	expiry, ok, err := r.EffectiveExpiry()
	if err != nil {
		return out, fmt.Errorf("rule %s: expiry: %w", r.RuleID, err)
	}
	if ok {
		if out.ExpiresAt, err = clock.Format(expiry); err != nil {
			return out, fmt.Errorf("rule %s: expiry: %w", r.RuleID, err)
		}
	}
	out.Temporary = ok || r.Lift != nil
	return out, nil
}

// Tree is the rule as the response carries it.
func (r Resolved) Tree() map[string]any {
	m := map[string]any{
		"rule_id": r.RuleID, "record_revision": r.RecordRevision, "scope_level": r.ScopeLevel, "subject": r.Subject,
		"line": r.Line, "effective_at": r.EffectiveAt, "temporary": r.Temporary, "tier": r.Tier, "filed_under": r.FiledUnder,
	}
	put(m, "expires_at", r.ExpiresAt)
	return m
}

// Trees are the rules as the response's array carries them.
func Trees(rules []Resolved) []any {
	out := make([]any, len(rules))
	for i, r := range rules {
		out[i] = r.Tree()
	}
	return out
}

// Body is the response body of the resolution for the role at the instant.
func (r *Resolution) Body(at, role string) map[string]any {
	conflicts := make([]any, len(r.Conflicts))
	for i, c := range r.Conflicts {
		conflicts[i] = map[string]any{"subject": c.Subject, "scope_level": c.ScopeLevel, "rules": textsAny(c.Rules)}
	}
	excluded := make([]any, len(r.Excluded))
	for i, e := range r.Excluded {
		excluded[i] = map[string]any{"rule_id": e.RuleID, "reason": e.Reason}
	}
	body := map[string]any{"at": at, "role": role, "rules": Trees(r.Rules), "conflicts": conflicts, "excluded": excluded}
	put(body, "digest", r.Digest)
	return body
}
