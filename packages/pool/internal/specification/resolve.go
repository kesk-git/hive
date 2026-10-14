package specification

import "github.com/kesk-git/hive/packages/pool/internal/read"

// resolvers are the items that resolve their effective specification through
// the item (spec/pool.md, Specifications): those beneath it at any depth that
// have no binding of their own on the way down. An item with a binding of its
// own is not one, and nothing beneath it resolves through the item.
func resolvers(g *read.Graph, root string) []string {
	var out []string
	seen := map[string]bool{root: true}
	var walk func(string)
	walk = func(at string) {
		for _, k := range g.Children(at) {
			if seen[k] {
				continue
			}
			seen[k] = true
			if _, _, _, bound := bindingOf(g.Items[k].Data); bound {
				continue
			}
			out = append(out, k)
			walk(k)
		}
	}
	walk(root)
	return out
}

// dispatchable reports whether the item is one an invalidation reaches: a Hunt
// or a Prey with no Hunt, which has not landed and is not retired.
func dispatchable(g *read.Graph, id string) bool {
	n := g.Items[id]
	if n == nil || (n.Level != "hunt" && n.Level != "prey") || n.State == "archived" {
		return false
	}
	return !g.Derived(id) && !g.Landed(id) && n.Data["deliverable"] != "decision"
}
