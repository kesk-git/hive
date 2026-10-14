package decision

import (
	"fmt"

	"github.com/kesk-git/hive/contract/canon"
)

// SchemaQuestion is the name every question record carries in its schema
// member, and QuestionVersion the version of it that this build writes.
const (
	SchemaQuestion  = "pool.question"
	QuestionVersion = "1.0"
)

// The states of a question (spec/word.md, A question's life).
const (
	StateOpen      = "open"
	StateDeferred  = "deferred"
	StateAnswered  = "answered"
	StateReleased  = "released"
	StateCompleted = "completed"
	StateMoot      = "moot"
)

// Question is one question the Hive Mind owes, with every revision of it, the
// hold it gates, every answer recorded against it and how it was settled
// (records/pool.question).
type Question struct {
	RecordRevision int64
	QuestionID     string
	Scope          Scope
	// HeldTaskID is the item the question holds, and HeldItem says whether it
	// is the work the question gates (work) or the decision item the Pool made
	// for it (decision-item).
	HeldTaskID      string
	HeldItem        string
	HoldID          string
	DecisionRoundID string
	CardID          string
	// Origin is where the question arose, kept whole.
	Origin          map[string]any
	CurrentRevision int64
	Revisions       []Revision
	State           string
	Answers         []Answer
	// Applied is the answer the question now stands on, while one is applied.
	Applied *Applied
	// Deferral is the revisit date of the latest Later applied, while the
	// question is deferred.
	Deferral *Deferral
	// Disposition and Closure are present only in the states that settle
	// the question.
	Disposition *Disposition
	Closure     *Closure
	CreatedAt   string
	UpdatedAt   string
	Writer      map[string]any
	// Unknown is the record's open object, kept as it was.
	Unknown map[string]any
}

// Revision is the question as asked at one revision.
type Revision struct {
	Revision       int64
	Text           string
	Consequence    string
	Recommendation string
	Options        []Option
	Amount         string
	AskedAt        string
	AskedBy        map[string]any
	RequestID      string
}

// Option is one answer the question offers, with what choosing it brings.
type Option struct {
	OptionID    string
	Label       string
	Consequence string
}

// Answer is one answer recorded against the question, exactly as given.
type Answer struct {
	Sequence         int64
	RecordRevision   int64
	QuestionRevision int64
	// Card is the card binding the answer was given on, kept whole.
	Card        map[string]any
	OptionID    string
	OptionLabel string
	Text        string
	Later       *Later
	// Source is the evidence the answer rests on, kept whole.
	Source      map[string]any
	AnsweredAt  string
	RecordedAt  string
	RecordedBy  map[string]any
	Disposition string
	// Supersedes and Reconciles are the sequence of the answer replaced or
	// settled; 0 for none, since sequences count from 1.
	Supersedes int64
	Reconciles int64
	RequestID  string
	ReceiptID  string
}

// Later is the Hive Mind's choice to answer later, with the revisit date.
type Later struct{ RevisitAt string }

// Applied names the answer the question stands on.
type Applied struct {
	AnswerSequence int64
	RecordRevision int64
}

// Deferral is the revisit date of the answer that deferred the question.
type Deferral struct {
	RevisitAt      string
	AnswerSequence int64
}

// Disposition is how the owner settled the applied answer.
type Disposition struct {
	Kind           string
	AnswerSequence int64
	Note           string
	// Evidence is the records the owner names, each reference kept whole; nil
	// when the member is absent.
	Evidence  []map[string]any
	At        string
	By        map[string]any
	RequestID string
	ReceiptID string
}

// Closure is the close of a moot question.
type Closure struct {
	Reason    string
	Evidence  []MootEvidence
	At        string
	By        map[string]any
	RequestID string
	ReceiptID string
}

// MootEvidence is one piece of evidence that a question is moot.
type MootEvidence struct {
	Kind string
	Ref  map[string]any
}

// DecodeQuestion decodes the bytes of a question record.
func DecodeQuestion(data []byte) (*Question, error) {
	tree, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("a question: %w", err)
	}
	return ParseQuestion(tree)
}

