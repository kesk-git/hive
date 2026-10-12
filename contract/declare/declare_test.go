package declare

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// repo is the repository, read from the root two levels up.
func repo(t testing.TB) fs.FS {
	t.Helper()
	return os.DirFS("../..")
}

// declarations copies the repository's declarations into a file system a
// test may change.
func declarations(t testing.TB) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	root := repo(t)
	files := []string{path.Join(LibraryDir, FileName)}
	for _, name := range PackageNames {
		files = append(files, path.Join(PackagesDir, name, FileName))
	}
	for _, f := range files {
		data, err := fs.ReadFile(root, f)
		if err != nil {
			t.Fatal(err)
		}
		out[f] = &fstest.MapFile{Data: data}
	}
	return out
}

// edit changes the declaration at file with fn.
func edit(t testing.TB, fsys fstest.MapFS, file string, fn func(m map[string]any)) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(fsys[file].Data, &m); err != nil {
		t.Fatal(err)
	}
	fn(m)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	fsys[file] = &fstest.MapFile{Data: data}
}

func decl(name string) string { return path.Join(PackagesDir, name, FileName) }

// read reads a changed repository, which must read whole.
func read(t testing.TB, fsys fs.FS) *Set {
	t.Helper()
	set, problems := Read(fsys)
	if err := problems.Err(); err != nil {
		t.Fatal(err)
	}
	return set
}

// mention reports whether any problem's text holds s.
func mention(ps Problems, s string) bool {
	return slices.ContainsFunc(ps, func(p Problem) bool { return strings.Contains(p.String(), s) })
}

// The declarations of the repository follow the schema, and agree with the
// specification in every way this package checks.
func TestTheDeclarationsOfTheRepositoryAgree(t *testing.T) {
	set, problems := Read(repo(t))
	if err := problems.Err(); err != nil {
		t.Fatal(err)
	}
	if got := len(set.Packages); got != 7 || set.Library == nil {
		t.Fatalf("%d packages, library %v", got, set.Library)
	}
	if err := set.Check().Err(); err != nil {
		t.Fatal(err)
	}
}

// Every command has one owner, and the design's twenty-eight commands are
// all declared.
func TestEveryCommandHasOneOwner(t *testing.T) {
	set := read(t, repo(t))
	count := 0
	for _, p := range set.Packages {
		count += len(p.Commands)
		for _, c := range p.Commands {
			if set.Owner(c.Name) != p {
				t.Errorf("%s is owned by %v", c.Name, set.Owner(c.Name))
			}
		}
	}
	if count != 28 {
		t.Errorf("%d commands, want 28", count)
	}
	if set.Library.Commands != nil {
		t.Error("the library owns a command")
	}

	t.Run("two packages", func(t *testing.T) {
		fsys := declarations(t)
		edit(t, fsys, decl("word"), func(m map[string]any) {
			cmds := m["commands"].([]any)
			m["commands"] = append(cmds, map[string]any{"name": "hive-admit", "purpose": "x", "operations": []any{}})
		})
		ps := read(t, fsys).CommandOwners()
		if !mention(ps, "hive-admit is declared by") {
			t.Errorf("%v", ps)
		}
	})
	t.Run("twice in a package", func(t *testing.T) {
		fsys := declarations(t)
		edit(t, fsys, decl("ui"), func(m map[string]any) {
			cmds := m["commands"].([]any)
			m["commands"] = append(cmds, cmds[0])
		})
		if ps := read(t, fsys).CommandOwners(); !mention(ps, "hive-ui is declared by") {
			t.Errorf("%v", ps)
		}
	})
	t.Run("the executable is no command", func(t *testing.T) {
		fsys := declarations(t)
		edit(t, fsys, decl("shadow"), func(m map[string]any) { m["executable"] = "hive-watch" })
		if ps := read(t, fsys).CommandOwners(); !mention(ps, "hive-watch is not one of the commands") {
			t.Errorf("%v", ps)
		}
	})
	t.Run("an operation twice, or capabilities", func(t *testing.T) {
		fsys := declarations(t)
		edit(t, fsys, decl("word"), func(m map[string]any) {
			m["commands"].([]any)[0].(map[string]any)["operations"] = []any{"hold", "hold", "capabilities"}
		})
		ps := read(t, fsys).CommandOwners()
		if !mention(ps, "operation hold of hive-word is listed twice") || !mention(ps, "lists capabilities") {
			t.Errorf("%v", ps)
		}
	})
}

