package decision

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/pool/internal/read"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// What release-question, complete-question and close-question share
// (spec/word.md, release and complete, close): the question and the records it
// is settled against, read under the store's lock; the work its hold gates, as
// it stands in the transaction; and the release of the hold with the receipt
// that settled the question as its released_by.

// The codes of the Pool that these three operations return, beside those of
// hold.go, and the words their records and receipts name things by.
const (
	codeQuestionUnanswered = "pool.question_unanswered"
	codeHoldsWork          = "pool.question_holds_work"
	codeDecisionItem       = "pool.decision_item"
	codeNotMoot            = "pool.question_not_moot"

	deliverableDecision = "decision"
	holdStateReleased   = "released"
	receiptHoldReleased = "pool.hold.released"
	roleDisposition     = "disposition"
)

// settling is a question being settled by one of the three operations, with the
// records it is settled against.
type settling struct {
	c        *cli.Call
	question *poolstore.Loaded
	q        *Question
	round    *poolstore.Loaded
	r        *Round
	card     *Card
	hold     *poolstore.Loaded
}

// readQuestion reads the question the request names, which must exist, lie
// within the request's scope and stand at the revisions the request expects. It
// is made after the check of the handoff and before any other record is read.
func readQuestion(c *cli.Call) (*settling, error) {
	id, _ := c.Request.Body["question_id"].(string)
	l, err := load(c, "question", id, "question")
	if err != nil {
		return nil, err
	}
	q, err := ParseQuestion(l.Data)
	if err != nil {
		return nil, malformed("question").Because(err)
	}
	if err := checkScope(c, q.Scope); err != nil {
		return nil, err
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	return &settling{c: c, question: l, q: q}, nil
}

// readRecords reads the round of the question, its card and its hold, which
// must each exist and be readable, and the hold active: a question that is
// open, deferred or answered keeps its word hold.
func (s *settling) readRecords() error {
	rl, err := load(s.c, "decision-round", s.q.DecisionRoundID, "decision round")
	if err != nil {
		return err
	}
	round, err := ParseRound(rl.Data)
	if err != nil {
		return malformed("decision round").Because(err)
	}
	for i := range round.Cards {
		if round.Cards[i].CardID == s.q.CardID {
			s.card = &round.Cards[i]
		}
	}
	if s.card == nil {
		return malformed("decision round")
	}
	hold, err := load(s.c, "hold", s.q.HoldID, "hold")
	if err != nil {
		return err
	}
	if hold.Data["kind"] != holdKindWord || hold.Data["state"] != holdStateActive || hold.Data["task_id"] != s.q.HeldTaskID {
		return malformed("hold")
	}
	s.round, s.r, s.hold = rl, round, hold
	return nil
}

// items reads the items and holds of the Pool as the transaction sees them.
func (s *settling) items() (*read.Graph, error) {
	g, omitted, err := read.LoadGraph(s.c.Store, s.c.Clock.Now())
	if err != nil {
		return nil, poolstore.Failure("the items", err)
	}
	if len(omitted) > 0 {
		return nil, malformed("item " + omitted[0].ID)
	}
	return g, nil
}

// liveWork reports whether the item is live work: neither archived nor a
// decision item (deliverable decision). An item made as a decision item and
// since given work is work.
func liveWork(n *read.Node) bool {
	return n.State != itemStateArchived && n.Data["deliverable"] != deliverableDecision
}

// work is what the held item of a question gates, as it stands.
type work struct {
	// decisionItem is true where the held item is a decision item.
	decisionItem bool
	// held is true where the held item is live work, and beneath where an item
	// beneath it is.
	held, beneath bool
}

// workOf reads the held item and every item beneath it.
func (s *settling) workOf() (work, error) {
	g, err := s.items()
	if err != nil {
		return work{}, err
	}
	held := g.Items[s.q.HeldTaskID]
	if held == nil {
		return work{}, malformed("item the question holds")
	}
	w := work{decisionItem: held.Data["deliverable"] == deliverableDecision, held: liveWork(held)}
	for _, id := range g.Beneath(held.ID) {
		if liveWork(g.Items[id]) {
			w.beneath = true
		}
	}
	return w, nil
}

// finish writes the question as the operation changed it, with the receipt and
// the event of the kind that settle it under the identity the record already
// names, the round if the operation changed it, and the release of the
// question's hold with that receipt as its released_by, in one transaction.
func (s *settling) finish(kind, receiptID string, round *Round) (*cli.Result, error) {
	b := &builder{c: s.c}
	question, ref, err := b.writeQuestion(s.q, s.question.Revision)
	if err != nil {
		return nil, err
	}
	if _, err := b.answerReceipt(kind, receiptID, ref, s.q); err != nil {
		return nil, err
	}
	if err := b.answerEvent(kind, receiptID, s.q); err != nil {
		return nil, err
	}
	roundRecord := s.round.Data
	if round != nil {
		round.RecordRevision = s.round.Revision
		if roundRecord, err = b.write("decision-round", round.DecisionRoundID, round.Tree(), s.round.Revision, envelope.Scope{}, "", ""); err != nil {
			return nil, err
		}
	}
	hold, err := b.releaseHold(s, receiptID)
	if err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"question": question, "round": roundRecord, "hold": hold})
}

