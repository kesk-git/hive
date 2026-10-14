// Package poolfake is the fake hive-pool of the word package's tests
// (spec/word.md, What test-word proves). It is built from the declared
// schemas and nothing of the real Pool: it checks every request it is sent
// against the schema of the Pool operation, answers a read of kind
// request-outcome from the outcomes it recorded, keys the replay of a handoff
// on the causation's digest as spec/commands.md, A command with no store,
// says the Pool's harness does, refuses a request that is not a handoff from
// hive-word, and answers an operation with the valid response example of the
// declared schema, carrying the identities the request gave.
package poolfake

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// Operations are the hive-pool operations the handoffs reach, and for each the
// receipt kind its example answer carries and the example of the response
// schema it answers with.
var Operations = map[string]struct{ Receipt, Example string }{
	"hold-question":     {"pool.question.held", "work-item"},
	"revise-question":   {"pool.question.revised", "second-revision"},
	"release-question":  {"pool.question.released", "hold-released"},
	"complete-question": {"pool.question.completed", "hold-released"},
	"close-question":    {"pool.question.mooted", "moot-hold-released"},
	"answer-question":   {"pool.answer.accepted", "accepted"},
}

const (
	requestSchema  = "urn:hive:schema:ops:hive-pool.%s.request:v1"
	responseSchema = "urn:hive:schema:ops:hive-pool.%s.response:v1"
	// completedAt is the time every answer of the fake is completed at.
	completedAt = "2026-10-11T08:00:00.000Z"
)

// Fixtures is the directory of the contract's valid examples of the declared
// schemas when the fake runs where no repository is above its working
// directory, as a process started in a home does.
var Fixtures string

// Pool is the fake. The zero value answers every operation ok.
type Pool struct {
	mu sync.Mutex
	// Sent are the mutating requests the fake received, whole, in order,
	// replays and refusals among them.
	Sent []*envelope.Request
	// Reads are the request_ids read as outcomes, in order.
	Reads []string
	// Refuse, when it returns a failure for a request, is the answer to it
	// instead of the example, as a Pool that finds the records as they stand
	// would give. Such an answer records no outcome.
	Refuse func(req *envelope.Request) *envelope.Failure
	// Disposition, when set, is the disposition the fake gives an
	// answer-question request: accepted, deferred or unreconciled. Unset, an
	// answer is accepted.
	Disposition func(req *envelope.Request) string
	// Hide makes a read find nothing, as the read of a request that looked
	// before another reached the Pool.
	Hide bool
	// ReadErr, when set, is the Pool giving no answer to a read, and SendErr
	// the Pool giving none to a request.
	ReadErr, SendErr error

	outcomes map[string]recorded
}

// recorded is an outcome as the Pool keeps it: the digest it is keyed on and
// the response as it was given.
type recorded struct {
	digest   string
	response *envelope.Response
}

// ReadOutcome is the read of kind request-outcome.
func (p *Pool) ReadOutcome(c *cli.Call, requestID string) ([]byte, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Reads = append(p.Reads, requestID)
	if p.ReadErr != nil {
		return nil, false, p.ReadErr
	}
	rec, ok := p.outcomes[requestID]
	if !ok || p.Hide {
		return nil, false, nil
	}
	data, err := canon.Encode(map[string]any{
		"schema": "hive.request-outcome", "schema_version": "1.0",
		"request_id": requestID, "request_digest": rec.digest, "response": rec.response.Tree(),
	})
	return data, err == nil, err
}

// Send is a mutating request to the Pool.
func (p *Pool) Send(c *cli.Call, request []byte) ([]byte, int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	req, err := envelope.DecodeRequest(request)
	if err != nil {
		return nil, 0, err
	}
	p.Sent = append(p.Sent, req)
	if p.SendErr != nil {
		return nil, 0, p.SendErr
	}
	answer := func(r *envelope.Response) ([]byte, int, error) { return answer(req, r) }
	failed := func(f *envelope.Failure) ([]byte, int, error) { return failure(req, f) }
	op, ok := Operations[req.Operation]
	if req.Command != "hive-pool" || !ok {
		return failed(envelope.Common(envelope.CodeUnknownOperation, "the fake Pool has no such operation"))
	}
	if err := envelope.ValidateBody(req.Body, fmt.Sprintf(requestSchema, req.Operation)); err != nil {
		return failed(envelope.AsFailure(err))
	}
	// A request that is not a handoff from hive-word is refused, and the
	// causation's digest is what the replay is keyed on.
	cause := req.Causation
	if req.Caller.Kind != envelope.CallerPackage || req.Caller.Package != "word" || req.Caller.Command != "hive-word" ||
		cause == nil || cause.Schema != envelope.RequestSchema || cause.ID != req.RequestID || cause.Digest == "" {
		return failed(envelope.Common(envelope.CodeAuthorityInsufficient, "the operation takes only a handoff from hive-word"))
	}
	if rec, ok := p.outcomes[req.RequestID]; ok {
		if rec.digest != cause.Digest {
			return failed(envelope.Common(envelope.CodeReplayMismatch, "the request_id was used before with another request"))
		}
		replay := *rec.response
		replay.Replayed = true
		return answer(&replay)
	}
	if p.Refuse != nil {
		if f := p.Refuse(req); f != nil {
			return failed(f)
		}
	}
	body, err := example(req)
	if err != nil {
		return nil, 0, err
	}
	if req.Operation == "answer-question" {
		disposition := "accepted"
		if p.Disposition != nil {
			disposition = p.Disposition(req)
		}
		body["disposition"], body["duplicate"] = disposition, false
	}
	if err := envelope.ValidateBody(body, fmt.Sprintf(responseSchema, req.Operation)); err != nil {
		return nil, 0, err
	}
	kind := op.Receipt
	if req.Operation == "answer-question" {
		kind = answerReceipt[body["disposition"].(string)]
	}
	receipt, err := Receipt(req, kind)
	if err != nil {
		return nil, 0, err
	}
	resp := &envelope.Response{Status: envelope.Ok, Body: body, Receipts: []envelope.Receipt{receipt}}
	out, exit, err := answer(resp)
	if err != nil {
		return nil, 0, err
	}
	if p.outcomes == nil {
		p.outcomes = map[string]recorded{}
	}
	p.outcomes[req.RequestID] = recorded{cause.Digest, resp}
	return out, exit, nil
}