// The declared call graph has no cycle, and a cycle is named.
func TestTheCallGraphHasNoCycle(t *testing.T) {
	set := read(t, repo(t))
	if ps := set.CallGraph(); len(ps) != 0 {
		t.Fatal(ps)
	}
	setCalls := func(fsys fstest.MapFS, name string, calls ...string) {
		edit(t, fsys, decl(name), func(m map[string]any) {
			list := []any{}
			for _, c := range calls {
				list = append(list, c)
			}
			m["calls"] = list
		})
	}
	t.Run("two packages", func(t *testing.T) {
		fsys := declarations(t)
		setCalls(fsys, "pool", "word")
		ps := read(t, fsys).CallGraph()
		if len(ps) != 1 || !strings.Contains(ps[0].Message, "pool -> word -> pool") {
			t.Errorf("%v", ps)
		}
	})
	t.Run("a longer cycle, once", func(t *testing.T) {
		fsys := declarations(t)
		setCalls(fsys, "spawning-pool", "synapse") // synapse -> dispatch -> spawning-pool -> synapse
		ps := read(t, fsys).CallGraph()
		if len(ps) != 1 || !strings.Contains(ps[0].Message, "dispatch -> spawning-pool -> synapse -> dispatch") {
			t.Errorf("%v", ps)
		}
	})
	t.Run("itself, an unknown package, a repeat", func(t *testing.T) {
		fsys := declarations(t)
		setCalls(fsys, "word", "word", "pool", "pool")
		ps := read(t, fsys).CallGraph()
		if !mention(ps, "word calls itself") || !mention(ps, "lists pool twice") {
			t.Errorf("%v", ps)
		}
		fsys = declarations(t)
		edit(t, fsys, decl("word"), func(m map[string]any) { m["calls"] = []any{"contract"} })
		set, _ := Read(fsys) // the schema refuses it: a package is never called "contract"
		if set.Lookup("word") != nil {
			t.Error("a declaration that calls the library was read")
		}
	})
}

// The naming rules hold: names carry their owner, and the Spawning Pool is
// never shortened.
func TestNamingRules(t *testing.T) {
	if ps := read(t, repo(t)).Names(); len(ps) != 0 {
		t.Fatal(ps)
	}
	cases := []struct {
		name string
		file string
		edit func(m map[string]any)
		want string
	}{
		{"a prefix of another package", decl("word"), func(m map[string]any) { m["kind_prefixes"] = []any{"word", "pool"} }, "claimed by pool and by word"},
		{"a receipt kind of another package", decl("shadow"), func(m map[string]any) {
			m["receipt_kinds"] = []any{map[string]any{"kind": "dispatch.wake.sent", "asserts": "x"}}
		}, "does not begin with a prefix of shadow"},
		{"a receipt kind twice", decl("spawning-pool"), func(m map[string]any) {
			k := m["receipt_kinds"].([]any)
			m["receipt_kinds"] = append(k, k[0])
		}, "registered by spawning-pool and by spawning-pool"},
		{"the Spawning Pool shortened to pool", decl("spawning-pool"), func(m map[string]any) {
			m["kind_prefixes"] = []any{"spawning-pool", "spawning-pool-x", "pool"}
		}, "bare word pool"},
		{"the Spawning Pool shortened to sp", decl("spawning-pool"), func(m map[string]any) {
			m["home_paths"] = []any{"sp/leases"}
		}, "shortens the Spawning Pool to sp"},
		{"the bare word pool in another package", decl("dispatch"), func(m map[string]any) {
			m["receipt_kinds"] = []any{map[string]any{"kind": "dispatch.pool.sent", "asserts": "x"}}
		}, "uses the bare word pool"},
		{"the wrong executable", decl("ui"), func(m map[string]any) {
			m["executable"] = "hive-ui2"
			m["commands"].([]any)[0].(map[string]any)["name"] = "hive-ui2"
		}, "the executable of ui is hive-ui"},
		{"the wrong test target", decl("pool"), func(m map[string]any) { m["test_target"] = "test-pools" }, "the test target of pool is test-pool"},
		{"the package's own name is no prefix", decl("synapse"), func(m map[string]any) { m["kind_prefixes"] = []any{"syn"} }, "lack synapse"},
		{"another package's name as a prefix", decl("ui"), func(m map[string]any) { m["kind_prefixes"] = []any{"ui", "word"} }, "the name of another package"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := declarations(t)
			edit(t, fsys, tc.file, tc.edit)
			set, problems := Read(fsys)
			if len(problems) != 0 {
				t.Fatalf("the change does not read: %v", problems)
			}
			if ps := set.Names(); !mention(ps, tc.want) {
				t.Errorf("want %q in %v", tc.want, ps)
			}
		})
	}
	t.Run("the library", func(t *testing.T) {
		fsys := declarations(t)
		edit(t, fsys, path.Join(LibraryDir, FileName), func(m map[string]any) { m["kind_prefixes"] = []any{"hive", "pool"} })
		if ps := read(t, fsys).Names(); !mention(ps, "the contract library's are [hive]") {
			t.Errorf("%v", ps)
		}
	})
}

