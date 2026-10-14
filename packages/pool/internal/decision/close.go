package decision

import (
	"errors"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
	"github.com/kesk-git/hive/packages/pool/internal/read"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The operation close-question of hive-pool, the last of the six the Mind's
// word persists through (spec/word.md, close). It closes an open or deferred
// question as moot, on the owner's reason and evidence it checks as the Pool
// stands, never as an answer, and releases the question's word hold with the
// close's receipt. It accepts only a handoff from hive-word. The package
// registers it from its init function, as it does the others.

func init() { RegisterClose(entry.Default) }

// RegisterClose builds the operation close-question of hive-pool into the
// registry.
func RegisterClose(r *entry.Registry) {
	r.RegisterOperation("hive-pool", operation("close-question", closeQuestion, "pool.question.mooted", receiptHoldReleased))
}

// The kinds of evidence that a question is moot, and the receipt of the close.
const (
	evidenceItemArchived     = "item-archived"
	evidenceQuestionReplaced = "question-replaced"
	receiptMooted            = "pool.question.mooted"
)

// closeQuestion makes the question moot: its card becomes moot, its round
// closes when no card of it is open, and its hold is released. A question with
// an applied answer has the Hive Mind's word and its disposition is the
// owner's; one released, completed or moot is final. Each piece of evidence
// must hold when the Pool checks it, else nothing is written.
func closeQuestion(c *cli.Call) (*cli.Result, error) {
	if err := checkHandoff(c); err != nil {
		return nil, err
	}
	s, err := readQuestion(c)
	if err != nil {
		return nil, err
	}
	switch s.q.State {
	case StateOpen, StateDeferred:
	case StateAnswered:
		return nil, refuse(codeQuestionAnswered, "a question with an applied answer has the Hive Mind's word, and its disposition is the owner's")
	default:
		return nil, refuse(codeQuestionClosed, "a question that is released, completed or moot takes no close")
	}
	if err := s.readRecords(); err != nil {
		return nil, err
	}

	body := c.Request.Body
	var closure []MootEvidence
	var graph *read.Graph
	for _, v := range asList(body["evidence"]) {
		e := asMap(v)
		kind, _ := e["kind"].(string)
		ref := asMap(e["ref"])
		var holds bool
		switch kind {
		case evidenceItemArchived:
			if graph == nil {
				if graph, err = s.items(); err != nil {
					return nil, err
				}
			}
			holds = s.itemArchived(graph, ref)
		case evidenceQuestionReplaced:
			if holds, err = s.questionReplaced(ref); err != nil {
				return nil, err
			}
		}
		if !holds {
			return nil, refuse(codeNotMoot, "the evidence offered for the close does not hold")
		}
		closure = append(closure, MootEvidence{Kind: kind, Ref: asMap(copyTree(ref))})
	}
	if len(closure) == 0 {
		return nil, refuse(codeNotMoot, "a close rests on at least one piece of evidence")
	}

	now, err := c.Stamp()
	if err != nil {
		return nil, internal("a close cannot be given a time", err)
	}
	receiptID, err := newReceiptID(c)
	if err != nil {
		return nil, err
	}
	reason, _ := body["reason"].(string)
	q := s.q
	q.State, q.Deferral = StateMoot, nil
	q.Closure = &Closure{
		Reason: reason, Evidence: closure, At: now, By: asMap(copyTree(body["requested_by"])),
		RequestID: c.Request.RequestID, ReceiptID: receiptID,
	}
	q.UpdatedAt = now

	s.card.State = CardMoot
	s.r.UpdatedAt = now
	if s.r.State != RoundClosed && !anyOpen(s.r) {
		s.r.State, s.r.ClosedAt = RoundClosed, now
	}
	return s.finish(receiptMooted, receiptID, s.r)
}

// itemArchived reports whether the evidence item-archived holds: the reference
// names the held item, and it and every item beneath it are archived, so that
// nothing is left for the hold to gate.
func (s *settling) itemArchived(g *read.Graph, ref map[string]any) bool {
	if ref["schema"] != "pool.item" || ref["id"] != s.q.HeldTaskID || !s.ofThisHome(ref) {
		return false
	}
	held := g.Items[s.q.HeldTaskID]
	if held == nil || held.State != itemStateArchived {
		return false
	}
	for _, id := range g.Beneath(held.ID) {
		if g.Items[id].State != itemStateArchived {
			return false
		}
	}
	return true
}

// questionReplaced reports whether the evidence question-replaced holds: the
// reference names another question of this home that is not moot and holds the
// same item with its own word hold active, so that the item stays held on it.
func (s *settling) questionReplaced(ref map[string]any) (bool, error) {
	id, _ := ref["id"].(string)
	if ref["schema"] != SchemaQuestion || id == s.q.QuestionID || !s.ofThisHome(ref) {
		return false, nil
	}
	other, om, err := poolstore.Load(s.c.Store, "question", id)
	switch {
	case errors.Is(err, poolstore.ErrNotFound), om != nil:
		return false, nil
	case err != nil:
		return false, poolstore.Failure("the question that replaces this one", err)
	}
	q, err := ParseQuestion(other.Data)
	if err != nil || q.State == StateMoot || q.HeldTaskID != s.q.HeldTaskID || q.HoldID == s.q.HoldID {
		return false, nil
	}
	hold, om, err := poolstore.Load(s.c.Store, "hold", q.HoldID)
	switch {
	case errors.Is(err, poolstore.ErrNotFound), om != nil:
		return false, nil
	case err != nil:
		return false, poolstore.Failure("the hold of the question that replaces this one", err)
	}
	return hold.Data["kind"] == holdKindWord && hold.Data["state"] == holdStateActive && hold.Data["task_id"] == q.HeldTaskID, nil
}

// ofThisHome reports whether a reference that names a home names this one.
func (s *settling) ofThisHome(ref map[string]any) bool {
	home, has := ref["home_id"].(string)
	return !has || home == s.c.Home.HomeID
}
