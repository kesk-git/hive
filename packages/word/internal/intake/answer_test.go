package intake_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/word/internal/poolfake"
)

// The acceptance of spec/word.md, Operations, answer, and What test-word
// proves, for the answer: one handoff to hive-pool answer-question after the
// check of who brings it and of the source it rests on, against the fake
// hive-pool and the round files it holds.

// scene is one card, answered on a review round of the rig's home.
type scene struct {
	r     *rig
	card  map[string]any
	round poolfake.Round
	event string
}

func newScene(t testing.TB) *scene {
	t.Helper()
	r := newRig(t)
	return &scene{
		r: r,
		card: map[string]any{
			"card_id": mint(t, ids.Card), "card_revision": int64(2),
			"question_id": mint(t, ids.Question), "question_revision": int64(1),
			"held_task_id": mint(t, ids.Task), "held_task_revision": int64(3),
			"decision_round_id": mint(t, ids.DecisionRound), "decision_round_revision": int64(1),
			"owner_home_id": r.homeID, "owner_task_id": mint(t, ids.Task),
		},
		round: poolfake.Round{
			HomeID: r.homeID, WorkspaceID: r.workspaceID, TaskID: mint(t, ids.Task), Generation: 1,
			ReviewID: mint(t, ids.Review), RoundID: 1,
		},
		event: mint(t, ids.FeedbackEvent),
	}
}

// later is Later with its revisit date.
func later(at string) map[string]any { return map[string]any{"revisit_at": at} }

// typed is the answer as the Hive Mind left it, in the words of a request: an
// option with its label and the text typed with it.
func (s *scene) typed() map[string]any {
	return map[string]any{"option_id": "yes", "option_label": "Yes", "text": words}
}

// body is the body of a hive-word answer request: the card, the words given
// and the source.
func (s *scene) body(words, source map[string]any) map[string]any {
	b := map[string]any{"card": s.card, "text": "", "source": source, "answered_at": "2026-10-11T08:02:00.000Z"}
	for k, v := range words {
		b[k] = v
	}
	return b
}

// reviewSource is the source naming the submit event of the scene's round.
func (s *scene) reviewSource() map[string]any {
	return map[string]any{
		"kind": "review-submission", "task_id": s.round.TaskID, "generation": s.round.Generation,
		"review_id": s.round.ReviewID, "round_id": s.round.RoundID, "feedback_event_id": s.event,
	}
}

// entry is the answer the file carries for the words of a request: the same
// card, option, label, text and Later.
func (s *scene) entry(words map[string]any) map[string]any {
	e := map[string]any{"card": s.card}
	for _, k := range []string{"option_id", "option_label", "later"} {
		if v, ok := words[k]; ok {
			e[k] = v
		}
	}
	if text, _ := words["text"].(string); text != "" {
		e["answer_text"] = text
	}
	return e
}

// submit writes the round file holding the submit event with the answers.
func (s *scene) submit(answers ...map[string]any) {
	s.r.t.Helper()
	s.write(poolfake.Event(s.event, 1, "submit", poolfake.Submission(mint(s.r.t, ids.Request), answers...)))
}

func (s *scene) write(events ...map[string]any) {
	s.r.t.Helper()
	s.round.Events = events
	s.r.write(s.round.Path(), s.round.Tree())
}

// send sends the answer by the caller under a new request_id, or the one
// given.
func (s *scene) send(caller, body map[string]any, requestID ...string) (*envelope.Response, int, []byte) {
	s.r.t.Helper()
	id := mint(s.r.t, ids.Request)
	if len(requestID) > 0 {
		id = requestID[0]
	}
	req := s.request(id, caller, body)
	resp, exit := s.r.send("answer", req)
	return resp, exit, req
}

// request is the encoded answer request. An answer names no expected
// revision: its card binding carries the revisions the Hive Mind saw.
func (s *scene) request(requestID string, caller, body map[string]any) []byte {
	s.r.t.Helper()
	out, err := canon.Encode(map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": "hive-word", "operation": "answer", "operation_version": "1.0",
		"request_id": requestID, "correlation_id": mint(s.r.t, ids.Correlation),
		"issued_at": "2026-10-11T08:00:00.123Z", "caller": caller,
		"scope_level": "workspace", "home_id": s.r.homeID, "workspace_id": s.r.workspaceID,
		"authority": map[string]any{"kind": "none"}, "body": body,
	})
	if err != nil {
		s.r.t.Fatal(err)
	}
	return append(out, '\n')
}

