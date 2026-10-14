package intake

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/schema"
	"github.com/kesk-git/hive/contract/store"
)

// Answer is the one intake for an answer: one handoff to hive-pool
// answer-question, after the check of who brings it and of the source it
// rests on (spec/word.md, Operations, answer).
const Answer = "answer"

// The kinds of source an answer rests on.
const (
	reviewSubmission = "review-submission"
	operatorRequest  = "operator-request"
)

// questionChanged is the code of an answer recorded unreconciled, returned as
// held (spec/word.md, Error codes).
const questionChanged = "word.question_changed"

// feedbackSchema is the schema of a review round's feedback file.
const feedbackSchema = "urn:hive:schema:review-feedback:v1"

// AnswerOperation is the operation answer of hive-word: supported, mutating,
// keeping no store and writing no receipt of its own, as the other operations
// that hand off once. Only a supervisor or the operator may bring an answer.
func AnswerOperation() *cli.Operation {
	return &cli.Operation{
		OperationDecl: envelope.OperationDecl{
			Name:           Answer,
			State:          envelope.Supported,
			Mode:           envelope.Unary,
			Mutates:        true,
			Versions:       []string{bodyVersion},
			RequestSchema:  fmt.Sprintf("urn:hive:schema:ops:hive-word.%s.request:v1", Answer),
			ResponseSchema: fmt.Sprintf("urn:hive:schema:ops:hive-word.%s.response:v1", Answer),
			ReceiptKinds:   []string{},
			Executes:       []string{},
			Calls:          []string{poolCommand},
		},
		Handle: answer,
		Adapt:  adaptAnswer,
	}
}

// sourceOf is the kind and the members of the source of the answer.
func sourceOf(c *cli.Call) (string, map[string]any) {
	source, _ := c.Request.Body["source"].(map[string]any)
	kind, _ := source["kind"].(string)
	return kind, source
}

// bringsOperatorRequest reports whether the caller may bring the source: an
// operator request is the operator's, and no other caller's.
func bringsOperatorRequest(c *cli.Call) bool {
	kind, _ := sourceOf(c)
	return kind != operatorRequest || c.Request.Caller.Kind == envelope.CallerOperator
}

// GuardAnswer makes the callee refuse, with word.not_authorized and before
// anything is read from or sent to the Pool, an answer whose source is an
// operator request brought by a caller that is not the operator.
func GuardAnswer(next cli.Callee) cli.Callee { return answerGuarded{next} }

type answerGuarded struct{ cli.Callee }

func (g answerGuarded) ReadOutcome(c *cli.Call, requestID string) ([]byte, bool, error) {
	if c.Operation.Name == Answer && !bringsOperatorRequest(c) {
		return nil, false, refuse()
	}
	return g.Callee.ReadOutcome(c, requestID)
}

// answer checks the caller and the source of the answer, and sends the one
// handoff. The answer's words go to the Pool exactly as they were written.
func answer(c *cli.Call) (*cli.Result, error) {
	if !Authorized(Answer, c.Request.Caller) || !bringsOperatorRequest(c) {
		return nil, refuse()
	}
	var err error
	switch kind, source := sourceOf(c); kind {
	case operatorRequest:
		if source["request_id"] != c.Request.RequestID {
			return nil, envelope.Common(envelope.CodeSchemaViolation, "an operator request names the request_id of this request")
		}
	case reviewSubmission:
		err = checkReviewSubmission(c, source)
	}
	if err != nil {
		return nil, err
	}
	body, err := relayBody(c, Answer)
	if err != nil {
		return nil, err
	}
	resp, err := c.SendHandoff(cli.Handoff{
		Command: poolCommand, Operation: "answer-question", OperationVersion: bodyVersion,
		ExpectedRevisions: c.Request.ExpectedRevisions, Body: body,
	})
	if err != nil {
		return nil, err
	}
	out, err := adaptAnswer(c, resp)
	if err != nil {
		return nil, err
	}
	return &cli.Result{Body: out}, nil
}

// poolAnswerResponse is the schema of the body of the Pool's answer-question
// response.
const poolAnswerResponse = "urn:hive:schema:ops:hive-pool.answer-question.response:v1"

