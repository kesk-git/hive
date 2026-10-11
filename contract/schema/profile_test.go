package schema

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	testID   = "urn:hive:schema:t:v1"
	testFile = "t.v1.schema.json"
)

// file writes a schema file whose $id is id and whose other keywords are body.
func file(id, body string) *fstest.MapFile {
	doc := `{"$schema":"` + metaSchema + `","$id":"` + id + `"`
	if body != "" {
		doc += "," + body
	}
	return &fstest.MapFile{Data: []byte(doc + "}")}
}

// one loads a tree of the single schema t, with the given keywords.
func one(body string) (*Registry, error) {
	return Load(fstest.MapFS{testFile: file(testID, body)}, ".")
}

func mustOne(t *testing.T, body string) *Registry {
	t.Helper()
	r, err := one(body)
	if err != nil {
		t.Fatalf("schema %s: %v", body, err)
	}
	return r
}

func problemsOf(t *testing.T, err error) []Problem {
	t.Helper()
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("got %v (%T), want a *LoadError", err, err)
	}
	return le.Problems
}

// wantProblem checks that err names a problem at where whose text contains msg.
func wantProblem(t *testing.T, err error, where, msg string) {
	t.Helper()
	if err == nil {
		t.Errorf("no problem; want %s: %s", where, msg)
		return
	}
	for _, p := range problemsOf(t, err) {
		if p.Where == where && strings.Contains(p.Msg, msg) {
			return
		}
	}
	t.Errorf("no problem %s: %s in\n%v", where, msg, err)
}

func TestProfileHoldsEveryKeywordOfTheSpecification(t *testing.T) {
	r := mustOne(t, `
	  "title":"all", "description":"every keyword at once",
	  "$defs": {
	    "text": {"type":"string","minLength":1,"maxLength":5,"pattern":"^[a-z]+$"},
	    "count": {"type":"integer","minimum":0,"maximum":9}
	  },
	  "type":"object", "additionalProperties": false,
	  "required":["a"],
	  "properties": {
	    "a": {"$ref":"`+testID+`#/$defs/text"},
	    "b": {"type":"array","items":{"$ref":"`+testID+`#/$defs/count"},"minItems":0,"maxItems":3},
	    "c": {"enum":["x",1,null,true]},
	    "d": {"const":{"k":[1]}},
	    "e": {"anyOf":[true,false]},
	    "f": {"allOf":[{"type":"string"}],"oneOf":[{"minLength":1},{"maxLength":0}],"not":{"const":"no"}},
	    "g": {"if":{"type":"string"},"then":{"minLength":1},"else":{"type":"null"}}
	  }`)
	if got := r.IDs(); !slices.Equal(got, []string{testID}) {
		t.Errorf("IDs = %v", got)
	}
}

func TestProfileRefusesAnyOtherKeyword(t *testing.T) {
	for _, kw := range []string{
		"format", "patternProperties", "unevaluatedProperties", "unevaluatedItems",
		"$dynamicRef", "$dynamicAnchor", "$anchor", "$comment", "$vocabulary",
		"prefixItems", "contains", "minContains", "maxContains", "uniqueItems",
		"multipleOf", "exclusiveMinimum", "exclusiveMaximum", "minProperties",
		"maxProperties", "propertyNames", "dependentRequired", "dependentSchemas",
		"dependencies", "definitions", "default", "examples", "deprecated",
		"readOnly", "writeOnly", "contentEncoding", "contentMediaType",
		"contentSchema", "additionalItems", "$recursiveRef", "x-anything",
	} {
		_, err := one(`"type":"string","` + kw + `":true`)
		wantProblem(t, err, testID+"#/"+kw, `keyword "`+kw+`" is not in the profile`)
	}
}