// operatorSource is an operator request naming the request_id given.
func operatorSource(requestID string) map[string]any {
	return map[string]any{"kind": "operator-request", "request_id": requestID}
}

// sessionSource is a session input.
func (s *scene) sessionSource() map[string]any {
	return map[string]any{"kind": "session-input", "input_id": mint(s.r.t, ids.Input), "receipt_id": mint(s.r.t, ids.Receipt)}
}

func (s *scene) nothingSent(t *testing.T) {
	t.Helper()
	if s.r.pool.Count() != 0 || len(s.r.pool.Reads) != 0 {
		t.Errorf("%d handoffs and %d reads were made", s.r.pool.Count(), len(s.r.pool.Reads))
	}
}

// An operator answer naming its own request_id is one handoff to
// answer-question, with the request's own identity, correlation, scope and
// caller, the request as causation, the words exactly as typed and the
// caller to record; the Pool's body and receipts come back.
func TestAnOperatorAnswerSendsOneHandoff(t *testing.T) {
	s := newScene(t)
	id := mint(t, ids.Request)
	body := s.body(s.typed(), operatorSource(id))
	resp, exit, req := s.send(s.r.operator(), body, id)
	if exit != 0 || resp.Status != envelope.Ok || resp.Replayed {
		t.Fatalf("exit %d, %s, replayed %v, error %+v", exit, resp.Status, resp.Replayed, resp.Error)
	}
	if s.r.pool.Count() != 1 {
		t.Fatalf("%d handoffs were sent", s.r.pool.Count())
	}
	own, sent := decoded(t, req), s.r.pool.Sent[0]
	digest, err := envelope.Digest(req)
	if err != nil {
		t.Fatal(err)
	}
	if sent.Command != "hive-pool" || sent.Operation != "answer-question" {
		t.Errorf("handed to %s %s", sent.Command, sent.Operation)
	}
	if sent.RequestID != own.RequestID || sent.CorrelationID != own.CorrelationID || sent.Scope != own.Scope {
		t.Errorf("identity %s %s %+v, want %s %s %+v", sent.RequestID, sent.CorrelationID, sent.Scope, own.RequestID, own.CorrelationID, own.Scope)
	}
	if sent.Caller != (envelope.Caller{Kind: envelope.CallerPackage, Package: "word", Command: "hive-word"}) {
		t.Errorf("caller %+v", sent.Caller)
	}
	if c := sent.Causation; c == nil || c.Schema != envelope.RequestSchema || c.ID != own.RequestID || c.Digest != digest {
		t.Errorf("causation %+v, want the request %s with digest %s", c, own.RequestID, digest)
	}
	if !same(t, sent.Body["requested_by"], own.Tree()["caller"]) {
		t.Errorf("requested_by %v", sent.Body["requested_by"])
	}
	// Every word reaches the Pool as it was given, and nothing is added.
	want := map[string]any{"requested_by": own.Tree()["caller"]}
	for k, v := range own.Body {
		want[k] = v
	}
	if !same(t, sent.Body, want) {
		t.Errorf("the Pool was sent %v, want %v", sent.Body, want)
	}
	if sent.Body["text"] != words || sent.Body["option_label"] != "Yes" {
		t.Errorf("text %q, label %q", sent.Body["text"], sent.Body["option_label"])
	}
	if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "pool.answer.accepted" || resp.Body["disposition"] != "accepted" || resp.Body["duplicate"] != false {
		t.Errorf("the Pool's receipts and body were not returned: %d receipts, %v", len(resp.Receipts), resp.Body)
	}
}

// A deferral is returned as the Pool gave it.
func TestADeferralIsReturnedAsTheWordOfThePool(t *testing.T) {
	s := newScene(t)
	s.r.pool.Disposition = func(*envelope.Request) string { return "deferred" }
	id := mint(t, ids.Request)
	resp, _, _ := s.send(s.r.operator(), s.body(map[string]any{"later": later("2026-11-01T00:00:00.000Z")}, operatorSource(id)), id)
	if resp.Status != envelope.Ok || resp.Body["disposition"] != "deferred" || len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "pool.question.deferred" {
		t.Fatalf("%s %v %+v", resp.Status, resp.Body, resp.Error)
	}
}

