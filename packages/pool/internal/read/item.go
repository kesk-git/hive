package read

import (
	"errors"
	"slices"

	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/store"
)

// The readers of the kinds item and hold (spec/pool.md, Reading the Pool).
// Each returns whole records. An item comes with its derived facts, computed
// at the read through its ancestors; a hold comes as it is stored.

// ItemMembers are the selector members of a read of kind item, as the request
// schema and spec/pool.md attribute them to items: the identity, the project,
// the states, the ready predicate and the limit.
var ItemMembers = []string{MemberID, MemberProjectID, MemberStates, MemberReady, MemberLimit}

// HoldMembers are the selector members of a read of kind hold: the identity,
// the item held, the project, the states, the active predicate, the due time
// and the limit. A hold record carries the project of its item, as an item does.
var HoldMembers = []string{MemberID, MemberTaskID, MemberProjectID, MemberStates, MemberActive, MemberDueBefore, MemberLimit}

// RegisterItemReaders registers the readers of the kinds item and hold.
func RegisterItemReaders(r *Registry) {
	r.Register(Kind{
		Name:    "item",
		Members: ItemMembers,
		// Whatever the selector, the derived facts of an item read other
		// records, so every read of an item takes the lock shared.
		Read: readItems,
	})
	r.Register(Kind{
		Name:       "hold",
		Members:    HoldMembers,
		Standalone: func(s Selector) bool { return s.ID != "" },
		Read:       readHolds,
	})
}

func readItems(c *Context) (*Result, error) {
	sel := c.Selector
	for _, state := range sel.States {
		switch state {
		case "queued", "in-flight", "done", "archived":
		default:
			return nil, envelope.Common(envelope.CodeSchemaViolation, "selector.states must name only queued, in-flight, done or archived item states")
		}
	}
	g, omitted, err := LoadGraph(c.Store, c.Call.Clock.Now())
	if err != nil {
		return nil, err
	}
	res := &Result{}
	evaluated := false
	for _, id := range g.Order {
		n := g.Items[id]
		if sel.ID != "" && id != sel.ID {
			continue
		}
		if sel.Has(MemberProjectID) && n.Data["project_id"] != sel.ProjectID {
			continue
		}
		if sel.Has(MemberStates) && !slices.Contains(sel.States, n.State) {
			continue
		}
		f := g.Facts(id)
		evaluated = true
		if sel.Has(MemberReady) && f.Ready != sel.Ready {
			continue
		}
		res.Records = append(res.Records, Record{ID: id, Data: n.Data, Derived: derivedFacts(id, n, f)})
	}
	// The facts of an item read the records above it, so a record that could
	// not be read makes the read incomplete wherever the facts of an item were
	// computed, whether or not its facts then kept it; one the read is not
	// about does not make an identity that is absent anything but absent.
	for _, o := range omitted {
		if evaluated || sel.ID == "" || o.ID == sel.ID {
			res.Omitted = append(res.Omitted, o)
		}
	}
	return res, nil
}

// derivedFacts is the derived member of the response for one item.
func derivedFacts(id string, n *Node, f Facts) map[string]any {
	blockers := make([]any, len(f.Blockers))
	for i, b := range f.Blockers {
		blockers[i] = b
	}
	holds := make([]any, len(f.Holds))
	for i, h := range f.Holds {
		holds[i] = h.ID
	}
	d := map[string]any{
		"task_id": id, "blocked": f.Blocked, "held": f.Held, "ready": f.Ready, "landed": f.Landed,
		"active_blockers": blockers, "active_holds": holds,
	}
	if n.Level == "target" {
		d["building"] = f.Building
	}
	return d
}

func readHolds(c *Context) (*Result, error) {
	sel := c.Selector
	res := &Result{}
	if sel.Has(MemberDueBefore) && !clock.Valid(sel.DueBefore) {
		return nil, envelope.Common(envelope.CodeSchemaViolation, "due_before is not a calendar instant")
	}
	keep := func(h *Hold, l store.Loaded) bool {
		switch {
		case sel.Has(MemberTaskID) && h.TaskID != sel.TaskID:
			return false
		case sel.Has(MemberProjectID) && l.Data["project_id"] != sel.ProjectID:
			return false
		case sel.Has(MemberStates) && !slices.Contains(sel.States, h.State):
			return false
		case sel.Has(MemberActive) && (h.State == "active") != sel.Active:
			return false
		case sel.Has(MemberDueBefore):
			due, _ := clock.Parse(sel.DueBefore)
			if h.Until.IsZero() || h.Until.After(due) {
				return false
			}
		}
		return true
	}
	if sel.ID != "" {
		l, om, err := store.Load(c.Store, "hold", sel.ID)
		switch {
		case errors.Is(err, store.ErrNotFound):
		case err != nil:
			return nil, err
		case om != nil:
			res.Omitted = append(res.Omitted, *om)
		default:
			h, err := newHold(*l)
			switch {
			case err != nil:
				res.Omitted = append(res.Omitted, Omission{ID: l.ID, Reason: store.ReasonMalformed})
			case keep(h, *l):
				res.Records = append(res.Records, Record{ID: l.ID, Data: l.Data})
			}
		}
		return res, nil
	}
	all, omitted, err := store.LoadAll(c.Store, "hold")
	if err != nil {
		return nil, err
	}
	for _, l := range all {
		h, err := newHold(l)
		switch {
		case err != nil:
			omitted = append(omitted, Omission{ID: l.ID, Reason: store.ReasonMalformed})
		case keep(h, l):
			res.Records = append(res.Records, Record{ID: l.ID, Data: l.Data})
		}
	}
	res.Omitted = omitted
	return res, nil
}
