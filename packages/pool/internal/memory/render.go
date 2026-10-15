package memory

import (
	"fmt"
	"strings"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
)

// render makes the rendering of the projection of the key from the rules in
// force at the instant, the subject and the budget at the revision. An always
// projection renders the always-tier operating lines of its audience at its
// exact scope and no fact; a subject projection renders the subject-tier lines
// filed under its subject that apply to its audience, resolved through the
// scope and what it inherits, and the subject's facts. A product rule is no
// input: the resolver collects operating rules only.
//
// Every rule in force is a source, at the revision read, beside the subject and
// the budget; the rules the text renders are the inventory. The rules digest is
// the resolved-rule digest of the audience at the scope, empty where two rules
// conflict, and a rendering that conflicts is returned with its conflicts and
// is never written.
func (s *Snapshot) render(k Key, at time.Time, budgetRevision int64) (*Rendering, error) {
	res, err := s.resolve(k.Scope, k.Audience, at)
	if err != nil {
		return nil, err
	}
	var sub *rule.Subject
	if k.Tier == TierSubject {
		if sub = s.subject(k.SubjectID); sub == nil {
			return nil, noSubject()
		}
		if !within(sub.Scope, k.Scope) {
			return nil, subjectScope()
		}
	}
	instant, err := clock.Format(at)
	if err != nil {
		return nil, err
	}
	r := &Rendering{RendererVer: RendererVersion, RulesDigest: res.Digest, At: instant, Conflicts: res.Conflicts, budgetRevision: budgetRevision}
	var lines []string
	for _, e := range res.Rules {
		r.Sources = append(r.Sources, Source{Schema: schemaRule, ID: e.RuleID, Revision: e.RecordRevision})
		switch {
		case k.Tier == TierAlways && e.Tier == TierAlways && e.ScopeLevel == k.Scope.Level,
			k.Tier == TierSubject && e.Tier == TierSubject && e.FiledUnder == k.SubjectID:
			lines = append(lines, e.Line)
			r.Rules = append(r.Rules, RuleRef{RuleID: e.RuleID, Revision: e.RecordRevision})
		}
	}
	if sub != nil {
		r.Sources = append(r.Sources, Source{Schema: schemaSubject, ID: sub.SubjectID, Revision: sub.RecordRevision})
	}
	r.Sources = append(r.Sources, Source{Schema: schemaBudget, ID: budgetID, Revision: budgetRevision})
	r.Text = text(sub, lines)
	r.Digest = canon.FileDigest([]byte(r.Text))
	r.Bytes = int64(len(r.Text))
	return r, nil
}

// text is the rendered instructions, the same for the same lines and facts.
func text(sub *rule.Subject, lines []string) string {
	var b strings.Builder
	if sub != nil {
		fmt.Fprintf(&b, "# %s\n", sub.Key)
	}
	for _, l := range lines {
		b.WriteString("- " + l + "\n")
	}
	if sub != nil && len(sub.Facts) > 0 {
		b.WriteString("Facts:\n")
		for _, f := range sub.Facts {
			fmt.Fprintf(&b, "- %s: %s\n", f.FactID, f.Text)
		}
	}
	return b.String()
}
