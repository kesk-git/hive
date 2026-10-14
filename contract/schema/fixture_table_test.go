package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// The invalid.jsonl of a schema directory: one refused example per line, each
// the accepted example named by base with an ordered list of edits applied.
// spec/layout.md rule 9 states the format; this file is its one reader.

// fixtureRow is one line of an invalid.jsonl.
type fixtureRow struct {
	Line  int
	Name  string
	Base  string
	Edits []fixtureEdit
}

// fixtureEdit is one change to a decoded base.
type fixtureEdit struct {
	Op       string // add, remove or replace
	Path     string // a JSON pointer
	Value    any    // as Decode yields it; for add and replace
	HasValue bool
}

var (
	caseName  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	arrayStep = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
)

// parseFixtureTable reads the rows of an invalid.jsonl and reports every
// fault, each starting with its line. A row with a fault is not returned.
func parseFixtureTable(data []byte) (rows []fixtureRow, problems []string) {
	if len(data) == 0 {
		return nil, []string{"the table holds no row"}
	}
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		return nil, []string{"the table begins with a byte-order mark"}
	}
	if !utf8.Valid(data) {
		return nil, []string{"the table is not UTF-8"}
	}
	if data[len(data)-1] != '\n' {
		problems = append(problems, "the last row does not end with a line feed")
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	prev := ""
	for i, text := range lines {
		n := i + 1
		fault := func(format string, args ...any) {
			problems = append(problems, fmt.Sprintf("line %d: ", n)+fmt.Sprintf(format, args...))
		}
		switch {
		case text == "":
			fault("an empty line")
			continue
		case strings.Contains(text, "\r"):
			fault("a carriage return")
			continue
		}
		row, err := parseFixtureRow([]byte(text))
		if err != nil {
			fault("%v", err)
			continue
		}
		row.Line = n
		if canon, err := encodeFixtureRow(row); err != nil {
			fault("%v", err)
			continue
		} else if string(canon) != text+"\n" {
			fault("%s is not written as the standard JSON encoder writes it, with no space and no HTML escaping", row.Name)
			continue
		}
		switch {
		case row.Name == prev:
			fault("%s repeats the name of the line before", row.Name)
			continue
		case row.Name < prev:
			fault("%s is out of order; rows are sorted by name", row.Name)
			continue
		}
		prev = row.Name
		rows = append(rows, row)
	}
	return rows, problems
}

// parseFixtureRow reads one row, refusing an unknown or repeated member and
// anything after the row. Every string and value is read with Decode, so the
// table holds no number or string the records of Hive refuse, and a value is
// checked at its own root.
func parseFixtureRow(line []byte) (fixtureRow, error) {
	var row fixtureRow
	members, err := strictMembers(line, "name", "base", "edits")
	if err != nil {
		return row, err
	}
	if row.Name, err = decodeString(members["name"]); err != nil {
		return row, fmt.Errorf("name: %v", err)
	}
	if row.Base, err = decodeString(members["base"]); err != nil {
		return row, fmt.Errorf("base: %v", err)
	}
	if !caseName.MatchString(row.Name) || strings.Contains(row.Name, "..") || strings.HasSuffix(row.Name, ".") {
		return row, fmt.Errorf("name %q is not a file name stem", row.Name)
	}
	if !caseName.MatchString(row.Base) || !strings.HasSuffix(row.Base, ".json") || strings.Contains(row.Base, "..") {
		return row, fmt.Errorf("base %q is not the name of a .json file", row.Base)
	}
	raw := bytes.TrimSpace(members["edits"])
	if len(raw) == 0 || raw[0] != '[' {
		return row, errors.New("edits is not an array")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		return row, err
	}
	for dec.More() {
		var el json.RawMessage
		if err := dec.Decode(&el); err != nil {
			return row, err
		}
		edit, err := parseFixtureEdit(el)
		if err != nil {
			return row, fmt.Errorf("edit %d: %v", len(row.Edits)+1, err)
		}
		row.Edits = append(row.Edits, edit)
	}
	if len(row.Edits) == 0 {
		return row, errors.New("edits is empty")
	}
	return row, nil
}