// A build file selects nothing by the suffix pool (spec/naming.md, rule 5).
func TestBuildFilesNeverSelectByThePoolSuffix(t *testing.T) {
	for _, file := range []string{"Makefile", ".no-mistakes.yaml", "go.mod", ".gitignore", ".gitattributes"} {
		data, err := fs.ReadFile(repo(t), file)
		if err != nil {
			continue
		}
		if ps := CheckBuildFile(file, data); len(ps) != 0 {
			t.Errorf("%v", ps)
		}
	}
	workflows, _ := fs.Glob(repo(t), ".github/workflows/*")
	for _, file := range workflows {
		data, _ := fs.ReadFile(repo(t), file)
		if ps := CheckBuildFile(file, data); len(ps) != 0 {
			t.Errorf("%v", ps)
		}
	}
	bad := []string{
		"test-%pool: ;",
		"$(filter %pool,$(TARGETS))",
		"go test ./packages/*pool/...",
		"grep -E 'pool$' targets",
		"for t in *-pool; do",
		"ls packages/?pool",
	}
	for _, text := range bad {
		if ps := CheckBuildFile("Makefile", []byte(text)); len(ps) != 1 {
			t.Errorf("%q: %v", text, ps)
		}
	}
	good := []string{
		"test-pool test-spawning-pool: ;",
		"go test ./packages/pool/... ./packages/spawning-pool/...",
		"$(call build-exe,spawning-pool,hive-spawning-pool)",
		"# pool and spawning-pool",
		"go test ./packages/*/...",
		"find packages/pool -name '*_test.go'",
	}
	for _, text := range good {
		if ps := CheckBuildFile("Makefile", []byte(text)); len(ps) != 0 {
			t.Errorf("%q: %v", text, ps)
		}
	}
}

// The Makefile names every declared test target and executable in full.
func TestTheMakefileNamesEveryTargetInFull(t *testing.T) {
	set := read(t, repo(t))
	data, err := fs.ReadFile(repo(t), "Makefile")
	if err != nil {
		t.Fatal(err)
	}
	words := map[string]bool{}
	for _, w := range regexp.MustCompile(`[A-Za-z0-9_./-]+`).FindAllString(string(data), -1) {
		words[w] = true
	}
	for _, p := range set.All() {
		if !words[p.TestTarget] {
			t.Errorf("the Makefile does not name %s of %s", p.TestTarget, p.Name)
		}
		if p.Kind == KindPackage && !words[p.Executable] {
			t.Errorf("the Makefile does not name %s of %s", p.Executable, p.Name)
		}
	}
}

// Parts of a home belong to one package, and the shared parts to none.
func TestHomeOwnership(t *testing.T) {
	if ps := read(t, repo(t)).HomeOwnership(); len(ps) != 0 {
		t.Fatal(ps)
	}
	cases := []struct {
		name string
		file string
		path string
		want string
	}{
		{"the same path", decl("shadow"), "pool", "overlap"},
		{"a path inside another's", decl("shadow"), "pool/shadow", "overlap"},
		{"a path that holds another's", decl("shadow"), "dispatch/registry", "overlap"},
		{"the journal", decl("shadow"), "journal", "shared through the contract library"},
		{"the locks", decl("shadow"), "locks/shadow.lock", "shared through the contract library"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := declarations(t)
			edit(t, fsys, tc.file, func(m map[string]any) { m["home_paths"] = []any{"shadow", tc.path} })
			if ps := read(t, fsys).HomeOwnership(); !mention(ps, tc.want) {
				t.Errorf("%v", ps)
			}
		})
	}
}

