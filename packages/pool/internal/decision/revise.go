package decision

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
)

// reviseQuestion writes the next revision of an open or deferred question,
// whole, and in the same transaction raises the presentation revision of its
// card and sets the card's question revision to the new one (spec/word.md,
// revise). The question keeps its identity, its round, its hold and its state,
// a deferral with its revisit date included; nothing is released and no answer
// changes.
func reviseQuestion(c *cli.Call) (*cli.Result, error) {
	if err := checkHandoff(c); err != nil {
		return nil, err
	}
	body := c.Request.Body
	questionID, _ := body["question_id"].(string)
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
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	switch q.State {
	case StateOpen, StateDeferred:
	case StateAnswered:
		return nil, refuse(codeQuestionAnswered, "a question with an applied answer cannot be revised; a superseding answer changes it")
	default:
		return nil, refuse(codeQuestionClosed, "a question that is released, completed or moot is final and takes no revision")
	}
	options := asList(body["options"])
	recommendation, _ := body["recommendation"].(string)
	if err := checkOptions(options, recommendation); err != nil {
		return nil, err
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
		return nil, malformed("decision round")
	}
	now, err := c.Stamp()
	if err != nil {
		return nil, internal("a revision cannot be given a time", err)
	}

	text, _ := body["text"].(string)
	consequence, _ := body["consequence"].(string)
	amount, _ := body["amount"].(string)
	askedAt, _ := body["asked_at"].(string)
	q.CurrentRevision++
	q.Revisions = append(q.Revisions, Revision{
		Revision: q.CurrentRevision, Text: text, Consequence: consequence, Recommendation: recommendation,
		Options: readOptions(options), Amount: amount, AskedAt: askedAt,
		AskedBy: asMap(copyTree(body["requested_by"])), RequestID: c.Request.RequestID,
	})
	q.UpdatedAt = now
	card.CardRevision++
	card.QuestionRevision = q.CurrentRevision
	round.UpdatedAt = now

	b := &builder{c: c}
	question, err := b.write("question", q.QuestionID, q.Tree(), l.Revision, q.Scope.envelope(), q.HeldTaskID, "pool.question.revised")
	if err != nil {
		return nil, err
	}
	finalRound, err := b.write("decision-round", round.DecisionRoundID, round.Tree(), rl.Revision, envelope.Scope{}, "", "")
	if err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"question": question, "round": finalRound})
}
