package schema

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// fixtureRoot is where spec/layout.md keeps the fixtures of the schemas:
// contract/testdata/schemas/<name>/ or <name>_v<major>/ after major 1.
const fixtureRoot = "../testdata/schemas"

// TestFixturesProveTheCommittedSchemas runs every example under fixtureRoot
// through the embedded registry. It is the one walker for every schema's
// fixtures: a lane that adds examples for another schema adds files and
// table rows and nothing here.
//
// The walk starts from the registry, not from the directories: every
// committed schema needs fixtures unless the table of "Schema fixture
// obligations" in spec/test-targets.md defers it, and a deferred schema must
// have no fixture directory, so the deferral is removed in the change that
// brings its fixtures.
//
// Each directory <name> directly under fixtureRoot names the schema whose
// $id is urn:hive:schema:<name>:v1 unless a terminal _v<major> selects a
// later major, where a schema under a subdirectory of
// spec/schemas/ has the colons of its $id written as underscores: the
// fixtures of receipts/x.v1.schema.json sit under receipts_x/. A directory
// naming no schema fails.
//
// The schema common holds only definitions and no keyword of its own at its
// root, so its examples name the definition they test: a case under common/
// is called <definition>.<label> (home_id.ok.json, open_object.array) and is
// checked against urn:hive:schema:common:v1#/$defs/<definition>. A case under
// common/ that names no definition, or a definition the schema lacks, fails.
//
// Under each schema directory only valid/, invalid.jsonl and invalid/ may
// appear. The schema needs at least one accepted example, a file of valid/,
// and at least one refused example, a row of invalid.jsonl or a file of
// invalid/. A stray file or folder beside them fails. Every example file is a
// .json file; a file with another suffix, or a folder inside valid/ or
// invalid/, fails. A file under valid/ must be accepted by its schema.
//
// A row of invalid.jsonl names a file of valid/ and edits it; see
// fixture_table_test.go. The document the edits build must decode and be
// refused by the schema with a *ValidationError; a row whose base cannot be
// used, whose edits cannot be applied or whose document the decoder refuses
// is a mistake in the fixture, not a refusal by the schema. A file under
// invalid/ keeps a document as written, for what no edit can say: it must be
// refused, which means Validate returns a *ValidationError or a
// *SyntaxError; any other error, such as an unknown schema, is a failure of
// the test, not a refusal. A row and a file of one name fail. A failing case
// is reported by its path, or by the table, line and name of its row, with
// the violations the validator found.
func TestFixturesProveTheCommittedSchemas(t *testing.T) {
	r, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range checkFixtureTree(r, fixtureRoot, r.IDs(), readFixtureDeferrals(t)) {
		t.Error(p)
	}
}

