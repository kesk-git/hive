package decision_test

import (
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// The acceptance test of hive-pool answer-question (spec/word.md, answer, and
// Error codes). It drives the executable the default registry describes, in
// process, over a home of its own, with the requests the way hive-word hands
// them off, and holds the operation to the contract: who may send it, each
// check of the table deciding in its stated order where two apply, an answer
// idempotent by its source within its question, a late answer recorded
// unreconciled, each of the four dispositions with what it changes, an answer
// kept as written when another supersedes it, an unreconciled answer named by
// the answer that reconciles it, and a deferral ended by a later answer.

const (
	later    = "2026-11-02T09:00:00.000Z"
	laterToo = "2026-12-01T09:00:00.000Z"
)

// A source of each kind the Pool records.

func operatorSource(requestID string) map[string]any {
	return map[string]any{"kind": "operator-request", "request_id": requestID}
}

func submission(task, review, event string) map[string]any {
	return map[string]any{
		"kind": "review-submission", "task_id": task, "generation": int64(1), "review_id": review,
		"round_id": int64(1), "feedback_event_id": event,
	}
}

func (h *home) event() string { return mint(h.t, ids.FeedbackEvent) }

// shown is the binding of a card with members replaced: the revisions a card
// showed when it was answered.
func shown(binding map[string]any, replace ...any) map[string]any {
	out := map[string]any{}
	for k, v := range binding {
		out[k] = v
	}
	for i := 0; i < len(replace); i += 2 {
		out[replace[i].(string)] = replace[i+1]
	}
	return out
}

// binding is the card binding of the question as its card shows it now.
func (h *home) binding(question string) map[string]any {
	h.t.Helper()
	q := h.stored("question", question)
	r := h.stored("decision-round", q["decision_round_id"].(string))
	c := card(r, q["card_id"].(string))
	return map[string]any{
		"card_id": c["card_id"], "card_revision": c["card_revision"],
		"question_id": question, "question_revision": c["question_revision"],
		"held_task_id": q["held_task_id"], "held_task_revision": c["held_task_revision"],
		"decision_round_id": r["decision_round_id"], "decision_round_revision": r["record_revision"],
		"owner_home_id": h.homeID, "owner_task_id": q["held_task_id"],
	}
}

// The members of an answer other than the card and its source.

var (
	keep     = map[string]any{"option_id": "keep", "option_label": "Option keep"}
	switchTo = map[string]any{"option_id": "switch", "option_label": "Option switch"}
)

func withText(from map[string]any, text string) map[string]any {
	out := map[string]any{"text": text}
	for k, v := range from {
		out[k] = v
	}
	return out
}

func laterAt(date string) map[string]any {
	return map[string]any{"later": map[string]any{"revisit_at": date}}
}

// answerBody is the body of an answer-question: the question's card as it
// shows now, with the members given over it.
func (h *home) answerBody(question string, source, with map[string]any) map[string]any {
	h.t.Helper()
	b := map[string]any{
		"card": h.binding(question), "text": "", "source": source, "answered_at": "2026-10-14T09:30:00.000Z",
		"requested_by": map[string]any{"kind": "operator"},
	}
	for k, v := range with {
		b[k] = v
	}
	return b
}

// answer sends an answer to the question under a source.
func (h *home) answer(question string, source, with map[string]any) *envelope.Response {
	h.t.Helper()
	return h.send("answer-question", h.answerBody(question, source, with), sent{})
}

// operator sends an answer typed at a terminal: under a request of its own,
// which is its source.
func (h *home) operator(question string, with map[string]any) *envelope.Response {
	h.t.Helper()
	id := mint(h.t, ids.Request)
	return h.send("answer-question", h.answerBody(question, operatorSource(id), with), sent{requestID: id})
}

// raised is a question raised on an item of its own, with the identities of
// what holds it.
type raised struct {
	item, question, round, cardID, hold string
}

func (h *home) fresh() raised {
	h.t.Helper()
	item := h.item()
	body := h.raise(item)
	q, r := rec(body, "question"), rec(body, "round")
	return raised{item, q["question_id"].(string), r["decision_round_id"].(string), q["card_id"].(string), rec(body, "hold")["hold_id"].(string)}
}

func (h *home) incomplete(round string) {
	h.t.Helper()
	r := h.stored("decision-round", round)
	r["complete"], r["incomplete_reason"] = false, "An attachment could not be read."
	h.plant("decision-round", round, r)
}

func answers(q map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, a := range q["answers"].([]any) {
		out = append(out, a.(map[string]any))
	}
	return out
}

// Only a handoff from hive-word is accepted, and any other request is refused
// with authority_insufficient before the Pool reads anything.
func TestOnlyAHandoffFromWordAnswers(t *testing.T) {
	h := newHome(t)
	r := h.fresh()
	id := mint(t, ids.Request)
	body := h.answerBody(r.question, operatorSource(id), keep)
	requests := map[string]sent{
		"no causation":       {requestID: id, noCausation: true},
		"another caller":     {requestID: id, caller: map[string]any{"kind": "operator"}},
		"another package":    {requestID: id, caller: map[string]any{"kind": "package", "package": "ui", "command": "hive-ui"}},
		"another request":    {requestID: id, causationOf: mint(t, ids.Request)},
		"a test, no handoff": {requestID: id, caller: map[string]any{"kind": "test"}, noCausation: true},
	}
	for name, o := range requests {
		t.Run(name, func(t *testing.T) {
			h.unchanged(func() {
				refused(t, h.send("answer-question", body, o), envelope.Refused, envelope.CodeAuthorityInsufficient)
			})
		})
	}
	if stored := h.stored("question", r.question); len(answers(stored)) != 0 || stored["state"] != "open" {
		t.Errorf("a refused request answered the question: %v", stored)
	}
}

// Each check of the table decides in its stated order: where two apply, the
// first that applies decides, and nothing is written by a refusal.
func TestEachCheckOfAnswerDecidesInItsOrder(t *testing.T) {
	// answered is a question answered by a source, once, to leave an answer
	// recorded and applied.
	answered := func(t *testing.T) (*home, raised, map[string]any) {
		h := newHome(t)
		r := h.fresh()
		source := submission(h.item(), mint(t, ids.Review), h.event())
		h.ok(h.answer(r.question, source, keep))
		return h, r, source
	}
	// revised is a question revised once, so that its first revision is
	// earlier.
	revised := func(t *testing.T) (*home, raised) {
		h := newHome(t)
		r := h.fresh()
		h.ok(h.send("revise-question", revision(r.question), sent{}))
		return h, r
	}

	t.Run("not_found before an answer of the same source", func(t *testing.T) {
		h, r, source := answered(t)
		for name, binding := range map[string]map[string]any{
			"a revision the question does not have": shown(h.binding(r.question), "question_revision", int64(9)),
			"a question that does not exist":        shown(h.binding(r.question), "question_id", mint(t, ids.Question)),
			"another card":                          shown(h.binding(r.question), "card_id", mint(t, ids.Card)),
			"another round":                         shown(h.binding(r.question), "decision_round_id", mint(t, ids.DecisionRound)),
			"another held item":                     shown(h.binding(r.question), "held_task_id", mint(t, ids.Task)),
		} {
			t.Run(name, func(t *testing.T) {
				h.unchanged(func() {
					body := h.answerBody(r.question, source, keep)
					body["card"] = binding
					refused(t, h.send("answer-question", body, sent{}), envelope.NotFound, envelope.CodeNotFound)
				})
			})
		}
	})

	t.Run("duplicate before round_incomplete", func(t *testing.T) {
		h, r, source := answered(t)
		first := h.stored("question", r.question)
		h.incomplete(r.round)
		got := h.ok(h.answer(r.question, source, switchTo))
		if got["duplicate"] != true || got["answer_sequence"] != int64(1) {
			t.Errorf("the second answer of the source: %v", got)
		}
		// Another source meets the round, which was not collected whole.
		refused(t, h.answer(r.question, operatorSource(mint(t, ids.Request)), keep), envelope.Refused, "pool.round_incomplete")
		if !equal(h.stored("question", r.question), first) {
			t.Errorf("the question changed")
		}
	})

	t.Run("round_incomplete before option_unknown", func(t *testing.T) {
		h := newHome(t)
		r := h.fresh()
		h.incomplete(r.round)
		h.unchanged(func() {
			refused(t, h.operator(r.question, map[string]any{"option_id": "nope", "option_label": "Nope"}), envelope.Refused, "pool.round_incomplete")
		})
	})

	t.Run("option_unknown before unreconciled", func(t *testing.T) {
		h, r := revised(t)
		// "both" is an option of the second revision, not of the first.
		early := map[string]any{"card": shown(h.binding(r.question), "question_revision", int64(1)), "option_id": "both", "option_label": "Option both"}
		h.unchanged(func() {
			refused(t, h.operator(r.question, early), envelope.Invalid, "pool.option_unknown")
		})
		// The same option against the revision that has it is an answer.
		if got := h.ok(h.operator(r.question, map[string]any{"option_id": "both", "option_label": "Option both"})); got["disposition"] != "accepted" {
			t.Errorf("answer %v", got)
		}
	})

	t.Run("unreconciled before question_closed", func(t *testing.T) {
		h, r := revised(t)
		h.settled(r.question, "released.json")
		early := map[string]any{"card": shown(h.binding(r.question), "question_revision", int64(1)), "text": "That, then."}
		got := h.ok(h.operator(r.question, early))
		if got["disposition"] != "unreconciled" {
			t.Fatalf("a late answer to a released question: %v", got)
		}
		q := rec(got, "question")
		if q["state"] != "released" || q["applied"].(map[string]any)["answer_sequence"] != int64(1) || len(answers(q)) != 2 {
			t.Errorf("the released question changed: %v", q)
		}
		// Against the current revision the question is closed.
		h.unchanged(func() {
			refused(t, h.operator(r.question, map[string]any{"text": "That, then."}), envelope.Refused, "pool.question_closed")
		})
	})

	t.Run("question_closed before card_stale", func(t *testing.T) {
		h, r := revised(t)
		h.settled(r.question, "released.json")
		stale := map[string]any{"card": shown(h.binding(r.question), "card_revision", int64(1)), "text": "That, then."}
		h.unchanged(func() {
			refused(t, h.operator(r.question, stale), envelope.Refused, "pool.question_closed")
		})
	})

	t.Run("card_stale before answer_exists", func(t *testing.T) {
		h, r, _ := answered(t)
		stale := map[string]any{"card": shown(h.binding(r.question), "card_revision", int64(7)), "text": "Instead.", "supersedes": int64(1)}
		h.unchanged(func() {
			refused(t, h.operator(r.question, stale), envelope.Conflict, "pool.card_stale")
		})
		// Its other revisions are never grounds to refuse.
		odd := map[string]any{"card": shown(h.binding(r.question), "held_task_revision", int64(40), "decision_round_revision", int64(40)), "text": "Instead.", "supersedes": int64(1)}
		if got := h.ok(h.operator(r.question, odd)); got["disposition"] != "accepted" {
			t.Errorf("answer %v", got)
		}
	})

	t.Run("answer_exists before answer_reference", func(t *testing.T) {
		h, r, _ := answered(t)
		// The applied answer is not unreconciled, and the answer does not
		// supersede it.
		h.unchanged(func() {
			refused(t, h.operator(r.question, map[string]any{"text": "Instead.", "reconciles": int64(1)}), envelope.Conflict, "pool.answer_exists")
		})
		// Later never supersedes, and a supersedes naming another answer does
		// not.
		h.unchanged(func() {
			refused(t, h.operator(r.question, laterAt(later)), envelope.Conflict, "pool.answer_exists")
			refused(t, h.operator(r.question, map[string]any{"text": "Instead.", "supersedes": int64(2)}), envelope.Conflict, "pool.answer_exists")
		})
	})

	t.Run("answer_reference", func(t *testing.T) {
		h := newHome(t)
		r := h.fresh()
		// No answer is applied, so none is superseded.
		h.unchanged(func() {
			refused(t, h.operator(r.question, map[string]any{"text": "Instead.", "supersedes": int64(1)}), envelope.Invalid, "pool.answer_reference")
		})
		// A deferral is an answer, and an answer that is not unreconciled is
		// not reconciled; nor is one that does not exist.
		h.ok(h.operator(r.question, laterAt(later)))
		h.unchanged(func() {
			refused(t, h.operator(r.question, map[string]any{"text": "That.", "reconciles": int64(1)}), envelope.Invalid, "pool.answer_reference")
			refused(t, h.operator(r.question, map[string]any{"text": "That.", "reconciles": int64(5)}), envelope.Invalid, "pool.answer_reference")
		})
	})
}

// Two answers to one question with the same source are one answer: the second
// returns the first's receipts with duplicate true and writes nothing. The
// same source answering another question is its own answer, and two operator
// requests are two sources.
func TestASecondAnswerWithTheSameSourceIsOneAnswer(t *testing.T) {
	h := newHome(t)
	r, other := h.fresh(), h.fresh()
	source := submission(h.item(), mint(t, ids.Review), h.event())

	firstResp := h.answer(r.question, source, withText(keep, "As it stands."))
	first := h.ok(firstResp)
	sameKinds(t, firstResp, "pool.answer.accepted")
	if first["duplicate"] != false || first["answer_sequence"] != int64(1) || first["disposition"] != "accepted" {
		t.Fatalf("the first answer: %v", first)
	}

	// The second, under a request of its own and with other words, is the
	// first.
	var secondResp *envelope.Response
	h.unchanged(func() {
		secondResp = h.answer(r.question, source, withText(switchTo, "Not as it stands."))
	})
	second := h.ok(secondResp)
	if second["duplicate"] != true || second["answer_sequence"] != int64(1) || second["disposition"] != "accepted" {
		t.Errorf("the second answer: %v", second)
	}
	if secondResp.RequestID == firstResp.RequestID {
		t.Fatal("the two answers share a request")
	}
	if len(secondResp.Receipts) != len(firstResp.Receipts) || len(firstResp.Receipts) == 0 {
		t.Fatalf("receipts %v, want %v", secondResp.Receipts, firstResp.Receipts)
	}
	for i := range firstResp.Receipts {
		if secondResp.Receipts[i].ReceiptID != firstResp.Receipts[i].ReceiptID || secondResp.Receipts[i].Kind != firstResp.Receipts[i].Kind {
			t.Errorf("receipt %d is %s %s, want the first answer's %s %s", i, secondResp.Receipts[i].Kind, secondResp.Receipts[i].ReceiptID, firstResp.Receipts[i].Kind, firstResp.Receipts[i].ReceiptID)
		}
	}
	q := h.stored("question", r.question)
	if a := answers(q); len(a) != 1 || a[0]["text"] != "As it stands." || a[0]["option_id"] != "keep" {
		t.Errorf("answers %v", a)
	}

	// The same source answering another question is its own answer.
	got := h.ok(h.answer(other.question, source, keep))
	if got["duplicate"] != false || got["answer_sequence"] != int64(1) {
		t.Errorf("the same source on another question: %v", got)
	}
	if a := answers(h.stored("question", other.question)); len(a) != 1 || !equal(a[0]["source"], source) {
		t.Errorf("answers %v", a)
	}

	// Two operator requests are two sources: an operator who chose Later and
	// answers again under a new request is recorded and applied.
	third := h.fresh()
	deferred := h.operator(third.question, laterAt(later))
	if body := h.ok(deferred); body["disposition"] != "deferred" {
		t.Fatalf("Later: %v", body)
	}
	again := h.operator(third.question, keep)
	body := h.ok(again)
	if body["duplicate"] != false || body["answer_sequence"] != int64(2) || body["disposition"] != "accepted" {
		t.Errorf("the operator's second request: %v", body)
	}
	if state := rec(body, "question")["state"]; state != "answered" || len(answers(rec(body, "question"))) != 2 {
		t.Errorf("question %v", rec(body, "question"))
	}
}

// A Later against an earlier revision is recorded unreconciled, exactly as
// submitted, and applied to nothing; the answer given afterwards against the
// revision now asked may name it, and neither changes the other as written.
func TestALateLaterIsRecordedUnreconciledAndReconciled(t *testing.T) {
	h := newHome(t)
	r := h.fresh()
	h.ok(h.send("revise-question", revision(r.question), sent{}))
	beforeQuestion, beforeRound, beforeHold := h.stored("question", r.question), h.stored("decision-round", r.round), h.stored("hold", r.hold)

	late := withText(laterAt(later), "Ask me again in a month.")
	late["card"] = shown(h.binding(r.question), "question_revision", int64(1), "card_revision", int64(1))
	resp := h.operator(r.question, late)
	got := h.ok(resp)
	sameKinds(t, resp, "pool.answer.unreconciled")
	if got["disposition"] != "unreconciled" || got["answer_sequence"] != int64(1) || got["duplicate"] != false {
		t.Fatalf("response %v", got)
	}
	q := rec(got, "question")
	a := answers(q)
	if len(a) != 1 || a[0]["disposition"] != "unreconciled" || a[0]["question_revision"] != int64(1) || a[0]["text"] != "Ask me again in a month." ||
		a[0]["later"].(map[string]any)["revisit_at"] != later || a[0]["receipt_id"] != resp.Receipts[0].ReceiptID || a[0]["record_revision"] != int64(3) {
		t.Errorf("the answer as recorded: %v", a)
	}
	if q["state"] != "open" || q["deferral"] != nil || q["applied"] != nil || q["record_revision"] != int64(3) {
		t.Errorf("an unreconciled answer changed the question: %v", q)
	}
	// Applied to nothing: the round and its card, and the hold, are as they were.
	if !equal(h.stored("decision-round", r.round), beforeRound) || !equal(h.stored("hold", r.hold), beforeHold) {
		t.Errorf("an unreconciled answer changed the round or the hold")
	}
	for _, m := range []string{"state", "current_revision", "revisions", "deferral", "applied"} {
		if !equal(q[m], beforeQuestion[m]) {
			t.Errorf("%s changed from %v to %v", m, beforeQuestion[m], q[m])
		}
	}
	if !equal(rec(got, "round"), beforeRound) || !equal(rec(got, "hold"), beforeHold) {
		t.Errorf("the response shows another round or hold")
	}
	if resp.Receipts[0].Subject == nil || resp.Receipts[0].Subject.ID != r.question || resp.Receipts[0].Subject.RecordRevision != 3 {
		t.Errorf("subject %+v", resp.Receipts[0].Subject)
	}

	// The answer given against the revision now asked names the unreconciled
	// answer it settles.
	written := h.stored("question", r.question)
	settling := h.operator(r.question, withText(map[string]any{"reconciles": int64(1), "option_id": "switch", "option_label": "Option switch"}, "Switch."))
	body := h.ok(settling)
	sameKinds(t, settling, "pool.answer.accepted")
	if body["disposition"] != "accepted" || body["answer_sequence"] != int64(2) {
		t.Fatalf("the reconciling answer: %v", body)
	}
	all := answers(rec(body, "question"))
	if len(all) != 2 || all[1]["reconciles"] != int64(1) || all[1]["question_revision"] != int64(2) || all[1]["record_revision"] != int64(4) {
		t.Errorf("answers %v", all)
	}
	if !equal(all[0], answers(written)[0]) {
		t.Errorf("the unreconciled answer changed: %v", all[0])
	}
	if q := rec(body, "question"); q["state"] != "answered" || q["applied"].(map[string]any)["answer_sequence"] != int64(2) || q["deferral"] != nil {
		t.Errorf("question %v", q)
	}
}

// An answer against the current revision, with no Later, is accepted: the
// question stands on it exactly as given, its card is answered and the round
// closes when no card of it is open, with the hold still active.
func TestAnAcceptedAnswerIsAppliedAsGiven(t *testing.T) {
	h := newHome(t)
	r := h.fresh()
	beforeRound, beforeHold := h.stored("decision-round", r.round), h.stored("hold", r.hold)
	words := "  Keep it,\n  but renegotiate the price.  "
	source := submission(h.item(), mint(t, ids.Review), h.event())
	body := h.answerBody(r.question, source, withText(keep, words))
	body["answered_at"] = "2026-10-14T09:31:02.500Z"
	body["requested_by"] = map[string]any{"kind": "supervisor", "session_id": mint(t, ids.Session), "session_generation": int64(2)}
	resp := h.send("answer-question", body, sent{})
	got := h.ok(resp)
	sameKinds(t, resp, "pool.answer.accepted")
	if got["disposition"] != "accepted" || got["answer_sequence"] != int64(1) || got["duplicate"] != false {
		t.Fatalf("response %v", got)
	}

	q := rec(got, "question")
	a := answers(q)
	if len(a) != 1 {
		t.Fatalf("answers %v", a)
	}
	want := map[string]any{
		"sequence": int64(1), "record_revision": int64(2), "question_revision": int64(1), "card": body["card"], "option_id": "keep",
		"option_label": "Option keep", "text": words, "source": source, "answered_at": "2026-10-14T09:31:02.500Z",
		"recorded_by": body["requested_by"], "disposition": "accepted", "request_id": resp.RequestID, "receipt_id": resp.Receipts[0].ReceiptID,
	}
	for k, v := range want {
		if !equal(a[0][k], v) {
			t.Errorf("answer %s is %v, want %v", k, a[0][k], v)
		}
	}
	if a[0]["recorded_at"] == nil || a[0]["later"] != nil || a[0]["supersedes"] != nil || a[0]["reconciles"] != nil {
		t.Errorf("answer %v", a[0])
	}
	if q["state"] != "answered" || q["record_revision"] != int64(2) || q["deferral"] != nil ||
		!equal(q["applied"], map[string]any{"answer_sequence": int64(1), "record_revision": int64(2)}) {
		t.Errorf("question %v", q)
	}
	if !equal(h.stored("question", r.question), q) {
		t.Errorf("what is returned is not what is stored")
	}
	if s := resp.Receipts[0].Subject; s == nil || s.Schema != "pool.question" || s.ID != r.question || s.RecordRevision != 2 {
		t.Errorf("subject %+v", s)
	}

	// The card is answered with the answer's sequence; the round, with its one
	// card answered, is closed.
	round := rec(got, "round")
	c := card(round, r.cardID)
	if c["state"] != "answered" || c["answer_sequence"] != int64(1) || c["card_revision"] != int64(1) || c["question_revision"] != int64(1) {
		t.Errorf("card %v", c)
	}
	if round["state"] != "closed" || round["closed_at"] == nil || round["record_revision"] != beforeRound["record_revision"].(int64)+1 {
		t.Errorf("round %v", round)
	}
	if !equal(h.stored("decision-round", r.round), round) {
		t.Errorf("what is returned is not what is stored")
	}

	// The hold stays: an answer never releases it.
	hold := rec(got, "hold")
	if hold["state"] != "active" || !equal(hold, beforeHold) || !equal(h.stored("hold", r.hold), beforeHold) {
		t.Errorf("hold %v", hold)
	}
}

// Free text with no option is a complete answer; an option is kept with its
// label as displayed, with or without text.
func TestAnOptionAndFreeTextAreEachAnAnswer(t *testing.T) {
	h := newHome(t)
	for name, with := range map[string]map[string]any{
		"free text alone":    {"text": "Neither: build it in-house."},
		"an option alone":    keep,
		"an option and text": withText(switchTo, "Switch, after the audit."),
	} {
		t.Run(name, func(t *testing.T) {
			r := h.fresh()
			got := h.ok(h.operator(r.question, with))
			a := answers(rec(got, "question"))[0]
			if got["disposition"] != "accepted" || a["text"] != with["text"] && (with["text"] != nil || a["text"] != "") || a["option_id"] != with["option_id"] || a["option_label"] != with["option_label"] {
				t.Errorf("response %v", got)
			}
		})
	}
}

// A round closes when no card of it is open: one review submission answering
// two cards of one round applies both, each by its own answer, in either
// order, and answering one never makes the other stale.
func TestAnAnswerToOneCardNeverStalesAnother(t *testing.T) {
	h := newHome(t)
	item := h.item()
	first := h.raise(item)
	roundID := rec(first, "round")["decision_round_id"].(string)
	second := h.raiseIn(item, roundID)
	one, two := rec(first, "question")["question_id"].(string), rec(second, "question")["question_id"].(string)
	source := submission(h.item(), mint(t, ids.Review), h.event())

	// Both cards were shown at the round's revision 2; the first answer
	// raises it.
	bound := map[string]any{"card": h.binding(two)}
	got := h.ok(h.answer(one, source, keep))
	round := rec(got, "round")
	if round["state"] != "open" || card(round, rec(first, "question")["card_id"].(string))["state"] != "answered" ||
		card(round, rec(second, "question")["card_id"].(string))["state"] != "open" {
		t.Fatalf("round %v", round)
	}
	body := h.answerBody(two, source, keep)
	body["card"] = bound["card"]
	resp := h.send("answer-question", body, sent{})
	got = h.ok(resp)
	round = rec(got, "round")
	if got["disposition"] != "accepted" || got["duplicate"] != false || round["state"] != "closed" || round["closed_at"] == nil {
		t.Errorf("the second card: %v", got)
	}
	for _, id := range []string{one, two} {
		if q := h.stored("question", id); q["state"] != "answered" || len(answers(q)) != 1 || !equal(answers(q)[0]["source"], source) {
			t.Errorf("question %v", q)
		}
	}

	// In the other order the round stays open until the last card.
	third := h.raise(item)
	x := rec(h.raiseIn(item, rec(third, "round")["decision_round_id"].(string)), "question")["question_id"].(string)
	y := rec(third, "question")["question_id"].(string)
	other := submission(h.item(), mint(t, ids.Review), h.event())
	if got := h.ok(h.answer(x, other, keep)); rec(got, "round")["state"] != "open" {
		t.Errorf("round %v", rec(got, "round"))
	}
	if got := h.ok(h.answer(y, other, switchTo)); rec(got, "round")["state"] != "closed" {
		t.Errorf("round %v", rec(got, "round"))
	}
}

// An answer that supersedes the applied answer replaces it: the question
// stands on the new one with its disposition still pending, the card names
// it, and the superseded entry stays as written.
func TestASupersedingAnswerKeepsTheSupersededOneAsWritten(t *testing.T) {
	h := newHome(t)
	r := h.fresh()
	h.ok(h.operator(r.question, withText(keep, "Keep it.")))
	written := answers(h.stored("question", r.question))[0]
	beforeRound := h.stored("decision-round", r.round)

	resp := h.operator(r.question, withText(map[string]any{"supersedes": int64(1), "option_id": "switch", "option_label": "Option switch"}, "On reflection, switch."))
	got := h.ok(resp)
	sameKinds(t, resp, "pool.answer.accepted", "pool.answer.superseded")
	if got["disposition"] != "accepted" || got["answer_sequence"] != int64(2) || got["duplicate"] != false {
		t.Fatalf("response %v", got)
	}
	q := rec(got, "question")
	all := answers(q)
	if len(all) != 2 || !equal(all[0], written) {
		t.Errorf("the superseded answer changed: %v", all)
	}
	if all[1]["supersedes"] != int64(1) || all[1]["disposition"] != "accepted" || all[1]["text"] != "On reflection, switch." || all[1]["record_revision"] != int64(3) {
		t.Errorf("the superseding answer: %v", all[1])
	}
	for _, rc := range resp.Receipts {
		if rc.Subject == nil || rc.Subject.ID != r.question || rc.Subject.RecordRevision != 3 {
			t.Errorf("receipt %s subject %+v", rc.Kind, rc.Subject)
		}
		if rc.Kind == "pool.answer.accepted" && all[1]["receipt_id"] != rc.ReceiptID {
			t.Errorf("the answer names receipt %v, not its acceptance %s", all[1]["receipt_id"], rc.ReceiptID)
		}
	}
	if q["state"] != "answered" || !equal(q["applied"], map[string]any{"answer_sequence": int64(2), "record_revision": int64(3)}) || q["disposition"] != nil {
		t.Errorf("question %v", q)
	}
	// The card names the new answer; the round, closed already, is unchanged
	// but for the card.
	round := rec(got, "round")
	c := card(round, r.cardID)
	if c["state"] != "answered" || c["answer_sequence"] != int64(2) || round["state"] != "closed" || round["closed_at"] != beforeRound["closed_at"] {
		t.Errorf("round %v", round)
	}
	if hold := rec(got, "hold"); hold["state"] != "active" {
		t.Errorf("hold %v", hold)
	}
}

// Later against the current revision, with no answer applied, defers the
// question with its revisit date: the hold stays and the card stays open. A
// later accepted answer ends the deferral.
func TestALaterDefersAndALaterAnswerEndsTheDeferral(t *testing.T) {
	h := newHome(t)
	r := h.fresh()
	beforeRound, beforeHold := h.stored("decision-round", r.round), h.stored("hold", r.hold)

	resp := h.operator(r.question, withText(laterAt(later), "Not now."))
	got := h.ok(resp)
	sameKinds(t, resp, "pool.question.deferred")
	if got["disposition"] != "deferred" || got["answer_sequence"] != int64(1) || got["duplicate"] != false {
		t.Fatalf("response %v", got)
	}
	q := rec(got, "question")
	a := answers(q)[0]
	if q["state"] != "deferred" || q["applied"] != nil || !equal(q["deferral"], map[string]any{"revisit_at": later, "answer_sequence": int64(1)}) ||
		a["disposition"] != "deferred" || a["later"].(map[string]any)["revisit_at"] != later || a["text"] != "Not now." || a["receipt_id"] != resp.Receipts[0].ReceiptID {
		t.Errorf("question %v", q)
	}
	// The card stays open, and the round and the hold are as they were.
	if !equal(rec(got, "round"), beforeRound) || !equal(h.stored("decision-round", r.round), beforeRound) || !equal(rec(got, "hold"), beforeHold) {
		t.Errorf("a deferral changed the round or the hold")
	}

	// A later Later replaces the revisit date, and still applies nothing.
	got = h.ok(h.operator(r.question, laterAt(laterToo)))
	if q := rec(got, "question"); q["state"] != "deferred" || !equal(q["deferral"], map[string]any{"revisit_at": laterToo, "answer_sequence": int64(2)}) || q["applied"] != nil {
		t.Errorf("question %v", q)
	}

	// An answer that is not Later meets no applied answer, needs no
	// supersedes and ends the deferral.
	resp = h.operator(r.question, keep)
	got = h.ok(resp)
	sameKinds(t, resp, "pool.answer.accepted")
	q = rec(got, "question")
	if got["disposition"] != "accepted" || got["answer_sequence"] != int64(3) || q["state"] != "answered" || q["deferral"] != nil ||
		!equal(q["applied"], map[string]any{"answer_sequence": int64(3), "record_revision": int64(4)}) {
		t.Errorf("question %v", q)
	}
	if c := card(rec(got, "round"), r.cardID); c["state"] != "answered" || c["answer_sequence"] != int64(3) || rec(got, "round")["state"] != "closed" {
		t.Errorf("round %v", rec(got, "round"))
	}
	if again := h.stored("question", r.question); !equal(again, q) || again["deferral"] != nil {
		t.Errorf("stored %v", again)
	}
	// The deferred answers stay as written.
	if all := answers(q); len(all) != 3 || all[0]["disposition"] != "deferred" || all[1]["disposition"] != "deferred" {
		t.Errorf("answers %v", all)
	}
}
