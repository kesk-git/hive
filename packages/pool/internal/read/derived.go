package read

import (
	"slices"
	"time"

	"github.com/kesk-git/hive/contract/clock"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/store"
)

// The derived facts of an item (spec/pool.md, Derived facts) are never stored:
// they are computed from the item records and the hold records at the moment
// of a read or of a guard, through each item's ancestors. This file holds the
// graph they are computed over, which the readers of the kinds item and hold
// and the operations that guard a write on them share. It reads item and hold
// records only: the effective-specification binding of an item is resolved
// from the item records, never from a specification record.

// Edge types of an item's deps.
const (
	EdgeBlockedBy      = "blocked-by"
	EdgeParent         = "parent"
	EdgeDiscoveredFrom = "discovered-from"
)

// Node is one item as the graph holds it: the record whole, with the members
// the facts read taken out of it.
type Node struct {
	ID       string
	Data     map[string]any
	Revision int64
	// Parent is the item's one parent edge, "" for none. An item with more
	// than one, which no write of the Pool leaves, is read through the first.
	Parent    string
	Level     string
	State     string
	Completed bool
}

// Hold is one hold record as the graph holds it.
type Hold struct {
	ID       string
	TaskID   string
	Kind     string
	State    string
	Data     map[string]any
	Revision int64
	// Until is the hold's date, the zero time for a hold with none.
	Until time.Time
}

// Graph is the items and the holds of a Pool at one instant.
type Graph struct {
	Items map[string]*Node
	// Order is the identities of the items in the order they were read.
	Order []string
	// Holds are the hold records by the item they are placed on, in the
	// order of their identities, whatever their state.
	Holds map[string][]*Hold
	// Now is the instant dated holds are read at.
	Now time.Time

	kids   map[string][]string
	landed map[string]bool
}

// LoadGraph reads every item and every hold of the Pool. The records that
// could not be read are returned beside the graph and are in none of it.
func LoadGraph(st *cstore.Store, now time.Time) (*Graph, []Omission, error) {
	items, omitted, err := store.LoadAll(st, "item")
	if err != nil {
		return nil, nil, err
	}
	holds, heldOmitted, err := store.LoadAll(st, "hold")
	if err != nil {
		return nil, nil, err
	}
	g := &Graph{Items: map[string]*Node{}, Holds: map[string][]*Hold{}, Now: now}
	for _, l := range items {
		g.Items[l.ID] = newNode(l)
		g.Order = append(g.Order, l.ID)
	}
	for _, l := range holds {
		h, err := newHold(l)
		if err != nil {
			omitted = append(omitted, Omission{ID: l.ID, Reason: store.ReasonMalformed})
			continue
		}
		g.Holds[h.TaskID] = append(g.Holds[h.TaskID], h)
	}
	g.index()
	return g, append(omitted, heldOmitted...), nil
}

func newNode(l store.Loaded) *Node {
	n := &Node{ID: l.ID, Data: l.Data, Revision: l.Revision}
	n.Level, _ = l.Data["level"].(string)
	n.State, _ = l.Data["state"].(string)
	n.Completed, _ = l.Data["completed"].(bool)
	for _, e := range Edges(l.Data) {
		if e.Type == EdgeParent {
			n.Parent = e.TaskID
			break
		}
	}
	return n
}

// newHold reads a hold record. A date it holds that is not a real instant is
// an error: a hold whose date cannot be read is not read as one with none.
func newHold(l store.Loaded) (*Hold, error) {
	h := &Hold{ID: l.ID, Data: l.Data, Revision: l.Revision}
	h.TaskID, _ = l.Data["task_id"].(string)
	h.Kind, _ = l.Data["kind"].(string)
	h.State, _ = l.Data["state"].(string)
	if until, ok := l.Data["until"].(map[string]any); ok {
		if at, ok := until["at"].(string); ok {
			var err error
			if h.Until, err = clock.Parse(at); err != nil {
				return nil, err
			}
		}
	}
	return h, nil
}