func parseFixtureEdit(raw []byte) (fixtureEdit, error) {
	var e fixtureEdit
	members, err := strictMembers(raw, "op", "path", "value")
	if err != nil {
		return e, err
	}
	if e.Op, err = decodeString(members["op"]); err != nil {
		return e, fmt.Errorf("op: %v", err)
	}
	if e.Path, err = decodeString(members["path"]); err != nil {
		return e, fmt.Errorf("path: %v", err)
	}
	value, has := members["value"]
	switch e.Op {
	case "add", "replace":
		if !has {
			return e, fmt.Errorf("%s has no value", e.Op)
		}
	case "remove":
		if has {
			return e, errors.New("remove has a value")
		}
	default:
		return e, fmt.Errorf("op %q is none of add, remove, replace", e.Op)
	}
	if has {
		if e.Value, err = Decode(value); err != nil {
			return e, fmt.Errorf("value: %v", err)
		}
		e.HasValue = true
	}
	return e, nil
}

// strictMembers splits a JSON object into the raw bytes of its members. A
// member not in allowed, a member given twice and anything after the object
// are refused, and every allowed member but value must be present.
func strictMembers(data []byte, allowed ...string) (map[string][]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	members := map[string][]byte{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		known := false
		for _, a := range allowed {
			known = known || a == key
		}
		if !known {
			return nil, fmt.Errorf("unknown member %q", key)
		}
		if _, dup := members[key]; dup {
			return nil, fmt.Errorf("member %q given twice", key)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		members[key] = raw
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("data after the object")
	}
	for _, a := range allowed {
		if _, ok := members[a]; !ok && a != "value" {
			return nil, fmt.Errorf("member %q is missing", a)
		}
	}
	return members, nil
}

func decodeString(raw []byte) (string, error) {
	v, err := Decode(raw)
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", errors.New("not a string")
	}
	return s, nil
}

// encodeFixtureRow writes a row the way the table holds it: one line of the
// standard JSON encoder with HTML escaping off and a final line feed.
func encodeFixtureRow(row fixtureRow) ([]byte, error) {
	edits := make([]any, 0, len(row.Edits))
	for _, e := range row.Edits {
		if e.HasValue {
			edits = append(edits, struct {
				Op    string `json:"op"`
				Path  string `json:"path"`
				Value any    `json:"value"`
			}{e.Op, e.Path, e.Value})
		} else {
			edits = append(edits, struct {
				Op   string `json:"op"`
				Path string `json:"path"`
			}{e.Op, e.Path})
		}
	}
	return encodeJSON(struct {
		Name  string `json:"name"`
		Base  string `json:"base"`
		Edits []any  `json:"edits"`
	}{row.Name, row.Base, edits})
}

func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// applyFixtureEdits applies edits in order to a freshly decoded base and
// returns the result. An edit that cannot be applied is an error.
func applyFixtureEdits(base any, edits []fixtureEdit) (any, error) {
	doc := base
	for i, e := range edits {
		steps, err := pointerSteps(e.Path)
		if err != nil {
			return nil, fmt.Errorf("edit %d: %v", i+1, err)
		}
		if doc, err = applyEdit(doc, steps, e); err != nil {
			return nil, fmt.Errorf("edit %d: %v", i+1, err)
		}
	}
	return doc, nil
}

// pointerSteps reads a JSON pointer, not a URI fragment: empty for the root,
// else slash-separated steps whose only escapes are ~0 and ~1.
func pointerSteps(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	if path[0] != '/' {
		return nil, fmt.Errorf("path %q is neither empty nor begins with /", path)
	}
	steps := strings.Split(path[1:], "/")
	for i, s := range steps {
		for j := 0; j < len(s); j++ {
			if s[j] == '~' && (j+1 == len(s) || (s[j+1] != '0' && s[j+1] != '1')) {
				return nil, fmt.Errorf("path %q holds a ~ that is not ~0 or ~1", path)
			}
		}
		steps[i] = strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
	}
	return steps, nil
}

