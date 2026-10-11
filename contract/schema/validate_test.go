package schema

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
)

// accepts and rejects check one schema against instances, each a JSON text.
func accepts(t *testing.T, body string, instances ...string) {
	t.Helper()
	r := mustOne(t, body)
	for _, in := range instances {
		if err := r.Validate(testID, []byte(in)); err != nil {
			t.Errorf("schema {%s}: %s was refused: %v", body, in, err)
		}
	}
}

func rejects(t *testing.T, body string, instances ...string) {
	t.Helper()
	r := mustOne(t, body)
	for _, in := range instances {
		err := r.Validate(testID, []byte(in))
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("schema {%s}: %s gave %v, want a *ValidationError", body, in, err)
		}
	}
}

func TestType(t *testing.T) {
	samples := map[string]string{
		"null": `null`, "boolean": `true`, "integer": `3`, "string": `"s"`,
		"array": `[1]`, "object": `{}`,
	}
	for name, sample := range samples {
		kw := `"type":"` + name + `"`
		if name == "object" {
			kw += `,"additionalProperties":false`
		}
		accepts(t, kw, sample)
		for other, o := range samples {
			if other != name {
				rejects(t, kw, o)
			}
		}
	}
	accepts(t, `"type":"number"`, `0`, `-5`, `9007199254740991`)
	rejects(t, `"type":"number"`, `"1"`, `null`, `true`)
	accepts(t, `"type":["string","null"]`, `"s"`, `null`)
	rejects(t, `"type":["string","null"]`, `1`, `[]`, `{}`, `false`)
	rejects(t, `"type":"integer"`, `"3"`, `[3]`)
}

func TestEnumAndConst(t *testing.T) {
	accepts(t, `"enum":["a","b",1,null,true,[1,2],{"k":"v","j":0}]`,
		`"a"`, `"b"`, `1`, `null`, `true`, `[1,2]`, `{"j":0,"k":"v"}`)
	rejects(t, `"enum":["a","b",1,null,true,[1,2],{"k":"v","j":0}]`,
		`"c"`, `"A"`, `"1"`, `2`, `false`, `0`, `[2,1]`, `[1]`, `[1,2,3]`, `{"k":"v"}`, `{"k":"v","j":1}`, `{}`)
	rejects(t, `"enum":[0,false,""]`, `null`, `[]`, `{}`)
	rejects(t, `"enum":[1]`, `true`, `"1"`)
	accepts(t, `"const":{"a":[1,{"b":null}],"c":"d"}`, `{"c":"d","a":[1,{"b":null}]}`)
	rejects(t, `"const":{"a":[1,{"b":null}],"c":"d"}`, `{"c":"d","a":[1,{"b":0}]}`, `{"a":[1,{"b":null}]}`, `null`)
	accepts(t, `"const":null`, `null`)
	rejects(t, `"const":null`, `0`, `false`, `""`, `[]`, `{}`)
	accepts(t, `"const":"x"`, `"x"`)
	rejects(t, `"const":"x"`, `"x "`, `"X"`)
	// Strings compare as written: nothing normalizes them.
	accepts(t, j(`"const":"@u00e9"`), `"é"`)
	rejects(t, j(`"const":"@u00e9"`), j(`"e@u0301"`))
	accepts(t, `"const":false`, `false`)
	rejects(t, `"const":false`, `0`, `null`)
}

