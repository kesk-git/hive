package declare

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// PackageNames are the seven owners of spec/packages.md.
var PackageNames = []string{"pool", "spawning-pool", "dispatch", "shadow", "synapse", "word", "ui"}

// The five programs Hive needs, which a platform utility is never one of
// (spec/distribution.md, The five).
var five = []string{"git", "gh", "claude", "codex", "no-mistakes"}

// The two third-party modules the toolchain admits, each in the one package
// that links it (spec/distribution.md, The toolchain).
var admittedModules = map[string]string{
	"github.com/zalando/go-keyring": "contract",
	"github.com/ebitengine/purego":  "ui",
}

// Check runs every check of the declarations against the specification and
// returns what disagrees, in a fixed order. Each is also a method of its own.
func (s *Set) Check() Problems {
	var all Problems
	for _, check := range []func() Problems{
		s.Complete, s.CommandOwners, s.CallGraph, s.Names, s.HomeOwnership, s.Executables, s.Libraries,
	} {
		all = append(all, check()...)
	}
	return all
}

// Complete checks that the library and each of the seven packages are
// declared once.
func (s *Set) Complete() Problems {
	var ps Problems
	if s.Library == nil {
		ps = append(ps, Problem{"complete", LibraryDir, "the shared library has no declaration"})
	} else if s.Library.Name != "contract" {
		ps = append(ps, Problem{"complete", s.Library.Dir, "the library is declared as " + s.Library.Name})
	}
	seen := map[string]bool{}
	for _, p := range s.Packages {
		if seen[p.Name] {
			ps = append(ps, Problem{"complete", p.Dir, p.Name + " is declared twice"})
		}
		seen[p.Name] = true
		if !has(PackageNames, p.Name) {
			ps = append(ps, Problem{"complete", p.Dir, p.Name + " is not one of the seven packages"})
		}
	}
	for _, name := range PackageNames {
		if !seen[name] {
			ps = append(ps, Problem{"complete", PackagesDir + "/" + name, "the package has no declaration"})
		}
	}
	return ps
}

// CommandOwners checks that every command has exactly one owner: no command
// name is declared twice, in one package or in two; each package's executable
// is one of its commands; no operation is listed twice for a command; and
// capabilities, which every command answers, is not listed as an operation.
func (s *Set) CommandOwners() Problems {
	var ps Problems
	owner := map[string]string{}
	for _, p := range s.Packages {
		names := map[string]bool{}
		for _, c := range p.Commands {
			if first, dup := owner[c.Name]; dup {
				ps = append(ps, Problem{"one owner", p.Dir, fmt.Sprintf("the command %s is declared by %s and by %s", c.Name, first, p.Name)})
			}
			owner[c.Name] = p.Name
			names[c.Name] = true
			ops := map[string]bool{}
			for _, op := range c.Operations {
				switch {
				case op == "capabilities":
					ps = append(ps, Problem{"one owner", p.Dir, c.Name + " lists capabilities, which every command answers and no declaration lists"})
				case ops[op]:
					ps = append(ps, Problem{"one owner", p.Dir, fmt.Sprintf("the operation %s of %s is listed twice", op, c.Name)})
				}
				ops[op] = true
			}
		}
		if p.Kind == KindPackage && !names[p.Executable] {
			ps = append(ps, Problem{"one owner", p.Dir, fmt.Sprintf("the executable %s is not one of the commands of %s", p.Executable, p.Name)})
		}
	}
	return ps
}

// CallGraph checks the declared calls: each names a package other than its
// own, and the graph they make has no cycle, since a command may hold its
// own store's lock while it calls a package it declares, so a cycle of calls
// would be a cycle of locks (spec/packages.md, Calls between packages).
func (s *Set) CallGraph() Problems {
	var ps Problems
	edges := map[string][]string{}
	for _, p := range s.Packages {
		for _, to := range p.Calls {
			switch {
			case to == p.Name:
				ps = append(ps, Problem{"call graph", p.Dir, p.Name + " calls itself"})
			case !has(PackageNames, to) || !s.hasPackage(to):
				ps = append(ps, Problem{"call graph", p.Dir, fmt.Sprintf("%s calls %s, which is not a declared package", p.Name, to)})
			default:
				edges[p.Name] = append(edges[p.Name], to)
			}
		}
		if dups := duplicates(p.Calls); len(dups) > 0 {
			ps = append(ps, Problem{"call graph", p.Dir, fmt.Sprintf("%s lists %s twice", p.Name, strings.Join(dups, ", "))})
		}
	}
	for _, cycle := range cycles(edges) {
		ps = append(ps, Problem{"call graph", "calls", "the declared calls form a cycle: " + strings.Join(cycle, " -> ")})
	}
	return ps
}

