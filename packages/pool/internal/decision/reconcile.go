package decision

import (
	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	cstore "github.com/kesk-git/hive/contract/store"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// How an answer is reconciled with the question it was given to and applied to
// it (spec/word.md, answer): what recording an answer does is fixed by the
// revision it was given against and by whether it is Later, and what applying
// it changes is the question's state, its card and its round. The functions
// here decide and apply; answer.go reads the records and makes the checks.

// The dispositions of an answer, fixed when it is recorded.
const (
	dispositionAccepted     = "accepted"
	dispositionDeferred     = "deferred"
	dispositionUnreconciled = "unreconciled"
)

// The receipt and event kinds an answer writes.
const (
	receiptAccepted     = "pool.answer.accepted"
	receiptSuperseded   = "pool.answer.superseded"
	receiptUnreconciled = "pool.answer.unreconciled"
	receiptDeferred     = "pool.question.deferred"
	eventAnswered       = "pool.question.answered"
)

// The codes of the Pool that the checks of an answer return.
const (
	codeCardStale       = "pool.card_stale"
	codeAnswerExists    = "pool.answer_exists"
	codeAnswerReference = "pool.answer_reference"
)

// dispositionOf is what recording an answer does: an answer given against an
// earlier revision than the current one is recorded unreconciled, Later or
// not; against the current revision Later defers the question, and any other
// answer is accepted.
func dispositionOf(q *Question, revisionAnswered int64, isLater bool) string {
	switch {
	case revisionAnswered != q.CurrentRevision:
		return dispositionUnreconciled
	case isLater:
		return dispositionDeferred
	}
	return dispositionAccepted
}

// revisionOf is the revision of the question the number names, or nil where
// the question has none.
func revisionOf(q *Question, number int64) *Revision {
	for i := range q.Revisions {
		if q.Revisions[i].Revision == number {
			return &q.Revisions[i]
		}
	}
	return nil
}

// offers reports whether the revision has the option.
func (r *Revision) offers(optionID string) bool {
	for _, o := range r.Options {
		if o.OptionID == optionID {
			return true
		}
	}
	return false
}

// answerWithSource is the answer first recorded to the question with the
// source, or nil. Two answers to one question with the same source are one
// answer; the key is the question and the source together.
func answerWithSource(q *Question, source map[string]any) *Answer {
	want, err := canon.Encode(source)
	if err != nil {
		return nil
	}
	for i := range q.Answers {
		if have, err := canon.Encode(q.Answers[i].Source); err == nil && string(have) == string(want) {
			return &q.Answers[i]
		}
	}
	return nil
}

// checkAnswerExists refuses, with pool.answer_exists, an answer to a question
// that stands on an applied answer unless the answer is not Later and
// supersedes names that answer: while its disposition is pending the applied
// answer is replaced only by naming it, and Later never replaces it.
func checkAnswerExists(q *Question, isLater bool, supersedes int64) error {
	if q.Applied == nil || (!isLater && supersedes == q.Applied.AnswerSequence) {
		return nil
	}
	return envelope.Fail(envelope.Conflict, codeAnswerExists, "the question stands on an applied answer, and this answer does not supersede it")
}

// checkReferences refuses, with pool.answer_reference, a supersedes given while
// no answer is applied, and a reconciles that names an answer that is not
// unreconciled. Only an answer against the current revision is held to it: an
// unreconciled answer keeps both as given and acts on neither.
func checkReferences(q *Question, supersedes, reconciles int64) error {
	if supersedes != 0 && q.Applied == nil {
		return envelope.Fail(envelope.Invalid, codeAnswerReference, "supersedes names an answer while no answer is applied")
	}
	if reconciles == 0 {
		return nil
	}
	for _, a := range q.Answers {
		if a.Sequence == reconciles && a.Disposition == dispositionUnreconciled {
			return nil
		}
	}
	return envelope.Fail(envelope.Invalid, codeAnswerReference, "reconciles names an answer that is not unreconciled")
}

// apply applies the answer just recorded, the last of the question's answers,
// by its disposition. An accepted answer leaves the question standing on it
// and ends a deferral, answers the card with the answer's sequence and closes
// the round when no card of it is open; a deferred one leaves the question
// deferred with its revisit date and the card open; an unreconciled one
// changes nothing. It reports whether the round changed. The hold is no part
// of it: an answer never releases it.
func apply(q *Question, card *Card, round *Round, a Answer, now string) (roundChanged bool) {
	switch a.Disposition {
	case dispositionAccepted:
		q.State, q.Applied, q.Deferral = StateAnswered, &Applied{AnswerSequence: a.Sequence, RecordRevision: a.RecordRevision}, nil
		card.State, card.AnswerSequence = CardAnswered, a.Sequence
		round.UpdatedAt = now
		if round.State != RoundClosed && !anyOpen(round) {
			round.State, round.ClosedAt = RoundClosed, now
		}
		roundChanged = true
	case dispositionDeferred:
		q.State, q.Deferral = StateDeferred, &Deferral{RevisitAt: a.Later.RevisitAt, AnswerSequence: a.Sequence}
	default:
		return false
	}
	q.UpdatedAt = now
	return roundChanged
}

// anyOpen reports whether a card of the round is open.
func anyOpen(r *Round) bool {
	for _, c := range r.Cards {
		if c.State == CardOpen {
			return true
		}
	}
	return false
}

// writeQuestion adds the question to the transaction, as the revision after
// the one read, and returns it as the store will write it with the digest of
// that record, which the receipts of the answer name as their subject. The
// receipt the answer names is made before the record, because the record
// holds its identity.
func (b *builder) writeQuestion(q *Question, expected int64) (final map[string]any, ref *envelope.RecordRef, err error) {
	body := q.Tree()
	w := b.c.Writer()
	body["writer"] = map[string]any{"package": w.Package, "command": w.Command, "operation": w.Operation, "implementation_version": w.ImplementationVersion}
	k, _ := poolstore.Lookup("question")
	path, err := k.Path(q.QuestionID)
	if err != nil {
		return nil, nil, internal("a record cannot be finalized", err)
	}
	content, err := canon.Encode(body)
	if err != nil {
		return nil, nil, internal("a record cannot be finalized", err)
	}
	rw, digest, err := b.c.Finalize(cstore.RecordWrite{Path: path, Content: content, Expected: expected})
	if err != nil {
		return nil, nil, internal("a record cannot be finalized", err)
	}
	v, err := canon.Parse(rw.Content)
	if err != nil {
		return nil, nil, internal("a record cannot be finalized", err)
	}
	final = v.(map[string]any)
	b.tx.Records = append(b.tx.Records, poolstore.Write{Kind: "question", ID: q.QuestionID, Body: final, Expected: expected})
	ref = &envelope.RecordRef{Schema: SchemaQuestion, ID: q.QuestionID, HomeID: b.c.Home.HomeID, RecordRevision: expected + 1, Digest: digest}
	return final, ref, nil
}

// answerReceipt adds the receipt of the kind about the question, under the
// identity given or a new one, to the transaction, and returns its identity.
func (b *builder) answerReceipt(kind, id string, subject *envelope.RecordRef, q *Question) (string, error) {
	r, err := b.c.NewReceipt(kind, versionOne, subject, nil, nil)
	if err != nil {
		return "", internal("a receipt cannot be made", err)
	}
	if id != "" {
		r.ReceiptID = id
	}
	r.Scope = q.Scope.envelope()
	r.Scope.TaskID = q.HeldTaskID
	b.tx.Receipts = append(b.tx.Receipts, r)
	return r.ReceiptID, nil
}

// answerEvent adds the event of the kind, with the empty body and the basis
// the Pool registers beside a receipt, to the transaction. Its scope and
// task_id are the question's.
func (b *builder) answerEvent(kind, basis string, q *Question) error {
	eventID, err := ids.Mint(b.c.Clock, ids.Event)
	if err != nil {
		return internal("an event cannot be made", err)
	}
	now, err := b.c.Stamp()
	if err != nil {
		return internal("an event cannot be made", err)
	}
	event := map[string]any{
		"schema": "hive.event", "schema_version": versionOne, "event_id": eventID,
		"kind": kind, "kind_version": versionOne,
		"source":         map[string]any{"package": poolstore.Package, "source_id": poolstore.Package},
		"correlation_id": b.c.Request.CorrelationID,
		"causation":      map[string]any{"schema": schemaRequest, "id": b.c.Request.RequestID},
		"occurred_at":    now, "observed_at": now,
		"basis": map[string]any{"kind": "receipt", "ref": map[string]any{"schema": schemaReceipt, "id": basis}},
		"body":  map[string]any{},
	}
	putScope(event, q.Scope.envelope(), true)
	event["task_id"] = q.HeldTaskID
	content, err := canon.Encode(event)
	if err != nil {
		return internal("an event cannot be written", err)
	}
	b.tx.Events = append(b.tx.Events, cstore.Event{ID: eventID, Content: content})
	return nil
}
