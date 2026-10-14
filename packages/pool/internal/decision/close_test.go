package decision_test

import (
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// The acceptance test of hive-pool close-question (spec/word.md, close): an
// open or deferred question closed as moot, on the owner's reason and evidence
// the Pool checks as it stands, which records no answer, settles the card,
// closes the round when no card of it is open and releases the hold with the
// close's receipt.

// A close on archived work: the held item and every item beneath it archived.
func TestCloseOnArchivedWork(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start func(h *home, r raised)
	}{
		{"an open question", func(h *home, r raised) {}},
		{"a deferred question", func(h *home, r raised) { h.ok(h.operator(r.question, laterAt(later))) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHome(t)
			r := h.fresh()
			tc.start(h, r)
			h.archive(r.item)
			question, item := h.stored("question", r.question), h.stored("item", r.item)

			body := closeBody(r.question, archivedEvidence(r.item))
			resp := h.send("close-question", body, h.atRevision(r.question))
			got := h.ok(resp)
			sameKinds(t, resp, "pool.question.mooted", "pool.hold.released")
			receipt := receiptOf(t, resp, "pool.question.mooted")

			q := rec(got, "question")
			if q["state"] != "moot" || q["record_revision"] != question["record_revision"].(int64)+1 {
				t.Fatalf("question %v at revision %v", q["state"], q["record_revision"])
			}
			c := q["closure"].(map[string]any)
			if c["reason"] != body["reason"] || c["request_id"] != resp.RequestID || c["receipt_id"] != receipt ||
				c["by"].(map[string]any)["kind"] != "operator" || c["at"] == nil || !equal(c["evidence"], body["evidence"]) {
				t.Errorf("closure %v", c)
			}
			// No answer is recorded or implied, and a deferral ends.
			for _, m := range []string{"applied", "deferral", "disposition"} {
				if _, has := q[m]; has {
					t.Errorf("a moot question carries %s: %v", m, q[m])
				}
			}
			if !equal(q["answers"], question["answers"]) {
				t.Errorf("the close changed the answers: %v", q["answers"])
			}

			hold := rec(got, "hold")
			by, _ := hold["released_by"].(map[string]any)
			if hold["hold_id"] != r.hold || hold["state"] != "released" || hold["released_at"] == nil || by["schema"] != "hive.receipt" || by["id"] != receipt {
				t.Errorf("hold %v", hold)
			}
			// The round has no other card, so it closes with the card moot.
			round := rec(got, "round")
			c1 := card(round, r.cardID)
			if c1["state"] != "moot" || round["state"] != "closed" || round["closed_at"] == nil {
				t.Errorf("round %v, card %v", round["state"], c1)
			}
			if _, has := c1["answer_sequence"]; has {
				t.Errorf("a moot card names an answer: %v", c1)
			}
			if !equal(h.stored("item", r.item), item) {
				t.Errorf("the item changed")
			}
			if !equal(h.stored("question", r.question), q) || !equal(h.stored("hold", r.hold), hold) || !equal(h.stored("decision-round", r.round), round) {
				t.Errorf("what is returned is not what is stored")
			}
		})
	}
}

// The round closes only when no card of it is open.
func TestCloseLeavesARoundOpenWhileACardIsOpen(t *testing.T) {
	h := newHome(t)
	first := h.fresh()
	second := rec(h.raiseIn(h.item(), first.round), "question")["question_id"].(string)
	h.archive(first.item)

	got := h.ok(h.closeOn(first.question, archivedEvidence(first.item)))
	round := rec(got, "round")
	if round["state"] != "open" || round["closed_at"] != nil || card(round, first.cardID)["state"] != "moot" {
		t.Errorf("round %v, card %v", round["state"], card(round, first.cardID))
	}
	// Closing the last open card closes the round.
	held := h.stored("question", second)["held_task_id"].(string)
	h.archive(held)
	got = h.ok(h.closeOn(second, archivedEvidence(held)))
	if round := rec(got, "round"); round["state"] != "closed" || round["closed_at"] == nil {
		t.Errorf("round %v", round)
	}
}

// A close on a replacing question: another question that is not moot holds the
// same item with its own word hold active, so the item stays held.
func TestCloseOnAReplacingQuestion(t *testing.T) {
	h := newHome(t)
	first := h.fresh()
	replacement := h.raisedOn(first.item)
	if replacement.hold == first.hold {
		t.Fatalf("the replacing question shares the first question's hold")
	}
	resp := h.closeOn(first.question, replacedEvidence(replacement.question))
	got := h.ok(resp)
	sameKinds(t, resp, "pool.question.mooted", "pool.hold.released")
	if q := rec(got, "question"); q["state"] != "moot" || q["closure"].(map[string]any)["evidence"].([]any)[0].(map[string]any)["kind"] != "question-replaced" {
		t.Errorf("question %v", q)
	}
	if hold := rec(got, "hold"); hold["hold_id"] != first.hold || hold["state"] != "released" {
		t.Errorf("hold %v", hold)
	}
	// The item stays held on the other question.
	if replaced := h.stored("question", replacement.question); replaced["state"] != "open" || h.stored("hold", replacement.hold)["state"] != "active" {
		t.Errorf("the replacing question or its hold changed: %v", replaced["state"])
	}
	if h.stored("item", first.item)["state"] == "archived" {
		t.Errorf("the item was archived")
	}
}