// An operator request must name the request's own request_id: another is
// invalid, and nothing is sent.
func TestAnOperatorRequestMustNameItsOwnRequestID(t *testing.T) {
	s := newScene(t)
	resp, exit, _ := s.send(s.r.operator(), s.body(s.typed(), operatorSource(mint(t, ids.Request))))
	if exit != 3 || resp.Status != envelope.Invalid {
		t.Fatalf("exit %d, %s, %+v", exit, resp.Status, resp.Error)
	}
	if s.r.pool.Count() != 0 {
		t.Errorf("%d handoffs were sent", s.r.pool.Count())
	}
}

// An answer from a worker or the package ui, whatever its source, and an
// operator-request source from a caller that is not the operator, are refused
// with word.not_authorized before anything is read or sent.
func TestAnAnswerIsRefusedToACallerThatMayNotBringIt(t *testing.T) {
	sources := map[string]func(s *scene, id string) map[string]any{
		"review":   func(s *scene, id string) map[string]any { return s.reviewSource() },
		"session":  func(s *scene, id string) map[string]any { return s.sessionSource() },
		"operator": func(s *scene, id string) map[string]any { return operatorSource(id) },
	}
	callers := map[string]func(r *rig) map[string]any{
		"supervisor": (*rig).supervisor, "operator": (*rig).operator, "worker": (*rig).worker,
		"ui": func(*rig) map[string]any { return pkg("ui") }, "package": func(*rig) map[string]any { return pkg("dispatch") },
		"test": func(*rig) map[string]any { return map[string]any{"kind": "test"} },
	}
	allowed := map[string]map[string]bool{
		"review": {"supervisor": true, "operator": true}, "session": {"supervisor": true, "operator": true},
		"operator": {"operator": true},
	}
	for source, makeSource := range sources {
		for caller, makeCaller := range callers {
			t.Run(source+" by "+caller, func(t *testing.T) {
				s := newScene(t)
				words := s.typed()
				s.submit(s.entry(words))
				id := mint(t, ids.Request)
				resp, exit, _ := s.send(makeCaller(s.r), s.body(words, makeSource(s, id)), id)
				if allowed[source][caller] {
					if exit != 0 || resp.Status != envelope.Ok || s.r.pool.Count() != 1 {
						t.Fatalf("exit %d, %s, %+v, %d sent", exit, resp.Status, resp.Error, s.r.pool.Count())
					}
					return
				}
				if exit != 4 || resp.Status != envelope.Refused || resp.Error == nil || resp.Error.Code != "word.not_authorized" {
					t.Fatalf("exit %d, %s, %+v", exit, resp.Status, resp.Error)
				}
				s.nothingSent(t)
			})
		}
	}
}

// A review submission is accepted only when the round file holds the submit
// event and its submission carries this card's answer with the same binding,
// option, label, text and Later.
func TestAReviewSubmissionIsSentThroughWhenTheRoundFileCarriesIt(t *testing.T) {
	later := later("2026-11-01T00:00:00.000Z")
	for name, words := range map[string]map[string]any{
		"option with text": {"option_id": "yes", "option_label": "Yes", "text": words},
		"option alone":     {"option_id": "yes", "option_label": "Yes", "text": ""},
		"text alone":       {"text": words},
		"later":            {"later": later, "text": ""},
		"later with text":  {"later": later, "text": words},
	} {
		t.Run(name, func(t *testing.T) {
			s := newScene(t)
			other := s.entry(map[string]any{"text": "another card"})
			other["card"] = map[string]any{"card_id": mint(t, ids.Card), "card_revision": int64(1), "question_id": mint(t, ids.Question), "question_revision": int64(1),
				"held_task_id": mint(t, ids.Task), "held_task_revision": int64(1), "decision_round_id": mint(t, ids.DecisionRound), "decision_round_revision": int64(1),
				"owner_home_id": s.r.homeID, "owner_task_id": mint(t, ids.Task)}
			s.submit(other, s.entry(words))
			resp, exit, _ := s.send(s.r.supervisor(), s.body(words, s.reviewSource()))
			if exit != 0 || resp.Status != envelope.Ok || s.r.pool.Count() != 1 {
				t.Fatalf("exit %d, %s, %+v, %d sent", exit, resp.Status, resp.Error, s.r.pool.Count())
			}
			if !same(t, s.r.pool.Sent[0].Body["source"], s.reviewSource()) {
				t.Errorf("source %v", s.r.pool.Sent[0].Body["source"])
			}
		})
	}
}

