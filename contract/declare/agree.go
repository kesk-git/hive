package declare

import (
	"fmt"
	"slices"
	"sort"

	"github.com/kesk-git/hive/contract/cli"
)

// Agree holds an executable's description to the declaration of its package
// (spec/commands.md, Capabilities): it is the package's executable and owns
// the commands the declaration lists, each with the operations the
// declaration lists; what an operation says it executes, calls, writes as a
// receipt and runs as a platform utility is among what the package
// declares; and the executable keeps no store exactly when the declaration
// lists no home path of the package's own. A storeless executable that
// declares a home path, or a stored one that declares none, is refused. The
// packages whose handoffs an executable keys on the original request's digest
// are packages the declaration says keep no store: an executable that lists
// one that is not, or one that is not a package, is refused.
func (s *Set) Agree(e *cli.Executable) Problems {
	p := s.Lookup(e.Package)
	if p == nil || p.Kind != KindPackage {
		return Problems{{"agreement", e.Name, fmt.Sprintf("%q is not a declared package", e.Package)}}
	}
	var ps Problems
	add := func(where, msg string, args ...any) {
		ps = append(ps, Problem{"agreement", where, fmt.Sprintf(msg, args...)})
	}
	if e.Name != p.Executable {
		add(e.Name, "the executable of %s is %s", p.Name, p.Executable)
	}
	if storeless := len(p.HomePaths) == 0; e.Storeless != storeless {
		if storeless {
			add(e.Name, "%s declares no home path, so its executable keeps no store, and this one is not storeless", p.Name)
		} else {
			add(e.Name, "%s declares the home paths %v, so its executable keeps a store, and this one is storeless", p.Name, p.HomePaths)
		}
	}
	for _, name := range e.Handoffs {
		q := s.Lookup(name)
		switch {
		case q == nil || q.Kind != KindPackage:
			add(e.Name, "%q is listed as a package whose handoffs it accepts, and it is not a declared package", name)
		case len(q.HomePaths) != 0:
			add(e.Name, "%s declares the home paths %v, so its requests are never handoffs", q.Name, q.HomePaths)
		}
	}
	declared := map[string]Command{}
	for _, c := range p.Commands {
		declared[c.Name] = c
	}
	described := map[string]*cli.Command{}
	for _, c := range e.Commands {
		described[c.Name] = c
		if _, ok := declared[c.Name]; !ok {
			add(c.Name, "the command is not one of the commands %s declares", p.Name)
		}
	}
	for _, c := range p.Commands {
		got, ok := described[c.Name]
		if !ok {
			add(c.Name, "%s declares the command, and the executable does not own it", p.Name)
			continue
		}
		var ops []string
		for _, op := range got.Operations {
			ops = append(ops, op.Name)
		}
		sort.Strings(ops)
		want := slices.Sorted(slices.Values(c.Operations))
		if !slices.Equal(ops, want) {
			add(c.Name, "the operations are %v; the declaration lists %v", ops, want)
		}
		for _, op := range got.Operations {
			ps = append(ps, s.agreeOperation(p, c.Name, op)...)
		}
	}
	return ps
}

// agreeOperation holds one operation's declaration to its package's.
func (s *Set) agreeOperation(p *Package, command string, op *cli.Operation) Problems {
	var ps Problems
	where := command + " " + op.Name
	add := func(msg string, args ...any) {
		ps = append(ps, Problem{"agreement", where, fmt.Sprintf(msg, args...)})
	}
	for _, x := range op.Executes {
		if !p.runs(x) {
			add("it may run %s, which %s does not declare", x, p.Name)
		}
	}
	for _, call := range op.Calls {
		owner := s.Owner(call)
		switch {
		case owner == nil:
			add("it may call %s, which no package owns", call)
		case owner.Name == p.Name:
			add("it may call %s, a command of its own package", call)
		case !has(p.Calls, owner.Name):
			add("it may call %s, and %s does not declare a call to %s", call, p.Name, owner.Name)
		}
	}
	for _, kind := range op.ReceiptKinds {
		if !slices.ContainsFunc(p.ReceiptKinds, func(k ReceiptKind) bool { return k.Kind == kind }) {
			add("it writes the receipt kind %s, which %s has not registered", kind, p.Name)
		}
	}
	for _, name := range op.Platform {
		if !slices.ContainsFunc(p.Platform, func(u Utility) bool { return u.Name == name }) {
			add("it may run the platform utility %s, which %s does not declare", name, p.Name)
		}
	}
	return ps
}

// runs reports whether the package declares the executable an operation
// names: one of the programs of its executes lists, or a project's own
// validation command where the package declares project.
func (p *Package) runs(name string) bool {
	if name == ToolProject {
		return p.Executes.Project
	}
	return has(p.Executes.Core, name) || has(p.Executes.Transitional, name) || has(p.Executes.Optional, name)
}
