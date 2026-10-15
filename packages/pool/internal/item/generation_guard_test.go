package item_test

import (
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// The guards of open-generation (spec/pool.md, Generations and State): the
// dispatch guards of the transition table at every open, for an in-flight item
// as for a queued one, the two that only an open applies, and the admission
// the open requires.

// work is a Hunt with the Prey and the Target above it, the Hunt prepared with
// an Imperative that names no criteria.
type work struct{ target, prey, hunt string }

func (h *home) work() work {
	h.t.Helper()
	w := work{target: h.addID(target("A Target"))}
	w.prey = h.addID(under("prey", "A Prey", w.target))
	body := under("hunt", "A Hunt", w.prey)
	body["deliverable"] = "swarm"
	w.hunt = h.addID(body)
	h.imperative(w.hunt, nil)
	return w
}

// bind gives the item the accepted specification as hive-spec accept leaves it.
func (h *home) bind(id string) map[string]any {
	h.t.Helper()
	binding := map[string]any{"specification_id": mint(h.t, ids.Spec), "revision": int64(1), "digest": "sha256:" + strings.Repeat("a", 64)}
	rec := h.stored("item", id)
	rec["specification_required"], rec["definition"], rec["specification"] = true, "accepted", binding
	h.plant("item", id, rec)
	return binding
}

// failedOnce opens a generation of the item and closes it failed, which leaves
// the item in flight.
func (h *home) failedOnce(id string) {
	h.t.Helper()
	h.opened(id)
	h.ok(h.closeGeneration(id, 1, "failed"))
	if record, _ := h.item(id); record["state"] != "in-flight" {
		h.t.Fatalf("the item is %v", record["state"])
	}
}

var past = map[string]any{"at": "2026-10-14T08:00:00.000Z", "zone": "UTC"}

// Every dispatch guard refuses an open with the code the table gives, for a
// queued item and for an in-flight one that a failed generation left so, and a
// refusal changes nothing.
func TestOpenGenerationAppliesTheDispatchGuardsAtEveryOpen(t *testing.T) {
	for _, tc := range []struct {
		name    string
		arrange func(h *home, w work)
		status  envelope.Status
		code    string
	}{
		{"an active hold on the item", func(h *home, w work) { h.hold(w.hunt, "parked") }, envelope.Held, envelope.CodeHeld},
		{"an active hold on an ancestor", func(h *home, w work) { h.hold(w.prey, "parked") }, envelope.Held, envelope.CodeHeld},
		{"an active hold on the Target above", func(h *home, w work) { h.hold(w.target, "parked") }, envelope.Held, envelope.CodeHeld},
		{"a word hold whatever its date", func(h *home, w work) { h.plantHold(w.hunt, "word", past) }, envelope.Held, envelope.CodeHeld},
		{"a blocker of the item that has not landed", func(h *home, w work) {
			h.ok(h.do("update", map[string]any{"task_id": w.hunt, "patch": map[string]any{"add_deps": blockedBy(h.addID(leaf("A blocker")))["deps"]}}))
		}, envelope.Refused, "pool.blocked"},
		{"a blocker of an ancestor that has not landed", func(h *home, w work) {
			h.ok(h.do("update", map[string]any{"task_id": w.prey, "patch": map[string]any{"add_deps": blockedBy(h.addID(leaf("A blocker")))["deps"]}}))
		}, envelope.Refused, "pool.blocked"},
		{"a required specification that has no effective specification", func(h *home, w work) {
			rec := h.stored("item", w.hunt)
			rec["specification_required"] = true
			h.plant("item", w.hunt, rec)
		}, envelope.Refused, "pool.specification_unaccepted"},
		{"an Imperative whose criteria differ from the effective specification", func(h *home, w work) {
			h.bind(w.target)
			other := h.bind(h.addID(target("Another Target")))
			h.imperative(w.hunt, other)
		}, envelope.Refused, "pool.imperative_stale"},
		{"an Imperative that names no criteria for an item that has an effective specification", func(h *home, w work) {
			h.bind(w.target)
		}, envelope.Refused, "pool.imperative_stale"},
	} {
		for _, from := range []string{"queued", "in-flight"} {
			t.Run(tc.name+", "+from, func(t *testing.T) {
				h := newHome(t)
				w := h.work()
				if from == "in-flight" {
					h.failedOnce(w.hunt)
				}
				tc.arrange(h, w)
				admission := h.admit(w.hunt)
				h.unchanged(func() {
					refused(t, h.open(w.hunt, admission), tc.status, tc.code)
				})
				if record, _ := h.item(w.hunt); record["state"] != from {
					t.Errorf("the refusal left the item %v", record["state"])
				}
			})
		}
	}
}

// What the guards allow opens: criteria that agree with the effective
// specification, the item's own or the one it resolves through its parent, a
// dated hold that is not a word hold once its date has passed, and a blocker
// that has landed.
func TestOpenGenerationPassesWhatTheGuardsAllow(t *testing.T) {
	t.Run("criteria that agree with the specification of a Target above", func(t *testing.T) {
		h := newHome(t)
		w := h.work()
		h.imperative(w.hunt, h.bind(w.target))
		if g := h.opened(w.hunt); g["generation"] != int64(1) {
			t.Errorf("the generation is %v", g)
		}
	})
	t.Run("criteria that agree with the specification of the item", func(t *testing.T) {
		h := newHome(t)
		w := h.work()
		h.imperative(w.hunt, h.bind(w.hunt))
		h.opened(w.hunt)
	})
	t.Run("a dated hold past its date", func(t *testing.T) {
		h := newHome(t)
		w := h.work()
		h.plantHold(w.hunt, "future", past)
		h.opened(w.hunt)
	})
	t.Run("a blocker that has landed", func(t *testing.T) {
		h := newHome(t)
		w := h.work()
		blocker := h.addID(leaf("A blocker"))
		h.ok(h.do("update", map[string]any{"task_id": w.hunt, "patch": map[string]any{"add_deps": blockedBy(blocker)["deps"]}}))
		h.move(blocker, "done")
		h.opened(w.hunt)
	})
	t.Run("a Prey with no Hunt beneath it", func(t *testing.T) {
		h := newHome(t)
		id := h.addID(leaf("A leaf"))
		h.imperative(id, nil)
		h.opened(id)
	})
}

// A failed generation that left the item in flight, followed by the reopening of
// a blocker, cannot open another until the blocker lands again.
func TestAFailedGenerationFollowedByAReopenedBlockerCannotOpenAnotherUntilItLandsAgain(t *testing.T) {
	h := newHome(t)
	blocker := h.addID(leaf("A blocker"))
	id := h.addID(hunting("A leaf", blockedBy(blocker)))
	h.imperative(id, nil)
	h.move(blocker, "done")
	h.failedOnce(id)

	h.move(blocker, "queued")
	admission := h.admit(id)
	h.unchanged(func() {
		refused(t, h.open(id, admission), envelope.Refused, "pool.blocked")
	})
	h.move(blocker, "done")
	if g := h.ok(h.open(id, admission))["generation"].(map[string]any); g["generation"] != int64(2) {
		t.Errorf("the generation is %v", g)
	}
}

// A decision item, a Target and a Prey that has a Hunt beneath it are never
// dispatched, whatever the admission they hold: the Hunt beneath the Prey is.
func TestOpenGenerationRefusesWhatIsNeverDispatched(t *testing.T) {
	h := newHome(t)
	w := h.work()
	decision := h.addID(map[string]any{"title": "A decision", "deliverable": "decision"})
	h.imperative(decision, nil)
	for name, id := range map[string]string{"a decision item": decision, "a Target": w.target, "a Prey with a Hunt beneath it": w.prey} {
		t.Run(name, func(t *testing.T) {
			h.imperative(id, nil)
			admission := h.admit(id)
			h.unchanged(func() {
				refused(t, h.open(id, admission), envelope.Refused, "pool.not_dispatchable")
			})
			if len(h.generations(id)) != 0 {
				t.Error("a generation was opened")
			}
		})
	}
	if g := h.opened(w.hunt); g["generation"] != int64(1) {
		t.Errorf("the Hunt beneath opened %v", g)
	}
}

// An open requires an admission in state admitted, of the item, whose last check
// read the item's current revision and the Imperative's current revision;
// anything else is pool.admission_stale and the caller checks again.
func TestOpenGenerationRequiresAnAdmittedAdmissionThatReadTheCurrentRecords(t *testing.T) {
	setup := func(t *testing.T) (*home, string) {
		h := newHome(t)
		id := h.addID(leaf("A leaf"))
		h.imperative(id, nil)
		return h, id
	}
	stale := func(t *testing.T, h *home, id, admission string) {
		t.Helper()
		h.unchanged(func() {
			refused(t, h.open(id, admission), envelope.Conflict, "pool.admission_stale")
		})
		if record, _ := h.item(id); record["state"] != "queued" || len(h.generations(id)) != 0 {
			t.Errorf("the refusal left the item %v with %d generations", record["state"], len(h.generations(id)))
		}
	}
	for _, name := range []string{"created-no-checks", "refused-codex-cloud-hive", "held-field-guess-workspace"} {
		t.Run("an admission "+strings.SplitN(name, "-", 2)[0], func(t *testing.T) {
			h, id := setup(t)
			stale(t, h, id, h.admitAs(name, id))
		})
	}
	t.Run("an admission of another item", func(t *testing.T) {
		h, id := setup(t)
		other := h.addID(leaf("Another leaf"))
		h.imperative(other, nil)
		stale(t, h, id, h.admit(other))
	})
	t.Run("a last check that read an older revision of the item", func(t *testing.T) {
		h, id := setup(t)
		h.ok(h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"priority": int64(2)}}))
		stale(t, h, id, h.admit(id, func(_, check map[string]any) { check["item_revision"] = h.revisionOf("item", id) - 1 }))
	})
	t.Run("a last check that read an older revision of the Imperative", func(t *testing.T) {
		h, id := setup(t)
		h.reviseImperative(id)
		stale(t, h, id, h.admit(id, func(_, check map[string]any) { check["imperative_revision"] = int64(1) }))
	})
	t.Run("an item changed since the check", func(t *testing.T) {
		h, id := setup(t)
		admission := h.admit(id)
		h.ok(h.do("update", map[string]any{"task_id": id, "patch": map[string]any{"priority": int64(2)}}))
		stale(t, h, id, admission)
		h.ok(h.open(id, h.admit(id)))
	})
	t.Run("an Imperative revised since the check", func(t *testing.T) {
		h, id := setup(t)
		admission := h.admit(id)
		h.reviseImperative(id)
		stale(t, h, id, admission)
		g := h.ok(h.open(id, h.admit(id)))["generation"].(map[string]any)
		if g["imperative_revision"] != int64(2) {
			t.Errorf("the generation runs under Imperative revision %v", g["imperative_revision"])
		}
	})
	t.Run("an admission that does not exist", func(t *testing.T) {
		h, id := setup(t)
		h.unchanged(func() {
			refused(t, h.open(id, mint(t, ids.Admission)), envelope.NotFound, envelope.CodeNotFound)
		})
	})
}
