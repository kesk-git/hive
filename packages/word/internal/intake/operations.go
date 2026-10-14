package intake

import (
	"fmt"
	"maps"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// The operations built here, each one handoff to the operation of hive-pool
// that writes the question (spec/word.md, Operations).
const (
	Hold     = "hold"
	Revise   = "revise"
	Release  = "release"
	Complete = "complete"
	Close    = "close"
)

// The command word hands off to, and the body version it sends.
const (
	poolCommand = "hive-pool"
	bodyVersion = "1.0"
)

// poolOperation is the hive-pool operation each operation hands off to.
var poolOperation = map[string]string{
	Hold: "hold-question", Revise: "revise-question", Release: "release-question",
	Complete: "complete-question", Close: "close-question",
}

// Operation is the operation of hive-word of the name, when it is one built
// here, or nil. It is supported, mutating, keeps no store and writes no receipt of
// its own: its receipts are the Pool's.
func Operation(name string) *cli.Operation {
	if _, ok := poolOperation[name]; !ok {
		return nil
	}
	return &cli.Operation{
		OperationDecl: envelope.OperationDecl{
			Name:           name,
			State:          envelope.Supported,
			Mode:           envelope.Unary,
			Mutates:        true,
			Versions:       []string{bodyVersion},
			RequestSchema:  fmt.Sprintf("urn:hive:schema:ops:hive-word.%s.request:v1", name),
			ResponseSchema: fmt.Sprintf("urn:hive:schema:ops:hive-word.%s.response:v1", name),
			ReceiptKinds:   []string{},
			Executes:       []string{},
			Calls:          []string{poolCommand},
		},
		Handle: handler(name),
		Adapt:  adapter(name),
	}
}

// handler is the handler of the operation: the caller is checked again, the
// identities it needs are minted, and the one handoff is sent. The Pool's
// refusals, conflicts and holds are returned as the Pool gave them, with its
// receipts.
func handler(name string) func(*cli.Call) (*cli.Result, error) {
	return func(c *cli.Call) (*cli.Result, error) {
		if !Authorized(name, c.Request.Caller) {
			return nil, refuse()
		}
		var body map[string]any
		var err error
		if name == Hold {
			body, err = holdBody(c)
		} else {
			body, err = relayBody(c, name)
		}
		if err != nil {
			return nil, err
		}
		resp, err := c.SendHandoff(cli.Handoff{
			Command: poolCommand, Operation: poolOperation[name], OperationVersion: bodyVersion,
			ExpectedRevisions: c.Request.ExpectedRevisions, Body: body,
		})
		if err != nil {
			return nil, err
		}
		out, err := c.Operation.Adapt(c, resp)
		if err != nil {
			return nil, err
		}
		return &cli.Result{Body: out}, nil
	}
}

// relayBody is the Pool's body for an operation that names a question: the
// request's own words, exactly as written, with the caller to record. A
// revision is asked at the request's issued_at.
func relayBody(c *cli.Call, name string) (map[string]any, error) {
	body := maps.Clone(c.Request.Body)
	body["requested_by"] = c.Request.Tree()["caller"]
	if name == Revise {
		body["asked_at"] = c.Request.IssuedAt
	}
	return body, nil
}

// holdBody is the Pool's body for a question raised: word mints the question,
// the card and, for a new round, the round, and sends the question's words
// exactly with the request's issued_at as its asked_at.
func holdBody(c *cli.Call) (map[string]any, error) {
	in := c.Request.Body
	questionID, err := ids.Mint(c.Clock, ids.Question)
	if err != nil {
		return nil, err
	}
	cardID, err := ids.Mint(c.Clock, ids.Card)
	if err != nil {
		return nil, err
	}
	question := map[string]any{"question_id": questionID, "asked_at": c.Request.IssuedAt}
	for _, key := range []string{"held_task_id", "new_item", "origin", "text", "consequence", "options", "recommendation", "amount"} {
		if v, ok := in[key]; ok {
			question[key] = v
		}
	}
	round := map[string]any{}
	given, _ := in["round"].(map[string]any)
	if id, ok := given["decision_round_id"]; ok {
		round["decision_round_id"], round["new"] = id, false
	} else {
		roundID, err := ids.Mint(c.Clock, ids.DecisionRound)
		if err != nil {
			return nil, err
		}
		round["decision_round_id"], round["new"] = roundID, true
		round["origin"] = in["origin"]
		for _, key := range []string{"origin", "attachments", "complete", "incomplete_reason"} {
			if v, ok := given[key]; ok {
				round[key] = v
			}
		}
	}
	body := map[string]any{
		"question": question, "round": round, "card_id": cardID,
		"requested_by": c.Request.Tree()["caller"],
	}
	if v, ok := in["hold_reason"]; ok {
		body["hold_reason"] = v
	}
	return body, nil
}

// adapter is how the Pool's outcome becomes the operation's own response
// (spec/word.md, How word persists through the Pool, Adapting the outcome):
// for hold the Pool's question, round, hold and item with the identities read
// from them, for every other operation the Pool's body unchanged.
func adapter(name string) func(*cli.Call, *envelope.Response) (map[string]any, error) {
	return func(_ *cli.Call, callee *envelope.Response) (map[string]any, error) {
		if name != Hold {
			return callee.Body, nil
		}
		return holdResponse(callee.Body)
	}
}

// holdResponse is the response of hold from the Pool's body.
func holdResponse(pool map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for _, key := range []string{"question", "round", "hold", "item"} {
		record, ok := pool[key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("intake: the Pool's body holds no %s", key)
		}
		out[key] = record
	}
	for _, id := range []struct{ name, record, field string }{
		{"question_id", "question", "question_id"},
		{"decision_round_id", "round", "decision_round_id"},
		{"card_id", "question", "card_id"},
		{"hold_id", "hold", "hold_id"},
	} {
		v, ok := out[id.record].(map[string]any)[id.field].(string)
		if !ok {
			return nil, fmt.Errorf("intake: the Pool's %s holds no %s", id.record, id.field)
		}
		out[id.name] = v
	}
	return out, nil
}
