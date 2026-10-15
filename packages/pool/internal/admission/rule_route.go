package admission

import (
	"errors"
	"time"

	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/decision"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The rule route of spec/pool.md, Admission: a standing rule is the Hive Mind's
// own sentence recorded as a rule, and clears a sensitive boundary before launch
// only when its subject names that one boundary, its applying authority is a
// word that stands at its scope, it is in force when the work is checked, and
// its scope contains the admission's. A rule narrowed to a task clears only an
// admission of that task, and one narrowed to a generation clears nothing, since
// an admission is bound to no generation. Any other rule clears nothing.

// subjectPrefix begins the subject of a rule on a boundary of admission.
const subjectPrefix = "admission."

// ruleClaim is the rule the request's authority names, read as it now stands,
// and what the Pool found of its applying word. A reference that names no
// readable rule of this home has no rule and says why.
type ruleClaim struct {
	rule *rule.Rule
	// scope is the admission's own, and task the item it concerns.
	scope envelope.Scope
	task  string
	// word is the verdict on the rule's applying authority at the rule's scope.
	word decision.Standing
	// unread says why no rule is read, where none is.
	unread string
}

// readRule reads the rule the authority names. A reference that does not name a
// readable rule of this home gives a claim that clears nothing; only a failing
// store is an error.
func readRule(st *cstore.Store, rec *Record, auth envelope.Authority) (*ruleClaim, error) {
	claim := &ruleClaim{scope: rec.Scope(), task: rec.TaskID()}
	ref := auth.Ref
	if ref == nil || ref.Schema != rule.SchemaRule || ref.ID == "" || ref.RecordRevision <= 0 ||
		(ref.HomeID != "" && ref.HomeID != claim.scope.HomeID) {
		claim.unread = "the reference names no rule of this home"
		return claim, nil
	}
	l, om, err := poolstore.Load(st, "rule", ref.ID)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		claim.unread = "the reference names no rule of this home"
		return claim, nil
	case err != nil:
		return nil, err
	case om != nil:
		claim.unread = "the rule it names cannot be read"
		return claim, nil
	case ref.RecordRevision > l.Revision:
		claim.unread = "the reference is at a revision the rule has not reached"
		return claim, nil
	}
	r, err := rule.ParseRule(l.Data)
	if err != nil {
		claim.unread = "the rule it names cannot be read"
		return claim, nil
	}
	claim.rule = r
	if claim.word, err = decision.WordStands(st, envelopeScope(r.Scope), authorityOf(r.Authority)); err != nil {
		return nil, err
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
	case !inForce(r, at):
		return "the rule is not in force"
	case r.Generation != 0:
		return "the rule is narrowed to a generation and an admission is bound to none"
	case r.TaskID != "" && r.TaskID != c.task:
		return "the rule is narrowed to another task"
	case !contains(r.Scope, c.scope):
		return "the rule's scope does not contain the admission's"
	}
	return ""
}

// inForce says whether the rule is in force at the instant: in force by its
// status, begun, not lifted and not past its effective expiry by the clock.
func inForce(r *rule.Rule, at time.Time) bool {
	if r.Status != rule.StatusInForce || r.Lifted() {
		return false
	}
	begun, err := clock.Parse(r.EffectiveAt)
	if err != nil || at.Before(begun) {
		return false
	}
	past, err := r.ExpiredAt(at)
	return err == nil && !past
}

// envelopeScope is the scope of a rule as the envelope carries it.
func envelopeScope(s rule.Scope) envelope.Scope {
	return envelope.Scope{Level: s.Level, HomeID: s.HomeID, WorkspaceID: s.WorkspaceID, ProjectID: s.ProjectID}
}

// contains says whether the scope outer is the scope inner or contains it: the
// same home, the Hive for any scope of it, a workspace for itself and its
// projects, a project for itself.
func contains(outer rule.Scope, inner envelope.Scope) bool {
	if outer.HomeID != inner.HomeID {
		return false
	}
	switch outer.Level {
	case envelope.ScopeHive:
		return true
	case envelope.ScopeWorkspace:
		return inner.Level != envelope.ScopeHive && inner.WorkspaceID == outer.WorkspaceID
	case envelope.ScopeProject:
		return inner.Level == envelope.ScopeProject && inner.WorkspaceID == outer.WorkspaceID && inner.ProjectID == outer.ProjectID
	}
	return false
}