// Evidence that does not hold refuses the close with pool.question_not_moot,
// and nothing is written; so does a piece that does not hold among others that
// do.
func TestCloseIsRefusedOnEvidenceThatDoesNotHold(t *testing.T) {
	h := newHome(t)
	archivedWork := h.fresh()
	h.archive(archivedWork.item)

	cases := map[string]func() (question string, evidence []map[string]any){
		"work that is live": func() (string, []map[string]any) {
			r := h.fresh()
			return r.question, []map[string]any{archivedEvidence(r.item)}
		},
		"an item that is not the held item": func() (string, []map[string]any) {
			r := h.fresh()
			return r.question, []map[string]any{archivedEvidence(archivedWork.item)}
		},
		"a held item archived with live work beneath it": func() (string, []map[string]any) {
			prey := h.item()
			r := h.raisedOn(prey)
			h.child(prey, "hunt")
			h.archive(prey)
			return r.question, []map[string]any{archivedEvidence(prey)}
		},
		"a record that is not an item": func() (string, []map[string]any) {
			r := h.fresh()
			h.archive(r.item)
			return r.question, []map[string]any{{"kind": "item-archived", "ref": recordRef("pool.question", r.question)}}
		},
		"a question replaced by itself": func() (string, []map[string]any) {
			r := h.fresh()
			return r.question, []map[string]any{replacedEvidence(r.question)}
		},
		"a replacing question that does not exist": func() (string, []map[string]any) {
			r := h.fresh()
			return r.question, []map[string]any{replacedEvidence(mint(t, ids.Question))}
		},
		"a replacing question that holds another item": func() (string, []map[string]any) {
			r, other := h.fresh(), h.fresh()
			return r.question, []map[string]any{replacedEvidence(other.question)}
		},
		"a replacing question that is moot": func() (string, []map[string]any) {
			r := h.fresh()
			moot, last := h.raisedOn(r.item), h.raisedOn(r.item)
			h.ok(h.closeOn(moot.question, replacedEvidence(last.question)))
			return r.question, []map[string]any{replacedEvidence(moot.question)}
		},
		"a replacing question whose hold is released": func() (string, []map[string]any) {
			r := h.fresh()
			released := h.answeredOn(r.item)
			h.ok(h.release(released.question))
			return r.question, []map[string]any{replacedEvidence(released.question)}
		},
		"one piece that holds and one that does not": func() (string, []map[string]any) {
			r := h.fresh()
			other := h.raisedOn(r.item)
			return r.question, []map[string]any{replacedEvidence(other.question), archivedEvidence(r.item)}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			question, evidence := build()
			h.unchanged(func() {
				refused(t, h.closeOn(question, evidence...), envelope.Refused, "pool.question_not_moot")
			})
			if q := h.stored("question", question); q["state"] != "open" {
				t.Errorf("a refusal closed the question: %v", q["state"])
			}
		})
	}
}

// A question with an applied answer has the Hive Mind's word, and its
// disposition is the owner's: it is refused with pool.question_answered even
// on evidence that holds. One released, completed or moot is final.
func TestCloseIsRefusedOnAnAnsweredQuestionAndOnAFinalOne(t *testing.T) {
	h := newHome(t)

	answered := h.answeredOn(h.item())
	h.archive(answered.item)
	h.unchanged(func() {
		refused(t, h.closeOn(answered.question, archivedEvidence(answered.item)), envelope.Refused, "pool.question_answered")
	})

	released := h.answeredOn(h.item())
	h.ok(h.release(released.question))
	h.archive(released.item)
	completed := h.answeredOn(h.item())
	h.archive(completed.item)
	h.ok(h.complete(completed.question))
	moot := h.fresh()
	h.archive(moot.item)
	h.ok(h.closeOn(moot.question, archivedEvidence(moot.item)))
	for name, r := range map[string]raised{"released": released, "completed": completed, "moot": moot} {
		t.Run(name, func(t *testing.T) {
			h.unchanged(func() {
				refused(t, h.closeOn(r.question, archivedEvidence(r.item)), envelope.Refused, "pool.question_closed")
			})
		})
	}
}