func TestProfileRefusesMalformedKeywords(t *testing.T) {
	cases := []struct{ body, at, msg string }{
		{`"title":1`, "/title", "string"},
		{`"description":[]`, "/description", "string"},
		{`"type":"text"`, "/type", "not a JSON type"},
		{`"type":[]`, "/type", "no name"},
		{`"type":["string","string"]`, "/type", "twice"},
		{`"type":5`, "/type", "type name"},
		{`"type":[1]`, "/type", "strings"},
		{`"enum":"a"`, "/enum", "an array"},
		{`"required":"a"`, "/required", "array"},
		{`"required":[1]`, "/required", "strings"},
		{`"required":["a","a"]`, "/required", "twice"},
		{`"additionalProperties":{}`, "/additionalProperties", "only ever a boolean"},
		{`"additionalProperties":{"type":"string"}`, "/additionalProperties", "only ever a boolean"},
		{`"properties":[]`, "/properties", "object of schemas"},
		{`"properties":{"a":1}`, "/properties/a", "object or a boolean"},
		{`"properties":{"a":null}`, "/properties/a", "object or a boolean"},
		{`"$defs":{"a":"x"}`, "/$defs/a", "object or a boolean"},
		{`"items":[{}]`, "/items", "object or a boolean"},
		{`"items":5`, "/items", "object or a boolean"},
		{`"minItems":-1`, "/minItems", "zero or more"},
		{`"maxItems":"1"`, "/maxItems", "whole number"},
		{`"minLength":-1`, "/minLength", "zero or more"},
		{`"maxLength":true`, "/maxLength", "whole number"},
		{`"minimum":"1"`, "/minimum", "whole number"},
		{`"maximum":null`, "/maximum", "whole number"},
		{`"allOf":[]`, "/allOf", "non-empty array of schemas"},
		{`"anyOf":{}`, "/anyOf", "non-empty array of schemas"},
		{`"oneOf":[3]`, "/oneOf/0", "object or a boolean"},
		{`"not":[]`, "/not", "object or a boolean"},
		{`"if":1`, "/if", "object or a boolean"},
		{`"if":true,"then":2`, "/then", "object or a boolean"},
		{`"if":true,"else":"x"`, "/else", "object or a boolean"},
		{`"pattern":5`, "/pattern", "string"},
		{`"pattern":"("`, "/pattern", "pattern ("},
		{`"pattern":"(?i)x"`, "/pattern", "pattern (?i)x"},
		{`"pattern":"\\p{L}"`, "/pattern", "not in the syntax common"},
		{`"properties":{"a":{"$id":"urn:hive:schema:other:v1"}}`, "/properties/a/$id", "root of a schema file"},
		{`"properties":{"a":{"$schema":"` + metaSchema + `"}}`, "/properties/a/$schema", "root of a schema file"},
		{`"$ref":5`, "/$ref", "string"},
	}
	for _, c := range cases {
		_, err := one(c.body)
		wantProblem(t, err, testID+"#"+c.at, c.msg)
	}
}

// The profile refuses a schema for a reason spec/records.md gives; a schema
// that is odd but within it loads, and means what JSON Schema says.
func TestProfileLoadsWhatIsWithinIt(t *testing.T) {
	for _, body := range []string{
		`"enum":[]`,
		`"enum":[1,1]`,
		`"enum":[{"a":1,"b":2},{"b":2,"a":1}]`,
		`"then":false`,
		`"else":false`,
		`"minItems":3,"maxItems":2`,
		`"type":["string","null"],"minLength":3,"maxLength":2`,
		`"minimum":3,"maximum":2`,
		`"type":"object","additionalProperties":false,"required":["x"]`,
	} {
		mustOne(t, body)
	}
	if _, err := Load(fstest.MapFS{}, "."); err != nil {
		t.Errorf("a tree without a schema file: %v", err)
	}
	r := mustOne(t, `"enum":[]`)
	if err := r.Validate(testID, []byte(`1`)); err == nil {
		t.Error("an empty enum admitted a value")
	}
	r = mustOne(t, `"enum":[1,1]`)
	if err := r.Validate(testID, []byte(`1`)); err != nil {
		t.Error(err)
	}
	r = mustOne(t, `"then":false`)
	if err := r.Validate(testID, []byte(`1`)); err != nil {
		t.Errorf("a then without an if was applied: %v", err)
	}
	r = mustOne(t, `"type":"object","additionalProperties":false,"required":["x"]`)
	if err := r.Validate(testID, []byte(`{}`)); err == nil {
		t.Error("a missing required member was accepted")
	}
}