// applyEdit applies one edit below node, which it returns.
func applyEdit(node any, steps []string, e fixtureEdit) (any, error) {
	if len(steps) == 0 {
		if e.Op != "replace" {
			return nil, fmt.Errorf("%s of the whole document; only replace applies there", e.Op)
		}
		return e.Value, nil
	}
	step, last := steps[0], len(steps) == 1
	switch parent := node.(type) {
	case map[string]any:
		old, present := parent[step]
		switch {
		case !last:
			if !present {
				return nil, fmt.Errorf("no member %q on the way to %q", step, e.Path)
			}
			child, err := applyEdit(old, steps[1:], e)
			if err != nil {
				return nil, err
			}
			parent[step] = child
		case e.Op == "add" && present:
			return nil, fmt.Errorf("add of %q, which is already there", e.Path)
		case e.Op != "add" && !present:
			return nil, fmt.Errorf("%s of %q, which is not there", e.Op, e.Path)
		case e.Op == "remove":
			delete(parent, step)
		default:
			parent[step] = e.Value
		}
	case []any:
		if !arrayStep.MatchString(step) {
			return nil, fmt.Errorf("%q is not the index of an array element", step)
		}
		i, err := strconv.Atoi(step)
		if err != nil || i >= len(parent) {
			return nil, fmt.Errorf("index %s is out of bounds, the array holds %d", step, len(parent))
		}
		if !last {
			child, err := applyEdit(parent[i], steps[1:], e)
			if err != nil {
				return nil, err
			}
			parent[i] = child
			break
		}
		if e.Op != "replace" {
			return nil, fmt.Errorf("%s of an array element; replace the whole array to change its length", e.Op)
		}
		parent[i] = e.Value
	default:
		return nil, fmt.Errorf("%q is below a value that holds no members", e.Path)
	}
	return node, nil
}

// constructFixture reads the base, applies the row's edits and writes the
// result as the document the validator is given.
func constructFixture(base []byte, row fixtureRow) ([]byte, error) {
	doc, err := Decode(base)
	if err != nil {
		return nil, fmt.Errorf("the base cannot be read: %v", err)
	}
	doc, err = applyFixtureEdits(doc, row.Edits)
	if err != nil {
		return nil, err
	}
	return encodeJSON(doc)
}

func TestFixtureTableReadsARow(t *testing.T) {
	const line = `{"name":"a.b","base":"ok.json","edits":[{"op":"replace","path":"/x~1y/0","value":null},{"op":"remove","path":"/z"},{"op":"add","path":"/w","value":{"k":[1,"<&>"]}}]}`
	rows, problems := parseFixtureTable([]byte(line + "\n"))
	if len(problems) > 0 || len(rows) != 1 {
		t.Fatalf("rows %v, problems %q", rows, problems)
	}
	row := rows[0]
	if row.Name != "a.b" || row.Base != "ok.json" || len(row.Edits) != 3 || row.Line != 1 {
		t.Fatalf("row = %+v", row)
	}
	if e := row.Edits[0]; e.Op != "replace" || e.Path != "/x~1y/0" || !e.HasValue || e.Value != nil {
		t.Errorf("a literal null was not kept: %+v", e)
	}
	if e := row.Edits[1]; e.HasValue {
		t.Errorf("a remove holds a value: %+v", e)
	}
	got, err := encodeFixtureRow(row)
	if err != nil || string(got) != line+"\n" {
		t.Errorf("the row does not encode as it was read:\n got %q (%v)\nwant %q", got, err, line+"\n")
	}
}