// The executes lists, the platform utilities and the libraries hold what the
// distribution contract allows.
func TestExecutablesAndLibraries(t *testing.T) {
	set := read(t, repo(t))
	if ps := append(set.Executables(), set.Libraries()...); len(ps) != 0 {
		t.Fatal(ps)
	}
	t.Run("a platform utility that is one of the five", func(t *testing.T) {
		fsys := declarations(t)
		edit(t, fsys, decl("shadow"), func(m map[string]any) {
			m["platform"] = []any{map[string]any{"name": "git", "reason": "x"}}
		})
		if ps := read(t, fsys).Executables(); !mention(ps, "platform utility git is one of the five") {
			t.Errorf("%v", ps)
		}
	})
	t.Run("a platform utility without a reason", func(t *testing.T) {
		fsys := declarations(t)
		edit(t, fsys, decl("shadow"), func(m map[string]any) {
			m["platform"] = []any{map[string]any{"name": "ps", "reason": "x"}}
		})
		set := read(t, fsys)
		set.Lookup("shadow").Platform[0].Reason = " "
		if ps := set.Executables(); !mention(ps, "platform utility ps has no reason") {
			t.Errorf("%v", ps)
		}
		fsys = declarations(t)
		edit(t, fsys, decl("shadow"), func(m map[string]any) {
			m["platform"] = []any{map[string]any{"name": "ps", "reason": ""}}
		})
		if _, problems := Read(fsys); len(problems) == 0 {
			t.Error("the schema admitted a utility with no reason")
		}
	})
	t.Run("the library runs a program", func(t *testing.T) {
		fsys := declarations(t)
		edit(t, fsys, path.Join(LibraryDir, FileName), func(m map[string]any) {
			m["executes"].(map[string]any)["core"] = []any{"git"}
		})
		if ps := read(t, fsys).Executables(); !mention(ps, "the contract library runs nothing itself") {
			t.Errorf("%v", ps)
		}
	})
	t.Run("a module the toolchain does not admit", func(t *testing.T) {
		fsys := declarations(t)
		edit(t, fsys, decl("ui"), func(m map[string]any) {
			m["libraries"] = []any{
				map[string]any{"module": "example.com/other", "import_id": "other"},
				map[string]any{"module": "github.com/zalando/go-keyring", "import_id": "keyring"},
				map[string]any{"module": "github.com/ebitengine/purego", "import_id": "purego"},
			}
		})
		ps := read(t, fsys).Libraries()
		if len(ps) != 2 || !mention(ps, "not one of the two modules") || !mention(ps, "go-keyring is linked by contract alone") {
			t.Errorf("%v", ps)
		}
	})
}

// A declaration that is not whole is named, and the rest are still read.
func TestReadNamesWhatItCannotRead(t *testing.T) {
	fsys := declarations(t)
	delete(fsys, decl("word"))
	fsys[decl("stray")+"x"] = &fstest.MapFile{Data: []byte("{}")}
	edit(t, fsys, decl("shadow"), func(m map[string]any) { m["schema_version"] = "9.9" })
	edit(t, fsys, decl("ui"), func(m map[string]any) { m["name"] = "pool" })
	set, problems := Read(fsys)
	for _, want := range []string{"packages/stray/hive-package.json", "packages/shadow/hive-package.json", "the directory ui holds the declaration of pool"} {
		if !mention(problems, want) {
			t.Errorf("no problem names %q: %v", want, problems)
		}
	}
	if len(set.Packages) != 5 {
		t.Errorf("%d packages read", len(set.Packages))
	}
	if ps := set.Complete(); !mention(ps, "packages/word") || !mention(ps, "pool is declared twice") {
		t.Errorf("%v", ps)
	}
	if _, problems := Read(fstest.MapFS{}); len(problems) == 0 {
		t.Error("an empty repository read")
	}
	delete(fsys, path.Join(LibraryDir, FileName))
	set, _ = Read(fsys)
	if ps := set.Complete(); !mention(ps, "the shared library has no declaration") {
		t.Errorf("%v", ps)
	}
}