// releaseHold writes the release of the question's word hold at the revision it
// was read: released at the time of the request, with the receipt that settled
// the question as its released_by. The hold's own receipt, pool.hold.released,
// names that receipt as the evidence the hold was released on.
func (b *builder) releaseHold(s *settling, settledID string) (map[string]any, error) {
	now, err := b.c.Stamp()
	if err != nil {
		return nil, internal("a release cannot be given a time", err)
	}
	next := asMap(copyTree(s.hold.Data))
	next["state"], next["released_at"], next["updated_at"] = holdStateReleased, now, now
	next["released_by"] = map[string]any{"schema": schemaReceipt, "id": settledID}
	final, err := b.write("hold", s.q.HoldID, next, s.hold.Revision, scopeOf(next).envelope(), s.q.HeldTaskID, receiptHoldReleased)
	if err != nil {
		return nil, err
	}
	// write added the hold's receipt last.
	last := &b.tx.Receipts[len(b.tx.Receipts)-1]
	last.Evidence = append(last.Evidence, envelope.Evidence{Role: roleDisposition, Ref: envelope.RecordRef{Schema: schemaReceipt, ID: settledID}})
	return final, nil
}

// newReceiptID mints the identity of the receipt that settles a question, which
// the question's record holds before the receipt is made.
func newReceiptID(c *cli.Call) (string, error) {
	id, err := ids.Mint(c.Clock, ids.Receipt)
	if err != nil {
		return "", internal("a receipt cannot be given an identity", err)
	}
	return id, nil
}

// how is one of the two dispositions of an answered question: the state the
// question comes to, the receipt that establishes it, and what it refuses of
// the work the question gates.
type how struct {
	state, receipt string
	refuse         func(work) error
}

// dispose records the owner's disposition of the applied answer of a question
// and releases its hold in the same transaction (spec/word.md, release and
// complete). A question with no applied answer has no disposition to record,
// and one already released, completed or moot is final.
func dispose(c *cli.Call, d how) (*cli.Result, error) {
	if err := checkHandoff(c); err != nil {
		return nil, err
	}
	s, err := readQuestion(c)
	if err != nil {
		return nil, err
	}
	switch s.q.State {
	case StateAnswered:
	case StateOpen, StateDeferred:
		return nil, refuse(codeQuestionUnanswered, "a question with no applied answer has no disposition to record")
	default:
		return nil, refuse(codeQuestionClosed, "a question that is released, completed or moot takes no disposition")
	}
	if s.q.Applied == nil {
		return nil, malformed("question")
	}
	if err := s.readRecords(); err != nil {
		return nil, err
	}
	w, err := s.workOf()
	if err != nil {
		return nil, err
	}
	if err := d.refuse(w); err != nil {
		return nil, err
	}

	now, err := c.Stamp()
	if err != nil {
		return nil, internal("a disposition cannot be given a time", err)
	}
	receiptID, err := newReceiptID(c)
	if err != nil {
		return nil, err
	}
	body := c.Request.Body
	note, _ := body["note"].(string)
	var evidence []map[string]any
	if list, ok := body["evidence"].([]any); ok {
		evidence = []map[string]any{}
		for _, e := range list {
			evidence = append(evidence, asMap(copyTree(e)))
		}
	}
	s.q.State = d.state
	s.q.Disposition = &Disposition{
		Kind: d.state, AnswerSequence: s.q.Applied.AnswerSequence, Note: note, Evidence: evidence,
		At: now, By: asMap(copyTree(body["requested_by"])), RequestID: c.Request.RequestID, ReceiptID: receiptID,
	}
	s.q.UpdatedAt = now
	return s.finish(d.receipt, receiptID, nil)
}
