package memory

import (
	"slices"
	"strings"

	"github.com/kesk-git/hive/packages/pool/internal/rule"
)

// The reasons a note becomes an item, as the folded receipt names them. A fact
// identity that stands with other text is pool.fact_conflict, which the fold
// never raises: it routes the note with the reason fact-conflict instead.
const (
	reasonSubjectUnknown = "subject-unknown"
	reasonOverBudget     = "over-budget"
	reasonFactConflict   = "fact-conflict"
)

// factOutcome is what filing a fact under a subject came to.
type factOutcome int

const (
	factAdded factOutcome = iota
	// factRepeated is a fact identical to one the subject owns: the filing is
	// idempotent and changes nothing.
	factRepeated
	// factConflicts is a fact identity the subject owns with other text.
	factConflicts
)

// factIDOf is the identity a note's fact is filed under: the one it carries, or
// the subject-local key note-<note ULID> minted for it, never inferred from its
// text.
func factIDOf(n *Note) string {
	if n.FactID != "" {
		return n.FactID
	}
	return "note-" + strings.TrimPrefix(n.NoteID, notePrefix)
}

// factOf is the note as a fact of its subject: its exact text and its
// provenance, kept whole.
func factOf(n *Note) rule.Fact {
	return rule.Fact{FactID: factIDOf(n), Text: n.Text, Provenance: copyValue(n.Provenance).(map[string]any)}
}

// fileFact files the fact under a copy of the subject, which is returned
// changed only when the fact is new. A repeated identical fact is idempotent;
// an existing identity with different text is a conflict and is never replaced.
func fileFact(sub *rule.Subject, f rule.Fact) (*rule.Subject, factOutcome) {
	for _, have := range sub.Facts {
		if have.FactID != f.FactID {
			continue
		}
		if have.Text == f.Text {
			return sub, factRepeated
		}
		return sub, factConflicts
	}
	next := *sub
	next.Facts = append(slices.Clone(sub.Facts), f)
	return &next, factAdded
}