// ParseQuestion decodes the tree of a question record, whole numbers as int64
// as the canonical parse gives them.
func ParseQuestion(tree map[string]any) (*Question, error) {
	o := open("question", tree)
	if o.err != nil {
		return nil, o.err
	}
	if s := o.str("schema", true); s != SchemaQuestion {
		o.fail("the schema is %q", s)
	}
	if v := o.str("schema_version", true); v != QuestionVersion {
		o.fail("the schema_version is %q", v)
	}
	q := &Question{
		RecordRevision:  o.num("record_revision", true),
		QuestionID:      o.str("question_id", true),
		Scope:           readScope(o),
		HeldTaskID:      o.str("held_task_id", true),
		HeldItem:        o.str("held_item", true),
		HoldID:          o.str("hold_id", true),
		DecisionRoundID: o.str("decision_round_id", true),
		CardID:          o.str("card_id", true),
		Origin:          o.tree("origin", true),
		CurrentRevision: o.num("current_revision", true),
		Revisions:       list(o, "revisions", true, readRevision),
		State:           o.str("state", true),
		Answers:         list(o, "answers", true, readAnswer),
	}
	if c := o.child("applied", false); c != nil {
		q.Applied = &Applied{AnswerSequence: c.num("answer_sequence", true), RecordRevision: c.num("record_revision", true)}
		o.fold(c)
	}
	if c := o.child("deferral", false); c != nil {
		q.Deferral = &Deferral{RevisitAt: c.str("revisit_at", true), AnswerSequence: c.num("answer_sequence", true)}
		o.fold(c)
	}
	if c := o.child("disposition", false); c != nil {
		q.Disposition = &Disposition{
			Kind: c.str("kind", true), AnswerSequence: c.num("answer_sequence", true), Note: c.str("note", true),
			Evidence: c.trees("evidence", false), At: c.str("at", true), By: c.tree("by", true),
			RequestID: c.str("request_id", true), ReceiptID: c.str("receipt_id", true),
		}
		o.fold(c)
	}
	if c := o.child("closure", false); c != nil {
		q.Closure = &Closure{
			Reason: c.str("reason", true),
			Evidence: list(c, "evidence", true, func(e *object) MootEvidence {
				return MootEvidence{Kind: e.str("kind", true), Ref: e.tree("ref", true)}
			}),
			At: c.str("at", true), By: c.tree("by", true), RequestID: c.str("request_id", true), ReceiptID: c.str("receipt_id", true),
		}
		o.fold(c)
	}
	q.CreatedAt = o.str("created_at", true)
	q.UpdatedAt = o.str("updated_at", true)
	q.Writer = o.tree("writer", true)
	q.Unknown = o.tree("unknown", false)
	if err := o.done(); err != nil {
		return nil, err
	}
	return q, nil
}

// fold makes the child's fault the parent's, and a member of the child that
// was never asked for a fault as well.
func (o *object) fold(c *object) {
	if err := c.done(); err != nil && o.err == nil {
		o.err = err
	}
}

func readRevision(o *object) Revision {
	return Revision{
		Revision: o.num("revision", true), Text: o.str("text", true), Consequence: o.str("consequence", true),
		Recommendation: o.str("recommendation", false),
		Options: list(o, "options", true, func(p *object) Option {
			return Option{OptionID: p.str("option_id", true), Label: p.str("label", true), Consequence: p.str("consequence", true)}
		}),
		Amount: o.str("amount", false), AskedAt: o.str("asked_at", true), AskedBy: o.tree("asked_by", true), RequestID: o.str("request_id", true),
	}
}

func readAnswer(o *object) Answer {
	a := Answer{
		Sequence: o.num("sequence", true), RecordRevision: o.num("record_revision", true), QuestionRevision: o.num("question_revision", true),
		Card: o.tree("card", true), OptionID: o.str("option_id", false), OptionLabel: o.str("option_label", false), Text: o.str("text", true),
		Source: o.tree("source", true), AnsweredAt: o.str("answered_at", true), RecordedAt: o.str("recorded_at", true),
		RecordedBy: o.tree("recorded_by", true), Disposition: o.str("disposition", true),
		Supersedes: o.num("supersedes", false), Reconciles: o.num("reconciles", false),
		RequestID: o.str("request_id", true), ReceiptID: o.str("receipt_id", true),
	}
	if c := o.child("later", false); c != nil {
		a.Later = &Later{RevisitAt: c.str("revisit_at", true)}
		o.fold(c)
	}
	return a
}