func TestFixtureTableRefusesAMistakenTable(t *testing.T) {
	ok := func(name string) string {
		return `{"name":"` + name + `","base":"ok.json","edits":[{"op":"remove","path":"/x"}]}`
	}
	for _, c := range []struct {
		why, table, says string
	}{
		{"an empty file", "", "no row"},
		{"a byte-order mark", "\xef\xbb\xbf" + ok("a") + "\n", "byte-order mark"},
		{"invalid UTF-8", ok("a") + "\xff\n", "not UTF-8"},
		{"no final line feed", ok("a"), "line feed"},
		{"an empty line", ok("a") + "\n\n" + ok("b") + "\n", "line 2: an empty line"},
		{"a carriage return", ok("a") + "\r\n", "carriage return"},
		{"a repeated name", ok("a") + "\n" + ok("a") + "\n", "repeats"},
		{"an unsorted name", ok("b") + "\n" + ok("a") + "\n", "out of order"},
		{"an unknown member", `{"name":"a","base":"ok.json","edits":[{"op":"remove","path":"/x"}],"note":1}` + "\n", "unknown member"},
		{"a repeated member", `{"name":"a","name":"b","base":"ok.json","edits":[{"op":"remove","path":"/x"}]}` + "\n", "given twice"},
		{"a missing member", `{"name":"a","base":"ok.json"}` + "\n", "missing"},
		{"an edit's unknown member", `{"name":"a","base":"ok.json","edits":[{"op":"remove","path":"/x","by":1}]}` + "\n", "unknown member"},
		{"data after the row", ok("a") + " 1\n", "after the object"},
		{"spaces", `{"name": "a","base":"ok.json","edits":[{"op":"remove","path":"/x"}]}` + "\n", "standard JSON encoder"},
		{"an HTML escape", `{"name":"a","base":"ok.json","edits":[{"op":"add","path":"/x","value":"\u003c"}]}` + "\n", "standard JSON encoder"},
		{"a name with a slash", ok("a/b") + "\n", "file name stem"},
		{"a dot segment name", ok("..") + "\n", "file name stem"},
		{"a base in a folder", `{"name":"a","base":"v/ok.json","edits":[{"op":"remove","path":"/x"}]}` + "\n", "base"},
		{"a base not .json", `{"name":"a","base":"ok.txt","edits":[{"op":"remove","path":"/x"}]}` + "\n", "base"},
		{"no edit", `{"name":"a","base":"ok.json","edits":[]}` + "\n", "edits is empty"},
		{"edits that are no array", `{"name":"a","base":"ok.json","edits":{}}` + "\n", "not an array"},
		{"an unknown op", `{"name":"a","base":"ok.json","edits":[{"op":"move","path":"/x"}]}` + "\n", "none of add"},
		{"an add with no value", `{"name":"a","base":"ok.json","edits":[{"op":"add","path":"/x"}]}` + "\n", "no value"},
		{"a remove with a value", `{"name":"a","base":"ok.json","edits":[{"op":"remove","path":"/x","value":1}]}` + "\n", "has a value"},
		{"a fraction", `{"name":"a","base":"ok.json","edits":[{"op":"add","path":"/x","value":1.5}]}` + "\n", "value"},
		{"a number past the limit", `{"name":"a","base":"ok.json","edits":[{"op":"add","path":"/x","value":9007199254740992}]}` + "\n", "value"},
		{"a repeated member inside a value", `{"name":"a","base":"ok.json","edits":[{"op":"add","path":"/x","value":{"k":1,"k":2}}]}` + "\n", "value"},
		{"a name that is no string", `{"name":1,"base":"ok.json","edits":[{"op":"remove","path":"/x"}]}` + "\n", "name"},
	} {
		rows, problems := parseFixtureTable([]byte(c.table))
		if len(rows) > 0 && strings.Count(c.table, "\n") <= 1 && c.why != "no final line feed" {
			t.Errorf("%s: a row was read: %+v", c.why, rows)
		}
		if !mentions(problems, c.says) {
			t.Errorf("%s: want a problem saying %q, got %q", c.why, c.says, problems)
		}
	}
}

func TestFixtureTableDecodesAValueAtItsOwnRoot(t *testing.T) {
	// A value as deep as Decode allows stays expressible although the row that
	// holds it is deeper still.
	deep := strings.Repeat("[", MaxDepth) + strings.Repeat("]", MaxDepth)
	line := `{"name":"a","base":"ok.json","edits":[{"op":"replace","path":"","value":` + deep + `}]}` + "\n"
	if rows, problems := parseFixtureTable([]byte(line)); len(rows) != 1 || len(problems) > 0 {
		t.Errorf("a value at the depth limit was refused: %q", problems)
	}
	deeper := strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1)
	line = `{"name":"a","base":"ok.json","edits":[{"op":"replace","path":"","value":` + deeper + `}]}` + "\n"
	if rows, _ := parseFixtureTable([]byte(line)); len(rows) != 0 {
		t.Error("a value past the depth limit was read")
	}
}