// The walker's own refusals are not vacuous: each kind of mistake in a
// fixture tree is reported, by path, and a tree with only correct examples
// is not.
func TestFixtureWalkerRefusesAMistakenTree(t *testing.T) {
	r, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mkdir := func(rel string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	// A correct tree, with the smallest examples there are, passes.
	write("common/valid/open_object.ok.json", "{}\n")
	write("common/invalid/open_object.array.json", "[]\n")
	write("common/invalid/open_object.unparsable.json", "{\n")
	names, problems := fixtureDirs(root)
	if len(problems) > 0 {
		t.Errorf("a correct root was refused: %q", problems)
	}
	if got := checkFixtureDir(r, root, "common"); len(got) > 0 {
		t.Errorf("a correct schema directory was refused: %q", got)
	}
	if strings.Join(names, ",") != "common" {
		t.Errorf("fixtureDirs = %v, want [common]", names)
	}

	// Every mistake is reported, by path.
	write("README.md", "")
	write("common/notes.txt", "")
	write("common/valid/ok.txt", "{}\n")
	mkdir("common/valid/nested")
	write("common/valid/ok.json", "{}\n")
	write("common/valid/nonesuch.ok.json", "{}\n")
	write("common/valid/open_object.refused.json", "[]\n")
	write("common/invalid/open_object.accepted.json", "{}\n")
	write("nothing/valid/ok.json", "{}\n")
	mkdir("event/invalid")
	write("receipt/valid/ok.json", "[]\n")
	mkdir("response")
	write("request/invalid/refused.json", "{}\n")

	_, got := fixtureDirs(root)
	for _, name := range []string{"common", "event", "nothing", "receipt", "request", "response"} {
		got = append(got, checkFixtureDir(r, root, name)...)
	}
	for _, want := range []struct{ path, says string }{
		{"README.md", "a stray file"},
		{"common/notes.txt", "a stray entry"},
		{"common/valid/ok.txt", "not a .json file"},
		{"common/valid/nested", "a folder"},
		{"common/valid/ok.json", "names no definition"},
		{"common/valid/nonesuch.ok.json", "could not be checked"},
		{"common/valid/open_object.refused.json", "was refused"},
		{"common/invalid/open_object.accepted.json", "was accepted"},
		{"nothing", "names no schema"},
		{"event", "no accepted example"},
		{"event", "no refused example"},
		{"receipt", "no refused example"},
		{"receipt/valid/ok.json", "was refused"},
		{"response", "no accepted example"},
		{"response", "no refused example"},
		{"request", "no accepted example"},
	} {
		p := filepath.Join(root, filepath.FromSlash(want.path))
		found := false
		for _, g := range got {
			if strings.HasPrefix(g, p+":") && strings.Contains(g, want.says) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no report that %s %s; got:\n%s", want.path, want.says, strings.Join(got, "\n"))
		}
	}
	// A correct file beside the mistakes is not reported.
	for _, g := range got {
		if strings.HasPrefix(g, filepath.Join(root, "common", "valid", "open_object.ok.json")+":") {
			t.Errorf("a correct example was reported: %s", g)
		}
	}
}

// fixtureDeferral is a schema whose fixtures are not yet owed, and the stage
// that owes them.
type fixtureDeferral struct {
	ID    string
	Stage int
}

// checkFixtureTree checks the whole tree of fixtures under root against r and
// reports every problem, each starting with the path concerned. ids are the
// schemas the tree must cover, except those named by deferred, which must have
// no directory.
func checkFixtureTree(r *Registry, root string, ids []string, deferred []fixtureDeferral) (problems []string) {
	required, deferredDirs, problems := checkFixtureObligations(ids, deferred, root)
	names, found := fixtureDirs(root)
	problems = append(problems, found...)
	have := map[string]bool{}
	for _, name := range names {
		have[name] = true
		if d, ok := deferredDirs[name]; ok {
			problems = append(problems, fmt.Sprintf("%s: the schema is deferred to stage %d by test-targets.md and has fixtures; remove its row there", filepath.Join(root, name), d.Stage))
			continue
		}
		problems = append(problems, checkFixtureDir(r, root, name)...)
	}
	for _, name := range required {
		if !have[name] {
			dir := filepath.Join(root, name)
			problems = append(problems,
				dir+": no accepted example under valid/; the schema has no fixture directory",
				dir+": no refused example in invalid.jsonl or under invalid/; the schema has no fixture directory")
		}
	}
	return problems
}

// fixtureDirs lists the schema directories under root, sorted, and reports
// anything else found there.
func fixtureDirs(root string) (names, problems []string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, []string{err.Error()}
	}
	for _, e := range entries {
		if !e.IsDir() {
			problems = append(problems, fmt.Sprintf("%s: a stray file; fixtures sit under <name>/valid/ and <name>/invalid/", filepath.Join(root, e.Name())))
			continue
		}
		names = append(names, e.Name())
	}
	return names, problems
}

// schemaIDFor turns a fixture directory name into the $id it names.
func schemaIDFor(dir string) string {
	name, major := dir, "1"
	if i := strings.LastIndex(dir, "_v"); i >= 0 && dir[i+2:] != "" && strings.Trim(dir[i+2:], "0123456789") == "" {
		name, major = dir[:i], dir[i+2:]
		if major == "1" || !fixtureMajor.MatchString(major) {
			return ""
		}
	}
	id := idPrefix + strings.ReplaceAll(name, "_", ":") + ":v" + major
	if canonical, ok := fixtureDirFor(id); !ok || canonical != dir {
		return ""
	}
	return id
}