func TestObjectKeywords(t *testing.T) {
	const closed = `"type":"object","additionalProperties":false,"required":["a"],"properties":{"a":{"type":"integer"},"b":{"type":"string"}}`
	accepts(t, closed, `{"a":1}`, `{"a":1,"b":"x"}`, `{"b":"x","a":-1}`)
	rejects(t, closed, `{}`, `{"b":"x"}`, `{"a":"1"}`, `{"a":1,"b":2}`, `{"a":1,"c":1}`, `{"a":1,"A":1}`, `[]`, `"a"`)
	// The keywords look at objects only.
	accepts(t, `"required":["a"],"properties":{"a":{"type":"string"}}`, `5`, `"a"`, `[1]`, `null`)
	// A keyword names only what is present.
	accepts(t, `"properties":{"a":false}`, `{}`, `{"b":1}`)
	rejects(t, `"properties":{"a":false}`, `{"a":1}`, `{"a":null}`)
	accepts(t, `"type":"object","additionalProperties":false`, `{}`)
	rejects(t, `"type":"object","additionalProperties":false`, `{"a":1}`)
	// A closed object's members are not forgiven by a sibling that lists them.
	rejects(t, `"additionalProperties":false,"allOf":[{"properties":{"a":true}}]`, `{"a":1}`)
	accepts(t, `"required":[]`, `{}`)
	accepts(t, `"required":["a/b","~"]`, `{"a/b":1,"~":2}`)
}

func TestArrayKeywords(t *testing.T) {
	accepts(t, `"type":"array","items":{"type":"integer"}`, `[]`, `[1]`, `[1,2,3]`)
	rejects(t, `"type":"array","items":{"type":"integer"}`, `[1,"2"]`, `["1"]`, `[[]]`, `{}`)
	accepts(t, `"minItems":2,"maxItems":3`, `[1,2]`, `[1,2,3]`, `"ab"`, `5`)
	rejects(t, `"minItems":2,"maxItems":3`, `[]`, `[1]`, `[1,2,3,4]`)
	accepts(t, `"minItems":0`, `[]`)
	accepts(t, `"maxItems":0`, `[]`)
	rejects(t, `"maxItems":0`, `[1]`)
	accepts(t, `"items":false`, `[]`)
	rejects(t, `"items":false`, `[1]`)
	accepts(t, `"items":{"items":{"type":"string"}}`, `[["a"],[]]`)
	rejects(t, `"items":{"items":{"type":"string"}}`, `[["a"],[1]]`)
}

func TestStringKeywords(t *testing.T) {
	accepts(t, `"minLength":1,"maxLength":3`, `"a"`, `"abc"`, `5`, `[]`)
	rejects(t, `"minLength":1,"maxLength":3`, `""`, `"abcd"`)
	// Length counts characters, not bytes and not combining marks.
	accepts(t, `"maxLength":1`, `"é"`, `"😀"`, `"日"`)
	rejects(t, `"maxLength":1`, j(`"e@u0301"`), `"éa"`)
	accepts(t, `"maxLength":2`, j(`"@ud83d@ude00@ud83d@ude00"`), j(`"e@u0301"`))
	rejects(t, `"maxLength":2`, j(`"@ud83d@ude00@ud83d@ude00@ud83d@ude00"`))
	accepts(t, `"minLength":2`, `"日本"`, j(`"@ud83d@ude00x"`))
	rejects(t, `"minLength":2`, `"日"`)
	accepts(t, `"minLength":0`, `""`)
	accepts(t, `"maxLength":0`, `""`)
	// A pattern is a search unless it anchors itself.
	accepts(t, `"pattern":"a"`, `"a"`, `"xax"`, `"bba"`, `5`)
	rejects(t, `"pattern":"a"`, `""`, `"b"`, `"A"`)
	accepts(t, `"pattern":"^a$"`, `"a"`)
	rejects(t, `"pattern":"^a$"`, `"xa"`, `"ax"`, `"a\n"`, `""`)
	accepts(t, `"pattern":"^[a-z]+(-[a-z]+)*$"`, `"a"`, `"a-b"`)
	rejects(t, `"pattern":"^[a-z]+(-[a-z]+)*$"`, `"a-"`, `"-a"`, `"A"`, `"a--b"`)
}

func TestNumberKeywords(t *testing.T) {
	accepts(t, `"minimum":1,"maximum":3`, `1`, `2`, `3`, `"x"`, `[]`)
	rejects(t, `"minimum":1,"maximum":3`, `0`, `4`, `-1`)
	accepts(t, `"minimum":-5`, `-5`, `-4`, `0`)
	rejects(t, `"minimum":-5`, `-6`, `-9007199254740991`)
	accepts(t, `"maximum":-5`, `-5`, `-6`)
	rejects(t, `"maximum":-5`, `-4`, `0`)
	accepts(t, `"minimum":9007199254740991`, `9007199254740991`)
	accepts(t, `"type":"integer","minimum":1`, `1`)
}