// A review submission whose round file lacks the submit event, or carries a
// different option, label, text, Later or binding, is refused with
// authority_insufficient and nothing is sent.
func TestAReviewSubmissionTheRoundFileDoesNotCarryIsRefused(t *testing.T) {
	asked := map[string]any{"option_id": "yes", "option_label": "Yes", "text": words}
	cases := map[string]func(s *scene){
		"no round file": func(s *scene) {},
		"no events":     func(s *scene) { s.write() },
		"another event": func(s *scene) {
			s.write(poolfake.Event(mint(t, ids.FeedbackEvent), 1, "submit", poolfake.Submission(mint(t, ids.Request), s.entry(asked))))
		},
		"only a draft": func(s *scene) { s.write(poolfake.Event(s.event, 1, "card-draft", s.entry(asked))) },
		"no answer":    func(s *scene) { s.submit() },
		"another option": func(s *scene) {
			s.submit(s.entry(map[string]any{"option_id": "no", "option_label": "Yes", "text": words}))
		},
		"no option": func(s *scene) { s.submit(s.entry(map[string]any{"text": words})) },
		"another label": func(s *scene) {
			s.submit(s.entry(map[string]any{"option_id": "yes", "option_label": "Yes!", "text": words}))
		},
		"another text": func(s *scene) {
			s.submit(s.entry(map[string]any{"option_id": "yes", "option_label": "Yes", "text": words + "."}))
		},
		"trimmed text": func(s *scene) {
			s.submit(s.entry(map[string]any{"option_id": "yes", "option_label": "Yes", "text": strings.TrimSpace(words)}))
		},
		"no text": func(s *scene) { s.submit(s.entry(map[string]any{"option_id": "yes", "option_label": "Yes"})) },
		"a later too": func(s *scene) {
			s.submit(s.entry(map[string]any{"option_id": "yes", "option_label": "Yes", "text": words, "later": later("2026-11-01T00:00:00.000Z")}))
		},
		"another card rev": func(s *scene) {
			e := s.entry(asked)
			e["card"] = withField(s.card, "card_revision", int64(1))
			s.submit(e)
		},
		"another question": func(s *scene) {
			e := s.entry(asked)
			e["card"] = withField(s.card, "question_revision", int64(2))
			s.submit(e)
		},
		"another item rev": func(s *scene) {
			e := s.entry(asked)
			e["card"] = withField(s.card, "held_task_revision", int64(4))
			s.submit(e)
		},
		"another round rev": func(s *scene) {
			e := s.entry(asked)
			e["card"] = withField(s.card, "decision_round_revision", int64(2))
			s.submit(e)
		},
		"another card": func(s *scene) {
			e := s.entry(asked)
			e["card"] = withField(s.card, "card_id", mint(t, ids.Card))
			s.submit(e)
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			s := newScene(t)
			prepare(s)
			resp, exit, _ := s.send(s.r.supervisor(), s.body(asked, s.reviewSource()))
			if exit != 4 || resp.Status != envelope.Refused || resp.Error == nil || resp.Error.Code != envelope.CodeAuthorityInsufficient {
				t.Fatalf("exit %d, %s, %+v", exit, resp.Status, resp.Error)
			}
			if s.r.pool.Count() != 0 {
				t.Errorf("%d handoffs were sent", s.r.pool.Count())
			}
		})
	}
}

// A Later that differs, in its date or in being chosen at all, is not the
// answer the file carries.
func TestALaterTheRoundFileDoesNotCarryIsRefused(t *testing.T) {
	asked := map[string]any{"later": later("2026-11-01T00:00:00.000Z"), "text": ""}
	for name, file := range map[string]map[string]any{
		"another date": {"later": later("2026-11-02T00:00:00.000Z"), "text": ""},
		"no later":     {"text": words},
	} {
		t.Run(name, func(t *testing.T) {
			s := newScene(t)
			s.submit(s.entry(file))
			resp, _, _ := s.send(s.r.supervisor(), s.body(asked, s.reviewSource()))
			if resp.Status != envelope.Refused || resp.Error == nil || resp.Error.Code != envelope.CodeAuthorityInsufficient || s.r.pool.Count() != 0 {
				t.Fatalf("%s %+v, %d sent", resp.Status, resp.Error, s.r.pool.Count())
			}
		})
	}
}

