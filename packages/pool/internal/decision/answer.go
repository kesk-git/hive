package decision

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
)

// The operation answer-question of hive-pool, the third of the six the Mind's
// word persists through (spec/word.md, answer). It records one answer exactly
// as given and applies it in the same transaction, under the store's lock, and
// accepts only a handoff from hive-word. The package registers it from its init
// function, as it does hold-question and revise-question.

func init() { RegisterAnswers(entry.Default) }

// RegisterAnswers builds the operation answer-question of hive-pool into the
// registry.
func RegisterAnswers(r *entry.Registry) {
	r.RegisterOperation("hive-pool", operation("answer-question", answerQuestion,
		receiptAccepted, receiptSuperseded, receiptDeferred, receiptUnreconciled))
}

// answerQuestion reads the question, its card and its round and checks, in the
// order of the table of spec/word.md, the first that applies deciding; then it
// records the answer and applies it by its disposition.
func answerQuestion(c *cli.Call) (*cli.Result, error) {
	if err := checkHandoff(c); err != nil {
		return nil, err
	}
	body := c.Request.Body
	binding := asMap(body["card"])
	if err := checkExpected(c); err != nil {
		return nil, err
	}

	// The binding names a question of this home, its one card, its round, the
	// item it holds and a revision the question has.
	questionID, _ := binding["question_id"].(string)
	l, err := load(c, "question", questionID, "question")
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
	answered := intOf(binding["question_revision"])
	revision := revisionOf(q, answered)
	switch {
	case binding["card_id"] != q.CardID:
		return nil, notFound("card of this question")
	case binding["decision_round_id"] != q.DecisionRoundID:
		return nil, notFound("round of this question")
	case binding["held_task_id"] != q.HeldTaskID:
		return nil, notFound("item this question holds")
	case revision == nil:
		return nil, notFound("revision of this question")
	}
	rl, err := load(c, "decision-round", q.DecisionRoundID, "decision round")
	if err != nil {
		return nil, err
	}
	round, err := ParseRound(rl.Data)
	if err != nil {
		return nil, malformed("decision round").Because(err)
	}
	var card *Card
	for i := range round.Cards {
		if round.Cards[i].CardID == q.CardID {
			card = &round.Cards[i]
		}
	}
	if card == nil {
		return nil, notFound("card of this question")
	}
	hold, err := load(c, "hold", q.HoldID, "hold")
	if err != nil {
		return nil, err
	}

	// An answer to this question with the same source is recorded: the first
	// answer's, and nothing is written.
	source := asMap(body["source"])
	if first := answerWithSource(q, source); first != nil {
		return duplicate(first, l.Data, rl.Data, hold.Data)
	}

	optionID, _ := body["option_id"].(string)
	later := asMap(body["later"])
	supersedes, reconciles := intOf(body["supersedes"]), intOf(body["reconciles"])
	disposition := dispositionOf(q, answered, later != nil)

	switch {
	case !round.Complete:
		return nil, refuse(codeRoundIncomplete, "the round was not collected whole, so its cards take no answer")
	case optionID != "" && !revision.offers(optionID):
		return nil, envelope.Fail(envelope.Invalid, codeOptionUnknown, "the option is not one of the question revision answered")
	case disposition == dispositionUnreconciled:
		// Recorded unreconciled in any state, and applied to nothing.
	case q.State != StateOpen && q.State != StateDeferred && q.State != StateAnswered:
		return nil, refuse(codeQuestionClosed, "a question that is released, completed or moot takes no answer at its current revision")
	case intOf(binding["card_revision"]) != card.CardRevision:
		return nil, envelope.Fail(envelope.Conflict, codeCardStale, "the card changed what it shows and cannot submit")
	default:
		if err := checkAnswerExists(q, later != nil, supersedes); err != nil {
			return nil, err
		}
		if err := checkReferences(q, supersedes, reconciles); err != nil {
			return nil, err
		}
	}

	now, err := c.Stamp()
	if err != nil {
		return nil, internal("an answer cannot be given a time", err)
	}
	receiptID, err := ids.Mint(c.Clock, ids.Receipt)
	if err != nil {
		return nil, internal("a receipt cannot be given an identity", err)
	}

	// The answer, exactly as given, with its source, the caller that brought it
	// and the record revision that records it.
	text, _ := body["text"].(string)
	label, _ := body["option_label"].(string)
	answeredAt, _ := body["answered_at"].(string)
	a := Answer{
		Sequence: int64(len(q.Answers)) + 1, RecordRevision: l.Revision + 1, QuestionRevision: answered,
		Card: asMap(copyTree(binding)), OptionID: optionID, OptionLabel: label, Text: text,
		Source: asMap(copyTree(source)), AnsweredAt: answeredAt, RecordedAt: now, RecordedBy: asMap(copyTree(body["requested_by"])),
		Disposition: disposition, Supersedes: supersedes, Reconciles: reconciles,
		RequestID: c.Request.RequestID, ReceiptID: receiptID,
	}
	if later != nil {
		a.Later = &Later{RevisitAt: later["revisit_at"].(string)}
	}
	q.Answers = append(q.Answers, a)
	roundChanged := apply(q, card, round, a, now)
	q.UpdatedAt = now

	b := &builder{c: c}
	question, ref, err := b.writeQuestion(q, l.Revision)
	if err != nil {
		return nil, err
	}
	kind, event := receiptAccepted, eventAnswered
	switch disposition {
	case dispositionDeferred:
		kind, event = receiptDeferred, receiptDeferred
	case dispositionUnreconciled:
		kind = receiptUnreconciled
	}
	if _, err := b.answerReceipt(kind, receiptID, ref, q); err != nil {
		return nil, err
	}
	if disposition == dispositionAccepted && supersedes != 0 {
		if _, err := b.answerReceipt(receiptSuperseded, "", ref, q); err != nil {
			return nil, err
		}
	}
	if err := b.answerEvent(event, receiptID, q); err != nil {
		return nil, err
	}
	finalRound := rl.Data
	if roundChanged {
		round.RecordRevision = rl.Revision
		if finalRound, err = b.write("decision-round", round.DecisionRoundID, round.Tree(), rl.Revision, envelope.Scope{}, "", ""); err != nil {
			return nil, err
		}
	}
	return b.stage(map[string]any{
		"question": question, "round": finalRound, "hold": hold.Data,
		"answer_sequence": a.Sequence, "disposition": disposition, "duplicate": false,
	})
}

// duplicate is the result for an answer whose source is already recorded for
// the question: the first answer's sequence and disposition, the records as
// they stand, and nothing written.
func duplicate(first *Answer, question, round, hold map[string]any) (*cli.Result, error) {
	return &cli.Result{Body: map[string]any{
		"question": question, "round": round, "hold": hold,
		"answer_sequence": first.Sequence, "disposition": first.Disposition, "duplicate": true,
	}}, nil
}

// intOf is the whole number a member holds, or 0 where it holds none.
func intOf(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}