func TestCombinators(t *testing.T) {
	const parts = `{"type":"integer"},{"minimum":0},{"maximum":9}`
	accepts(t, `"allOf":[`+parts+`]`, `0`, `5`, `9`)
	rejects(t, `"allOf":[`+parts+`]`, `-1`, `10`, `"1"`)
	accepts(t, `"anyOf":[{"type":"string"},{"type":"integer"}]`, `"a"`, `1`)
	rejects(t, `"anyOf":[{"type":"string"},{"type":"integer"}]`, `null`, `[]`, `true`)
	accepts(t, `"oneOf":[{"type":"string"},{"type":"integer"}]`, `"a"`, `1`)
	rejects(t, `"oneOf":[{"type":"string"},{"type":"integer"}]`, `null`)
	// oneOf is exactly one.
	rejects(t, `"oneOf":[{"minimum":0},{"maximum":9}]`, `5`)
	accepts(t, `"oneOf":[{"minimum":0},{"maximum":9}]`, `-1`, `10`)
	rejects(t, `"oneOf":[true,true]`, `1`)
	accepts(t, `"oneOf":[true,false]`, `1`)
	rejects(t, `"oneOf":[false,false]`, `1`)
	accepts(t, `"not":{"type":"string"}`, `1`, `null`)
	rejects(t, `"not":{"type":"string"}`, `"a"`)
	accepts(t, `"not":false`, `1`)
	rejects(t, `"not":true`, `1`)
	// Keywords of one schema all apply.
	accepts(t, `"type":"integer","minimum":1,"not":{"const":5}`, `1`, `6`)
	rejects(t, `"type":"integer","minimum":1,"not":{"const":5}`, `5`, `0`, `"a"`)
}

func TestConditionals(t *testing.T) {
	const cond = `"if":{"properties":{"k":{"const":"a"}},"required":["k"]},"then":{"required":["x"]},"else":{"required":["y"]}`
	accepts(t, cond, `{"k":"a","x":1}`, `{"k":"b","y":1}`, `{"y":1}`)
	rejects(t, cond, `{"k":"a"}`, `{"k":"a","y":1}`, `{"k":"b"}`, `{}`)
	accepts(t, `"if":{"type":"string"},"then":{"minLength":2}`, `"ab"`, `1`, `null`)
	rejects(t, `"if":{"type":"string"},"then":{"minLength":2}`, `"a"`)
	accepts(t, `"if":{"type":"string"},"else":{"type":"integer"}`, `"a"`, `1`)
	rejects(t, `"if":{"type":"string"},"else":{"type":"integer"}`, `null`, `[]`)
	accepts(t, `"if":false,"then":false`, `1`)
	rejects(t, `"if":true,"then":false`, `1`)
	accepts(t, `"if":{"type":"string"}`, `"a"`, `1`)
	// The condition itself never fails the schema.
	accepts(t, `"if":{"const":1},"then":true`, `2`)
}

func TestBooleanSchemas(t *testing.T) {
	accepts(t, `"$defs":{"t":true}`, `null`, `1`, `"a"`, `[]`, `{}`)
	r := mustOne(t, `"$defs":{"yes":true,"no":false}`)
	for _, in := range []string{`null`, `0`, `""`, `[]`, `{}`} {
		if err := r.Validate(testID+"#/$defs/yes", []byte(in)); err != nil {
			t.Errorf("true refused %s: %v", in, err)
		}
		if err := r.Validate(testID+"#/$defs/no", []byte(in)); err == nil {
			t.Errorf("false accepted %s", in)
		}
	}
}

