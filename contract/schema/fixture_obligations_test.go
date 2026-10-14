package schema

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// specTestTargets is the file whose "Schema fixture obligations" section names
// the schemas that are not yet owed fixtures.
const specTestTargets = "../../spec/test-targets.md"

const obligationsHeading = "### Schema fixture obligations"

var (
	obligationRow   = regexp.MustCompile("^\\| `([^`|]+)` \\| ([^|]+) \\|$")
	obligationStage = regexp.MustCompile(`^stage ([0-9]+)$`)
	fixtureMajor    = regexp.MustCompile(`^[1-9][0-9]*$`)
)

// parseFixtureDeferrals reads the table of the obligations section of
// test-targets.md into the schemas it defers. Each path is read as the path of
// a schema file below spec/schemas/, and gives that schema's $id.
func parseFixtureDeferrals(text string) ([]fixtureDeferral, error) {
	lines := strings.Split(text, "\n")
	start := -1
	for i, l := range lines {
		if l == obligationsHeading {
			if start >= 0 {
				return nil, fmt.Errorf("the heading %q appears twice", obligationsHeading)
			}
			start = i + 1
		}
	}
	if start < 0 {
		return nil, fmt.Errorf("no heading %q", obligationsHeading)
	}
	var table []string
	for _, l := range lines[start:] {
		if strings.HasPrefix(l, "#") {
			break
		}
		if strings.HasPrefix(l, "|") {
			table = append(table, l)
		}
	}
	const header = "| Schema path under `spec/schemas/` | Fixtures owed |"
	if len(table) < 2 || table[0] != header || table[1] != "| --- | --- |" {
		return nil, fmt.Errorf("the obligations section holds no table headed %q", header)
	}
	var out []fixtureDeferral
	for _, l := range table[2:] {
		m := obligationRow.FindStringSubmatch(l)
		if m == nil {
			return nil, fmt.Errorf("%q is not a row of a schema path and its stage", l)
		}
		id, err := idForPath(m[1])
		if err != nil || !strings.HasSuffix(m[1], ".schema.json") {
			return nil, fmt.Errorf("%q is not the path of a schema file: %v", m[1], err)
		}
		s := obligationStage.FindStringSubmatch(m[2])
		if s == nil {
			return nil, fmt.Errorf("%q: %q is not a stage", m[1], m[2])
		}
		var stage int
		fmt.Sscan(s[1], &stage)
		out = append(out, fixtureDeferral{ID: id, Stage: stage})
	}
	return out, nil
}

// readFixtureDeferrals reads the committed obligations.
func readFixtureDeferrals(t testing.TB) []fixtureDeferral {
	t.Helper()
	data, err := os.ReadFile(specTestTargets)
	if err != nil {
		t.Fatal(err)
	}
	deferred, err := parseFixtureDeferrals(string(data))
	if err != nil {
		t.Fatalf("%s: %v", specTestTargets, err)
	}
	return deferred
}

// fixtureDirFor is the fixture directory of the schema id: the inverse of
// schemaIDFor.
func fixtureDirFor(id string) (string, bool) {
	rest, ok := strings.CutPrefix(id, idPrefix)
	if !ok {
		return "", false
	}
	i := strings.LastIndex(rest, ":v")
	if i <= 0 || !fixtureMajor.MatchString(rest[i+2:]) {
		return "", false
	}
	name, major := rest[:i], rest[i+2:]
	if canonical, err := idForPath(strings.ReplaceAll(name, ":", "/") + ".v" + major + ".schema.json"); err != nil || canonical != id {
		return "", false
	}
	dir := strings.ReplaceAll(name, ":", "_")
	if major != "1" {
		dir += "_v" + major
	}
	return dir, true
}

func TestFixtureDirectoriesKeepTheirSchemaMajor(t *testing.T) {
	for _, c := range []struct{ id, dir string }{
		{"urn:hive:schema:request:v1", "request"},
		{"urn:hive:schema:records:pool.rule:v2", "records_pool.rule_v2"},
		{"urn:hive:schema:ops:hive-memory.fold.response:v12", "ops_hive-memory.fold.response_v12"},
		{"urn:hive:schema:common:v2", "common_v2"},
		{"urn:hive:schema:records:views:v1", "records_views"},
	} {
		got, ok := fixtureDirFor(c.id)
		if !ok || got != c.dir {
			t.Errorf("fixtureDirFor(%q) = %q, %v; want %q, true", c.id, got, ok, c.dir)
		}
		if got := schemaIDFor(c.dir); got != c.id {
			t.Errorf("schemaIDFor(%q) = %q, want %q", c.dir, got, c.id)
		}
	}
	for _, id := range []string{"elsewhere:request:v2", "urn:hive:schema:request", "urn:hive:schema:request:v0", "urn:hive:schema:request:v02", "urn:hive:schema:request:v2#/$defs/value"} {
		if dir, ok := fixtureDirFor(id); ok {
			t.Errorf("malformed schema id %q mapped to %q", id, dir)
		}
	}
	for _, dir := range []string{"request_v1", "request_v0", "request_v02", "request_", "_v2", "request/v2"} {
		if id := schemaIDFor(dir); id != "" {
			t.Errorf("noncanonical directory %q mapped to %q", dir, id)
		}
	}
}