// Two majors of one schema consume different fixtures; neither may borrow
// the other's acceptance or refusal proof.
func TestFixtureWalkerSeparatesSchemaMajors(t *testing.T) {
	const v1 = "urn:hive:schema:example:v1"
	const v2 = "urn:hive:schema:example:v2"
	r, err := Load(fstest.MapFS{
		"example.v1.schema.json": file(v1, `"const":1`),
		"example.v2.schema.json": file(v2, `"const":2`),
	}, ".")
	if err != nil {
		t.Fatal(err)
	}
	root := fixtureTree(t, map[string]string{
		"example/valid/one.json":      "1\n",
		"example/invalid/two.json":    "2\n",
		"example_v2/valid/two.json":   "2\n",
		"example_v2/invalid/one.json": "1\n",
	})
	if got := checkFixtureTree(r, root, r.IDs(), nil); len(got) != 0 {
		t.Fatalf("correct major-specific proofs failed: %v", got)
	}
	if err := os.RemoveAll(filepath.Join(root, "example_v2")); err != nil {
		t.Fatal(err)
	}
	got := checkFixtureTree(r, root, r.IDs(), nil)
	if !mentions(got, "example_v2", "no accepted example") || !mentions(got, "example_v2", "no refused example") {
		t.Errorf("v1 proof satisfied a missing v2 directory: %v", got)
	}
	deferred := []fixtureDeferral{{ID: v2, Stage: 2}}
	if got := checkFixtureTree(r, root, r.IDs(), deferred); len(got) != 0 {
		t.Errorf("a v2 deferral affected the complete v1 proof: %v", got)
	}
	if err := os.Mkdir(filepath.Join(root, "example_v2"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := checkFixtureTree(r, root, r.IDs(), deferred); !mentions(got, "example_v2", "deferred", "has fixtures") {
		t.Errorf("a v2 directory beside its deferral was accepted: %v", got)
	}
}

func TestFixtureWalkerSelectsDefinitionsForLaterCommonMajors(t *testing.T) {
	const id = "urn:hive:schema:common:v2"
	r, err := Load(fstest.MapFS{
		"common.v2.schema.json": file(id, `"$defs":{"sample":{"const":2}}`),
	}, ".")
	if err != nil {
		t.Fatal(err)
	}
	root := fixtureTree(t, map[string]string{
		"common_v2/valid/sample.two.json":   "2\n",
		"common_v2/invalid/sample.one.json": "1\n",
	})
	if got := checkFixtureTree(r, root, r.IDs(), nil); len(got) != 0 {
		t.Errorf("later common major did not select its definition: %v", got)
	}
}

// checkFixtureDir checks the fixtures of the schema directory name under
// root and reports every problem, each starting with the path concerned.
func checkFixtureDir(r *Registry, root, name string) (problems []string) {
	dir := filepath.Join(root, name)
	id := schemaIDFor(name)
	if _, err := r.lookup(id); err != nil {
		return []string{fmt.Sprintf("%s: the directory names no schema: %v", dir, err)}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{err.Error()}
	}
	var valid, invalid []string
	var table string
	for _, e := range entries {
		switch {
		case e.IsDir() && e.Name() == "valid":
			valid = examplesIn(filepath.Join(dir, "valid"), &problems)
		case e.IsDir() && e.Name() == "invalid":
			invalid = examplesIn(filepath.Join(dir, "invalid"), &problems)
		case !e.IsDir() && e.Name() == "invalid.jsonl":
			table = filepath.Join(dir, "invalid.jsonl")
		default:
			problems = append(problems, fmt.Sprintf("%s: a stray entry; only valid/, invalid.jsonl and invalid/ belong here", filepath.Join(dir, e.Name())))
		}
	}
	var rows []fixtureRow
	if table != "" {
		data, err := os.ReadFile(table)
		if err != nil {
			problems = append(problems, err.Error())
		}
		var faults []string
		rows, faults = parseFixtureTable(data)
		for _, f := range faults {
			problems = append(problems, table+": "+f)
		}
	}
	if len(valid) == 0 {
		problems = append(problems, dir+": no accepted example under valid/")
	}
	if len(invalid)+len(rows) == 0 {
		problems = append(problems, dir+": no refused example in invalid.jsonl or under invalid/")
	}
	accepted := map[string]bool{}
	for _, p := range valid {
		accepted[filepath.Base(p)] = true
		ref, ok := refFor(name, id, p, &problems)
		if !ok {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		switch err := r.Validate(ref, data); {
		case err == nil:
			// Accepted, as it must be.
		case errors.Is(err, ErrUnknownSchema):
			problems = append(problems, fmt.Sprintf("%s: could not be checked against %s: %v", p, ref, err))
		default:
			problems = append(problems, fmt.Sprintf("%s: was refused by %s:\n%s", p, ref, describe(err)))
		}
	}
	files := map[string]bool{}
	for _, p := range invalid {
		files[strings.TrimSuffix(filepath.Base(p), ".json")] = true
		ref, ok := refFor(name, id, p, &problems)
		if !ok {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		err = r.Validate(ref, data)
		var ve *ValidationError
		var se *SyntaxError
		switch {
		case err == nil:
			problems = append(problems, fmt.Sprintf("%s: was accepted by %s", p, ref))
		case errors.As(err, &ve), errors.As(err, &se):
			// Refused, as it must be.
		default:
			problems = append(problems, fmt.Sprintf("%s: could not be checked against %s: %v", p, ref, err))
		}
	}
	for _, row := range rows {
		problems = append(problems, checkFixtureRow(r, dir, name, id, table, row, accepted, files)...)
	}
	return problems
}

// checkFixtureRow builds the case of one row and reports every problem with
// it.
func checkFixtureRow(r *Registry, dir, name, id, table string, row fixtureRow, accepted, files map[string]bool) (problems []string) {
	at := fmt.Sprintf("%s:%d:%s", table, row.Line, row.Name)
	fail := func(format string, args ...any) {
		problems = append(problems, at+": "+fmt.Sprintf(format, args...))
	}
	if files[row.Name] {
		fail("a row of this name and the file invalid/%s.json both hold a case", row.Name)
		return problems
	}
	ref, ok := refFor(name, id, row.Name+".json", &problems)
	if !ok {
		problems[len(problems)-1] = at + ": " + strings.TrimPrefix(problems[len(problems)-1], row.Name+".json: ")
		return problems
	}
	if !accepted[row.Base] {
		fail("the base %s names no file under valid/", row.Base)
		return problems
	}
	baseRef, ok := refFor(name, id, row.Base, &problems)
	if !ok {
		problems[len(problems)-1] = at + ": the base " + strings.TrimPrefix(problems[len(problems)-1], row.Base+": ")
		return problems
	}
	if baseRef != ref {
		fail("the base %s is an example of a different definition than the row, %s", row.Base, ref)
		return problems
	}
	base, err := os.ReadFile(filepath.Join(dir, "valid", row.Base))
	if err != nil {
		fail("%v", err)
		return problems
	}
	if err := r.Validate(ref, base); err != nil {
		fail("the base %s is not accepted by %s", row.Base, ref)
		return problems
	}
	doc, err := constructFixture(base, row)
	if err != nil {
		fail("the case cannot be built: %v", err)
		return problems
	}
	err = r.Validate(ref, doc)
	var ve *ValidationError
	var se *SyntaxError
	switch {
	case err == nil:
		fail("was accepted by %s", ref)
	case errors.As(err, &se):
		fail("the decoder refused the case the edits built, so it does not prove %s: %v", ref, err)
	case errors.As(err, &ve):
		// Refused by the schema, as it must be.
	default:
		fail("could not be checked against %s: %v", ref, err)
	}
	return problems
}

// examplesIn lists the example files of one valid/ or invalid/ folder, and
// reports anything there that is not a .json file.
func examplesIn(dir string, problems *[]string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		*problems = append(*problems, err.Error())
		return nil
	}
	var files []string
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		switch {
		case e.IsDir():
			*problems = append(*problems, p+": a folder; an example is a .json file")
		case !strings.HasSuffix(e.Name(), ".json"):
			*problems = append(*problems, p+": not a .json file")
		default:
			files = append(files, p)
		}
	}
	return files
}

// refFor names the schema the example at p is checked against: the
// directory's schema, or for common, the definition the file name selects.
func refFor(name, id, p string, problems *[]string) (string, bool) {
	if !strings.HasPrefix(id, idPrefix+"common:v") {
		return id, true
	}
	def, label, found := strings.Cut(strings.TrimSuffix(filepath.Base(p), ".json"), ".")
	if !found || def == "" || label == "" {
		*problems = append(*problems, p+": names no definition; an example of common is called <definition>.<label>.json")
		return "", false
	}
	return id + "#/$defs/" + def, true
}

// describe writes every violation of a refusal, one per line, or the error
// itself when it is not a validation error.
func describe(err error) string {
	var ve *ValidationError
	if !errors.As(err, &ve) {
		return "  " + err.Error()
	}
	lines := make([]string, 0, len(ve.Violations)+1)
	for _, v := range ve.Violations {
		lines = append(lines, "  "+v.String())
	}
	if ve.Omitted > 0 {
		lines = append(lines, fmt.Sprintf("  ... and %d more", ve.Omitted))
	}
	return strings.Join(lines, "\n")
}