// Replace returns the graph as it would stand with the item's record
// replaced by data, which a write checks its rules against before it writes.
func (g *Graph) Replace(id string, data map[string]any) *Graph {
	out := &Graph{Items: make(map[string]*Node, len(g.Items)+1), Holds: g.Holds, Now: g.Now, Order: g.Order}
	for k, v := range g.Items {
		out.Items[k] = v
	}
	rev := int64(0)
	if old, ok := g.Items[id]; ok {
		rev = old.Revision
	} else {
		out.Order = append(slices.Clone(g.Order), id)
	}
	out.Items[id] = newNode(store.Loaded{ID: id, Data: data, Revision: rev})
	out.index()
	return out
}

func (g *Graph) index() {
	g.kids = map[string][]string{}
	g.landed = map[string]bool{}
	for _, id := range g.Order {
		if p := g.Items[id].Parent; p != "" {
			if _, ok := g.Items[p]; ok {
				g.kids[p] = append(g.kids[p], id)
			}
		}
	}
}

// Edge is one dependency of an item.
type Edge struct{ Type, TaskID, Reason string }

// Edges are the deps of an item record, in order.
func Edges(data map[string]any) []Edge {
	var out []Edge
	list, _ := data["deps"].([]any)
	for _, v := range list {
		m, _ := v.(map[string]any)
		e := Edge{}
		e.Type, _ = m["type"].(string)
		e.TaskID, _ = m["task_id"].(string)
		e.Reason, _ = m["reason"].(string)
		out = append(out, e)
	}
	return out
}

// Ancestors are the items above the item through its parent edges, nearest
// first. An edge to an item that does not exist, and a loop, end the chain.
func (g *Graph) Ancestors(id string) []string {
	var out []string
	seen := map[string]bool{id: true}
	for n := g.Items[id]; n != nil && n.Parent != ""; {
		next := g.Items[n.Parent]
		if next == nil || seen[next.ID] {
			break
		}
		seen[next.ID] = true
		out = append(out, next.ID)
		n = next
	}
	return out
}

// Children are the items whose parent edge names the item.
func (g *Graph) Children(id string) []string { return g.kids[id] }

// Beneath are the items below the item at any depth, whatever their level.
func (g *Graph) Beneath(id string) []string {
	var out []string
	seen := map[string]bool{id: true}
	var walk func(string)
	walk = func(at string) {
		for _, k := range g.kids[at] {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
				walk(k)
			}
		}
	}
	walk(id)
	return out
}

// levelBelow is the level of the items a level lands from.
func levelBelow(level string) string {
	switch level {
	case "target":
		return "prey"
	case "prey":
		return "hunt"
	}
	return ""
}

// Below are the items the item lands from: the Prey that point at a Target and
// the Hunts that point at a Prey. A Hunt, and an item with no level, has none.
func (g *Graph) Below(id string) []string {
	n := g.Items[id]
	if n == nil {
		return nil
	}
	want := levelBelow(n.Level)
	var out []string
	for _, k := range g.kids[id] {
		if want != "" && g.Items[k].Level == want {
			out = append(out, k)
		}
	}
	return out
}

// HasHunt reports whether a Hunt points at the item.
func (g *Graph) HasHunt(id string) bool {
	n := g.Items[id]
	return n != nil && n.Level == "prey" && len(g.Below(id)) > 0
}

// Derived reports whether the item's landing is derived from the items
// beneath it, which is so of a Target and of a Prey that has a Hunt. Such an
// item is never set done and never dispatched.
func (g *Graph) Derived(id string) bool {
	n := g.Items[id]
	return n != nil && (n.Level == "target" || g.HasHunt(id))
}

// Landed is the effective completion of the item: a leaf has landed when it
// carries the completion marker; a Prey with Hunts when at least one Hunt
// points at it and every Hunt has landed; a Target when at least one Prey
// points at it and every Prey has landed.
func (g *Graph) Landed(id string) bool {
	if v, ok := g.landed[id]; ok {
		return v
	}
	n := g.Items[id]
	if n == nil {
		return false
	}
	var v bool
	if n.Level == "target" || g.HasHunt(id) {
		below := g.Below(id)
		v = len(below) > 0
		for _, b := range below {
			v = v && g.Landed(b)
		}
	} else {
		v = n.Completed
	}
	g.landed[id] = v
	return v
}