// Tree is the record as a tree, whole numbers as int64: the document the store
// holds the question as. It shares nothing with the Question.
func (q *Question) Tree() map[string]any {
	m := map[string]any{
		"schema": SchemaQuestion, "schema_version": QuestionVersion,
		"record_revision": q.RecordRevision, "question_id": q.QuestionID,
		"held_task_id": q.HeldTaskID, "held_item": q.HeldItem, "hold_id": q.HoldID,
		"decision_round_id": q.DecisionRoundID, "card_id": q.CardID,
		"origin": copyTree(q.Origin), "current_revision": q.CurrentRevision,
		"revisions": each(q.Revisions, Revision.tree), "state": q.State,
		"answers":    each(q.Answers, Answer.tree),
		"created_at": q.CreatedAt, "updated_at": q.UpdatedAt, "writer": copyTree(q.Writer),
	}
	q.Scope.into(m)
	if a := q.Applied; a != nil {
		m["applied"] = map[string]any{"answer_sequence": a.AnswerSequence, "record_revision": a.RecordRevision}
	}
	if d := q.Deferral; d != nil {
		m["deferral"] = map[string]any{"revisit_at": d.RevisitAt, "answer_sequence": d.AnswerSequence}
	}
	if d := q.Disposition; d != nil {
		dm := map[string]any{
			"kind": d.Kind, "answer_sequence": d.AnswerSequence, "note": d.Note, "at": d.At,
			"by": copyTree(d.By), "request_id": d.RequestID, "receipt_id": d.ReceiptID,
		}
		if d.Evidence != nil {
			dm["evidence"] = asAny(d.Evidence)
		}
		m["disposition"] = dm
	}
	if c := q.Closure; c != nil {
		m["closure"] = map[string]any{
			"reason": c.Reason, "at": c.At, "by": copyTree(c.By), "request_id": c.RequestID, "receipt_id": c.ReceiptID,
			"evidence": each(c.Evidence, func(e MootEvidence) map[string]any {
				return map[string]any{"kind": e.Kind, "ref": copyTree(e.Ref)}
			}),
		}
	}
	if q.Unknown != nil {
		m["unknown"] = copyTree(q.Unknown)
	}
	return m
}

func (r Revision) tree() map[string]any {
	m := map[string]any{
		"revision": r.Revision, "text": r.Text, "consequence": r.Consequence,
		"options": each(r.Options, func(o Option) map[string]any {
			return map[string]any{"option_id": o.OptionID, "label": o.Label, "consequence": o.Consequence}
		}),
		"asked_at": r.AskedAt, "asked_by": copyTree(r.AskedBy), "request_id": r.RequestID,
	}
	put(m, "recommendation", r.Recommendation)
	put(m, "amount", r.Amount)
	return m
}

func (a Answer) tree() map[string]any {
	m := map[string]any{
		"sequence": a.Sequence, "record_revision": a.RecordRevision, "question_revision": a.QuestionRevision,
		"card": copyTree(a.Card), "text": a.Text, "source": copyTree(a.Source),
		"answered_at": a.AnsweredAt, "recorded_at": a.RecordedAt, "recorded_by": copyTree(a.RecordedBy),
		"disposition": a.Disposition, "request_id": a.RequestID, "receipt_id": a.ReceiptID,
	}
	if a.OptionID != "" {
		m["option_id"], m["option_label"] = a.OptionID, a.OptionLabel
	}
	if a.Later != nil {
		m["later"] = map[string]any{"revisit_at": a.Later.RevisitAt}
	}
	if a.Supersedes != 0 {
		m["supersedes"] = a.Supersedes
	}
	if a.Reconciles != 0 {
		m["reconciles"] = a.Reconciles
	}
	return m
}

// Encode is the document of the record in canonical form.
func (q *Question) Encode() ([]byte, error) { return canon.Encode(q.Tree()) }