func TestProfileRefusesNonWholeNumbersInASchema(t *testing.T) {
	_, err := one(`"minimum":1.5`)
	wantProblem(t, err, testFile, "not whole")
	_, err = one(`"maxLength":1e2`)
	wantProblem(t, err, testFile, "not whole")
}

func TestProfileRefusesBadFiles(t *testing.T) {
	cases := []struct {
		name  string
		files fstest.MapFS
		where string
		msg   string
	}{
		{"no dialect", fstest.MapFS{testFile: {Data: []byte(`{"$id":"` + testID + `"}`)}}, testID + "#", "$schema must be"},
		{"other dialect", fstest.MapFS{testFile: {Data: []byte(`{"$schema":"http://json-schema.org/draft-07/schema#","$id":"` + testID + `"}`)}}, testID + "#", "$schema must be"},
		{"no id", fstest.MapFS{testFile: {Data: []byte(`{"$schema":"` + metaSchema + `"}`)}}, testID + "#", "$id must be"},
		{"wrong id", fstest.MapFS{testFile: file("urn:hive:schema:other:v1", "")}, testID + "#", "$id must be " + `"` + testID + `"`},
		{"wrong major", fstest.MapFS{testFile: file("urn:hive:schema:t:v2", "")}, testID + "#", "$id must be"},
		{"root true", fstest.MapFS{testFile: {Data: []byte(`true`)}}, testID + "#", "names its dialect"},
		{"root array", fstest.MapFS{testFile: {Data: []byte(`[]`)}}, testID + "#", "JSON object"},
		{"not json", fstest.MapFS{testFile: {Data: []byte(`{"$id":`)}}, testFile, "end of document"},
		{"duplicate keys", fstest.MapFS{testFile: {Data: []byte(`{"$id":"a","$id":"b"}`)}}, testFile, "duplicate member name"},
		{"upper case", fstest.MapFS{"T.v1.schema.json": file("urn:hive:schema:T:v1", "")}, "T.v1.schema.json", "lowercase"},
		{"no version", fstest.MapFS{"t.schema.json": file("urn:hive:schema:t", "")}, "t.schema.json", "<name>.v<major>"},
		{"version zero", fstest.MapFS{"t.v0.schema.json": file("urn:hive:schema:t:v0", "")}, "t.v0.schema.json", "<name>.v<major>"},
		{"minor in name", fstest.MapFS{"t.v1.1.schema.json": file("urn:hive:schema:t.v1:v1", "")}, "t.v1.1.schema.json", "<name>.v<major>"},
		{"not a schema", fstest.MapFS{"README.md": {Data: []byte("x")}}, "README.md", "<name>.v<major>"},
		{"bad directory", fstest.MapFS{"Dir/t.v1.schema.json": file("urn:hive:schema:Dir:t:v1", "")}, "Dir/t.v1.schema.json", "lowercase name"},
		{"wrong directory id", fstest.MapFS{"ops/t.v1.schema.json": file(testID, "")}, "urn:hive:schema:ops:t:v1#", `"urn:hive:schema:ops:t:v1"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(c.files, ".")
			wantProblem(t, err, c.where, c.msg)
		})
	}
}

func TestLoadDerivesIDsFromPaths(t *testing.T) {
	r, err := Load(fstest.MapFS{
		"schemas/a.v1.schema.json":                file("urn:hive:schema:a:v1", ""),
		"schemas/a.v2.schema.json":                file("urn:hive:schema:a:v2", ""),
		"schemas/receipts/x.y-z.v3.schema.json":   file("urn:hive:schema:receipts:x.y-z:v3", ""),
		"schemas/ops/c.op.request.v1.schema.json": file("urn:hive:schema:ops:c.op.request:v1", ""),
		"schemas/a/b/c.v1.schema.json":            file("urn:hive:schema:a:b:c:v1", ""),
	}, "schemas")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"urn:hive:schema:a:b:c:v1", "urn:hive:schema:a:v1", "urn:hive:schema:a:v2",
		"urn:hive:schema:ops:c.op.request:v1", "urn:hive:schema:receipts:x.y-z:v3",
	}
	if got := r.IDs(); !slices.Equal(got, want) {
		t.Errorf("IDs = %v, want %v", got, want)
	}
}

func TestLoadReportsEveryProblemInAFixedOrder(t *testing.T) {
	files := fstest.MapFS{
		"b.v1.schema.json": file("urn:hive:schema:b:v1", `"format":"x","$ref":"urn:hive:schema:nowhere:v1"`),
		"a.v1.schema.json": file("urn:hive:schema:a:v1", `"default":1,"minLength":-1`),
	}
	_, err := Load(files, ".")
	got := problemsOf(t, err)
	var where []string
	for _, p := range got {
		where = append(where, p.Where)
	}
	want := []string{
		"urn:hive:schema:a:v1#/default", "urn:hive:schema:a:v1#/minLength",
		"urn:hive:schema:b:v1#", "urn:hive:schema:b:v1#/format",
	}
	if !slices.Equal(where, want) {
		t.Errorf("problems at %v, want %v\n%v", where, want, err)
	}
	for range 5 {
		_, again := Load(files, ".")
		if again.Error() != err.Error() {
			t.Fatal("the report differs between runs")
		}
	}
}

func TestReferences(t *testing.T) {
	good := []struct{ name, body string }{
		{"whole document", `"$defs":{"a":{"$ref":"` + testID + `"}},"type":"string"`},
		{"definition", `"$defs":{"a":{"type":"string"},"b":{"$ref":"` + testID + `#/$defs/a"}}`},
		{"escaped pointer", `"$defs":{"a/b":{"type":"string"},"c~d":true,"r":{"$ref":"` + testID + `#/$defs/a~1b"},"s":{"$ref":"` + testID + `#/$defs/c~0d"}}`},
		{"empty fragment", `"$defs":{"a":{"$ref":"` + testID + `#"}},"type":"string"`},
		{"percent encoded leading slash", `"$defs":{"a":{"type":"string"},"r":{"$ref":"` + testID + `#%2F$defs%2Fa"}}`},
		{"consecutive tilde escapes", `"$defs":{"~~1":true,"r":{"$ref":"` + testID + `#/$defs/~0~01"}}`},
		{"percent encoded", `"$defs":{"a b":{"type":"string"},"r":{"$ref":"` + testID + `#/$defs/a%20b"}}`},
		{"boolean target", `"$defs":{"no":false,"r":{"$ref":"` + testID + `#/$defs/no"}}`},
		{"nested position", `"properties":{"p":{"type":"string"}},"$defs":{"r":{"$ref":"` + testID + `#/properties/p"}}`},
		{"array position", `"allOf":[{"type":"string"}],"$defs":{"r":{"$ref":"` + testID + `#/allOf/0"}}`},
		{"descending recursion", `"$defs":{"node":{"type":"object","additionalProperties":false,"properties":{"kids":{"type":"array","items":{"$ref":"` + testID + `#/$defs/node"}}}}}`},
	}
	for _, c := range good {
		t.Run("resolves "+c.name, func(t *testing.T) { mustOne(t, c.body) })
	}

	bad := []struct{ name, ref, msg string }{
		{"relative", `#/$defs/a`, "no relative reference"},
		{"empty", ``, "no relative reference"},
		{"network", `https://example.com/a.json`, "no relative reference"},
		{"other urn", `urn:other:thing`, "no relative reference"},
		{"prefix only", `urn:hive:schema:`, "no relative reference"},
		{"no slash", testID + `#a`, "JSON pointer"},
		{"bad percent", testID + `#/%zz`, "percent-encoded"},
		{"tilde escapes read twice", testID + `#/$defs/~~01`, "~ must be followed"},
		{"lone tilde", testID + `#/a~`, "~ must be followed"},
		{"bad tilde", testID + `#/a~2b`, "~ must be followed"},
		{"no such file", `urn:hive:schema:nowhere:v1`, "resolves to no schema file"},
		{"no such version", `urn:hive:schema:t:v2`, "resolves to no schema file"},
		{"no such pointer", testID + `#/$defs/missing`, "selects no schema"},
		{"not a schema", testID + `#/properties`, "selects no schema"},
		{"inside an enum", testID + `#/enum/0`, "selects no schema"},
		{"unnormalized pointer", testID + `#/$defs//a`, "selects no schema"},
	}
	for _, c := range bad {
		t.Run("refuses "+c.name, func(t *testing.T) {
			_, err := one(`"properties":{"p":true},"enum":[1],"$defs":{"a":true,"r":{"$ref":"` + c.ref + `"}}`)
			if err == nil || !strings.Contains(err.Error(), c.msg) {
				t.Errorf("$ref %q: %v, want it to say %q", c.ref, err, c.msg)
			}
		})
	}
}

func TestReferencesAcrossFiles(t *testing.T) {
	files := fstest.MapFS{
		"common.v1.schema.json": file("urn:hive:schema:common:v1", `"$defs":{"name":{"type":"string","minLength":1}}`),
		"x/rec.v1.schema.json":  file("urn:hive:schema:x:rec:v1", `"type":"object","additionalProperties":false,"properties":{"n":{"$ref":"urn:hive:schema:common:v1#/$defs/name"}}`),
	}
	r, err := Load(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Validate("urn:hive:schema:x:rec:v1", []byte(`{"n":"a"}`)); err != nil {
		t.Error(err)
	}
	if err := r.Validate("urn:hive:schema:x:rec:v1", []byte(`{"n":""}`)); err == nil {
		t.Error("a name shorter than the shared definition allows was accepted")
	}
	files["x/rec.v1.schema.json"] = file("urn:hive:schema:x:rec:v1", `"properties":{"n":{"$ref":"urn:hive:schema:common:v1#/$defs/gone"}}`)
	_, err = Load(files, ".")
	wantProblem(t, err, "urn:hive:schema:x:rec:v1#/properties/n", "selects no schema")
}

// A chain of references that returns to its start reads no more of the
// document and would never end; recursion that descends into the document is
// fine.
func TestReferenceCyclesAreRefused(t *testing.T) {
	cases := []struct{ name, body string }{
		{"self", `"$ref":"` + testID + `"`},
		{"pair", `"$defs":{"a":{"$ref":"` + testID + `#/$defs/b"},"b":{"$ref":"` + testID + `#/$defs/a"}}`},
		{"through allOf", `"$defs":{"a":{"allOf":[{"$ref":"` + testID + `#/$defs/a"}]}}`},
		{"through anyOf", `"$defs":{"a":{"anyOf":[true,{"$ref":"` + testID + `#/$defs/a"}]}}`},
		{"through oneOf", `"$defs":{"a":{"oneOf":[{"$ref":"` + testID + `#/$defs/a"}]}}`},
		{"through not", `"$defs":{"a":{"not":{"$ref":"` + testID + `#/$defs/a"}}}`},
		{"through if", `"$defs":{"a":{"if":{"$ref":"` + testID + `#/$defs/a"}}}`},
		{"through then", `"$defs":{"a":{"if":true,"then":{"$ref":"` + testID + `#/$defs/a"}}}`},
		{"through else", `"$defs":{"a":{"if":true,"else":{"$ref":"` + testID + `#/$defs/a"}}}`},
		{"into its own part", `"$defs":{"a":{"allOf":[{"$ref":"` + testID + `#/$defs/a/allOf/0"}]}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := one(c.body)
			if err == nil || !strings.Contains(err.Error(), "returns to this schema") {
				t.Errorf("%v, want a reference cycle", err)
			}
		})
	}
	// then and else apply only beside an if; without one they end no chain.
	for _, body := range []string{
		`"then":{"$ref":"` + testID + `"}`,
		`"else":{"$ref":"` + testID + `"}`,
		`"$defs":{"a":{"then":{"$ref":"` + testID + `#/$defs/a"},"else":{"$ref":"` + testID + `#/$defs/a"}}}`,
	} {
		mustOne(t, body)
	}
	files := fstest.MapFS{
		"a.v1.schema.json": file("urn:hive:schema:a:v1", `"$ref":"urn:hive:schema:b:v1"`),
		"b.v1.schema.json": file("urn:hive:schema:b:v1", `"$ref":"urn:hive:schema:a:v1"`),
	}
	if _, err := Load(files, "."); err == nil || !strings.Contains(err.Error(), "returns to this schema") {
		t.Errorf("across files: %v", err)
	}
}

func TestEveryObjectIsClosedButTheDesignatedOpenMembers(t *testing.T) {
	const open = `"$defs":{"open_object":{"type":"object"}}`
	files := func(body string) fstest.MapFS {
		return fstest.MapFS{
			"common.v1.schema.json": file("urn:hive:schema:common:v1", open),
			testFile:                file(testID, body),
		}
	}
	const openRef = "urn:hive:schema:common:v1#/$defs/open_object"
	ref := `{"$ref":"` + openRef + `"}`
	for _, member := range []string{"body", "detail", "unknown"} {
		for name, decl := range map[string]string{"bare": `{"type":"object"}`, "described": `{"type":"object","description":"x"}`, "shared": ref} {
			body := `"type":"object","additionalProperties":false,"properties":{"` + member + `":` + decl + `}`
			if _, err := Load(files(body), "."); err != nil {
				t.Errorf("%s member %s: %v", name, member, err)
			}
		}
	}

	bad := []struct{ name, body, at, msg string }{
		{"bare at the root", `"type":"object"`, "#", "only body, detail, unknown may be open"},
		{"bare member", `"properties":{"payload":{"type":"object"}}`, "#/properties/payload", "may be open"},
		{"bare in items", `"items":{"type":"object"}`, "#/items", "may be open"},
		{"bare in a branch", `"allOf":[{"type":"object"}]`, "#/allOf/0", "may be open"},
		{"bare under defs", `"$defs":{"anything":{"type":"object"}}`, "#/$defs/anything", "may be open"},
		{"shared open object elsewhere", `"properties":{"payload":` + ref + `}`, "#/properties/payload", "reaches an open object"},
		{"shared open object by an encoded reference", `"properties":{"payload":{"$ref":"urn:hive:schema:common:v1#/$defs/open%5Fobject"}}`, "#/properties/payload", "reaches an open object"},
		{"shared open object in a branch", `"allOf":[` + ref + `]`, "#/allOf/0", "reaches an open object"},
		{"shared open object through a definition", `"$defs":{"alias":` + ref + `},"properties":{"payload":{"$ref":"` + testID + `#/$defs/alias"}}`, "#/properties/payload", "reaches an open object"},
		{"open object declared in another file", `"$defs":{"open_object":{"type":"object"}}`, "#/$defs/open_object", "may be open"},
		{"properties without closing", `"type":"object","properties":{"a":true}`, "#", "sets additionalProperties to false"},
		{"explicitly open", `"type":"object","properties":{"a":true},"additionalProperties":true`, "#", "sets additionalProperties to false"},
		{"required only", `"type":"object","required":[]`, "#", "sets additionalProperties to false"},
		{"type list", `"type":["object","null"]`, "#", "may be open"},
		{"nested in a closed object", `"type":"object","additionalProperties":false,"properties":{"o":{"type":"object","properties":{}}}`, "#/properties/o", "sets additionalProperties to false"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(files(c.body), ".")
			wantProblem(t, err, testID+c.at, c.msg)
		})
	}

	// A designated member may share another designated member, and nothing else
	// may.
	other := func(body string) fstest.MapFS {
		return fstest.MapFS{
			"common.v1.schema.json": file("urn:hive:schema:common:v1", open),
			"other.v1.schema.json":  file("urn:hive:schema:other:v1", `"type":"object","additionalProperties":false,"properties":{"body":`+ref+`}`),
			testFile:                file(testID, body),
		}
	}
	shared := `{"$ref":"urn:hive:schema:other:v1#/properties/body"}`
	if _, err := Load(other(`"type":"object","additionalProperties":false,"properties":{"body":`+shared+`}`), "."); err != nil {
		t.Errorf("a designated member sharing another: %v", err)
	}
	_, err := Load(other(`"type":"object","additionalProperties":false,"properties":{"payload":`+shared+`}`), ".")
	wantProblem(t, err, testID+"#/properties/payload", "reaches an open object")

	// A closed object with nothing to list, and a conditional that names
	// properties without declaring an object, are both fine.
	mustOne(t, `"type":"object","additionalProperties":false`)
	mustOne(t, `"type":"object","additionalProperties":false,"properties":{"a":true},"if":{"required":["a"]},"then":{"properties":{"a":false}}`)
	mustOne(t, `"type":"string"`)
	mustOne(t, `"type":["string","null"]`)
}
