package admission

import (
	"slices"
	"time"

	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/decision"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The rule route of spec/pool.md, Admission: a standing rule is the Hive Mind's
// own sentence recorded as a rule, and clears a sensitive boundary before launch
// only when its subject names that one boundary, its applying authority is a
// word that stands at its scope, every extension of its expiry was made under a
// word that stands, and the resolver of spec/pool.md, Rules, resolve, gives it
// among the rules in force for the admission's scope at the instant of the check,
// with no conflict on its subject. An admission carries no role, task generation
// or generation, so a rule narrowed to a task or a generation, one that applies
// to a role, and a product rule clear nothing. Any other rule clears nothing.

// subjectPrefix begins the subject of a rule on a boundary of admission.
const subjectPrefix = "admission."

// ruleClaim is the rule the request's authority names, read as it now stands,
// the rules of the home it is resolved among, and what the Pool found of the
// words it rests on. A reference that names no readable rule of this home has no
// rule and says why.
type ruleClaim struct {
	rule *rule.Rule
	// home is every rule of the home, as the resolver reads them, and scope the
	// admission's own.
	home  []*rule.Rule
	scope envelope.Scope
	// word is the verdict on the rule's applying authority at the rule's scope,
	// and extended on its extensions of expiry.
	word     decision.Standing
	extended []decision.Standing
	// unread says why no rule is read, where none is.
	unread string
}

// readRule reads the rule the authority names and the rules it is resolved
// among. A reference that does not name a readable rule of this home, or a home
// whose rules cannot all be read, gives a claim that clears nothing; only a
// failing store is an error.
func readRule(st *cstore.Store, rec *Record, auth envelope.Authority) (*ruleClaim, error) {
	claim := &ruleClaim{scope: rec.Scope()}
	ref := auth.Ref
	if ref == nil || ref.Schema != rule.SchemaRule || ref.ID == "" || ref.RecordRevision <= 0 ||
		(ref.HomeID != "" && ref.HomeID != claim.scope.HomeID) {
		claim.unread = "the reference names no rule of this home"
		return claim, nil
	}
	all, omitted, err := poolstore.LoadAll(st, "rule")
	if err != nil {
		return nil, err
	}
	var named *poolstore.Loaded
	for i := range all {
		if all[i].ID == ref.ID {
			named = &all[i]
		}
	}
	switch {
	case named == nil && !slices.ContainsFunc(omitted, func(o poolstore.Omission) bool { return o.ID == ref.ID }):
		claim.unread = "the reference names no rule of this home"
		return claim, nil
	case len(omitted) > 0:
		claim.unread = "a rule of the home cannot be read, so the rules in force cannot be known"
		return claim, nil
	case ref.RecordRevision > named.Revision:
		claim.unread = "the reference is at a revision the rule has not reached"
		return claim, nil
	}
	for _, l := range all {
		r, err := rule.ParseRule(l.Data)
		if err != nil {
			claim.unread = "a rule of the home cannot be read, so the rules in force cannot be known"
			return claim, nil
		}
		claim.home = append(claim.home, r)
		if l.ID == ref.ID {
			claim.rule = r
		}
	}
	r := claim.rule
	if claim.word, err = decision.WordStands(st, envelopeScope(r.Scope), authorityOf(r.Authority)); err != nil {
		return nil, err
	}
	for _, a := range r.Amendments {
		if a.Kind != rule.AmendmentExtend {
			continue
		}
		verdict, err := decision.WordStands(st, envelopeScope(r.Scope), authorityOf(a.Authority))
		if err != nil {
			return nil, err
		}
		claim.extended = append(claim.extended, verdict)
	}
	return claim, nil
}

// clears says why the rule does not clear the boundary at the instant, in one
// clause, and "" when it does.
func (c *ruleClaim) clears(boundary string, at time.Time) string {
	if c.rule == nil {
		return c.unread
	}
	r := c.rule
	switch {
	case r.Subject != subjectPrefix+boundary:
		return "the rule's subject is not " + subjectPrefix + boundary
	case r.Authority["kind"] != envelope.AuthorityWord:
		return "the rule's applying authority is not a word"
	case !c.word.Stands():
		return "the word the rule was applied by no longer stands"
	case slices.ContainsFunc(c.extended, func(s decision.Standing) bool { return !s.Stands() }):
		return "an extension of the rule's expiry was not made under a word that stands"
	}
	res, err := rule.Compute(rule.Request{Scope: c.scope, At: at}, c.home)
	switch {
	case err != nil:
		return "a rule of the home cannot be placed in time, so the rules in force cannot be known"
	case slices.ContainsFunc(res.Conflicts, func(k rule.Conflict) bool { return k.Subject == r.Subject }):
		return "the rule's subject is in conflict"
	case !slices.ContainsFunc(res.Rules, func(x rule.Resolved) bool { return x.RuleID == r.RuleID }):
		return "the rule is not in force for the admission"
	}
	return ""
}

// envelopeScope is the scope of a rule as the envelope carries it.
func envelopeScope(s rule.Scope) envelope.Scope {
	return envelope.Scope{Level: s.Level, HomeID: s.HomeID, WorkspaceID: s.WorkspaceID, ProjectID: s.ProjectID}
}