func TestFixtureEditsApply(t *testing.T) {
	edit := func(op, path string, value ...any) fixtureEdit {
		e := fixtureEdit{Op: op, Path: path}
		if len(value) > 0 {
			e.Value, e.HasValue = value[0], true
		}
		return e
	}
	const base = `{"a":1,"b":{"c":[10,20,{"d":null}]},"e/f":2,"g~h":3,"":4}`
	for _, c := range []struct {
		why   string
		edits []fixtureEdit
		want  string
	}{
		{"remove a member", []fixtureEdit{edit("remove", "/a")}, `{"":4,"b":{"c":[10,20,{"d":null}]},"e/f":2,"g~h":3}`},
		{"add a member", []fixtureEdit{edit("add", "/z", "s")}, `{"":4,"a":1,"b":{"c":[10,20,{"d":null}]},"e/f":2,"g~h":3,"z":"s"}`},
		{"replace with a literal null", []fixtureEdit{edit("replace", "/a", nil)}, `{"":4,"a":null,"b":{"c":[10,20,{"d":null}]},"e/f":2,"g~h":3}`},
		{"replace an array element", []fixtureEdit{edit("replace", "/b/c/1", int64(21))}, `{"":4,"a":1,"b":{"c":[10,21,{"d":null}]},"e/f":2,"g~h":3}`},
		{"reach below an array element", []fixtureEdit{edit("add", "/b/c/2/x", true)}, `{"":4,"a":1,"b":{"c":[10,20,{"d":null,"x":true}]},"e/f":2,"g~h":3}`},
		{"escaped steps", []fixtureEdit{edit("remove", "/e~1f"), edit("remove", "/g~0h")}, `{"":4,"a":1,"b":{"c":[10,20,{"d":null}]}}`},
		{"the empty member name", []fixtureEdit{edit("remove", "/")}, `{"a":1,"b":{"c":[10,20,{"d":null}]},"e/f":2,"g~h":3}`},
		{"the whole document", []fixtureEdit{edit("replace", "", []any{})}, `[]`},
		{"edits in order", []fixtureEdit{edit("replace", "", map[string]any{}), edit("add", "/k", int64(1))}, `{"k":1}`},
	} {
		doc, err := Decode([]byte(base))
		if err != nil {
			t.Fatal(err)
		}
		got, err := applyFixtureEdits(doc, c.edits)
		if err != nil {
			t.Errorf("%s: %v", c.why, err)
			continue
		}
		out, err := encodeJSON(got)
		if err != nil || strings.TrimSpace(string(out)) != c.want {
			t.Errorf("%s:\n got %s (%v)\nwant %s", c.why, out, err, c.want)
		}
	}

	for _, c := range []struct {
		why  string
		edit fixtureEdit
	}{
		{"add of an existing member", edit("add", "/a", int64(2))},
		{"remove of an absent member", edit("remove", "/zz")},
		{"replace of an absent member", edit("replace", "/zz", int64(1))},
		{"a missing parent", edit("add", "/zz/y", int64(1))},
		{"a scalar parent", edit("add", "/a/y", int64(1))},
		{"a path with no leading slash", edit("remove", "a")},
		{"a bad escape", edit("remove", "/g~2h")},
		{"a trailing tilde", edit("remove", "/g~")},
		{"add at the root", edit("add", "", int64(1))},
		{"remove of the root", edit("remove", "")},
		{"an index with a leading zero", edit("replace", "/b/c/01", int64(1))},
		{"a signed index", edit("replace", "/b/c/+1", int64(1))},
		{"an index out of bounds", edit("replace", "/b/c/3", int64(1))},
		{"the end-of-array step", edit("add", "/b/c/-", int64(1))},
		{"add to an array", edit("add", "/b/c/0", int64(1))},
		{"remove from an array", edit("remove", "/b/c/0")},
		{"a member name as an index", edit("replace", "/b/c/d", int64(1))},
	} {
		doc, err := Decode([]byte(base))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := applyFixtureEdits(doc, []fixtureEdit{c.edit}); err == nil {
			t.Errorf("%s: the edit was applied", c.why)
		}
	}
}

