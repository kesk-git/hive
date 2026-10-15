package memory

import (
	"slices"
	"strings"

	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/decision"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// SecondMiss is the count at which a subject is due a decision.
const SecondMiss = 2

// SubjectMisses is what the check reports of one subject: the distinct verified
// independent root incidents counted against it, the misses that contribute to
// them, whether a decision is due, and the earlier question that names the
// subject where there is one.
type SubjectMisses struct {
	SubjectID   string
	Independent int
	Misses      []string
	DecisionDue bool
	QuestionID  string
}

// countMisses counts the misses per filing subject (spec/pool.md, Generated
// knowledge, Independent evidence provenance). A count is the number of distinct
// verified independent root incidents, following derivation links transitively.
//
//   - An independent account is its own root: its incident, by home, schema and
//     record identity. A revision or a digest does not make another incident.
//   - A derived account has the roots of the accounts it derives from. A link
//     names a miss of the Pool, or an incident that an independent account of the
//     Pool reports; a link that names neither is dangling, and a link that leads
//     back to the account is a cycle. Either gives the account no root, so it
//     contributes no count until the provenance is reconciled.
//   - An account whose independence is unknown, or whose derivation reaches one
//     that is, contributes no count: independence is never assumed.
//
// Two misses of one incident, whether of two rules or of the same account echoed
// by a supervisor or a synchronized report, are one root and count once. Every
// miss stays readable: a subject with misses is reported even at a count of zero.
func countMisses(misses []*Miss) []SubjectMisses {
	byID := map[string]*Miss{}
	independentOf := map[string]bool{}
	for _, m := range misses {
		byID[m.ID] = m
		if m.Independence == "independent" {
			independentOf[m.Incident.key()] = true
		}
	}

	type outcome struct {
		roots []string
		ok    bool
	}
	memo := map[string]outcome{}
	visiting := map[string]bool{}
	var rootsOf func(m *Miss) outcome
	rootsOf = func(m *Miss) outcome {
		if o, done := memo[m.ID]; done {
			return o
		}
		if visiting[m.ID] {
			return outcome{}
		}
		visiting[m.ID] = true
		defer delete(visiting, m.ID)
		var o outcome
		switch m.Independence {
		case "independent":
			o = outcome{roots: []string{m.Incident.key()}, ok: true}
		case "derived":
			o.ok = len(m.DerivedFrom) > 0
			for _, link := range m.DerivedFrom {
				var linked outcome
				if link.Schema == schemaMiss {
					if target, held := byID[link.ID]; held && link.Home == m.Scope.HomeID {
						linked = rootsOf(target)
					}
				} else if independentOf[link.key()] {
					linked = outcome{roots: []string{link.key()}, ok: true}
				}
				if !linked.ok {
					o = outcome{}
					break
				}
				o.roots = append(o.roots, linked.roots...)
			}
		}
		memo[m.ID] = o
		return o
	}

	type tally struct {
		roots map[string]bool
		ids   []string
	}
	bySubject := map[string]*tally{}
	for _, m := range misses {
		t := bySubject[m.SubjectID]
		if t == nil {
			t = &tally{roots: map[string]bool{}}
			bySubject[m.SubjectID] = t
		}
		if o := rootsOf(m); o.ok {
			t.ids = append(t.ids, m.ID)
			for _, r := range o.roots {
				t.roots[r] = true
			}
		}
	}

	subjects := make([]string, 0, len(bySubject))
	for id := range bySubject {
		subjects = append(subjects, id)
	}
	slices.Sort(subjects)
	out := make([]SubjectMisses, 0, len(subjects))
	for _, id := range subjects {
		t := bySubject[id]
		slices.Sort(t.ids)
		out = append(out, SubjectMisses{SubjectID: id, Independent: len(t.roots), Misses: t.ids})
	}
	return out
}

// decisionsDue marks the subjects whose count has reached the second miss, and
// names the earlier question that already names a subject in its origin: the
// card for a subject's second miss has origin kind rule and an origin ref of
// schema pool.subject naming the subject, and that subject is its durable
// deduplication key. A subject with such a question is not due another.
func decisionsDue(st *cstore.Store, subjects []SubjectMisses) error {
	reached := false
	for _, s := range subjects {
		reached = reached || s.Independent >= SecondMiss
	}
	if !reached {
		return nil
	}
	questions, omitted, err := poolstore.LoadAll(st, "question")
	if err != nil {
		return poolstore.Failure("the questions", err)
	}
	if len(omitted) > 0 {
		return malformed("a question cannot be read, so whether a decision is already asked cannot be known", omitted[0].ID, nil)
	}
	earlier := map[string]string{}
	for _, l := range questions {
		q, err := decision.ParseQuestion(l.Data)
		if err != nil {
			return malformed("a question cannot be read, so whether a decision is already asked cannot be known", l.ID, err)
		}
		origin := tree(q.Origin["ref"])
		if q.Origin["kind"] != "rule" || origin["schema"] != schemaSubject {
			continue
		}
		subject := str(origin["id"])
		if first, named := earlier[subject]; !named || strings.Compare(q.QuestionID, first) < 0 {
			earlier[subject] = q.QuestionID
		}
	}
	for i := range subjects {
		subjects[i].QuestionID = earlier[subjects[i].SubjectID]
		subjects[i].DecisionDue = subjects[i].Independent >= SecondMiss && subjects[i].QuestionID == ""
	}
	return nil
}

// tree is the subject's row as the report holds it.
func (s SubjectMisses) tree() map[string]any {
	ids := make([]any, len(s.Misses))
	for i, id := range s.Misses {
		ids[i] = id
	}
	row := map[string]any{
		"subject_id": s.SubjectID, "independent_misses": int64(s.Independent), "misses": ids, "decision_due": s.DecisionDue,
	}
	if s.QuestionID != "" {
		row["question_id"] = s.QuestionID
	}
	return row
}
