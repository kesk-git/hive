package decision_test

import (
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
)

// The acceptance test of hive-pool complete-question (spec/word.md, release and
// complete): the owner's disposition completed on an applied answer, which
// closes the question and releases the hold only where the held item and every
// item beneath it are each archived or a decision item, as they stand.

// A question completed on an item with nothing left to gate records the
// disposition completed and releases the hold with the completion's receipt.
func TestCompleteReleasesTheHoldWhereNothingIsLeftToGate(t *testing.T) {
	// ready makes the held item and everything beneath it archived or a
	// decision item, and returns the question.
	for _, tc := range []struct {
		name  string
		ready func(h *home) raised
	}{
		{"archived work", func(h *home) raised {
			r := h.answeredOn(h.item())
			h.archive(r.item)
			return r
		}},
		{"a decision item", func(h *home) raised { return h.decided() }},
		{"a decision item with archived work beneath it", func(h *home) raised {
			r := h.decided()
			h.archive(h.beneath(r.item))
			return r
		}},
		{"a decision item with a decision item beneath it", func(h *home) raised {
			r, other := h.decided(), h.decided()
			beneath := h.stored("item", other.item)
			beneath["deps"] = []any{map[string]any{"type": "parent", "task_id": r.item}}
			h.plant("item", other.item, beneath)
			return r
		}},
		{"a Prey and its Hunt, both archived", func(h *home) raised {
			prey := h.item()
			r := h.answeredOn(prey)
			h.archive(h.child(prey, "hunt"))
			h.archive(prey)
			return r
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHome(t)
			r := tc.ready(h)
			question, round := h.stored("question", r.question), h.stored("decision-round", r.round)

			body := disposeBody(r.question)
			body["evidence"] = []any{recordRef("pool.item", r.item)}
			resp := h.send("complete-question", body, h.atRevision(r.question))
			got := h.ok(resp)
			sameKinds(t, resp, "pool.question.completed", "pool.hold.released")
			receipt := receiptOf(t, resp, "pool.question.completed")

			q := rec(got, "question")
			d, _ := q["disposition"].(map[string]any)
			if q["state"] != "completed" || q["record_revision"] != question["record_revision"].(int64)+1 {
				t.Fatalf("question %v at revision %v", q["state"], q["record_revision"])
			}
			if d["kind"] != "completed" || d["answer_sequence"] != int64(1) || d["note"] != body["note"] || d["request_id"] != resp.RequestID ||
				d["receipt_id"] != receipt || len(d["evidence"].([]any)) != 1 {
				t.Errorf("disposition %v", d)
			}
			if !equal(q["applied"], question["applied"]) || !equal(q["answers"], question["answers"]) {
				t.Errorf("the applied answer changed")
			}
			hold := rec(got, "hold")
			by, _ := hold["released_by"].(map[string]any)
			if hold["hold_id"] != r.hold || hold["state"] != "released" || hold["released_at"] == nil || by["schema"] != "hive.receipt" || by["id"] != receipt {
				t.Errorf("hold %v", hold)
			}
			if !equal(rec(got, "round"), round) {
				t.Errorf("round changed: %v", rec(got, "round"))
			}
			if !equal(h.stored("question", r.question), q) || !equal(h.stored("hold", r.hold), hold) {
				t.Errorf("what is returned is not what is stored")
			}
		})
	}
}

// A question whose held item, or an item beneath it, is live work is refused
// with pool.question_holds_work and nothing is written; once the work is
// archived the question is completed.
func TestCompleteIsRefusedWhileLiveWorkIsLeftToGate(t *testing.T) {
	for _, tc := range []struct {
		name string
		// make returns the question and the work to archive for it to complete.
		make func(h *home) (raised, []string)
	}{
		{"live work held", func(h *home) (raised, []string) {
			r := h.answeredOn(h.item())
			return r, []string{r.item}
		}},
		{"a Hunt live beneath an archived Prey", func(h *home) (raised, []string) {
			prey := h.item()
			r := h.answeredOn(prey)
			hunt := h.child(prey, "hunt")
			h.archive(prey)
			return r, []string{hunt}
		}},
		{"a Hunt live beneath a live Prey", func(h *home) (raised, []string) {
			prey := h.item()
			r := h.answeredOn(prey)
			hunt := h.child(prey, "hunt")
			return r, []string{hunt, prey}
		}},
		{"a decision item since given work", func(h *home) (raised, []string) {
			r := h.decided()
			h.ok(h.do("update", map[string]any{"task_id": r.item, "patch": map[string]any{"deliverable": "swarm", "level": "prey"}}))
			return r, []string{r.item}
		}},
		{"a live item beneath a held decision item", func(h *home) (raised, []string) {
			r := h.decided()
			return r, []string{h.beneath(r.item)}
		}},
		{"a live item beneath a held decision item, with archived work beside it", func(h *home) (raised, []string) {
			r := h.decided()
			h.archive(h.beneath(r.item))
			return r, []string{h.beneath(r.item)}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHome(t)
			r, work := tc.make(h)
			h.unchanged(func() {
				refused(t, h.complete(r.question), envelope.Refused, "pool.question_holds_work")
			})
			if q := h.stored("question", r.question); q["state"] != "answered" || h.stored("hold", r.hold)["state"] != "active" {
				t.Errorf("a refusal settled the question: %v", q["state"])
			}
			for _, id := range work {
				h.archive(id)
			}
			got := h.ok(h.complete(r.question))
			if rec(got, "question")["state"] != "completed" || rec(got, "hold")["state"] != "released" {
				t.Errorf("completed %v", got)
			}
		})
	}
}
