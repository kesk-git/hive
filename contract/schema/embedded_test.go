package schema

import (
	"encoding/json"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kesk-git/hive/spec"
)

// committed returns every file of the embedded schema tree, by path below
// "schemas", decoded.
func committed(t *testing.T) map[string]any {
	t.Helper()
	docs := map[string]any{}
	err := fs.WalkDir(spec.Schemas, "schemas", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(spec.Schemas, p)
		if err != nil {
			return err
		}
		doc, err := Decode(data)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		docs[strings.TrimPrefix(p, "schemas/")] = doc
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return docs
}

// Every committed schema stays inside the profile and every reference in
// them resolves: Load refuses the tree otherwise.
func TestCommittedSchemasStayInsideTheProfileAndResolve(t *testing.T) {
	r, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	docs := committed(t)
	if len(docs) < 20 {
		t.Fatalf("only %d schema files are embedded", len(docs))
	}
	var want []string
	for rel, doc := range docs {
		id, err := idForPath(rel)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if got := doc.(map[string]any)["$id"]; got != id {
			t.Errorf("%s: $id %v, want %s", rel, got, id)
		}
		want = append(want, id)
	}
	slices.Sort(want)
	if got := r.IDs(); !slices.Equal(got, want) {
		t.Errorf("IDs = %v, want %v", got, want)
	}
	if again, err := Embedded(); err != nil || again != r {
		t.Error("Embedded does not return the one registry")
	}
}

// refsIn lists every $ref string in a decoded schema.
func refsIn(v any, out *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, c := range t {
			if s, ok := c.(string); ok && k == "$ref" {
				*out = append(*out, s)
			}
			refsIn(c, out)
		}
	case []any:
		for _, c := range t {
			refsIn(c, out)
		}
	}
}

func TestEveryCommittedReferenceResolvesToASchema(t *testing.T) {
	r, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, doc := range committed(t) {
		refsIn(doc, &refs)
	}
	if len(refs) < 100 {
		t.Fatalf("found only %d references", len(refs))
	}
	for _, ref := range refs {
		n, err := r.lookup(ref)
		if err != nil {
			t.Errorf("%s: %v", ref, err)
		} else if !strings.HasPrefix(n.loc, strings.SplitN(ref, "#", 2)[0]+"#") {
			t.Errorf("%s resolved to %s", ref, n.loc)
		}
	}
}

