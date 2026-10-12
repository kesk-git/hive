package declare

import (
	"testing"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
)

// unsupported declares an operation the build has not reached.
func unsupported(name string) *cli.Operation {
	return &cli.Operation{OperationDecl: envelope.OperationDecl{
		Name: name, State: envelope.Unsupported, Reason: "built at a later stage", Mode: envelope.Unary,
		Versions: []string{}, ReceiptKinds: []string{}, Executes: []string{}, Calls: []string{},
	}}
}

// described makes the executable the declaration of a package describes,
// every operation unsupported, as a first build of it is.
func described(t testing.TB, set *Set, name string) *cli.Executable {
	t.Helper()
	p := set.Lookup(name)
	e := &cli.Executable{Package: p.Name, Name: p.Executable, Version: "0.0.1", Storeless: len(p.HomePaths) == 0}
	for _, c := range p.Commands {
		cmd := &cli.Command{Name: c.Name}
		for _, op := range c.Operations {
			cmd.Operations = append(cmd.Operations, unsupported(op))
		}
		e.Commands = append(e.Commands, cmd)
	}
	if err := e.Check(); err != nil {
		t.Fatal(err)
	}
	return e
}

// An executable described from its declaration agrees with it, for each of
// the seven packages.
func TestAnExecutableAgreesWithItsDeclaration(t *testing.T) {
	set := read(t, repo(t))
	for _, p := range set.Packages {
		if ps := set.Agree(described(t, set, p.Name)); len(ps) != 0 {
			t.Errorf("%s: %v", p.Name, ps)
		}
	}
}

// An executable's storeless field is held to the declaration's home paths
// being empty: word keeps no store, and no package that declares a home path
// is storeless.
func TestStorelessFollowsTheHomePaths(t *testing.T) {
	set := read(t, repo(t))
	if len(set.Lookup("word").HomePaths) != 0 {
		t.Fatal("word declares a home path")
	}
	for _, p := range set.Packages {
		e := described(t, set, p.Name)
		e.Storeless = !e.Storeless
		ps := set.Agree(e)
		want := "keeps a store, and this one is storeless"
		if p.Name == "word" {
			want = "so its executable keeps no store"
		}
		if len(ps) != 1 || !mention(ps, want) {
			t.Errorf("%s: %v", p.Name, ps)
		}
	}
}

// What a command or an operation declares must be among what its package
// declares.
func TestAnExecutableThatStraysFromItsDeclaration(t *testing.T) {
	set := read(t, repo(t))
	cases := []struct {
		name   string
		pkg    string
		change func(e *cli.Executable)
		want   string
	}{
		{"another package's name", "word", func(e *cli.Executable) { e.Package = "nothing" }, "is not a declared package"},
		{"the library", "word", func(e *cli.Executable) { e.Package = "contract" }, "is not a declared package"},
		{"another executable name", "shadow", func(e *cli.Executable) { e.Name = "hive-state" }, "the executable of shadow is hive-shadow"},
		{"a command not declared", "shadow", func(e *cli.Executable) {
			e.Commands = append(e.Commands, &cli.Command{Name: "hive-admit"})
		}, "hive-admit: the command is not one of the commands shadow declares"},
		{"a command left out", "shadow", func(e *cli.Executable) { e.Commands = e.Commands[:1] }, "shadow declares the command, and the executable does not own it"},
		{"an operation left out", "word", func(e *cli.Executable) {
			e.Commands[0].Operations = e.Commands[0].Operations[1:]
		}, "the operations are"},
		{"an operation added", "word", func(e *cli.Executable) {
			e.Commands[0].Operations = append(e.Commands[0].Operations, unsupported("burn"))
		}, "the operations are"},
		{"a program the package does not run", "word", func(e *cli.Executable) {
			e.Commands[0].Operations[0].Executes = []string{"git"}
		}, "it may run git, which word does not declare"},
		{"a project command from a package that runs none", "word", func(e *cli.Executable) {
			e.Commands[0].Operations[0].Executes = []string{ToolProject}
		}, "it may run project-validation"},
		{"a call that is not declared", "word", func(e *cli.Executable) {
			e.Commands[0].Operations[0].Calls = []string{"hive-dispatch"}
		}, "word does not declare a call to dispatch"},
		{"a call to its own package", "word", func(e *cli.Executable) {
			e.Commands[0].Operations[0].Calls = []string{"hive-word"}
		}, "a command of its own package"},
		{"a call to no command", "word", func(e *cli.Executable) {
			e.Commands[0].Operations[0].Calls = []string{"hive-nothing"}
		}, "which no package owns"},
		{"a receipt kind not registered", "word", func(e *cli.Executable) {
			e.Commands[0].Operations[0].ReceiptKinds = []string{"word.held"}
		}, "word has not registered"},
		{"a platform utility not declared", "dispatch", func(e *cli.Executable) {
			e.Commands[1].Operations[0].Platform = []string{"ps"}
		}, "platform utility ps, which dispatch does not declare"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := described(t, set, tc.pkg)
			tc.change(e)
			if ps := set.Agree(e); !mention(ps, tc.want) {
				t.Errorf("want %q in %v", tc.want, ps)
			}
		})
	}
	// What is declared is accepted.
	e := described(t, set, "dispatch")
	for _, c := range e.Commands {
		if c.Name == "hive-fleet" {
			c.Operations[0].Executes = []string{"git", "gh"}
			c.Operations[0].Calls = []string{"hive-pool", "hive-spawning-pool"}
			c.Operations[0].Platform = []string{"security"}
		}
	}
	e.Commands[1].Operations[0].Platform = []string{"security"}
	e.Commands[1].Operations[0].Calls = []string{"hive-word"}
	e.Commands[1].Operations[0].Executes = []string{"claude", "codex"}
	e.Commands[1].Operations[0].ReceiptKinds = []string{"dispatch.home.created"}
	if ps := set.Agree(e); len(ps) != 0 {
		t.Errorf("%v", ps)
	}
	// A project's own validation command is run by the package that declares
	// project, and by no other.
	for _, p := range set.Packages {
		if got := p.runs(ToolProject); got != p.Executes.Project {
			t.Errorf("%s: runs %v, declared %v", p.Name, got, p.Executes.Project)
		}
	}
	if !set.Lookup("synapse").runs(ToolProject) || !set.Lookup("synapse").runs("no-mistakes") || set.Lookup("word").runs("no-mistakes") {
		t.Error("synapse runs a project command and no-mistakes, and word runs neither")
	}
}