// A $ref is one applicator among the others of its schema, not a replacement
// of them.
func TestReferenceWithSiblings(t *testing.T) {
	const body = `"$defs":{"n":{"type":"integer","minimum":1}},"properties":{"p":{"$ref":"` + testID + `#/$defs/n","maximum":3,"description":"d"}}`
	accepts(t, body, `{"p":1}`, `{"p":3}`)
	rejects(t, body, `{"p":0}`, `{"p":4}`, `{"p":"1"}`)
	accepts(t, `"$ref":"`+testID+`#/$defs/n","$defs":{"n":{"type":"string"}}`, `"a"`)
	rejects(t, `"$ref":"`+testID+`#/$defs/n","$defs":{"n":{"type":"string"}}`, `1`)
}

func TestRecursionThroughTheDocument(t *testing.T) {
	const tree = `"$ref":"` + testID + `#/$defs/node","$defs":{"node":{"type":"object","additionalProperties":false,"required":["v"],"properties":{"v":{"type":"integer"},"kids":{"type":"array","items":{"$ref":"` + testID + `#/$defs/node"}}}}}`
	accepts(t, tree, `{"v":1}`, `{"v":1,"kids":[{"v":2,"kids":[{"v":3}]},{"v":4}]}`)
	rejects(t, tree, `{"v":1,"kids":[{"v":2,"kids":[{"v":"3"}]}]}`, `{"v":1,"kids":[{"kids":[]}]}`)
	deep := strings.Repeat(`{"v":1,"kids":[`, 200) + `{"v":1}` + strings.Repeat(`]}`, 200)
	accepts(t, tree, deep)
}

func violationsOf(t *testing.T, err error) []Violation {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("got %v (%T), want a *ValidationError", err, err)
	}
	return ve.Violations
}

