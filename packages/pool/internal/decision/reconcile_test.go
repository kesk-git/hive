package decision

import (
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
)

// openQuestion is a question at its second revision with the answers given.
func openQuestion(answers ...Answer) *Question {
	return &Question{
		QuestionID: "q", State: StateOpen, CurrentRevision: 2, Answers: answers,
		Revisions: []Revision{{Revision: 1, Options: []Option{{OptionID: "keep"}}}, {Revision: 2, Options: []Option{{OptionID: "switch"}}}},
	}
}

// What recording an answer does is fixed by the revision it was given against
// and by Later: an earlier revision is unreconciled, Later or not.
func TestTheDispositionOfAnAnswer(t *testing.T) {
	q := openQuestion()
	for _, tc := range []struct {
		revision int64
		later    bool
		want     string
	}{
		{2, false, dispositionAccepted}, {2, true, dispositionDeferred},
		{1, false, dispositionUnreconciled}, {1, true, dispositionUnreconciled},
	} {
		if got := dispositionOf(q, tc.revision, tc.later); got != tc.want {
			t.Errorf("revision %d, later %v: %s, want %s", tc.revision, tc.later, got, tc.want)
		}
	}
	if revisionOf(q, 3) != nil || revisionOf(q, 0) != nil || !revisionOf(q, 1).offers("keep") || revisionOf(q, 1).offers("switch") {
		t.Errorf("the options of a revision")
	}
}

func code(err error) string {
	if err == nil {
		return ""
	}
	return envelope.AsFailure(err).Err.Code
}

// An applied answer is replaced only by an answer that is not Later and names
// it; a supersedes needs an applied answer and a reconciles an unreconciled
// one.
func TestTheReferencesOfAnAnswer(t *testing.T) {
	q := openQuestion(Answer{Sequence: 1, Disposition: dispositionUnreconciled}, Answer{Sequence: 2, Disposition: dispositionAccepted})
	if code(checkAnswerExists(q, true, 0)) != "" || code(checkAnswerExists(q, false, 5)) != "" {
		t.Errorf("a question with no applied answer takes any answer")
	}
	if code(checkReferences(q, 2, 0)) != codeAnswerReference || code(checkReferences(q, 0, 2)) != codeAnswerReference ||
		code(checkReferences(q, 0, 9)) != codeAnswerReference || code(checkReferences(q, 0, 1)) != "" || code(checkReferences(q, 0, 0)) != "" {
		t.Errorf("references of a question with no applied answer")
	}
	q.State, q.Applied = StateAnswered, &Applied{AnswerSequence: 2, RecordRevision: 3}
	if code(checkAnswerExists(q, false, 2)) != "" {
		t.Errorf("an answer naming the applied one supersedes it")
	}
	for _, tc := range []struct {
		later      bool
		supersedes int64
	}{{false, 0}, {false, 1}, {true, 0}, {true, 2}} {
		if code(checkAnswerExists(q, tc.later, tc.supersedes)) != codeAnswerExists {
			t.Errorf("later %v, supersedes %d", tc.later, tc.supersedes)
		}
	}
	if code(checkReferences(q, 2, 0)) != "" {
		t.Errorf("supersedes names the applied answer")
	}
}

// Two answers with the same source are one; the key is the whole source.
func TestTheSourceOfAnAnswerIsItsKey(t *testing.T) {
	q := openQuestion(Answer{Sequence: 1, Source: map[string]any{"kind": "operator-request", "request_id": "req_a"}})
	if a := answerWithSource(q, map[string]any{"request_id": "req_a", "kind": "operator-request"}); a == nil || a.Sequence != 1 {
		t.Errorf("the same source: %v", a)
	}
	if answerWithSource(q, map[string]any{"kind": "operator-request", "request_id": "req_b"}) != nil {
		t.Errorf("another source")
	}
}

// Applying an answer changes the state, the card and the round by its
// disposition and by nothing else.
func TestApplyingAnAnswer(t *testing.T) {
	two := func() (*Question, *Round) {
		q := openQuestion()
		return q, &Round{State: RoundOpen, Cards: []Card{{CardID: "a", State: CardOpen}, {CardID: "b", State: CardOpen}}}
	}
	q, r := two()
	if apply(q, &r.Cards[0], r, Answer{Sequence: 1, RecordRevision: 4, Disposition: dispositionAccepted}, "t1") != true ||
		q.State != StateAnswered || q.Applied.AnswerSequence != 1 || r.Cards[0].State != CardAnswered || r.Cards[0].AnswerSequence != 1 || r.State != RoundOpen {
		t.Errorf("an accepted answer: %+v %+v", q, r)
	}
	if !apply(q, &r.Cards[1], r, Answer{Sequence: 2, RecordRevision: 5, Disposition: dispositionAccepted}, "t2") || r.State != RoundClosed || r.ClosedAt != "t2" {
		t.Errorf("the last card: %+v", r)
	}

	q, r = two()
	q.State, q.Deferral = StateDeferred, &Deferral{RevisitAt: "d0", AnswerSequence: 1}
	if apply(q, &r.Cards[0], r, Answer{Sequence: 2, Disposition: dispositionDeferred, Later: &Later{RevisitAt: "d1"}}, "t") ||
		q.State != StateDeferred || q.Deferral.RevisitAt != "d1" || q.Deferral.AnswerSequence != 2 || r.Cards[0].State != CardOpen || r.State != RoundOpen {
		t.Errorf("a deferral: %+v %+v", q, r)
	}
	if !apply(q, &r.Cards[0], r, Answer{Sequence: 3, RecordRevision: 6, Disposition: dispositionAccepted}, "t") || q.Deferral != nil {
		t.Errorf("an accepted answer ends a deferral: %+v", q)
	}

	q, r = two()
	if apply(q, &r.Cards[0], r, Answer{Sequence: 1, Disposition: dispositionUnreconciled}, "t") || q.State != StateOpen || q.Applied != nil || q.Deferral != nil ||
		r.Cards[0].State != CardOpen || r.UpdatedAt != "" {
		t.Errorf("an unreconciled answer applies to nothing: %+v %+v", q, r)
	}
}
