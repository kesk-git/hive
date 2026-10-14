package decision

import (
	"fmt"

	"github.com/kesk-git/hive/contract/canon"
)

// SchemaRound is the name every decision-round record carries in its schema
// member, and RoundVersion the version of it that this build writes.
const (
	SchemaRound  = "pool.decision-round"
	RoundVersion = "1.0"
)

// The states of a decision round and of its cards.
const (
	RoundOpen   = "open"
	RoundClosed = "closed"

	CardOpen     = "open"
	CardAnswered = "answered"
	CardMoot     = "moot"
)

// Round is one round of questions put to the Hive Mind together, with the card
// that presents each (records/pool.decision-round).
type Round struct {
	RecordRevision  int64
	DecisionRoundID string
	Scope           Scope
	// Origin is where the questions arose, kept whole.
	Origin      map[string]any
	Cards       []Card
	Attachments []Attachment
	// Complete is true when every nested text, question and attachment of the
	// round was collected whole; IncompleteReason names the blocker when not.
	Complete         bool
	IncompleteReason string
	State            string
	OpenedAt         string
	ClosedAt         string
	CreatedAt        string
	UpdatedAt        string
	Writer           map[string]any
	// Unknown is the record's open object, kept as it was.
	Unknown map[string]any
}

// Card presents one question of the round at a revision.
type Card struct {
	CardID           string
	CardRevision     int64
	QuestionID       string
	QuestionRevision int64
	HeldTaskID       string
	HeldTaskRevision int64
	PresentedAt      string
	State            string
	// AnswerSequence is the question's applied answer; 0 unless the card is
	// answered, since sequences count from 1.
	AnswerSequence int64
}

// Attachment is one file of the round's manifest with its fetch outcome.
type Attachment struct {
	// File is the file reference, kept whole.
	File    map[string]any
	Fetched string
}

// DecodeRound decodes the bytes of a decision-round record.
func DecodeRound(data []byte) (*Round, error) {
	tree, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("a decision round: %w", err)
	}
	return ParseRound(tree)
}

// ParseRound decodes the tree of a decision-round record, whole numbers as
// int64 as the canonical parse gives them.
func ParseRound(tree map[string]any) (*Round, error) {
	o := open("decision round", tree)
	if o.err != nil {
		return nil, o.err
	}
	if s := o.str("schema", true); s != SchemaRound {
		o.fail("the schema is %q", s)
	}
	if v := o.str("schema_version", true); v != RoundVersion {
		o.fail("the schema_version is %q", v)
	}
	r := &Round{
		RecordRevision:  o.num("record_revision", true),
		DecisionRoundID: o.str("decision_round_id", true),
		Scope:           readScope(o),
		Origin:          o.tree("origin", true),
		Cards: list(o, "cards", true, func(c *object) Card {
			return Card{
				CardID: c.str("card_id", true), CardRevision: c.num("card_revision", true),
				QuestionID: c.str("question_id", true), QuestionRevision: c.num("question_revision", true),
				HeldTaskID: c.str("held_task_id", true), HeldTaskRevision: c.num("held_task_revision", true),
				PresentedAt: c.str("presented_at", true), State: c.str("state", true), AnswerSequence: c.num("answer_sequence", false),
			}
		}),
		Attachments: list(o, "attachments", true, func(a *object) Attachment {
			return Attachment{File: a.tree("file", true), Fetched: a.str("fetched", true)}
		}),
		Complete:         o.flag("complete", true),
		IncompleteReason: o.str("incomplete_reason", false),
		State:            o.str("state", true),
		OpenedAt:         o.str("opened_at", true),
		ClosedAt:         o.str("closed_at", false),
		CreatedAt:        o.str("created_at", true),
		UpdatedAt:        o.str("updated_at", true),
		Writer:           o.tree("writer", true),
		Unknown:          o.tree("unknown", false),
	}
	if err := o.done(); err != nil {
		return nil, err
	}
	return r, nil
}

// Tree is the record as a tree, whole numbers as int64: the document the store
// holds the round as. It shares nothing with the Round.
func (r *Round) Tree() map[string]any {
	m := map[string]any{
		"schema": SchemaRound, "schema_version": RoundVersion,
		"record_revision": r.RecordRevision, "decision_round_id": r.DecisionRoundID,
		"origin": copyTree(r.Origin),
		"cards": each(r.Cards, func(c Card) map[string]any {
			cm := map[string]any{
				"card_id": c.CardID, "card_revision": c.CardRevision, "question_id": c.QuestionID,
				"question_revision": c.QuestionRevision, "held_task_id": c.HeldTaskID,
				"held_task_revision": c.HeldTaskRevision, "presented_at": c.PresentedAt, "state": c.State,
			}
			if c.AnswerSequence != 0 {
				cm["answer_sequence"] = c.AnswerSequence
			}
			return cm
		}),
		"attachments": each(r.Attachments, func(a Attachment) map[string]any {
			return map[string]any{"file": copyTree(a.File), "fetched": a.Fetched}
		}),
		"complete": r.Complete, "state": r.State, "opened_at": r.OpenedAt,
		"created_at": r.CreatedAt, "updated_at": r.UpdatedAt, "writer": copyTree(r.Writer),
	}
	r.Scope.into(m)
	put(m, "incomplete_reason", r.IncompleteReason)
	put(m, "closed_at", r.ClosedAt)
	if r.Unknown != nil {
		m["unknown"] = copyTree(r.Unknown)
	}
	return m
}

// Encode is the document of the record in canonical form.
func (r *Round) Encode() ([]byte, error) { return canon.Encode(r.Tree()) }