// answerReceipt is the receipt kind the Pool writes for an answer of each
// disposition.
var answerReceipt = map[string]string{
	"accepted": "pool.answer.accepted", "deferred": "pool.question.deferred", "unreconciled": "pool.answer.unreconciled",
}

// answer is the response to the request, completed as the Pool completes one,
// encoded, with the exit code its status fixes.
func answer(req *envelope.Request, r *envelope.Response) ([]byte, int, error) {
	r.RequestID, r.Command, r.Operation = req.RequestID, req.Command, req.Operation
	r.Responder = envelope.Responder{Package: "pool", Command: "hive-pool", ImplementationVersion: "0.1.0-fake", ContractVersion: envelope.ContractVersion}
	r.CompletedAt = completedAt
	if r.Receipts == nil {
		r.Receipts = []envelope.Receipt{}
	}
	data, err := r.Encode()
	return data, r.Status.ExitCode(), err
}

// failure is the response of the failure to the request.
func failure(req *envelope.Request, f *envelope.Failure) ([]byte, int, error) {
	return answer(req, &envelope.Response{Status: f.Status, Error: &f.Err, Receipts: f.Receipts})
}

// Serve is the fake as a process: the operation of hive-pool given the
// request on its standard input, answered as the document and exit code the
// process writes. It keeps nothing between runs, so a read of the recorded
// outcome of a request, sent under that request's own request_id, finds none;
// any other read is invalid. An operation is a handoff answered with the
// example of its schema.
func (p *Pool) Serve(operation string, request []byte) ([]byte, int, error) {
	req, err := envelope.DecodeRequest(request)
	if err != nil {
		return nil, 0, err
	}
	if operation == "read" {
		selector, _ := req.Body["selector"].(map[string]any)
		if req.Body["kind"] != "request-outcome" || selector["id"] != req.RequestID {
			return failure(req, envelope.Common(envelope.CodeSchemaViolation, "the fake Pool reads only the outcome of the request it is sent under"))
		}
		return failure(req, envelope.Common(envelope.CodeNotFound, "the Pool records no outcome of the request"))
	}
	return p.Send(nil, request)
}

// Count is the number of mutating requests received.
func (p *Pool) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.Sent)
}

// Receipt is a receipt of the kind that the Pool writes for the request.
func Receipt(req *envelope.Request, kind string) (envelope.Receipt, error) {
	id, err := ids.Mint(clock.System{}, ids.Receipt)
	if err != nil {
		return envelope.Receipt{}, err
	}
	return envelope.Receipt{
		ReceiptID: id, Kind: kind, KindVersion: "1.0", WrittenAt: completedAt,
		Writer:    envelope.Writer{Package: "pool", Command: "hive-pool", Operation: req.Operation, ImplementationVersion: "0.1.0-fake"},
		RequestID: req.RequestID, CorrelationID: req.CorrelationID, Scope: req.Scope,
		Authority: envelope.Authority{Kind: envelope.AuthorityNone},
		Evidence:  []envelope.Evidence{}, Body: map[string]any{},
	}, nil
}

// example is the valid response example of the operation's declared schema,
// carrying the question, round and card identities the request gave.
func example(req *envelope.Request) (map[string]any, error) {
	name := Operations[req.Operation].Example
	path := filepath.Join(fixtures(), fmt.Sprintf("ops_hive-pool.%s.response", req.Operation), "valid", name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("poolfake: the example of the declared schema: %w", err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		return nil, err
	}
	body := v.(map[string]any)
	question, _ := body["question"].(map[string]any)
	round, _ := body["round"].(map[string]any)
	var cards []any
	if round != nil {
		cards, _ = round["cards"].([]any)
	}
	var card map[string]any
	if len(cards) > 0 {
		card, _ = cards[0].(map[string]any)
	}
	if req.Operation == "hold-question" {
		q := req.Body["question"].(map[string]any)
		r := req.Body["round"].(map[string]any)
		question["question_id"], question["card_id"] = q["question_id"], req.Body["card_id"]
		question["decision_round_id"], round["decision_round_id"] = r["decision_round_id"], r["decision_round_id"]
		if card != nil {
			card["card_id"], card["question_id"] = req.Body["card_id"], q["question_id"]
		}
		return body, nil
	}
	id := req.Body["question_id"]
	if req.Operation == "answer-question" {
		id = req.Body["card"].(map[string]any)["question_id"]
	}
	question["question_id"] = id
	if card != nil {
		card["question_id"] = id
	}
	return body, nil
}

// fixtures is the directory of the contract's valid examples of the declared
// schemas, found above the working directory of the test.
func fixtures() string {
	if Fixtures != "" {
		return Fixtures
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "contract", "testdata", "schemas")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