// InForce reports whether the hold holds at the instant of the graph: it is
// active as a record and, where it is dated and not a word hold, its date has
// not come.
func (g *Graph) InForce(h *Hold) bool {
	if h.State != "active" {
		return false
	}
	if h.Kind == "word" || h.Until.IsZero() {
		return true
	}
	return g.Now.Before(h.Until)
}

// Facts are the derived facts of one item.
type Facts struct {
	Blocked, Held, Ready, Landed bool
	// Building is true of a Target with an item in flight beneath it.
	Building bool
	// Blockers are the items that block it: those its own blocked-by edges
	// and its ancestors' name and that have not landed.
	Blockers []string
	// Holds are the holds in force on it and on its ancestors, its own
	// first and then each ancestor's, nearest first.
	Holds []*Hold
}

// Facts computes the derived facts of the item.
func (g *Graph) Facts(id string) Facts {
	n := g.Items[id]
	if n == nil {
		return Facts{}
	}
	f := Facts{Landed: g.Landed(id)}
	chain := append([]string{id}, g.Ancestors(id)...)
	if !f.Landed {
		for _, a := range chain {
			for _, e := range Edges(g.Items[a].Data) {
				if e.Type != EdgeBlockedBy {
					continue
				}
				// An edge to an item that does not exist is resolved.
				if _, ok := g.Items[e.TaskID]; ok && !g.Landed(e.TaskID) && !slices.Contains(f.Blockers, e.TaskID) {
					f.Blockers = append(f.Blockers, e.TaskID)
				}
			}
		}
		f.Blocked = len(f.Blockers) > 0
	}
	for _, a := range chain {
		for _, h := range g.Holds[a] {
			if g.InForce(h) {
				f.Holds = append(f.Holds, h)
			}
		}
	}
	f.Held = len(f.Holds) > 0
	deliverable, _ := n.Data["deliverable"].(string)
	f.Ready = n.State == "queued" && !f.Blocked && !f.Held && deliverable != "decision" && !g.Derived(id)
	if n.Level == "target" {
		for _, b := range g.Beneath(id) {
			if g.Items[b].State == "in-flight" {
				f.Building = true
			}
		}
	}
	return f
}

// Needs reports whether the item needs an accepted specification: its own
// specification_required is true, or the item it resolves through, its parent,
// needs one.
func (g *Graph) Needs(id string) bool {
	for _, a := range append([]string{id}, g.Ancestors(id)...) {
		if required, _ := g.Items[a].Data["specification_required"].(bool); required {
			return true
		}
	}
	return false
}

// Effective is the item's effective specification binding: its own accepted
// binding where it has one, and otherwise its parent's, read from the item
// records alone. It is nil for an item with none on its path to the top.
func (g *Graph) Effective(id string) map[string]any {
	for _, a := range append([]string{id}, g.Ancestors(id)...) {
		if spec, ok := g.Items[a].Data["specification"].(map[string]any); ok {
			return spec
		}
	}
	return nil
}

// OpenGeneration reports whether a generation of the item is open.
func OpenGeneration(data map[string]any) bool {
	list, _ := data["generations"].([]any)
	for _, v := range list {
		if m, ok := v.(map[string]any); ok && m["state"] == "open" {
			return true
		}
	}
	return false
}

// Generations is the number of generations ever opened on the item.
func Generations(data map[string]any) int {
	list, _ := data["generations"].([]any)
	return len(list)
}

// Dependents are the items whose own blocked-by edges name the item.
func (g *Graph) Dependents(id string) []string {
	var out []string
	for _, other := range g.Order {
		for _, e := range Edges(g.Items[other].Data) {
			if e.Type == EdgeBlockedBy && e.TaskID == id {
				out = append(out, other)
				break
			}
		}
	}
	return out
}