// What the specification's own tables say about the packages is what their
// declarations say.
func TestTheSpecificationsTablesAgreeWithTheDeclarations(t *testing.T) {
	set := read(t, repo(t))
	packages, err := fs.ReadFile(repo(t), "spec/packages.md")
	if err != nil {
		t.Fatal(err)
	}
	ticks := regexp.MustCompile("`([^`]+)`")
	ticked := func(cell string) []string {
		var out []string
		for _, m := range ticks.FindAllStringSubmatch(cell, -1) {
			out = append(out, m[1])
		}
		return out
	}
	for _, section := range []string{"The seven", "Commands by owner", "Calls between packages"} {
		rows := tableRows(t, string(packages), section)
		if len(rows) != 7 {
			t.Fatalf("%s: %d rows", section, len(rows))
		}
		for _, row := range rows {
			p := set.Lookup(ticked(row[0])[0])
			if p == nil {
				t.Fatalf("%s: %v names no package", section, row)
			}
			switch section {
			case "The seven":
				if got := ticked(row[2]); len(got) != 1 || got[0] != p.Executable {
					t.Errorf("%s: executable %v, declared %s", p.Name, got, p.Executable)
				}
				var homes []string
				for _, h := range ticked(row[3]) {
					homes = append(homes, strings.TrimSuffix(h, "/"))
				}
				if !slices.Equal(sorted(homes), sorted(p.HomePaths)) {
					t.Errorf("%s: home paths %v, declared %v", p.Name, homes, p.HomePaths)
				}
				if got := ticked(row[4]); len(got) != 1 || got[0] != p.TestTarget {
					t.Errorf("%s: test target %v, declared %s", p.Name, got, p.TestTarget)
				}
				if strings.TrimSpace(row[5]) != string(rune('0'+p.PlanStage)) {
					t.Errorf("%s: built in stage %s, declared %d", p.Name, row[5], p.PlanStage)
				}
			case "Commands by owner":
				var declared []string
				for _, c := range p.Commands {
					declared = append(declared, c.Name)
				}
				if !slices.Equal(sorted(ticked(row[1])), sorted(declared)) {
					t.Errorf("%s: commands %v, declared %v", p.Name, ticked(row[1]), declared)
				}
			case "Calls between packages":
				if !slices.Equal(sorted(ticked(row[1])), sorted(p.Calls)) {
					t.Errorf("%s: calls %v, declared %v", p.Name, ticked(row[1]), p.Calls)
				}
			}
		}
	}
	// The home table of the record format names the same owners.
	records, err := fs.ReadFile(repo(t), "spec/records.md")
	if err != nil {
		t.Fatal(err)
	}
	owner := map[string]string{}
	for _, row := range tableRows(t, string(records), "The home") {
		path := strings.TrimSuffix(ticked(row[0])[0], "/")
		if o := ticked(row[1]); len(o) == 1 {
			owner[path] = o[0]
		} else {
			owner[path] = "shared"
		}
	}
	for _, p := range set.Packages {
		for _, h := range p.HomePaths {
			if owner[h] != p.Name {
				t.Errorf("%s declares %s, and the home table gives it to %q", p.Name, h, owner[h])
			}
		}
	}
	for path, o := range owner {
		if o == "shared" {
			continue
		}
		if p := set.Lookup(o); p == nil || !slices.Contains(p.HomePaths, path) {
			t.Errorf("the home table gives %s to %s, which does not declare it", path, o)
		}
	}
}

func sorted(list []string) []string {
	out := slices.Clone(list)
	slices.Sort(out)
	return out
}

// tableRows returns the cells of each body row of the first table after the
// heading whose text holds section.
func tableRows(t testing.TB, doc, section string) [][]string {
	t.Helper()
	lines := strings.Split(doc, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "#") && strings.Contains(l, section) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("no section %q", section)
	}
	var rows [][]string
	in := false
	for _, l := range lines[start+1:] {
		if strings.HasPrefix(l, "|") {
			in = true
			cells := strings.Split(strings.Trim(l, "| "), " | ")
			if strings.HasPrefix(cells[0], "---") || strings.HasPrefix(cells[0], "Package") || strings.HasPrefix(cells[0], "Path") || strings.HasPrefix(cells[0], "Entry") {
				continue
			}
			rows = append(rows, cells)
		} else if in {
			break
		}
	}
	return rows
}
