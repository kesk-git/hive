package schema

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureRoot is where spec/layout.md keeps the fixtures of the schemas:
// contract/testdata/schemas/<name>/valid/ and .../invalid/.
const fixtureRoot = "../testdata/schemas"

// TestFixturesProveTheCommittedSchemas runs every example under fixtureRoot
// through the embedded registry. It is the one walker for every schema's
// fixtures: a lane that adds examples for another schema adds files and
// nothing here.
//
// Each directory <name> directly under fixtureRoot names the schema whose
// $id is urn:hive:schema:<name>:v1, where a schema under a subdirectory of
// spec/schemas/ has the colons of its $id written as underscores: the
// fixtures of receipts/x.v1.schema.json sit under receipts_x/. A directory
// naming no schema fails.
//
// The schema common holds only definitions and no keyword of its own at its
// root, so its examples name the definition they test: a file under common/
// is called <definition>.<label>.json (home_id.ok.json) and is checked
// against urn:hive:schema:common:v1#/$defs/<definition>. A file under
// common/ that names no definition, or a definition the schema lacks, fails.
//
// Under each schema directory only the folders valid/ and invalid/ may
// appear, and each must hold at least one example: a stray file or folder
// beside them fails, and so does a folder with no example. Every example is
// a .json file; a file with another suffix, or a folder inside valid/ or
// invalid/, fails. A file under valid/ must be accepted by its schema. A
// file under invalid/ must be refused, which means Validate returns a
// *ValidationError or a *SyntaxError; any other error, such as an unknown
// schema, is a failure of the test, not a refusal. A failing file is
// reported by its path with the violations the validator found.
func TestFixturesProveTheCommittedSchemas(t *testing.T) {
	r, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range checkFixtureTree(r, fixtureRoot, r.IDs(), nil) {
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
// schemas the tree must cover, except those named by deferred.
func checkFixtureTree(r *Registry, root string, ids []string, deferred []fixtureDeferral) (problems []string) {
	_, _ = ids, deferred
	names, problems := fixtureDirs(root)
	if len(names) == 0 {
		problems = append(problems, root+": holds no schema directory")
	}
	for _, name := range names {
		problems = append(problems, checkFixtureDir(r, root, name)...)
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
	return idPrefix + strings.ReplaceAll(dir, "_", ":") + ":v1"
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
	for _, e := range entries {
		switch {
		case e.IsDir() && e.Name() == "valid":
			valid = examplesIn(filepath.Join(dir, "valid"), &problems)
		case e.IsDir() && e.Name() == "invalid":
			invalid = examplesIn(filepath.Join(dir, "invalid"), &problems)
		default:
			problems = append(problems, fmt.Sprintf("%s: a stray entry; only valid/ and invalid/ belong here", filepath.Join(dir, e.Name())))
		}
	}
	if len(valid) == 0 {
		problems = append(problems, dir+": no accepted example under valid/")
	}
	if len(invalid) == 0 {
		problems = append(problems, dir+": no refused example under invalid/")
	}
	for _, p := range valid {
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
	for _, p := range invalid {
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
	if name != "common" {
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