// adaptAnswer is the Pool's body, except for an outcome whose disposition is
// unreconciled, which is returned as held: no body, the hold in error.hold,
// and the code word.question_changed. That outcome is read only once the body
// is held to the Pool's response schema; a body that breaks it is an error,
// never a recorded answer and hold.
func adaptAnswer(_ *cli.Call, callee *envelope.Response) (map[string]any, error) {
	if callee.Body["disposition"] != "unreconciled" {
		return callee.Body, nil
	}
	if err := envelope.ValidateBody(callee.Body, poolAnswerResponse); err != nil {
		return nil, fmt.Errorf("intake: the Pool's body is not its answer-question response: %v", err)
	}
	hold := callee.Body["hold"].(map[string]any)
	id := hold["hold_id"].(string)
	record, err := canon.Encode(hold)
	if err != nil {
		return nil, err
	}
	digest, err := canon.RecordDigest(record)
	if err != nil {
		return nil, err
	}
	ref := envelope.RecordRef{Schema: "pool.hold", ID: id, Digest: digest}
	ref.HomeID, _ = hold["home_id"].(string)
	ref.RecordRevision, _ = hold["record_revision"].(int64)
	f := envelope.Hold(ref, "the question changed after the answer was given: the answer is recorded unreconciled and applies to nothing")
	f.Err.Code = questionChanged
	return nil, f
}

// roundFile is the path below the home of the feedback file of the round a
// review submission names (spec/review.md, The directory).
func roundFile(source map[string]any) (string, error) {
	task, ok1 := source["task_id"].(string)
	generation, ok2 := source["generation"].(int64)
	review, ok3 := source["review_id"].(string)
	round, ok4 := source["round_id"].(int64)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return "", errors.New("intake: the source of a review submission names no round")
	}
	return fmt.Sprintf("reviews/%s/%d/%s/round-%04d.feedback.json", task, generation, review, round), nil
}

// insufficient is the refusal of a review submission the round file does not
// carry. Its message names what is missing and never repeats a word of the
// answer.
func insufficient(what string) *envelope.Failure {
	return envelope.Common(envelope.CodeAuthorityInsufficient, "the review round file "+what)
}

// checkReviewSubmission reads the round file through the schema-validated
// reading of the feedback file and checks that it holds the submit event the
// source names, and that the submission carries this card's answer with the
// same binding, option, label, text and Later. Anything else is
// authority_insufficient.
func checkReviewSubmission(c *cli.Call, source map[string]any) error {
	rel, err := roundFile(source)
	if err != nil {
		return err
	}
	file, err := readRound(c, rel)
	if errors.Is(err, fs.ErrNotExist) {
		return insufficient("is not there")
	}
	if err != nil {
		return insufficient("cannot be read as a feedback file").Because(err)
	}
	for _, key := range []string{"task_id", "generation", "review_id", "round_id"} {
		if file[key] != source[key] {
			return insufficient("is not the file of the round the source names")
		}
	}
	event := submitEvent(file, source["feedback_event_id"])
	if event == nil {
		return insufficient("holds no such submit event")
	}
	submission, _ := event["submission"].(map[string]any)
	answers, _ := submission["answers"].([]any)
	card, _ := c.Request.Body["card"].(map[string]any)
	found := false
	for _, a := range answers {
		entry, _ := a.(map[string]any)
		given, _ := entry["card"].(map[string]any)
		if given["card_id"] != card["card_id"] {
			continue
		}
		if !carries(c.Request.Body, entry) {
			return insufficient("carries another answer of this card")
		}
		found = true
	}
	if !found {
		return insufficient("carries no answer of this card")
	}
	return nil
}

// readRound is the feedback file at rel below the home, read as the contract
// library reads it: through the home's resolved path, and held to the
// feedback schema before any of it is used.
func readRound(c *cli.Call, rel string) (map[string]any, error) {
	path, err := c.Home.Path(rel)
	if err != nil {
		return nil, err
	}
	data, err := store.ReadFile(path)
	if err != nil {
		return nil, err
	}
	reg, err := schema.Embedded()
	if err != nil {
		return nil, err
	}
	if err := reg.Validate(feedbackSchema, data); err != nil {
		return nil, err
	}
	v, err := canon.Parse(data)
	if err != nil {
		return nil, err
	}
	file, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("intake: a feedback file is an object")
	}
	return file, nil
}

// submitEvent is the event of the file of the id, when it is a submit event.
func submitEvent(file map[string]any, id any) map[string]any {
	events, _ := file["events"].([]any)
	for _, e := range events {
		event, _ := e.(map[string]any)
		if event["event_id"] == id && event["kind"] == "submit" {
			return event
		}
	}
	return nil
}

// carries reports whether the answer entry of a submission is the answer the
// request brings: the same binding, option, label, text and Later. A member
// the entry leaves out is absent, and for the text it is empty.
func carries(request, entry map[string]any) bool {
	text, _ := entry["answer_text"].(string)
	asked, _ := request["text"].(string)
	return text == asked &&
		sameValue(request["card"], entry["card"]) &&
		sameValue(request["option_id"], entry["option_id"]) &&
		sameValue(request["option_label"], entry["option_label"]) &&
		sameValue(request["later"], entry["later"])
}

// sameValue reports whether two members, either of them possibly absent, are
// the same document.
func sameValue(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	x, err := canon.Encode(a)
	if err != nil {
		return false
	}
	y, err := canon.Encode(b)
	return err == nil && string(x) == string(y)
}