func TestViolationsSayWhereAndWhy(t *testing.T) {
	r := mustOne(t, `"type":"object","additionalProperties":false,"required":["z"],
	  "properties":{
	    "a/b":{"type":"string","minLength":2},
	    "m~n":{"type":"array","items":{"type":"integer"}},
	    "q":{"$ref":"`+testID+`#/$defs/d"},
	    "z":true
	  },
	  "$defs":{"d":{"pattern":"^x$"}}`)
	err := r.Validate(testID, []byte(`{"m~n":[1,"s",3,"t"],"a/b":"x","q":"y","extra":0}`))
	got := violationsOf(t, err)
	want := []Violation{
		{"", testID + "#/required", `lacks the required member "z"`},
		{"/a~1b", testID + "#/properties/a~1b/minLength", "has 1 characters, want at least 2"},
		{"/m~0n/1", testID + "#/properties/m~0n/items/type", "is string, want integer"},
		{"/m~0n/3", testID + "#/properties/m~0n/items/type", "is string, want integer"},
		{"/q", testID + "#/$defs/d/pattern", "does not match the pattern ^x$"},
		{"", testID + "#/additionalProperties", `has the member "extra", which the schema does not list`},
	}
	if len(got) != len(want) {
		t.Fatalf("violations:\n%v\nwant:\n%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("violation %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	// The same document gives the same report every time.
	for range 5 {
		again := violationsOf(t, r.Validate(testID, []byte(`{"m~n":[1,"s",3,"t"],"a/b":"x","q":"y","extra":0}`)))
		if fmt.Sprint(again) != fmt.Sprint(got) {
			t.Fatal("the report differs between runs")
		}
	}
	if !strings.Contains(err.Error(), `lacks the required member "z"`) {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestExtraMembersAreReportedInOrder(t *testing.T) {
	r := mustOne(t, `"type":"object","additionalProperties":false`)
	got := violationsOf(t, r.Validate(testID, []byte(`{"c":1,"a":1,"b":1}`)))
	if len(got) != 3 || !strings.Contains(got[0].Message, `"a"`) || !strings.Contains(got[2].Message, `"c"`) {
		t.Errorf("%v", got)
	}
}

func TestViolationsDoNotRepeatTheDocument(t *testing.T) {
	r := mustOne(t, `"properties":{"secret":{"pattern":"^public$","maxLength":3,"enum":["ok"],"const":"ok"}}`)
	const text = "hunter2-private-text"
	err := r.Validate(testID, []byte(`{"secret":"`+text+`"}`))
	if err == nil || strings.Contains(err.Error(), text) {
		t.Errorf("error %v repeats the document", err)
	}
	for _, v := range violationsOf(t, err) {
		if strings.Contains(v.String(), "hunter2") {
			t.Errorf("violation %v repeats the document", v)
		}
	}
}

func TestViolationsAreCapped(t *testing.T) {
	r := mustOne(t, `"items":{"type":"string"}`)
	items := strings.TrimSuffix(strings.Repeat("1,", 250), ",")
	var ve *ValidationError
	if !errors.As(r.Validate(testID, []byte("["+items+"]")), &ve) {
		t.Fatal("accepted")
	}
	if len(ve.Violations) != maxViolations || ve.Omitted != 150 {
		t.Errorf("%d violations, %d omitted", len(ve.Violations), ve.Omitted)
	}
	if !strings.Contains(ve.Error(), "and 247 more") {
		t.Errorf("Error() = %q", ve.Error())
	}
}

func TestValidateRefusesWhatIsNotARecord(t *testing.T) {
	r := mustOne(t, `"$defs":{"d":true}`)
	for _, in := range []string{`{"a":1.5}`, `{"a":1,"a":2}`, `[1e2]`, "\xef\xbb\xbf1", `{`, ``, `1 2`} {
		var se *SyntaxError
		if err := r.Validate(testID, []byte(in)); !errors.As(err, &se) {
			t.Errorf("%q: %v, want a *SyntaxError", in, err)
		}
	}
	// Even a schema that allows everything does not admit a fraction in a member
	// it never looks at.
	if err := r.Validate(testID, []byte(`{"unlisted":{"deep":[0.5]}}`)); err == nil {
		t.Error("a fraction deep in an unlisted member was accepted")
	}
}

func TestUnknownReferenceToValidateAgainst(t *testing.T) {
	r := mustOne(t, ``)
	for _, ref := range []string{"urn:hive:schema:nope:v1", testID + "#/$defs/none", "x", "", testID + "#a", "urn:hive:schema:t:v2"} {
		if err := r.Validate(ref, []byte(`1`)); !errors.Is(err, ErrUnknownSchema) {
			t.Errorf("Validate(%q) = %v, want ErrUnknownSchema", ref, err)
		}
		if err := r.ValidateValue(ref, int64(1)); !errors.Is(err, ErrUnknownSchema) {
			t.Errorf("ValidateValue(%q) = %v, want ErrUnknownSchema", ref, err)
		}
	}
}

func TestValidateValue(t *testing.T) {
	r := mustOne(t, `"$defs":{"anything":true,"int":{"type":"integer","minimum":1}}`)
	free := testID + "#/$defs/anything"
	whole := testID + "#/$defs/int"
	for _, v := range []any{nil, true, "s", 1, int64(2), float64(3), []any{1, "a", nil}, map[string]any{"a": []any{int64(1)}}, float64(-9007199254740991)} {
		if err := r.ValidateValue(free, v); err != nil {
			t.Errorf("%#v: %v", v, err)
		}
	}
	if err := r.ValidateValue(whole, float64(3)); err != nil {
		t.Errorf("a whole float64: %v", err)
	}
	if err := r.ValidateValue(whole, 0); err == nil {
		t.Error("0 passed minimum 1")
	}
	for name, v := range map[string]any{
		"fraction":       1.5,
		"nan":            math.NaN(),
		"infinity":       math.Inf(1),
		"too large":      float64(9007199254740992),
		"too small":      float64(-9007199254740992),
		"nested":         map[string]any{"a": []any{1, 0.1}},
		"invalid UTF-8":  "\xff",
		"bad member":     map[string]any{"\xff": 1},
		"struct":         struct{}{},
		"string slice":   []string{"a"},
		"typed map":      map[string]int{"a": 1},
		"unsigned":       uint(1),
		"pointer":        new(int),
		"float32":        float32(1),
		"complex number": complex(1, 1),
	} {
		var ve *ValidationError
		if err := r.ValidateValue(free, v); !errors.As(err, &ve) {
			t.Errorf("%s: %v, want a *ValidationError", name, err)
		} else if ve.Violations[0].Schema != "encoding" {
			t.Errorf("%s: %+v", name, ve.Violations[0])
		}
	}
	// A cyclic value ends in a refusal and not in a crash.
	cyc := map[string]any{}
	cyc["self"] = cyc
	if err := r.ValidateValue(free, cyc); err == nil || !strings.Contains(err.Error(), "nested too deeply") {
		t.Errorf("cycle: %v", err)
	}
	var deep any
	for range MaxDepth + 1 {
		deep = []any{deep}
	}
	if err := r.ValidateValue(free, deep); err == nil {
		t.Error("a value nested beyond MaxDepth was accepted")
	}
}

func TestRegistryIsSafeForConcurrentUse(t *testing.T) {
	r := mustOne(t, `"type":"object","additionalProperties":false,"properties":{"a":{"type":"integer","maximum":9}}`)
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				if err := r.Validate(testID, []byte(`{"a":1}`)); err != nil {
					t.Error(err)
					return
				}
				if err := r.Validate(testID, []byte(fmt.Sprintf(`{"a":%d}`, 10+i))); err == nil {
					t.Error("accepted")
					return
				}
			}
		}()
	}
	wg.Wait()
}

// Inside a condition, a schema is asked only whether it passes. That answer
// must be the one the full check gives, for every keyword.
func TestConditionsAgreeWithTheFullCheck(t *testing.T) {
	schemas := []string{
		`"type":"string"`, `"type":["integer","null"]`, `"enum":[1,"a",null]`, `"const":[1]`,
		`"minLength":2`, `"maxLength":1`, `"pattern":"^a"`,
		`"required":["a"]`, `"properties":{"a":{"type":"integer"}}`, `"additionalProperties":false`,
		`"required":["a"],"properties":{"a":true},"additionalProperties":false`,
		`"minItems":2`, `"maxItems":1`, `"items":{"type":"integer"}`,
		`"minimum":2`, `"maximum":1`,
		`"allOf":[{"type":"integer"},{"minimum":1}]`, `"anyOf":[{"type":"string"},{"minimum":3}]`,
		`"oneOf":[{"minimum":1},{"maximum":3}]`, `"not":{"type":"string"}`,
		`"if":{"type":"integer"},"then":{"minimum":2},"else":{"type":"string"}`,
		`"if":{"type":"integer"},"then":{"minimum":2}`, `"if":{"type":"integer"},"else":{"type":"string"}`,
		`"$ref":"` + testID + `#/$defs/s"`,
		`"type":"string","minLength":3,"pattern":"c$"`,
		`"minimum":1,"maximum":3,"not":{"const":2}`,
	}
	instances := []string{
		`null`, `true`, `0`, `1`, `2`, `3`, `5`, `""`, `"a"`, `"ab"`, `"abc"`, `[]`, `[1]`, `[1,2]`, `["x"]`,
		`{}`, `{"a":1}`, `{"a":"x"}`, `{"b":1}`,
	}
	const defs = `"$defs":{"s":{"type":"string","minLength":1}},`
	accepted := func(body, in string) bool {
		r := mustOne(t, defs+body)
		return r.Validate(testID, []byte(in)) == nil
	}
	for _, s := range schemas {
		for _, in := range instances {
			direct := accepted(s, in)
			for name, wrapped := range map[string]struct {
				body string
				want bool
			}{
				"not":   {`"not":{` + s + `}`, !direct},
				"anyOf": {`"anyOf":[{` + s + `},false]`, direct},
				"allOf": {`"allOf":[{` + s + `}]`, direct},
				"oneOf": {`"oneOf":[{` + s + `},false]`, direct},
				"if":    {`"if":{` + s + `},"then":true,"else":false`, direct},
			} {
				if got := accepted(wrapped.body, in); got != wrapped.want {
					t.Errorf("%s: {%s} on %s: direct %v, but %s gives %v", name, s, in, direct, name, got)
				}
			}
		}
	}
}
