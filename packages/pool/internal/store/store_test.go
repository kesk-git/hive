package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/schema"
	cstore "github.com/kesk-git/hive/contract/store"
)

// fixtures is the tree of the schema fixtures of the contract.
var fixtures = filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas")

// parse reads a JSON object.
func parse(t testing.TB, data []byte) map[string]any {
	t.Helper()
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)
}

// fixture reads a valid fixture of the kind.
func fixture(t testing.TB, k Kind, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtures, "records_"+k.Schema, "valid", name))
	if err != nil {
		t.Fatal(err)
	}
	return parse(t, data)
}

// fixtureDir is the fixture directory of a kind, which names a schema of a
// later major version with a suffix.
func fixtureDir(k Kind) string {
	if major, _, _ := strings.Cut(k.Version, "."); major != "1" {
		return "records_" + k.Schema + "_v" + major
	}
	return "records_" + k.Schema
}

// Each kind the Pool stores is dispatched by the schema its records name, and
// every valid fixture of the kind passes the check a write makes, under the
// identity it carries.
func TestEveryKindDispatchesAndItsFixturesPass(t *testing.T) {
	for _, k := range kinds {
		t.Run(k.Name, func(t *testing.T) {
			if got, ok := Dispatch(k.Schema); !ok || got.Name != k.Name {
				t.Fatalf("the schema %s dispatches to %v", k.Schema, got)
			}
			entries, err := os.ReadDir(filepath.Join(fixtures, fixtureDir(k), "valid"))
			if err != nil || len(entries) == 0 {
				t.Fatalf("no fixtures: %v", err)
			}
			for _, e := range entries {
				data, err := os.ReadFile(filepath.Join(fixtures, fixtureDir(k), "valid", e.Name()))
				if err != nil {
					t.Fatal(err)
				}
				id, _ := parse(t, data)[k.IDField].(string)
				if err := k.Check(data, id, true); err != nil {
					t.Errorf("%s: %v", e.Name(), err)
				}
				// A record is stored under its own identity and no other.
				if err := k.Check(data, "someone_else", true); !errors.Is(err, cstore.ErrRecordMalformed) && k.Fixed == "" {
					t.Errorf("%s under another identity: %v", e.Name(), err)
				}
			}
		})
	}
}

// The newest version the table says a kind has is the newest its schema file
// accepts, so that a record of a newer one is refused and one of the schema's
// own is not.
func TestTheTableKnowsTheNewestVersionOfEachSchema(t *testing.T) {
	for _, k := range kinds {
		_, major, _ := strings.Cut(k.Ref(), ":v")
		files, _ := filepath.Glob(filepath.Join("..", "..", "..", "..", "spec", "schemas", "records", k.Schema+".v"+major+".schema.json"))
		if len(files) != 1 {
			t.Fatalf("%s: the schema file of %s", k.Name, k.Ref())
		}
		data, err := os.ReadFile(files[0])
		if err != nil {
			t.Fatal(err)
		}
		var versions []any
		if enum, ok := parse(t, data)["properties"].(map[string]any)["schema_version"].(map[string]any)["enum"]; ok {
			versions = enum.([]any)
		}
		if len(versions) == 0 || versions[len(versions)-1] != k.Version {
			t.Errorf("%s: the table has %s, the schema accepts %v", k.Name, k.Version, versions)
		}
	}
}

// A write is refused for a version the build does not read, a credential
// shape, a record that breaks its schema and one filed under another
// identity; a read does not apply the no-value check.
func TestACheckNamesWhatIsWrongWithARecord(t *testing.T) {
	k, _ := Lookup("hold")
	good := fixture(t, k, "active-parked.json")
	id := good["hold_id"].(string)
	encode := func(m map[string]any) []byte {
		out, err := canon.Encode(m)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	edit := func(change func(m map[string]any)) []byte {
		m := parse(t, encode(good))
		change(m)
		return encode(m)
	}
	var shape *schema.ShapeError
	var violation *schema.ValidationError
	tests := []struct {
		name  string
		data  []byte
		check func(error) bool
	}{
		{"newer minor", edit(func(m map[string]any) { m["schema_version"] = "1.1" }), func(err error) bool { return errors.Is(err, cstore.ErrUnsupportedVersion) }},
		{"newer major", edit(func(m map[string]any) { m["schema_version"] = "2.0" }), func(err error) bool { return errors.Is(err, cstore.ErrUnsupportedVersion) }},
		{"a credential shape", edit(func(m map[string]any) { m["reason"] = "key ghp_" + strings.Repeat("a", 36) }), func(err error) bool { return errors.As(err, &shape) && !strings.Contains(err.Error(), "ghp_") }},
		{"a required member missing", edit(func(m map[string]any) { delete(m, "reason") }), func(err error) bool { return errors.As(err, &violation) }},
		{"another schema", edit(func(m map[string]any) { m["schema"] = "pool.item" }), func(err error) bool { return errors.Is(err, cstore.ErrRecordMalformed) }},
		{"not an object", []byte(`[]`), func(err error) bool { return errors.Is(err, cstore.ErrRecordMalformed) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := k.Check(tc.data, id, true); err == nil || !tc.check(err) {
				t.Errorf("a write: %v", err)
			}
		})
	}
	// Failure gives each the code the contract names.
	for name, want := range map[string]string{
		"a credential shape":        envelope.CodeSecretShapeRecognized,
		"a required member missing": envelope.CodeSchemaViolation,
		"newer minor":               envelope.CodeUnsupportedVersion,
		"another schema":            envelope.CodeRecordMalformed,
	} {
		for _, tc := range tests {
			if tc.name != name {
				continue
			}
			if f := Failure("a hold", k.Check(tc.data, id, true)); f.Err.Code != want {
				t.Errorf("%s: %s, not %s", name, f.Err.Code, want)
			}
		}
	}
	// A read reads a record as it was written.
	if err := k.Check(tests[2].data, id, false); err != nil {
		t.Errorf("a read of a record holding a shape: %v", err)
	}
}

// The identities a record can be stored under are lowercase, one segment, and
// the fixed identity of a kind of which the home keeps one is the only one.
func TestAnIdentityIsAFileNameAndNothingElse(t *testing.T) {
	hold, _ := Lookup("hold")
	budget, _ := Lookup("budget")
	for _, id := range []string{"", "../x", "a/b", "Hold_1", "a b", "a.json", `a\b`, strings.Repeat("a", 65)} {
		if p, err := hold.Path(id); err == nil {
			t.Errorf("%q is stored at %s", id, p)
		}
	}
	if p, err := hold.Path("hold_01jc7d8e9f0g1h2j3k4m5n6p7m"); err != nil || p != "pool/records/hold/hold_01jc7d8e9f0g1h2j3k4m5n6p7m.json" {
		t.Errorf("a hold: %s, %v", p, err)
	}
	if p, err := budget.Path(""); err != nil || p != "pool/records/budget/memory.json" {
		t.Errorf("the budget: %s, %v", p, err)
	}
	if _, err := budget.Path("other"); err == nil {
		t.Error("a second budget")
	}
}