func (s *Set) hasPackage(name string) bool {
	for _, p := range s.Packages {
		if p.Name == name {
			return true
		}
	}
	return false
}

// cycles returns each cycle of the graph once, as the path that closes it,
// starting at its smallest member.
func cycles(edges map[string][]string) [][]string {
	var nodes []string
	for n := range edges {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	const (
		white = iota
		grey
		black
	)
	color := map[string]int{}
	var stack []string
	seen := map[string]bool{}
	var out [][]string
	var visit func(n string)
	visit = func(n string) {
		color[n] = grey
		stack = append(stack, n)
		next := slices.Clone(edges[n])
		sort.Strings(next)
		for _, m := range next {
			switch color[m] {
			case white:
				visit(m)
			case grey:
				at := slices.Index(stack, m)
				path := slices.Clone(stack[at:])
				// Rotate to start at the smallest member, so a cycle is one entry.
				least := 0
				for i, x := range path {
					if x < path[least] {
						least = i
					}
				}
				path = append(path[least:], path[:least]...)
				path = append(path, path[0])
				if key := strings.Join(path, ">"); !seen[key] {
					seen[key] = true
					out = append(out, path)
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
	}
	for _, n := range nodes {
		if color[n] == white {
			visit(n)
		}
	}
	return out
}

func duplicates(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range list {
		if seen[s] && !has(out, s) {
			out = append(out, s)
		}
		seen[s] = true
	}
	return out
}

// Names checks that every name carries its owner and that the naming rules
// of spec/naming.md hold for what a declaration names: each package's
// executable and test target have the forms the specification gives; its
// record, receipt, event and error names begin with its own name, which no
// other package has; the Spawning Pool is never shortened in an
// identifier, and the bare word pool belongs to the package named pool alone.
func (s *Set) Names() Problems {
	var ps Problems
	prefixOwner := map[string]string{}
	kindOwner := map[string]string{}
	for _, p := range s.All() {
		add := func(msg string, args ...any) {
			ps = append(ps, Problem{"names", p.Dir, fmt.Sprintf(msg, args...)})
		}
		switch p.Kind {
		case KindPackage:
			if want := "hive-" + p.Name; p.Executable != want {
				add("the executable is %s; the executable of %s is %s", p.Executable, p.Name, want)
			}
			if want := "test-" + p.Name; p.TestTarget != want {
				add("the test target is %s; the test target of %s is %s", p.TestTarget, p.Name, want)
			}
			if !has(p.KindPrefixes, p.Name) {
				add("kind_prefixes lack %s, the package's own name", p.Name)
			}
			if len(p.KindPrefixes) != 1 {
				add("kind_prefixes are %v; the only prefix of %s is its own name", p.KindPrefixes, p.Name)
			}
		case KindLibrary:
			if p.TestTarget != "test-contract" {
				add("the test target is %s; the contract library's is test-contract", p.TestTarget)
			}
			if !slices.Equal(p.KindPrefixes, []string{"hive"}) {
				add("kind_prefixes are %v; the contract library's are [hive]", p.KindPrefixes)
			}
		}
		for _, prefix := range p.KindPrefixes {
			if other, taken := prefixOwner[prefix]; taken && other != p.Name {
				add("the prefix %s is claimed by %s and by %s", prefix, other, p.Name)
			}
			prefixOwner[prefix] = p.Name
			if prefix != p.Name && has(PackageNames, prefix) {
				add("the prefix %s is the name of another package", prefix)
			}
		}
		for _, k := range p.ReceiptKinds {
			first, _, _ := strings.Cut(k.Kind, ".")
			if !has(p.KindPrefixes, first) {
				add("the receipt kind %s does not begin with a prefix of %s", k.Kind, p.Name)
			}
			if other, taken := kindOwner[k.Kind]; taken {
				add("the receipt kind %s is registered by %s and by %s", k.Kind, other, p.Name)
			}
			kindOwner[k.Kind] = p.Name
		}
		for _, id := range p.identifiers() {
			for _, problem := range spawningPoolNames(p.Name, id) {
				add("%s", problem)
			}
		}
	}
	return ps
}

// identifiers are the names a declaration gives: its own, its executable,
// commands, operations, home paths, prefixes, receipt kinds and test target.
func (p *Package) identifiers() []string {
	ids := []string{p.Name, p.Executable, p.TestTarget}
	for _, c := range p.Commands {
		ids = append(ids, c.Name)
		ids = append(ids, c.Operations...)
	}
	ids = append(ids, p.HomePaths...)
	ids = append(ids, p.KindPrefixes...)
	for _, k := range p.ReceiptKinds {
		ids = append(ids, k.Kind)
	}
	return slices.DeleteFunc(ids, func(s string) bool { return s == "" })
}

// spawningPoolNames applies rules 2 and 5 of spec/naming.md to one
// identifier of the package owner: the Spawning Pool is never shortened to
// pool or sp, and the bare word pool is the name of the work queue, which
// only the package named pool has.
func spawningPoolNames(owner, id string) []string {
	var out []string
	words := segments(id)
	for i, w := range words {
		switch {
		case w == "sp":
			out = append(out, fmt.Sprintf("%s shortens the Spawning Pool to sp", id))
		case w == "pool" && owner != "pool" && (i == 0 || words[i-1] != "spawning"):
			out = append(out, fmt.Sprintf("%s uses the bare word pool, which names the work queue and belongs to the package pool alone", id))
		case w == "pool" && owner == "pool" && i > 0 && words[i-1] == "spawning":
			out = append(out, fmt.Sprintf("%s names the Spawning Pool in the package pool", id))
		}
	}
	return out
}

// HomeOwnership checks that only the owner writes and reads a part of a
// home (spec/packages.md, What ownership means): the home paths of the
// packages are relative to the home, none holds another, and the journal and
// the locks, which are shared through the contract library, belong to no
// package. Paths are compared in any case, since the default file systems of
// macOS and Windows would take two that differ only in case for one
// (spec/records.md); a package may nest its own paths, but not alias them by
// case. A package without a store, as word, declares none.
func (s *Set) HomeOwnership() Problems {
	var ps Problems
	type claim struct{ pkg, path, folded string }
	var claims []claim
	for _, p := range s.All() {
		for _, h := range p.HomePaths {
			clean := strings.TrimSuffix(h, "/")
			folded := strings.ToLower(clean)
			first, _, _ := strings.Cut(folded, "/")
			if first == "journal" || first == "locks" {
				ps = append(ps, Problem{"home ownership", p.Dir, fmt.Sprintf("%s declares %s, which is shared through the contract library", p.Name, h)})
			}
			claims = append(claims, claim{p.Name, clean, folded})
		}
	}
	within := func(a, b string) bool { return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/") }
	for i, a := range claims {
		for _, b := range claims[i+1:] {
			if !within(a.folded, b.folded) {
				continue
			}
			if a.pkg == b.pkg && within(a.path, b.path) {
				continue
			}
			ps = append(ps, Problem{"home ownership", "home_paths", fmt.Sprintf("%s of %s and %s of %s overlap", a.path, a.pkg, b.path, b.pkg)})
		}
	}
	return ps
}

// Executables checks what each package declares it runs: no platform utility
// is one of the five or lacks a reason, a command line is run only by the
// package that declares the program, and the executes lists hold the right
// programs in the right places.
func (s *Set) Executables() Problems {
	var ps Problems
	for _, p := range s.All() {
		add := func(msg string, args ...any) {
			ps = append(ps, Problem{"executables", p.Dir, fmt.Sprintf(msg, args...)})
		}
		for _, name := range p.Executes.Core {
			if has(five, name) && name != "no-mistakes" {
				continue
			}
			add("%s is not a core program", name)
		}
		for _, name := range p.Executes.Transitional {
			if name != "no-mistakes" {
				add("%s is not the program kept under transitional", name)
			}
		}
		for _, u := range p.Platform {
			if has(five, u.Name) {
				add("the platform utility %s is one of the five", u.Name)
			}
			if strings.TrimSpace(u.Reason) == "" {
				add("the platform utility %s has no reason", u.Name)
			}
		}
		if p.Kind == KindLibrary && (len(p.Executes.Core)+len(p.Executes.Transitional)+len(p.Executes.Optional) > 0 || p.Executes.Project || len(p.Platform) > 0) {
			add("the contract library runs nothing itself; the packages declare what they run")
		}
	}
	return ps
}

// Libraries checks the third-party modules a declaration lists: only the two
// the toolchain admits, each in the package that links it.
func (s *Set) Libraries() Problems {
	var ps Problems
	for _, p := range s.All() {
		for _, l := range p.Libraries {
			owner, ok := admittedModules[l.Module]
			switch {
			case !ok:
				ps = append(ps, Problem{"libraries", p.Dir, fmt.Sprintf("%s is not one of the two modules the toolchain admits", l.Module)})
			case owner != p.Name:
				ps = append(ps, Problem{"libraries", p.Dir, fmt.Sprintf("%s is linked by %s alone", l.Module, owner)})
			}
		}
	}
	return ps
}