// A round file that is not a feedback file of the round the source names
// proves nothing.
func TestARoundFileThatIsNotTheSourcesIsRefused(t *testing.T) {
	asked := map[string]any{"option_id": "yes", "option_label": "Yes", "text": words}
	for name, damage := range map[string]func(s *scene, path string){
		"not a record": func(s *scene, path string) { os.WriteFile(path, []byte("{not json"), 0o600) },
		"another review": func(s *scene, path string) {
			s.round.ReviewID = mint(t, ids.Review)
			tree := s.round.Tree()
			tree["events"] = []any{poolfake.Event(s.event, 1, "submit", poolfake.Submission(mint(t, ids.Request), s.entry(asked)))}
			out, _ := canon.Encode(tree)
			os.WriteFile(path, append(out, '\n'), 0o600)
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newScene(t)
			s.submit(s.entry(asked))
			source := s.reviewSource()
			damage(s, filepath.Join(s.r.home, filepath.FromSlash(s.round.Path())))
			resp, _, _ := s.send(s.r.supervisor(), s.body(asked, source))
			if resp.Status != envelope.Refused || resp.Error == nil || resp.Error.Code != envelope.CodeAuthorityInsufficient || s.r.pool.Count() != 0 {
				t.Fatalf("%s %+v, %d sent", resp.Status, resp.Error, s.r.pool.Count())
			}
		})
	}
}

// A session input is recorded and shown unverified: nothing is checked, and
// the source reaches the Pool as it was given.
func TestASessionInputIsSentThroughUnverified(t *testing.T) {
	s := newScene(t)
	source := s.sessionSource()
	resp, exit, _ := s.send(s.r.supervisor(), s.body(s.typed(), source))
	if exit != 0 || resp.Status != envelope.Ok || s.r.pool.Count() != 1 {
		t.Fatalf("exit %d, %s, %+v, %d sent", exit, resp.Status, resp.Error, s.r.pool.Count())
	}
	if !same(t, s.r.pool.Sent[0].Body["source"], source) {
		t.Errorf("source %v", s.r.pool.Sent[0].Body["source"])
	}
}

// exampleHold is the hold of the example answer the fake gives.
func exampleHold(t testing.TB) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas", "ops_hive-pool.answer-question.response", "valid", "accepted.json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)["hold"].(map[string]any)
}

// An unreconciled outcome is returned as held, with no body, the hold in
// error.hold and code word.question_changed, with the Pool's receipt, on the
// first call and on its replay.
func TestAnUnreconciledOutcomeIsReturnedAsHeld(t *testing.T) {
	s := newScene(t)
	s.r.pool.Disposition = func(*envelope.Request) string { return "unreconciled" }
	id := mint(t, ids.Request)
	req := s.request(id, s.r.operator(), s.body(s.typed(), operatorSource(id)))
	hold := exampleHold(t)
	digest, err := canon.RecordDigest(mustEncode(t, hold))
	if err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, resp *envelope.Response, exit int, replayed bool) {
		t.Helper()
		if exit != 10 || resp.Status != envelope.Held || resp.Replayed != replayed {
			t.Fatalf("exit %d, %s, replayed %v, error %+v", exit, resp.Status, resp.Replayed, resp.Error)
		}
		e := resp.Error
		if e == nil || e.Code != "word.question_changed" || e.Hold == nil {
			t.Fatalf("error %+v", e)
		}
		if h := e.Hold; h.Schema != "pool.hold" || h.ID != hold["hold_id"] || h.HomeID != hold["home_id"] || h.RecordRevision != hold["record_revision"] || h.Digest != digest {
			t.Errorf("hold %+v", h)
		}
		if resp.Body != nil {
			t.Errorf("body %v", resp.Body)
		}
		if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "pool.answer.unreconciled" {
			t.Errorf("receipts %+v", resp.Receipts)
		}
	}
	first, exit := s.r.send("answer", req)
	check(t, first, exit, false)
	again, exit := s.r.send("answer", req)
	check(t, again, exit, true)
	if s.r.pool.Count() != 1 {
		t.Errorf("%d handoffs were sent", s.r.pool.Count())
	}
	if again.Receipts[0].ReceiptID != first.Receipts[0].ReceiptID {
		t.Errorf("the replay's receipt is not the recorded one")
	}
}