// edit decodes every committed file, lets change alter the first file named
// rel, and loads the result.
func loadEdited(t *testing.T, rel string, change func(map[string]any)) error {
	t.Helper()
	files := fstest.MapFS{}
	for name, doc := range committed(t) {
		if name == rel {
			change(doc.(map[string]any))
		}
		data, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	_, err := Load(files, ".")
	return err
}

// The checks are not vacuous: a committed schema, altered in one small way, is
// refused. Each alteration is made to one representative, the schema of the
// request envelope; the refusal of each alteration is proved once on a
// synthetic schema as well, and the unaltered load of every committed schema
// is proved by TestCommittedSchemasStayInsideTheProfileAndResolve.
func TestCommittedSchemasAreRefusedWhenAltered(t *testing.T) {
	const rel = "request.v1.schema.json"
	if err := loadEdited(t, "", func(map[string]any) {}); err != nil {
		t.Fatalf("an unaltered copy is refused: %v", err)
	}
	alterations := []struct {
		name   string
		change func(map[string]any)
	}{
		{"a format keyword", func(m map[string]any) { m["format"] = "date-time" }},
		{"a changed $id", func(m map[string]any) { m["$id"] = "urn:hive:schema:elsewhere:v1" }},
		{"another dialect", func(m map[string]any) { m["$schema"] = "http://json-schema.org/draft-07/schema#" }},
		{"a reference to nothing", func(m map[string]any) {
			m["$defs"] = map[string]any{"cut": map[string]any{"$ref": "urn:hive:schema:common:v1#/$defs/cut"}}
		}},
		{"unevaluatedProperties", func(m map[string]any) { m["unevaluatedProperties"] = false }},
		{"an object that was opened", func(m map[string]any) { m["additionalProperties"] = true }},
	}
	for _, a := range alterations {
		t.Run(a.name, func(t *testing.T) {
			if err := loadEdited(t, rel, a.change); err == nil {
				t.Errorf("%s in %s was accepted", a.name, rel)
			}
		})
	}
}

func TestCommittedSchemasHoldNothingButSchemaFiles(t *testing.T) {
	for rel := range committed(t) {
		if !strings.HasSuffix(rel, ".schema.json") {
			t.Errorf("%s is not a schema file", rel)
		}
	}
}

// The validator runs the committed common definitions. These are a few
// instances for the rules that exercise the validator hardest, not the
// fixtures of any record.
func TestValidatorRunsTheCommonDefinitions(t *testing.T) {
	r, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	const (
		ulid = "01arz3ndektsv4rrffq69g5fav"
		sum  = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	)
	check := func(def string, ok bool, docs ...string) {
		t.Helper()
		for _, doc := range docs {
			err := r.Validate("urn:hive:schema:common:v1#/$defs/"+def, []byte(doc))
			if ok && err != nil {
				t.Errorf("%s: %s was refused: %v", def, doc, err)
			}
			if !ok && err == nil {
				t.Errorf("%s: %s was accepted", def, doc)
			}
		}
	}
	check("home_id", true, `"home_`+ulid+`"`)
	check("home_id", false, `"ws_`+ulid+`"`, `"home_`+ulid+`0"`, `"home_`+strings.ToUpper(ulid)+`"`, `"home_`+ulid[:25]+`"`, `"home_8`+ulid[1:]+`"`, `"home_`+ulid[:24]+`l"`, `1`, `null`)
	check("timestamp", true, `"2026-10-11T08:00:00.000Z"`)
	check("timestamp", false, `"2026-10-11T08:00:00Z"`, `"2026-10-11T08:00:00.000+00:00"`, `"2026-10-11T08:00:00.0000Z"`, `"2026-10-11 08:00:00.000Z"`, `""`)
	check("digest", true, `"`+sum+`"`)
	check("digest", false, `"`+strings.ToUpper(sum)+`"`, `"sha256:00"`, `"sha1:`+sum[7:]+`"`, `"`+sum+`0"`)
	check("home_relative_path", true, `"a"`, `"a/b/c.json"`, `"a.b/c"`, `".hidden/x"`, `"a..b"`, `"..a/b"`, `"console/x"`, `"com10"`, `"a/conin"`, `"conin$x"`, `"nul-0"`)
	check("home_relative_path", false, `""`, `"/a"`, `"a/"`, `"a//b"`, `"./a"`, `"a/./b"`, `"a/.."`, `".."`, `"."`, `"../a"`, `"a/../b"`, "\"a\\u0000b\"", `"con"`, `"a/NUL.txt"`, `"a/Com1"`, `"a/lpt9.log"`, "\"a/COM\u00b9\"", "\"a/lpt\u00b3.x\"", "\"a/com\u00b2\"", `"a/CONIN$"`, `"a/conout$.txt"`, `"a/con .txt"`, `"con  .txt"`, `"a\\b"`, `"a:b"`)
	check("record_revision", true, `1`, `9007199254740991`)
	check("record_revision", false, `0`, `-1`, `"1"`, `null`)
	check("package_name", true, `"pool"`, `"spawning-pool"`)
	check("package_name", false, `"spawningpool"`, `"worktree-pool"`, `"Pool"`)
	// A conditional schema, an unlisted member of a closed object and a
	// reference with a pointer, together.
	check("authority", true, `{"kind":"none"}`, `{"kind":"word","ref":{"schema":"word.record","id":"x"}}`)
	check("authority", false, `{"kind":"none","ref":{"schema":"word.record","id":"x"}}`, `{"kind":"word"}`, `{"kind":"other"}`, `{"kind":"none","x":1}`, `{"kind":"word","ref":{"schema":"word.record"}}`, `{}`)
	check("caller", true, `{"kind":"test"}`, `{"kind":"package","package":"pool","command":"hive-pool"}`)
	check("caller", false, `{"kind":"package","package":"pool"}`, `{"kind":"worker","task_id":"task_`+ulid+`"}`, `{"kind":"supervisor"}`)
	// The scope rules that other records apply through allOf.
	check("scope_rules", true, `{"scope_level":"hive"}`, `{"scope_level":"workspace","workspace_id":"ws_`+ulid+`"}`, `{"scope_level":"project","workspace_id":"ws_`+ulid+`","project_id":"proj_`+ulid+`"}`, `{"task_id":"task_`+ulid+`","generation":1}`, `{}`)
	check("scope_rules", false, `{"scope_level":"hive","project_id":"proj_`+ulid+`"}`, `{"scope_level":"workspace"}`, `{"scope_level":"workspace","workspace_id":"ws_`+ulid+`","clone_id":"clone_`+ulid+`"}`, `{"scope_level":"project","workspace_id":"ws_`+ulid+`"}`, `{"generation":1}`)
}