func TestFixtureTreeRunsTheCasesOfATable(t *testing.T) {
	r, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	const ok = "{}\n"
	row := func(name, base, edits string) string {
		return `{"name":"` + name + `","base":"` + base + `","edits":` + edits + `}` + "\n"
	}
	replace := func(path, value string) string {
		return `[{"op":"replace","path":"` + path + `","value":` + value + `}]`
	}
	for _, c := range []struct {
		why   string
		files map[string]string
		says  string // empty: the tree is correct
	}{
		{"a row and a raw refusal side by side", map[string]string{
			"common/valid/open_object.ok.json":           ok,
			"common/invalid.jsonl":                       row("open_object.array", "open_object.ok.json", replace("", "[]")),
			"common/invalid/open_object.unparsable.json": "{\n",
		}, ""},
		{"a raw refusal alone", map[string]string{
			"common/valid/open_object.ok.json":           ok,
			"common/invalid/open_object.unparsable.json": "{\n",
		}, ""},
		{"a row that the schema accepts", map[string]string{
			"common/valid/open_object.ok.json": ok,
			"common/invalid.jsonl":             row("open_object.same", "open_object.ok.json", `[{"op":"add","path":"/x","value":1}]`),
		}, "was accepted"},
		{"a row that the decoder refuses", map[string]string{
			"common/valid/open_object.ok.json": `{"a":1}` + "\n",
			"common/invalid.jsonl":             row("open_object.deep", "open_object.ok.json", replace("/a", strings.Repeat("[", MaxDepth)+strings.Repeat("]", MaxDepth))),
		}, "decoder refused"},
		{"a row with a base that is no file", map[string]string{
			"common/valid/open_object.ok.json": ok,
			"common/invalid.jsonl":             row("open_object.array", "nonesuch.json", replace("", "[]")),
		}, "no file under valid/"},
		{"a row with a base that the schema refuses", map[string]string{
			"common/valid/open_object.ok.json": "[]\n",
			"common/invalid.jsonl":             row("open_object.array", "open_object.ok.json", replace("", "[]")),
		}, "base"},
		{"a row with a base of another definition", map[string]string{
			"common/valid/open_object.ok.json": ok,
			"common/valid/home_id.ok.json":     `"h-1"` + "\n",
			"common/invalid.jsonl":             row("open_object.array", "home_id.ok.json", replace("", "[]")),
		}, "different definition"},
		{"a row that names no definition", map[string]string{
			"common/valid/open_object.ok.json": ok,
			"common/invalid.jsonl":             row("array", "open_object.ok.json", replace("", "[]")),
		}, "names no definition"},
		{"a row of an unknown definition", map[string]string{
			"common/valid/nonesuch.ok.json": ok,
			"common/invalid.jsonl":          row("nonesuch.array", "nonesuch.ok.json", replace("", "[]")),
		}, "could not be checked"},
		{"a row that cannot be applied", map[string]string{
			"common/valid/open_object.ok.json": ok,
			"common/invalid.jsonl":             row("open_object.array", "open_object.ok.json", `[{"op":"remove","path":"/x"}]`),
		}, "not there"},
		{"a row and a raw file of one name", map[string]string{
			"common/valid/open_object.ok.json":      ok,
			"common/invalid.jsonl":                  row("open_object.array", "open_object.ok.json", replace("", "[]")),
			"common/invalid/open_object.array.json": "[]\n",
		}, "both hold a case"},
		{"a table of no valid row", map[string]string{
			"common/valid/open_object.ok.json": ok,
			"common/invalid.jsonl":             "\n",
		}, "an empty line"},
		{"a table beside an empty invalid folder", map[string]string{
			"common/valid/open_object.ok.json": ok,
			"common/invalid.jsonl":             row("open_object.array", "open_object.ok.json", replace("", "[]")),
			"common/invalid/.keep":             "",
		}, "not a .json file"},
		{"a table with no accepted example", map[string]string{
			"common/invalid.jsonl": row("open_object.array", "open_object.ok.json", replace("", "[]")),
		}, "no accepted example"},
		{"an invalid.jsonl that is a folder", map[string]string{
			"common/valid/open_object.ok.json":      ok,
			"common/invalid.jsonl/open_object.json": "[]\n",
		}, "stray entry"},
	} {
		root := fixtureTree(t, c.files)
		got := checkFixtureTree(r, root, []string{commonID}, nil)
		switch {
		case c.says == "" && len(got) > 0:
			t.Errorf("%s: a correct tree was reported: %q", c.why, got)
		case c.says != "" && !mentions(got, c.says):
			t.Errorf("%s: want a report saying %q, got %q", c.why, c.says, got)
		}
	}
}

func TestFixtureRowIsRefusedByTheValidator(t *testing.T) {
	// A row proves the schema only when the validator refuses the document its
	// edits build, with the violation they were meant to cause.
	r, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	base := []byte(`{"a":1}` + "\n")
	row := fixtureRow{Name: "open_object.array", Base: "ok.json", Edits: []fixtureEdit{{Op: "replace", Path: "", Value: []any{}, HasValue: true}}}
	doc, err := constructFixture(base, row)
	if err != nil {
		t.Fatal(err)
	}
	err = r.Validate(commonID+"#/$defs/open_object", doc)
	var ve *ValidationError
	if !errors.As(err, &ve) || len(ve.Violations) != 1 || ve.Violations[0].Instance != "" || !strings.Contains(ve.Violations[0].Message, "array") {
		t.Fatalf("a root replaced by an array was refused as %v, want one violation at the document naming the array", err)
	}
	var se *SyntaxError
	if _, err := constructFixture([]byte("{\n"), row); err == nil || errors.As(err, &se) {
		t.Errorf("a base that does not decode built a case: %v", err)
	}
}