// checkFixtureObligations reports what is wrong with deferred itself, and
// returns the directory of each schema ids holds that is deferred and the
// directory of each one that is required.
func checkFixtureObligations(ids []string, deferred []fixtureDeferral, root string) (required []string, deferredDirs map[string]fixtureDeferral, problems []string) {
	known := map[string]bool{}
	for _, id := range ids {
		known[id] = true
	}
	deferredDirs = map[string]fixtureDeferral{}
	for _, d := range deferred {
		dir, ok := fixtureDirFor(d.ID)
		switch {
		case !known[d.ID]:
			problems = append(problems, fmt.Sprintf("%s: the deferral names no schema", d.ID))
		case !ok:
			problems = append(problems, fmt.Sprintf("%s: the schema has no fixture directory name", d.ID))
		case d.Stage != 2:
			problems = append(problems, fmt.Sprintf("%s: stage %d is not a stage that defers fixtures; only stage 2 does", d.ID, d.Stage))
		default:
			if _, dup := deferredDirs[dir]; dup {
				problems = append(problems, fmt.Sprintf("%s: the schema is deferred twice", d.ID))
			}
			deferredDirs[dir] = d
		}
	}
	for _, id := range ids {
		dir, ok := fixtureDirFor(id)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: the schema has no fixture directory name", id))
			continue
		}
		if _, skip := deferredDirs[dir]; !skip {
			required = append(required, dir)
		}
	}
	return required, deferredDirs, problems
}

func TestFixtureObligationsAreRead(t *testing.T) {
	const table = obligationsHeading + `

Prose.

| Schema path under ` + "`spec/schemas/`" + ` | Fixtures owed |
| --- | --- |
| ` + "`ops/hive-pool.read.request.v1.schema.json`" + ` | stage 2 |
| ` + "`records/pool.item.v1.schema.json`" + ` | stage 2 |

## Next
| ` + "`ignored.v1.schema.json`" + ` | stage 2 |
`
	got, err := parseFixtureDeferrals(table)
	if err != nil {
		t.Fatal(err)
	}
	want := []fixtureDeferral{
		{ID: "urn:hive:schema:ops:hive-pool.read.request:v1", Stage: 2},
		{ID: "urn:hive:schema:records:pool.item:v1", Stage: 2},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("deferrals = %v, want %v", got, want)
	}
	if got, err := parseFixtureDeferrals(obligationsHeading + "\n\n| Schema path under `spec/schemas/` | Fixtures owed |\n| --- | --- |\n"); err != nil || len(got) != 0 {
		t.Errorf("an empty table: %v, %v", got, err)
	}
}

func TestFixtureObligationsRefuseAMalformedTable(t *testing.T) {
	head := obligationsHeading + "\n\n| Schema path under `spec/schemas/` | Fixtures owed |\n| --- | --- |\n"
	for _, c := range []struct{ why, text string }{
		{"no section", "# Test targets\n"},
		{"two sections", head + "\n" + obligationsHeading + "\n"},
		{"no table", obligationsHeading + "\n\nProse.\n"},
		{"another header", strings.Replace(head, "Fixtures owed", "Stage", 1)},
		{"a row of one cell", head + "| `request.v1.schema.json` |\n"},
		{"a path that is not backquoted", head + "| request.v1.schema.json | stage 2 |\n"},
		{"a path of no schema file", head + "| `request.json` | stage 2 |\n"},
		{"a path of an uppercase directory", head + "| `Ops/request.v1.schema.json` | stage 2 |\n"},
		{"a stage in words", head + "| `request.v1.schema.json` | later |\n"},
		{"a bare number", head + "| `request.v1.schema.json` | 2 |\n"},
	} {
		if got, err := parseFixtureDeferrals(c.text); err == nil {
			t.Errorf("%s: read %v", c.why, got)
		}
	}
}

func TestFixtureObligationsNameRealSchemas(t *testing.T) {
	r, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	deferred := readFixtureDeferrals(t)
	_, _, problems := checkFixtureObligations(r.IDs(), deferred, fixtureRoot)
	for _, p := range problems {
		t.Error(p)
	}
	if len(deferred) == 0 {
		t.Log("no schema defers its fixtures")
	}
}
