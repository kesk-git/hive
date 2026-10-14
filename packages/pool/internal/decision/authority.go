package decision

import (
	"errors"

	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The Pool's check of an authority reference of kind word, by the rule of
// spec/word.md, The word stands, applied to its own question records. An
// operation that accepts a word authority calls WordStands inside the
// transaction that reads the question, under the store's lock, so that the
// answer it relies on cannot be superseded between the check and the write.
// The reference is never itself authority: the verdict is read from the record.

// The reasons the word does not stand, as spec/word.md names them. The Pool
// reads questions only, so the reasons of a hold or an item are not among them.
const (
	reasonUnanswered   = "unanswered"
	reasonDeferred     = "deferred"
	reasonUnreconciled = "unreconciled"
	reasonSuperseded   = "superseded"
	reasonMoot         = "moot"
	reasonNotWord      = "not-word"
	reasonOutOfScope   = "out-of-scope"
	reasonNotFound     = "not-found"
	reasonMalformed    = "malformed"
)

// reasonOrder is the order the reasons are listed in, each once.
var reasonOrder = []string{
	reasonUnanswered, reasonDeferred, reasonUnreconciled, reasonSuperseded, reasonMoot,
	reasonNotWord, reasonOutOfScope, reasonNotFound, reasonMalformed,
}

// Standing is the verdict on a reference: the word stands when it gives no
// reason it does not.
type Standing struct {
	Reasons []string
}

// Stands reports whether the word stands.
func (s Standing) Stands() bool { return len(s.Reasons) == 0 }

// WordStands checks the authority, which names a question of the Pool at a
// record revision, against the question as the store holds it, for the scope of
// the envelope the authority accompanies. It stands when, and only when:
//
//  1. the reference is a record reference of schema pool.question, naming a
//     question of this home, at a record revision no greater than the
//     question's own;
//  2. the question is answered, released or completed, so that it stands on an
//     applied answer: a question that is open, deferred or moot, a moot one
//     whose hold a close released among them, never stands;
//  3. the applied answer was recorded at a record revision no greater than the
//     reference's: an answer that superseded the one relied on since is
//     superseded, and an answer first applied after the revision named is
//     unanswered;
//  4. the scope of the question contains the envelope's.
//
// The error is for what is not about the record, such as a failing disk; a
// record that is absent or cannot be read is a reason.
func WordStands(st *cstore.Store, scope envelope.Scope, auth envelope.Authority) (Standing, error) {
	got := verdict{}
	if auth.Kind != envelope.AuthorityWord {
		got[reasonNotWord] = true
		return got.standing(), nil
	}
	ref := auth.Ref
	if ref == nil || ref.Schema != SchemaQuestion || ref.ID == "" || ref.RecordRevision <= 0 {
		got[reasonMalformed] = true
		return got.standing(), nil
	}
	if ref.HomeID != "" && ref.HomeID != scope.HomeID {
		got[reasonNotFound] = true
		return got.standing(), nil
	}
	l, om, err := poolstore.Load(st, "question", ref.ID)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		got[reasonNotFound] = true
		return got.standing(), nil
	case err != nil:
		return Standing{}, poolstore.Failure("the question the authority names", err)
	case om != nil:
		got[reasonMalformed] = true
		return got.standing(), nil
	}
	q, err := ParseQuestion(l.Data)
	switch {
	case err != nil:
		got[reasonMalformed] = true
		return got.standing(), nil
	case q.Scope.HomeID != scope.HomeID:
		got[reasonNotFound] = true
		return got.standing(), nil
	case ref.RecordRevision > l.Revision:
		got[reasonMalformed] = true
		return got.standing(), nil
	}
	switch q.State {
	case StateAnswered, StateReleased, StateCompleted:
		for _, reason := range appliedBy(q, ref.RecordRevision) {
			got[reason] = true
		}
	case StateOpen:
		got[reasonUnanswered] = true
	case StateDeferred:
		got[reasonDeferred] = true
	case StateMoot:
		got[reasonMoot] = true
	default:
		got[reasonMalformed] = true
	}
	if !contains(q.Scope, scope) {
		got[reasonOutOfScope] = true
	}
	if len(got) > 0 && !got[reasonMalformed] && awaitsReconciliation(q) {
		got[reasonUnreconciled] = true
	}
	return got.standing(), nil
}

// verdict collects the reasons, each once.
type verdict map[string]bool

func (v verdict) standing() Standing {
	s := Standing{}
	for _, reason := range reasonOrder {
		if v[reason] {
			s.Reasons = append(s.Reasons, reason)
		}
	}
	return s
}

// appliedBy are the reasons the answer applied to the question does not stand
// for the revision the reference names: none when it was recorded at or before
// that revision; superseded when an answer it replaced, reached by following
// supersedes back from it, was recorded at or before; unanswered when none was.
func appliedBy(q *Question, revision int64) []string {
	if q.Applied == nil {
		return []string{reasonMalformed}
	}
	if q.Applied.RecordRevision <= revision {
		return nil
	}
	find := func(sequence int64) *Answer {
		for i := range q.Answers {
			if q.Answers[i].Sequence == sequence {
				return &q.Answers[i]
			}
		}
		return nil
	}
	cur := find(q.Applied.AnswerSequence)
	for steps := 0; cur != nil && steps <= len(q.Answers); steps++ {
		if cur.Supersedes == 0 {
			return []string{reasonUnanswered}
		}
		if cur = find(cur.Supersedes); cur == nil {
			break
		}
		if cur.RecordRevision <= revision {
			return []string{reasonSuperseded}
		}
	}
	if cur == nil {
		return []string{reasonMalformed}
	}
	return []string{reasonUnanswered}
}

// awaitsReconciliation reports whether an answer of the question awaits
// reconciliation: one of disposition unreconciled whose sequence is greater
// than that of every accepted or deferred answer.
func awaitsReconciliation(q *Question) bool {
	var applied, late int64
	for _, a := range q.Answers {
		switch a.Disposition {
		case dispositionAccepted, dispositionDeferred:
			applied = max(applied, a.Sequence)
		case dispositionUnreconciled:
			late = max(late, a.Sequence)
		}
	}
	return late > applied
}

// contains reports whether the scope of a question contains the envelope's: a
// Hive question any scope of its home, a workspace question its workspace and
// that workspace's projects, a project question that project.
func contains(question Scope, s envelope.Scope) bool {
	if question.HomeID != s.HomeID {
		return false
	}
	switch question.Level {
	case envelope.ScopeHive:
		return true
	case envelope.ScopeWorkspace:
		return s.Level != envelope.ScopeHive && question.WorkspaceID == s.WorkspaceID
	case envelope.ScopeProject:
		return s.Level == envelope.ScopeProject && question.WorkspaceID == s.WorkspaceID && question.ProjectID == s.ProjectID
	}
	return false
}
