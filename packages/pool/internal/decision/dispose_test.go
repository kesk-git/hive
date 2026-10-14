package decision_test

import (
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// The shared parts of the acceptance tests of hive-pool release-question,
// complete-question and close-question (spec/word.md, release and complete,
// close, and Error codes): the requests the way hive-word hands them off, the
// questions in each state they can be in, the work they hold, and the checks
// every one of the three makes before it looks at the work.

// recordRef is a reference to a record, as evidence names one.
func recordRef(schema, id string) map[string]any { return map[string]any{"schema": schema, "id": id} }

// disposeBody is the body of a release-question or a complete-question.
func disposeBody(question string) map[string]any {
	return map[string]any{
		"question_id": question, "note": "The owner read the Hive Mind's words and acted on them.",
		"requested_by": map[string]any{"kind": "operator"},
	}
}

// archivedEvidence and replacedEvidence are the two kinds of evidence a close
// carries.
func archivedEvidence(item string) map[string]any {
	return map[string]any{"kind": "item-archived", "ref": recordRef("pool.item", item)}
}

func replacedEvidence(question string) map[string]any {
	return map[string]any{"kind": "question-replaced", "ref": recordRef("pool.question", question)}
}

// closeBody is the body of a close-question.
func closeBody(question string, evidence ...map[string]any) map[string]any {
	list := []any{}
	for _, e := range evidence {
		list = append(list, e)
	}
	return map[string]any{
		"question_id": question, "reason": "Nothing is left for the question to gate.", "evidence": list,
		"requested_by": map[string]any{"kind": "operator"},
	}
}

// revision is the record revision a record stands at.
func (h *home) revisionOf(kind, id string) int64 {
	h.t.Helper()
	return h.stored(kind, id)["record_revision"].(int64)
}

// atRevision is a request that expects the question to stand where it does.
func (h *home) atRevision(question string) sent {
	h.t.Helper()
	return sent{expectedRevs: []rev{{"pool.question", question, h.revisionOf("question", question)}}}
}

func (h *home) raisedOn(item string) raised {
	h.t.Helper()
	body := h.raise(item)
	q, r := rec(body, "question"), rec(body, "round")
	return raised{item, q["question_id"].(string), r["decision_round_id"].(string), q["card_id"].(string), rec(body, "hold")["hold_id"].(string)}
}

// answeredOn is a question raised on the item and answered, its answer applied
// and its hold still active.
func (h *home) answeredOn(item string) raised {
	h.t.Helper()
	r := h.raisedOn(item)
	h.ok(h.operator(r.question, keep))
	return r
}

// decided is a question with a decision item of its own, answered; its item is
// the decision item the Pool added.
func (h *home) decided() raised {
	h.t.Helper()
	q := ask(h.t, "")
	delete(q, "held_task_id")
	q["new_item"] = map[string]any{"title": "A decision"}
	body := h.ok(h.send("hold-question", holdBody(h.t, q, newRound(h.t)), sent{}))
	question, round := rec(body, "question"), rec(body, "round")
	r := raised{
		rec(body, "item")["task_id"].(string), question["question_id"].(string), round["decision_round_id"].(string),
		question["card_id"].(string), rec(body, "hold")["hold_id"].(string),
	}
	h.ok(h.operator(r.question, keep))
	return r
}

// child adds an item of the level beneath the parent.
func (h *home) child(parent, level string) string {
	h.t.Helper()
	body := h.ok(h.do("add", map[string]any{
		"title": "A " + level, "level": level, "deliverable": "swarm",
		"deps": []any{map[string]any{"type": "parent", "task_id": parent}},
	}))
	return body["item"].(map[string]any)["task_id"].(string)
}

// beneath is a live item with a parent edge to an item that the Pool's own
// operations would not give it, a decision item, as an import leaves it.
func (h *home) beneath(parent string) string {
	h.t.Helper()
	id := h.item()
	item := h.stored("item", id)
	item["deps"] = []any{map[string]any{"type": "parent", "task_id": parent}}
	h.plant("item", id, item)
	return id
}

// receiptOf is the identity of the receipt of the kind a response carries.
func receiptOf(t testing.TB, resp *envelope.Response, kind string) string {
	t.Helper()
	for _, r := range resp.Receipts {
		if r.Kind == kind {
			return r.ReceiptID
		}
	}
	t.Fatalf("no receipt of %s among %v", kind, kinds(resp))
	return ""
}

// The three operations, each sent as a handoff and expecting the question where
// it stands.

func (h *home) release(question string) *envelope.Response {
	h.t.Helper()
	return h.send("release-question", disposeBody(question), h.atRevision(question))
}

func (h *home) complete(question string) *envelope.Response {
	h.t.Helper()
	return h.send("complete-question", disposeBody(question), h.atRevision(question))
}

func (h *home) closeOn(question string, evidence ...map[string]any) *envelope.Response {
	h.t.Helper()
	return h.send("close-question", closeBody(question, evidence...), h.atRevision(question))
}

// Only a handoff from hive-word is accepted by the three operations, and any
// other request is refused with authority_insufficient before the Pool reads
// anything.
func TestOnlyAHandoffFromWordDisposes(t *testing.T) {
	h := newHome(t)
	answered, archived := h.answeredOn(h.item()), h.fresh()
	h.archive(archived.item)
	bodies := map[string]map[string]any{
		"release-question":  disposeBody(answered.question),
		"complete-question": disposeBody(answered.question),
		"close-question":    closeBody(archived.question, archivedEvidence(archived.item)),
	}
	requests := map[string]sent{
		"no causation":       {noCausation: true},
		"another caller":     {caller: map[string]any{"kind": "operator"}},
		"another package":    {caller: map[string]any{"kind": "package", "package": "ui", "command": "hive-ui"}},
		"another command":    {caller: map[string]any{"kind": "package", "package": "word", "command": "hive-other"}},
		"another request":    {causationOf: mint(t, ids.Request)},
		"a test, no handoff": {caller: map[string]any{"kind": "test"}, noCausation: true},
	}
	for operation, body := range bodies {
		for name, o := range requests {
			t.Run(operation+" from "+name, func(t *testing.T) {
				h.unchanged(func() {
					refused(t, h.send(operation, body, o), envelope.Refused, envelope.CodeAuthorityInsufficient)
				})
			})
		}
	}
	if q := h.stored("question", answered.question); q["state"] != "answered" || h.stored("hold", answered.hold)["state"] != "active" {
		t.Errorf("a refused request settled the question: %v", q["state"])
	}
	if q := h.stored("question", archived.question); q["state"] != "open" {
		t.Errorf("a refused request closed the question: %v", q["state"])
	}
}

// A question not at the revision the request expects is revision_conflict from
// each of the three, and nothing is written; a question that does not exist is
// not_found.
func TestADispositionPassesOnTheExpectedRevision(t *testing.T) {
	h := newHome(t)
	answered, open := h.answeredOn(h.item()), h.fresh()
	h.archive(open.item)
	stale := func(question string) sent {
		return sent{expectedRevs: []rev{{"pool.question", question, h.revisionOf("question", question) + 5}}}
	}
	for name, call := range map[string]func() *envelope.Response{
		"release": func() *envelope.Response {
			return h.send("release-question", disposeBody(answered.question), stale(answered.question))
		},
		"complete": func() *envelope.Response {
			return h.send("complete-question", disposeBody(answered.question), stale(answered.question))
		},
		"close": func() *envelope.Response {
			return h.send("close-question", closeBody(open.question, archivedEvidence(open.item)), stale(open.question))
		},
	} {
		t.Run(name, func(t *testing.T) {
			h.unchanged(func() { refused(t, call(), envelope.Conflict, envelope.CodeRevisionConflict) })
		})
	}
	missing := mint(t, ids.Question)
	h.unchanged(func() {
		refused(t, h.send("release-question", disposeBody(missing), sent{}), envelope.NotFound, envelope.CodeNotFound)
		refused(t, h.send("complete-question", disposeBody(missing), sent{}), envelope.NotFound, envelope.CodeNotFound)
		refused(t, h.send("close-question", closeBody(missing, archivedEvidence(open.item)), sent{}), envelope.NotFound, envelope.CodeNotFound)
	})
}

// Neither release nor complete supplies an answer: a question with none, open
// or deferred, is pool.question_unanswered, and a question released,
// completed or moot is final, pool.question_closed. Nothing is written.
func TestADispositionNeedsAnAppliedAnswerAndAQuestionNotFinal(t *testing.T) {
	h := newHome(t)
	open := h.fresh().question
	deferred := h.fresh().question
	h.ok(h.operator(deferred, laterAt(later)))

	released := h.answeredOn(h.item()).question
	h.ok(h.release(released))

	completedItem := h.item()
	completed := h.answeredOn(completedItem).question
	h.archive(completedItem)
	h.ok(h.complete(completed))

	mootItem := h.fresh()
	h.archive(mootItem.item)
	h.ok(h.closeOn(mootItem.question, archivedEvidence(mootItem.item)))

	for _, operation := range []string{"release-question", "complete-question"} {
		for name, tc := range map[string]struct{ question, code string }{
			"an open question":     {open, "pool.question_unanswered"},
			"a deferred question":  {deferred, "pool.question_unanswered"},
			"a released question":  {released, "pool.question_closed"},
			"a completed question": {completed, "pool.question_closed"},
			"a moot question":      {mootItem.question, "pool.question_closed"},
		} {
			t.Run(operation+" on "+name, func(t *testing.T) {
				h.unchanged(func() {
					refused(t, h.send(operation, disposeBody(tc.question), h.atRevision(tc.question)), envelope.Refused, tc.code)
				})
			})
		}
	}
}