func mustEncode(t testing.TB, v any) []byte {
	t.Helper()
	out, err := canon.Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A retry returns the recorded outcome with replayed true, nothing sent and
// no check against the records, though the round file is gone and the Pool
// would now refuse; the same request_id with other words is replay_mismatch.
func TestARetryOfAnAnswerReturnsTheRecordedOutcome(t *testing.T) {
	s := newScene(t)
	words := s.typed()
	s.submit(s.entry(words))
	id := mint(t, ids.Request)
	caller := s.r.supervisor()
	req := s.request(id, caller, s.body(words, s.reviewSource()))
	first, _ := s.r.send("answer", req)
	if first.Status != envelope.Ok {
		t.Fatalf("%s %+v", first.Status, first.Error)
	}
	if err := os.Remove(filepath.Join(s.r.home, filepath.FromSlash(s.round.Path()))); err != nil {
		t.Fatal(err)
	}
	s.r.pool.Refuse = func(*envelope.Request) *envelope.Failure {
		return envelope.Fail(envelope.Conflict, "pool.answer_exists", "the first answer changed the round")
	}
	again, exit := s.r.send("answer", req)
	if exit != 0 || again.Status != envelope.Ok || !again.Replayed || s.r.pool.Count() != 1 {
		t.Fatalf("exit %d, %s, replayed %v, %+v, %d sent", exit, again.Status, again.Replayed, again.Error, s.r.pool.Count())
	}
	if !same(t, first.Body, again.Body) || again.Receipts[0].ReceiptID != first.Receipts[0].ReceiptID {
		t.Errorf("the retry is not the recorded outcome")
	}
	other := s.request(id, caller, s.body(map[string]any{"option_id": "yes", "option_label": "Yes", "text": words["text"].(string) + " and more"}, s.reviewSource()))
	resp, exit := s.r.send("answer", other)
	if exit != 5 || resp.Status != envelope.Conflict || resp.Error == nil || resp.Error.Code != envelope.CodeReplayMismatch || s.r.pool.Count() != 1 {
		t.Fatalf("exit %d, %s, %+v, %d sent", exit, resp.Status, resp.Error, s.r.pool.Count())
	}
}

// A refusal or a conflict of the Pool comes back with the Pool's code and
// receipts, unchanged.
func TestThePoolsRefusalOfAnAnswerIsReturnedAsItIs(t *testing.T) {
	for _, tc := range []struct {
		status envelope.Status
		code   string
		exit   int
	}{
		{envelope.Conflict, "pool.card_stale", 5},
		{envelope.Conflict, "pool.answer_exists", 5},
		{envelope.Refused, "pool.question_closed", 4},
		{envelope.Invalid, "pool.option_unknown", 3},
		{envelope.NotFound, envelope.CodeNotFound, 6},
	} {
		t.Run(tc.code, func(t *testing.T) {
			s := newScene(t)
			s.r.pool.Refuse = func(req *envelope.Request) *envelope.Failure {
				f := envelope.Fail(tc.status, tc.code, "the Pool's reason")
				receipt, err := poolfake.Receipt(req, "pool.refusal.recorded")
				if err != nil {
					t.Fatal(err)
				}
				f.Receipts = []envelope.Receipt{receipt}
				return f
			}
			id := mint(t, ids.Request)
			resp, exit, _ := s.send(s.r.operator(), s.body(s.typed(), operatorSource(id)), id)
			if exit != tc.exit || resp.Status != tc.status || resp.Error == nil || resp.Error.Code != tc.code {
				t.Fatalf("exit %d, %s, %+v", exit, resp.Status, resp.Error)
			}
			if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "pool.refusal.recorded" || resp.Body != nil {
				t.Errorf("receipts %d, body %v", len(resp.Receipts), resp.Body)
			}
		})
	}
}

// An answer holding a string of a listed credential shape is refused whole
// before anything is sent, and free text without one passes unchanged.
func TestAnAnswerHoldingACredentialShapeIsRefused(t *testing.T) {
	s := newScene(t)
	id := mint(t, ids.Request)
	secret := map[string]any{"text": "use " + "ghp_" + strings.Repeat("a", 36)}
	resp, exit, _ := s.send(s.r.operator(), s.body(secret, operatorSource(id)), id)
	if exit != 3 || resp.Status != envelope.Invalid || resp.Error == nil || resp.Error.Code != envelope.CodeSecretShapeRecognized {
		t.Fatalf("exit %d, %s, %+v", exit, resp.Status, resp.Error)
	}
	if strings.Contains(resp.Error.Message, "ghp_") {
		t.Errorf("the refusal repeats the text: %s", resp.Error.Message)
	}
	if s.r.pool.Count() != 0 {
		t.Errorf("%d handoffs were sent", s.r.pool.Count())
	}
}

// withField is a copy of the binding with one field changed.
func withField(card map[string]any, key string, value any) map[string]any {
	out := map[string]any{}
	for k, v := range card {
		out[k] = v
	}
	out[key] = value
	return out
}
