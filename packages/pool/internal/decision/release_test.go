package decision_test

import (
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
)

// The acceptance test of hive-pool release-question (spec/word.md, release and
// complete): the owner's disposition released on an applied answer, which
// releases the word hold with the release's receipt, and the refusal on a
// decision item that gates no work. It drives the executable the default
// registry describes, in process, over a home of its own.

// An answer alone releases nothing; the release records the disposition on the
// applied answer with the owner's note and evidence and releases the hold in
// the same transaction, with its receipt as the hold's released_by.
func TestReleaseFreesTheWorkOnlyOnAnAppliedAnswer(t *testing.T) {
	h := newHome(t)
	r := h.answeredOn(h.item())
	if hold := h.stored("hold", r.hold); hold["state"] != "active" {
		t.Fatalf("an answer released the hold: %v", hold["state"])
	}
	question, round, item := h.stored("question", r.question), h.stored("decision-round", r.round), h.stored("item", r.item)

	body := disposeBody(r.question)
	body["evidence"] = []any{recordRef("pool.item", r.item)}
	resp := h.send("release-question", body, sent{expectedRevs: []rev{{"pool.question", r.question, 2}}})
	got := h.ok(resp)
	sameKinds(t, resp, "pool.question.released", "pool.hold.released")
	receipt := receiptOf(t, resp, "pool.question.released")

	q := rec(got, "question")
	if q["state"] != "released" || q["record_revision"] != int64(3) {
		t.Fatalf("question %v at revision %v", q["state"], q["record_revision"])
	}
	d := q["disposition"].(map[string]any)
	if d["kind"] != "released" || d["answer_sequence"] != int64(1) || d["note"] != body["note"] || d["request_id"] != resp.RequestID ||
		d["receipt_id"] != receipt || d["by"].(map[string]any)["kind"] != "operator" || d["at"] == nil {
		t.Errorf("disposition %v", d)
	}
	if ev := d["evidence"].([]any); len(ev) != 1 || !equal(ev[0], recordRef("pool.item", r.item)) {
		t.Errorf("evidence %v", ev)
	}
	for _, m := range []string{"answers", "applied", "revisions", "hold_id", "held_task_id", "card_id", "decision_round_id"} {
		if !equal(q[m], question[m]) {
			t.Errorf("%s changed from %v to %v", m, question[m], q[m])
		}
	}

	hold := rec(got, "hold")
	by, _ := hold["released_by"].(map[string]any)
	if hold["hold_id"] != r.hold || hold["state"] != "released" || hold["record_revision"] != int64(2) || hold["released_at"] == nil ||
		by["schema"] != "hive.receipt" || by["id"] != receipt {
		t.Errorf("hold %v", hold)
	}

	// The card stays answered: the round is the one the answer closed.
	if !equal(rec(got, "round"), round) {
		t.Errorf("round changed: %v", rec(got, "round"))
	}
	if !equal(h.stored("item", r.item), item) {
		t.Errorf("the item changed")
	}
	// What is returned is what is stored.
	if !equal(h.stored("question", r.question), q) || !equal(h.stored("hold", r.hold), hold) {
		t.Errorf("what is returned is not what is stored")
	}
}

// A question whose held item, as it stands, is a decision item with no live
// work beneath it gates no work and is completed instead: pool.decision_item,
// and nothing is written. Live work beneath it, which a decision item may come
// to carry, is released.
func TestReleaseIsRefusedOnADecisionItemWithNoLiveWorkBeneath(t *testing.T) {
	t.Run("a decision item with nothing beneath it", func(t *testing.T) {
		h := newHome(t)
		r := h.decided()
		h.unchanged(func() {
			refused(t, h.release(r.question), envelope.Refused, "pool.decision_item")
		})
		// It is completed instead.
		got := h.ok(h.complete(r.question))
		if rec(got, "question")["state"] != "completed" || rec(got, "hold")["state"] != "released" {
			t.Errorf("completed %v", got)
		}
	})
	t.Run("a decision item with only archived work beneath it", func(t *testing.T) {
		h := newHome(t)
		r := h.decided()
		h.archive(h.beneath(r.item))
		h.unchanged(func() {
			refused(t, h.release(r.question), envelope.Refused, "pool.decision_item")
		})
	})
	t.Run("a decision item with a decision item beneath it", func(t *testing.T) {
		h := newHome(t)
		r := h.decided()
		other := h.decided()
		beneath := h.stored("item", other.item)
		beneath["deps"] = []any{map[string]any{"type": "parent", "task_id": r.item}}
		h.plant("item", other.item, beneath)
		h.unchanged(func() {
			refused(t, h.release(r.question), envelope.Refused, "pool.decision_item")
		})
	})
	t.Run("a decision item with live work beneath it is released", func(t *testing.T) {
		h := newHome(t)
		r := h.decided()
		h.beneath(r.item)
		got := h.ok(h.release(r.question))
		if rec(got, "question")["state"] != "released" || rec(got, "hold")["state"] != "released" {
			t.Errorf("released %v", got)
		}
	})
	t.Run("a decision item since given work is released", func(t *testing.T) {
		h := newHome(t)
		r := h.decided()
		h.ok(h.do("update", map[string]any{"task_id": r.item, "patch": map[string]any{"deliverable": "swarm", "level": "prey"}}))
		got := h.ok(h.release(r.question))
		if rec(got, "question")["state"] != "released" || rec(got, "hold")["state"] != "released" {
			t.Errorf("released %v", got)
		}
	})
}
